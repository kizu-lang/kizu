package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// frameLayout assigns fixed offsets to the memory-backed SSA values in one
// function invocation. A global stack pointer gives each recursive invocation
// a distinct copy of this frame.
type frameLayout struct {
	size  int
	slots map[string]int
}

// planFrame assigns fixed offsets to every memory-backed result and local
// storage cell in fn.
func (e *emitter) planFrame(fn *ir.Function) (*frameLayout, error) {
	frame := &frameLayout{slots: map[string]int{}}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if mapInstrNeedsTemp(instr.Op) {
				frame.allocate(mapTempSlotKey(instr.Result.Name),
					wasmLayout{size: mapTempSize, align: 8})
			}
			if e.isMemoryType(instr.Result.Type) && instr.Op != "phi" {
				layout, err := e.typeLayout(instr.Result.Type)
				if err != nil {
					return nil, err
				}
				frame.allocate(resultSlotKey(instr.Result.Name), layout)
			}
			if instr.Op == "local.slot" {
				layout, err := e.typeLayout(derefWasmType(instr.Result.Type))
				if err != nil {
					return nil, err
				}
				frame.allocate(localSlotKey(instr.Result.Name), layout)
			}
		}
	}
	frame.size = alignUp(frame.size, 8)
	return frame, nil
}

// registerFrameValues makes fixed-address results available before blocks are
// written. A merge block can be printed before the predecessor that defines an
// incoming value, but its runtime edge still reads the same frame address.
func (e *emitter) registerFrameValues(fn *ir.Function) error {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if e.isMemoryType(instr.Result.Type) && instr.Op != "phi" {
				slot, err := e.resultSlot(instr.Result)
				if err != nil {
					return err
				}
				e.values[instr.Result.Name] = valueInfo{expr: slot}
			}
			if instr.Op == "local.slot" {
				slot, err := e.localSlot(instr.Result)
				if err != nil {
					return err
				}
				e.values[instr.Result.Name] = valueInfo{expr: slot}
			}
		}
	}
	return nil
}

// allocate reserves an aligned frame slot unless key already has one.
func (f *frameLayout) allocate(key string, layout wasmLayout) {
	if _, exists := f.slots[key]; exists {
		return
	}
	offset := alignUp(f.size, layout.align)
	f.slots[key] = offset
	f.size = offset + maxInt(layout.size, 1)
}

// resultSlot returns the address expression for a memory-backed SSA result.
func (e *emitter) resultSlot(result ir.Value) (string, error) {
	return e.frameSlot(resultSlotKey(result.Name))
}

// localSlot returns the address expression for an addressable local cell.
func (e *emitter) localSlot(result ir.Value) (string, error) {
	return e.frameSlot(localSlotKey(result.Name))
}

// frameSlot resolves one planned slot to an expression based on this frame.
func (e *emitter) frameSlot(key string) (string, error) {
	if e.frame == nil {
		return "", fmt.Errorf("wasm error: no active function frame")
	}
	offset, ok := e.frame.slots[key]
	if !ok {
		return "", fmt.Errorf("wasm error: no frame slot for `%s`", key)
	}
	if offset == 0 {
		return "(local.get $__kizu_frame)", nil
	}
	return fmt.Sprintf("(i32.add (local.get $__kizu_frame) (i32.const %d))", offset), nil
}

// resultSlotKey separates aggregate result slots from local storage slots.
func resultSlotKey(name string) string {
	return "result:" + name
}

// localSlotKey separates local storage slots from aggregate result slots.
func localSlotKey(name string) string {
	return "local:" + name
}
