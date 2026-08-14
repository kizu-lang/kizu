package ir

import (
	"errors"
	"strings"
	"testing"
)

// TestVerifyRejects checks each rule against a module that breaks exactly it.
// Lowering is what these rules are for, but lowering cannot produce a module
// that breaks one on demand, so the cases are written by hand. That also keeps
// the rules honest: an op renamed in the lowerer without renaming it here makes
// the matching case stop failing, which fails this test.
func TestVerifyRejects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module *Module
		want   string
	}{
		{
			name: "call argument", module: callArgumentMismatch(),
			want: "call callee argument 0 in block entry is i32, declared i64",
		},
		{
			name: "struct field", module: structFieldMismatch(),
			want: "Point.x in block entry is i32, declared i64",
		},
		{
			name: "union payload", module: unionPayloadMismatch(),
			want: "Shape::Circle payload in block entry is i32, declared i64",
		},
		{
			name: "error union success", module: successWrapMismatch(),
			want: "success wrapped as !i64 in block entry is i32, declared i64",
		},
		{
			name: "return value", module: returnMismatch(),
			want: "return value in block entry is i32, declared i64",
		},
		{
			name: "missing terminator", module: missingTerminator(),
			want: "block entry has no terminator",
		},
		{
			name: "unknown branch target", module: unknownBranchTarget(),
			want: "block entry branches to nowhere, which does not exist",
		},
		{
			name: "phi dominance", module: phiWithoutDominance(),
			want: "does not dominate b",
		},
		{
			// The address exemption is the declared Passing, not the `&`. A
			// parameter taken by value is filled by its own type like any other.
			name: "borrow spelling without the passing", module: valueParamSpelledAsBorrow(),
			want: "call callee argument 0 in block entry is i64, declared &i64",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Verify(tc.module)
			if err == nil {
				t.Fatalf("Verify accepted a module breaking %s", tc.name)
			}
			if !errors.Is(err, ErrVerify) {
				t.Errorf("error does not carry ErrVerify: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestVerifyAcceptsDeclaredExceptions covers the two places a value's spelling
// differs from its slot's on purpose. Both are rules, not oversights, so a
// change that starts rejecting them fails here.
func TestVerifyAcceptsDeclaredExceptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module *Module
	}{
		// A borrow's address is never named in the IR, so a parameter read
		// through one is handed either the value the address is taken of at the
		// call, or an address the caller already held.
		{"borrow parameter takes the borrowed value", borrowedArgument("i64")},
		{"borrow parameter takes an address already held", borrowedArgument("&i64")},
		// `!T` declares no error set (ADR-0087), so it absorbs an `E!T`.
		{"undeclared error set absorbs a declared one", absorbedErrorSetReturn()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Verify(tc.module); err != nil {
				t.Errorf("Verify rejected a well-formed module: %v", err)
			}
		})
	}
}

// callee is a function taking one parameter, passed the given way, and
// returning it.
func callee(paramType string, passing Passing) *Function {
	param := Param{Name: "%a", Type: paramType, Passing: passing}
	return &Function{
		Name:   "callee",
		Params: []Param{param},
		Return: paramType,
		Blocks: []*Block{{
			Name:       "entry",
			Terminator: Terminator{Op: "return", Value: param.Value()},
		}},
	}
}

// caller is a function named for what every case checks: the one that holds
// the position under test. Its blocks are given whole so a case can break the
// terminator or the CFG as easily as an instruction's type.
func caller(returns string, blocks ...*Block) *Function {
	return &Function{Name: "caller", Return: returns, Blocks: blocks}
}

// returningBlock is the single block a rule about one instruction needs: the
// instructions, then a return of value.
func returningBlock(value Value, instrs ...*Instr) *Block {
	return &Block{
		Name:       "entry",
		Instrs:     instrs,
		Terminator: Terminator{Op: "return", Value: value},
	}
}

// callArgumentMismatch hands a callee declaring i64 an i32.
func callArgumentMismatch() *Module {
	call := &Instr{
		Result: Value{Name: "%1", Type: "i64"},
		Op:     "call.callee",
		Args:   []Value{{Name: "%wrong", Type: "i32"}},
	}
	return &Module{Functions: []*Function{
		callee("i64", PassValue),
		caller("void", returningBlock(Value{}, call)),
	}}
}

// borrowedArgument hands a callee reading through an address the given spelling
// of what it borrows.
func borrowedArgument(argType string) *Module {
	call := &Instr{
		Result: Value{Name: "%1", Type: "&i64"},
		Op:     "call.callee",
		Args:   []Value{{Name: "%value", Type: argType}},
	}
	return &Module{Functions: []*Function{
		callee("&i64", PassCopyAddress),
		caller("void", returningBlock(Value{}, call)),
	}}
}

// valueParamSpelledAsBorrow hands a by-value parameter spelled `&i64` the i64
// it points at, which only a parameter read through an address accepts.
func valueParamSpelledAsBorrow() *Module {
	call := &Instr{
		Result: Value{Name: "%1", Type: "&i64"},
		Op:     "call.callee",
		Args:   []Value{{Name: "%value", Type: "i64"}},
	}
	return &Module{Functions: []*Function{
		callee("&i64", PassValue),
		caller("void", returningBlock(Value{}, call)),
	}}
}

// structFieldMismatch puts an i32 in a field declared i64.
func structFieldMismatch() *Module {
	build := &Instr{
		Result: Value{Name: "%1", Type: "Point"},
		Op:     "struct.new",
		Fields: []FieldArg{{Name: "x", Value: Value{Name: "%wrong", Type: "i32"}}},
	}
	return &Module{
		Structs:   map[string]Struct{"Point": {Name: "Point", Fields: []Field{{Name: "x", Type: "i64"}}}},
		Functions: []*Function{caller("void", returningBlock(Value{}, build))},
	}
}

// unionPayloadMismatch carries an i32 in a variant declared i64.
func unionPayloadMismatch() *Module {
	build := &Instr{
		Result:    Value{Name: "%1", Type: "Shape"},
		Op:        "union.new",
		Immediate: "Circle",
		Args:      []Value{{Name: "%wrong", Type: "i32"}},
	}
	return &Module{
		Unions: map[string]Union{"Shape": {Name: "Shape", Variants: map[string]UnionVariant{
			"Circle": {Name: "Circle", Payload: "i64"},
		}}},
		Functions: []*Function{caller("void", returningBlock(Value{}, build))},
	}
}

// successWrapMismatch wraps an i32 as the success of an `!i64`.
func successWrapMismatch() *Module {
	wrap := &Instr{
		Result: Value{Name: "%1", Type: "!i64"},
		Op:     "error.ok",
		Args:   []Value{{Name: "%wrong", Type: "i32"}},
	}
	return &Module{Functions: []*Function{caller("void", returningBlock(Value{}, wrap))}}
}

// returnMismatch returns an i32 from a function declaring i64.
func returnMismatch() *Module {
	return &Module{Functions: []*Function{
		caller("i64", returningBlock(Value{Name: "%wrong", Type: "i32"})),
	}}
}

// absorbedErrorSetReturn returns an `FsError!i64` from an `!i64`.
func absorbedErrorSetReturn() *Module {
	return &Module{Functions: []*Function{
		caller("!i64", returningBlock(Value{Name: "%inner", Type: "FsError!i64"})),
	}}
}

// missingTerminator leaves a block without a way out.
func missingTerminator() *Module {
	return &Module{Functions: []*Function{caller("void", &Block{Name: "entry"})}}
}

// unknownBranchTarget branches to a block the function does not hold.
func unknownBranchTarget() *Module {
	return &Module{Functions: []*Function{caller("void",
		&Block{Name: "entry", Terminator: Terminator{
			Op:     "branch",
			Cond:   Value{Name: "%c", Type: "bool"},
			Target: "nowhere",
			Else:   "exit",
		}},
		&Block{Name: "exit", Terminator: Terminator{Op: "return"}},
	)}}
}

// phiWithoutDominance defines a value on one arm of a branch and lets the merge
// claim it arrives on the other arm too, where it has never been defined.
func phiWithoutDominance() *Module {
	defined := Value{Name: "%x", Type: "i64"}
	merged := Value{Name: "%p", Type: "i64"}
	return &Module{Functions: []*Function{caller("i64",
		&Block{Name: "entry", Terminator: Terminator{
			Op:     "branch",
			Cond:   Value{Name: "%c", Type: "bool"},
			Target: "a",
			Else:   "b",
		}},
		&Block{
			Name:       "a",
			Instrs:     []*Instr{{Result: defined, Op: "const", Immediate: "1"}},
			Terminator: Terminator{Op: "jump", Target: "merge"},
		},
		&Block{Name: "b", Terminator: Terminator{Op: "jump", Target: "merge"}},
		&Block{
			Name: "merge",
			Instrs: []*Instr{{Result: merged, Op: "phi", Incoming: []Incoming{
				{Block: "a", Value: defined},
				{Block: "b", Value: defined},
			}}},
			Terminator: Terminator{Op: "return", Value: merged},
		},
	)}}
}
