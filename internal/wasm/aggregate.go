package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeStructNew materializes a checked struct literal in its fixed result
// slot, storing fields by declaration order and target layout.
func (e *emitter) writeStructNew(instr *ir.Instr) error {
	st, ok := e.module.Structs[instr.Result.Type]
	if !ok {
		return fmt.Errorf("wasm error: unknown struct type `%s`", instr.Result.Type)
	}
	values := map[string]ir.Value{}
	for _, field := range instr.Fields {
		if _, exists := values[field.Name]; exists {
			return fmt.Errorf("wasm error: duplicate struct field `%s.%s`", st.Name, field.Name)
		}
		values[field.Name] = field.Value
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	for _, field := range st.Fields {
		value, ok := values[field.Name]
		if !ok {
			return fmt.Errorf("wasm error: missing struct field `%s.%s`", st.Name, field.Name)
		}
		if value.Type != field.Type {
			return fmt.Errorf("wasm error: struct field `%s.%s` expects %s, got %s",
				st.Name, field.Name, field.Type, value.Type)
		}
		_, offset, err := e.fieldLayout(st.Name, field.Name)
		if err != nil {
			return err
		}
		if err := e.writeStoreValue(slot, offset, field.Type, e.value(value)); err != nil {
			return err
		}
	}
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeFieldInstr dispatches value reads, borrowed reads, value updates,
// borrowed updates, and field-address projections.
func (e *emitter) writeFieldInstr(instr *ir.Instr) error {
	switch {
	case strings.HasPrefix(instr.Op, "field.ref.set."):
		return e.writeFieldRefSet(instr)
	case strings.HasPrefix(instr.Op, "field.ref."):
		return e.writeFieldRead(instr, true)
	case strings.HasPrefix(instr.Op, "field.addr."):
		return e.writeFieldAddr(instr)
	case strings.HasPrefix(instr.Op, "field.set."):
		return e.writeFieldSet(instr)
	default:
		return e.writeFieldRead(instr, false)
	}
}

// writeFieldRead loads one field from an owned or borrowed struct value.
func (e *emitter) writeFieldRead(instr *ir.Instr, borrowed bool) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: field read expects 1 arg")
	}
	receiver := instr.Args[0]
	structName := receiver.Type
	prefix := "field."
	if borrowed {
		structName = derefWasmType(receiver.Type)
		prefix = "field.ref."
	}
	fieldName := strings.TrimPrefix(instr.Op, prefix)
	field, offset, err := e.fieldLayout(structName, fieldName)
	if err != nil {
		return err
	}
	if instr.Result.Type != field.Type {
		return fmt.Errorf("wasm error: field `%s.%s` returns %s, got %s",
			structName, fieldName, field.Type, instr.Result.Type)
	}
	return e.writeLoadValue(instr.Result, e.value(receiver).expr, offset)
}

// writeFieldAddr projects a field reference from a borrowed struct.
func (e *emitter) writeFieldAddr(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: field address expects 1 arg")
	}
	receiver := instr.Args[0]
	structName := derefWasmType(receiver.Type)
	fieldName := strings.TrimPrefix(instr.Op, "field.addr.")
	field, offset, err := e.fieldLayout(structName, fieldName)
	if err != nil {
		return err
	}
	if derefWasmType(instr.Result.Type) != field.Type {
		return fmt.Errorf("wasm error: field address `%s.%s` returns %s, got %s",
			structName, fieldName, field.Type, instr.Result.Type)
	}
	expr := addressAt(e.value(receiver).expr, offset)
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeFieldSet copies a struct value and replaces one field.
func (e *emitter) writeFieldSet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: field write expects 2 args")
	}
	receiver := instr.Args[0]
	fieldName := strings.TrimPrefix(instr.Op, "field.set.")
	field, offset, err := e.fieldLayout(receiver.Type, fieldName)
	if err != nil {
		return err
	}
	if instr.Args[1].Type != field.Type || instr.Result.Type != receiver.Type {
		return fmt.Errorf("wasm error: field `%s.%s` update has mismatched types",
			receiver.Type, fieldName)
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	layout, err := e.typeLayout(receiver.Type)
	if err != nil {
		return err
	}
	e.writeMemoryCopy(slot, e.value(receiver).expr, layout.size)
	if err := e.writeStoreValue(slot, offset, field.Type, e.value(instr.Args[1])); err != nil {
		return err
	}
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeFieldRefSet stores one field through a mutable struct reference.
func (e *emitter) writeFieldRefSet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: borrowed field write expects 2 args and void result")
	}
	receiver := instr.Args[0]
	structName := derefWasmType(receiver.Type)
	fieldName := strings.TrimPrefix(instr.Op, "field.ref.set.")
	field, offset, err := e.fieldLayout(structName, fieldName)
	if err != nil {
		return err
	}
	if instr.Args[1].Type != field.Type {
		return fmt.Errorf("wasm error: borrowed field `%s.%s` accepts %s, got %s",
			structName, fieldName, field.Type, instr.Args[1].Type)
	}
	return e.writeStoreValue(e.value(receiver).expr, offset, field.Type, e.value(instr.Args[1]))
}

