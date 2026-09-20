package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// An Array value is its inline `{data, len, cap}` header. The pointer is one
// wasm32 word followed by two i64 words at their natural offsets.
const (
	arrayHeaderSize     = 24
	arrayDataOffset     = 0
	arrayLenOffset      = 8
	arrayCapacityOffset = 16
)

// isArrayWasmType reports whether name is a direct Array storage type.
func isArrayWasmType(name string) bool {
	return strings.HasPrefix(name, "std::array::Array<") && strings.HasSuffix(name, ">")
}

// arrayElementWasmType returns T through either direct or borrowed Array<T>.
func arrayElementWasmType(name string) (string, bool) {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "&var "), "&")
	if !isArrayWasmType(name) {
		return "", false
	}
	return name[len("std::array::Array<") : len(name)-1], true
}

// usesArrayRuntime reports whether this module needs the allocation and swap
// helpers shared by array operations.
func (e *emitter) usesArrayRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "array.") {
					return true
				}
			}
		}
	}
	return false
}

// writeArrayInstr dispatches Array operations to their wasm32 lowerings.
func (e *emitter) writeArrayInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "array.new":
		return e.writeArrayNew(instr)
	case "array.append":
		return e.writeArrayAppend(instr)
	case "array.append_bytes":
		return e.writeArrayAppendBytes(instr)
	case "array.len":
		return e.writeArrayField(instr, arrayLenOffset, "array.len")
	case "array.capacity":
		return e.writeArrayField(instr, arrayCapacityOffset, "array.capacity")
	case "array.reserve":
		return e.writeArrayReserve(instr)
	case "array.pop":
		return e.writeArrayPop(instr)
	case "array.pop_or_panic":
		return e.writeArrayPopOrPanic(instr)
	case "array.get":
		return e.writeArrayGet(instr)
	case "array.get_or_panic":
		return e.writeArrayGetOrPanic(instr)
	default:
		return e.writeArrayMutationInstr(instr)
	}
}

// writeArrayMutationInstr dispatches borrow and mutation operations.
func (e *emitter) writeArrayMutationInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "array.at", "array.at_mut":
		return e.writeArrayAt(instr)
	case "array.set":
		return e.writeArraySet(instr)
	case "array.swap":
		return e.writeArraySwap(instr)
	case "array.truncate":
		return e.writeArrayTruncate(instr)
	case "array.clear":
		return e.writeArrayClear(instr)
	case "array.as_bytes":
		return e.writeArrayAsBytes(instr)
	case "array.deinit":
		return e.writeArrayDeinit(instr)
	default:
		return fmt.Errorf("wasm error: unsupported array instruction `%s`", instr.Op)
	}
}

// arrayElementLayout resolves and measures the element of an Array operation.
func (e *emitter) arrayElementLayout(instr *ir.Instr) (string, wasmLayout, error) {
	var container string
	if instr.Op == "array.new" {
		container = instr.Result.Type
	} else if len(instr.Args) > 0 {
		container = instr.Args[0].Type
	}
	elem, ok := arrayElementWasmType(container)
	if !ok {
		return "", wasmLayout{}, fmt.Errorf(
			"wasm error: `%s` was handed no Array<T>", instr.Op)
	}
	layout, err := e.typeLayout(elem)
	if err != nil {
		return "", wasmLayout{}, err
	}
	return elem, layout, nil
}

// arrayFieldAddress returns one field address in an inline Array header.
func arrayFieldAddress(array string, offset int) string {
	return addressAt(array, offset)
}

// arrayElementAddress returns one checked index's backing-storage address.
func arrayElementAddress(array string, index string, size int) string {
	return fmt.Sprintf("(i32.add (i32.load %s) "+
		"(i32.wrap_i64 (i64.mul %s (i64.const %d))))",
		arrayFieldAddress(array, arrayDataOffset), index, size)
}

// arrayHeader is how an operation reaches the `{data, len, cap}` header of the
// Array it works on: at an address in linear memory, with the storage pointer
// and length read from locals instead when the loop being written hoisted
// those reads (planLoopHoists).
type arrayHeader struct {
	address string
	hoist   *loopHoist
}

