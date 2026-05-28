package llvm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeUnionInstr dispatches tagged union operations.
func (e *emitter) writeUnionInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "union.new":
		return e.writeUnionNew(instr)
	case "union.tag":
		return e.writeUnionTag(instr)
	case "union.payload":
		return e.writeUnionPayload(instr)
	default:
		return fmt.Errorf("llvm error: unsupported union instruction `%s`", instr.Op)
	}
}

// writeUnionNew lowers a tagged union constructor to the #991 inline layout:
// it initializes the tag and only the active variant's inline payload.
func (e *emitter) writeUnionNew(instr *ir.Instr) error {
	_, variant, ok := e.unionVariant(instr.Result.Type, instr.Immediate)
	if !ok {
		return fmt.Errorf("llvm error: unknown union variant `%s::%s`",
			instr.Result.Type, instr.Immediate)
	}
	if len(instr.Args) > 1 {
		return fmt.Errorf("llvm error: union.new expects at most one payload")
	}
	var payload *ir.Value
	if len(instr.Args) == 1 {
		payload = &instr.Args[0]
	}
	resultName := localName(instr.Result.Name)
	if err := e.writeInlineUnion(resultName, instr.Result.Type, variant, payload); err != nil {
		return err
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeInlineUnion materializes a `tag + inline payload storage` union value
// named resultName. It writes the active tag and, when present, stores only the
// active variant's payload into the inline storage, leaving the inactive bytes
// uninitialized per the #991 ABI.
func (e *emitter) writeInlineUnion(
	resultName string,
	unionIRType string,
	variant ir.UnionVariant,
	payload *ir.Value,
) error {
	if variant.Payload == "" && payload != nil {
		return fmt.Errorf("llvm error: union variant `%s::%s` stores no payload, got `%s`",
			unionIRType, variant.Name, payload.Type)
	}
	if variant.Payload != "" && payload == nil {
		return fmt.Errorf("llvm error: union variant `%s::%s` requires a `%s` payload",
			unionIRType, variant.Name, variant.Payload)
	}
	if payload != nil && payload.Type != variant.Payload {
		return fmt.Errorf("llvm error: union variant `%s::%s` expects payload `%s`, got `%s`",
			unionIRType, variant.Name, variant.Payload, payload.Type)
	}
	unionType := e.llvmType(unionIRType)
	slotName := resultName + ".slot"
	tagPtr := resultName + ".tag.ptr"
	fmt.Fprintf(&e.out, "  %s = alloca %s, align %d\n", slotName, unionType, maxInlinePayloadAlign)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 0\n", tagPtr, unionType, slotName)
	fmt.Fprintf(&e.out, "  store i64 %d, ptr %s, align %d\n",
		variant.Index, tagPtr, maxInlinePayloadAlign)
	if payload != nil {
		_, payloadAlign, ok := e.typeLayout(payload.Type)
		if !ok {
			return fmt.Errorf(
				"llvm error: union variant `%s` has an unsupported payload type `%s`; "+
					"inline payload size/alignment must be compile-time known per the #991 ABI (#495)",
				variant.Name, payload.Type,
			)
		}
		payloadPtr := resultName + ".payload.ptr"
		value := e.value(*payload)
		fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 1\n",
			payloadPtr, unionType, slotName)
		fmt.Fprintf(&e.out, "  store %s %s, ptr %s, align %d\n",
			e.llvmType(payload.Type), value.operand, payloadPtr, payloadAlign)
	}
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s, align %d\n",
		resultName, unionType, slotName, maxInlinePayloadAlign)
	return nil
}

// writeUnionTag extracts the integer discriminant from a union value.
func (e *emitter) writeUnionTag(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: union.tag expects union -> i64")
	}
	value := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n",
		resultName, e.llvmType(instr.Args[0].Type), value.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeUnionPayload reads the active variant's payload from the inline storage.
// The tag dispatch happens in match before this runs, so only the active
// payload bytes are read; inactive storage is never touched.
func (e *emitter) writeUnionPayload(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: union.payload expects one union argument")
	}
	unionType, variant, ok := e.unionVariant(instr.Args[0].Type, instr.Immediate)
	if !ok || variant.Payload == "" {
		return fmt.Errorf("llvm error: unknown union payload `%s::%s`",
			instr.Args[0].Type, instr.Immediate)
	}
	if instr.Result.Type != variant.Payload {
		return fmt.Errorf("llvm error: union payload `%s::%s` returns %s, got %s",
			unionType.Name, variant.Name, variant.Payload, instr.Result.Type)
	}
	_, payloadAlign, ok := e.typeLayout(instr.Result.Type)
	if !ok {
		return fmt.Errorf(
			"llvm error: union payload `%s::%s` has an unsupported type `%s`; "+
				"inline payload size/alignment must be compile-time known per the #991 ABI (#495)",
			unionType.Name, variant.Name, instr.Result.Type)
	}
	value := e.value(instr.Args[0])
	llvmUnion := e.llvmType(instr.Args[0].Type)
	resultName := localName(instr.Result.Name)
	slotName := resultName + ".slot"
	payloadPtr := resultName + ".payload.ptr"
	fmt.Fprintf(&e.out, "  %s = alloca %s, align %d\n", slotName, llvmUnion, maxInlinePayloadAlign)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s, align %d\n",
		llvmUnion, value.operand, slotName, maxInlinePayloadAlign)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 1\n",
		payloadPtr, llvmUnion, slotName)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s, align %d\n",
		resultName, e.llvmType(instr.Result.Type), payloadPtr, payloadAlign)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// unionVariant resolves one union variant metadata entry.
func (e *emitter) unionVariant(
	typeName string,
	variantName string,
) (ir.Union, ir.UnionVariant, bool) {
	unionType, ok := e.module.Unions[typeName]
	if !ok {
		return ir.Union{}, ir.UnionVariant{}, false
	}
	variant, ok := unionType.Variants[variantName]
	return unionType, variant, ok
}
