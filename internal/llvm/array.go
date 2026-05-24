package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	arrayAppendMessage         = "array append failed"
	arrayAppendMessageGlobal   = "@.kizu.array.append_failed"
	arrayBoundsMessage         = "array index out of bounds"
	arrayBoundsMessageGlobal   = "@.kizu.array.index_oob"
	arrayPopMessage            = "array pop from empty"
	arrayPopMessageGlobal      = "@.kizu.array.pop_empty"
	arrayReserveMessage        = "array reserve failed"
	arrayReserveMessageGlobal  = "@.kizu.array.reserve_failed"
	arrayTruncateMessage       = "array truncate out of bounds"
	arrayTruncateMessageGlobal = "@.kizu.array.truncate_oob"
)

// writeArrayRuntimeGlobals writes static messages used by runtime Array errors.
func (e *emitter) writeArrayRuntimeGlobals() {
	if !e.usesArrayRuntime() {
		return
	}
	e.writeStaticStringGlobal(arrayAppendMessageGlobal, arrayAppendMessage)
	e.writeStaticStringGlobal(arrayBoundsMessageGlobal, arrayBoundsMessage)
	e.writeStaticStringGlobal(arrayPopMessageGlobal, arrayPopMessage)
	e.writeStaticStringGlobal(arrayReserveMessageGlobal, arrayReserveMessage)
	e.writeStaticStringGlobal(arrayTruncateMessageGlobal, arrayTruncateMessage)
	e.out.WriteByte('\n')
}

// writeArrayRuntimeDecls writes declarations for the hosted Array runtime.
func (e *emitter) writeArrayRuntimeDecls() {
	if !e.usesArrayRuntime() {
		return
	}
	e.out.WriteString("declare ptr @kizu_array_new(i64)\n")
	e.out.WriteString("declare i1 @kizu_array_append(ptr, ptr)\n")
	e.out.WriteString("declare i64 @kizu_array_len(ptr)\n")
	e.out.WriteString("declare i64 @kizu_array_capacity(ptr)\n")
	e.out.WriteString("declare i1 @kizu_array_reserve(ptr, i64)\n")
	e.out.WriteString("declare ptr @kizu_array_get(ptr, i64)\n")
	e.out.WriteString("declare ptr @kizu_array_pop(ptr)\n")
	e.out.WriteString("declare i1 @kizu_array_set(ptr, i64, ptr)\n")
	e.out.WriteString("declare i1 @kizu_array_truncate(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_array_clear(ptr)\n")
	e.out.WriteString("declare %kizu.slice.u8 @kizu_array_as_bytes(ptr)\n")
	e.out.WriteString("declare void @kizu_array_deinit(ptr)\n\n")
}

// usesArrayRuntime reports whether this module uses std::array::Array lowering.
func (e *emitter) usesArrayRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "array.") {
					return true
				}
			}
		}
	}
	return false
}

// writeArrayInstr dispatches runtime-backed Array operations.
func (e *emitter) writeArrayInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "array.new":
		return e.writeArrayNew(instr)
	case "array.append":
		return e.writeArrayAppend(instr)
	case "array.len":
		return e.writeArrayLen(instr)
	case "array.capacity":
		return e.writeArrayCapacity(instr)
	case "array.reserve":
		return e.writeArrayReserve(instr)
	case "array.pop":
		return e.writeArrayPop(instr)
	case "array.get":
		return e.writeArrayGet(instr)
	case "array.get_or_panic":
		return e.writeArrayGetOrPanic(instr)
	case "array.at", "array.at_mut":
		return e.writeArrayAt(instr)
	case "array.set":
		return e.writeArraySet(instr)
	case "array.truncate":
		return e.writeArrayTruncate(instr)
	case "array.clear":
		return e.writeArrayClear(instr)
	case "array.as_bytes":
		return e.writeArrayAsBytes(instr)
	case "array.deinit":
		return e.writeArrayDeinit(instr)
	default:
		return fmt.Errorf("llvm error: unsupported array instruction `%s`", instr.Op)
	}
}

