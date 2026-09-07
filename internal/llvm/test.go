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
	if e.usesTestContext() {
		e.out.WriteString("declare void @kizu_test_begin(ptr, i64, ptr, i64)\n")
		e.out.WriteString("declare void @kizu_test_mark(i64, i64)\n\n")
	}
}

// usesTestContext reports whether a test body tells the runtime where it is.
func (e *emitter) usesTestContext() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "test.begin" {
					return true
				}
			}
		}
	}
	return false
}

// usesByteEqualityRuntime reports whether []u8 equality is needed.
func (e *emitter) usesByteEqualityRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "test.expect_equal" && len(instr.Args) > 0 &&
					instr.Args[0].Type == "[]u8" {
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
	case "test.begin":
		return e.writeTestBegin(instr)
	case "test.mark":
		return e.writeTestMark(instr)
	default:
		return fmt.Errorf("llvm error: unsupported test instruction `%s`", instr.Op)
	}
}

// writeTestBegin hands the runtime the name of the test that is starting and
// the file it was written in, so a failure can name both.
func (e *emitter) writeTestBegin(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[0].Type != "[]u8" || instr.Args[1].Type != "[]u8" {
		return fmt.Errorf("llvm error: test.begin expects a []u8 name and a []u8 file")
	}
	namePtr, nameLen := e.writeSliceParts(localName(instr.Result.Name)+".name",
		e.value(instr.Args[0]).operand)
	filePtr, fileLen := e.writeSliceParts(localName(instr.Result.Name)+".file",
		e.value(instr.Args[1]).operand)
	fmt.Fprintf(&e.out, "  call void @kizu_test_begin(ptr %s, i64 %s, ptr %s, i64 %s)\n",
		namePtr, nameLen, filePtr, fileLen)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeTestMark hands the runtime the position of the statement about to run.
func (e *emitter) writeTestMark(instr *ir.Instr) error {
	fmt.Fprintf(&e.out, "  call void @kizu_test_mark(%s)\n",
		strings.Join(panicPosition(instr.Span), ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
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
			e.runtimeIntegerOperand(instr.Args[0].Type, left.operand),
			e.runtimeIntegerOperand(instr.Args[1].Type, right.operand), position)
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
