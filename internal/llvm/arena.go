package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// arenaHeaderType names the runtime's KizuArena header in emitted modules.
// Element access reads these fields rather than calling the runtime for each
// one, the same reason Array does: the compiler keeps every AST node, IR
// instruction and type node in an arena and reaches them constantly, and a
// call the optimizer cannot see through costs more than the access. runtime.c
// asserts the same offsets, so the two spellings cannot drift apart.
const arenaHeaderType = "%kizu.arena"

// The field indices of arenaHeaderType.
const (
	arenaFieldData     = 0
	arenaFieldLen      = 1
	arenaFieldElemSize = 3
)

// arenaEmptyGlobal is the header read in place of a null handle. An all-zero
// header holds no elements, so every index is out of range on it -- the same
// null element the runtime returned for a null arena.
const arenaEmptyGlobal = "@kizu.arena.empty"

// writeArenaRuntimeDecls writes declarations for the hosted Arena runtime.
func (e *emitter) writeArenaRuntimeDecls() {
	if !e.usesArenaRuntime() {
		return
	}
	fmt.Fprintf(&e.out, "%s = type { ptr, i64, i64, i64, ptr }\n", arenaHeaderType)
	fmt.Fprintf(&e.out, "%s = private unnamed_addr global %s zeroinitializer\n",
		arenaEmptyGlobal, arenaHeaderType)
	e.out.WriteString("declare ptr @kizu_arena_new(ptr, i64)\n")
	e.out.WriteString("declare i64 @kizu_arena_add(ptr, ptr)\n")
	e.out.WriteString("declare i64 @kizu_arena_len(ptr)\n")
	e.out.WriteString("declare ptr @kizu_arena_pop(ptr)\n")
	e.out.WriteString("declare void @kizu_arena_deinit(ptr)\n\n")
}

// arenaHandle returns an operand that always points at a readable header.
func (e *emitter) arenaHandle(operand string) string {
	nullName := "%" + e.nextSyntheticValue("arena.handle.null")
	handleName := "%" + e.nextSyntheticValue("arena.handle")
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, operand)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr %s\n",
		handleName, nullName, arenaEmptyGlobal, operand)
	return handleName
}

// arenaLoadField loads one i64 header field.
func (e *emitter) arenaLoadField(handle string, field int, name string) string {
	addr := "%" + e.nextSyntheticValue(name+".addr")
	into := "%" + e.nextSyntheticValue(name)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 0, i32 %d\n",
		addr, arenaHeaderType, handle, field)
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", into, addr)
	return into
}

// arenaCheckedElement returns the address of the element a handle names, or
// null when the handle is outside the arena. The stride is the arena's own
// elem_size, which is what arena.new sized it by, so the address is the one
// kizu_arena_get computed. The comparison is unsigned, so a negative index is
// out of range without a second test.
func (e *emitter) arenaCheckedElement(arenaOperand string, index string, into string) string {
	handle := e.arenaHandle(arenaOperand)
	length := e.arenaLoadField(handle, arenaFieldLen, "arena.len")
	inRange := "%" + e.nextSyntheticValue("arena.in_range")
	fmt.Fprintf(&e.out, "  %s = icmp ult i64 %s, %s\n", inRange, index, length)
	elemSize := e.arenaLoadField(handle, arenaFieldElemSize, "arena.elem_size")
	offset := "%" + e.nextSyntheticValue("arena.offset")
	fmt.Fprintf(&e.out, "  %s = mul i64 %s, %s\n", offset, index, elemSize)
	dataAddr := "%" + e.nextSyntheticValue("arena.data.addr")
	data := "%" + e.nextSyntheticValue("arena.data")
	elemAddr := "%" + e.nextSyntheticValue("arena.elem")
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 0, i32 %d\n",
		dataAddr, arenaHeaderType, handle, arenaFieldData)
	fmt.Fprintf(&e.out, "  %s = load ptr, ptr %s\n", data, dataAddr)
	fmt.Fprintf(&e.out, "  %s = getelementptr i8, ptr %s, i64 %s\n", elemAddr, data, offset)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr null\n", into, inRange, elemAddr)
	return into
}

