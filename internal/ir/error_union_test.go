package ir

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/typ"
)

// voidTailPropagationSource returns a `!void` callee's result directly. The try
// unwraps the union to a void value and the return wraps it again, so the wrap
// was handed a payload a `!void` union has no room for. `kizu run` never saw it
// -- the interpreter does not build the union -- and no example carried the
// shape, so the emitter rejected its own IR the first time a program wrote it:
// `llvm error: error.ok !void expects 0 args`.
const voidTailPropagationSource = `fn sink(v: i64) -> !void {
    print(v);
    return;
}

fn dispatch(kind: i64) -> !void {
    if kind == 1 {
        return try sink(1);
    }
    return try sink(2);
}

fn main() -> !void {
    try dispatch(1);
    return;
}`

// valueTailPropagationSource is the control: the same tail shape where the
// success type does carry a payload, so the wrap must take exactly one operand.
const valueTailPropagationSource = `fn source(v: i64) -> !i64 {
    return v;
}

fn forward(v: i64) -> !i64 {
    return try source(v);
}

fn main() -> !void {
    print(try forward(7));
    return;
}`

// TestLowerErrorOkArgCountMatchesSuccessType asserts the arity the emitter
// requires of every success wrap: a `!void` success carries no payload and every
// other success carries exactly one. Checking every error.ok in the module
// catches the class rather than the one statement that exposed it.
func TestLowerErrorOkArgCountMatchesSuccessType(t *testing.T) {
	for name, source := range map[string]string{
		"void_tail_propagation":  voidTailPropagationSource,
		"value_tail_propagation": valueTailPropagationSource,
		"error_union":            errorUnionSource,
	} {
		t.Run(name, func(t *testing.T) {
			module := lowerSource(t, source)
			types := typ.NewTable()
			wraps := 0
			for _, fn := range module.Functions {
				wraps += assertErrorOKArity(t, types, fn)
			}
			if wraps == 0 {
				t.Fatal("source lowered no success wrap, so the arity rule went unchecked")
			}
		})
	}
}

// assertErrorOKArity checks every success wrap in one function and returns how
// many it saw, so a source that stops exercising the rule fails loudly instead
// of passing vacuously.
func assertErrorOKArity(t *testing.T, types *typ.Table, fn *Function) int {
	t.Helper()
	seen := 0
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op != "error.ok" {
				continue
			}
			seen++
			success, ok := errorUnionSuccessType(types, instr.Result.Type)
			if !ok {
				t.Errorf("%s: error.ok result %s is not an error union", fn.Name, instr.Result.Type)
				continue
			}
			want := 1
			if success == "void" {
				want = 0
			}
			if len(instr.Args) != want {
				t.Errorf(
					"%s: error.ok wrapping %s takes %d args, want %d",
					fn.Name, instr.Result.Type, len(instr.Args), want,
				)
			}
		}
	}
	return seen
}
