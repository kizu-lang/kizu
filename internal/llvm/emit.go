package llvm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"tiny-safe/internal/ir"
)

// Emit formats a typed SSA IR module as LLVM IR.
func Emit(module *ir.Module) (string, error) {
	e := &emitter{
		module:  module,
		strings: map[string]string{},
		values:  map[string]valueInfo{},
	}
	if err := e.emit(); err != nil {
		return "", err
	}
	return strings.TrimRight(e.out.String(), "\n"), nil
}

type emitter struct {
	module  *ir.Module
	out     bytes.Buffer
	strings map[string]string
	values  map[string]valueInfo
}

type valueInfo struct {
	typ     string
	operand string
	length  int
}

// emit writes declarations and function definitions.
func (e *emitter) emit() error {
	e.collectStrings()
	e.writeHeader()
	for _, fn := range e.module.Functions {
		if err := e.writeFunction(fn); err != nil {
			return err
		}
	}
	return nil
}

// collectStrings assigns stable global names to string constants.
func (e *emitter) collectStrings() {
	next := 0
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "const" && instr.Result.Type == "string" {
					if _, ok := e.strings[instr.Immediate]; !ok {
						e.strings[instr.Immediate] = fmt.Sprintf("@.str.%d", next)
						next++
					}
				}
			}
		}
	}
}

// writeHeader writes globals and runtime declarations.
func (e *emitter) writeHeader() {
	e.out.WriteString("; Kizu LLVM IR\n")
	for _, lit := range e.sortedStringLiterals() {
		name := e.strings[lit]
		unquoted, _ := strconv.Unquote(lit)
		fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n",
			name, len(unquoted)+1, escapeString(unquoted))
	}
	if len(e.strings) > 0 {
		e.out.WriteByte('\n')
	}
	e.out.WriteString("declare void @kizu_print_string(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_print_int(i64)\n")
	e.out.WriteString("declare void @kizu_print_bool(i1)\n\n")
}

// sortedStringLiterals returns string constants in global-name order.
func (e *emitter) sortedStringLiterals() []string {
	literals := make([]string, 0, len(e.strings))
	for lit := range e.strings {
		literals = append(literals, lit)
	}
	sort.Slice(literals, func(i int, j int) bool {
		return e.strings[literals[i]] < e.strings[literals[j]]
	})
	return literals
}

// writeFunction writes one LLVM function.
func (e *emitter) writeFunction(fn *ir.Function) error {
	e.values = map[string]valueInfo{}
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, llvmType(param.Type)+" "+param.Name)
		e.values[param.Name] = valueInfo{typ: param.Type, operand: param.Name}
	}
	fmt.Fprintf(&e.out, "define %s @%s(%s) {\n",
		llvmType(fn.Return), fn.Name, strings.Join(params, ", "))
	for _, block := range fn.Blocks {
		if err := e.writeBlock(block); err != nil {
			return err
		}
	}
	e.out.WriteString("}\n\n")
	return nil
}

// writeBlock writes one LLVM basic block.
func (e *emitter) writeBlock(block *ir.Block) error {
	fmt.Fprintf(&e.out, "%s:\n", block.Name)
	for _, instr := range block.Instrs {
		if err := e.writeInstr(instr); err != nil {
			return err
		}
	}
	return e.writeTerminator(block.Terminator)
}

// writeInstr writes one LLVM instruction.
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
	case instr.Op == "phi":
		return e.writePhi(instr)
	case instr.Op == "struct.new", strings.HasPrefix(instr.Op, "field."):
		return e.writeOpaqueValue(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" || instr.Op == "arena.get":
		return e.writeOpaqueValue(instr)
	case instr.Op == "error.error" || instr.Op == "error.try":
		return e.writeOpaqueValue(instr)
	default:
		return fmt.Errorf("llvm error: unsupported instruction `%s`", instr.Op)
	}
}

// writeConst writes scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	switch instr.Result.Type {
	case "i64":
		e.values[instr.Result.Name] = valueInfo{typ: "i64", operand: instr.Immediate}
	case "bool":
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: llvmBool(instr.Immediate)}
	case "string":
		unquoted, _ := strconv.Unquote(instr.Immediate)
		global := e.strings[instr.Immediate]
		fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
			instr.Result.Name, len(unquoted)+1, global)
		e.values[instr.Result.Name] = valueInfo{
			typ: "string", operand: instr.Result.Name, length: len(unquoted),
		}
	default:
		return fmt.Errorf("llvm error: unsupported const type `%s`", instr.Result.Type)
	}
	return nil
}

