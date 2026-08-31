package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeTestInstr dispatches std::testing runtime instructions.
func (e *emitter) writeTestInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "test.fail":
		return e.writeMessageFail(instr, "test.fail")
	case "test.expect_equal":
		return e.writeTestExpectEqual(instr)
	default:
		return fmt.Errorf("wasm error: unsupported test instruction `%s`", instr.Op)
	}
}

// writeTestExpectEqual compares two values and reports both only on failure.
func (e *emitter) writeTestExpectEqual(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "void" ||
		instr.Args[0].Type != instr.Args[1].Type {
		return fmt.Errorf("wasm error: test.expect_equal expects two matching args")
	}
	typ := instr.Args[0].Type
	left := e.value(instr.Args[0]).expr
	right := e.value(instr.Args[1]).expr
	helper := ""
	failure := ""
	args := ""
	switch typ {
	case "bool":
		helper = "__panic_expect_equal_bool"
		failure = fmt.Sprintf("(i32.ne %s %s)", left, right)
		args = left + " " + right
	case "[]u8":
		helper = "__panic_expect_equal_bytes"
		leftPtr, leftLen := byteSliceParts(left)
		rightPtr, rightLen := byteSliceParts(right)
		failure = fmt.Sprintf("(i32.eqz (call $__bytes_equal %s %s %s %s))",
			leftPtr, leftLen, rightPtr, rightLen)
		args = leftPtr + " " + leftLen + " " + rightPtr + " " + rightLen
	default:
		if !isIntegerType(typ) && !e.isTagType(typ) {
			return fmt.Errorf("wasm error: unsupported test.expect_equal type `%s`", typ)
		}
		helper = "__panic_expect_equal_int"
		failure = fmt.Sprintf("(i64.ne %s %s)", left, right)
		args = left + " " + right
	}
	fmt.Fprintf(&e.out, "            (if %s\n", failure)
	e.out.WriteString("              (then\n")
	fmt.Fprintf(&e.out,
		"                (call $%s %s (i64.const %d) (i64.const %d))))\n",
		helper, args, instr.Span.Start.Line, instr.Span.Start.Column)
	return nil
}

// writeTestPanicRuntime writes only the assertion diagnostic entries used by
// the module.
func (e *emitter) writeTestPanicRuntime() {
	if e.panicKinds[panicExpectIntKind] {
		e.writePanicExpectEqualIntHelper()
	}
	if e.panicKinds[panicExpectBoolKind] {
		e.writePanicExpectEqualBoolHelper()
	}
	if e.panicKinds[panicExpectBytesKind] {
		e.writePanicExpectEqualBytesHelper()
	}
}

// writePanicExpectPrefix writes `runtime error: expected `.
func (e *emitter) writePanicExpectPrefix() {
	e.writePanicBytes(e.strings[panicPrefixKey], "    ")
	e.writePanicBytes(e.strings[panicExpectedKey], "    ")
}

// writePanicExpectGot writes `, got `.
func (e *emitter) writePanicExpectGot() {
	e.writePanicBytes(e.strings[panicGotKey], "    ")
}

// writePanicExpectEqualIntHelper reports integer assertion values.
func (e *emitter) writePanicExpectEqualIntHelper() {
	e.out.WriteString("  (func $__panic_expect_equal_int (param $expected i64) (param $actual i64)\n")
	e.out.WriteString("      (param $line i64) (param $column i64)\n")
	e.writePanicExpectPrefix()
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $expected))\n")
	e.writePanicExpectGot()
	e.out.WriteString("    (call $__write_i64 (i32.const 2) (local.get $actual))\n")
	e.writePanicFinishAndExit()
}

// writePanicExpectEqualBoolHelper reports bool assertion values.
func (e *emitter) writePanicExpectEqualBoolHelper() {
	e.out.WriteString("  (func $__panic_expect_equal_bool (param $expected i32) (param $actual i32)\n")
	e.out.WriteString("      (param $line i64) (param $column i64)\n")
	e.writePanicExpectPrefix()
	e.writePanicBool("$expected")
	e.writePanicExpectGot()
	e.writePanicBool("$actual")
	e.writePanicFinishAndExit()
}

// writePanicBool writes one bool without the stdout helper's newline.
func (e *emitter) writePanicBool(local string) {
	truth := e.strings["true"]
	falsehood := e.strings["false"]
	fmt.Fprintf(&e.out, "    (if (local.get %s)\n", local)
	fmt.Fprintf(&e.out,
		"      (then (call $__write_bytes (i32.const 2) (i32.const %d) (i32.const %d)))\n",
		truth.offset, truth.length)
	fmt.Fprintf(&e.out,
		"      (else (call $__write_bytes (i32.const 2) (i32.const %d) (i32.const %d))))\n",
		falsehood.offset, falsehood.length)
}

// writePanicExpectEqualBytesHelper reports quoted byte assertion values.
func (e *emitter) writePanicExpectEqualBytesHelper() {
	quote := e.strings[panicQuoteKey]
	e.out.WriteString("  (func $__panic_expect_equal_bytes " +
		"(param $expected i32) (param $expected_len i32)\n")
	e.out.WriteString("      (param $actual i32) (param $actual_len i32)\n")
	e.out.WriteString("      (param $line i64) (param $column i64)\n")
	e.writePanicExpectPrefix()
	e.writePanicBytes(quote, "    ")
	e.out.WriteString("    (call $__write_bytes (i32.const 2) " +
		"(local.get $expected) (local.get $expected_len))\n")
	e.writePanicBytes(quote, "    ")
	e.writePanicExpectGot()
	e.writePanicBytes(quote, "    ")
	e.out.WriteString("    (call $__write_bytes (i32.const 2) " +
		"(local.get $actual) (local.get $actual_len))\n")
	e.writePanicBytes(quote, "    ")
	e.writePanicFinishAndExit()
}

// writePanicFinishAndExit closes one assertion helper.
func (e *emitter) writePanicFinishAndExit() {
	e.out.WriteString("    (call $__panic_at (local.get $line) (local.get $column))\n")
	e.out.WriteString("    (call $__panic_exit)\n")
	e.out.WriteString("  )\n\n")
}
