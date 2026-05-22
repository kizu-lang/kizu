package wasm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	scratchOffset = 0
	intBufferEnd  = 192
	dataOffset    = 4096
)

// Emit formats a typed SSA IR module as WASI-compatible WebAssembly text.
func Emit(module *ir.Module) (string, error) {
	e := &emitter{module: module, strings: map[string]dataRef{}, values: map[string]valueInfo{}}
	if err := e.emit(); err != nil {
		return "", err
	}
	return strings.TrimRight(e.out.String(), "\n"), nil
}

type dataRef struct {
	offset int
	length int
}

type valueInfo struct {
	typ    string
	expr   string
	length int
}

type emitter struct {
	module  *ir.Module
	out     bytes.Buffer
	strings map[string]dataRef
	values  map[string]valueInfo
}

// emit writes the module, runtime helpers, and user functions.
func (e *emitter) emit() error {
	e.collectStrings()
	e.writeHeader()
	e.writeRuntime()
	for _, fn := range e.module.Functions {
		if err := e.writeFunction(fn); err != nil {
			return err
		}
	}
	e.out.WriteString("  (func $_start (export \"_start\")\n")
	e.out.WriteString("    (call $main))\n")
	e.out.WriteString(")\n")
	return nil
}

// collectStrings assigns stable memory offsets to literal data.
func (e *emitter) collectStrings() {
	offset := dataOffset
	for _, lit := range e.sortedStringLiteralsByDiscovery() {
		unquoted, _ := strconv.Unquote(lit)
		e.strings[lit] = dataRef{offset: offset, length: len(unquoted)}
		offset += len(unquoted)
	}
	e.strings["true"] = dataRef{offset: offset, length: 4}
	offset += 4
	e.strings["false"] = dataRef{offset: offset, length: 5}
	offset += 5
	e.strings["newline"] = dataRef{offset: offset, length: 1}
}

// sortedStringLiteralsByDiscovery returns constants in deterministic IR order.
func (e *emitter) sortedStringLiteralsByDiscovery() []string {
	found := map[string]bool{}
	literals := []string{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "const" && instr.Result.Type == "[]u8" &&
					!found[instr.Immediate] {
					found[instr.Immediate] = true
					literals = append(literals, instr.Immediate)
				}
			}
		}
	}
	return literals
}

// writeHeader writes imports, memory, and data segments.
func (e *emitter) writeHeader() {
	e.out.WriteString("(module\n")
	e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"fd_write\"\n")
	e.out.WriteString("    (func $__wasi_fd_write (param i32 i32 i32 i32) (result i32)))\n")
	e.out.WriteString("  (memory (export \"memory\") 1)\n")
	for _, lit := range e.sortedDataLiterals() {
		ref := e.strings[lit]
		fmt.Fprintf(&e.out, "  (data (i32.const %d) \"%s\")\n", ref.offset, dataLiteral(lit))
	}
	e.out.WriteByte('\n')
}

// sortedDataLiterals returns memory data in ascending offset order.
func (e *emitter) sortedDataLiterals() []string {
	literals := make([]string, 0, len(e.strings))
	for lit := range e.strings {
		literals = append(literals, lit)
	}
	sort.Slice(literals, func(i int, j int) bool {
		return e.strings[literals[i]].offset < e.strings[literals[j]].offset
	})
	return literals
}

// dataLiteral converts a map key to WAT data text.
func dataLiteral(key string) string {
	switch key {
	case "newline":
		return "\\0a"
	case "true", "false":
		return key
	default:
		unquoted, _ := strconv.Unquote(key)
		return stringBytes(unquoted)
	}
}

// writeRuntime writes the minimal WASI stdout helpers.
func (e *emitter) writeRuntime() {
	e.writeLineHelper()
	e.writeBoolHelper()
	e.writeIntHelper()
}

// writeLineHelper writes a buffer and a trailing newline to stdout.
func (e *emitter) writeLineHelper() {
	newline := e.strings["newline"]
	fmt.Fprintf(&e.out, "  (func $__write_line (param $ptr i32) (param $len i32)\n")
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) (local.get $ptr))\n", scratchOffset)
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) (local.get $len))\n", scratchOffset+4)
	fmt.Fprintf(&e.out, "    (drop (call $__wasi_fd_write\n")
	fmt.Fprintf(&e.out, "      (i32.const 1) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) ", scratchOffset)
	fmt.Fprintf(&e.out, "(i32.const %d))\n", newline.offset)
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) (i32.const 1))\n", scratchOffset+4)
	fmt.Fprintf(&e.out, "    (drop (call $__wasi_fd_write\n")
	fmt.Fprintf(&e.out, "      (i32.const 1) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	e.out.WriteString("  )\n\n")
}