// arrayHeaderOf returns how the Array value reaches its header.
func (e *emitter) arrayHeaderOf(value ir.Value) arrayHeader {
	h := arrayHeader{address: e.value(value).expr}
	if hoist, ok := e.hoisted[value.Name]; ok {
		h.hoist = &hoist
	}
	return h
}

// data reads the header's storage pointer.
func (h arrayHeader) data() string {
	if h.hoist != nil {
		return "(local.get " + h.hoist.data + ")"
	}
	return "(i32.load " + arrayFieldAddress(h.address, arrayDataOffset) + ")"
}

// length reads the header's element count.
func (h arrayHeader) length() string {
	if h.hoist != nil {
		return "(local.get " + h.hoist.length + ")"
	}
	return "(i64.load " + arrayFieldAddress(h.address, arrayLenOffset) + ")"
}

// capacity reads the header's reserved element count.
func (h arrayHeader) capacity() string {
	return "(i64.load " + arrayFieldAddress(h.address, arrayCapacityOffset) + ")"
}

// element returns the storage address of the element at a checked index.
func (h arrayHeader) element(index string, size int) string {
	return fmt.Sprintf("(i32.add %s (i32.wrap_i64 (i64.mul %s (i64.const %d))))",
		h.data(), index, size)
}

// writeArrayLength sets the header's element count. A loop that writes it
// hoists no read of it, so the count is always stored where it lives.
func (e *emitter) writeArrayLength(h arrayHeader, value string) {
	fmt.Fprintf(&e.out, "                (i64.store %s %s)\n",
		arrayFieldAddress(h.address, arrayLenOffset), value)
}

// writeArrayNew initializes an empty inline Array header.
func (e *emitter) writeArrayNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "Allocator" ||
		!isArrayWasmType(instr.Result.Type) {
		return fmt.Errorf("wasm error: array.new expects allocator -> Array<T>")
	}
	if _, _, err := e.arrayElementLayout(instr); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	e.writeMemoryZero(slot, arrayHeaderSize)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeArrayField loads an Array length or capacity field.
func (e *emitter) writeArrayField(instr *ir.Instr, offset int, op string) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: %s expects Array<T> -> i64", op)
	}
	if _, ok := arrayElementWasmType(instr.Args[0].Type); !ok {
		return fmt.Errorf("wasm error: %s expects Array<T> -> i64", op)
	}
	h := e.arrayHeaderOf(instr.Args[0])
	field := h.length()
	if offset == arrayCapacityOffset {
		field = h.capacity()
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, field)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeArrayAppend appends one element: into the reserved tail when there is
// room, the way the native backend does, and through the reserve helper --
// a call and the allocator's dispatch -- only when the storage must grow.
func (e *emitter) writeArrayAppend(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"wasm error: array.append expects Array<T>, Allocator, T -> std::mem::Error!void")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Args[2].Type != elem {
		return fmt.Errorf("wasm error: array.append expects %s, got %s", elem, instr.Args[2].Type)
	}
	h := e.arrayHeaderOf(instr.Args[0])
	allocator := e.value(instr.Args[1]).expr
	length := h.length()
	needed := fmt.Sprintf("(i64.add %s (i64.const 1))", length)
	reserve := fmt.Sprintf("(call $__array_reserve %s %s %s (i32.const %d))",
		allocator, h.address, needed, layout.size)
	write := func() error {
		destination := h.element(length, layout.size)
		if err := e.writeStoreValue(destination, 0, elem, e.value(instr.Args[2])); err != nil {
			return err
		}
		e.writeArrayLength(h, needed)
		return nil
	}
	return e.writeArrayAppendPaths(instr.Result, h, reserve, write)
}

