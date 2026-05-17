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
	module  *ir.Module
	out     bytes.Buffer
	strings map[string]string
	values  map[string]valueInfo
	defined map[string]bool
	retType string
	block   *ir.Block
	preds   map[string]map[string]bool
	temp    int
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

// collectDefinedValues records SSA names that are produced in one function.
func (e *emitter) collectDefinedValues(fn *ir.Function) {
	e.defined = map[string]bool{}
	for _, param := range fn.Params {
		e.defined[param.Name] = true
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instrDefinesLLVM(instr) {
				e.defined[instr.Result.Name] = true
			}
		}
	}
}

// instrDefinesLLVM reports whether an instruction prints a local definition.
func instrDefinesLLVM(instr *ir.Instr) bool {
	if instr.Result.Type == "void" {
		return false
	}
	switch {
	case instr.Op == "const":
		return instr.Result.Type == "[]const u8"
	case strings.HasPrefix(instr.Op, "unary."):
		return true
	case strings.HasPrefix(instr.Op, "binary."):
		return true
	case strings.HasPrefix(instr.Op, "call."):
		return callDefinesLLVM(strings.TrimPrefix(instr.Op, "call."))
	case instr.Op == "cast" || instr.Op == "error.try":
		return true
	case instr.Op == "struct.new" || isFieldLoad(instr.Op):
		return true
	case instr.Op == "method.at" || instr.Op == "method.len":
		return true
	case instr.Op == "phi":
		return true
	default:
		return false
	}
}

// callDefinesLLVM reports whether a call instruction has a concrete result.
func callDefinesLLVM(name string) bool {
	if name == "print" || strings.HasPrefix(name, "std.io.write_") {
		return false
	}
	return !strings.HasPrefix(name, "std.") || stdCallDefinesLLVM(name)
}

// stdCallDefinesLLVM reports std calls that the LLVM backend lowers concretely.
func stdCallDefinesLLVM(name string) bool {
	switch name {
	case "std.process.arg_count", "std.process.arg", "std.mem.equal_bytes",
		"std.mem.starts_with", "std.mem.len", "std.mem.byte_at", "std.mem.slice",
		"std.fs.read_file", "std.fs.exists", "std.path.join":
		return true
	default:
		return strings.HasPrefix(name, "std.array.Array<")
	}
}

