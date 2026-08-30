package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	panicPrefixKey = "panic.prefix"
	panicAtKey     = "panic.at"
	panicColonKey  = "panic.colon"
)

// wasmPanicData is one runtime-owned diagnostic fragment in linear memory.
type wasmPanicData struct {
	key  string
	text string
}

// wasmPanicSpec is one checked failure kind carried by cond_fail.
type wasmPanicSpec struct {
	kind       string
	helper     string
	valueCount int
	summary    wasmPanicData
	extra      []wasmPanicData
}

var wasmPanicCommonData = []wasmPanicData{
	{key: panicPrefixKey, text: "runtime error: "},
	{key: panicAtKey, text: " at "},
	{key: panicColonKey, text: ":"},
}

var wasmPanicSpecs = []wasmPanicSpec{
	{
		kind:       "bounds",
		helper:     "__panic_bounds",
		valueCount: 2,
		summary:    wasmPanicData{key: "panic.bounds.summary", text: "index out of bounds"},
		extra: []wasmPanicData{
			{key: "panic.bounds.note", text: "note: index is "},
			{key: "panic.bounds.length", text: ", length is "},
		},
	},
	{
		kind:       "range",
		helper:     "__panic_range",
		valueCount: 3,
		summary:    wasmPanicData{key: "panic.range.summary", text: "range out of bounds"},
		extra: []wasmPanicData{
			{key: "panic.range.note", text: "note: range is "},
			{key: "panic.range.dots", text: ".."},
			{key: "panic.range.length", text: ", length is "},
		},
	},
	{
		kind:       "array_empty",
		helper:     "__panic_array_empty",
		valueCount: 0,
		summary:    wasmPanicData{key: "panic.array_empty.summary", text: "array pop from empty"},
	},
	{
		kind:       "arena_empty",
		helper:     "__panic_arena_empty",
		valueCount: 0,
		summary:    wasmPanicData{key: "panic.arena_empty.summary", text: "arena pop from empty"},
	},
	{
		kind:       "arena_handle",
		helper:     "__panic_arena_handle",
		valueCount: 0,
		summary:    wasmPanicData{key: "panic.arena_handle.summary", text: "invalid arena handle"},
	},
	{
		kind:       "arena_full",
		helper:     "__panic_arena_full",
		valueCount: 0,
		summary:    wasmPanicData{key: "panic.arena_full.summary", text: "arena is full"},
	},
}

// collectPanicKinds records checked failures before the header and data are
// written, so modules without one gain neither proc_exit nor panic helpers.
func (e *emitter) collectPanicKinds() {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "cond_fail" {
					e.panicKinds[instr.Immediate] = true
				}
			}
		}
	}
}

// usedPanicData returns runtime fragments in fixed helper-table order.
func (e *emitter) usedPanicData() []wasmPanicData {
	if len(e.panicKinds) == 0 {
		return nil
	}
	data := append([]wasmPanicData{}, wasmPanicCommonData...)
	for _, spec := range wasmPanicSpecs {
		if !e.panicKinds[spec.kind] {
			continue
		}
		data = append(data, spec.summary)
		data = append(data, spec.extra...)
	}
	return data
}

// panicDataText resolves a runtime data key for the WAT data renderer.
func panicDataText(key string) (string, bool) {
	for _, data := range wasmPanicCommonData {
		if data.key == key {
			return data.text, true
		}
	}
	for _, spec := range wasmPanicSpecs {
		if spec.summary.key == key {
			return spec.summary.text, true
		}
		for _, data := range spec.extra {
			if data.key == key {
				return data.text, true
			}
		}
	}
	return "", false
}

// wasmPanicSpecFor returns the runtime entry selected by one IR kind.
func wasmPanicSpecFor(kind string) (wasmPanicSpec, bool) {
	for _, spec := range wasmPanicSpecs {
		if spec.kind == kind {
			return spec, true
		}
	}
	return wasmPanicSpec{}, false
}

// writeCondFail calls one runtime failure when its checked condition is true.
func (e *emitter) writeCondFail(instr *ir.Instr) error {
	spec, ok := wasmPanicSpecFor(instr.Immediate)
	if !ok {
		return fmt.Errorf("wasm error: unknown cond_fail kind `%s`", instr.Immediate)
	}
	if len(instr.Args) != spec.valueCount+1 || instr.Args[0].Type != "bool" {
		return fmt.Errorf("wasm error: cond_fail `%s` expects bool and %d values",
			instr.Immediate, spec.valueCount)
	}
	cond := e.value(instr.Args[0]).expr
	args := make([]string, 0, spec.valueCount+2)
	for _, arg := range instr.Args[1:] {
		args = append(args, e.value(arg).expr)
	}
	args = append(args,
		fmt.Sprintf("(i64.const %d)", instr.Span.Start.Line),
		fmt.Sprintf("(i64.const %d)", instr.Span.Start.Column),
	)
	fmt.Fprintf(&e.out, "            (if %s\n", cond)
	e.out.WriteString("              (then\n")
	fmt.Fprintf(&e.out, "                (call $%s %s)))\n",
		spec.helper, strings.Join(args, " "))
	return nil
}

