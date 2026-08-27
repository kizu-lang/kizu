package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// arrayHeaderType names the runtime's KizuArray header in emitted modules.
// Element access reads these fields rather than calling the runtime for each
// one: `kizu ir compiler` reaches an element 264 million times in one run, and
// a call the optimizer cannot see through costs more than the access. runtime.c
// asserts the same offsets, so the two spellings cannot drift apart.
const arrayHeaderType = "%kizu.array"

// arrayHeaderSize is what arrayHeaderType occupies: three words, the same
// three a Rust `Vec` carries. Neither the element size nor the allocator is
// among them -- the compiler knows T at every call that needs it and the
// source names the allocator at every call that needs one (ADR-0132), so a
// header carrying either would carry what the caller already has, in every
// array and in every element of an array of arrays.
const arrayHeaderSize = 24

// The field indices of arrayHeaderType.
const (
	arrayFieldData     = 0
	arrayFieldLen      = 1
	arrayFieldCapacity = 2
)

// arrayEmptyGlobal is the header read in place of a null handle. The runtime
// hands back null when the header itself could not be allocated, and answered
// every read on it from its own null checks; an all-zero header answers them
// the same way. It holds no capacity, so an append still goes back to the
// runtime and comes back as the failure.
const arrayEmptyGlobal = "@kizu.array.empty"

// writeArrayRuntimeDecls writes declarations for the hosted Array runtime.
func (e *emitter) writeArrayRuntimeDecls() {
	if !e.usesArrayRuntime() {
		return
	}
	fmt.Fprintf(&e.out, "%s = type { ptr, i64, i64 }\n", arrayHeaderType)
	fmt.Fprintf(&e.out, "%s = private unnamed_addr global %s zeroinitializer\n",
		arrayEmptyGlobal, arrayHeaderType)
	e.out.WriteString("declare i1 @kizu_array_append(ptr, ptr, ptr, i64)\n")
	e.out.WriteString("declare i1 @kizu_array_append_bytes(ptr, ptr, ptr, i64)\n")
	e.out.WriteString("declare i1 @kizu_array_reserve(ptr, ptr, i64, i64)\n")
	e.out.WriteString("declare ptr @kizu_array_pop(ptr, i64)\n")
	e.out.WriteString("declare i1 @kizu_array_swap(ptr, i64, i64, i64)\n")
	e.out.WriteString("declare i1 @kizu_array_truncate(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_array_clear(ptr)\n")
	e.out.WriteString("declare %kizu.slice.u8 @kizu_array_as_bytes(ptr)\n")
	e.out.WriteString("declare void @kizu_array_deinit(ptr, ptr, i64)\n\n")
}

// arrayHandle returns an operand that always points at a readable header.
func (e *emitter) arrayHandle(operand string) string {
	nullName := "%" + e.nextSyntheticValue("array.handle.null")
	handleName := "%" + e.nextSyntheticValue("array.handle")
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, operand)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr %s\n",
		handleName, nullName, arrayEmptyGlobal, operand)
	return handleName
}

// arrayFieldAddr returns the address of one header field.
func (e *emitter) arrayFieldAddr(handle string, field int, name string) string {
	addr := "%" + e.nextSyntheticValue(name)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 0, i32 %d\n",
		addr, arrayHeaderType, handle, field)
	return addr
}

// arrayLoadField loads one i64 header field into the named register.
func (e *emitter) arrayLoadField(handle string, field int, name string, into string) {
	addr := e.arrayFieldAddr(handle, field, name)
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", into, addr)
}

// arrayElementAddr returns the address of the element at index. The stride is
// the element type, which is what array.new sized the elements by, and the
// index is not checked here.
func (e *emitter) arrayElementAddr(handle string, elem string, index string) string {
	dataAddr := e.arrayFieldAddr(handle, arrayFieldData, "array.data.addr")
	data := "%" + e.nextSyntheticValue("array.data")
	elemAddr := "%" + e.nextSyntheticValue("array.elem")
	fmt.Fprintf(&e.out, "  %s = load ptr, ptr %s\n", data, dataAddr)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 %s\n",
		elemAddr, e.llvmType(elem), data, index)
	return elemAddr
}