// collectStrings assigns stable global names to string constants.
func (e *emitter) collectStrings() {
	next := 0
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "const" && instr.Result.Type == "[]const u8" {
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
	e.out.WriteString("declare i64 @kizu_process_arg_count()\n")
	e.out.WriteString("declare ptr @kizu_process_arg(i64)\n")
	e.out.WriteString("declare i1 @kizu_bytes_equal(ptr, ptr)\n")
	e.out.WriteString("declare i1 @kizu_bytes_starts_with(ptr, ptr)\n")
	e.out.WriteString("declare i64 @kizu_bytes_len(ptr)\n")
	e.out.WriteString("declare i8 @kizu_byte_at(ptr, i64)\n")
	e.out.WriteString("declare ptr @kizu_bytes_slice(ptr, i64, i64)\n")
	e.out.WriteString("declare ptr @kizu_read_file(ptr)\n")
	e.out.WriteString("declare i1 @kizu_file_exists(ptr)\n")
	e.out.WriteString("declare ptr @kizu_path_join(ptr, ptr)\n\n")
	e.out.WriteString("declare ptr @malloc(i64)\n")
	e.out.WriteString("declare ptr @kizu_array_new()\n")
	e.out.WriteString("declare void @kizu_array_append(ptr, ptr)\n")
	e.out.WriteString("declare ptr @kizu_array_at(ptr, i64)\n")
	e.out.WriteString("declare i64 @kizu_array_len(ptr)\n\n")
}

// writeStructTypes writes LLVM identified struct layouts.
func (e *emitter) writeStructTypes() {
	names := make([]string, 0, len(e.module.Structs))
	for name := range e.module.Structs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := e.module.Structs[name]
		fields := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			fields = append(fields, llvmType(field.Type))
		}
		fmt.Fprintf(&e.out, "%s = type { %s }\n", structLLVMName(name), strings.Join(fields, ", "))
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

// writeFunction writes one LLVM function.
func (e *emitter) writeFunction(fn *ir.Function) error {
	e.values = map[string]valueInfo{}
	e.temp = 0
	e.collectDefinedValues(fn)
	e.preds = functionPredecessors(fn)
	e.retType = fn.Return
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		operand := llvmLocal(param.Name)
		params = append(params, llvmType(param.Type)+" "+operand)
		e.values[param.Name] = valueInfo{typ: param.Type, operand: operand}
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
	e.block = block
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
	if ok, err := e.writeNativeValueInstr(instr); ok {
		return err
	}
	switch {
	case instr.Op == "const":
		return e.writeConst(instr)
	case strings.HasPrefix(instr.Op, "unary."):
		return e.writeUnary(instr)
	case strings.HasPrefix(instr.Op, "binary."):
		return e.writeBinary(instr)
	case strings.HasPrefix(instr.Op, "call."):
		return e.writeCall(instr)
	case instr.Op == "cast":
		return e.writeCast(instr)
	case instr.Op == "phi":
		return e.writePhi(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" || instr.Op == "arena.get":
		return e.writeOpaqueValue(instr)
	case instr.Op == "error.try":
		return e.writeErrorTry(instr)
	case instr.Op == "error.error" || instr.Op == "error.ok":
		return e.writeOpaqueValue(instr)
	default:
		return fmt.Errorf("llvm error: unsupported instruction `%s`", instr.Op)
	}
}

// writeNativeValueInstr handles concrete aggregate and receiver operations.
func (e *emitter) writeNativeValueInstr(instr *ir.Instr) (bool, error) {
	switch {
	case instr.Op == "struct.new":
		return true, e.writeStructNew(instr)
	case isFieldLoad(instr.Op):
		return true, e.writeField(instr)
	case strings.HasPrefix(instr.Op, "field.store."):
		return true, e.writeFieldStore(instr)
	case strings.HasPrefix(instr.Op, "method."):
		return true, e.writeMethod(instr)
	default:
		return false, nil
	}
}

// writeConst writes scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	switch instr.Result.Type {
	case "i64":
		e.values[instr.Result.Name] = valueInfo{typ: "i64", operand: instr.Immediate}
	case "bool":
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: llvmBool(instr.Immediate)}
	case "[]const u8":
		unquoted, _ := strconv.Unquote(instr.Immediate)
		global := e.strings[instr.Immediate]
		result := llvmLocal(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
			result, len(unquoted)+1, global)
		e.values[instr.Result.Name] = valueInfo{
			typ: "[]const u8", operand: result, length: len(unquoted),
		}
	default:
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
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
	leftOperand := llvmOperand(left.operand, instr.Args[0].Type)
	rightOperand := llvmOperand(right.operand, instr.Args[1].Type)
	operandType := llvmType(instr.Args[0].Type)
	if instr.Result.Type == "bool" {
		pred := llvmPredicate(op)
		result := llvmLocal(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = icmp %s %s %s, %s\n",
			result, pred, operandType, leftOperand, rightOperand)
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: result}
		return nil
	}
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s %s %s, %s\n",
		result, llvmBinaryOp(op), operandType, leftOperand, rightOperand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// writeUnary lowers native-safe unary operators used by selfhost code.