// writeArrayAppendPaths writes the two ways an append ends: when the length
// is below the capacity, `write` runs and the result is success; otherwise
// `reserve` is called and `write` runs on its success. Both paths write the
// error result the same way, so the result's slot reads the same after
// either.
func (e *emitter) writeArrayAppendPaths(
	result ir.Value,
	h arrayHeader,
	reserve string,
	write func() error,
) error {
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", h.length(), h.capacity())
	e.out.WriteString("              (then\n")
	_, err := e.writeArrayErrorResult(result, "(i32.const 1)", "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	grown, err := e.writeArrayErrorResult(result, reserve, "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if %s\n", grown)
	e.out.WriteString("              (then\n")
	if err := write(); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	e.out.WriteString("              )\n")
	e.out.WriteString("            )\n")
	return nil
}

// writeArrayAppendBytes appends one byte slice to an Array of bytes, into
// the reserved tail when the run fits.
func (e *emitter) writeArrayAppendBytes(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "Allocator" ||
		instr.Args[2].Type != "[]u8" || instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf("wasm error: array.append_bytes expects" +
			" Array<u8>, Allocator, []u8 -> std::mem::Error!void")
	}
	elem, _, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	if elem != "u8" {
		return fmt.Errorf("wasm error: array.append_bytes expects Array<u8>")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	allocator := e.value(instr.Args[1]).expr
	bytes := e.value(instr.Args[2]).expr
	length := h.length()
	byteLength32 := fmt.Sprintf("(i32.load %s)", addressAt(bytes, 4))
	byteLength := fmt.Sprintf("(i64.extend_i32_u %s)", byteLength32)
	needed := fmt.Sprintf("(i64.add %s %s)", length, byteLength)
	reserve := fmt.Sprintf("(call $__array_reserve %s %s %s (i32.const 1))",
		allocator, h.address, needed)
	write := func() error {
		destination := h.element(length, 1)
		fmt.Fprintf(&e.out, "                (memory.copy %s (i32.load %s) %s)\n",
			destination, bytes, byteLength32)
		e.writeArrayLength(h, needed)
		return nil
	}
	// The tail fits when what is needed is within the capacity: the run may
	// be a view of this same array, below its length, so the copy never
	// overlaps what it writes.
	fmt.Fprintf(&e.out, "            (if (i64.le_u %s %s)\n", needed, h.capacity())
	e.out.WriteString("              (then\n")
	_, err = e.writeArrayErrorResult(instr.Result, "(i32.const 1)", "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	grown, err := e.writeArrayErrorResult(instr.Result, reserve, "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if %s\n", grown)
	e.out.WriteString("              (then\n")
	if err := write(); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	e.out.WriteString("              )\n")
	e.out.WriteString("            )\n")
	return nil
}

// writeArrayReserve grows capacity without changing length.
func (e *emitter) writeArrayReserve(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "Allocator" ||
		instr.Args[2].Type != "i64" || instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"wasm error: array.reserve expects Array<T>, Allocator, i64 -> std::mem::Error!void")
	}
	_, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	h := e.arrayHeaderOf(instr.Args[0])
	allocator := e.value(instr.Args[1]).expr
	additional := e.value(instr.Args[2]).expr
	needed := fmt.Sprintf("(i64.add %s %s)", h.length(), additional)
	ok := fmt.Sprintf("(i32.and (i64.ge_s %s (i64.const 0)) "+
		"(call $__array_reserve %s %s %s (i32.const %d)))",
		additional, allocator, h.address, needed, layout.size)
	_, err = e.writeArrayErrorResult(instr.Result, ok, "std::mem::Error", "OutOfMemory")
	return err
}

