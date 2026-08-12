package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeTestRuntimeDecls declares helpers used by test intrinsics. The failure
// reporting entries are declared with every other failure by writePanicDecls.
func (e *emitter) writeTestRuntimeDecls() {
	if e.usesByteEqualityRuntime() {
		e.out.WriteString("declare i1 @kizu_bytes_equal(ptr, i64, ptr, i64)\n\n")
	}
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

// writeTestFail reports an explicit std::testing failure and stops.
func (e *emitter) writeTestFail(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" {
		return fmt.Errorf("llvm error: test.fail expects one []u8 message")
	}
	ptr, length := e.writeSliceParts(localName(instr.Result.Name)+".msg",
		e.value(instr.Args[0]).operand)
	fmt.Fprintf(&e.out, "  call void @kizu_panic_test_fail(ptr %s, i64 %s, %s)\n",
		ptr, length, strings.Join(panicPosition(instr.Span), ", "))
	e.out.WriteString("  unreachable\n")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeTestExpectEqual reports the expected and actual values when they differ.
func (e *emitter) writeTestExpectEqual(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: test.expect_equal expects two args")
	}
	left := e.value(instr.Args[0])
	right := e.value(instr.Args[1])
	okName := localName(instr.Result.Name) + ".ok"
	position := strings.Join(panicPosition(instr.Span), ", ")
	var report string
	switch instr.Args[0].Type {
	case "bool":
		fmt.Fprintf(&e.out, "  %s = icmp eq i1 %s, %s\n", okName, left.operand, right.operand)
		report = fmt.Sprintf("  call void @kizu_panic_expect_equal_bool(i1 %s, i1 %s, %s)\n",
			left.operand, right.operand, position)
	case "[]u8":
		leftPtr, leftLen := e.writeSliceParts(localName(instr.Result.Name)+".left", left.operand)
		rightPtr, rightLen := e.writeSliceParts(localName(instr.Result.Name)+".right", right.operand)
		fmt.Fprintf(&e.out, "  %s = call i1 @kizu_bytes_equal(ptr %s, i64 %s, ptr %s, i64 %s)\n",
			okName, leftPtr, leftLen, rightPtr, rightLen)
		report = fmt.Sprintf(
			"  call void @kizu_panic_expect_equal_bytes(ptr %s, i64 %s, ptr %s, i64 %s, %s)\n",
			leftPtr, leftLen, rightPtr, rightLen, position)
	default:
		fmt.Fprintf(&e.out, "  %s = icmp eq %s %s, %s\n",
			okName, e.llvmType(instr.Args[0].Type), left.operand, right.operand)
		report = fmt.Sprintf("  call void @kizu_panic_expect_equal_int(i64 %s, i64 %s, %s)\n",
			left.operand, right.operand, position)
	}
	e.writeReportedFailure(okName, report)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeReportedFailure branches to a reporting block unless okOperand holds.
func (e *emitter) writeReportedFailure(okOperand string, report string) {
	failLabel := helperLabel(okOperand, "fail")
	okLabel := helperLabel(okOperand, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okOperand, okLabel, failLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	e.out.WriteString(report)
	e.out.WriteString("  unreachable\n")
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}