// writeBoolHelper writes bool values as lowercase text.
func (e *emitter) writeBoolHelper() {
	truth := e.strings["true"]
	falsehood := e.strings["false"]
	e.out.WriteString("  (func $__print_bool (param $value i32)\n")
	e.out.WriteString("    (if (local.get $value)\n")
	fmt.Fprintf(&e.out, "      (then (call $__write_line (i32.const %d) (i32.const %d)))\n",
		truth.offset, truth.length)
	fmt.Fprintf(&e.out, "      (else (call $__write_line (i32.const %d) (i32.const %d))))\n",
		falsehood.offset, falsehood.length)
	e.out.WriteString("  )\n\n")
}

// writeIntHelper writes signed i64 values as decimal text.
func (e *emitter) writeIntHelper() {
	e.out.WriteString("  (func $__print_i64 (param $value i64)\n")
	e.out.WriteString("    (local $n i64) (local $pos i32) (local $negative i32)\n")
	e.out.WriteString("    (local.set $n (local.get $value))\n")
	e.out.WriteString("    (local.set $pos (i32.const 192))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $n))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (i32.store8 (i32.const 191) (i32.const 48))\n")
	e.out.WriteString("        (call $__write_line (i32.const 191) (i32.const 1))\n")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $n) (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $negative (i32.const 1))\n")
	e.out.WriteString("        (local.set $n (i64.sub (i64.const 0) (local.get $n)))))\n")
	e.writeIntLoop()
	e.out.WriteString("    (if (local.get $negative)\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $pos (i32.sub (local.get $pos) (i32.const 1)))\n")
	e.out.WriteString("        (i32.store8 (local.get $pos) (i32.const 45))))\n")
	fmt.Fprintf(&e.out, "    (call $__write_line (local.get $pos) ")
	fmt.Fprintf(&e.out, "(i32.sub (i32.const %d) ", intBufferEnd)
	e.out.WriteString("(local.get $pos)))\n")
	e.out.WriteString("  )\n\n")
}

// writeIntLoop writes decimal digits into memory from right to left.
func (e *emitter) writeIntLoop() {
	e.out.WriteString("    (loop $digits\n")
	e.out.WriteString("      (local.set $pos (i32.sub (local.get $pos) (i32.const 1)))\n")
	e.out.WriteString("      (i32.store8 (local.get $pos)\n")
	e.out.WriteString("        (i32.add\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.rem_u (local.get $n) (i64.const 10)))\n")
	e.out.WriteString("          (i32.const 48)))\n")
	e.out.WriteString("      (local.set $n (i64.div_u (local.get $n) (i64.const 10)))\n")
	e.out.WriteString("      (br_if $digits (i64.ne (local.get $n) (i64.const 0))))\n")
}

// writeFunction writes one user function with a dispatch loop for blocks.
func (e *emitter) writeFunction(fn *ir.Function) error {
	e.values = map[string]valueInfo{}
	params := e.functionParams(fn)
	fmt.Fprintf(&e.out, "  (func $%s %s%s\n", fn.Name, params, functionResult(fn.Return))
	e.writeLocals(fn)
	e.out.WriteString("    (local $pc i32)\n")
	e.out.WriteString("    (block $exit\n")
	e.out.WriteString("      (loop $dispatch\n")
	index := blockIndexes(fn)
	for i, block := range fn.Blocks {
		if err := e.writeBlock(block, index, i); err != nil {
			return err
		}
	}
	e.out.WriteString("        (br $exit)\n")
	e.out.WriteString("      )\n")
	e.out.WriteString("    )\n")
	if fn.Return != "void" {
		e.out.WriteString("    (unreachable)\n")
	}
	e.out.WriteString("  )\n\n")
	return nil
}

// functionParams writes WebAssembly parameters and records their values.
func (e *emitter) functionParams(fn *ir.Function) string {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		name := symbolName(param.Name)
		params = append(params, fmt.Sprintf("(param %s %s)", name, wasmType(param.Type)))
		e.values[param.Name] = valueInfo{typ: param.Type, expr: fmt.Sprintf("(local.get %s)", name)}
	}
	return strings.Join(params, " ")
}

// functionResult returns the WebAssembly result declaration.
func functionResult(typ string) string {
	if typ == "void" {
		return ""
	}
	return " (result " + wasmType(typ) + ")"
}

// writeLocals declares SSA locals that must live across blocks.
func (e *emitter) writeLocals(fn *ir.Function) {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if needsLocal(instr) {
				fmt.Fprintf(&e.out, "    (local %s %s)\n",
					symbolName(instr.Result.Name), wasmType(instr.Result.Type))
			}
		}
	}
}

// needsLocal reports whether an instruction result needs a WebAssembly local.
func needsLocal(instr *ir.Instr) bool {
	return instr.Result.Type != "" && instr.Result.Type != "void" &&
		!(instr.Op == "const" && instr.Result.Type == "[]u8")
}

// blockIndexes maps block names to dispatch ids.
func blockIndexes(fn *ir.Function) map[string]int {
	index := map[string]int{}
	for i, block := range fn.Blocks {
		index[block.Name] = i
	}
	return index
}