// usesArenaRuntime reports whether this module uses std::arena::Arena lowering.
func (e *emitter) usesArenaRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "arena.") {
					return true
				}
			}
		}
	}
	return false
}

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

// writeArenaLen returns the number of initialized elements still owned by the arena.
func (e *emitter) writeArenaLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: arena.len expects Arena<T> -> i64")
	}
	arena := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_arena_len(ptr %s)\n",
		resultName, arena.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaPopOrPanic moves the last element out for Arena.deinit's cleanup cascade.
func (e *emitter) writeArenaPopOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: arena.pop_or_panic expects Arena<T> -> T")
	}
	arena := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_arena_pop(ptr %s)\n", ptrName, arena.operand)
	e.writeNullFailure(instr, ptrName, "arena.pop.panic", "arena_empty")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaNew lowers std::arena::new<T>(allocator).
func (e *emitter) writeArenaNew(instr *ir.Instr) error {
	return e.writeContainerNew(instr, "kizu_arena_new",
		isArenaLLVMType, "arena.new expects allocator -> Arena<T>")
}

// writeArenaAdd lowers Arena.add(value) to an opaque i64 handle.
func (e *emitter) writeArenaAdd(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Result.Type) {
		return fmt.Errorf("llvm error: arena.add expects Arena<T>, T -> Handle<T>")
	}
	arena := e.value(instr.Args[0])
	valueSlot := e.writeStackValue(localName(instr.Result.Name)+".value", instr.Args[1])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_arena_add(ptr %s, ptr %s)\n",
		resultName, arena.operand, valueSlot)
	badName := resultName + ".bad"
	fmt.Fprintf(&e.out, "  %s = icmp slt i64 %s, 0\n", badName, resultName)
	e.writeBoolFailure(instr, badName, "arena.add", "arena_add")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaAt lowers Arena.at(handle) to a checked shared borrow. Borrows with
// value IR load a copy; address-spelled borrows (currently unions) keep the
// runtime element address.
func (e *emitter) writeArenaAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Args[1].Type) {
		return fmt.Errorf("llvm error: arena.at expects Arena<T>, Handle<T> -> &T")
	}
	arena := e.value(instr.Args[0])
	handle := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	e.arenaCheckedElement(arena.operand, handle.operand, ptrName)
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
// runtime's nullable element pointer becomes the payload and its presence,
// branch-free. It calls the same kizu_arena_get as Arena.at and skips both
// the trap and the load.
func (e *emitter) writeArenaAtMut(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Args[1].Type) {
		return fmt.Errorf("llvm error: arena.at_mut expects Arena<T>, Handle<T> -> ?&var T")
	}
	arena := e.value(instr.Args[0])
	handle := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	e.arenaCheckedElement(arena.operand, handle.operand, ptrName)
	return e.writeBorrowOptionalResult(instr, ptrName)
}

// writeArenaDeinit lowers Arena.deinit().
func (e *emitter) writeArenaDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: arena.deinit expects Arena<T> -> void")
	}
	arena := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_arena_deinit(ptr %s)\n", arena.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeBoolFailure reports the named failure when badOperand is true.
func (e *emitter) writeBoolFailure(
	instr *ir.Instr,
	badOperand string,
	prefix string,
	key string,
) {
	failLabel := helperLabel(badOperand, prefix+".fail")
	okLabel := helperLabel(badOperand, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", badOperand, failLabel, okLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n",
		panicEntries[key].entry, strings.Join(panicPosition(instr.Span), ", "))
	e.out.WriteString("  unreachable\n")
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}

// isArenaLLVMType reports whether a lowered IR type is a std::arena::Arena<T>.
func isArenaLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::arena::Arena<") && strings.HasSuffix(typ, ">")
}