// writeArrayNew lowers std::array::Array<T>(allocator) to an opaque runtime handle.
func (e *emitter) writeArrayNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isArrayLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: array.new expects allocator -> Array<T>")
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_new(i64 %s)\n",
		resultName, e.elementSizeOperand(instr.Immediate))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayAppend lowers Array.append(value) and preserves !void failure flow.
func (e *emitter) writeArrayAppend(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "!void" {
		return fmt.Errorf("llvm error: array.append expects Array<T>, T -> !void")
	}
	array := e.value(instr.Args[0])
	elemSlot := e.writeStackValue(localName(instr.Result.Name)+".elem", instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_append(ptr %s, ptr %s)\n",
		okName, array.operand, elemSlot)
	e.writeArrayBoolResult(instr.Result, okName, arrayAppendMessageGlobal, len(arrayAppendMessage))
	return nil
}

// writeArrayLen lowers Array.len().
func (e *emitter) writeArrayLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: array.len expects Array<T> -> i64")
	}
	array := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_array_len(ptr %s)\n", resultName, array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayCapacity lowers Array.capacity().
func (e *emitter) writeArrayCapacity(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: array.capacity expects Array<T> -> i64")
	}
	array := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_array_capacity(ptr %s)\n",
		resultName, array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayReserve lowers Array.reserve(additional).
func (e *emitter) writeArrayReserve(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" || instr.Result.Type != "!void" {
		return fmt.Errorf("llvm error: array.reserve expects Array<T>, i64 -> !void")
	}
	array := e.value(instr.Args[0])
	additional := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_reserve(ptr %s, i64 %s)\n",
		okName, array.operand, additional.operand)
	e.writeArrayBoolResult(instr.Result, okName, arrayReserveMessageGlobal, len(arrayReserveMessage))
	return nil
}

// writeArrayPop lowers Array.pop().
func (e *emitter) writeArrayPop(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: array.pop expects Array<T> -> !T")
	}
	array := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_pop(ptr %s)\n", ptrName, array.operand)
	e.writeArrayOptionalLoadResult(instr, ptrName, arrayPopMessageGlobal, len(arrayPopMessage))
	return nil
}

// writeArrayGet lowers Array.get(index).
func (e *emitter) writeArrayGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.get expects Array<T>, i64 -> !T")
	}
	array := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_get(ptr %s, i64 %s)\n",
		ptrName, array.operand, index.operand)
	e.writeArrayOptionalLoadResult(instr, ptrName, arrayBoundsMessageGlobal, len(arrayBoundsMessage))
	return nil
}

// writeArrayGetOrPanic lowers Array.get_or_panic(index).
func (e *emitter) writeArrayGetOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.get_or_panic expects Array<T>, i64 -> T")
	}
	array := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_get(ptr %s, i64 %s)\n",
		ptrName, array.operand, index.operand)
	e.writeNullTrap(ptrName, "array.get.panic")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayAt lowers Array.at(index) and Array.at_mut(index) to a checked pointer result.
func (e *emitter) writeArrayAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: array.at expects Array<T>, i64 -> !&T")
	}
	array := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_array_get(ptr %s, i64 %s)\n",
		ptrName, array.operand, index.operand)
	e.writeArrayOptionalPointerResult(
		instr,
		ptrName,
		arrayBoundsMessageGlobal,
		len(arrayBoundsMessage),
	)
	return nil
}

// writeArraySet lowers Array.set(index, value) and preserves !void failure flow.
func (e *emitter) writeArraySet(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "i64" || instr.Result.Type != "!void" {
		return fmt.Errorf("llvm error: array.set expects Array<T>, i64, T -> !void")
	}
	array := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	elemSlot := e.writeStackValue(localName(instr.Result.Name)+".elem", instr.Args[2])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_set(ptr %s, i64 %s, ptr %s)\n",
		okName, array.operand, index.operand, elemSlot)
	e.writeArrayBoolResult(instr.Result, okName, arrayBoundsMessageGlobal, len(arrayBoundsMessage))
	return nil
}