// arrayCheckedElement returns the address of the element at index, or null when
// index is outside the array. The comparison is unsigned, so a negative index
// is out of range without a second test -- the same answer the runtime gave by
// returning a null element pointer.
func (e *emitter) arrayCheckedElement(
	instr *ir.Instr,
	handle string,
	index string,
	into string,
) (string, error) {
	elem, err := e.instrElementType(instr)
	if err != nil {
		return "", err
	}
	length := "%" + e.nextSyntheticValue("array.len")
	e.arrayLoadField(handle, arrayFieldLen, "array.len", length)
	inRange := "%" + e.nextSyntheticValue("array.in_range")
	fmt.Fprintf(&e.out, "  %s = icmp ult i64 %s, %s\n", inRange, index, length)
	elemAddr := e.arrayElementAddr(handle, elem, index)
	checked := into
	if checked == "" {
		checked = "%" + e.nextSyntheticValue("array.checked")
	}
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr null\n", checked, inRange, elemAddr)
	return checked, nil
}

// usesArrayRuntime reports whether this module uses the array header lowering.
// An arena is that header, so an arena op needs the same declarations even in
// a module that names no array.
func (e *emitter) usesArrayRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "array.") ||
					strings.HasPrefix(instr.Op, "arena.") {
					return true
				}
			}
		}
	}
	return false
}

// writeArrayInstr dispatches the Array operations that act on the container as a
// whole -- lifetime, size, and bulk views. Anything that reaches into a single
// element falls through to writeArrayElementInstr, which also owns the error for
// instructions neither half recognises.
func (e *emitter) writeArrayInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "array.new":
		return e.writeArrayNew(instr)
	case "array.len":
		return e.writeArrayLen(instr)
	case "array.capacity":
		return e.writeArrayCapacity(instr)
	case "array.reserve":
		return e.writeArrayReserve(instr)
	case "array.truncate":
		return e.writeArrayTruncate(instr)
	case "array.clear":
		return e.writeArrayClear(instr)
	case "array.as_bytes":
		return e.writeArrayAsBytes(instr)
	case "array.deinit":
		return e.writeArrayDeinit(instr)
	default:
		return e.writeArrayElementInstr(instr)
	}
}

// writeArrayElementInstr dispatches the Array operations that move or borrow a
// single element. All of them go through a pointer to one element slot -- read
// out of the header here for the ones that only need an address, handed back by
// the runtime for pop -- so they share the failure plumbing.
func (e *emitter) writeArrayElementInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "array.append":
		return e.writeArrayAppend(instr)
	case "array.append_bytes":
		return e.writeArrayAppendBytes(instr)
	case "array.pop":
		return e.writeArrayPop(instr)
	case "array.pop_or_panic":
		return e.writeArrayPopOrPanic(instr)
	case "array.get":
		return e.writeArrayGet(instr)
	case "array.get_or_panic":
		return e.writeArrayGetOrPanic(instr)
	case "array.at", "array.at_mut":
		return e.writeArrayAt(instr)
	case "array.set":
		return e.writeArraySet(instr)
	case "array.swap":
		return e.writeArraySwap(instr)
	default:
		return fmt.Errorf("llvm error: unsupported array instruction `%s`", instr.Op)
	}
}

// writeArraySwap exchanges two initialized slots without copying ownership.
func (e *emitter) writeArraySwap(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "i64" ||
		instr.Args[2].Type != "i64" || instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf(
			"llvm error: array.swap expects Array<T>, i64, i64 -> std::array::Error!void")
	}
	array := e.value(instr.Args[0])
	left := e.value(instr.Args[1])
	right := e.value(instr.Args[2])
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_swap(ptr %s, i64 %s, i64 %s, i64 %s)\n",
		okName, array.operand, left.operand, right.operand,
		e.elementSizeOperand(elem))
	return e.writeArrayBoolResult(instr.Result, okName, "array_swap")
}

// writeArrayNew lowers std::array::new<T>(allocator) to the header value an
// empty array is: three zero words. An empty array owns no storage, so it
// needs none, and it keeps nothing about how to grow either -- the allocator
// the constructor names is read by the checker, which requires every later
// call that allocates or releases to name the same one (ADR-0132). So the
// construction costs no instruction at all.
func (e *emitter) writeArrayNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isArrayLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: array.new expects allocator -> Array<T>")
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "zeroinitializer"}
	return nil
}

