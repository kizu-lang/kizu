package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const boxWasmPrefix = "std::mem::Box<"

// isBoxWasmType reports whether name is a direct Box storage type.
func isBoxWasmType(name string) bool {
	return strings.HasPrefix(name, boxWasmPrefix) && strings.HasSuffix(name, ">")
}

// boxElementWasmType returns T for a direct Box<T> storage type.
func boxElementWasmType(name string) (string, bool) {
	if !isBoxWasmType(name) {
		return "", false
	}
	return name[len(boxWasmPrefix) : len(name)-1], true
}

// usesBoxRuntime reports whether this module allocates or releases Box cells.
func (e *emitter) usesBoxRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "box.") {
					return true
				}
			}
		}
	}
	return false
}

// writeBoxInstr dispatches Box operations to their wasm32 lowerings.
func (e *emitter) writeBoxInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "box.new":
		return e.writeBoxNew(instr)
	case "box.borrow", "box.borrow_mut":
		return e.writeBoxBorrow(instr)
	case "box.deinit":
		return e.writeBoxDeinit(instr)
	case "box.take":
		return e.writeBoxTake(instr)
	default:
		return fmt.Errorf("wasm error: unsupported box instruction `%s`", instr.Op)
	}
}

// boxElementLayout resolves and measures the payload of a Box type.
func (e *emitter) boxElementLayout(name string) (string, wasmLayout, error) {
	elem, ok := boxElementWasmType(name)
	if !ok {
		return "", wasmLayout{}, fmt.Errorf("wasm error: expected Box<T>, got %s", name)
	}
	layout, err := e.typeLayout(elem)
	if err != nil {
		return "", wasmLayout{}, err
	}
	return elem, layout, nil
}

// writeBoxNew allocates one payload-only cell and builds its recoverable result.
func (e *emitter) writeBoxNew(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[0].Type != "Allocator" {
		return fmt.Errorf("wasm error: box.new expects allocator, T -> !Box<T>")
	}
	errorSet, boxType, payloadOffset, err := e.errorPayloadOffset(instr.Result.Type)
	if err != nil || errorSet != "std::mem::Error" {
		return fmt.Errorf("wasm error: box.new expects allocator, T -> !Box<T>")
	}
	elem, layout, err := e.boxElementLayout(boxType)
	if err != nil || instr.Args[1].Type != elem {
		return fmt.Errorf("wasm error: box.new expects allocator, T -> !Box<T>")
	}
	code, err := e.wasmErrorCode(errorSet, "OutOfMemory")
	if err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	cell := addressAt(slot, payloadOffset)
	allocation := "(i32.const 0)"
	if layout.size > 0 {
		allocation = fmt.Sprintf("(call $__allocator_alloc %s (i32.const %d))",
			e.value(instr.Args[0]).expr, layout.size)
	}
	fmt.Fprintf(&e.out, "            (i32.store %s %s)\n", cell, allocation)
	fmt.Fprintf(&e.out, "            (i64.store %s "+
		"(i64.extend_i32_u (i32.ne (i32.load %s) (i32.const 0))))\n", slot, cell)
	fmt.Fprintf(&e.out, "            (if (i32.load %s)\n", cell)
	e.out.WriteString("              (then\n")
	if err := e.writeStoreValue("(i32.load "+cell+")", 0, elem,
		e.value(instr.Args[1])); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	fmt.Fprintf(&e.out, "              (else (i64.store %s (i64.const %d))))\n", cell, code)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeBoxBorrow returns the payload address, or loads a copy-shaped result.
func (e *emitter) writeBoxBorrow(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: %s expects Box<T> -> &T", instr.Op)
	}
	elem, _, err := e.boxElementLayout(instr.Args[0].Type)
	if err != nil {
		return fmt.Errorf("wasm error: %s expects Box<T> -> &T", instr.Op)
	}
	box := e.value(instr.Args[0]).expr
	if isReferenceType(instr.Result.Type) {
		if derefWasmType(instr.Result.Type) != elem {
			return fmt.Errorf("wasm error: %s expects Box<T> -> &T", instr.Op)
		}
		return e.writeScalarResult(instr.Result, box)
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("wasm error: %s expects Box<T> -> &T", instr.Op)
	}
	return e.writeLoadValue(instr.Result, box, 0)
}

// writeBoxDeinit releases one payload-only cell through the named allocator.
func (e *emitter) writeBoxDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: box.deinit expects Box<T>, Allocator -> void")
	}
	_, layout, err := e.boxElementLayout(instr.Args[0].Type)
	if err != nil {
		return fmt.Errorf("wasm error: box.deinit expects Box<T>, Allocator -> void")
	}
	if layout.size > 0 {
		fmt.Fprintf(&e.out, "            (call $__allocator_free %s %s (i32.const %d))\n",
			e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr, layout.size)
	}
	return nil
}

// writeBoxTake moves the payload out before releasing its cell.
func (e *emitter) writeBoxTake(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "Allocator" {
		return fmt.Errorf("wasm error: box.take expects Box<T>, Allocator -> T")
	}
	elem, layout, err := e.boxElementLayout(instr.Args[0].Type)
	if err != nil || instr.Result.Type != elem {
		return fmt.Errorf("wasm error: box.take expects Box<T>, Allocator -> T")
	}
	box := e.value(instr.Args[0]).expr
	if err := e.writeLoadValue(instr.Result, box, 0); err != nil {
		return err
	}
	if layout.size > 0 {
		fmt.Fprintf(&e.out, "            (call $__allocator_free %s %s (i32.const %d))\n",
			e.value(instr.Args[1]).expr, box, layout.size)
	}
	return nil
}
