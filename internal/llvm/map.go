package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// mapHeaderType names the runtime's KizuMap header in emitted modules. A map
// is its header rather than a pointer to one, so an empty map costs no
// allocation at all: the compiler keeps half a million alive at once and most
// of them never hold an entry. runtime.c asserts the same offsets, so the two
// spellings cannot drift apart (ADR-0131).
const mapHeaderType = "%kizu.map"

// mapHeaderSize is what mapHeaderType occupies: the entry storage and its
// count, plus the index that makes a lookup O(1). Neither the value size nor
// the allocator is among them (ADR-0132).
const mapHeaderSize = 40

// The field index of the entry count, which is all Map.len reads.
const mapFieldLen = 1

// writeMapRuntimeDecls writes declarations for the hosted Map runtime.
func (e *emitter) writeMapRuntimeDecls() {
	if !e.usesMapRuntime() {
		return
	}
	fmt.Fprintf(&e.out, "%s = type { ptr, i64, i64, ptr, i64 }\n", mapHeaderType)
	e.out.WriteString("declare i1 @kizu_map_insert(ptr, ptr, ptr, i64, ptr, i64)\n")
	e.out.WriteString("declare ptr @kizu_map_get(ptr, ptr, i64)\n")
	e.out.WriteString("declare ptr @kizu_map_value_at(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_map_key_at(ptr, ptr, i64)\n")
	e.out.WriteString("declare i1 @kizu_map_contains(ptr, ptr, i64)\n")
	e.out.WriteString("declare i64 @kizu_map_len(ptr)\n")
	e.out.WriteString("declare void @kizu_map_deinit(ptr, ptr, i64)\n\n")
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
	case "map.at", "map.at_mut":
		return e.writeMapAt(instr)
	case "map.take_value_at":
		return e.writeMapTakeValueAt(instr)
	case "map.key_at":
		return e.writeMapKeyAt(instr)
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

// writeMapNew lowers std::map::new<K, V>(allocator) to the header value an
// empty map is: five zero words. An empty map owns no storage, so it needs
// none, and it keeps nothing about how to grow either -- the allocator the
// constructor names is read by the checker, which requires every later call
// that allocates or releases to name the same one (ADR-0131, ADR-0132). So
// the construction costs no instruction at all.
func (e *emitter) writeMapNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isMapLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: map.new expects allocator -> Map<K, V>")
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "zeroinitializer"}
	return nil
}

// writeMapInsert lowers Map.insert(allocator, key, value). The insert is what
// buys the storage the entry goes in, so it names the allocator it buys from
// and the width of the value it copies: the header keeps neither (ADR-0132).
func (e *emitter) writeMapInsert(instr *ir.Instr) error {
	if len(instr.Args) != 4 || instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"llvm error: map.insert expects Map, Allocator, K, V -> std::mem::Error!void")
	}
	value, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	mapValue := e.value(instr.Args[0])
	allocator := e.value(instr.Args[1])
	keyPtr, keyLen, err := e.writeMapKeyParts(
		localName(instr.Result.Name)+".key", instr.Args[2])
	if err != nil {
		return err
	}
	valueSlot := e.writeStackValue(localName(instr.Result.Name)+".value", instr.Args[3])
	okName := localName(instr.Result.Name) + ".ok"
	fmt.Fprintf(&e.out,
		"  %s = call i1 @kizu_map_insert(ptr %s, ptr %s, ptr %s, i64 %s, ptr %s, i64 %s)\n",
		okName, allocator.operand, mapValue.operand, keyPtr, keyLen, valueSlot,
		e.elementSizeOperand(value))
	return e.writeArrayBoolResult(instr.Result, okName, "map_insert")
}

// writeMapGet lowers Map.get(key).
func (e *emitter) writeMapGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: map.get expects Map, K -> ?V")
	}
	mapValue := e.value(instr.Args[0])
	keyPtr, keyLen, err := e.writeMapKeyParts(
		localName(instr.Result.Name)+".key", instr.Args[1])
	if err != nil {
		return err
	}
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_map_get(ptr %s, ptr %s, i64 %s)\n",
		ptrName, mapValue.operand, keyPtr, keyLen)
	return e.writeArrayOptionalLoadResult(instr, ptrName, 0)
}

// writeMapAt lowers Map.at(key) and Map.at_mut(key) to a borrow optional: the
// runtime's nullable value pointer becomes the payload and its presence,
// branch-free. It calls the same kizu_map_get as Map.get and skips the load.
func (e *emitter) writeMapAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: %s expects Map, K -> ?&V", instr.Op)
	}
	mapValue := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	keyPtr, keyLen, err := e.writeMapKeyParts(resultName+".key", instr.Args[1])
	if err != nil {
		return err
	}
	ptrName := resultName + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_map_get(ptr %s, ptr %s, i64 %s)\n",
		ptrName, mapValue.operand, keyPtr, keyLen)
	return e.writeBorrowOptionalResult(instr, ptrName)
}