// writeArrayAppend lowers Array.append(allocator, value) and preserves !void
// failure flow.
func (e *emitter) writeArrayAppend(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"llvm error: array.append expects Array<T>, Allocator, T -> std::mem::Error!void")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	array := e.value(instr.Args[0])
	handle := e.arrayHandle(array.operand)
	okName := localName(instr.Result.Name) + ".ok"
	e.writeArrayAppendPaths(instr, elem, array.operand, handle, okName, "")
	return e.writeArrayBoolResult(instr.Result, okName, "array_append")
}

// writeArrayAppendPaths writes into the reserved tail when there is one and
// otherwise hands the append back to the runtime, which is what owns growing
// the storage. The slow path is given the handle as it came, not the readable
// stand-in, so a null handle still comes back as the failure it is.
//
// The length the append starts from is the index the element lands at. An
// array has no use for it; an arena hands it back as the handle that names the
// element, so it is read into indexName, the register the arena's result is
// resolved by (ADR-0131). An empty indexName synthesizes one.
func (e *emitter) writeArrayAppendPaths(
	instr *ir.Instr,
	elem string,
	handleOperand string,
	handle string,
	okName string,
	indexName string,
) {
	length := indexName
	if length == "" {
		length = "%" + e.nextSyntheticValue("array.append.len")
	}
	capacity := "%" + e.nextSyntheticValue("array.append.cap")
	lengthAddr := e.arrayFieldAddr(handle, arrayFieldLen, "array.append.len.addr")
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", length, lengthAddr)
	e.arrayLoadField(handle, arrayFieldCapacity, "array.append.cap", capacity)
	fits := "%" + e.nextSyntheticValue("array.append.fits")
	fmt.Fprintf(&e.out, "  %s = icmp slt i64 %s, %s\n", fits, length, capacity)
	fastLabel := helperLabel(okName, "array.append.fast")
	slowLabel := helperLabel(okName, "array.append.slow")
	joinLabel := helperLabel(okName, "array.append.join")
	e.markCurrentBlockExit(joinLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", fits, fastLabel, slowLabel)
	fmt.Fprintf(&e.out, "%s:\n", fastLabel)
	elemAddr := e.arrayElementAddr(handle, elem, length)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(instr.Args[2].Type), e.value(instr.Args[2]).operand, elemAddr)
	grown := "%" + e.nextSyntheticValue("array.append.grown")
	fmt.Fprintf(&e.out, "  %s = add i64 %s, 1\n", grown, length)
	fmt.Fprintf(&e.out, "  store i64 %s, ptr %s\n", grown, lengthAddr)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", slowLabel)
	elemSlot := e.writeStackValue(localName(instr.Result.Name)+".elem", instr.Args[2])
	slowOk := "%" + e.nextSyntheticValue("array.append.slow.ok")
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_append(ptr %s, ptr %s, ptr %s, i64 %s)\n",
		slowOk, e.value(instr.Args[1]).operand, handleOperand, elemSlot,
		e.elementSizeOperand(elem))
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	fmt.Fprintf(&e.out, "  %s = phi i1 [ true, %%%s ], [ %s, %%%s ]\n",
		okName, fastLabel, slowOk, slowLabel)
}

