package wasm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeBlock writes a dispatch arm for one IR block.
func (e *emitter) writeBlock(block *ir.Block, index map[string]dispatchBlock, id int) error {
	fmt.Fprintf(&e.out, "        (if (i32.eq (local.get $pc) (i32.const %d))\n", id)
	e.out.WriteString("          (then\n")
	for _, instr := range block.Instrs {
		if instr.Op == "phi" {
			continue
		}
		if err := e.writeInstr(instr); err != nil {
			return err
		}
	}
	if err := e.writeTerminator(block, index); err != nil {
		return err
	}
	e.out.WriteString("          )\n")
	e.out.WriteString("        )\n")
	return nil
}

// writeInstr writes one WebAssembly instruction sequence.
func (e *emitter) writeInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "const":
		return e.writeConst(instr)
	case strings.HasPrefix(instr.Op, "binary."):
		return e.writeBinary(instr)
	case strings.HasPrefix(instr.Op, "unary."):
		return e.writeUnary(instr)
	case strings.HasPrefix(instr.Op, "func.addr."), strings.HasPrefix(instr.Op, "call."):
		return e.writeCallableInstr(instr)
	case instr.Op == "cast":
		return e.writeCast(instr)
	default:
		return e.writeMemoryInstr(instr)
	}
}

// writeMemoryInstr writes memory-backed values and the opaque operations that
// remain outside the target subset.
func (e *emitter) writeMemoryInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "struct.new":
		return e.writeStructNew(instr)
	case strings.HasPrefix(instr.Op, "field."):
		return e.writeFieldInstr(instr)
	case instr.Op == "local.slot":
		return e.writeLocalSlot(instr)
	case instr.Op == "ref.store":
		return e.writeRefStore(instr)
	case instr.Op == "ref.load":
		return e.writeRefLoad(instr)
	case strings.HasPrefix(instr.Op, "union."):
		return e.writeUnionInstr(instr)
	case strings.HasPrefix(instr.Op, "slice."):
		return e.writeSliceInstr(instr)
	case strings.HasPrefix(instr.Op, "opt."), strings.HasPrefix(instr.Op, "error."):
		return e.writeTaggedInstr(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" ||
		instr.Op == "arena.at" || instr.Op == "arena.len" ||
		instr.Op == "arena.pop_or_panic" || instr.Op == "arena.deinit":
		return e.writeUnsupportedOpaque(instr)
	default:
		return fmt.Errorf("wasm error: unsupported instruction `%s`", instr.Op)
	}
}

// writeCast records a no-op value conversion for the Phase 16 low-level subset.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: cast expects 1 arg")
	}
	value := e.value(instr.Args[0])
	e.values[instr.Result.Name] = valueInfo{expr: value.expr}
	return nil
}

// writeConst records scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	if isIntegerType(instr.Result.Type) || e.isTagType(instr.Result.Type) {
		// Every scalar integer is one wasm i64, so a constant of any width is
		// written the same way.
		e.values[instr.Result.Name] = valueInfo{
			expr: "(i64.const " + instr.Immediate + ")",
		}
		return nil
	}
	switch instr.Result.Type {
	case "bool":
		e.values[instr.Result.Name] = valueInfo{expr: wasmBool(instr.Immediate)}
	case "[]u8":
		ref := e.strings[instr.Immediate]
		slot, err := e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.out, "            (i32.store %s (i32.const %d))\n", slot, ref.offset)
		fmt.Fprintf(&e.out, "            (i32.store %s (i32.const %d))\n",
			addressAt(slot, 4), ref.length)
		e.values[instr.Result.Name] = valueInfo{expr: slot}
	default:
		return fmt.Errorf("wasm error: unsupported const type `%s`", instr.Result.Type)
	}
	return nil
}

// writeBinary writes arithmetic and comparison local assignments.
func (e *emitter) writeBinary(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: binary expects 2 args")
	}
	left := e.value(instr.Args[0]).expr
	right := e.value(instr.Args[1]).expr
	op := strings.TrimPrefix(instr.Op, "binary.")
	wasmOp := wasmBinaryOp(op)
	if instr.Result.Type == "bool" {
		wasmOp = wasmCompareOp(op)
	}
	fmt.Fprintf(&e.out, "            (local.set %s (%s %s %s))\n",
		symbolName(instr.Result.Name), wasmOp, left, right)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeUnary writes boolean negation and integer arithmetic negation.
