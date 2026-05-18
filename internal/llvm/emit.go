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
	needsProcess   bool
	needsFS        bool
	needsMem       bool
	needsString    bool
	currentMain    bool
	currentReturn  string
}

type valueInfo struct {
	typ     string
	operand string
	length  int
}

// emit writes declarations and function definitions.
func (e *emitter) emit() error {
	e.collectStrings()
	e.collectRuntimeNeeds()
	e.writeHeader()
	for _, fn := range e.module.Functions {
		if shouldSkipHostedStdFunction(fn.Name) {
			continue
		}
		if err := e.writeFunction(fn); err != nil {
			return err
		}
	}
	return nil
}

// collectRuntimeNeeds records which hosted runtime helpers are referenced.
func (e *emitter) collectRuntimeNeeds() {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "call.std.builtin.process_") {
					e.needsProcess = true
				}
				if strings.HasPrefix(instr.Op, "call.std.builtin.fs_") ||
					strings.HasPrefix(instr.Op, "call.std.builtin.io_") {
					e.needsFS = true
				}
				if instr.Op == "call.std.builtin.mem_len" {
					e.needsMem = true
				}
				if strings.HasPrefix(instr.Op, "string.") {
					e.needsString = true
				}
			}
		}
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
	if e.needsProcess {
		e.out.WriteString("declare void @kizu_process_init(i32, ptr)\n")
		e.out.WriteString("declare i64 @kizu_process_arg_count()\n")
		e.out.WriteString("declare ptr @kizu_process_arg(i64)\n")
		e.out.WriteString("declare ptr @kizu_process_env(ptr)\n\n")
	}
	if e.needsFS {
		e.out.WriteString("declare ptr @kizu_fs_read_file(ptr)\n")
		e.out.WriteString("declare void @kizu_fs_write_file(ptr, ptr)\n")
		e.out.WriteString("declare i1 @kizu_fs_exists(ptr)\n")
		e.out.WriteString("declare void @kizu_print_cstring(ptr)\n\n")
	}
	if e.needsMem {
		e.out.WriteString("declare i64 @kizu_mem_len(ptr)\n\n")
	}
	if e.needsString {
		e.out.WriteString("declare ptr @kizu_string_new()\n")
		e.out.WriteString("declare void @kizu_string_append_bytes(ptr, ptr)\n")
		e.out.WriteString("declare void @kizu_string_append_byte(ptr, i8)\n")
		e.out.WriteString("declare void @kizu_string_reserve(ptr, i64)\n")
		e.out.WriteString("declare void @kizu_string_truncate(ptr, i64)\n")
		e.out.WriteString("declare void @kizu_string_clear(ptr)\n")
		e.out.WriteString("declare void @kizu_string_deinit(ptr)\n")
		e.out.WriteString("declare ptr @kizu_string_as_bytes(ptr)\n")
		e.out.WriteString("declare i64 @kizu_string_len(ptr)\n")
		e.out.WriteString("declare i64 @kizu_string_capacity(ptr)\n\n")
	}
}

// shouldSkipHostedStdFunction omits source wrappers replaced by native runtime calls.
func shouldSkipHostedStdFunction(name string) bool {
	return strings.HasPrefix(name, "std.string.") ||
		strings.HasPrefix(name, "std.array.") ||
		name == "std.mem.byte_at" ||
		name == "std.mem.slice"
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
	returnType := llvmType(fn.Return)
	e.mainReturnsInt = fn.Name == "main" && (fn.Return == "void" || fn.Return == "!void")
	e.currentMain = fn.Name == "main"
	e.currentReturn = fn.Return
	if e.mainReturnsInt {
		returnType = "i32"
	}
	paramsText := strings.Join(params, ", ")
	if e.needsProcess && e.mainReturnsInt {
		paramsText = "i32 %argc, ptr %argv"
	}
	fmt.Fprintf(&e.out, "define %s @%s(%s) {\n", returnType, fn.Name, paramsText)
	for _, block := range fn.Blocks {
		if err := e.writeBlock(block); err != nil {
			return err
		}
	}
	e.out.WriteString("}\n\n")
	e.mainReturnsInt = false
	e.currentMain = false
	e.currentReturn = ""
	return nil
}

