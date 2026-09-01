package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeSliceInstr dispatches byte-view operations. A []u8 value is an
// addressed `{ i32 pointer, i32 length }` descriptor.
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
	default:
		return fmt.Errorf("wasm error: unsupported slice instruction `%s`", instr.Op)
	}
}

// writeSliceLen reads the length word from a byte-view descriptor.
func (e *emitter) writeSliceLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: slice.len expects []u8 -> i64")
	}
	descriptor := e.value(instr.Args[0]).expr
	expr := fmt.Sprintf("(i64.extend_i32_u (i32.load %s))", addressAt(descriptor, 4))
	return e.writeScalarResult(instr.Result, expr)
}

// writeSliceIndex reads one byte through a byte-view descriptor.
func (e *emitter) writeSliceIndex(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" || instr.Result.Type != "u8" {
		return fmt.Errorf("wasm error: slice.index expects []u8, i64 -> u8")
	}
	descriptor := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	address := fmt.Sprintf("(i32.add (i32.load %s) (i32.wrap_i64 %s))", descriptor, index)
	return e.writeScalarResult(instr.Result, "(i64.load8_u "+address+")")
}

// writeSliceStore writes one byte through a mutable byte-view descriptor.
func (e *emitter) writeSliceStore(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" || instr.Args[2].Type != "u8" ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: slice.store expects []u8, i64, u8 -> void")
	}
	descriptor := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	value := e.value(instr.Args[2]).expr
	address := fmt.Sprintf("(i32.add (i32.load %s) (i32.wrap_i64 %s))", descriptor, index)
	fmt.Fprintf(&e.out, "            (i64.store8 %s %s)\n", address, value)
	return nil
}

// writeSliceSlice materializes a descriptor for one byte subview.
func (e *emitter) writeSliceSlice(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" || instr.Args[2].Type != "i64" ||
		instr.Result.Type != "[]u8" {
		return fmt.Errorf("wasm error: slice.slice expects []u8, i64, i64 -> []u8")
	}
	descriptor := e.value(instr.Args[0]).expr
	start := e.value(instr.Args[1]).expr
	end := e.value(instr.Args[2]).expr
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i32.store %s (i32.add (i32.load %s) (i32.wrap_i64 %s)))\n",
		slot, descriptor, start)
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
