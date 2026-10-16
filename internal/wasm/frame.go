package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// frameLayout assigns fixed offsets to the memory-backed SSA values in one
// function invocation. A global stack pointer gives each recursive invocation
// a distinct copy of this frame.
type frameLayout struct {
	size  int
	slots map[string]int
}

// planFrame assigns fixed offsets to every memory-backed result, stack-buffer
// storage, and local storage cell in fn.
func (e *emitter) planFrame(fn *ir.Function) (*frameLayout, error) {
	frame := &frameLayout{slots: map[string]int{}}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if mapInstrNeedsTemp(instr.Op) {
				frame.allocate(mapTempSlotKey(instr.Result.Name),
					wasmLayout{size: mapTempSize, align: 8})
			}
			if err := e.planCallCopySlots(frame, instr); err != nil {
				return nil, err
			}
			if e.isMemoryType(instr.Result.Type) &&
				instr.Op != "phi" && !e.flagResults[instr.Result.Name] &&
				!e.optionLocals[instr.Result.Name] {
				layout, err := e.typeLayout(instr.Result.Type)
				if err != nil {
					return nil, err
				}
				frame.allocate(resultSlotKey(instr.Result.Name), layout)
			}
			if instr.Op == "local.slot" {
				layout, err := e.valueSlotLayout(derefWasmType(instr.Result.Type))
				if err != nil {
					return nil, err
				}
				frame.allocate(localSlotKey(instr.Result.Name), layout)
			}
		}
	}
	if err := e.planPhiCells(frame, fn); err != nil {
		return nil, err
	}
	frame.size = alignUp(frame.size, 8)
	return frame, nil
}

// phiCell is the storage of one memory-backed phi: the slot that holds its
// value and, when another phi of its block reads it, the slot a cycle of
// copies parks its old value in.
type phiCell struct {
	slot string
	park string
	size int
}

// hasPhiCell reports whether the phi result is memory-backed. Every other
// memory-backed value owns the result slot its instruction writes, but that
// instruction writes the same slot again on the next trip around a loop, so a
// phi that only pointed at its incoming value would see the value change
// under it while it is still live -- two phis fed the same value, or one fed
// a value its own loop rebuilds. The phi copies the bytes into a slot of its
// own instead.
func (e *emitter) hasPhiCell(result ir.Value) bool {
	return e.isMemoryType(result.Type) && !e.flagResults[result.Name] &&
		!e.optionLocals[result.Name]
}

// planPhiCells reserves each memory-backed phi's slot, and a park slot for the
// ones another phi of the same block reads.
func (e *emitter) planPhiCells(frame *frameLayout, fn *ir.Function) error {
	for _, block := range fn.Blocks {
		read := phisReadByPhis(block)
		for _, instr := range block.Instrs {
			if instr.Op != "phi" || !e.hasPhiCell(instr.Result) {
				continue
			}
			layout, err := e.typeLayout(instr.Result.Type)
			if err != nil {
				return err
			}
			frame.allocate(phiSlotKey(instr.Result.Name), layout)
			if read[instr.Result.Name] {
				frame.allocate(phiParkSlotKey(instr.Result.Name), layout)
			}
		}
	}
	return nil
}

// phisReadByPhis names the phis of block that another phi of block takes as
// an incoming value.
func phisReadByPhis(block *ir.Block) map[string]bool {
	phis := map[string]bool{}
	for _, instr := range block.Instrs {
		if instr.Op == "phi" {
			phis[instr.Result.Name] = true
		}
	}
	read := map[string]bool{}
	for _, instr := range block.Instrs {
		if instr.Op != "phi" {
			continue
		}
		for _, incoming := range instr.Incoming {
			if phis[incoming.Value.Name] && incoming.Value.Name != instr.Result.Name {
				read[incoming.Value.Name] = true
			}
		}
	}
	return read
}