func (e *emitter) writeUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: unary expects 1 arg")
	}
	op := strings.TrimPrefix(instr.Op, "unary.")
	value := e.value(instr.Args[0])
	result := llvmLocal(instr.Result.Name)
	switch op {
	case "!":
		operand := llvmOperand(value.operand, "bool")
		fmt.Fprintf(&e.out, "  %s = xor i1 %s, true\n", result, operand)
	case "&", "*":
		operand := llvmOperand(value.operand, instr.Result.Type)
		if err := e.writeCoercedAlias(result, operand, value.typ, instr.Result.Type); err != nil {
			return err
		}
	default:
		operand := llvmOperand(value.operand, instr.Result.Type)
		if err := e.writeCoercedAlias(result, operand, value.typ, instr.Result.Type); err != nil {
			return err
		}
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// writeCall writes runtime print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "call.")
	if name == "print" {
		return e.writePrint(instr.Args)
	}
	if e.writeKnownStdCall(name, instr) {
		return nil
	}
	if strings.HasPrefix(name, "std.") {
		return e.writeOpaqueValue(instr)
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
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", result, call)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// writeKnownStdCall lowers native host capability calls used by selfhost CLI.
func (e *emitter) writeKnownStdCall(name string, instr *ir.Instr) bool {
	switch name {
	case "std.process.arg_count":
		e.writeRuntimeValueCall(instr, "call i64 @kizu_process_arg_count()", "i64")
	case "std.process.arg":
		arg := e.callArg(instr, 0, "i64")
		e.writeRuntimeValueCall(instr, "call ptr @kizu_process_arg(i64 "+arg+")", "[]const u8")
	default:
		if e.writeMemoryStdCall(name, instr) {
			return true
		}
		if e.writeFileStdCall(name, instr) {
			return true
		}
		if e.writePathStdCall(name, instr) {
			return true
		}
		if strings.HasPrefix(name, "std.array.Array<") {
			e.writeRuntimeValueCall(instr, "call ptr @kizu_array_new()", instr.Result.Type)
			return true
		}
		return false
	case "std.io.write_stdout", "std.io.write_stderr":
		e.writeStdIOCall(instr)
	}
	return true
}

// writeMemoryStdCall lowers native byte-slice helpers.
func (e *emitter) writeMemoryStdCall(name string, instr *ir.Instr) bool {
	switch name {
	case "std.mem.equal_bytes":
		e.writeStdMemCompare(instr, "@kizu_bytes_equal")
	case "std.mem.starts_with":
		e.writeStdMemCompare(instr, "@kizu_bytes_starts_with")
	case "std.mem.len":
		text := e.callArg(instr, 0, "[]const u8")
		e.writeRuntimeValueCall(instr, "call i64 @kizu_bytes_len(ptr "+text+")", "i64")
	case "std.mem.byte_at":
		text := e.callArg(instr, 0, "[]const u8")
		index := e.callArg(instr, 1, "i64")
		call := "call i8 @kizu_byte_at(ptr " + text + ", i64 " + index + ")"
		e.writeRuntimeValueCall(instr, call, "!u8")
	case "std.mem.slice":
		text := e.callArg(instr, 0, "[]const u8")
		start := e.callArg(instr, 1, "i64")
		end := e.callArg(instr, 2, "i64")
		call := "call ptr @kizu_bytes_slice(ptr " + text + ", i64 " + start + ", i64 " + end + ")"
		e.writeRuntimeValueCall(instr, call, "![]const u8")
	default:
		return false
	}
	return true
}

// writeFileStdCall lowers explicit filesystem capability helpers.
func (e *emitter) writeFileStdCall(name string, instr *ir.Instr) bool {
	switch name {
	case "std.fs.read_file":
		path := e.callArg(instr, 1, "[]const u8")
		e.writeRuntimeValueCall(instr, "call ptr @kizu_read_file(ptr "+path+")", "![]const u8")
	case "std.fs.exists":
		path := e.callArg(instr, 1, "[]const u8")
		e.writeRuntimeValueCall(instr, "call i1 @kizu_file_exists(ptr "+path+")", "!bool")
	default:
		return false
	}
	return true
}

// writePathStdCall lowers native path helpers needed before Kizu std is self-hosted.
func (e *emitter) writePathStdCall(name string, instr *ir.Instr) bool {
	if name != "std.path.join" {
		return false
	}
	left := e.callArg(instr, 0, "[]const u8")
	right := e.callArg(instr, 1, "[]const u8")
	call := "call ptr @kizu_path_join(ptr " + left + ", ptr " + right + ")"
	e.writeRuntimeValueCall(instr, call, "[]const u8")
	return true
}

// writeStdMemCompare lowers two-slice byte predicates to runtime calls.
func (e *emitter) writeStdMemCompare(instr *ir.Instr, runtime string) {
	left := e.callArg(instr, 0, "[]const u8")
	right := e.callArg(instr, 1, "[]const u8")
	call := "call i1 " + runtime + "(ptr " + left + ", ptr " + right + ")"
	e.writeRuntimeValueCall(instr, call, "bool")
}

// writeRuntimeValueCall writes a runtime call with one SSA result.
func (e *emitter) writeRuntimeValueCall(instr *ir.Instr, call string, typ string) {
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", result, call)
	e.values[instr.Result.Name] = valueInfo{typ: typ, operand: result}
}

// writeStdIOCall lowers explicit stdout/stderr helpers to the print runtime.
func (e *emitter) writeStdIOCall(instr *ir.Instr) {
	if len(instr.Args) >= 2 {
		value := e.value(instr.Args[1])
		e.writePrintString(value)
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
}

// callArg returns a valid operand for one runtime call argument.
func (e *emitter) callArg(instr *ir.Instr, index int, typ string) string {
	if index >= len(instr.Args) {
		return llvmZero(typ)
	}
	value := e.value(instr.Args[index])
	return llvmOperand(value.operand, typ)
}

// writeCast emits a no-op value conversion for the Phase 16 low-level subset.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: cast expects 1 arg")
	}
	value := e.value(instr.Args[0])
	result := llvmLocal(instr.Result.Name)
	operand := llvmOperand(value.operand, instr.Args[0].Type)
	if err := e.writeCoercedAlias(result, operand, instr.Args[0].Type, instr.Result.Type); err != nil {
		return err
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// writeErrorTry forwards the successful payload for the opaque v0 error union.
func (e *emitter) writeErrorTry(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: try expects 1 arg")
	}
	value := e.value(instr.Args[0])
	result := llvmLocal(instr.Result.Name)
	operand := llvmTypedOperand(value.operand, value.typ, instr.Result.Type)
	if err := e.writeCoercedAlias(result, operand, value.typ, instr.Result.Type); err != nil {
		return err
	}
	e.values[instr.Result.Name] = valueInfo{
		typ:     instr.Result.Type,
		operand: result,
		length:  value.length,
	}
	return nil
}

// writeCoercedAlias emits a concrete SSA value for transparent conversions.
func (e *emitter) writeCoercedAlias(
	result string,
	operand string,
	fromType string,
	toType string,
) error {
	from := llvmType(fromType)
	to := llvmType(toType)
	if from == to {
		e.writeSameTypeAlias(result, operand, to)
		return nil
	}
	if isLLVMInteger(from) && isLLVMInteger(to) {
		fmt.Fprintf(&e.out, "  %s = %s %s %s to %s\n",
			result, llvmIntCastOp(from, to), from, operand, to)
		return nil
	}
	e.writeSameTypeAlias(result, llvmZero(toType), to)
	return nil
}

// writeSameTypeAlias emits a no-op instruction that still defines result.
func (e *emitter) writeSameTypeAlias(result string, operand string, typ string) {
	switch typ {
	case "i1":
		fmt.Fprintf(&e.out, "  %s = xor i1 %s, false\n", result, operand)
	case "i8", "i16", "i32", "i64":
		fmt.Fprintf(&e.out, "  %s = add %s %s, 0\n", result, typ, operand)
	default:
		fmt.Fprintf(&e.out, "  %s = select i1 true, ptr %s, ptr null\n", result, operand)
	}
}

// isLLVMInteger reports whether typ is an integer scalar LLVM type.
func isLLVMInteger(typ string) bool {
	return typ == "i1" || typ == "i8" || typ == "i16" || typ == "i32" || typ == "i64"
}

// llvmIntCastOp selects an integer cast for native scalar conversions.
func llvmIntCastOp(from string, to string) string {
	if llvmIntWidth(from) < llvmIntWidth(to) {
		return "zext"
	}
	return "trunc"
}

// llvmIntWidth returns the bit width of an integer LLVM type.
func llvmIntWidth(typ string) int {
	switch typ {
	case "i1":
		return 1
	case "i8":
		return 8
	case "i16":
		return 16
	case "i32":
		return 32
	default:
		return 64
	}
}

// writeStructNew allocates and initializes one Kizu struct value.
func (e *emitter) writeStructNew(instr *ir.Instr) error {
	st, ok := e.module.Structs[instr.Result.Type]
	if !ok {
		return e.writeOpaqueValue(instr)
	}
	result := llvmLocal(instr.Result.Name)
	size := len(st.Fields) * 8
	if size == 0 {
		size = 8
	}
	fmt.Fprintf(&e.out, "  %s = call ptr @malloc(i64 %d)\n", result, size)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	for _, field := range instr.Fields {
		if err := e.writeStructFieldStore(result, instr.Result.Type, field); err != nil {
			return err
		}
	}
	return nil
}

// writeStructFieldStore stores one initialized struct field.
func (e *emitter) writeStructFieldStore(base string, structType string, field ir.FieldArg) error {
	index, typ, ok := e.structField(structType, field.Name)
	if !ok {
		return fmt.Errorf("llvm error: unknown field `%s.%s`", structType, field.Name)
	}
	value := e.value(field.Value)
	slot := e.nextTemp("field")
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds %s, ptr %s, i64 0, i32 %d\n",
		slot, structLLVMName(structType), base, index)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		llvmType(typ), llvmTypedOperand(value.operand, value.typ, typ), slot)
	return nil
}