// writeArrayTruncate lowers Array.truncate(length).
func (e *emitter) writeArrayTruncate(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" || instr.Result.Type != "!void" {
		return fmt.Errorf("llvm error: array.truncate expects Array<T>, i64 -> !void")
	}
	array := e.value(instr.Args[0])
	length := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_array_truncate(ptr %s, i64 %s)\n",
		okName, array.operand, length.operand)
	e.writeArrayBoolResult(instr.Result, okName, arrayTruncateMessageGlobal, len(arrayTruncateMessage))
	return nil
}

// writeArrayClear lowers Array.clear().
func (e *emitter) writeArrayClear(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: array.clear expects Array<T> -> void")
	}
	array := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_array_clear(ptr %s)\n", array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeArrayAsBytes lowers Array<u8>.as_bytes().
func (e *emitter) writeArrayAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "[]u8" {
		return fmt.Errorf("llvm error: array.as_bytes expects Array<u8> -> []u8")
	}
	array := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call %%kizu.slice.u8 @kizu_array_as_bytes(ptr %s)\n",
		resultName, array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArrayDeinit lowers Array.deinit().
func (e *emitter) writeArrayDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: array.deinit expects Array<T> -> void")
	}
	array := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_array_deinit(ptr %s)\n", array.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeArrayOptionalLoadResult converts a nullable element pointer to !T.
func (e *emitter) writeArrayOptionalLoadResult(
	instr *ir.Instr,
	ptrName string,
	messageGlobal string,
	messageLen int,
) {
	success, _ := errorUnionSuccessType(instr.Result.Type)
	resultName := localName(instr.Result.Name)
	failLabel, okLabel, joinLabel := e.arrayResultLabels("array.result")
	nullName := resultName + ".is_null"
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, ptrName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", nullName, failLabel, okLabel)
	e.writeArrayFailureBlock(
		failLabel,
		resultName+".fail",
		instr.Result.Type,
		messageGlobal,
		messageLen,
		joinLabel,
	)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	valueName := resultName + ".value"
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", valueName, e.llvmType(success), ptrName)
	e.values[valueName] = valueInfo{typ: success, operand: valueName}
	okName := resultName + ".success"
	e.writeErrorSuccessValue(okName, instr.Result.Type, ir.Value{Name: valueName, Type: success})
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	fmt.Fprintf(&e.out, "  %s = phi %s [ %s, %%%s ], [ %s, %%%s ]\n",
		resultName, e.llvmType(instr.Result.Type), resultName+".fail", failLabel, okName, okLabel)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
}

// writeArrayOptionalPointerResult converts a nullable element pointer to !&T.
func (e *emitter) writeArrayOptionalPointerResult(
	instr *ir.Instr,
	ptrName string,
	messageGlobal string,
	messageLen int,
) {
	success, _ := errorUnionSuccessType(instr.Result.Type)
	resultName := localName(instr.Result.Name)
	failLabel, okLabel, joinLabel := e.arrayResultLabels("array.ref")
	nullName := resultName + ".is_null"
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, ptrName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", nullName, failLabel, okLabel)
	e.writeArrayFailureBlock(
		failLabel,
		resultName+".fail",
		instr.Result.Type,
		messageGlobal,
		messageLen,
		joinLabel,
	)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	e.values[ptrName] = valueInfo{typ: success, operand: ptrName}
	okName := resultName + ".success"
	e.writeErrorSuccessValue(okName, instr.Result.Type, ir.Value{Name: ptrName, Type: success})
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	fmt.Fprintf(&e.out, "  %s = phi %s [ %s, %%%s ], [ %s, %%%s ]\n",
		resultName, e.llvmType(instr.Result.Type), resultName+".fail", failLabel, okName, okLabel)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
}

// writeArrayBoolResult converts a runtime boolean into a !void result.
func (e *emitter) writeArrayBoolResult(
	result ir.Value,
	okOperand string,
	messageGlobal string,
	messageLen int,
) {
	resultName := localName(result.Name)
	unionType := e.llvmType(result.Type)
	baseName := resultName + ".base"
	okByteName := resultName + ".ok.byte"
	messageName := resultName + ".message"
	e.writeStaticStringSlice(messageName, messageGlobal, messageLen)
	fmt.Fprintf(&e.out, "  %s = zext i1 %s to i8\n", okByteName, okOperand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 %s, 0\n",
		baseName, unionType, okByteName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %%kizu.slice.u8 %s, %d\n",
		resultName, unionType, baseName, messageName, errorUnionFailureIndex(result.Type))
	e.values[result.Name] = valueInfo{typ: result.Type, operand: resultName}
}

