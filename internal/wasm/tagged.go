package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// taggedValueLayout returns the wasm32 layout of an i64 tag followed by one
// aligned inline payload, and the byte offset where that payload starts.
// Optional values, error unions, and declared unions share this representation.
func taggedValueLayout(payload wasmLayout) (wasmLayout, int) {
	align := maxInt(8, payload.align)
	payloadOffset := alignUp(8, payload.align)
	return wasmLayout{
		size:  alignUp(payloadOffset+payload.size, align),
		align: align,
	}, payloadOffset
}

// errorUnionPayloadLayout reserves one common payload for either the success
// value or the global i64 error code.
func errorUnionPayloadLayout(success wasmLayout) wasmLayout {
	return wasmLayout{
		size:  maxInt(8, success.size),
		align: maxInt(8, success.align),
	}
}

// optionalElemWasm returns T for a tagged optional value `?T`. Nullable raw
// pointers keep their scalar pointer ABI and are not optional values.
func optionalElemWasm(name string) (string, bool) {
	return typ.OptionalElem(name)
}

// errorUnionParts returns Error and T for Error!T, or empty Error and T for
// !T. Invalid and non-error-union spellings report false.
func (e *emitter) errorUnionParts(name string) (string, string, bool) {
	parsed, err := e.types.Parse(name)
	if err != nil {
		return "", "", false
	}
	errorType, success, ok := typ.ErrorUnionParts(parsed)
	return typ.Text(errorType), typ.Text(success), ok
}

// wasmErrorCode returns one declaration-owned global error code.
func (e *emitter) wasmErrorCode(errorSet string, member string) (int, error) {
	set, exists := e.module.ErrorSets[errorSet]
	if !exists {
		return 0, fmt.Errorf("wasm error: failure needs error set `%s`", errorSet)
	}
	code, exists := set.Tags[member]
	if !exists {
		return 0, fmt.Errorf("wasm error: error set `%s` has no member `%s`", errorSet, member)
	}
	return code, nil
}

// taggedTypeLayoutVisiting computes an optional or error-union layout and
// reports false when typ is not one of those tagged values.
func (e *emitter) taggedTypeLayoutVisiting(
	typ string,
	seen map[string]bool,
) (wasmLayout, bool, error) {
	if elem, ok := optionalElemWasm(typ); ok {
		payload, err := e.typeLayoutVisiting(elem, seen)
		if err != nil {
			return wasmLayout{}, true, err
		}
		layout, _ := taggedValueLayout(payload)
		return layout, true, nil
	}
	if _, success, ok := e.errorUnionParts(typ); ok {
		payload, err := e.typeLayoutVisiting(success, seen)
		if err != nil {
			return wasmLayout{}, true, err
		}
		layout, _ := taggedValueLayout(errorUnionPayloadLayout(payload))
		return layout, true, nil
	}
	return wasmLayout{}, false, nil
}

// optionalPayloadOffset returns the inline payload offset of one `?T`.
func (e *emitter) optionalPayloadOffset(name string) (string, int, error) {
	elem, ok := optionalElemWasm(name)
	if !ok {
		return "", 0, fmt.Errorf("wasm error: expected optional type, got %s", name)
	}
	layout, err := e.typeLayout(elem)
	if err != nil {
		return "", 0, err
	}
	_, offset := taggedValueLayout(layout)
	return elem, offset, nil
}

// errorPayloadOffset returns the common success/error-code payload offset.
func (e *emitter) errorPayloadOffset(name string) (string, string, int, error) {
	errorType, success, ok := e.errorUnionParts(name)
	if !ok {
		return "", "", 0, fmt.Errorf("wasm error: expected error union, got %s", name)
	}
	layout, err := e.typeLayout(success)
	if err != nil {
		return "", "", 0, err
	}
	_, offset := taggedValueLayout(errorUnionPayloadLayout(layout))
	return errorType, success, offset, nil
}

// writeOptionalInstr dispatches tagged optional construction and projection.
func (e *emitter) writeOptionalInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "opt.null":
		return e.writeOptionalNull(instr)
	case "opt.some":
		return e.writeOptionalSome(instr)
	case "opt.has":
		return e.writeTaggedHas(instr, "opt.has")
	case "opt.value":
		return e.writeOptionalValue(instr)
	default:
		return fmt.Errorf("wasm error: unsupported optional instruction `%s`", instr.Op)
	}
}

// writeTaggedInstr dispatches optional and recoverable-result operations.
func (e *emitter) writeTaggedInstr(instr *ir.Instr) error {
	if strings.HasPrefix(instr.Op, "opt.") {
		return e.writeOptionalInstr(instr)
	}
	return e.writeErrorInstr(instr)
}