func (e *emitter) writeUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: unary expects 1 arg")
	}
	value := e.value(instr.Args[0]).expr
	var expr string
	switch strings.TrimPrefix(instr.Op, "unary.") {
	case "!":
		if instr.Result.Type != "bool" {
			return fmt.Errorf("wasm error: unary ! expects bool")
		}
		expr = "(i32.eqz " + value + ")"
	case "-":
		if !isIntegerType(instr.Result.Type) {
			return fmt.Errorf("wasm error: unary - expects integer")
		}
		expr = "(i64.sub (i64.const 0) " + value + ")"
	default:
		return fmt.Errorf("wasm error: unsupported unary `%s`", instr.Op)
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeCallableInstr writes the instructions that name or reach a function:
// its table index, a call through one, and a call by name.
func (e *emitter) writeCallableInstr(instr *ir.Instr) error {
	if name, ok := strings.CutPrefix(instr.Op, "func.addr."); ok {
		return e.writeFuncAddr(name, instr)
	}
	if instr.Op == "call.indirect" {
		return e.writeIndirectCall(instr)
	}
	return e.writeCall(instr)
}

// writeFuncAddr writes the table index a function name holds. wasm has no
// address for a function, so a pointer is the position the header's `elem`
// gave it.
func (e *emitter) writeFuncAddr(name string, instr *ir.Instr) error {
	index, ok := e.tableIndex[name]
	if !ok {
		return fmt.Errorf("wasm error: `%s` has no table entry", name)
	}
	e.values[instr.Result.Name] = valueInfo{
		expr: fmt.Sprintf("(i32.const %d)", index),
	}
	return nil
}

// writeIndirectCall writes a call through a function pointer. The callee is
// the first operand and reaches wasm as the table index it lowered to, which
// `call_indirect` takes last.
func (e *emitter) writeIndirectCall(instr *ir.Instr) error {
	if len(instr.Args) == 0 {
		return fmt.Errorf("wasm error: call.indirect expects a callee")
	}
	args := make([]string, 0, len(instr.Args)+1)
	var resultSlot string
	if e.isMemoryType(instr.Result.Type) {
		var err error
		resultSlot, err = e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		args = append(args, resultSlot)
	}
	for _, arg := range instr.Args[1:] {
		args = append(args, e.value(arg).expr)
	}
	args = append(args, e.value(instr.Args[0]).expr)
	call := fmt.Sprintf("(call_indirect (type $sig%d) %s)",
		e.internSignature(instr), strings.Join(args, " "))
	if instr.Result.Type == "void" || e.isMemoryType(instr.Result.Type) {
		fmt.Fprintf(&e.out, "            %s\n", call)
		if e.isMemoryType(instr.Result.Type) {
			e.values[instr.Result.Name] = valueInfo{expr: resultSlot}
		}
		return nil
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n",
		symbolName(instr.Result.Name), call)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeCall writes builtin print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "call.")
	if name == "print" {
		return e.writePrint(instr.Args)
	}
	args := make([]string, 0, len(instr.Args)+1)
	var resultSlot string
	if e.isMemoryType(instr.Result.Type) {
		var err error
		resultSlot, err = e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		args = append(args, resultSlot)
	}
	for _, arg := range instr.Args {
		args = append(args, e.value(arg).expr)
	}
	call := fmt.Sprintf("(call $%s %s)", name, strings.Join(args, " "))
	if instr.Result.Type == "void" || e.isMemoryType(instr.Result.Type) {
		fmt.Fprintf(&e.out, "            %s\n", call)
		if e.isMemoryType(instr.Result.Type) {
			e.values[instr.Result.Name] = valueInfo{expr: resultSlot}
		}
		return nil
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbolName(instr.Result.Name), call)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writePrint writes calls to WASI stdout helpers.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("wasm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "[]u8":
		fmt.Fprintf(&e.out, "            (call $__write_line (i32.load %s) (i32.load %s))\n",
			value.expr, addressAt(value.expr, 4))
	case "i64":
		fmt.Fprintf(&e.out, "            (call $__print_i64 %s)\n", value.expr)
	case "bool":
		fmt.Fprintf(&e.out, "            (call $__print_bool %s)\n", value.expr)
	default:
		return fmt.Errorf("wasm error: unsupported print type `%s`", args[0].Type)
	}
	return nil
}

