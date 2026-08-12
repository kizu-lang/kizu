package ir

import (
	"bytes"
	"fmt"
	"strings"
)

// Dump formats a module as a stable text representation.
func Dump(module *Module) string {
	var out bytes.Buffer
	for _, fn := range module.Functions {
		writeFunction(&out, fn)
	}
	return strings.TrimRight(out.String(), "\n")
}

// writeFunction writes one function dump.
func writeFunction(out *bytes.Buffer, fn *Function) {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, param.String())
	}
	fmt.Fprintf(out, "fn %s(%s) -> %s {\n", fn.Name, strings.Join(params, ", "), fn.Return)
	for _, block := range fn.Blocks {
		writeBlock(out, block)
	}
	out.WriteString("}\n")
}

// writeBlock writes one basic block.
func writeBlock(out *bytes.Buffer, block *Block) {
	fmt.Fprintf(out, "%s:\n", block.Name)
	for _, instr := range block.Instrs {
		fmt.Fprintf(out, "  %s\n", formatInstr(instr))
	}
	if block.Terminator.Op != "" {
		fmt.Fprintf(out, "  %s\n", formatTerminator(block.Terminator))
	}
}

// formatInstr formats one instruction.
func formatInstr(instr *Instr) string {
	switch instr.Op {
	case "const":
		return fmt.Sprintf("%s = const %s", instr.Result.String(), instr.Immediate)
	case "phi":
		return fmt.Sprintf("%s = phi %s", instr.Result.String(), formatIncoming(instr.Incoming))
	case "struct.new":
		return fmt.Sprintf("%s = struct.new {%s}", instr.Result.String(), formatFields(instr.Fields))
	case "cond_fail":
		return fmt.Sprintf("cond_fail %s, %q", formatValues(instr.Args), instr.Immediate)
	default:
		return formatGenericInstr(instr)
	}
}

// formatGenericInstr formats a normal non-special instruction.
func formatGenericInstr(instr *Instr) string {
	args := formatValues(instr.Args)
	if instr.Immediate != "" {
		if args != "" {
			args += ", "
		}
		args += instr.Immediate
	}
	if len(instr.Cleanups) > 0 {
		if args != "" {
			args += ", "
		}
		args += "cleanup " + formatCleanups(instr.Cleanups)
	}
	if instr.Result.Type == "void" {
		return fmt.Sprintf("%s %s", instr.Op, args)
	}
	if args == "" {
		return fmt.Sprintf("%s = %s", instr.Result.String(), instr.Op)
	}
	return fmt.Sprintf("%s = %s %s", instr.Result.String(), instr.Op, args)
}

// formatCleanups formats deferred cleanups attached to a fallible instruction.
func formatCleanups(cleanups []Cleanup) string {
	parts := make([]string, 0, len(cleanups))
	for _, cleanup := range cleanups {
		parts = append(parts, fmt.Sprintf("%s %s", cleanup.Op, formatValues(cleanup.Args)))
	}
	return strings.Join(parts, "; ")
}

// formatTerminator formats one block terminator.
func formatTerminator(term Terminator) string {
	switch term.Op {
	case "return":
		return "return " + term.Value.String()
	case "jump":
		return "jump " + term.Target
	case "branch":
		return fmt.Sprintf("branch %s, %s, %s", term.Cond.String(), term.Target, term.Else)
	case "unreachable":
		return "unreachable"
	default:
		return term.Op
	}
}

// formatValues formats instruction operands.
func formatValues(values []Value) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.String())
	}
	return strings.Join(parts, ", ")
}

// formatIncoming formats phi incoming edges.
func formatIncoming(incoming []Incoming) string {
	parts := make([]string, 0, len(incoming))
	for _, item := range incoming {
		parts = append(parts, fmt.Sprintf("[%s, %s]", item.Block, item.Value.String()))
	}
	return strings.Join(parts, ", ")
}

// formatFields formats struct field initializers.
func formatFields(fields []FieldArg) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, fmt.Sprintf("%s: %s", field.Name, field.Value.String()))
	}
	return strings.Join(parts, ", ")
}
