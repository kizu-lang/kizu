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
	currentReturn  string
	mainReturnsInt bool
	nextLabel      int
}

type valueInfo struct {
	typ         string
	operand     string
	length      int
	lengthKnown bool
}

// emit writes declarations and function definitions.
func (e *emitter) emit() error {
	e.collectStrings()
	if err := e.validateModuleTypes(); err != nil {
		return err
	}
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
	if e.usesErrorUnionABI() {
		e.out.WriteString("%kizu.slice.u8 = type { ptr, i64 }\n")
	}
	e.writeErrorUnionTypes()
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

// writeErrorUnionTypes writes named recoverable-result ABI definitions.
func (e *emitter) writeErrorUnionTypes() {
	names := e.sortedErrorUnionNames()
	for _, name := range names {
		success, _ := errorUnionSuccessType(name)
		if success == "void" {
			fmt.Fprintf(&e.out, "%s = type { i1, %%kizu.slice.u8 }\n",
				llvmErrorUnionTypeName(name))
			continue
		}
		fmt.Fprintf(&e.out, "%s = type { i1, %s, %%kizu.slice.u8 }\n",
			llvmErrorUnionTypeName(name), e.llvmType(success))
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
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

// usesErrorUnionABI reports whether this module needs the recoverable-result ABI.
func (e *emitter) usesErrorUnionABI() bool {
	return len(e.sortedErrorUnionNames()) > 0
}

// sortedErrorUnionNames returns all error-union types referenced by this module.
func (e *emitter) sortedErrorUnionNames() []string {
	seen := map[string]bool{}
	for _, st := range e.module.Structs {
		for _, field := range st.Fields {
			e.collectErrorUnionName(seen, field.Type)
		}
	}
	for _, fn := range e.module.Functions {
		e.collectErrorUnionName(seen, fn.Return)
		for _, param := range fn.Params {
			e.collectErrorUnionName(seen, param.Type)
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				e.collectErrorUnionName(seen, instr.Result.Type)
				for _, arg := range instr.Args {
					e.collectErrorUnionName(seen, arg.Type)
				}
				for _, field := range instr.Fields {
					e.collectErrorUnionName(seen, field.Value.Type)
				}
				for _, incoming := range instr.Incoming {
					e.collectErrorUnionName(seen, incoming.Value.Type)
				}
			}
			e.collectErrorUnionName(seen, block.Terminator.Value.Type)
			e.collectErrorUnionName(seen, block.Terminator.Cond.Type)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validateModuleTypes rejects unsupported ABI shapes before header emission.
func (e *emitter) validateModuleTypes() error {
	for _, name := range e.sortedErrorUnionNames() {
		if err := validateErrorUnionType(name); err != nil {
			return err
		}
	}
	return nil
}

// collectErrorUnionName records concrete !T / Error!T type names.
func (e *emitter) collectErrorUnionName(seen map[string]bool, typ string) {
	if _, ok := errorUnionSuccessType(typ); ok {
		seen[typ] = true
	}
}

// writeFunction writes one LLVM function.
func (e *emitter) writeFunction(fn *ir.Function) error {
	if err := e.validateFunctionTypes(fn); err != nil {
		return err
	}
	e.values = map[string]valueInfo{}
	e.currentReturn = fn.Return
	e.nextLabel = 0
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, e.llvmType(param.Type)+" "+param.Name)
		e.values[param.Name] = valueInfo{typ: param.Type, operand: param.Name}
	}
	returnType := e.llvmType(fn.Return)
	_, returnsErrorUnion := errorUnionSuccessType(fn.Return)
	e.mainReturnsInt = fn.Name == "main" && (fn.Return == "void" || returnsErrorUnion)
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
	e.currentReturn = ""
	return nil
}

// validateFunctionTypes rejects ABI shapes this backend cannot lower faithfully.
func (e *emitter) validateFunctionTypes(fn *ir.Function) error {
	if err := validateErrorUnionType(fn.Return); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if err := validateErrorUnionType(param.Type); err != nil {
			return err
		}
	}
	return nil
}

// validateErrorUnionType checks the supported error-union ABI subset.
func validateErrorUnionType(typ string) error {
	success, ok := errorUnionSuccessType(typ)
	if !ok {
		return nil
	}
	if !isLowerableErrorUnionSuccess(success) {
		return fmt.Errorf(
			"llvm error: error union success type `%s` is not supported by the LLVM backend yet",
			success,
		)
	}
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
	case instr.Op == "error.ok":
		return e.writeErrorOK(instr)
	case instr.Op == "error.error":
		return e.writeErrorError(instr)
	case instr.Op == "error.try":
		return e.writeErrorTry(instr)
	case instr.Op == "arena.new" || instr.Op == "arena.add" ||
		instr.Op == "arena.get" || instr.Op == "arena.deinit":
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
			typ: "[]u8", operand: name, length: len(unquoted), lengthKnown: true,
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

// writeCast emits scalar no-op casts and explicit error-union ABI adaptation.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: cast expects 1 arg")
	}
	source := instr.Args[0]
	if _, sourceIsError := errorUnionSuccessType(source.Type); sourceIsError {
		if _, targetIsError := errorUnionSuccessType(instr.Result.Type); targetIsError {
			return e.writeErrorUnionCast(instr)
		}
		return fmt.Errorf(
			"llvm error: cannot cast error union %s to %s",
			source.Type,
			instr.Result.Type,
		)
	}
	if _, targetIsError := errorUnionSuccessType(instr.Result.Type); targetIsError {
		return fmt.Errorf(
			"llvm error: cannot cast %s to error union %s",
			source.Type,
			instr.Result.Type,
		)
	}
	value := e.value(instr.Args[0])
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: value.operand}
	return nil
}

