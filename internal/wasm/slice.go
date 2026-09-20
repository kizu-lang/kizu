package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeSliceInstr dispatches view operations. A `[]T` value is an addressed
// `{ i32 pointer, i32 length }` descriptor, the length counted in elements.
func (e *emitter) writeSliceInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "slice.len":
		return e.writeSliceLen(instr)
	case "slice.index":
		return e.writeSliceIndex(instr)
	case "slice.store":
		return e.writeSliceStore(instr)
	case "slice.slice":
		return e.writeSliceSlice(instr)
	case "slice.compare":
		return e.writeSliceCompare(instr)
	default:
		return fmt.Errorf("wasm error: unsupported slice instruction `%s`", instr.Op)
	}
}

// viewCell returns the byte width of one element of a view: the element's
// own layout, whatever it is, since a view counts elements and an element
// of copy data is read and written the way a container's is.
func (e *emitter) viewCell(elem string) (int, error) {
	layout, err := e.typeLayout(elem)
	if err != nil {
		return 0, err
	}
	return layout.size, nil
}

// viewElem returns the element type of a view spelling `[]T`.
func viewElem(typ string) (string, bool) {
	if strings.HasPrefix(typ, "[]") {
		return typ[2:], true
	}
	return "", false
}

// elementAddress spells the address of element `index` of a view: the
// pointer word plus the index scaled by the cell width.
func elementAddress(pointer string, index string, size int) string {
	offset := fmt.Sprintf("(i32.wrap_i64 %s)", index)
	if size != 1 {
		offset = fmt.Sprintf("(i32.mul %s (i32.const %d))", offset, size)
	}
	return fmt.Sprintf("(i32.add %s %s)", pointer, offset)
}

// writeSliceLen reads the length word from a view descriptor.
func (e *emitter) writeSliceLen(instr *ir.Instr) error {
	_, ok := viewElem(instr.Args[0].Type)
	if len(instr.Args) != 1 || !ok || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: slice.len expects []T -> i64")
	}
	expr := fmt.Sprintf("(i64.extend_i32_u %s)", e.viewLength(instr.Args[0]))
	return e.writeScalarResult(instr.Result, expr)
}

// writeSliceIndex reads one element through a view descriptor.
func (e *emitter) writeSliceIndex(instr *ir.Instr) error {
	elem, ok := viewElem(instr.Args[0].Type)
	if len(instr.Args) != 2 || !ok ||
		instr.Args[1].Type != "i64" || instr.Result.Type != elem {
		return fmt.Errorf("wasm error: slice.index expects []T, i64 -> T")
	}
	size, err := e.viewCell(elem)
	if err != nil {
		return err
	}
	index := e.value(instr.Args[1]).expr
	address := elementAddress(e.viewPointer(instr.Args[0]), index, size)
	return e.writeLoadValue(instr.Result, address, 0)
}

// writeSliceStore writes one element through a mutable view descriptor.
func (e *emitter) writeSliceStore(instr *ir.Instr) error {
	elem, ok := viewElem(instr.Args[0].Type)
	if len(instr.Args) != 3 || !ok ||
		instr.Args[1].Type != "i64" || instr.Args[2].Type != elem ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: slice.store expects []T, i64, T -> void")
	}
	size, err := e.viewCell(elem)
	if err != nil {
		return err
	}
	index := e.value(instr.Args[1]).expr
	address := elementAddress(e.viewPointer(instr.Args[0]), index, size)
	return e.writeStoreValue(address, 0, elem, e.value(instr.Args[2]))
}

// writeSliceSlice materializes a descriptor for one subview.
func (e *emitter) writeSliceSlice(instr *ir.Instr) error {
	elem, ok := viewElem(instr.Args[0].Type)
	if len(instr.Args) != 3 || !ok ||
		instr.Args[1].Type != "i64" || instr.Args[2].Type != "i64" ||
		instr.Result.Type != instr.Args[0].Type {
		return fmt.Errorf("wasm error: slice.slice expects []T, i64, i64 -> []T")
	}
	size, err := e.viewCell(elem)
	if err != nil {
		return err
	}
	start := e.value(instr.Args[1]).expr
	end := e.value(instr.Args[2]).expr
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i32.store %s %s)\n",
		slot, elementAddress(e.viewPointer(instr.Args[0]), start, size))
	fmt.Fprintf(&e.out, "            (i32.store %s (i32.wrap_i64 (i64.sub %s %s)))\n",
		addressAt(slot, 4), end, start)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeScalarResult assigns expr to one scalar SSA local.
func (e *emitter) writeScalarResult(result ir.Value, expr string) error {
	if e.isMemoryType(result.Type) {
		return fmt.Errorf("wasm error: scalar result helper got aggregate `%s`", result.Type)
	}
	symbol := symbolName(result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}
