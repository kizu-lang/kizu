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