// writeBoundsFailure traps when index is outside the array. The comparison is
// unsigned, so a negative index is out of range without a second test.
func (e *emitter) writeBoundsFailure(instr *ir.Instr, index string, length string) {
	inRange := "%" + e.nextSyntheticValue("array.get.panic.in_range")
	failLabel := helperLabel(inRange, "array.get.panic.bounds")
	okLabel := helperLabel(inRange, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  %s = icmp ult i64 %s, %s\n", inRange, index, length)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", inRange, okLabel, failLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	e.writePanicCall(instr, "bounds", index, length)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}

// writeArrayLen lowers Array.len().
func (e *emitter) writeArrayLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: array.len expects Array<T> -> i64")
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	resultName := localName(instr.Result.Name)
	e.arrayLoadField(handle, arrayFieldLen, "array.len", resultName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayCapacity lowers Array.capacity().
func (e *emitter) writeArrayCapacity(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: array.capacity expects Array<T> -> i64")
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	resultName := localName(instr.Result.Name)
	e.arrayLoadField(handle, arrayFieldCapacity, "array.capacity", resultName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayAppendBytes lowers Array.append_bytes(bytes) for a u8 array: the
// run is copied in one runtime call rather than one element at a time.
func (e *emitter) writeArrayAppendBytes(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[2].Type != "[]u8" ||
		instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"llvm error: array.append_bytes expects" +
				" Array<u8>, Allocator, []u8 -> std::mem::Error!void")
	}
	array := e.value(instr.Args[0])
	allocator := e.value(instr.Args[1])
	slice := e.value(instr.Args[2])
	ptrName := "%" + e.nextSyntheticValue("array.append_bytes.ptr")
	lenName := "%" + e.nextSyntheticValue("array.append_bytes.len")
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n", ptrName, slice.operand)
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 1\n", lenName, slice.operand)
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out,
		"  %s = call i1 @kizu_array_append_bytes(ptr %s, ptr %s, ptr %s, i64 %s)\n",
		okName, allocator.operand, array.operand, ptrName, lenName)
	return e.writeArrayBoolResult(instr.Result, okName, "array_append_bytes")
}

// writeArrayReserve lowers Array.reserve(allocator, additional).
func (e *emitter) writeArrayReserve(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[2].Type != "i64" ||
		instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"llvm error: array.reserve expects Array<T>, Allocator, i64 -> std::mem::Error!void")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	array := e.value(instr.Args[0])
	allocator := e.value(instr.Args[1])
	additional := e.value(instr.Args[2])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_reserve(ptr %s, ptr %s, i64 %s, i64 %s)\n",
		okName, allocator.operand, array.operand, additional.operand,
		e.elementSizeOperand(elem))
	return e.writeArrayBoolResult(instr.Result, okName, "array_reserve")
}

// writeArrayPop lowers Array.pop().
func (e *emitter) writeArrayPop(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: array.pop expects Array<T> -> ?T")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	array := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_pop(ptr %s, i64 %s)\n",
		ptrName, array.operand, e.elementSizeOperand(elem))
	return e.writeArrayOptionalLoadResult(instr, ptrName, 0)
}

// writeArrayPopOrPanic moves the last element out or traps on an empty Array.
func (e *emitter) writeArrayPopOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: array.pop_or_panic expects Array<T> -> T")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	array := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_pop(ptr %s, i64 %s)\n",
		ptrName, array.operand, e.elementSizeOperand(elem))
	e.writeNullFailure(instr, ptrName, "array.pop.panic", "array_empty")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayGet lowers Array.get(index).
func (e *emitter) writeArrayGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.get expects Array<T>, i64 -> ?T")
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	index := e.value(instr.Args[1])
	checked, err := e.arrayCheckedElement(instr, handle, index.operand, "")
	if err != nil {
		return err
	}
	return e.writeArrayOptionalLoadResult(instr, checked, 0)
}

// writeArrayGetOrPanic lowers Array.get_or_panic(index).
func (e *emitter) writeArrayGetOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.get_or_panic expects Array<T>, i64 -> T")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	index := e.value(instr.Args[1])
	lenName := "%" + e.nextSyntheticValue("array.get.panic.len")
	e.arrayLoadField(handle, arrayFieldLen, "array.get.panic.len", lenName)
	e.writeBoundsFailure(instr, index.operand, lenName)
	elemAddr := e.arrayElementAddr(handle, elem, index.operand)
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), elemAddr)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayAt lowers Array.at(index) and Array.at_mut(index) to a borrow
// optional: the runtime's nullable element pointer becomes the payload and its
// presence, branch-free.
func (e *emitter) writeArrayAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.at expects Array<T>, i64 -> ?&T")
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	index := e.value(instr.Args[1])
	checked, err := e.arrayCheckedElement(instr, handle, index.operand, "")
	if err != nil {
		return err
	}
	return e.writeBorrowOptionalResult(instr, checked)
}

