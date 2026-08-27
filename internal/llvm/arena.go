package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// An arena is the header an array is -- `{data, len, cap}` -- and owns its
// elements the same way, so everything below reaches for the array lowering
// rather than repeating it. What separates the two is above the backend: a
// Handle is an index rather than a borrow, and nothing is removed from the
// middle, which is why the index a handle carries stays the element's for as
// long as the arena lives. runtime.c has no arena entry points for the same
// reason (ADR-0131).

// writeArenaInstr dispatches runtime-backed Arena operations.
func (e *emitter) writeArenaInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "arena.new":
		return e.writeArenaNew(instr)
	case "arena.add":
		return e.writeArenaAdd(instr)
	case "arena.at":
		return e.writeArenaAt(instr)
	case "arena.at_mut":
		return e.writeArenaAtMut(instr)
	case "arena.len":
		return e.writeArenaLen(instr)
	case "arena.pop_or_panic":
		return e.writeArenaPopOrPanic(instr)
	case "arena.deinit":
		return e.writeArenaDeinit(instr)
	default:
		return fmt.Errorf("llvm error: unsupported arena instruction `%s`", instr.Op)
	}
}

// writeArenaNew lowers std::arena::new<T>(allocator) to the header value an
// empty arena is: three zero words, and no allocation to fail at. The first
// add is what buys storage, and it is the call that names an allocator and
// says whether it got any (ADR-0131).
func (e *emitter) writeArenaNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isArenaLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: arena.new expects allocator -> Arena<T>")
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "zeroinitializer"}
	return nil
}

// writeArenaAdd lowers Arena.add(allocator, value). The element goes where an
// append would put it, and the length the append started from is the handle:
// nothing is ever removed from the middle, so that index names the element for
// as long as the arena holds it. This is the call that buys storage, so an
// allocator that refuses comes back as the failure half of the union, the same
// way an append's does (ADR-0131).
func (e *emitter) writeArenaAdd(instr *ir.Instr) error {
	success, ok := e.errorUnionSuccessType(instr.Result.Type)
	if len(instr.Args) != 3 || !ok || !isArenaHandleType(success) {
		return fmt.Errorf(
			"llvm error: arena.add expects Arena<T>, Allocator, T -> std::mem::Error!Handle<T>")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	arena := e.value(instr.Args[0])
	handle := e.arrayHandle(arena.operand)
	okName := localName(instr.Result.Name) + ".ok"
	index := e.writeArrayAppendPaths(instr, elem, arena.operand, handle, okName)
	return e.writeErrorUnionFromBool(
		instr.Result, okName, "arena_add", e.arenaHandleOf(index), "i64")
}

// writeArenaLen returns the number of initialized elements still owned by the arena.
func (e *emitter) writeArenaLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: arena.len expects Arena<T> -> i64")
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	resultName := localName(instr.Result.Name)
	e.arrayLoadField(handle, arrayFieldLen, "arena.len", resultName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaPopOrPanic moves the last element out for Arena.deinit's cleanup cascade.
func (e *emitter) writeArenaPopOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: arena.pop_or_panic expects Arena<T> -> T")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	arena := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_pop(ptr %s, i64 %s)\n",
		ptrName, arena.operand, e.elementSizeOperand(elem))
	e.writeNullFailure(instr, ptrName, "arena.pop.panic", "arena_empty")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaAt lowers Arena.at(handle) to a checked shared borrow. Borrows with
// value IR load a copy; address-spelled borrows (currently unions) keep the
// element address.
func (e *emitter) writeArenaAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Args[1].Type) {
		return fmt.Errorf("llvm error: arena.at expects Arena<T>, Handle<T> -> &T")
	}
	ptrName, err := e.arenaCheckedElement(instr)
	if err != nil {
		return err
	}
	e.writeNullFailure(instr, ptrName, "arena.at", "arena_handle")
	if strings.HasPrefix(instr.Result.Type, "&") {
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: ptrName}
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaAtMut lowers Arena.at_mut(handle) to a borrow optional: the
// nullable element pointer becomes the payload and its presence, branch-free.
// It reaches the same element Arena.at does and skips both the trap and the
// load.
func (e *emitter) writeArenaAtMut(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Args[1].Type) {
		return fmt.Errorf("llvm error: arena.at_mut expects Arena<T>, Handle<T> -> ?&var T")
	}
	ptrName, err := e.arenaCheckedElement(instr)
	if err != nil {
		return err
	}
	return e.writeBorrowOptionalResult(instr, ptrName)
}

// arenaCheckedElement returns the address of the element a handle names, or
// null when the handle is outside the arena. The address is named after the
// result rather than synthesized, because continuationLabel predicts the label
// the trap continues at from that name before this block is written.
func (e *emitter) arenaCheckedElement(instr *ir.Instr) (string, error) {
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	index := e.arenaIndexOf(e.value(instr.Args[1]).operand)
	return e.arrayCheckedElement(instr, handle, index,
		localName(instr.Result.Name)+".ptr")
}

// arenaHandleOf turns the index an add settled on into the handle it hands
// back, and arenaIndexOf reads that index again. A handle is the index biased
// by one so that zero is a bit pattern no live handle carries, which is what
// lets `?std::arena::Handle<T>` be one word instead of two (ADR-0133). The
// bias is invisible above the backend: a handle is opaque and only ever
// compared for equality, and biasing is a bijection.
func (e *emitter) arenaHandleOf(index string) string {
	handle := "%" + e.nextSyntheticValue("arena.handle")
	fmt.Fprintf(&e.out, "  %s = add i64 %s, 1\n", handle, index)
	return handle
}

// arenaIndexOf undoes arenaHandleOf. An absent handle is zero, so the index it
// reads is -1, which the unsigned range test rejects like any other index past
// the end -- the same answer the element it does not name would get.
func (e *emitter) arenaIndexOf(handle string) string {
	index := "%" + e.nextSyntheticValue("arena.index")
	fmt.Fprintf(&e.out, "  %s = sub i64 %s, 1\n", index, handle)
	return index
}

// writeArenaDeinit lowers Arena.deinit(allocator). It releases the storage and
// nothing else: the elements are the caller's to consume first, the same way
// Array.deinit is handed an array whose owners are already gone.
func (e *emitter) writeArenaDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: arena.deinit expects Arena<T>, Allocator -> void")
	}
	return e.writeContainerStorageRelease(instr, "arena.deinit")
}

// isArenaLLVMType reports whether a lowered IR type is a std::arena::Arena<T>.
func isArenaLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::arena::Arena<") && strings.HasSuffix(typ, ">")
}