// writeBlock writes one LLVM basic block.
func (e *emitter) writeBlock(block *ir.Block) error {
	fmt.Fprintf(&e.out, "%s:\n", block.Name)
	if e.needsProcess && e.currentMain && block.Name == "entry" {
		e.out.WriteString("  call void @kizu_process_init(i32 %argc, ptr %argv)\n")
	}
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
	case strings.HasPrefix(instr.Op, "unary."):
		return e.writeUnary(instr)
	case strings.HasPrefix(instr.Op, "binary."):
		return e.writeBinary(instr)
	case strings.HasPrefix(instr.Op, "call."):
		return e.writeCall(instr)
	case strings.HasPrefix(instr.Op, "string."):
		return e.writeStringOp(instr)
	default:
		return e.writeNonCallInstr(instr)
	}
}

// writeNonCallInstr writes non-call instructions that need no name dispatch.
func (e *emitter) writeNonCallInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "cast":
		return e.writeCast(instr)
	case instr.Op == "phi":
		return e.writePhi(instr)
	case instr.Op == "index.byte":
		return e.writeByteIndex(instr)
	case instr.Op == "struct.new":
		return e.writeStructNew(instr)
	case strings.HasPrefix(instr.Op, "field."):
		return e.writeField(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" || instr.Op == "arena.get":
		return e.unsupported(instr)
	case instr.Op == "error.try":
		return e.writeErrorTry(instr)
	case instr.Op == "error.error":
		return e.unsupported(instr)
	default:
		return fmt.Errorf("llvm error: unsupported instruction `%s`", instr.Op)
	}
}

// writeUnary writes simple scalar unary operations.
func (e *emitter) writeUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: unary expects 1 arg")
	}
	value := e.value(instr.Args[0])
	name := localName(instr.Result.Name)
	op := strings.TrimPrefix(instr.Op, "unary.")
	switch op {
	case "!":
		fmt.Fprintf(&e.out, "  %s = xor i1 %s, true\n", name, value.operand)
	case "-":
		fmt.Fprintf(&e.out, "  %s = sub i64 0, %s\n", name, value.operand)
	default:
		return fmt.Errorf("llvm error: unsupported unary `%s`", op)
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
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
		name := localName(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
			name, len(unquoted)+1, global)
		e.values[instr.Result.Name] = valueInfo{
			typ: "[]const u8", operand: name, length: len(unquoted),
		}
	default:
		e.values[instr.Result.Name] = zeroValue(instr.Result.Type)
	}
	return nil
}

// writeByteIndex emits a raw byte load from a hosted byte buffer.
func (e *emitter) writeByteIndex(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: byte index expects bytes and index")
	}
	bytes := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	ptrName := resultName + ".ptr"
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds i8, ptr %s, i64 %s\n",
		ptrName, bytes.operand, index.operand)
	fmt.Fprintf(&e.out, "  %s = load i8, ptr %s\n", resultName, ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: "u8", operand: resultName}
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
		fmt.Fprintf(&e.out, "  %s = icmp %s %s %s, %s\n",
			name, pred, llvmType(instr.Args[0].Type), left.operand, right.operand)
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
	if name == "std.builtin.process_arg_count" {
		return e.writeProcessArgCount(instr)
	}
	if name == "std.builtin.process_arg" {
		return e.writeProcessArg(instr)
	}
	if name == "std.builtin.process_env" {
		return e.writeProcessEnv(instr)
	}
	if name == "std.builtin.mem_len" {
		return e.writeMemLen(instr)
	}
	if name == "std.builtin.mem_page_allocator" {
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "null"}
		return nil
	}
	if strings.HasPrefix(name, "std.builtin.fs_") || strings.HasPrefix(name, "std.builtin.io_") {
		return e.writeHostedBuiltin(name, instr)
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
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", resultName, call)
	e.values[instr.Result.Name] = callResultValue(instr.Result.Type, resultName)
	return nil
}

// writeHostedBuiltin emits minimal hosted std::io and std::fs runtime calls.
func (e *emitter) writeHostedBuiltin(name string, instr *ir.Instr) error {
	switch name {
	case "std.builtin.io_blocking", "std.builtin.io_threaded", "std.builtin.io_failing":
		e.values[instr.Result.Name] = valueInfo{typ: "Io", operand: "null"}
	case "std.builtin.io_write_stdout", "std.builtin.io_write_stderr", "std.builtin.io_read_stdin":
		return e.writeHostedVoidResult(instr)
	case "std.builtin.fs_read_file":
		return e.writeFSReadFile(instr)
	case "std.builtin.fs_write_file":
		return e.writeFSWriteFile(instr)
	case "std.builtin.fs_exists":
		return e.writeFSExists(instr)
	case "std.builtin.fs_metadata", "std.builtin.fs_create_dir", "std.builtin.fs_remove_dir",
		"std.builtin.fs_remove_file":
		return e.writeHostedVoidResult(instr)
	default:
		return fmt.Errorf("llvm error: unsupported hosted builtin `%s`", name)
	}
	return nil
}

