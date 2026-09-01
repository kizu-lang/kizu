package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// usesByteEqualityRuntime reports whether an instruction compares byte
// slices. Both language equality and testing assertions share one byte loop.
func (e *emitter) usesByteEqualityRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "test.expect_equal" && len(instr.Args) == 2 &&
					instr.Args[0].Type == "[]u8" {
					return true
				}
				if strings.HasPrefix(instr.Op, "binary.") && len(instr.Args) == 2 &&
					instr.Args[0].Type == "[]u8" {
					return true
				}
			}
		}
	}
	return false
}

// byteSliceParts returns the pointer and wasm32 length loaded from one []u8
// descriptor expression.
func byteSliceParts(descriptor string) (string, string) {
	return "(i32.load " + descriptor + ")", "(i32.load " + addressAt(descriptor, 4) + ")"
}

// writeByteEquality lowers []u8 == and != by content rather than by the
// descriptor address that happens to carry each view.
func (e *emitter) writeByteEquality(instr *ir.Instr, op string) error {
	if instr.Result.Type != "bool" || instr.Args[1].Type != "[]u8" ||
		(op != "==" && op != "!=") {
		return fmt.Errorf("wasm error: byte slices support only == and !=")
	}
	leftPtr, leftLen := byteSliceParts(e.value(instr.Args[0]).expr)
	rightPtr, rightLen := byteSliceParts(e.value(instr.Args[1]).expr)
	expr := fmt.Sprintf("(call $__bytes_equal %s %s %s %s)",
		leftPtr, leftLen, rightPtr, rightLen)
	if op == "!=" {
		expr = "(i32.eqz " + expr + ")"
	}
	return e.writeScalarResult(instr.Result, expr)
}

// writeByteEqualityHelper compares two byte ranges without allocating.
func (e *emitter) writeByteEqualityHelper() {
	e.out.WriteString("  (func $__bytes_equal (param $left i32) (param $left_len i32)\n")
	e.out.WriteString("      (param $right i32) (param $right_len i32) (result i32)\n")
	e.out.WriteString("    (local $index i32)\n")
	e.out.WriteString("    (if (i32.ne (local.get $left_len) (local.get $right_len))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (loop $bytes\n")
	e.out.WriteString("      (if (i32.lt_u (local.get $index) (local.get $left_len))\n")
	e.out.WriteString("        (then\n")
	e.out.WriteString("          (if (i32.ne\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $left) (local.get $index)))\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $right) (local.get $index))))\n")
	e.out.WriteString("            (then (return (i32.const 0))))\n")
	e.out.WriteString("          (local.set $index (i32.add (local.get $index) (i32.const 1)))\n")
	e.out.WriteString("          (br $bytes))))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}
