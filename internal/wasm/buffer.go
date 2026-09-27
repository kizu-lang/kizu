package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// bufferSize returns N and T for one fixed stack-buffer spelling `[N]T`.
func (e *emitter) bufferSize(name string) (int, string, bool) {
	parsed, err := e.types.Parse(name)
	if err != nil {
		return 0, "", false
	}
	buffer, ok := parsed.(*typ.Buffer)
	if !ok || buffer.Size < 0 || int64(int(buffer.Size)) != buffer.Size {
		return 0, "", false
	}
	return int(buffer.Size), typ.Text(buffer.Elem), true
}

// writeBufferInstr lowers fixed-length zeroed storage and its byte view.
func (e *emitter) writeBufferInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "buffer.new":
		return e.writeBufferNew(instr)
	case "buffer.as_bytes":
		return e.writeBufferAsBytes(instr)
	case "buffer.addr":
		return e.writeBufferAddr(instr)
	default:
		return fmt.Errorf("wasm error: unsupported buffer instruction `%s`", instr.Op)
	}
}

// writeBufferNew zeros the storage of one fixed-length array.
func (e *emitter) writeBufferNew(instr *ir.Instr) error {
	if _, _, ok := e.bufferSize(instr.Result.Type); !ok || len(instr.Args) != 0 {
		return fmt.Errorf("wasm error: buffer.new expects `[N]T` result, got %s",
			instr.Result.Type)
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	layout, err := e.typeLayout(instr.Result.Type)
	if err != nil {
		return err
	}
	e.writeMemoryZero(slot, layout.size)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeBufferAsBytes builds a slice descriptor over an array's storage.
func (e *emitter) writeBufferAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: buffer.as_bytes expects the storage of `[N]T`")
	}
	size, elem, ok := e.bufferSize(derefWasmType(instr.Args[0].Type))
	if !ok {
		return fmt.Errorf("wasm error: buffer.as_bytes expects the storage of `[N]T`, got %s",
			instr.Args[0].Type)
	}
	if instr.Result.Type != "[]"+elem {
		return fmt.Errorf("wasm error: buffer.as_bytes expects `&var [N]T` -> `[]T`")
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i32.store %s %s)\n", slot, e.value(instr.Args[0]).expr)
	fmt.Fprintf(&e.out, "            (i32.store %s (i32.const %d))\n",
		addressAt(slot, 4), size)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeBufferAddr projects the address of one element out of an array's
// storage. The index was bounds-checked when it was lowered.
func (e *emitter) writeBufferAddr(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: buffer.addr expects the storage of `[N]T` and an index")
	}
	_, elem, ok := e.bufferSize(derefWasmType(instr.Args[0].Type))
	if !ok || derefWasmType(instr.Result.Type) != elem {
		return fmt.Errorf("wasm error: buffer.addr expects `&var [N]T` -> `&var T`, got %s -> %s",
			instr.Args[0].Type, instr.Result.Type)
	}
	cell, err := e.typeLayout(elem)
	if err != nil {
		return err
	}
	symbol := symbolName(instr.Result.Name)
	offset := fmt.Sprintf("(i32.mul (i32.wrap_i64 %s) (i32.const %d))",
		e.value(instr.Args[1]).expr, cell.size)
	fmt.Fprintf(&e.out, "            (local.set %s (i32.add %s %s))\n",
		symbol, e.value(instr.Args[0]).expr, offset)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}