// writeBinary writes arithmetic and comparison instructions.
func (e *emitter) writeBinary(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: binary expects 2 args")
	}
	left := e.value(instr.Args[0])
	right := e.value(instr.Args[1])
	op := strings.TrimPrefix(instr.Op, "binary.")
	if instr.Result.Type == "bool" {
		pred := llvmPredicate(op)
		fmt.Fprintf(&e.out, "  %s = icmp %s i64 %s, %s\n",
			instr.Result.Name, pred, left.operand, right.operand)
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: instr.Result.Name}
		return nil
	}
	fmt.Fprintf(&e.out, "  %s = %s i64 %s, %s\n",
		instr.Result.Name, llvmBinaryOp(op), left.operand, right.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: instr.Result.Name}
	return nil
}

// writeCall writes runtime print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "call.")
	if name == "print" {
		return e.writePrint(instr.Args)
	}
	args := make([]string, 0, len(instr.Args))
	for _, arg := range instr.Args {
		value := e.value(arg)
		args = append(args, llvmType(arg.Type)+" "+value.operand)
	}
	call := fmt.Sprintf("call %s @%s(%s)", llvmType(instr.Result.Type), name, strings.Join(args, ", "))
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "  %s\n", call)
		return nil
	}
	fmt.Fprintf(&e.out, "  %s = %s\n", instr.Result.Name, call)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: instr.Result.Name}
	return nil
}

// writeCast emits a no-op value conversion for the Phase 16 low-level subset.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: cast expects 1 arg")
	}
	value := e.value(instr.Args[0])
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: value.operand}
	return nil
}

// writePrint writes calls to the Kizu runtime print ABI.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("llvm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "string":
		fmt.Fprintf(&e.out, "  call void @kizu_print_string(ptr %s, i64 %d)\n",
			value.operand, value.length)
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		fmt.Fprintf(&e.out, "  call void @kizu_print_int(i64 %s)\n", value.operand)
	case "bool":
		fmt.Fprintf(&e.out, "  call void @kizu_print_bool(i1 %s)\n", value.operand)
	default:
		fmt.Fprintf(&e.out, "  ; unsupported print type %s\n", args[0].Type)
	}
	return nil
}

// writePhi writes an LLVM phi instruction.
func (e *emitter) writePhi(instr *ir.Instr) error {
	parts := make([]string, 0, len(instr.Incoming))
	for _, incoming := range instr.Incoming {
		value := e.value(incoming.Value)
		parts = append(parts, fmt.Sprintf("[ %s, %%%s ]", value.operand, incoming.Block))
	}
	fmt.Fprintf(&e.out, "  %s = phi %s %s\n",
		instr.Result.Name, llvmType(instr.Result.Type), strings.Join(parts, ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: instr.Result.Name}
	return nil
}

// writeOpaqueValue represents values not lowered to concrete LLVM layout in phase 9.
func (e *emitter) writeOpaqueValue(instr *ir.Instr) error {
	fmt.Fprintf(&e.out, "  ; %s omitted in phase 9\n", instr.Op)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
	return nil
}

// writeTerminator writes one LLVM terminator.
func (e *emitter) writeTerminator(term ir.Terminator) error {
	switch term.Op {
	case "return":
		if term.Value.Type == "void" {
			e.out.WriteString("  ret void\n")
			return nil
		}
		value := e.value(term.Value)
		fmt.Fprintf(&e.out, "  ret %s %s\n", llvmType(term.Value.Type), value.operand)
	case "jump":
		fmt.Fprintf(&e.out, "  br label %%%s\n", term.Target)
	case "branch":
		cond := e.value(term.Cond)
		fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n",
			cond.operand, term.Target, term.Else)
	default:
		return fmt.Errorf("llvm error: unsupported terminator `%s`", term.Op)
	}
	return nil
}

// value resolves an SSA value to a LLVM operand.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	return valueInfo{typ: value.Type, operand: value.Name}
}
