package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeTestRuntimeDecls declares helpers used by test intrinsics.
func (e *emitter) writeTestRuntimeDecls() {
	if !e.usesByteEqualityRuntime() {
		return
	}
	e.out.WriteString("declare i1 @kizu_bytes_equal(ptr, i64, ptr, i64)\n\n")
}

// usesByteEqualityRuntime reports whether []u8 equality is needed.
func (e *emitter) usesByteEqualityRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "test.expect_equal" && instr.Immediate == "[]u8" {
					return true
				}
				if strings.HasPrefix(instr.Op, "binary.") &&
					len(instr.Args) == 2 && instr.Args[0].Type == "[]u8" {
					return true
				}
			}
		}
	}
	return false
}

// writeTestInstr dispatches std::testing intrinsics.
func (e *emitter) writeTestInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "test.fail":
		return e.writeTestFail(instr)
	case "test.expect_equal":
		return e.writeTestExpectEqual(instr)
	default:
		return fmt.Errorf("llvm error: unsupported test instruction `%s`", instr.Op)
	}
}

// writeTestFail traps unconditionally for std::testing failures.
func (e *emitter) writeTestFail(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" {
		return fmt.Errorf("llvm error: test.fail expects one []u8 message")
	}
	e.out.WriteString("  call void @llvm.trap()\n")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeTestExpectEqual traps when two first-class values differ.
func (e *emitter) writeTestExpectEqual(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: test.expect_equal expects two args")
	}
	left := e.value(instr.Args[0])
	right := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	switch instr.Args[0].Type {
	case "bool":
		fmt.Fprintf(&e.out, "  %s = icmp eq i1 %s, %s\n", okName, left.operand, right.operand)
	case "[]u8":
		leftPtr, leftLen := e.writeSliceParts(localName(instr.Result.Name)+".left", left.operand)
		rightPtr, rightLen := e.writeSliceParts(localName(instr.Result.Name)+".right", right.operand)
		fmt.Fprintf(&e.out, "  %s = call i1 @kizu_bytes_equal(ptr %s, i64 %s, ptr %s, i64 %s)\n",
			okName, leftPtr, leftLen, rightPtr, rightLen)
	default:
		fmt.Fprintf(&e.out, "  %s = icmp eq %s %s, %s\n",
			okName, e.llvmType(instr.Args[0].Type), left.operand, right.operand)
	}
	e.writeBoolTrap(okName, "test.expect_equal")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeBoolTrap traps unless okOperand is true.
func (e *emitter) writeBoolTrap(okOperand string, prefix string) {
	trapLabel := e.nextSyntheticLabel(prefix + ".fail")
	okLabel := e.nextSyntheticLabel(prefix + ".ok")
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okOperand, okLabel, trapLabel)
	e.writeTrapBlock(trapLabel)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}
