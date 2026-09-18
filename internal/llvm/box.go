package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeBoxRuntimeDecls writes declarations for the hosted Box runtime.
func (e *emitter) writeBoxRuntimeDecls() {
	if !e.usesBoxRuntime() {
		return
	}
	e.out.WriteString("declare ptr @kizu_box_alloc(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_box_deinit(ptr, ptr, i64)\n\n")
}

// usesBoxRuntime reports whether this module uses std::mem::Box lowering.
func (e *emitter) usesBoxRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "box.") {
					return true
				}
			}
		}
	}
	return false
}

// writeBoxInstr dispatches runtime-backed Box operations.
func (e *emitter) writeBoxInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "box.new":
		return e.writeBoxNew(instr)
	case "box.borrow", "box.borrow_mut":
		return e.writeBoxBorrow(instr)
	case "box.deinit":
		return e.writeBoxDeinit(instr)
	case "box.take":
		return e.writeBoxTake(instr)
	default:
		return fmt.Errorf("llvm error: unsupported box instruction `%s`", instr.Op)
	}
}

// writeBoxNew lowers std::mem::box<T>(allocator, value). The runtime hands
// back a cell or null, and the value is stored into the cell here, where its
// type is known, rather than copied byte by byte behind a call. The
// recoverable result is built from the null test, which selects the failure
// code.
func (e *emitter) writeBoxNew(instr *ir.Instr) error {
	success, ok := e.errorUnionSuccessType(instr.Result.Type)
	if len(instr.Args) != 2 || !ok || !isBoxLLVMType(success) {
		return fmt.Errorf("llvm error: box.new expects allocator, T -> !Box<T>")
	}
	code, err := e.failureErrorCode("box_new")
	if err != nil {
		return err
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	allocator := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	rawName := resultName + ".raw"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_box_alloc(ptr %s, i64 %s)\n",
		rawName, allocator.operand, e.elementSizeOperand(elem))
	okName := resultName + ".is_ok"
	fmt.Fprintf(&e.out, "  %s = icmp ne ptr %s, null\n", okName, rawName)
	storeLabel := helperLabel(resultName, "box.store")
	joinLabel := helperLabel(resultName, "box.join")
	e.markCurrentBlockExit(joinLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okName, storeLabel, joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", storeLabel)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(instr.Args[1].Type), e.value(instr.Args[1]).operand, rawName)
	fmt.Fprintf(&e.out, "  br label %%%s\n", joinLabel)
	fmt.Fprintf(&e.out, "%s:\n", joinLabel)
	codeName := resultName + ".code"
	fmt.Fprintf(&e.out, "  %s = select i1 %s, i64 0, i64 %d\n", codeName, okName, code)
	unionType := e.llvmType(instr.Result.Type)
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i64 %s, %d\n",
		baseName, unionType, codeName, errorCodeField)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, ptr %s, %d\n",
		resultName, unionType, baseName, rawName, errorPayloadField)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeBoxBorrow lowers Box.borrow() and Box.borrow_mut(). A Box value is its
// payload pointer, so a borrow that travels as a pointer is that pointer under
// the borrow's type, and one that travels as a copy loads the payload.
func (e *emitter) writeBoxBorrow(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isBoxLLVMType(instr.Args[0].Type) {
		return fmt.Errorf("llvm error: %s expects Box<T> -> &T", instr.Op)
	}
	box := e.value(instr.Args[0])
	if strings.HasPrefix(instr.Result.Type, "&") {
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: box.operand}
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), box.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeBoxDeinit lowers Box.deinit(): the runtime releases the cell.
func (e *emitter) writeBoxDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: box.deinit expects Box<T>, Allocator -> void")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	box := e.value(instr.Args[0])
	allocator := e.value(instr.Args[1])
	fmt.Fprintf(&e.out, "  call void @kizu_box_deinit(ptr %s, ptr %s, i64 %s)\n",
		allocator.operand, box.operand, e.elementSizeOperand(elem))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeBoxTake lowers the box_take primitive `Box.deinit` forwards to when the
// payload owns something: it moves out before the runtime releases the cell.
func (e *emitter) writeBoxTake(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isBoxLLVMType(instr.Args[0].Type) {
		return fmt.Errorf("llvm error: box.take expects Box<T>, Allocator -> T")
	}
	elem, err := e.instrElementType(instr)
	if err != nil {
		return err
	}
	box := e.value(instr.Args[0])
	allocator := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), box.operand)
	fmt.Fprintf(&e.out, "  call void @kizu_box_deinit(ptr %s, ptr %s, i64 %s)\n",
		allocator.operand, box.operand, e.elementSizeOperand(elem))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// isBoxLLVMType reports whether a lowered IR type is a std::mem::Box<T>.
func isBoxLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::mem::Box<") && strings.HasSuffix(typ, ">")
}