// registerPhiCells resolves the planned phi slots of fn to addresses.
func (e *emitter) registerPhiCells(fn *ir.Function) error {
	e.phiStorage = map[string]phiCell{}
	for _, block := range fn.Blocks {
		read := phisReadByPhis(block)
		for _, instr := range block.Instrs {
			if instr.Op != "phi" || !e.hasPhiCell(instr.Result) {
				continue
			}
			layout, err := e.typeLayout(instr.Result.Type)
			if err != nil {
				return err
			}
			cell := phiCell{size: layout.size}
			if cell.slot, err = e.frameSlot(phiSlotKey(instr.Result.Name)); err != nil {
				return err
			}
			if read[instr.Result.Name] {
				if cell.park, err = e.frameSlot(phiParkSlotKey(instr.Result.Name)); err != nil {
					return err
				}
			}
			e.phiStorage[instr.Result.Name] = cell
		}
	}
	return nil
}

// phiSlotKey separates phi storage from the other frame slots.
func phiSlotKey(name string) string {
	return "phi:" + name
}

// phiParkSlotKey names the slot a cycle of phi copies parks a phi's value in.
func phiParkSlotKey(name string) string {
	return "phi-park:" + name
}

// registerFrameValues makes fixed-address results available before blocks are
// written. A merge block can be printed before the predecessor that defines an
// incoming value, but its runtime edge still reads the same frame address.
func (e *emitter) registerFrameValues(fn *ir.Function) error {
	if err := e.registerPhiCells(fn); err != nil {
		return err
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if e.isMemoryType(instr.Result.Type) &&
				instr.Op != "phi" && !e.flagResults[instr.Result.Name] &&
				!e.optionLocals[instr.Result.Name] {
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

// valueSlotLayout returns storage for one wasm value placed in an addressable
// cell.
func (e *emitter) valueSlotLayout(typ string) (wasmLayout, error) {
	return e.typeLayout(typ)
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

// callArgsAndParams returns the value arguments and ABI parameters of a
// module-local direct or indirect call. Unknown external calls have no ABI
// metadata to adapt.
func (e *emitter) callArgsAndParams(instr *ir.Instr) ([]ir.Value, []ir.Param) {
	if instr.Op == "call.indirect" {
		if len(instr.Args) == 0 {
			return nil, instr.CallParams
		}
		return instr.Args[1:], instr.CallParams
	}
	name, ok := strings.CutPrefix(instr.Op, "call.")
	if !ok {
		return nil, nil
	}
	params, ok := e.paramsByFunction[name]
	if !ok {
		return nil, nil
	}
	return instr.Args, params
}

// callNeedsCopySlot reports whether wasm must materialize an addressed copy.
// Memory-backed values already are addressed copies; scalar owners such as a
// Box handle need a fixed cell before filling a PassCopyAddress parameter.
func (e *emitter) callNeedsCopySlot(param ir.Param, arg ir.Value) bool {
	return param.TakesAddressOf(arg.Type) && !e.isMemoryType(arg.Type)
}

// planCallCopySlots reserves recursive-safe cells for addressed scalar call
// arguments before the function body is emitted.
func (e *emitter) planCallCopySlots(frame *frameLayout, instr *ir.Instr) error {
	args, params := e.callArgsAndParams(instr)
	for index, arg := range args {
		if index >= len(params) || !e.callNeedsCopySlot(params[index], arg) {
			continue
		}
		layout, err := e.valueSlotLayout(arg.Type)
		if err != nil {
			return err
		}
		frame.allocate(callCopySlotKey(instr.Result.Name, index), layout)
	}
	return nil
}

// callCopySlot returns one planned addressed-copy cell for a call argument.
func (e *emitter) callCopySlot(result ir.Value, index int) (string, error) {
	return e.frameSlot(callCopySlotKey(result.Name, index))
}

// callCopySlotKey separates call copies from result and local storage slots.
func callCopySlotKey(result string, index int) string {
	return fmt.Sprintf("call-copy:%s:%d", result, index)
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