// writePanicRuntime writes only the kind-specific checked failures this module
// uses, plus their shared stderr, position, integer, and exit boundary.
func (e *emitter) writePanicRuntime() {
	e.writePanicExitHelper()
	e.writePanicSummaryHelper()
	for _, spec := range wasmPanicSpecs {
		if !e.panicKinds[spec.kind] {
			continue
		}
		switch spec.kind {
		case "bounds":
			e.writePanicBoundsHelper(spec)
		case "range":
			e.writePanicRangeHelper(spec)
		default:
			e.writeSimplePanicHelper(spec)
		}
	}
}

// writePanicExitHelper ends a checked failure without relying on an engine
// trap message. The unreachable keeps termination explicit if a host returns.
func (e *emitter) writePanicExitHelper() {
	e.out.WriteString("  (func $__panic_exit\n")
	e.out.WriteString("    (call $__wasi_proc_exit (i32.const 1))\n")
	e.out.WriteString("    (unreachable)\n")
	e.out.WriteString("  )\n\n")
}

// writePanicSummaryHelper writes `runtime error: summary`, an optional source
// position, and a newline to stderr.
func (e *emitter) writePanicSummaryHelper() {
	prefix := e.strings[panicPrefixKey]
	at := e.strings[panicAtKey]
	colon := e.strings[panicColonKey]
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__panic_summary (param $ptr i32) (param $len i32)\n")
	e.out.WriteString("      (param $line i64) (param $column i64)\n")
	e.writePanicBytes(prefix, "    ")
	e.out.WriteString("    (call $__write_bytes (i32.const 2) (local.get $ptr) (local.get $len))\n")
	e.out.WriteString("    (if (i64.gt_s (local.get $line) (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	e.writePanicBytes(at, "        ")
	e.out.WriteString("        (call $__write_i64 (i32.const 2) (local.get $line))\n")
	e.writePanicBytes(colon, "        ")
	e.out.WriteString("        (call $__write_i64 (i32.const 2) (local.get $column))))\n")
	e.writePanicBytes(newline, "    ")
	e.out.WriteString("  )\n\n")
}

// writePanicBoundsHelper writes the context carried by bounds(index, length).
func (e *emitter) writePanicBoundsHelper(spec wasmPanicSpec) {
	summary := e.strings[spec.summary.key]
	note := e.strings[spec.extra[0].key]
	length := e.strings[spec.extra[1].key]
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__panic_bounds (param $index i64) (param $length i64)\n")
	e.out.WriteString("      (param $line i64) (param $column i64)\n")
	e.writePanicSummaryCall(summary)
	e.writePanicBytes(note, "    ")
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $index))\n")
	e.writePanicBytes(length, "    ")
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $length))\n")
	e.writePanicBytes(newline, "    ")
	e.out.WriteString("    (call $__panic_exit)\n")
	e.out.WriteString("  )\n\n")
}

// writePanicRangeHelper writes the context carried by range(start, end, len).
func (e *emitter) writePanicRangeHelper(spec wasmPanicSpec) {
	summary := e.strings[spec.summary.key]
	note := e.strings[spec.extra[0].key]
	dots := e.strings[spec.extra[1].key]
	length := e.strings[spec.extra[2].key]
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__panic_range (param $start i64) (param $end i64)\n")
	e.out.WriteString("      (param $length i64) (param $line i64) (param $column i64)\n")
	e.writePanicSummaryCall(summary)
	e.writePanicBytes(note, "    ")
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $start))\n")
	e.writePanicBytes(dots, "    ")
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $end))\n")
	e.writePanicBytes(length, "    ")
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $length))\n")
	e.writePanicBytes(newline, "    ")
	e.out.WriteString("    (call $__panic_exit)\n")
	e.out.WriteString("  )\n\n")
}

// writeSimplePanicHelper writes a checked failure with no dynamic context.
func (e *emitter) writeSimplePanicHelper(spec wasmPanicSpec) {
	summary := e.strings[spec.summary.key]
	fmt.Fprintf(&e.out, "  (func $%s (param $line i64) (param $column i64)\n", spec.helper)
	e.writePanicSummaryCall(summary)
	e.out.WriteString("    (call $__panic_exit)\n")
	e.out.WriteString("  )\n\n")
}

// writePanicSummaryCall emits one call over a retained summary fragment.
func (e *emitter) writePanicSummaryCall(summary dataRef) {
	fmt.Fprintf(&e.out,
		"    (call $__panic_summary (i32.const %d) (i32.const %d) "+
			"(local.get $line) (local.get $column))\n",
		summary.offset, summary.length)
}

// writePanicBytes emits one stderr write over a retained fragment.
func (e *emitter) writePanicBytes(data dataRef, indent string) {
	fmt.Fprintf(&e.out,
		"%s(call $__write_bytes (i32.const 2) (i32.const %d) (i32.const %d))\n",
		indent, data.offset, data.length)
}
