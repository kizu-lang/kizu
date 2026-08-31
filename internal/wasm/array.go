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
	if layout.size <= 0 {
		return "", wasmLayout{}, fmt.Errorf(
			"wasm error: array element `%s` has zero-sized storage", elem)
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
	fmt.Fprintf(&e.out, "            (memory.fill %s (i32.const 0) (i32.const %d))\n",
		slot, arrayHeaderSize)
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
	array := e.value(instr.Args[0]).expr
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (i64.load %s))\n",
		symbol, arrayFieldAddress(array, offset))
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeArrayAppend reserves storage and appends one element on success.
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
	array := e.value(instr.Args[0]).expr
	allocator := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	needed := fmt.Sprintf("(i64.add %s (i64.const 1))", length)
	ok := fmt.Sprintf("(call $__array_reserve %s %s %s (i32.const %d))",
		allocator, array, needed, layout.size)
	slot, err := e.writeArrayErrorResult(instr.Result, ok, "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (i32.wrap_i64 (i64.load %s))\n", slot)
	e.out.WriteString("              (then\n")
	destination := arrayElementAddress(array, length, layout.size)
	if err := e.writeStoreValue(destination, 0, elem, e.value(instr.Args[2])); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i64.store %s %s)))\n",
		arrayFieldAddress(array, arrayLenOffset), needed)
	return nil
}

// writeArrayAppendBytes appends one byte slice to an Array of bytes.
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
	array := e.value(instr.Args[0]).expr
	allocator := e.value(instr.Args[1]).expr
	bytes := e.value(instr.Args[2]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	byteLength32 := fmt.Sprintf("(i32.load %s)", addressAt(bytes, 4))
	byteLength := fmt.Sprintf("(i64.extend_i32_u %s)", byteLength32)
	needed := fmt.Sprintf("(i64.add %s %s)", length, byteLength)
	ok := fmt.Sprintf("(call $__array_reserve %s %s %s (i32.const 1))",
		allocator, array, needed)
	slot, err := e.writeArrayErrorResult(instr.Result, ok, "std::mem::Error", "OutOfMemory")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (i32.wrap_i64 (i64.load %s))\n", slot)
	e.out.WriteString("              (then\n")
	destination := arrayElementAddress(array, length, 1)
	fmt.Fprintf(&e.out, "                (memory.copy %s (i32.load %s) %s)\n",
		destination, bytes, byteLength32)
	fmt.Fprintf(&e.out, "                (i64.store %s %s)))\n",
		arrayFieldAddress(array, arrayLenOffset), needed)
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
	array := e.value(instr.Args[0]).expr
	allocator := e.value(instr.Args[1]).expr
	additional := e.value(instr.Args[2]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	needed := fmt.Sprintf("(i64.add %s %s)", length, additional)
	ok := fmt.Sprintf("(i32.and (i64.ge_s %s (i64.const 0)) "+
		"(call $__array_reserve %s %s %s (i32.const %d)))",
		additional, allocator, array, needed, layout.size)
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
	array := e.value(instr.Args[0]).expr
	lengthAddress := arrayFieldAddress(array, arrayLenOffset)
	length := fmt.Sprintf("(i64.load %s)", lengthAddress)
	fmt.Fprintf(&e.out, "            (if (i64.gt_s %s (i64.const 0))\n", length)
	e.out.WriteString("              (then\n")
	fmt.Fprintf(&e.out, "                (i64.store %s (i64.sub %s (i64.const 1)))\n",
		lengthAddress, length)
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	source := arrayElementAddress(array, fmt.Sprintf("(i64.load %s)", lengthAddress), layout.size)
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
	array := e.value(instr.Args[0]).expr
	lengthAddress := arrayFieldAddress(array, arrayLenOffset)
	length := fmt.Sprintf("(i64.load %s)", lengthAddress)
	fmt.Fprintf(&e.out, "            (if (i64.eqz %s)\n", length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_array_empty "+
		"(i64.const %d) (i64.const %d))))\n",
		instr.Span.Start.Line, instr.Span.Start.Column)
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.sub %s (i64.const 1)))\n",
		lengthAddress, length)
	source := arrayElementAddress(array, fmt.Sprintf("(i64.load %s)", lengthAddress), layout.size)
	return e.writeLoadValue(instr.Result, source, 0)
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
	array := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, length)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	source := arrayElementAddress(array, index, layout.size)
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
	array := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s %s)\n", index, length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_bounds %s %s "+
		"(i64.const %d) (i64.const %d))))\n",
		index, length, instr.Span.Start.Line, instr.Span.Start.Column)
	return e.writeLoadValue(instr.Result, arrayElementAddress(array, index, layout.size), 0)
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
	array := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, length)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i32.store %s %s)\n",
		addressAt(slot, payloadOffset), arrayElementAddress(array, index, layout.size))
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
	array := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayLenOffset))
	ok := fmt.Sprintf("(i64.lt_u %s %s)", index, length)
	slot, err := e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (i32.wrap_i64 (i64.load %s))\n", slot)
	e.out.WriteString("              (then\n")
	if err := e.writeStoreValue(arrayElementAddress(array, index, layout.size), 0,
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
	_, layout, err := e.arrayElementLayout(instr)
	if err != nil {
		return err
	}
	ok := fmt.Sprintf("(call $__array_swap %s %s %s (i32.const %d))",
		e.value(instr.Args[0]).expr, e.value(instr.Args[1]).expr,
		e.value(instr.Args[2]).expr, layout.size)
	_, err = e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	return err
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
	array := e.value(instr.Args[0]).expr
	want := e.value(instr.Args[1]).expr
	lengthAddress := arrayFieldAddress(array, arrayLenOffset)
	length := fmt.Sprintf("(i64.load %s)", lengthAddress)
	ok := fmt.Sprintf("(i32.and (i64.ge_s %s (i64.const 0)) (i64.le_s %s %s))",
		want, want, length)
	slot, err := e.writeArrayErrorResult(instr.Result, ok, "std::array::Error", "OutOfBounds")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (i32.wrap_i64 (i64.load %s))\n", slot)
	fmt.Fprintf(&e.out, "              (then (i64.store %s %s)))\n", lengthAddress, want)
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
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.const 0))\n",
		arrayFieldAddress(e.value(instr.Args[0]).expr, arrayLenOffset))
	return nil
}

