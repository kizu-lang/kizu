package llvm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeUnionRuntimeDecls declares helpers used for boxed union payloads.
func (e *emitter) writeUnionRuntimeDecls() {
	if !e.usesUnionBoxRuntime() {
		return
	}
	e.out.WriteString("declare ptr @kizu_union_box(i64, ptr)\n\n")
}

// usesUnionBoxRuntime reports whether any union constructor stores a payload.
func (e *emitter) usesUnionBoxRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "union.new" && len(instr.Args) == 1 {
					return true
				}
				if instr.Op == "error.error" {
					errorName, _, ok := errorUnionParts(instr.Result.Type)
					if ok && errorName != "" {
						return true
					}
				}
			}
		}
	}
	return false
}

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

// writeUnionNew lowers a tagged union constructor.
func (e *emitter) writeUnionNew(instr *ir.Instr) error {
	_, variant, ok := e.unionVariant(instr.Result.Type, instr.Immediate)
	if !ok {
		return fmt.Errorf("llvm error: unknown union variant `%s::%s`",
			instr.Result.Type, instr.Immediate)
	}
	if len(instr.Args) > 1 {
		return fmt.Errorf("llvm error: union.new expects at most one payload")
	}
	resultName := localName(instr.Result.Name)
	tagName := resultName + ".tag"
	payload := "null"
	if len(instr.Args) == 1 {
		slot := e.writeStackValue(resultName+".payload", instr.Args[0])
		boxName := resultName + ".box"
		fmt.Fprintf(&e.out, "  %s = call ptr @kizu_union_box(i64 %s, ptr %s)\n",
			boxName, e.elementSizeOperand(instr.Args[0].Type), slot)
		payload = boxName
	}
	fmt.Fprintf(&e.out, "  %s = insertvalue %s poison, i64 %d, 0\n",
		tagName, e.llvmType(instr.Result.Type), variant.Index)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, ptr %s, 1\n",
		resultName, e.llvmType(instr.Result.Type), tagName, payload)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
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

// writeUnionPayload loads a checked payload from a union value.
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
	value := e.value(instr.Args[0])
	ptrName := localName(instr.Result.Name) + ".ptr"
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n",
		ptrName, e.llvmType(instr.Args[0].Type), value.operand)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", resultName, e.llvmType(instr.Result.Type), ptrName)
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
