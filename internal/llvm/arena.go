package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// An arena leads with the header an array is -- `{data, len, cap}` -- and owns
// its elements the same way, so everything below reaches for the array
// lowering rather than repeating it. The array runtime is handed a pointer to
// the header and reads only those three fields, so an append grows an arena
// the way it grows an array (ADR-0131). What separates the two is above the
// backend: a Handle is an index rather than a borrow, and nothing is removed
// from the middle, which is why the index a handle carries stays the element's
// for as long as the arena lives.
//
// A fourth word says which arena instance those three belong to. Handles from
// two arenas are otherwise the same small integers, so a handle that reached
// the wrong arena reads a live element of it and returns the wrong value
// without failing.
//
// That word is the number this arena's handles count from: its instance id in
// the half of a word an index does not need, plus the one the index is biased
// by. A handle is that number plus the element's index, and reading one
// subtracts it back off. The subtraction is what tells the arenas apart. A
// handle from elsewhere comes back off by a whole multiple of 2^32, which is
// past any length an arena can have, so the range test the read already ran
// rejects it -- the same answer, from the same comparison, as an index past
// the end. Nothing is added to the read but the word it subtracts.
//
// The one thing this asks in return is that a length stay under 2^32, which
// is why the add that would pass it stops instead.

// arenaHeaderType names the arena header in emitted modules. Its first three
// fields are arrayHeaderType's, at the same offsets, which is what lets the
// array runtime grow and release an arena's storage.
const arenaHeaderType = "%kizu.arena"

// arenaHeaderSize is what arenaHeaderType occupies: the array's three words
// and the number this arena's handles count from.
const arenaHeaderSize = 32

// arenaFieldOrigin is where arenaHeaderType keeps that number.
const arenaFieldOrigin = 3

// arenaEmptyGlobal is the arena's counterpart to arrayEmptyGlobal: the header
// read in place of a null one. It counts from zero, which no arena does, so a
// handle read against it fails the way any other handle from elsewhere does.
const arenaEmptyGlobal = "@kizu.arena.empty"

// arenaIndexBits is how much of a handle the index gets. An arena holding more
// elements than that would let a handle from another one land inside it, so
// arenaMaxLen is where an add stops.
const (
	arenaIndexBits = 32
	arenaMaxLen    = 1<<arenaIndexBits - 1
)

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
//
// What the arena counts its handles from is settled here rather than at the
// first add, because a construction is rarer than an element. It is asked for
// at run time rather than written as a constant because what it tells apart is
// instances: one `arena::new` reached twice builds two arenas, and a constant
// would give them the same handles and let each read the other's elements.
func (e *emitter) writeArenaNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isArenaLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: arena.new expects allocator -> Arena<T>")
	}
	originName := "%" + e.nextSyntheticValue("arena.new.origin")
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_arena_origin()\n", originName)
	headerName := "%" + e.nextSyntheticValue("arena.new.header")
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i64 %s, %d\n",
		headerName, arenaHeaderType, originName, arenaFieldOrigin)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: headerName}
	return nil
}

// writeArenaRuntimeDecls declares the arena header and the counter that hands
// out the number one instance's handles are told apart by.
func (e *emitter) writeArenaRuntimeDecls() {
	if !e.usesArenaHeader() {
		return
	}
	fmt.Fprintf(&e.out, "%s = type { ptr, i64, i64, i64 }\n", arenaHeaderType)
	if !e.usesArenaRuntime() {
		e.out.WriteByte('\n')
		return
	}
	fmt.Fprintf(&e.out, "%s = private unnamed_addr global %s zeroinitializer\n",
		arenaEmptyGlobal, arenaHeaderType)
	e.out.WriteString("declare i64 @kizu_arena_origin()\n\n")
}

// usesArenaHeader reports whether emitted values or aggregate definitions need
// the concrete Arena header independently of hosted-runtime calls.
func (e *emitter) usesArenaHeader() bool {
	if e.usesArenaRuntime() {
		return true
	}
	return e.moduleHasRuntimeHeaderType("std::arena::Arena<")
}

// usesArenaRuntime reports whether this module calls the hosted Arena runtime.
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