// writeStringOp emits native hosted String builder runtime calls.
func (e *emitter) writeStringOp(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "string.")
	switch name {
	case "new":
		resultName := localName(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = call ptr @kizu_string_new()\n", resultName)
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	case "append_bytes":
		return e.writeStringAppendBytes(instr)
	case "append_byte":
		return e.writeStringAppendByte(instr)
	case "reserve", "truncate":
		return e.writeStringI64Mutation(name, instr)
	case "clear", "deinit":
		return e.writeStringNoArgMutation(name, instr)
	case "as_bytes":
		return e.writeStringAsBytes(instr)
	case "len", "capacity":
		return e.writeStringI64Query(name, instr)
	default:
		return fmt.Errorf("llvm error: unsupported String op `%s`", name)
	}
	return nil
}

// writeStringAppendBytes appends one C string to a hosted String.
func (e *emitter) writeStringAppendBytes(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: String.append_bytes expects receiver and bytes")
	}
	receiver := e.value(instr.Args[0])
	bytes := e.value(instr.Args[1])
	fmt.Fprintf(&e.out, "  call void @kizu_string_append_bytes(ptr %s, ptr %s)\n",
		receiver.operand, bytes.operand)
	return e.writeHostedVoidResult(instr)
}

// writeStringAppendByte appends one byte to a hosted String.
func (e *emitter) writeStringAppendByte(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: String.append_byte expects receiver and byte")
	}
	receiver := e.value(instr.Args[0])
	byte := e.value(instr.Args[1])
	fmt.Fprintf(&e.out, "  call void @kizu_string_append_byte(ptr %s, i8 %s)\n",
		receiver.operand, byte.operand)
	return e.writeHostedVoidResult(instr)
}

// writeStringI64Mutation emits reserve and truncate calls.
func (e *emitter) writeStringI64Mutation(name string, instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: String.%s expects receiver and i64", name)
	}
	receiver := e.value(instr.Args[0])
	value := e.value(instr.Args[1])
	fmt.Fprintf(&e.out, "  call void @kizu_string_%s(ptr %s, i64 %s)\n",
		name, receiver.operand, value.operand)
	return e.writeHostedVoidResult(instr)
}

// writeStringNoArgMutation emits clear and deinit calls.
func (e *emitter) writeStringNoArgMutation(name string, instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: String.%s expects receiver", name)
	}
	receiver := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_string_%s(ptr %s)\n", name, receiver.operand)
	e.values[instr.Result.Name] = zeroValue(instr.Result.Type)
	return nil
}

// writeStringAsBytes exposes the String storage as a NUL-terminated byte view.
func (e *emitter) writeStringAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: String.as_bytes expects receiver")
	}
	receiver := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_string_as_bytes(ptr %s)\n",
		resultName, receiver.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName, length: -1}
	return nil
}

// writeStringI64Query emits len and capacity calls.
func (e *emitter) writeStringI64Query(name string, instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: String.%s expects receiver", name)
	}
	receiver := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_string_%s(ptr %s)\n",
		resultName, name, receiver.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMemLen emits hosted C-string length for bootstrap byte buffers.
func (e *emitter) writeMemLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: mem_len expects 1 arg")
	}
	bytes := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_mem_len(ptr %s)\n", resultName, bytes.operand)
	e.values[instr.Result.Name] = valueInfo{typ: "i64", operand: resultName}
	return nil
}

// writeFSReadFile emits hosted file loading for bootstrap source reads.
func (e *emitter) writeFSReadFile(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: fs_read_file expects io and path")
	}
	path := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_fs_read_file(ptr %s)\n", resultName, path.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName, length: -1}
	return nil
}

// writeFSWriteFile emits hosted file writing for bootstrap artifacts.
func (e *emitter) writeFSWriteFile(instr *ir.Instr) error {
	if len(instr.Args) != 3 {
		return fmt.Errorf("llvm error: fs_write_file expects io, path, and bytes")
	}
	path := e.value(instr.Args[1])
	bytes := e.value(instr.Args[2])
	fmt.Fprintf(
		&e.out,
		"  call void @kizu_fs_write_file(ptr %s, ptr %s)\n",
		path.operand,
		bytes.operand,
	)
	return e.writeHostedVoidResult(instr)
}

// writeFSExists emits hosted existence checks.
func (e *emitter) writeFSExists(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: fs_exists expects io and path")
	}
	path := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_fs_exists(ptr %s)\n", resultName, path.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeHostedVoidResult records a successful hosted error-union placeholder.