// writeArraySet lowers Array.set(index, value) and preserves !void failure flow.
func (e *emitter) writeArraySet(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "i64" ||
		instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf("llvm error: array.set expects Array<T>, i64, T -> std::array::Error!void")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	handle := e.arrayHandle(e.value(instr.Args[0]).operand)
	index := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	e.writeArrayCheckedStore(handle, elem, index.operand, instr.Args[2], okName)
	return e.writeArrayBoolResult(instr.Result, okName, "array_bounds")
}

// writeArrayCheckedStore writes value at index when index is inside the array,
// and leaves okName saying whether it did.
func (e *emitter) writeArrayCheckedStore(
	handle string,
	elem string,
	index string,
	value ir.Value,
	okName string,
) {
	length := "%" + e.nextSyntheticValue("array.set.len")
	e.arrayLoadField(handle, arrayFieldLen, "array.set.len", length)
	inRange := "%" + e.nextSyntheticValue("array.set.in_range")
	fmt.Fprintf(&e.out, "  %s = icmp ult i64 %s, %s\n", inRange, index, length)
	storeLabel := helperLabel(okName, "array.set.store")
	skipLabel := helperLabel(okName, "array.set.skip")
	joinLabel := helperLabel(okName, "array.set.join")
	e.markCurrentBlockExit(joinLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", inRange, storeLabel, skipLabel)
	fmt.Fprintf(&e.out, "%s:\n", storeLabel)
	elemAddr := e.arrayElementAddr(handle, elem, index)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(value.Type), e.value(value).operand, elemAddr)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", skipLabel)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	fmt.Fprintf(&e.out, "  %s = phi i1 [ true, %%%s ], [ false, %%%s ]\n",
		okName, storeLabel, skipLabel)
}

// writeArrayTruncate lowers Array.truncate(length).
func (e *emitter) writeArrayTruncate(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" ||
		instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf("llvm error: array.truncate expects Array<T>, i64 -> std::array::Error!void")
	}
	array := e.value(instr.Args[0])
	length := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_truncate(ptr %s, i64 %s)\n",
		okName, array.operand, length.operand)
	return e.writeArrayBoolResult(instr.Result, okName, "array_truncate")
}

// writeArrayClear lowers Array.clear().
func (e *emitter) writeArrayClear(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: array.clear expects Array<T> -> void")
	}
	array := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_array_clear(ptr %s)\n", array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeArrayAsBytes lowers Array<u8>.as_bytes().
func (e *emitter) writeArrayAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "[]u8" {
		return fmt.Errorf("llvm error: array.as_bytes expects Array<u8> -> []u8")
	}
	array := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call %%kizu.slice.u8 @kizu_array_as_bytes(ptr %s)\n",
		resultName, array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayDeinit lowers Array.deinit().
func (e *emitter) writeArrayDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: array.deinit expects Array<T>, Allocator -> void")
	}
	return e.writeContainerStorageRelease(instr, "array.deinit")
}

// writeContainerStorageRelease releases the storage a container header owns:
// the data pointer, measured by the capacity it was sized at. Array.deinit and
// Arena.deinit are both this, since the two are the same header and whatever
// owners the elements held are already gone by the time either runs.
func (e *emitter) writeContainerStorageRelease(instr *ir.Instr, what string) error {
	container := e.value(instr.Args[0])
	// A cleanup carries no immediate, so the element the release measures is
	// read from the container it was handed: the one place the type is written
	// down either way.
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	data := e.arrayFieldOf(container.operand, arrayFieldData, what+".data")
	capacity := e.arrayFieldOf(container.operand, arrayFieldCapacity, what+".cap")
	allocator := e.value(instr.Args[1])
	bytes := "%" + e.nextSyntheticValue(what+".bytes")
	fmt.Fprintf(&e.out, "  %s = mul i64 %s, %s\n",
		bytes, capacity, e.elementSizeOperand(elem))
	fmt.Fprintf(&e.out, "  call void @kizu_array_deinit(ptr %s, ptr %s, i64 %s)\n",
		allocator.operand, data, bytes)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// arrayFieldOf reads one field out of an array header value.
func (e *emitter) arrayFieldOf(value string, field int, name string) string {
	out := "%" + e.nextSyntheticValue(name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n", out, arrayHeaderType, value, field)
	return out
}

// writeArrayOptionalLoadResult converts a nullable element pointer to ?T:
// null becomes the all-zero optional and a live pointer loads the payload.
// loadAlign is what the pointer is known to be aligned to, or 0 for the
// payload type's own alignment: container storage is laid out for its element,
// but a map key blob is sized by the key and aligned by whatever allocator
// handed it out, so that load names 1.
func (e *emitter) writeArrayOptionalLoadResult(
	instr *ir.Instr,
	ptrName string,
	loadAlign int,
) error {
	elem, ok := optionalElemLLVM(instr.Result.Type)
	if !ok {
		return fmt.Errorf(
			"llvm error: %s expects a `?T` result, got %s", instr.Op, instr.Result.Type)
	}
	resultName := localName(instr.Result.Name)
	nullLabel, okLabel, joinLabel := arrayResultLabels(instr.Result.Name, "array")
	e.markCurrentBlockExit(joinLabel)
	nullName := resultName + ".is_null"
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, ptrName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", nullName, nullLabel, okLabel)
	optType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "%s:\n", nullLabel)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	valueName := resultName + ".value"
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s%s\n",
		valueName, e.llvmType(elem), ptrName, alignSuffix(loadAlign))
	someName := resultName + ".some"
	okName := resultName + ".ok"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n", someName, optType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		okName, optType, someName, e.llvmType(elem), valueName)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	fmt.Fprintf(&e.out, "  %s = phi %s [ zeroinitializer, %%%s ], [ %s, %%%s ]\n",
		resultName, optType, nullLabel, okName, okLabel)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayBoolResult converts a runtime boolean into a !void result.
