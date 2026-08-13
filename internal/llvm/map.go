package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeMapRuntimeDecls writes declarations for the hosted Map runtime.
func (e *emitter) writeMapRuntimeDecls() {
	if !e.usesMapRuntime() {
		return
	}
	e.out.WriteString("declare ptr @kizu_map_new(i64)\n")
	e.out.WriteString("declare i1 @kizu_map_insert(ptr, ptr, i64, ptr)\n")
	e.out.WriteString("declare ptr @kizu_map_get(ptr, ptr, i64)\n")
	e.out.WriteString("declare i1 @kizu_map_contains(ptr, ptr, i64)\n")
	e.out.WriteString("declare i64 @kizu_map_len(ptr)\n")
	e.out.WriteString("declare void @kizu_map_deinit(ptr)\n\n")
}

// usesMapRuntime reports whether this module uses std::map::Map lowering.
func (e *emitter) usesMapRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "map.") {
					return true
				}
			}
		}
	}
	return false
}

// writeMapInstr dispatches runtime-backed Map operations.
func (e *emitter) writeMapInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "map.new":
		return e.writeMapNew(instr)
	case "map.insert":
		return e.writeMapInsert(instr)
	case "map.get":
		return e.writeMapGet(instr)
	case "map.contains":
		return e.writeMapContains(instr)
	case "map.len":
		return e.writeMapLen(instr)
	case "map.deinit":
		return e.writeMapDeinit(instr)
	default:
		return fmt.Errorf("llvm error: unsupported map instruction `%s`", instr.Op)
	}
}

// writeMapNew lowers std::map::Map<[]u8, V>(allocator).
func (e *emitter) writeMapNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isMapLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: map.new expects allocator -> Map<[]u8, V>")
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_map_new(i64 %s)\n",
		resultName, e.elementSizeOperand(instr.Immediate))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapInsert lowers Map.insert(key, value).
func (e *emitter) writeMapInsert(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "[]u8" || instr.Result.Type != "!void" {
		return fmt.Errorf("llvm error: map.insert expects Map, []u8, V -> !void")
	}
	mapValue := e.value(instr.Args[0])
	key, err := e.sliceValue(instr.Args[1])
	if err != nil {
		return err
	}
	keyPtr, keyLen := e.writeSliceParts(localName(instr.Result.Name)+".key", key)
	valueSlot := e.writeStackValue(localName(instr.Result.Name)+".value", instr.Args[2])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_map_insert(ptr %s, ptr %s, i64 %s, ptr %s)\n",
		okName, mapValue.operand, keyPtr, keyLen, valueSlot)
	return e.writeArrayBoolResult(instr.Result, okName, "map_insert")
}

// writeMapGet lowers Map.get(key).
func (e *emitter) writeMapGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "[]u8" {
		return fmt.Errorf("llvm error: map.get expects Map, []u8 -> !V")
	}
	mapValue := e.value(instr.Args[0])
	key, err := e.sliceValue(instr.Args[1])
	if err != nil {
		return err
	}
	keyPtr, keyLen := e.writeSliceParts(localName(instr.Result.Name)+".key", key)
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_map_get(ptr %s, ptr %s, i64 %s)\n",
		ptrName, mapValue.operand, keyPtr, keyLen)
	return e.writeArrayOptionalLoadResult(instr, ptrName, "map_missing")
}

// writeMapContains lowers Map.contains(key).
func (e *emitter) writeMapContains(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "[]u8" || instr.Result.Type != "bool" {
		return fmt.Errorf("llvm error: map.contains expects Map, []u8 -> bool")
	}
	mapValue := e.value(instr.Args[0])
	key, err := e.sliceValue(instr.Args[1])
	if err != nil {
		return err
	}
	keyPtr, keyLen := e.writeSliceParts(localName(instr.Result.Name)+".key", key)
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_map_contains(ptr %s, ptr %s, i64 %s)\n",
		resultName, mapValue.operand, keyPtr, keyLen)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapLen lowers Map.len().
func (e *emitter) writeMapLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: map.len expects Map -> i64")
	}
	mapValue := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_map_len(ptr %s)\n", resultName, mapValue.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapDeinit lowers Map.deinit().
func (e *emitter) writeMapDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: map.deinit expects Map -> void")
	}
	mapValue := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_map_deinit(ptr %s)\n", mapValue.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeSliceParts extracts a byte-slice pointer and length.
func (e *emitter) writeSliceParts(prefix string, sliceOperand string) (string, string) {
	ptrName := prefix + ".ptr"
	lenName := prefix + ".len"
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n", ptrName, sliceOperand)
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 1\n", lenName, sliceOperand)
	return ptrName, lenName
}

// isMapLLVMType reports whether a lowered IR type is a std::map::Map<[]u8, V>.
func isMapLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::map::Map<[]u8, ") && strings.HasSuffix(typ, ">")
}
