package llvm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// Two byte slices are ordered by memcmp over the bytes they share and then by
// length. The comparison is a call LLVM knows by name rather than a loop it has
// to read: it compares a word at a time where a loop compares a byte, and a
// comparison whose result is only tested against zero -- an equality -- becomes
// a bcmp, which for a length known at the call is a few loads and no call.

// usesOp reports whether some instruction of this module is op.
func (e *emitter) usesOp(op string) bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == op {
					return true
				}
			}
		}
	}
	return false
}

// writeSliceCompareDecls declares memcmp for a module that compares slices.
func (e *emitter) writeSliceCompareDecls() {
	if e.usesOp("slice.compare") {
		e.out.WriteString("declare i32 @memcmp(ptr, ptr, i64)\n\n")
	}
}

// writeSliceCompare answers -1, 0 or 1 as left orders before, with or after
// right.
func (e *emitter) writeSliceCompare(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[0].Type != "[]u8" || instr.Args[1].Type != "[]u8" ||
		instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: slice.compare expects []u8, []u8 -> i64")
	}
	left := e.value(instr.Args[0]).operand
	right := e.value(instr.Args[1]).operand
	result := localName(instr.Result.Name)
	p := result + ".cmp"
	fmt.Fprintf(&e.out, "  %s.left.ptr = extractvalue %%kizu.slice.u8 %s, 0\n", p, left)
	fmt.Fprintf(&e.out, "  %s.left.len = extractvalue %%kizu.slice.u8 %s, 1\n", p, left)
	fmt.Fprintf(&e.out, "  %s.right.ptr = extractvalue %%kizu.slice.u8 %s, 0\n", p, right)
	fmt.Fprintf(&e.out, "  %s.right.len = extractvalue %%kizu.slice.u8 %s, 1\n", p, right)
	fmt.Fprintf(&e.out, "  %s.shorter = icmp slt i64 %s.left.len, %s.right.len\n", p, p, p)
	fmt.Fprintf(&e.out, "  %s.shared = select i1 %s.shorter, i64 %s.left.len, i64 %s.right.len\n",
		p, p, p, p)
	fmt.Fprintf(&e.out,
		"  %s.bytes = call i32 @memcmp(ptr %s.left.ptr, ptr %s.right.ptr, i64 %s.shared)\n",
		p, p, p, p)
	fmt.Fprintf(&e.out, "  %s.bytes.wide = sext i32 %s.bytes to i64\n", p, p)
	fmt.Fprintf(&e.out, "  %s.lengths = sub i64 %s.left.len, %s.right.len\n", p, p, p)
	fmt.Fprintf(&e.out, "  %s.tied = icmp eq i32 %s.bytes, 0\n", p, p)
	fmt.Fprintf(&e.out, "  %s.order = select i1 %s.tied, i64 %s.lengths, i64 %s.bytes.wide\n",
		p, p, p, p)
	fmt.Fprintf(&e.out, "  %s.after = icmp sgt i64 %s.order, 0\n", p, p)
	fmt.Fprintf(&e.out, "  %s.before = icmp slt i64 %s.order, 0\n", p, p)
	fmt.Fprintf(&e.out, "  %s.after.wide = zext i1 %s.after to i64\n", p, p)
	fmt.Fprintf(&e.out, "  %s.before.wide = zext i1 %s.before to i64\n", p, p)
	fmt.Fprintf(&e.out, "  %s = sub i64 %s.after.wide, %s.before.wide\n", result, p, p)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: result}
	return nil
}