// writeErrorUnionCast copies ok/value/message fields between compatible !T shapes.
func (e *emitter) writeErrorUnionCast(instr *ir.Instr) error {
	source := instr.Args[0]
	sourceSuccess, _ := errorUnionSuccessType(source.Type)
	targetSuccess, _ := errorUnionSuccessType(instr.Result.Type)
	if sourceSuccess != targetSuccess {
		return fmt.Errorf(
			"llvm error: cannot cast %s to %s",
			source.Type,
			instr.Result.Type,
		)
	}
	if err := validateErrorUnionType(source.Type); err != nil {
		return err
	}
	if err := validateErrorUnionType(instr.Result.Type); err != nil {
		return err
	}
	sourceInfo := e.value(source)
	sourceType := e.llvmType(source.Type)
	targetType := e.llvmType(instr.Result.Type)
	resultName := localName(instr.Result.Name)
	okName := resultName + ".ok"
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okName, sourceType, sourceInfo.operand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 %s, 0\n",
		baseName, targetType, okName)
	aggregate := baseName
	if sourceSuccess != "void" {
		valueName := resultName + ".value"
		valueBaseName := resultName + ".value.base"
		fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n",
			valueName, sourceType, sourceInfo.operand)
		fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
			valueBaseName, targetType, aggregate, e.llvmType(sourceSuccess), valueName)
		aggregate = valueBaseName
	}
	messageName := resultName + ".message"
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n",
		messageName, sourceType, sourceInfo.operand, errorUnionMessageIndex(source.Type))
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %%kizu.slice.u8 %s, %d\n",
		resultName, targetType, aggregate, messageName, errorUnionMessageIndex(instr.Result.Type))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
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