// writeArrayPop removes the last element into an optional result.
func (e *emitter) writeArrayPop(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: array.pop expects Array<T> -> ?T")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != elem {
		return fmt.Errorf("wasm error: array.pop expects Array<T> -> ?T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	length := h.length()
	fmt.Fprintf(&e.out, "            (if (i64.gt_s %s (i64.const 0))\n", length)
	e.out.WriteString("              (then\n")
	e.writeArrayLength(h, fmt.Sprintf("(i64.sub %s (i64.const 1))", length))
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	source := h.element(h.length(), layout.size)
	if err := e.writeArrayCopyValue(addressAt(slot, payloadOffset), source, elem); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeArrayPopOrPanic removes the last element or reports an empty Array.
func (e *emitter) writeArrayPopOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: array.pop_or_panic expects Array<T> -> T")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("wasm error: array.pop_or_panic expects Array<T> -> T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	length := h.length()
	fmt.Fprintf(&e.out, "            (if (i64.eqz %s)\n", length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_array_empty "+
		"(i64.const %d) (i64.const %d)) (unreachable)))\n",
		instr.Span.Start.Line, instr.Span.Start.Column)
	e.writeArrayLength(h, fmt.Sprintf("(i64.sub %s (i64.const 1))", length))
	return e.writeLoadValue(instr.Result, h.element(h.length(), layout.size), 0)
}

// writeArrayGet copies an in-bounds element into an optional result.
func (e *emitter) writeArrayGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: array.get expects Array<T>, i64 -> ?T")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != elem {
		return fmt.Errorf("wasm error: array.get expects Array<T>, i64 -> ?T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	index := e.value(instr.Args[1]).expr
	if e.optionLocals[instr.Result.Name] {
		load, err := e.loadExpr(h.element(index, layout.size), 0, elem)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, h.length())
		e.out.WriteString("              (then\n")
		e.writeOptionLocals(instr.Result, "(i32.const 1)", load)
		e.out.WriteString("              )\n")
		e.out.WriteString("              (else\n")
		e.writeOptionLocals(instr.Result, "(i32.const 0)", "")
		e.out.WriteString("              ))\n")
		return nil
	}
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, h.length())
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	source := h.element(index, layout.size)
	if err := e.writeArrayCopyValue(addressAt(slot, payloadOffset), source, elem); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeArrayGetOrPanic loads an element or reports its failed bounds check.
func (e *emitter) writeArrayGetOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: array.get_or_panic expects Array<T>, i64 -> T")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("wasm error: array.get_or_panic expects Array<T>, i64 -> T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	index := e.value(instr.Args[1]).expr
	length := h.length()
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s %s)\n", index, length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_bounds %s %s "+
		"(i64.const %d) (i64.const %d)) (unreachable)))\n",
		index, length, instr.Span.Start.Line, instr.Span.Start.Column)
	return e.writeLoadValue(instr.Result, h.element(index, layout.size), 0)
}

// writeArrayAt returns an optional borrow into Array storage.
func (e *emitter) writeArrayAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: array.at expects Array<T>, i64 -> ?&T")
	}
	_, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	_, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil {
		return fmt.Errorf("wasm error: array.at expects Array<T>, i64 -> ?&T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	index := e.value(instr.Args[1]).expr
	if e.optionLocals[instr.Result.Name] {
		// The address is only an add: working it out for an index past the
		// end reads nothing, so presence and payload are set without a branch.
		present := fmt.Sprintf("(i64.lt_u %s %s)", index, h.length())
		e.writeOptionLocals(instr.Result, present, h.element(index, layout.size))
		return nil
	}
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, h.length())
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i32.store %s %s)\n",
		addressAt(slot, payloadOffset), h.element(index, layout.size))
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeArraySet replaces one in-bounds copy element.
func (e *emitter) writeArraySet(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "i64" ||
		instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf(
			"wasm error: array.set expects Array<T>, i64, T -> std::array::Error!void")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Args[2].Type != elem {
		return fmt.Errorf("wasm error: array.set expects %s, got %s", elem, instr.Args[2].Type)
	}
	h := e.arrayHeaderOf(instr.Args[0])
	index := e.value(instr.Args[1]).expr
	ok := fmt.Sprintf("(i64.lt_u %s %s)", index, h.length())
	inBounds, err := e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if %s\n", inBounds)
	e.out.WriteString("              (then\n")
	if err := e.writeStoreValue(h.element(index, layout.size), 0,
		elem, e.value(instr.Args[2])); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeArraySwap exchanges two in-bounds elements.
func (e *emitter) writeArraySwap(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "i64" ||
		instr.Args[2].Type != "i64" || instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf(
			"wasm error: array.swap expects Array<T>, i64, i64 -> std::array::Error!void")
	}
	elem, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	h := e.arrayHeaderOf(instr.Args[0])
	left := e.value(instr.Args[1]).expr
	right := e.value(instr.Args[2]).expr
	if e.isMemoryType(elem) || (h.hoist == nil && !isPlainAddress(h.address)) {
		ok := fmt.Sprintf("(call $__array_swap %s %s %s (i32.const %d))",
			h.address, left, right, layout.size)
		_, err = e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
		return err
	}
	// A scalar element is moved as the one word it is, through the swap
	// local its type declares, instead of byte by byte in a helper.
	length := h.length()
	ok := fmt.Sprintf("(i32.and (i64.lt_u %s %s) (i64.lt_u %s %s))", left, length, right, length)
	inBounds, err := e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	if err != nil {
		return err
	}
	load, err := e.loadOp(elem)
	if err != nil {
		return err
	}
	store, err := e.storeOp(elem)
	if err != nil {
		return err
	}
	leftAddress := h.element(left, layout.size)
	rightAddress := h.element(right, layout.size)
	temp := swapLocal(e.wasmType(elem))
	fmt.Fprintf(&e.out, "            (if %s\n", inBounds)
	e.out.WriteString("              (then\n")
	fmt.Fprintf(&e.out, "            (local.set %s (%s %s))\n", temp, load, leftAddress)
	fmt.Fprintf(&e.out, "            (%s %s (%s %s))\n", store, leftAddress, load, rightAddress)
	fmt.Fprintf(&e.out, "            (%s %s (local.get %s))\n", store, rightAddress, temp)
	e.out.WriteString("              ))\n")
	return nil
}

