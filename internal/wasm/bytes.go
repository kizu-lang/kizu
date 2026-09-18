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

// usesByteOrderRuntime reports whether an instruction orders byte slices.
func (e *emitter) usesByteOrderRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "slice.compare" {
					return true
				}
			}
		}
	}
	return false
}

// writeSliceCompare answers -1, 0 or 1 as the left bytes order before, with
// or after the right ones.
func (e *emitter) writeSliceCompare(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[0].Type != "[]u8" || instr.Args[1].Type != "[]u8" ||
		instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: slice.compare expects []u8, []u8 -> i64")
	}
	expr := fmt.Sprintf("(i64.extend_i32_s (call $__bytes_compare %s %s %s %s))",
		e.viewPointer(instr.Args[0]), e.viewLength(instr.Args[0]),
		e.viewPointer(instr.Args[1]), e.viewLength(instr.Args[1]))
	return e.writeScalarResult(instr.Result, expr)
}

// writeByteOrderHelper orders two byte ranges by the first byte they differ
// in, and a range that ends first before the other.
func (e *emitter) writeByteOrderHelper() {
	e.out.WriteString("  (func $__bytes_compare (param $left i32) (param $left_len i32)\n")
	e.out.WriteString("      (param $right i32) (param $right_len i32) (result i32)\n")
	e.out.WriteString("    (local $index i32) (local $shared i32) (local $a i32) (local $b i32)\n")
	e.out.WriteString("    (local.set $shared (select (local.get $left_len) (local.get $right_len)\n")
	e.out.WriteString("      (i32.lt_u (local.get $left_len) (local.get $right_len))))\n")
	e.out.WriteString("    (loop $bytes\n")
	e.out.WriteString("      (if (i32.lt_u (local.get $index) (local.get $shared))\n")
	e.out.WriteString("        (then\n")
	e.out.WriteString("          (local.set $a\n")
	e.out.WriteString("            (i32.load8_u (i32.add (local.get $left) (local.get $index))))\n")
	e.out.WriteString("          (local.set $b\n")
	e.out.WriteString("            (i32.load8_u (i32.add (local.get $right) (local.get $index))))\n")
	e.out.WriteString("          (if (i32.ne (local.get $a) (local.get $b))\n")
	e.out.WriteString("            (then (return (select (i32.const -1) (i32.const 1)\n")
	e.out.WriteString("              (i32.lt_u (local.get $a) (local.get $b))))))\n")
	e.out.WriteString("          (local.set $index (i32.add (local.get $index) (i32.const 1)))\n")
	e.out.WriteString("          (br $bytes))))\n")
	e.out.WriteString("    (i32.sub (i32.gt_u (local.get $left_len) (local.get $right_len))\n")
	e.out.WriteString("      (i32.lt_u (local.get $left_len) (local.get $right_len)))\n")
	e.out.WriteString("  )\n\n")
}