// writeErrorOK builds a successful error-union value.
func (e *emitter) writeErrorOK(instr *ir.Instr) error {
	success, ok := errorUnionSuccessType(instr.Result.Type)
	if !ok {
		return fmt.Errorf("llvm error: error.ok result must be !T, got %s", instr.Result.Type)
	}
	if err := validateErrorUnionType(instr.Result.Type); err != nil {
		return err
	}
	resultName := localName(instr.Result.Name)
	unionType := e.llvmType(instr.Result.Type)
	if success == "void" {
		if len(instr.Args) != 0 {
			return fmt.Errorf("llvm error: error.ok !void expects 0 args")
		}
		fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 true, 0\n",
			resultName, unionType)
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
		return nil
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.ok %s expects 1 arg", instr.Result.Type)
	}
	value := instr.Args[0]
	if value.Type != success {
		return fmt.Errorf("llvm error: error.ok expects %s, got %s", success, value.Type)
	}
	okName := resultName + ".ok"
	argInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 true, 0\n",
		okName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		resultName, unionType, okName, e.llvmType(success), argInfo.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeErrorError builds a failed error-union value.
func (e *emitter) writeErrorError(instr *ir.Instr) error {
	if _, ok := errorUnionSuccessType(instr.Result.Type); !ok {
		return fmt.Errorf("llvm error: error.error result must be !T, got %s", instr.Result.Type)
	}
	if err := validateErrorUnionType(instr.Result.Type); err != nil {
		return err
	}
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" {
		return fmt.Errorf("llvm error: error.error expects one []u8 message")
	}
	message, err := e.sliceValue(instr.Args[0])
	if err != nil {
		return err
	}
	resultName := localName(instr.Result.Name)
	unionType := e.llvmType(instr.Result.Type)
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 false, 0\n",
		baseName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %%kizu.slice.u8 %s, %d\n",
		resultName, unionType, baseName, message, errorUnionMessageIndex(instr.Result.Type))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeErrorTry unwraps success or returns failure from the current function.
func (e *emitter) writeErrorTry(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.try expects 1 arg")
	}
	source := instr.Args[0]
	sourceError, success, ok := errorUnionParts(source.Type)
	if !ok {
		return fmt.Errorf("llvm error: error.try expects !T, got %s", source.Type)
	}
	if err := validateErrorUnionType(source.Type); err != nil {
		return err
	}
	if instr.Result.Type != success {
		return fmt.Errorf("llvm error: error.try returns %s, got %s", success, instr.Result.Type)
	}
	targetError, _, ok := errorUnionParts(e.currentReturn)
	if !ok {
		return fmt.Errorf("llvm error: error.try requires function to return !T")
	}
	if sourceError != targetError {
		return fmt.Errorf(
			"llvm error: error.try cannot propagate %s into %s",
			source.Type,
			e.currentReturn,
		)
	}
	sourceValue := e.value(source)
	sourceType := e.llvmType(source.Type)
	okValue := localName(instr.Result.Name) + ".ok"
	okLabel := e.nextSyntheticLabel("try.ok")
	errLabel := e.nextSyntheticLabel("try.err")
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okValue, sourceType, sourceValue.operand)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okValue, okLabel, errLabel)
	fmt.Fprintf(&e.out, "%s:\n", errLabel)
	if err := e.writeErrorFailureReturn(source); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	if success == "void" {
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n", resultName, sourceType, sourceValue.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
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
		if !value.lengthKnown {
			return fmt.Errorf("llvm error: []u8 length is unavailable for print")
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
		if success, ok := errorUnionSuccessType(e.currentReturn); ok {
			return e.writeErrorUnionReturn(term.Value, success)
		}
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

// writeErrorUnionReturn emits a return from a function declared as !T.
func (e *emitter) writeErrorUnionReturn(value ir.Value, success string) error {
	if e.mainReturnsInt {
		if value.Type == e.currentReturn {
			return e.writeMainErrorUnionReturn(value)
		}
		if value.Type == success || value.Type == "void" && success == "void" {
			e.out.WriteString("  ret i32 0\n")
			return nil
		}
		return fmt.Errorf(
			"llvm error: cannot return %s from %s",
			value.Type,
			e.currentReturn,
		)
	}
	if value.Type == e.currentReturn {
		valueInfo := e.value(value)
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(value.Type), valueInfo.operand)
		return nil
	}
	if value.Type == success || value.Type == "void" && success == "void" {
		return e.writeImplicitErrorOKReturn(value)
	}
	return fmt.Errorf("llvm error: cannot return %s from %s", value.Type, e.currentReturn)
}

// writeImplicitErrorOKReturn wraps legacy success returns from malformed IR.
func (e *emitter) writeImplicitErrorOKReturn(value ir.Value) error {
	name := "%" + e.nextSyntheticValue("return.ok")
	unionType := e.llvmType(e.currentReturn)
	if value.Type == "void" {
		fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 true, 0\n",
			name, unionType)
		fmt.Fprintf(&e.out, "  ret %s %s\n", unionType, name)
		return nil
	}
	okName := "%" + e.nextSyntheticValue("return.ok.flag")
	valueInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 true, 0\n",
		okName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		name, unionType, okName, e.llvmType(value.Type), valueInfo.operand)
	fmt.Fprintf(&e.out, "  ret %s %s\n", unionType, name)
	return nil
}

