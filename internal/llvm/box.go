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
	e.out.WriteString("declare ptr @kizu_box_new(ptr, i64, ptr)\n")
	e.out.WriteString("declare void @kizu_box_deinit(ptr)\n\n")
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
// back the payload pointer or null, and the recoverable result is built
// branch-free: the null test is the ok flag and selects the failure code.
func (e *emitter) writeBoxNew(instr *ir.Instr) error {
	success, ok := errorUnionSuccessType(instr.Result.Type)
	if len(instr.Args) != 2 || !ok || !isBoxLLVMType(success) {
		return fmt.Errorf("llvm error: box.new expects allocator, T -> !Box<T>")
	}
	code, err := e.failureErrorCode("box_new")
	if err != nil {
		return err
	}
	allocator := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	valueSlot := e.writeStackValue(resultName+".value", instr.Args[1])
	rawName := resultName + ".raw"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_box_new(ptr %s, i64 %s, ptr %s)\n",
		rawName, allocator.operand, e.elementSizeOperand(instr.Immediate), valueSlot)
	okName := resultName + ".is_ok"
	okByteName := resultName + ".ok.byte"
	codeName := resultName + ".code"
	fmt.Fprintf(&e.out, "  %s = icmp ne ptr %s, null\n", okName, rawName)
	fmt.Fprintf(&e.out, "  %s = zext i1 %s to i8\n", okByteName, okName)
	fmt.Fprintf(&e.out, "  %s = select i1 %s, i64 0, i64 %d\n", codeName, okName, code)
	unionType := e.llvmType(instr.Result.Type)
	baseName := resultName + ".base"
	payloadName := resultName + ".payload"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 %s, 0\n",
		baseName, unionType, okByteName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, ptr %s, 1\n",
		payloadName, unionType, baseName, rawName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, i64 %s, %d\n",
		resultName, unionType, payloadName, codeName,
		errorUnionFailureIndex(instr.Result.Type))
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
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: box.deinit expects Box<T> -> void")
	}
	box := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_box_deinit(ptr %s)\n", box.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeBoxTake lowers the box_take primitive deinit_all forwards to: the
// payload moves out before the runtime releases the cell.
func (e *emitter) writeBoxTake(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isBoxLLVMType(instr.Args[0].Type) {
		return fmt.Errorf("llvm error: box.take expects Box<T> -> T")
	}
	box := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), box.operand)
	fmt.Fprintf(&e.out, "  call void @kizu_box_deinit(ptr %s)\n", box.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// isBoxLLVMType reports whether a lowered IR type is a std::mem::Box<T>.
func isBoxLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::mem::Box<") && strings.HasSuffix(typ, ">")
}