// swapLocal names the local an inline swap of one wasm value type holds the
// left element in.
func swapLocal(wasmType string) string {
	return "$__kizu_swap_" + wasmType
}

// swapLocalTypes lists, in first-use order, the wasm value types fn's inline
// swaps hold an element in, so each is declared once.
func (e *emitter) swapLocalTypes(fn *ir.Function) []string {
	types := []string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op != "array.swap" || len(instr.Args) != 3 {
				continue
			}
			elem, ok := arrayElementWasmType(instr.Args[0].Type)
			if !ok || e.isMemoryType(elem) {
				continue
			}
			wasmType := e.wasmType(elem)
			seen := false
			for _, existing := range types {
				seen = seen || existing == wasmType
			}
			if !seen {
				types = append(types, wasmType)
			}
		}
	}
	return types
}

// writeArrayTruncate shortens an Array to a validated length.
func (e *emitter) writeArrayTruncate(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" ||
		instr.Result.Type != "std::array::Error!void" {
		return fmt.Errorf(
			"wasm error: array.truncate expects Array<T>, i64 -> std::array::Error!void")
	}
	if _, _, err := e.arrayElementLayout(instr); err != nil {
		return err
	}
	h := e.arrayHeaderOf(instr.Args[0])
	want := e.value(instr.Args[1]).expr
	ok := fmt.Sprintf("(i32.and (i64.ge_s %s (i64.const 0)) (i64.le_s %s %s))",
		want, want, h.length())
	inBounds, err := e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if %s\n", inBounds)
	e.out.WriteString("              (then\n")
	e.writeArrayLength(h, want)
	e.out.WriteString("              ))\n")
	return nil
}

// writeArrayClear resets an Array length while retaining capacity.
func (e *emitter) writeArrayClear(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: array.clear expects Array<T> -> void")
	}
	if _, ok := arrayElementWasmType(instr.Args[0].Type); !ok {
		return fmt.Errorf("wasm error: array.clear expects Array<T> -> void")
	}
	e.writeArrayLength(e.arrayHeaderOf(instr.Args[0]), "(i64.const 0)")
	return nil
}