// writeMainErrorUnionReturn maps main's !T result to process exit status.
func (e *emitter) writeMainErrorUnionReturn(value ir.Value) error {
	valueInfo := e.value(value)
	unionType := e.llvmType(value.Type)
	okName := "%" + e.nextSyntheticValue("main.ok")
	codeName := "%" + e.nextSyntheticValue("main.code")
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okName, unionType, valueInfo.operand)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, i32 0, i32 1\n", codeName, okName)
	fmt.Fprintf(&e.out, "  ret i32 %s\n", codeName)
	return nil
}

// writeErrorFailureReturn propagates a failed try from the current function.
func (e *emitter) writeErrorFailureReturn(source ir.Value) error {
	if e.mainReturnsInt {
		e.out.WriteString("  ret i32 1\n")
		return nil
	}
	sourceInfo := e.value(source)
	if source.Type == e.currentReturn {
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(source.Type), sourceInfo.operand)
		return nil
	}
	messageName := "%" + e.nextSyntheticValue("try.err.message")
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n",
		messageName,
		e.llvmType(source.Type),
		sourceInfo.operand,
		errorUnionMessageIndex(source.Type),
	)
	name := "%" + e.nextSyntheticValue("try.err")
	unionType := e.llvmType(e.currentReturn)
	baseName := name + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i1 false, 0\n",
		baseName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %%kizu.slice.u8 %s, %d\n",
		name, unionType, baseName, messageName, errorUnionMessageIndex(e.currentReturn))
	fmt.Fprintf(&e.out, "  ret %s %s\n", unionType, name)
	return nil
}

// sliceValue materializes a %kizu.slice.u8 from the current ptr+length string view.
func (e *emitter) sliceValue(value ir.Value) (string, error) {
	info := e.value(value)
	if value.Type != "[]u8" {
		return "", fmt.Errorf("llvm error: expected []u8, got %s", value.Type)
	}
	if !info.lengthKnown {
		return "", fmt.Errorf("llvm error: []u8 length is unavailable for error message")
	}
	name := "%" + e.nextSyntheticValue("slice")
	baseName := name + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 poison, ptr %s, 0\n",
		baseName, info.operand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 %s, i64 %d, 1\n",
		name, baseName, info.length)
	return name, nil
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

// errorUnionMessageIndex returns the field index of the failure message.
func errorUnionMessageIndex(typ string) int {
	success, ok := errorUnionSuccessType(typ)
	if ok && success == "void" {
		return 1
	}
	return 2
}

// nextSyntheticLabel returns a unique helper label inside the current function.
func (e *emitter) nextSyntheticLabel(prefix string) string {
	e.nextLabel++
	return fmt.Sprintf("%s.%d", prefix, e.nextLabel)
}

// nextSyntheticValue returns a unique helper value name without a leading percent.
func (e *emitter) nextSyntheticValue(prefix string) string {
	e.nextLabel++
	return fmt.Sprintf("kizu.%s.%d", prefix, e.nextLabel)
}

// value resolves an SSA value to a LLVM operand.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	return valueInfo{typ: value.Type, operand: localName(value.Name)}
}