// writeField loads one field from a struct pointer.
func (e *emitter) writeField(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: field expects receiver")
	}
	receiver := instr.Args[0]
	name := strings.TrimPrefix(instr.Op, "field.")
	index, typ, ok := e.structField(receiver.Type, name)
	if !ok {
		return e.writeOpaqueValue(instr)
	}
	base := e.value(receiver)
	slot := e.nextTemp("field")
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds %s, ptr %s, i64 0, i32 %d\n",
		slot, structLLVMName(receiver.Type), llvmOperand(base.operand, receiver.Type), index)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", result, llvmType(typ), slot)
	e.values[instr.Result.Name] = valueInfo{typ: typ, operand: result}
	return nil
}

// writeFieldStore stores into one field of a struct pointer.
func (e *emitter) writeFieldStore(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: field store expects receiver and value")
	}
	receiver := instr.Args[0]
	valueArg := instr.Args[1]
	name := strings.TrimPrefix(instr.Op, "field.store.")
	index, typ, ok := e.structField(receiver.Type, name)
	if !ok {
		return e.writeOpaqueValue(instr)
	}
	base := e.value(receiver)
	value := e.value(valueArg)
	slot := e.nextTemp("field")
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds %s, ptr %s, i64 0, i32 %d\n",
		slot, structLLVMName(receiver.Type), llvmOperand(base.operand, receiver.Type), index)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		llvmType(typ), llvmTypedOperand(value.operand, value.typ, typ), slot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
	return nil
}

