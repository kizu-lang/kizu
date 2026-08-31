package wasm

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

const (
	scratchOffset = 0
	intBufferEnd  = 192
	dataOffset    = 4096
)

// Emit formats a typed SSA IR module as WASI-compatible WebAssembly text.
func Emit(module *ir.Module) (string, error) {
	paramsByFunction := make(map[string][]ir.Param, len(module.Functions))
	for _, fn := range module.Functions {
		paramsByFunction[fn.Name] = fn.Params
	}
	e := &emitter{
		module:           module,
		types:            typ.NewTable(),
		paramsByFunction: paramsByFunction,
		strings:          map[string]dataRef{},
		enumTables:       map[string]nameTable{},
		errorTable:       nameTable{},
		values:           map[string]valueInfo{},
		panicKinds:       map[string]bool{},
		tableIndex:       map[string]int{},
		signatureIndex:   map[string]int{},
	}
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
	expr string
}

type emitter struct {
	module           *ir.Module
	types            *typ.Table
	paramsByFunction map[string][]ir.Param
	out              bytes.Buffer
	strings          map[string]dataRef
	// dataOrder keeps assignment order when zero-length data shares an offset
	// with the following segment. Sorting offsets alone cannot order that tie.
	dataOrder []string
	// enumTables are the linear-memory name tables for enum types this module
	// prints. enumTableOrder preserves discovery order for deterministic WAT.
	enumTables     map[string]nameTable
	enumTableOrder []string
	// errorTable is the global-code-indexed {pointer, length} table used only
	// when a fallible main must report its uncaught error at the host boundary.
	errorTable nameTable
	values     map[string]valueInfo
	// panicKinds contains only the checked runtime failures this module uses.
	// Their data, proc_exit import, and helpers are omitted otherwise.
	panicKinds map[string]bool
	// dataEnd is the first byte after static data. Function frames start at its
	// aligned address and grow through the stack allocator.
	dataEnd int
	frame   *frameLayout
	// currentReturn is the Kizu return type of the function being written.
	// error.try uses it to rebuild a propagated failure in caller storage.
	currentReturn string
	// table lists, in call order, the functions whose address is taken. wasm
	// reaches a function pointer through a table index rather than an
	// address, so `func.addr` lowers to the position a name holds here.
	table      []string
	tableIndex map[string]int
	// signatures lists the `(type ...)` declarations `call_indirect` names,
	// in the order they were first needed.
	signatures     []funcSignature
	signatureIndex map[string]int
}

// funcSignature is one wasm function type a `call_indirect` names.
type funcSignature struct {
	params []string
	result string
}

// emit writes the module, runtime helpers, and user functions.
func (e *emitter) emit() error {
	if err := e.validateProcessTarget(); err != nil {
		return err
	}
	e.collectPanicKinds()
	e.collectStrings()
	if err := e.collectFunctionTable(); err != nil {
		return err
	}
	e.writeHeader()
	if err := e.writeRuntime(); err != nil {
		return err
	}
	for _, fn := range e.module.Functions {
		if err := e.writeFunction(fn); err != nil {
			return err
		}
	}
	return e.writeStart()
}