// arenaHandle returns an operand that always points at a readable arena
// header, the way arrayHandle does for an array's.
func (e *emitter) arenaHandle(operand string) string {
	nullName := "%" + e.nextSyntheticValue("arena.handle.null")
	handleName := "%" + e.nextSyntheticValue("arena.header")
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, operand)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr %s\n",
		handleName, nullName, arenaEmptyGlobal, operand)
	return handleName
}

// arenaOrigin reads the number an arena's handles count from.
func (e *emitter) arenaOrigin(header string, name string) string {
	addr := "%" + e.nextSyntheticValue(name+".addr")
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 0, i32 %d\n",
		addr, arenaHeaderType, header, arenaFieldOrigin)
	out := "%" + e.nextSyntheticValue(name)
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", out, addr)
	return out
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
	header := e.arenaHandle(arena.operand)
	e.writeArenaFullFailure(instr, header)
	okName := localName(instr.Result.Name) + ".ok"
	index := e.writeArrayAppendPaths(instr, elem, arena.operand, header, okName)
	return e.writeErrorUnionFromBool(
		instr.Result, okName, "arena_add", e.arenaHandleOf(header, index), "i64")
}

// writeArenaFullFailure stops an add that would carry the arena past the length
// its handles can name. Handles from two arenas are a whole 2^32 apart, so
// that distance is what makes a foreign handle land outside a range test
// (arenaCheckedElement); an arena allowed to grow that far would have room for
// one to land inside. The read stays two instructions by refusing the growth
// here, where an add is already asking the allocator for room.
func (e *emitter) writeArenaFullFailure(instr *ir.Instr, header string) {
	length := "%" + e.nextSyntheticValue("arena.add.len")
	e.arrayLoadField(header, arrayFieldLen, "arena.add.len.addr", length)
	room := "%" + e.nextSyntheticValue("arena.add.room")
	failLabel := helperLabel(room, "arena.add.full")
	okLabel := helperLabel(room, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  %s = icmp ult i64 %s, %d\n", room, length, arenaMaxLen)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", room, okLabel, failLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	e.writePanicCall(instr, "arena_full")
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
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
// null when the handle does not name one of this arena's. Subtracting what
// this arena counts from is the whole test: a handle it made comes back as the
// element's index, and a handle another arena made comes back off by a whole
// multiple of 2^32, which no length reaches. So the range test the read was
// already running answers both questions with the one comparison it already
// had, and the failure arrives at the one null the trap already watches for.
// The address is named after the result rather than synthesized, because
// continuationLabel predicts the label the trap continues at from that name
// before this block is written.
func (e *emitter) arenaCheckedElement(instr *ir.Instr) (string, error) {
	header := e.arenaHandle(e.value(instr.Args[0]).operand)
	index := e.arenaIndexOf(header, e.value(instr.Args[1]).operand)
	return e.arrayCheckedElement(instr, header, index,
		localName(instr.Result.Name)+".ptr")
}

// arenaHandleOf turns the index an add settled on into the handle it hands
// back, and arenaIndexOf reads that index again. A handle is the index plus
// what this arena counts from, which is its instance in the half of a word an
// index does not need plus the one that keeps zero free -- so no live handle
// is ever all zero, which is what lets `?std::arena::Handle<T>` be one word
// instead of two (ADR-0133). Both are invisible above the backend: a handle is
// opaque and only ever compared for equality, and offsetting is a bijection.
func (e *emitter) arenaHandleOf(header string, index string) string {
	origin := e.arenaOrigin(header, "arena.handle.origin")
	handle := "%" + e.nextSyntheticValue("arena.handle")
	fmt.Fprintf(&e.out, "  %s = add i64 %s, %s\n", handle, origin, index)
	return handle
}

// arenaIndexOf undoes arenaHandleOf. An absent handle is zero and a handle
// from another arena is off by a multiple of 2^32; both come back as indices
// the unsigned range test rejects like any other index past the end -- the
// same answer the element they do not name would get.
func (e *emitter) arenaIndexOf(header string, handle string) string {
	origin := e.arenaOrigin(header, "arena.origin")
	index := "%" + e.nextSyntheticValue("arena.index")
	fmt.Fprintf(&e.out, "  %s = sub i64 %s, %s\n", index, handle, origin)
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