// writeArrayAsBytes builds a view of an Array: the same {ptr, len}
// descriptor as every view, with the length in elements.
func (e *emitter) writeArrayAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !strings.HasPrefix(instr.Result.Type, "[]") {
		return fmt.Errorf("wasm error: array.as_bytes expects Array<T> -> []T")
	}
	if _, ok := arrayElementWasmType(instr.Args[0].Type); !ok {
		return fmt.Errorf("wasm error: array.as_bytes expects Array<T> -> []T")
	}
	h := e.arrayHeaderOf(instr.Args[0])
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i32.store %s %s)\n", slot, h.data())
	fmt.Fprintf(&e.out, "            (i32.store %s (i32.wrap_i64 %s))\n",
		addressAt(slot, 4), h.length())
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeArrayDeinit releases an Array's backing allocation.
func (e *emitter) writeArrayDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: array.deinit expects Array<T>, Allocator -> void")
	}
	_, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	h := e.arrayHeaderOf(instr.Args[0])
	bytes := fmt.Sprintf("(i32.wrap_i64 (i64.mul %s (i64.const %d)))", h.capacity(), layout.size)
	fmt.Fprintf(&e.out, "            (call $__allocator_free %s %s %s)\n",
		e.value(instr.Args[1]).expr, h.data(), bytes)
	return nil
}

// writeArrayErrorResult records a runtime boolean as an E!void tag and the
// declaration-owned global error code, and returns the expression that reads
// the boolean back. The tag alone selects which payload is observed, so the
// failure code can be stored on both paths without branching. A result only
// error.try, error.has and error.code read is not stored at all: its boolean
// is its local, and its code the constant (flagResults).
func (e *emitter) writeArrayErrorResult(
	result ir.Value,
	ok string,
	errorSet string,
	member string,
) (string, error) {
	code, err := e.wasmErrorCode(errorSet, member)
	if err != nil {
		return "", err
	}
	_, success, offset, err := e.errorPayloadOffset(result.Type)
	if err != nil {
		return "", err
	}
	if success != "void" {
		return "", fmt.Errorf("wasm error: array failure expects !void, got %s", result.Type)
	}
	if e.flagResults[result.Name] {
		symbol := symbolName(result.Name)
		fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, ok)
		e.values[result.Name] = valueInfo{flag: symbol, code: code}
		return "(local.get " + symbol + ")", nil
	}
	slot, err := e.resultSlot(result)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.extend_i32_u %s))\n", slot, ok)
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.const %d))\n",
		addressAt(slot, offset), code)
	e.values[result.Name] = valueInfo{expr: slot}
	return "(i32.wrap_i64 (i64.load " + slot + "))", nil
}

// writesFlagResult reports whether op writes its E!void result through
// writeArrayErrorResult, whose failure code is a constant of the operation.
func writesFlagResult(op string) bool {
	switch op {
	case "array.append", "array.append_bytes", "array.reserve", "array.set",
		"array.swap", "array.truncate", "map.insert":
		return true
	default:
		return false
	}
}

// flagResultsOf names the results of fn a flag local can hold: an operation
// that writes its E!void result through writeArrayErrorResult, read only by
// the instructions that ask for its tag or its code. Such a result is
// never handed on, so nothing needs it at an address, and a success that is
// tested where it was produced does not go through memory to be tested.
func flagResultsOf(fn *ir.Function) map[string]bool {
	flags := map[string]bool{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if writesFlagResult(instr.Op) && strings.HasSuffix(instr.Result.Type, "!void") {
				flags[instr.Result.Name] = true
			}
		}
	}
	if len(flags) == 0 {
		return flags
	}
	forEachRead(fn, func(instr *ir.Instr, index int, value ir.Value) {
		readsTag := instr != nil && index == 0 && (instr.Op == "error.try" ||
			instr.Op == "error.has" || instr.Op == "error.code")
		if !readsTag {
			delete(flags, value.Name)
		}
	})
	return flags
}

// writeArrayCopyValue copies one value from an element address into an
// already-addressed optional payload.
func (e *emitter) writeArrayCopyValue(destination string, source string, typ string) error {
	if e.isMemoryType(typ) {
		layout, err := e.typeLayout(typ)
		if err != nil {
			return err
		}
		e.writeMemoryCopy(destination, source, layout.size)
		return nil
	}
	loaded, err := e.loadExpr(source, 0, typ)
	if err != nil {
		return err
	}
	return e.writeStoreValue(destination, 0, typ, valueInfo{expr: loaded})
}