// collectFunctionTable walks the module for the addresses it takes and the
// indirect calls it makes, before anything is written: wasm declares its table
// and its function types in the header, above the functions that use them.
func (e *emitter) collectFunctionTable() error {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if err := e.collectCallableInstr(instr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// collectCallableInstr records the table entry or function type one
// instruction needs.
func (e *emitter) collectCallableInstr(instr *ir.Instr) error {
	if name, ok := strings.CutPrefix(instr.Op, "func.addr."); ok {
		if _, seen := e.tableIndex[name]; !seen {
			e.tableIndex[name] = len(e.table)
			e.table = append(e.table, name)
		}
		return nil
	}
	if instr.Op == "call.std::internal::builtin::mem_allocator_from" {
		e.internFuncSignature(userAllocatorAllocSignature())
		e.internFuncSignature(userAllocatorReleaseSignature())
		return nil
	}
	if instr.Op != "call.indirect" {
		return nil
	}
	if len(instr.Args) == 0 {
		return fmt.Errorf("wasm error: call.indirect expects a callee")
	}
	e.internSignature(instr)
	return nil
}

// internSignature records the wasm function type an indirect call names and
// returns its index.
func (e *emitter) internSignature(instr *ir.Instr) int {
	sig := funcSignature{}
	if e.isMemoryType(instr.Result.Type) {
		sig.params = append(sig.params, "i32")
	}
	for _, arg := range instr.Args[1:] {
		sig.params = append(sig.params, e.wasmType(arg.Type))
	}
	if instr.Result.Type != "void" && !e.isMemoryType(instr.Result.Type) {
		sig.result = e.wasmType(instr.Result.Type)
	}
	return e.internFuncSignature(sig)
}

// internFuncSignature records one already-lowered wasm function shape.
func (e *emitter) internFuncSignature(sig funcSignature) int {
	key := strings.Join(sig.params, ",") + "->" + sig.result
	if index, seen := e.signatureIndex[key]; seen {
		return index
	}
	index := len(e.signatures)
	e.signatureIndex[key] = index
	e.signatures = append(e.signatures, sig)
	return index
}

// collectStrings assigns stable memory offsets to literal data.
func (e *emitter) collectStrings() {
	offset := dataOffset
	for _, lit := range e.sortedStringLiteralsByDiscovery() {
		unquoted, _ := strconv.Unquote(lit)
		e.strings[lit] = dataRef{offset: offset, length: len(unquoted)}
		e.dataOrder = append(e.dataOrder, lit)
		offset += len(unquoted)
	}
	e.strings["true"] = dataRef{offset: offset, length: 4}
	e.dataOrder = append(e.dataOrder, "true")
	offset += 4
	e.strings["false"] = dataRef{offset: offset, length: 5}
	e.dataOrder = append(e.dataOrder, "false")
	offset += 5
	e.strings["newline"] = dataRef{offset: offset, length: 1}
	e.dataOrder = append(e.dataOrder, "newline")
	offset++
	for _, data := range e.usedPanicData() {
		e.strings[data.key] = dataRef{offset: offset, length: len(data.text)}
		e.dataOrder = append(e.dataOrder, data.key)
		offset += len(data.text)
	}
	offset = e.collectMainErrorStrings(offset)
	offset = e.collectEnumPrintData(offset)
	offset = e.collectMainErrorTable(offset)
	e.dataEnd = alignUp(offset, 8)
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
	if e.needsProcExit() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"proc_exit\"\n")
		e.out.WriteString("    (func $__wasi_proc_exit (param i32)))\n")
	}
	e.writeProcessImports()
	e.writeFSImports()
	pages := (e.dataEnd + 65535) / 65536
	if pages < 1 {
		pages = 1
	}
	fmt.Fprintf(&e.out, "  (memory (export \"memory\") %d)\n", pages)
	if e.usesAllocatorRuntime() {
		fmt.Fprintf(&e.out, "  (global $__heap_end (mut i32) (i32.const %d))\n", e.dataEnd)
		e.out.WriteString("  (global $__free_head (mut i32) (i32.const 0))\n")
	} else {
		fmt.Fprintf(&e.out, "  (global $__stack_pointer (mut i32) (i32.const %d))\n", e.dataEnd)
	}
	if e.usesArenaOriginRuntime() {
		e.out.WriteString("  (global $__arena_instances (mut i64) (i64.const 0))\n")
	}
	e.writeProcessGlobals()
	e.writeFunctionTable()
	for _, lit := range e.dataOrder {
		ref := e.strings[lit]
		fmt.Fprintf(&e.out, "  (data (i32.const %d) \"%s\")\n", ref.offset, dataLiteral(lit))
	}
	e.writeEnumPrintTables()
	e.writeMainErrorTable()
	e.out.WriteByte('\n')
}

// writeFunctionTable writes the `(type ...)` declarations `call_indirect`
// names and the table the addresses index into. Both are empty when no
// address is taken, so a module that uses no function pointer is unchanged.
func (e *emitter) writeFunctionTable() {
	for index, sig := range e.signatures {
		params := ""
		for _, param := range sig.params {
			params += " (param " + param + ")"
		}
		result := ""
		if sig.result != "" {
			result = " (result " + sig.result + ")"
		}
		fmt.Fprintf(&e.out, "  (type $sig%d (func%s%s))\n", index, params, result)
	}
	if len(e.table) == 0 {
		return
	}
	fmt.Fprintf(&e.out, "  (table %d funcref)\n", len(e.table))
	e.out.WriteString("  (elem (i32.const 0)")
	for _, name := range e.table {
		fmt.Fprintf(&e.out, " $%s", name)
	}
	e.out.WriteString(")\n")
}