// writeArrayAsBytes builds a byte-slice view of an Array of bytes.
func (e *emitter) writeArrayAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "[]u8" {
		return fmt.Errorf("wasm error: array.as_bytes expects Array<u8> -> []u8")
	}
	elem, ok := arrayElementWasmType(instr.Args[0].Type)
	if !ok || elem != "u8" {
		return fmt.Errorf("wasm error: array.as_bytes expects Array<u8> -> []u8")
	}
	array := e.value(instr.Args[0]).expr
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i32.store %s (i32.load %s))\n",
		slot, arrayFieldAddress(array, arrayDataOffset))
	fmt.Fprintf(&e.out, "            (i32.store %s "+
		"(i32.wrap_i64 (i64.load %s)))\n",
		addressAt(slot, 4), arrayFieldAddress(array, arrayLenOffset))
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
	array := e.value(instr.Args[0]).expr
	capacity := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(array, arrayCapacityOffset))
	bytes := fmt.Sprintf("(i32.wrap_i64 (i64.mul %s (i64.const %d)))", capacity, layout.size)
	fmt.Fprintf(&e.out, "            (call $__allocator_free %s (i32.load %s) %s)\n",
		e.value(instr.Args[1]).expr, arrayFieldAddress(array, arrayDataOffset), bytes)
	return nil
}

// writeArrayErrorResult records a runtime boolean as an E!void tag and the
// declaration-owned global error code. The tag alone selects which payload is
// observed, so the failure code can be stored on both paths without branching.
func (e *emitter) writeArrayErrorResult(
	result ir.Value,
	ok string,
	errorSet string,
	member string,
) (string, error) {
	set, exists := e.module.ErrorSets[errorSet]
	if !exists {
		return "", fmt.Errorf("wasm error: failure needs error set `%s`", errorSet)
	}
	code, exists := set.Tags[member]
	if !exists {
		return "", fmt.Errorf("wasm error: error set `%s` has no member `%s`", errorSet, member)
	}
	_, success, offset, err := e.errorPayloadOffset(result.Type)
	if err != nil {
		return "", err
	}
	if success != "void" {
		return "", fmt.Errorf("wasm error: array failure expects !void, got %s", result.Type)
	}
	slot, err := e.resultSlot(result)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.extend_i32_u %s))\n", slot, ok)
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.const %d))\n",
		addressAt(slot, offset), code)
	e.values[result.Name] = valueInfo{expr: slot}
	return slot, nil
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