// writeLocalSlot initializes one addressable local storage cell.
func (e *emitter) writeLocalSlot(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: local slot expects 1 arg")
	}
	want := derefWasmType(instr.Result.Type)
	if want != instr.Args[0].Type {
		return fmt.Errorf("wasm error: local slot of %s holds %s, got %s",
			instr.Result.Type, want, instr.Args[0].Type)
	}
	slot, err := e.localSlot(instr.Result)
	if err != nil {
		return err
	}
	if err := e.writeStoreValue(slot, 0, want, e.value(instr.Args[0])); err != nil {
		return err
	}
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeRefStore writes a typed value through a reference.
func (e *emitter) writeRefStore(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: dereference write expects 2 args and void result")
	}
	want := derefWasmType(instr.Args[0].Type)
	if want != instr.Args[1].Type {
		return fmt.Errorf("wasm error: dereference write on `%s` accepts %s, got %s",
			instr.Args[0].Type, want, instr.Args[1].Type)
	}
	return e.writeStoreValue(e.value(instr.Args[0]).expr, 0, want, e.value(instr.Args[1]))
}

// writeRefLoad reads a typed value through a reference.
func (e *emitter) writeRefLoad(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: borrow read expects 1 arg")
	}
	want := derefWasmType(instr.Args[0].Type)
	if want != instr.Result.Type {
		return fmt.Errorf("wasm error: borrow read of `%s` gives %s, got %s",
			instr.Args[0].Type, want, instr.Result.Type)
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, 0)
}

// writeUnionInstr dispatches tagged-union construction and projection.
func (e *emitter) writeUnionInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "union.new":
		return e.writeUnionNew(instr)
	case "union.load":
		return e.writeUnionLoad(instr)
	case "union.tag":
		return e.writeUnionTag(instr)
	case "union.payload":
		return e.writeUnionPayload(instr)
	default:
		return fmt.Errorf("wasm error: unsupported union instruction `%s`", instr.Op)
	}
}

// writeUnionNew stores a union tag and its optional inline payload.
func (e *emitter) writeUnionNew(instr *ir.Instr) error {
	union, ok := e.module.Unions[instr.Result.Type]
	if !ok {
		return fmt.Errorf("wasm error: unknown union type `%s`", instr.Result.Type)
	}
	variant, ok := union.Variants[instr.Immediate]
	if !ok {
		return fmt.Errorf("wasm error: unknown union variant `%s::%s`",
			instr.Result.Type, instr.Immediate)
	}
	if len(instr.Args) > 1 || (variant.Payload == "" && len(instr.Args) != 0) ||
		(variant.Payload != "" && len(instr.Args) != 1) {
		return fmt.Errorf("wasm error: union variant `%s::%s` has the wrong payload count",
			instr.Result.Type, instr.Immediate)
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	tag := valueInfo{expr: fmt.Sprintf("(i64.const %d)", variant.Index)}
	if err := e.writeStoreValue(slot, 0, "i64", tag); err != nil {
		return err
	}
	if variant.Payload != "" {
		if instr.Args[0].Type != variant.Payload {
			return fmt.Errorf("wasm error: union variant `%s::%s` expects %s, got %s",
				instr.Result.Type, instr.Immediate, variant.Payload, instr.Args[0].Type)
		}
		offset, err := e.unionPayloadOffset(instr.Result.Type)
		if err != nil {
			return err
		}
		if err := e.writeStoreValue(slot, offset, variant.Payload, e.value(instr.Args[0])); err != nil {
			return err
		}
	}
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeUnionLoad copies a union value through a reference.
func (e *emitter) writeUnionLoad(instr *ir.Instr) error {
	if len(instr.Args) != 1 || derefWasmType(instr.Args[0].Type) != instr.Result.Type {
		return fmt.Errorf("wasm error: union.load expects one borrowed union argument")
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, 0)
}

// writeUnionTag loads a union's discriminant.
func (e *emitter) writeUnionTag(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: union.tag expects union -> i64")
	}
	if _, ok := e.module.Unions[instr.Args[0].Type]; !ok {
		return fmt.Errorf("wasm error: union.tag expects union, got `%s`", instr.Args[0].Type)
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, 0)
}

// writeUnionPayload loads the checked active variant payload.
func (e *emitter) writeUnionPayload(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: union.payload expects one union argument")
	}
	union, ok := e.module.Unions[instr.Args[0].Type]
	if !ok {
		return fmt.Errorf("wasm error: unknown union type `%s`", instr.Args[0].Type)
	}
	variant, ok := union.Variants[instr.Immediate]
	if !ok || variant.Payload == "" || variant.Payload != instr.Result.Type {
		return fmt.Errorf("wasm error: unknown union payload `%s::%s`",
			instr.Args[0].Type, instr.Immediate)
	}
	offset, err := e.unionPayloadOffset(instr.Args[0].Type)
	if err != nil {
		return err
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, offset)
}