// dataLiteral converts a map key to WAT data text.
func dataLiteral(key string) string {
	if text, ok := panicDataText(key); ok {
		return stringBytes(text)
	}
	if text, ok := strings.CutPrefix(key, enumPrintDataPrefix); ok {
		return stringBytes(text)
	}
	if text, ok := strings.CutPrefix(key, errorNameDataPrefix); ok {
		return stringBytes(text)
	}
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

// writeRuntime writes the minimal WASI output and checked-failure helpers.
func (e *emitter) writeRuntime() error {
	if e.usesAllocatorRuntime() {
		e.writeAllocatorRuntime()
	}
	if e.usesMapRuntime() {
		e.writeMapRuntime()
	}
	if e.usesArenaOriginRuntime() {
		e.writeArenaRuntime()
	}
	e.writeStackAllocHelper()
	e.writeBytesHelper()
	e.writeLineHelper()
	e.writeBoolHelper()
	e.writeIntValueHelper()
	e.writeIntHelper()
	if len(e.enumTableOrder) > 0 {
		e.writeEnumPrintHelper()
	}
	if e.usesByteEqualityRuntime() {
		e.writeByteEqualityHelper()
	}
	if err := e.writeIORuntime(); err != nil {
		return err
	}
	if err := e.writeFSRuntime(); err != nil {
		return err
	}
	if len(e.panicKinds) > 0 {
		e.writePanicRuntime()
	}
	if e.needsMainExitBoundary() {
		e.writeMainErrorRuntime()
	}
	return e.writeProcessRuntime()
}

// writeStackAllocHelper reserves one recursive-safe linear-memory frame,
// growing memory by whole pages before a store can cross its end.
func (e *emitter) writeStackAllocHelper() {
	if e.usesAllocatorRuntime() {
		e.out.WriteString("  (func $__stack_alloc (param $size i32) (result i32)\n")
		e.out.WriteString("    (local $base i32)\n")
		e.out.WriteString("    (local.set $base (call $__page_alloc (local.get $size)))\n")
		e.out.WriteString("    (if (i32.eqz (local.get $base)) (then (unreachable)))\n")
		e.out.WriteString("    (local.get $base)\n")
		e.out.WriteString("  )\n\n")
		e.out.WriteString("  (func $__stack_free (param $base i32) (param $size i32)\n")
		e.out.WriteString("    (call $__page_free (local.get $base) (local.get $size))\n")
		e.out.WriteString("  )\n\n")
		return
	}
	e.out.WriteString("  (func $__stack_alloc (param $size i32) (result i32)\n")
	e.out.WriteString("    (local $base i32) (local $end i32) (local $pages i32)\n")
	e.out.WriteString("    (local.set $base (global.get $__stack_pointer))\n")
	e.out.WriteString("    (local.set $end (i32.add (local.get $base) (local.get $size)))\n")
	e.out.WriteString("    (if (i32.lt_u (local.get $end) (local.get $base)) (then (unreachable)))\n")
	e.out.WriteString("    (if (i32.gt_u (local.get $end)\n")
	e.out.WriteString("        (i32.shl (memory.size) (i32.const 16)))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $pages\n")
	e.out.WriteString("          (i32.sub\n")
	e.out.WriteString("            (i32.shr_u (i32.add (local.get $end) " +
		"(i32.const 65535)) (i32.const 16))\n")
	e.out.WriteString("            (memory.size)))\n")
	e.out.WriteString("        (if (i32.eq (memory.grow (local.get $pages)) (i32.const -1))\n")
	e.out.WriteString("          (then (unreachable)))))\n")
	e.out.WriteString("    (global.set $__stack_pointer (local.get $end))\n")
	e.out.WriteString("    (local.get $base)\n")
	e.out.WriteString("  )\n\n")
}

// writeBytesHelper writes one byte range to a selected WASI descriptor.
func (e *emitter) writeBytesHelper() {
	e.out.WriteString("  (func $__write_bytes (param $fd i32) (param $ptr i32) (param $len i32)\n")
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) (local.get $ptr))\n", scratchOffset)
	fmt.Fprintf(&e.out, "    (i32.store (i32.const %d) (local.get $len))\n", scratchOffset+4)
	e.out.WriteString("    (drop (call $__wasi_fd_write\n")
	fmt.Fprintf(&e.out, "      (local.get $fd) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	e.out.WriteString("  )\n\n")
}