// writeOptionalNull builds an absent tagged optional.
func (e *emitter) writeOptionalNull(instr *ir.Instr) error {
	if _, ok := optionalElemWasm(instr.Result.Type); !ok || len(instr.Args) != 0 {
		return fmt.Errorf("wasm error: opt.null expects no args and a `?T` result")
	}
	return e.writeTaggedResult(instr.Result, 0)
}

// writeOptionalSome builds a present tagged optional and stores its payload.
func (e *emitter) writeOptionalSome(instr *ir.Instr) error {
	elem, offset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil {
		return err
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: opt.some expects one payload and a `?T` result")
	}
	if instr.Args[0].Type != elem {
		return fmt.Errorf("wasm error: opt.some expects %s, got %s", elem, instr.Args[0].Type)
	}
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	return e.writeStoreValue(e.value(instr.Result).expr, offset, elem, e.value(instr.Args[0]))
}

// writeOptionalValue extracts the payload whose presence the IR tests.
func (e *emitter) writeOptionalValue(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: opt.value expects 1 arg")
	}
	elem, offset, err := e.optionalPayloadOffset(instr.Args[0].Type)
	if err != nil {
		return err
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("wasm error: opt.value returns %s, got %s", elem, instr.Result.Type)
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, offset)
}

// writeErrorInstr dispatches recoverable-result operations.
func (e *emitter) writeErrorInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "error.ok":
		return e.writeErrorOK(instr)
	case "error.error":
		return e.writeErrorError(instr)
	case "error.try":
		return e.writeErrorTry(instr)
	case "error.has":
		return e.writeTaggedHas(instr, "error.has")
	case "error.value":
		return e.writeErrorValue(instr)
	case "error.code":
		return e.writeErrorCode(instr)
	default:
		return fmt.Errorf("wasm error: unsupported error instruction `%s`", instr.Op)
	}
}

// writeErrorOK builds a successful error union.
func (e *emitter) writeErrorOK(instr *ir.Instr) error {
	_, success, offset, err := e.errorPayloadOffset(instr.Result.Type)
	if err != nil {
		return err
	}
	if success == "void" {
		if len(instr.Args) != 0 {
			return fmt.Errorf("wasm error: error.ok !void expects 0 args")
		}
		return e.writeTaggedResult(instr.Result, 1)
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: error.ok %s expects 1 arg", instr.Result.Type)
	}
	if instr.Args[0].Type != success {
		return fmt.Errorf("wasm error: error.ok expects %s, got %s", success, instr.Args[0].Type)
	}
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	return e.writeStoreValue(e.value(instr.Result).expr, offset, success, e.value(instr.Args[0]))
}

// writeErrorError builds a failed error union around one global error code.
func (e *emitter) writeErrorError(instr *ir.Instr) error {
	errorType, _, offset, err := e.errorPayloadOffset(instr.Result.Type)
	if err != nil {
		return err
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: error.error expects one error value")
	}
	arg := instr.Args[0]
	if _, ok := e.module.ErrorSets[arg.Type]; !ok {
		return fmt.Errorf("wasm error: error.error expects an error set member, got %s", arg.Type)
	}
	if errorType != "" && !e.errorSetFits(arg.Type, errorType) {
		return fmt.Errorf("wasm error: error.error cannot put %s into %s", arg.Type, instr.Result.Type)
	}
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i64.store %s %s)\n",
		addressAt(e.value(instr.Result).expr, offset), e.value(arg).expr)
	return nil
}

// writeErrorValue extracts the success payload after error.has succeeded.
func (e *emitter) writeErrorValue(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: error.value expects 1 arg")
	}
	_, success, offset, err := e.errorPayloadOffset(instr.Args[0].Type)
	if err != nil {
		return err
	}
	if success == "void" {
		return fmt.Errorf("wasm error: error.value expects an `E!T` with a payload, got %s",
			instr.Args[0].Type)
	}
	if instr.Result.Type != success {
		return fmt.Errorf("wasm error: error.value returns %s, got %s", success, instr.Result.Type)
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, offset)
}

// writeErrorCode extracts the failed union's global error code.
func (e *emitter) writeErrorCode(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: error.code expects 1 arg")
	}
	errorType, _, offset, err := e.errorPayloadOffset(instr.Args[0].Type)
	if err != nil {
		return err
	}
	if errorType == "" {
		return fmt.Errorf("wasm error: error.code expects `E!T`, got %s", instr.Args[0].Type)
	}
	if instr.Result.Type != errorType {
		return fmt.Errorf("wasm error: error.code returns %s, got %s", errorType, instr.Result.Type)
	}
	return e.writeLoadValue(instr.Result, e.value(instr.Args[0]).expr, offset)
}