// isFieldLoad reports whether op is a non-mutating field access instruction.
func isFieldLoad(op string) bool {
	return strings.HasPrefix(op, "field.") && !strings.HasPrefix(op, "field.store.")
}

// writeMethod lowers the minimal Array receiver methods needed by selfhost.
func (e *emitter) writeMethod(instr *ir.Instr) error {
	switch instr.Op {
	case "method.append":
		return e.writeArrayAppend(instr)
	case "method.at":
		return e.writeArrayAt(instr)
	case "method.len":
		return e.writeArrayLen(instr)
	default:
		return e.writeOpaqueValue(instr)
	}
}

// writeArrayAppend stores a pointer-like element in the growable runtime array.
func (e *emitter) writeArrayAppend(instr *ir.Instr) error {
	if len(instr.Args) < 2 {
		return fmt.Errorf("llvm error: append expects receiver and value")
	}
	array := e.callArg(instr, 0, instr.Args[0].Type)
	value := e.callArg(instr, 1, instr.Args[1].Type)
	fmt.Fprintf(&e.out, "  call void @kizu_array_append(ptr %s, ptr %s)\n", array, value)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
	return nil
}

// writeArrayAt loads one pointer-like element from the runtime array.
func (e *emitter) writeArrayAt(instr *ir.Instr) error {
	if len(instr.Args) < 2 {
		return fmt.Errorf("llvm error: at expects receiver and index")
	}
	array := e.callArg(instr, 0, instr.Args[0].Type)
	index := e.callArg(instr, 1, "i64")
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_at(ptr %s, i64 %s)\n", result, array, index)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// writeArrayLen loads the runtime array length.
func (e *emitter) writeArrayLen(instr *ir.Instr) error {
	if len(instr.Args) < 1 {
		return fmt.Errorf("llvm error: len expects receiver")
	}
	array := e.callArg(instr, 0, instr.Args[0].Type)
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_array_len(ptr %s)\n", result, array)
	e.values[instr.Result.Name] = valueInfo{typ: "i64", operand: result}
	return nil
}

// writePrint writes calls to the Kizu runtime print ABI.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("llvm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "[]const u8":
		e.writePrintString(value)
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		fmt.Fprintf(&e.out, "  call void @kizu_print_int(i64 %s)\n", value.operand)
	case "bool":
		fmt.Fprintf(&e.out, "  call void @kizu_print_bool(i1 %s)\n", value.operand)
	default:
		fmt.Fprintf(&e.out, "  ; unsupported print type %s\n", args[0].Type)
	}
	return nil
}

