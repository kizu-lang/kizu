package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// bufferSize returns N for one fixed stack-buffer spelling `[N]u8`.
func (e *emitter) bufferSize(name string) (int, bool) {
	parsed, err := e.types.Parse(name)
	if err != nil {
		return 0, false
	}
	buffer, ok := parsed.(*typ.Buffer)
	if !ok || buffer.Size < 0 || int64(int(buffer.Size)) != buffer.Size {
		return 0, false
	}
	return int(buffer.Size), true
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

// writeBufferNew zeros one fixed-size stack buffer.
func (e *emitter) writeBufferNew(instr *ir.Instr) error {
	size, ok := e.bufferSize(instr.Result.Type)
	if !ok || len(instr.Args) != 0 {
		return fmt.Errorf("wasm error: buffer.new expects `[N]u8` result, got %s",
			instr.Result.Type)
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	if size > 0 {
		fmt.Fprintf(&e.out, "            (memory.fill %s (i32.const 0) (i32.const %d))\n",
			slot, size)
	}
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeBufferAsBytes builds a slice descriptor over stack-buffer storage.
func (e *emitter) writeBufferAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "[]u8" {
		return fmt.Errorf("wasm error: buffer.as_bytes expects buffer -> []u8")
	}
	size, ok := e.bufferSize(instr.Args[0].Type)
	if !ok {
		return fmt.Errorf("wasm error: buffer.as_bytes expects `[N]u8`, got %s",
			instr.Args[0].Type)
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