func (e *emitter) writeArrayBoolResult(
	result ir.Value,
	okOperand string,
	failureKey string,
) error {
	code, err := e.failureErrorCode(failureKey)
	if err != nil {
		return err
	}
	resultName := localName(result.Name)
	unionType := e.llvmType(result.Type)
	baseName := resultName + ".base"
	okByteName := resultName + ".ok.byte"
	fmt.Fprintf(&e.out, "  %s = zext i1 %s to i8\n", okByteName, okOperand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 %s, 0\n",
		baseName, unionType, okByteName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, i64 %d, %d\n",
		resultName, unionType, baseName, code, e.errorUnionFailureIndex(result.Type))
	e.values[result.Name] = valueInfo{typ: result.Type, operand: resultName}
	return nil
}

// writeNullFailure reports the named failure when the pointer is null, which is
// how the hosted Array runtime signals a refused access.
func (e *emitter) writeNullFailure(
	instr *ir.Instr,
	ptrName string,
	prefix string,
	key string,
	args ...string,
) {
	nullName := "%" + e.nextSyntheticValue(prefix+".is_null")
	failLabel := helperLabel(ptrName, prefix+".null")
	okLabel := helperLabel(ptrName, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, ptrName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", nullName, failLabel, okLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	e.writePanicCall(instr, key, args...)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}

// writePanicCall writes the trap that ends a refused access.
func (e *emitter) writePanicCall(instr *ir.Instr, key string, args ...string) {
	spec := panicEntries[key]
	typed := make([]string, 0, len(args)+2)
	for i, arg := range args {
		typed = append(typed, spec.params[i]+" "+arg)
	}
	typed = append(typed, panicPosition(instr.Span)...)
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n", spec.entry, strings.Join(typed, ", "))
	e.out.WriteString("  unreachable\n")
}

// writeStackValue stores a first-class value in a temporary slot and returns its pointer.
func (e *emitter) writeStackValue(baseName string, value ir.Value) string {
	slotName := baseName + ".slot"
	valueInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slotName, e.llvmType(value.Type))
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(value.Type), valueInfo.operand, slotName)
	return slotName
}

// writeStaticStringGlobal writes one private string constant.
func (e *emitter) writeStaticStringGlobal(name string, value string) {
	fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n",
		name, len(value)+1, escapeString(value))
}

// arrayResultLabels creates fail, success, and join labels for checked Array operations.
func arrayResultLabels(result string, prefix string) (string, string, string) {
	failLabel := helperLabel(result, prefix+".fail")
	okLabel := helperLabel(result, prefix+".ok")
	joinLabel := helperLabel(result, prefix+".join")
	return failLabel, okLabel, joinLabel
}