// writeTaggedHas tests the i64 tag shared by optionals and error unions.
func (e *emitter) writeTaggedHas(instr *ir.Instr, op string) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: %s expects 1 arg", op)
	}
	source := instr.Args[0]
	if op == "opt.has" {
		if _, ok := optionalElemWasm(source.Type); !ok {
			return fmt.Errorf("wasm error: opt.has expects `?T`, got %s", source.Type)
		}
	} else if _, _, ok := e.errorUnionParts(source.Type); !ok {
		return fmt.Errorf("wasm error: error.has expects !T, got %s", source.Type)
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (i64.ne (i64.load %s) (i64.const 0)))\n",
		symbol, e.value(source).expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeTaggedResult stores one tag in the result's fixed frame slot.
func (e *emitter) writeTaggedResult(result ir.Value, tag int) error {
	slot, err := e.resultSlot(result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.const %d))\n", slot, tag)
	e.values[result.Name] = valueInfo{expr: slot}
	return nil
}

// writeErrorTry unwraps success or returns the error code through the current
// function's hidden result storage after running attached cleanups.
func (e *emitter) writeErrorTry(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: error.try expects 1 arg")
	}
	source := instr.Args[0]
	sourceError, success, sourceOffset, err := e.errorPayloadOffset(source.Type)
	if err != nil {
		return err
	}
	if instr.Result.Type != success {
		return fmt.Errorf("wasm error: error.try returns %s, got %s", success, instr.Result.Type)
	}
	targetError, _, targetOffset, err := e.errorPayloadOffset(e.currentReturn)
	if err != nil {
		return fmt.Errorf("wasm error: error.try requires function to return !T")
	}
	if targetError != "" && !e.errorSetFits(sourceError, targetError) {
		return fmt.Errorf("wasm error: error.try cannot propagate %s into %s",
			source.Type, e.currentReturn)
	}
	sourceExpr := e.value(source).expr
	e.out.WriteString("            (if (i64.eq (i64.load " + sourceExpr + ") (i64.const 0))\n")
	e.out.WriteString("              (then\n")
	for _, cleanup := range instr.Cleanups {
		if err := e.writeCleanup(cleanup); err != nil {
			return err
		}
	}
	e.out.WriteString("                (i64.store (local.get $__kizu_result) (i64.const 0))\n")
	fmt.Fprintf(&e.out, "                (i64.store %s (i64.load %s))\n",
		addressAt("(local.get $__kizu_result)", targetOffset),
		addressAt(sourceExpr, sourceOffset))
	e.restoreFrame()
	e.out.WriteString("                (br $exit)))\n")
	if success == "void" {
		return nil
	}
	return e.writeLoadValue(instr.Result, sourceExpr, sourceOffset)
}

// writeCleanup emits one deferred void instruction inside error.try's failed arm.
func (e *emitter) writeCleanup(cleanup ir.Cleanup) error {
	return e.writeInstr(&ir.Instr{
		Result: ir.Value{Type: "void"},
		Op:     cleanup.Op,
		Args:   cleanup.Args,
	})
}

// errorSetFits reports whether every source error code belongs to target.
func (e *emitter) errorSetFits(source string, target string) bool {
	sourceCodes, ok := ir.ErrorSetCodes(e.module, source)
	if !ok {
		return false
	}
	targetCodes, ok := ir.ErrorSetCodes(e.module, target)
	if !ok {
		return false
	}
	for code := range sourceCodes {
		if !targetCodes[code] {
			return false
		}
	}
	return true
}

// writeStart exports the WASI entry point. A memory-backed main receives one
// result slot whose error and ExitStatus variants are mapped at the host
// boundary before the successful result storage is released.
func (e *emitter) writeStart() error {
	main := e.mainFunction()
	e.out.WriteString("  (func $_start (export \"_start\")\n")
	if main == nil || main.Return == "void" {
		e.writeProcessStartInit()
		e.out.WriteString("    (call $main))\n")
		e.out.WriteString(")\n")
		return nil
	}
	if !e.isMemoryType(main.Return) {
		e.writeProcessStartInit()
		e.out.WriteString("    (drop (call $main)))\n")
		e.out.WriteString(")\n")
		return nil
	}
	layout, err := e.typeLayout(main.Return)
	if err != nil {
		return err
	}
	e.out.WriteString("    (local $__kizu_main_result i32)\n")
	e.writeProcessStartInit()
	fmt.Fprintf(&e.out,
		"    (local.set $__kizu_main_result (call $__stack_alloc (i32.const %d)))\n",
		layout.size)
	e.out.WriteString("    (call $main (local.get $__kizu_main_result))\n")
	if _, _, ok := e.errorUnionParts(main.Return); ok {
		if err := e.writeMainResultBoundary(main); err != nil {
			return err
		}
	}
	if e.usesAllocatorRuntime() {
		fmt.Fprintf(&e.out,
			"    (call $__stack_free (local.get $__kizu_main_result) (i32.const %d))\n",
			layout.size)
	} else {
		e.out.WriteString("    (global.set $__stack_pointer (local.get $__kizu_main_result))\n")
	}
	e.out.WriteString("  )\n")
	e.out.WriteString(")\n")
	return nil
}
