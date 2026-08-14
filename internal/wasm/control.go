package wasm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeBlock writes a dispatch arm for one IR block.
func (e *emitter) writeBlock(block *ir.Block, index map[string]int, id int) error {
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
	case strings.HasPrefix(instr.Op, "call."):
		return e.writeCall(instr)
	case instr.Op == "cast":
		return e.writeCast(instr)
	case instr.Op == "struct.new", strings.HasPrefix(instr.Op, "field."),
		instr.Op == "ref.store":
		return e.writeUnsupportedOpaque(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" ||
		instr.Op == "arena.get" || instr.Op == "arena.deinit":
		return e.writeUnsupportedOpaque(instr)
	case instr.Op == "error.error" || instr.Op == "error.try":
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
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, expr: value.expr}
	return nil
}

// writeConst records scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	if isIntegerType(instr.Result.Type) {
		// Every scalar integer is one wasm i64, so a constant of any width is
		// written the same way.
		e.values[instr.Result.Name] = valueInfo{
			typ:  instr.Result.Type,
			expr: "(i64.const " + instr.Immediate + ")",
		}
		return nil
	}
	switch instr.Result.Type {
	case "bool":
		e.values[instr.Result.Name] = valueInfo{typ: "bool", expr: wasmBool(instr.Immediate)}
	case "[]u8":
		ref := e.strings[instr.Immediate]
		e.values[instr.Result.Name] = valueInfo{
			typ: "[]u8", expr: fmt.Sprintf("(i32.const %d)", ref.offset), length: ref.length,
		}
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
		typ: instr.Result.Type, expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeCall writes builtin print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "call.")
	if name == "print" {
		return e.writePrint(instr.Args)
	}
	args := make([]string, 0, len(instr.Args))
	for _, arg := range instr.Args {
		args = append(args, e.value(arg).expr)
	}
	call := fmt.Sprintf("(call $%s %s)", name, strings.Join(args, " "))
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "            %s\n", call)
		return nil
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbolName(instr.Result.Name), call)
	e.values[instr.Result.Name] = valueInfo{
		typ: instr.Result.Type, expr: "(local.get " + symbolName(instr.Result.Name) + ")",
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
		fmt.Fprintf(&e.out, "            (call $__write_line %s (i32.const %d))\n",
			value.expr, value.length)
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
func (e *emitter) writeTerminator(block *ir.Block, index map[string]int) error {
	switch block.Terminator.Op {
	case "return":
		return e.writeReturn(block.Terminator.Value)
	case "jump":
		e.writeJump(block, block.Terminator.Target, index)
	case "branch":
		e.writeBranch(block, index)
	default:
		return fmt.Errorf("wasm error: unsupported terminator `%s`", block.Terminator.Op)
	}
	return nil
}

// writeReturn writes a function return or exits a void function.
func (e *emitter) writeReturn(value ir.Value) error {
	if value.Type == "void" {
		e.out.WriteString("            (br $exit)\n")
		return nil
	}
	fmt.Fprintf(&e.out, "            (return %s)\n", e.value(value).expr)
	return nil
}

// writeJump writes an unconditional dispatch jump.
func (e *emitter) writeJump(block *ir.Block, target string, index map[string]int) {
	e.writePhiCopies(block.Name, target)
	fmt.Fprintf(&e.out, "            (local.set $pc (i32.const %d))\n", index[target])
	e.out.WriteString("            (br $dispatch)\n")
}

// writeBranch writes a conditional dispatch jump.
func (e *emitter) writeBranch(block *ir.Block, index map[string]int) {
	term := block.Terminator
	e.out.WriteString("            (if " + e.value(term.Cond).expr + "\n")
	e.out.WriteString("              (then\n")
	e.writePhiCopies(block.Name, term.Target)
	fmt.Fprintf(&e.out, "                (local.set $pc (i32.const %d))\n", index[term.Target])
	e.out.WriteString("                (br $dispatch))\n")
	e.out.WriteString("              (else\n")
	e.writePhiCopies(block.Name, term.Else)
	fmt.Fprintf(&e.out, "                (local.set $pc (i32.const %d))\n", index[term.Else])
	e.out.WriteString("                (br $dispatch)))\n")
}

// writePhiCopies assigns target phi locals for an edge.
func (e *emitter) writePhiCopies(source string, target string) {
	block := e.findBlock(target)
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
	e.values[dst.Name] = valueInfo{typ: dst.Type, expr: "(local.get " + symbolName(dst.Name) + ")"}
}

// findBlock returns the block with the given name.
func (e *emitter) findBlock(name string) *ir.Block {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			if block.Name == name {
				return block
			}
		}
	}
	return nil
}

// value resolves a typed IR value to a WebAssembly expression.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	if _, err := strconv.Atoi(value.Name); err == nil {
		return valueInfo{typ: value.Type, expr: "(i64.const " + value.Name + ")"}
	}
	return valueInfo{typ: value.Type, expr: "(local.get " + symbolName(value.Name) + ")"}
}

// wasmBool maps bool constants to WebAssembly i32 constants.
func wasmBool(value string) string {
	if value == "true" {
		return "(i32.const 1)"
	}
	return "(i32.const 0)"
}
