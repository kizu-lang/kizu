package llvm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
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
	module         *ir.Module
	out            bytes.Buffer
	strings        map[string]string
	values         map[string]valueInfo
	mainReturnsInt bool
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
				if instr.Op == "const" && instr.Result.Type == "[]u8" {
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
	e.writeStructTypes()
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

// writeStructTypes writes named LLVM aggregate definitions for declared structs.
func (e *emitter) writeStructTypes() {
	names := e.sortedStructNames()
	for _, name := range names {
		st := e.module.Structs[name]
		fields := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			fields = append(fields, e.llvmType(field.Type))
		}
		fmt.Fprintf(&e.out, "%s = type { %s }\n",
			llvmStructTypeName(name), strings.Join(fields, ", "))
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
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

// sortedStructNames returns declared structs in stable order.
func (e *emitter) sortedStructNames() []string {
	names := make([]string, 0, len(e.module.Structs))
	for name := range e.module.Structs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writeFunction writes one LLVM function.
func (e *emitter) writeFunction(fn *ir.Function) error {
	e.values = map[string]valueInfo{}
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, e.llvmType(param.Type)+" "+param.Name)
		e.values[param.Name] = valueInfo{typ: param.Type, operand: param.Name}
	}
	returnType := e.llvmType(fn.Return)
	e.mainReturnsInt = fn.Name == "main" && fn.Return == "void"
	if e.mainReturnsInt {
		returnType = "i32"
	}
	fmt.Fprintf(&e.out, "define %s @%s(%s) {\n", returnType, fn.Name, strings.Join(params, ", "))
	for _, block := range fn.Blocks {
		if err := e.writeBlock(block); err != nil {
			return err
		}
	}
	e.out.WriteString("}\n\n")
	e.mainReturnsInt = false
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
	case instr.Op == "struct.new":
		return e.writeStructNew(instr)
	case strings.HasPrefix(instr.Op, "field."):
		return e.writeField(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" ||
		instr.Op == "arena.get" || instr.Op == "arena.deinit":
		return e.unsupported(instr)
	case instr.Op == "error.error" || instr.Op == "error.try":
		return e.unsupported(instr)
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
	case "[]u8":
		unquoted, _ := strconv.Unquote(instr.Immediate)
		global := e.strings[instr.Immediate]
		name := localName(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
			name, len(unquoted)+1, global)
		e.values[instr.Result.Name] = valueInfo{
			typ: "[]u8", operand: name, length: len(unquoted),
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
		name := localName(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = icmp %s i64 %s, %s\n",
			name, pred, left.operand, right.operand)
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: name}
		return nil
	}
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s i64 %s, %s\n", name, llvmBinaryOp(op), left.operand, right.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
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
		args = append(args, e.llvmType(arg.Type)+" "+value.operand)
	}
	call := fmt.Sprintf(
		"call %s @%s(%s)",
		e.llvmType(instr.Result.Type),
		name,
		strings.Join(args, ", "),
	)
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "  %s\n", call)
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", resultName, call)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
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

// writeStructNew lowers a checked struct literal to an LLVM aggregate value.
func (e *emitter) writeStructNew(instr *ir.Instr) error {
	st, ok := e.module.Structs[instr.Result.Type]
	if !ok {
		return fmt.Errorf("llvm error: unknown struct type `%s`", instr.Result.Type)
	}
	values := map[string]ir.Value{}
	for _, field := range instr.Fields {
		if _, ok := structFieldIndex(st, field.Name); !ok {
			return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, field.Name)
		}
		if _, exists := values[field.Name]; exists {
			return fmt.Errorf("llvm error: duplicate struct field `%s.%s`", st.Name, field.Name)
		}
		values[field.Name] = field.Value
	}
	structType := e.llvmType(instr.Result.Type)
	aggregate := "zeroinitializer"
	resultName := localName(instr.Result.Name)
	for index, field := range st.Fields {
		value, ok := values[field.Name]
		if !ok {
			return fmt.Errorf("llvm error: missing struct field `%s.%s`", st.Name, field.Name)
		}
		if value.Type != field.Type {
			return fmt.Errorf(
				"llvm error: struct field `%s.%s` expects %s, got %s",
				st.Name,
				field.Name,
				field.Type,
				value.Type,
			)
		}
		fieldValue := e.value(value)
		name := fmt.Sprintf("%s.field%d", resultName, index)
		if index == len(st.Fields)-1 {
			name = resultName
		}
		fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, %d\n",
			name, structType, aggregate, e.llvmType(field.Type), fieldValue.operand, index)
		aggregate = name
	}
	if len(st.Fields) == 0 {
		resultName = "zeroinitializer"
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeField lowers a checked struct field read to an LLVM aggregate extraction.
func (e *emitter) writeField(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: field read expects 1 arg")
	}
	receiver := instr.Args[0]
	st, ok := e.module.Structs[receiver.Type]
	if !ok {
		return fmt.Errorf("llvm error: unknown struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if instr.Result.Type != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: field `%s.%s` returns %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Result.Type,
		)
	}
	value := e.value(receiver)
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n",
		name, e.llvmType(receiver.Type), value.operand, index)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writePrint writes calls to the Kizu runtime print ABI.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("llvm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "[]u8":
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
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = phi %s %s\n",
		name, e.llvmType(instr.Result.Type), strings.Join(parts, ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// unsupported rejects checked IR that has no concrete LLVM representation yet.
func (e *emitter) unsupported(instr *ir.Instr) error {
	return fmt.Errorf("llvm error: `%s` is not supported by the LLVM backend yet", instr.Op)
}

// writeTerminator writes one LLVM terminator.
func (e *emitter) writeTerminator(term ir.Terminator) error {
	switch term.Op {
	case "return":
		if term.Value.Type == "void" {
			if e.mainReturnsInt {
				e.out.WriteString("  ret i32 0\n")
				return nil
			}
			e.out.WriteString("  ret void\n")
			return nil
		}
		value := e.value(term.Value)
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(term.Value.Type), value.operand)
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

// structFieldIndex resolves a field offset in a declared struct.
func structFieldIndex(st ir.Struct, name string) (int, bool) {
	for index, field := range st.Fields {
		if field.Name == name {
			return index, true
		}
	}
	return 0, false
}

// value resolves an SSA value to a LLVM operand.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	return valueInfo{typ: value.Type, operand: localName(value.Name)}
}