func (e *emitter) writeHostedVoidResult(instr *ir.Instr) error {
	e.values[instr.Result.Name] = zeroValue(instr.Result.Type)
	return nil
}

// writeProcessArgCount emits hosted process argument count access.
func (e *emitter) writeProcessArgCount(instr *ir.Instr) error {
	if len(instr.Args) != 0 {
		return fmt.Errorf("llvm error: process_arg_count expects 0 args")
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_process_arg_count()\n", resultName)
	e.values[instr.Result.Name] = valueInfo{typ: "i64", operand: resultName}
	return nil
}

// writeProcessArg emits hosted process argv access.
func (e *emitter) writeProcessArg(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: process_arg expects 1 arg")
	}
	index := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_process_arg(i64 %s)\n", resultName, index.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName, length: -1}
	return nil
}

// writeProcessEnv emits hosted environment lookup.
func (e *emitter) writeProcessEnv(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: process_env expects 1 arg")
	}
	name := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_process_env(ptr %s)\n", resultName, name.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName, length: -1}
	return nil
}

// writeCast emits integer width conversions for the native subset.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: cast expects 1 arg")
	}
	value := e.value(instr.Args[0])
	sourceType := llvmType(instr.Args[0].Type)
	targetType := llvmType(instr.Result.Type)
	if sourceType == targetType || sourceType == "ptr" || targetType == "ptr" {
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: value.operand}
		return nil
	}
	resultName := localName(instr.Result.Name)
	op := integerCastOp(sourceType, targetType)
	fmt.Fprintf(&e.out, "  %s = %s %s %s to %s\n",
		resultName, op, sourceType, value.operand, targetType)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeErrorTry forwards the success payload for hosted bootstrap primitives.
func (e *emitter) writeErrorTry(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.try expects 1 arg")
	}
	if !canForwardErrorUnionPayload(instr.Result.Type) {
		return e.unsupported(instr)
	}
	value := e.value(instr.Args[0])
	e.values[instr.Result.Name] = valueInfo{
		typ: instr.Result.Type, operand: value.operand, length: value.length,
	}
	return nil
}

// canForwardErrorUnionPayload limits temporary hosted !T lowering to std bootstrap shapes.
func canForwardErrorUnionPayload(typ string) bool {
	switch typ {
	case "[]const u8", "void", "bool", "std::fs::Metadata", "std::string::String":
		return true
	default:
		return false
	}
}

// writePrint writes calls to the Kizu runtime print ABI.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("llvm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "[]const u8":
		if value.length < 0 {
			fmt.Fprintf(&e.out, "  call void @kizu_print_cstring(ptr %s)\n", value.operand)
			return nil
		}
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
		name, llvmType(instr.Result.Type), strings.Join(parts, ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeStructNew stores an opaque placeholder for aggregate values.
func (e *emitter) writeStructNew(instr *ir.Instr) error {
	e.values[instr.Result.Name] = zeroValue(instr.Result.Type)
	return nil
}

// writeField stores a typed placeholder for aggregate field reads.
func (e *emitter) writeField(instr *ir.Instr) error {
	e.values[instr.Result.Name] = zeroValue(instr.Result.Type)
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
			if e.currentReturn != "void" {
				value := zeroValue(e.currentReturn)
				fmt.Fprintf(&e.out, "  ret %s %s\n", llvmType(e.currentReturn), value.operand)
				return nil
			}
			e.out.WriteString("  ret void\n")
			return nil
		}
		if e.currentReturn == "!"+term.Value.Type &&
			llvmType(e.currentReturn) != llvmType(term.Value.Type) {
			value := zeroValue(e.currentReturn)
			fmt.Fprintf(&e.out, "  ret %s %s\n", llvmType(e.currentReturn), value.operand)
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
	return valueInfo{typ: value.Type, operand: localName(value.Name)}
}

// callResultValue stores dynamic byte slices with unknown runtime length.
func callResultValue(typ string, operand string) valueInfo {
	if typ == "[]const u8" || typ == "![]const u8" {
		return valueInfo{typ: typ, operand: operand, length: -1}
	}
	return valueInfo{typ: typ, operand: operand}
}

// zeroValue returns a compileable LLVM placeholder for a Kizu type.
func zeroValue(typ string) valueInfo {
	switch typ {
	case "bool":
		return valueInfo{typ: typ, operand: "false"}
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return valueInfo{typ: typ, operand: "0"}
	case "[]const u8":
		return valueInfo{typ: typ, operand: "null", length: 0}
	default:
		return valueInfo{typ: typ, operand: "null"}
	}
}