// writeLineHelper writes a buffer and a trailing newline to stdout.
func (e *emitter) writeLineHelper() {
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__write_line (param $ptr i32) (param $len i32)\n")
	e.out.WriteString("    (call $__write_bytes (i32.const 1) (local.get $ptr) (local.get $len))\n")
	fmt.Fprintf(&e.out,
		"    (call $__write_bytes (i32.const 1) (i32.const %d) (i32.const 1))\n",
		newline.offset)
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

// writeIntValueHelper writes one signed i64 without a trailing newline.
func (e *emitter) writeIntValueHelper() {
	e.out.WriteString("  (func $__write_i64 (param $fd i32) (param $value i64)\n")
	e.out.WriteString("    (local $n i64) (local $pos i32) (local $negative i32)\n")
	e.out.WriteString("    (local.set $n (local.get $value))\n")
	e.out.WriteString("    (local.set $pos (i32.const 192))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $n))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (i32.store8 (i32.const 191) (i32.const 48))\n")
	e.out.WriteString("        (call $__write_bytes (local.get $fd) (i32.const 191) (i32.const 1))\n")
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
	fmt.Fprintf(&e.out, "    (call $__write_bytes (local.get $fd) (local.get $pos) ")
	fmt.Fprintf(&e.out, "(i32.sub (i32.const %d) ", intBufferEnd)
	e.out.WriteString("(local.get $pos)))\n")
	e.out.WriteString("  )\n\n")
}

// writeIntHelper writes signed i64 values as one stdout line.
func (e *emitter) writeIntHelper() {
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__print_i64 (param $value i64)\n")
	e.out.WriteString("    (call $__write_i64 (i32.const 1) (local.get $value))\n")
	fmt.Fprintf(&e.out,
		"    (call $__write_bytes (i32.const 1) (i32.const %d) (i32.const 1))\n",
		newline.offset)
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
	e.currentReturn = fn.Return
	defer func() { e.currentReturn = "" }()
	frame, err := e.planFrame(fn)
	if err != nil {
		return fmt.Errorf("wasm error: function `%s`: %w", fn.Name, err)
	}
	e.frame = frame
	params := e.functionParams(fn)
	if err := e.registerFrameValues(fn); err != nil {
		return fmt.Errorf("wasm error: function `%s`: %w", fn.Name, err)
	}
	fmt.Fprintf(&e.out, "  (func $%s %s%s\n", fn.Name, params, e.functionResult(fn.Return))
	e.writeLocals(fn)
	if frame.size > 0 {
		e.out.WriteString("    (local $__kizu_frame i32)\n")
	}
	e.out.WriteString("    (local $pc i32)\n")
	if frame.size > 0 {
		fmt.Fprintf(&e.out,
			"    (local.set $__kizu_frame (call $__stack_alloc (i32.const %d)))\n",
			frame.size,
		)
	}
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
	if frame.size > 0 {
		if !e.usesAllocatorRuntime() {
			e.out.WriteString("    (global.set $__stack_pointer (local.get $__kizu_frame))\n")
		}
	}
	if fn.Return != "void" && !e.isMemoryType(fn.Return) {
		e.out.WriteString("    (unreachable)\n")
	}
	e.out.WriteString("  )\n\n")
	e.frame = nil
	return nil
}

// functionParams writes WebAssembly parameters and records their values.
func (e *emitter) functionParams(fn *ir.Function) string {
	params := make([]string, 0, len(fn.Params)+1)
	if e.isMemoryType(fn.Return) {
		params = append(params, "(param $__kizu_result i32)")
	}
	for _, param := range fn.Params {
		name := symbolName(param.Name)
		params = append(params, fmt.Sprintf("(param %s %s)", name, e.wasmType(param.Type)))
		e.values[param.Name] = valueInfo{expr: fmt.Sprintf("(local.get %s)", name)}
	}
	return strings.Join(params, " ")
}

// functionResult returns the WebAssembly result declaration.
func (e *emitter) functionResult(typ string) string {
	if typ == "void" || e.isMemoryType(typ) {
		return ""
	}
	return " (result " + e.wasmType(typ) + ")"
}

// writeLocals declares SSA locals that must live across blocks.
func (e *emitter) writeLocals(fn *ir.Function) {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if needsLocal(instr) {
				fmt.Fprintf(&e.out, "    (local %s %s)\n",
					symbolName(instr.Result.Name), e.wasmType(instr.Result.Type))
			}
		}
	}
}

// needsLocal reports whether an instruction result needs a WebAssembly local.
func needsLocal(instr *ir.Instr) bool {
	return instr.Result.Type != "" && instr.Result.Type != "void" &&
		!(instr.Op == "const" && instr.Result.Type == "[]u8")
}

// dispatchBlock is the block one name reaches and the id its dispatch arm
// tests for.
type dispatchBlock struct {
	block *ir.Block
	id    int
}

// blockIndexes maps block names to their dispatch arms. The map is one
// function's, because a block name is unique only inside the function that
// declares it: lowering numbers each function's blocks from scratch, so
// `entry` and `while.header.1` name a block in every function that has one.
func blockIndexes(fn *ir.Function) map[string]dispatchBlock {
	index := map[string]dispatchBlock{}
	for i, block := range fn.Blocks {
		index[block.Name] = dispatchBlock{block: block, id: i}
	}
	return index
}