// writeUnsupportedOpaque marks values that are not part of the phase 11 target subset.
func (e *emitter) writeUnsupportedOpaque(instr *ir.Instr) error {
	return fmt.Errorf("wasm error: `%s` is outside the phase 11 target subset", instr.Op)
}

// writeTerminator writes control transfer for one dispatch arm.
func (e *emitter) writeTerminator(block *ir.Block, index map[string]dispatchBlock) error {
	switch block.Terminator.Op {
	case "return":
		return e.writeReturn(block.Terminator.Value)
	case "jump":
		e.writeJump(block, block.Terminator.Target, index)
	case "branch":
		e.writeBranch(block, index)
	case "unreachable":
		e.out.WriteString("            (unreachable)\n")
	default:
		return fmt.Errorf("wasm error: unsupported terminator `%s`", block.Terminator.Op)
	}
	return nil
}

// writeReturn writes a function return or exits a void function.
func (e *emitter) writeReturn(value ir.Value) error {
	if value.Type == "void" {
		e.restoreFrame()
		e.out.WriteString("            (br $exit)\n")
		return nil
	}
	if e.isMemoryType(value.Type) {
		layout, err := e.typeLayout(value.Type)
		if err != nil {
			return err
		}
		e.writeMemoryCopy("(local.get $__kizu_result)", e.value(value).expr, layout.size)
		e.restoreFrame()
		e.out.WriteString("            (br $exit)\n")
		return nil
	}
	e.restoreFrame()
	fmt.Fprintf(&e.out, "            (return %s)\n", e.value(value).expr)
	return nil
}

// restoreFrame releases this invocation's fixed frame before any return.
func (e *emitter) restoreFrame() {
	if e.frame != nil && e.frame.size > 0 {
		e.out.WriteString("            (global.set $__stack_pointer (local.get $__kizu_frame))\n")
	}
}

// writeJump writes an unconditional dispatch jump.
func (e *emitter) writeJump(block *ir.Block, target string, index map[string]dispatchBlock) {
	e.writePhiCopies(block.Name, target, index)
	fmt.Fprintf(&e.out, "            (local.set $pc (i32.const %d))\n", index[target].id)
	e.out.WriteString("            (br $dispatch)\n")
}

// writeBranch writes a conditional dispatch jump.
func (e *emitter) writeBranch(block *ir.Block, index map[string]dispatchBlock) {
	term := block.Terminator
	e.out.WriteString("            (if " + e.value(term.Cond).expr + "\n")
	e.out.WriteString("              (then\n")
	e.writePhiCopies(block.Name, term.Target, index)
	fmt.Fprintf(&e.out, "                (local.set $pc (i32.const %d))\n", index[term.Target].id)
	e.out.WriteString("                (br $dispatch))\n")
	e.out.WriteString("              (else\n")
	e.writePhiCopies(block.Name, term.Else, index)
	fmt.Fprintf(&e.out, "                (local.set $pc (i32.const %d))\n", index[term.Else].id)
	e.out.WriteString("                (br $dispatch)))\n")
}

// writePhiCopies assigns target phi locals for an edge. The edge stays inside
// one function, so the target is read out of that function's dispatch map.
func (e *emitter) writePhiCopies(source string, target string, index map[string]dispatchBlock) {
	block := index[target].block
	if block == nil {
		return
	}
	for _, instr := range block.Instrs {
		if instr.Op != "phi" {
			continue
		}
		for _, incoming := range instr.Incoming {
			if incoming.Block == source {
				e.writeLocalCopy(instr.Result, incoming.Value, "            ")
			}
		}
	}
}

// writeLocalCopy copies one value into a local.
func (e *emitter) writeLocalCopy(dst ir.Value, src ir.Value, indent string) {
	value := e.value(src)
	fmt.Fprintf(&e.out, "%s(local.set %s %s)\n", indent, symbolName(dst.Name), value.expr)
	e.values[dst.Name] = valueInfo{expr: "(local.get " + symbolName(dst.Name) + ")"}
}

// value resolves a typed IR value to a WebAssembly expression.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	if _, err := strconv.Atoi(value.Name); err == nil {
		return valueInfo{expr: "(i64.const " + value.Name + ")"}
	}
	return valueInfo{expr: "(local.get " + symbolName(value.Name) + ")"}
}

// wasmBool maps bool constants to WebAssembly i32 constants.
func wasmBool(value string) string {
	if value == "true" {
		return "(i32.const 1)"
	}
	return "(i32.const 0)"
}