// writePrintString writes a string print with static or runtime length.
func (e *emitter) writePrintString(value valueInfo) {
	text := llvmOperand(value.operand, "[]const u8")
	if value.length > 0 {
		fmt.Fprintf(&e.out, "  call void @kizu_print_string(ptr %s, i64 %d)\n", text, value.length)
		return
	}
	length := e.nextTemp("len")
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_bytes_len(ptr %s)\n", length, text)
	fmt.Fprintf(&e.out, "  call void @kizu_print_string(ptr %s, i64 %s)\n", text, length)
}

// writePhi writes an LLVM phi instruction.
func (e *emitter) writePhi(instr *ir.Instr) error {
	parts := make([]string, 0, len(instr.Incoming))
	for _, incoming := range instr.Incoming {
		if !e.blockHasPredecessor(incoming.Block) {
			continue
		}
		value := e.value(incoming.Value)
		operand := llvmTypedOperand(value.operand, value.typ, instr.Result.Type)
		parts = append(parts, fmt.Sprintf("[ %s, %%%s ]", operand, incoming.Block))
	}
	if len(parts) == 0 {
		e.values[instr.Result.Name] = valueInfo{
			typ:     instr.Result.Type,
			operand: llvmZero(instr.Result.Type),
		}
		return nil
	}
	result := llvmLocal(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = phi %s %s\n",
		result, llvmType(instr.Result.Type), strings.Join(parts, ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}

// blockHasPredecessor checks phi incoming blocks against the lowered CFG.
func (e *emitter) blockHasPredecessor(name string) bool {
	return e.block == nil || e.preds[e.block.Name] == nil || e.preds[e.block.Name][name]
}

// functionPredecessors computes basic-block predecessors from terminators.
func functionPredecessors(fn *ir.Function) map[string]map[string]bool {
	preds := map[string]map[string]bool{}
	for _, block := range fn.Blocks {
		switch block.Terminator.Op {
		case "jump":
			addPred(preds, block.Terminator.Target, block.Name)
		case "branch":
			addPred(preds, block.Terminator.Target, block.Name)
			addPred(preds, block.Terminator.Else, block.Name)
		}
	}
	return preds
}

// addPred records one predecessor edge.
func addPred(preds map[string]map[string]bool, target string, pred string) {
	if target == "" {
		return
	}
	if preds[target] == nil {
		preds[target] = map[string]bool{}
	}
	preds[target][pred] = true
}

// structField returns the field index and type for a struct member.
func (e *emitter) structField(structType string, name string) (int, string, bool) {
	st, ok := e.module.Structs[structType]
	if !ok {
		return 0, "", false
	}
	for index, field := range st.Fields {
		if field.Name == name {
			return index, field.Type, true
		}
	}
	return 0, "", false
}

// nextTemp creates an instruction-local temporary name.
func (e *emitter) nextTemp(prefix string) string {
	e.temp++
	return fmt.Sprintf("%%.%s.%d", prefix, e.temp)
}

// writeOpaqueValue represents values not lowered to concrete LLVM layout in phase 9.
func (e *emitter) writeOpaqueValue(instr *ir.Instr) error {
	fmt.Fprintf(&e.out, "  ; %s omitted in phase 9\n", instr.Op)
	e.values[instr.Result.Name] = valueInfo{
		typ:     instr.Result.Type,
		operand: llvmZero(instr.Result.Type),
	}
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
		typ := llvmType(e.retType)
		operand := llvmReturnOperand(value.operand, term.Value.Type, e.retType)
		fmt.Fprintf(&e.out, "  ret %s %s\n", typ, operand)
	case "jump":
		fmt.Fprintf(&e.out, "  br label %%%s\n", term.Target)
	case "branch":
		cond := e.value(term.Cond)
		fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n",
			llvmOperand(cond.operand, "bool"), term.Target, term.Else)
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
	if strings.HasPrefix(value.Name, "%") && !e.defined[value.Name] {
		return valueInfo{typ: value.Type, operand: llvmZero(value.Type)}
	}
	return valueInfo{typ: value.Type, operand: llvmLocal(value.Name)}
}