// elementSizeOperand returns a LLVM constant expression for sizeof(T).
func (e *emitter) elementSizeOperand(typ string) string {
	llvmType := e.llvmType(typ)
	if llvmType == "ptr" {
		return "8"
	}
	return fmt.Sprintf("ptrtoint (ptr getelementptr (%s, ptr null, i64 1) to i64)", llvmType)
}

// isArrayLLVMType reports whether a lowered IR type is a std::array::Array<T>.
func isArrayLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::array::Array<") && strings.HasSuffix(typ, ">")
}

// instrElementType names the type an instruction measures, read from the value
// it was handed rather than from a separate field. A container spells its
// element in its own type, so the two can never disagree and no instruction can
// arrive without one -- a cleanup carries no immediate, and a release measured
// against a missing type would free the wrong number of bytes.
//
// Which value carries it depends on the instruction: a constructor names the
// container it returns, and everything else was handed the container it works
// on. Reading whichever happens to match would take the wrong answer for an
// element that is itself a container, where the result of a `pop` is an Array
// whose own element is not what the outer storage is sized by.
func (e *emitter) instrElementType(instr *ir.Instr) (string, error) {
	if isContainerConstructor(instr.Op) {
		if elem, ok := e.containerElementOfResult(instr.Result.Type); ok {
			return elem, nil
		}
		return "", fmt.Errorf(
			"llvm error: emitter bug: `%s` returns `%s`, which names no container",
			instr.Op, instr.Result.Type)
	}
	if len(instr.Args) > 0 {
		if elem, ok := containerElementOf(withoutBorrow(instr.Args[0].Type)); ok {
			return elem, nil
		}
	}
	return "", fmt.Errorf(
		"llvm error: emitter bug: `%s` measures an element but was handed no container",
		instr.Op)
}

// isContainerConstructor reports the instructions whose container is the value
// they return rather than the one they were handed.
func isContainerConstructor(op string) bool {
	switch op {
	case "array.new", "map.new", "arena.new", "box.new":
		return true
	}
	return false
}

// containerElementOfResult names what a constructor's result holds: the element
// for an Array, Arena or Box, and the value for a Map, which is what its
// storage is sized by.
func (e *emitter) containerElementOfResult(typ string) (string, bool) {
	if success, ok := e.errorUnionSuccessType(typ); ok {
		typ = success
	}
	return containerElementOf(typ)
}

// withoutBorrow drops the borrow a spelling was reached through.
func withoutBorrow(typ string) string {
	return strings.TrimPrefix(strings.TrimPrefix(typ, "&var "), "&")
}

// containerElementOf names the static argument a container spelling carries:
// the element for an Array, Arena or Box, and the value for a Map, which is
// what its storage is sized by.
func containerElementOf(typ string) (string, bool) {
	if elem, ok := arrayElementLLVMType(typ); ok {
		return elem, true
	}
	if elem, ok := genericArgOf(typ, "std::arena::Arena<"); ok {
		return elem, true
	}
	if elem, ok := genericArgOf(typ, "std::mem::Box<"); ok {
		return elem, true
	}
	if args, ok := genericArgOf(typ, "std::map::Map<"); ok {
		if parts := splitTopLevelCommas(args); len(parts) == 2 {
			return strings.TrimSpace(parts[1]), true
		}
	}
	return "", false
}

// genericArgOf returns the static arguments a spelling carries under prefix.
func genericArgOf(typ string, prefix string) (string, bool) {
	if !strings.HasPrefix(typ, prefix) || !strings.HasSuffix(typ, ">") {
		return "", false
	}
	return typ[len(prefix) : len(typ)-1], true
}

// arrayElementLLVMType names T for a `std::array::Array<T>` spelling, through a
// borrow of one.
func arrayElementLLVMType(typ string) (string, bool) {
	typ = strings.TrimPrefix(strings.TrimPrefix(typ, "&var "), "&")
	if !isArrayLLVMType(typ) {
		return "", false
	}
	return typ[len("std::array::Array<") : len(typ)-1], true
}

// alignSuffix renders the `, align N` an LLVM load carries when the pointer is
// known to be aligned to less than its type wants. 0 leaves the type's own
// alignment in force.
func alignSuffix(align int) string {
	if align <= 0 {
		return ""
	}
	return fmt.Sprintf(", align %d", align)
}