// writeArrayFailureBlock writes one failed !T branch and jumps to joinLabel.
func (e *emitter) writeArrayFailureBlock(
	label string,
	resultName string,
	resultType string,
	messageGlobal string,
	messageLen int,
	joinLabel string,
) {
	fmt.Fprintf(&e.out, "%s:\n", label)
	e.writeErrorFailureValue(resultName, resultType, messageGlobal, messageLen)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
}

// writeErrorSuccessValue writes an error-union success value.
func (e *emitter) writeErrorSuccessValue(resultName string, resultType string, value ir.Value) {
	unionType := e.llvmType(resultType)
	baseName := resultName + ".base"
	valueInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
		baseName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		resultName, unionType, baseName, e.llvmType(value.Type), valueInfo.operand)
}

// writeErrorFailureValue writes an error-union failure value with a static message.
func (e *emitter) writeErrorFailureValue(
	resultName string,
	resultType string,
	messageGlobal string,
	messageLen int,
) {
	unionType := e.llvmType(resultType)
	baseName := resultName + ".base"
	messageName := resultName + ".message"
	e.writeStaticStringSlice(messageName, messageGlobal, messageLen)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 0, 0\n",
		baseName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %%kizu.slice.u8 %s, %d\n",
		resultName, unionType, baseName, messageName, errorUnionFailureIndex(resultType))
}

// writeNullTrap branches to llvm.trap when pointer is null and continues otherwise.
func (e *emitter) writeNullTrap(ptrName string, prefix string) {
	nullName := "%" + e.nextSyntheticValue(prefix+".is_null")
	trapLabel := e.nextSyntheticLabel(prefix + ".null")
	okLabel := e.nextSyntheticLabel(prefix + ".ok")
	fmt.Fprintf(&e.out, "  %s = icmp eq ptr %s, null\n", nullName, ptrName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", nullName, trapLabel, okLabel)
	e.writeTrapBlock(trapLabel)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}

// writeStackValue stores a first-class value in a temporary slot and returns its pointer.
func (e *emitter) writeStackValue(baseName string, value ir.Value) string {
	slotName := baseName + ".slot"
	valueInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slotName, e.llvmType(value.Type))
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(value.Type), valueInfo.operand, slotName)
	return slotName
}

// writeStaticStringGlobal writes one private string constant.
func (e *emitter) writeStaticStringGlobal(name string, value string) {
	fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n",
		name, len(value)+1, escapeString(value))
}

// writeStaticStringSlice materializes a %kizu.slice.u8 around a static global.
func (e *emitter) writeStaticStringSlice(resultName string, global string, length int) {
	ptrName := resultName + ".ptr"
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
		ptrName, length+1, global)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 poison, ptr %s, 0\n",
		baseName, ptrName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 %s, i64 %d, 1\n",
		resultName, baseName, length)
}

// arrayResultLabels creates fail, success, and join labels for checked Array operations.
func (e *emitter) arrayResultLabels(prefix string) (string, string, string) {
	failLabel := e.nextSyntheticLabel(prefix + ".fail")
	okLabel := e.nextSyntheticLabel(prefix + ".ok")
	joinLabel := e.nextSyntheticLabel(prefix + ".join")
	return failLabel, okLabel, joinLabel
}

// elementSizeOperand returns a LLVM constant expression for sizeof(T).
func (e *emitter) elementSizeOperand(typ string) string {
	llvmType := e.llvmType(typ)
	if llvmType == "ptr" {
		return "8"
	}
	return fmt.Sprintf("ptrtoint (ptr getelementptr (%s, ptr null, i64 1) to i64)", llvmType)
}

// isArrayLLVMType reports whether a lowered IR type is a std::array::Array<T>.
func isArrayLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::array::Array<") && strings.HasSuffix(typ, ">")
}