// writeMapTakeValueAt moves the value at insertion position index out of the
// map, or traps past the end. Only Map.deinit's cascade reaches it, and it
// walks 0..len, so the trap stands for a broken runtime rather than a
// reachable state.
func (e *emitter) writeMapTakeValueAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("llvm error: map.take_value_at expects Map, i64 -> V")
	}
	mapValue := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	lenName := "%" + e.nextSyntheticValue("map.take.panic.len")
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_map_len(ptr %s)\n", lenName, mapValue.operand)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_map_value_at(ptr %s, i64 %s)\n",
		ptrName, mapValue.operand, index.operand)
	e.writeNullFailure(instr, ptrName, "map.take.panic", "bounds", index.operand, lenName)
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapKeyAt lowers Map.key_at(index). The runtime fills a `?[]u8` slot
// through an out pointer: a by-value struct return would pin this declaration
// to the C ABI's aggregate-return rules, and the out pointer sidesteps that.
// A `[]u8` key is that slot. A scalar key is stored as its own bytes, so the
// same call answers it: the slot's pointer is where the key sits, and null
// past the end is the absent case every container optional already renders.
func (e *emitter) writeMapKeyAt(instr *ir.Instr) error {
	keyType, ok := typ.OptionalElem(instr.Result.Type)
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" || !ok || !typ.IsMapKey(keyType) {
		return fmt.Errorf("llvm error: map.key_at expects Map, i64 -> ?K")
	}
	mapValue := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	slotName := resultName + ".slot"
	// The out slot is the runtime's `?[]u8` either way. Only a `[]u8` key is
	// also the result, so only that spelling names the module's aggregate;
	// a scalar key reads the same bytes through the literal layout, which a
	// module holding no `[]u8` of its own still has.
	slotType := mapKeySlotType
	if keyType == "[]u8" {
		slotType = llvmOptionalTypeName(instr.Result.Type)
	}
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slotName, slotType)
	fmt.Fprintf(&e.out, "  call void @kizu_map_key_at(ptr %s, ptr %s, i64 %s)\n",
		slotName, mapValue.operand, index.operand)
	if keyType == "[]u8" {
		fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", resultName, slotType, slotName)
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
		return nil
	}
	return e.writeArrayOptionalLoadResult(instr, e.writeMapKeyPointer(resultName, slotName), 1)
}

// mapKeySlotType is the layout kizu_map_key_at fills: a presence tag and the
// {pointer, length} pair behind it.
const mapKeySlotType = "{ i8, ptr, i64 }"

// writeMapKeyPointer reads the filled slot back as a nullable pointer to the
// key bytes, so a scalar key rejoins the container-optional path an absent
// element already takes.
func (e *emitter) writeMapKeyPointer(resultName string, slotName string) string {
	optName := resultName + ".opt"
	hasName := resultName + ".has"
	foundName := resultName + ".found"
	bytesName := resultName + ".bytes"
	ptrName := resultName + ".key"
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", optName, mapKeySlotType, slotName)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", hasName, mapKeySlotType, optName)
	fmt.Fprintf(&e.out, "  %s = icmp ne i8 %s, 0\n", foundName, hasName)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n", bytesName, mapKeySlotType, optName)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, ptr %s, ptr null\n", ptrName, foundName, bytesName)
	return ptrName
}

// writeMapKeyParts renders one key as the (pointer, length) pair the runtime
// hashes and compares. A `[]u8` key already is that pair. A scalar key is its
// own bytes, so it goes to a stack slot and the pair names that slot and the
// key's width -- the runtime has one key representation, and this is where a
// key becomes it.
func (e *emitter) writeMapKeyParts(prefix string, key ir.Value) (string, string, error) {
	if key.Type == "[]u8" {
		operand, err := e.sliceValue(key)
		if err != nil {
			return "", "", err
		}
		ptrName, lenName := e.writeSliceParts(prefix, operand)
		return ptrName, lenName, nil
	}
	bits, ok := integerBitWidth(key.Type)
	if !ok || !typ.IsMapKey(key.Type) {
		return "", "", fmt.Errorf("llvm error: `%s` is not a std::map::Map key type", key.Type)
	}
	return e.writeStackValue(prefix, key), fmt.Sprintf("%d", bits/8), nil
}

// writeMapContains lowers Map.contains(key).
func (e *emitter) writeMapContains(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "bool" {
		return fmt.Errorf("llvm error: map.contains expects Map, K -> bool")
	}
	mapValue := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	keyPtr, keyLen, err := e.writeMapKeyParts(resultName+".key", instr.Args[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "  %s = call i1 @kizu_map_contains(ptr %s, ptr %s, i64 %s)\n",
		resultName, mapValue.operand, keyPtr, keyLen)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapLen lowers Map.len(), which is one header field.
func (e *emitter) writeMapLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: map.len expects Map -> i64")
	}
	addr := "%" + e.nextSyntheticValue("map.len.addr")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i64 0, i32 %d\n",
		addr, mapHeaderType, e.value(instr.Args[0]).operand, mapFieldLen)
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", resultName, addr)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeMapDeinit lowers Map.deinit(allocator). It releases the entries, the
// keys and values they point at, and the index; the values are the caller's to
// consume first, the same way Array.deinit is handed an array whose owners are
// already gone.
func (e *emitter) writeMapDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: map.deinit expects Map, Allocator -> void")
	}
	value, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	// The release is handed the header itself, the way Array.deinit is. The
	// runtime walks the entries it frees, so the header goes to a slot the
	// call can reach it through; nothing reads it back, since the binding is
	// gone by the time this returns.
	slot := e.writeStackValue("%"+e.nextSyntheticValue("map.deinit"), instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_map_deinit(ptr %s, ptr %s, i64 %s)\n",
		e.value(instr.Args[1]).operand, slot, e.elementSizeOperand(value))
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

// isMapLLVMType reports whether a lowered IR type is a std::map::Map<K, V>.
func isMapLLVMType(name string) bool {
	return strings.HasPrefix(name, "std::map::Map<") && strings.HasSuffix(name, ">")
}
