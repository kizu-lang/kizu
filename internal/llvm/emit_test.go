package llvm

import (
	"errors"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
)

// TestEmitSnapshots checks stable LLVM IR generation for phase 2 examples.
func TestEmitSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "hello", source: `fn main() { print("hello, kizu"); }`, want: helloLLVM},
		{name: "functions", source: functionsSource, want: functionsLLVM},
		{name: "variables", source: variablesSource, want: variablesLLVM},
		{name: "if", source: ifSource, want: ifLLVM},
		{name: "while", source: whileSource, want: whileLLVM},
		{name: "struct", source: structSource, want: structLLVM},
		{name: "error_union", source: errorUnionSource, want: errorUnionLLVM},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			module := lowerSource(t, tt.source)
			got, err := Emit(module)
			if err != nil {
				t.Fatalf("emit failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestEmitRejectsUnsupportedLoweredInstructions avoids invalid placeholder IR.
func TestEmitRejectsUnsupportedLoweredInstructions(t *testing.T) {
	module := &ir.Module{Functions: []*ir.Function{{
		Name:   "main",
		Return: "void",
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{{
				Result: ir.Value{Name: "%1", Type: "i64"},
				Op:     "unknown.runtime",
			}},
			Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "void", Type: "void"}},
		}},
	}}}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject unsupported instruction")
	}
	if !strings.Contains(err.Error(), "unsupported instruction `unknown.runtime`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestEmitRejectsUnknownFieldInstruction checks malformed IR is not guessed.
func TestEmitRejectsUnknownFieldInstruction(t *testing.T) {
	module := &ir.Module{Functions: []*ir.Function{{
		Name:   "main",
		Return: "void",
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{{
				Result: ir.Value{Name: "%1", Type: "i64"},
				Op:     "field.age",
				Args:   []ir.Value{{Name: "%user", Type: "User"}},
			}},
			Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "void", Type: "void"}},
		}},
	}}}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject field instruction")
	}
	if !strings.Contains(err.Error(), "unknown struct type `User`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestEmitRejectsMalformedStructNew checks aggregate construction is explicit.
func TestEmitRejectsMalformedStructNew(t *testing.T) {
	module := &ir.Module{
		Structs: map[string]ir.Struct{
			"User": {
				Name: "User",
				Fields: []ir.Field{{
					Name: "age",
					Type: "i64",
				}},
			},
		},
		Functions: []*ir.Function{{
			Name:   "main",
			Return: "void",
			Blocks: []*ir.Block{{
				Name: "entry",
				Instrs: []*ir.Instr{{
					Result: ir.Value{Name: "%1", Type: "User"},
					Op:     "struct.new",
					Fields: []ir.FieldArg{{
						Name:  "name",
						Value: ir.Value{Name: "0", Type: "i64"},
					}},
				}},
				Terminator: ir.Terminator{
					Op:    "return",
					Value: ir.Value{Name: "void", Type: "void"},
				},
			}},
		}},
	}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject malformed struct.new")
	}
	if !strings.Contains(err.Error(), "unknown struct field `User.name`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestEmitRejectsMismatchedStructFieldValue prevents invalid insertvalue IR.
func TestEmitRejectsMismatchedStructFieldValue(t *testing.T) {
	module := &ir.Module{
		Structs: map[string]ir.Struct{
			"User": {
				Name: "User",
				Fields: []ir.Field{{
					Name: "age",
					Type: "i64",
				}},
			},
		},
		Functions: []*ir.Function{{
			Name:   "main",
			Return: "void",
			Blocks: []*ir.Block{{
				Name: "entry",
				Instrs: []*ir.Instr{{
					Result: ir.Value{Name: "%1", Type: "User"},
					Op:     "struct.new",
					Fields: []ir.FieldArg{{
						Name:  "age",
						Value: ir.Value{Name: "true", Type: "bool"},
					}},
				}},
				Terminator: ir.Terminator{
					Op:    "return",
					Value: ir.Value{Name: "void", Type: "void"},
				},
			}},
		}},
	}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject mismatched struct field value")
	}
	if !strings.Contains(err.Error(), "struct field `User.age` expects i64, got bool") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestEmitRejectsMismatchedFieldResult prevents invalid downstream value use.
func TestEmitRejectsMismatchedFieldResult(t *testing.T) {
	module := &ir.Module{
		Structs: map[string]ir.Struct{
			"User": {
				Name: "User",
				Fields: []ir.Field{{
					Name: "age",
					Type: "i64",
				}},
			},
		},
		Functions: []*ir.Function{{
			Name:   "main",
			Return: "void",
			Blocks: []*ir.Block{{
				Name: "entry",
				Instrs: []*ir.Instr{{
					Result: ir.Value{Name: "%1", Type: "bool"},
					Op:     "field.age",
					Args:   []ir.Value{{Name: "%user", Type: "User"}},
				}},
				Terminator: ir.Terminator{
					Op:    "return",
					Value: ir.Value{Name: "void", Type: "void"},
				},
			}},
		}},
	}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject mismatched field result")
	}
	if !strings.Contains(err.Error(), "field `User.age` returns i64, got bool") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestEmitHostedRuntimeErrorCallUsesOutPointerABI keeps hosted C runtime
// recoverable results off the platform aggregate return ABI.
func TestEmitHostedRuntimeErrorCallUsesOutPointerABI(t *testing.T) {
	module := &ir.Module{Functions: []*ir.Function{{
		Name:   "main",
		Return: "!void",
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{
				{
					Result: ir.Value{Name: "%io", Type: "ptr"},
					Op:     "call.std_builtin_io_blocking",
				},
				{
					Result:    ir.Value{Name: "%message", Type: "[]u8"},
					Op:        "const",
					Immediate: `"hello"`,
				},
				{
					Result: ir.Value{Name: "%write", Type: "!void"},
					Op:     "call.std_builtin_io_write_stdout",
					Args: []ir.Value{
						{Name: "%io", Type: "ptr"},
						{Name: "%message", Type: "[]u8"},
					},
				},
			},
			Terminator: ir.Terminator{
				Op:    "return",
				Value: ir.Value{Name: "%write", Type: "!void"},
			},
		}},
	}}}
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"declare void @std_builtin_io_write_stdout(ptr, ptr, ptr)",
		"%kizu.write.slot = alloca %kizu.error.void",
		"%kizu.write.arg.1 = alloca %kizu.slice.u8",
		"store %kizu.slice.u8 %kizu.message, ptr %kizu.write.arg.1",
		"call void @std_builtin_io_write_stdout(ptr %kizu.write.slot, " +
			"ptr %kizu.io, ptr %kizu.write.arg.1)",
		"%kizu.write = load %kizu.error.void, ptr %kizu.write.slot",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitTrySuccessLabelFeedsFollowingPhi keeps phi predecessors aligned when
// an error.try pseudo-branch feeds a later loop/header block.
func TestEmitTrySuccessLabelFeedsFollowingPhi(t *testing.T) {
	got := emitTestModule(t, trySuccessPhiModule())
	want := "%kizu.loop = phi i64 [ %kizu.value, %kizu.value.try.ok ], [ %kizu.next, %body ]"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant substring %q", got, want)
	}
}

// TestEmitCheckedSliceLabelFeedsFollowingPhi keeps short-circuit phi inputs
// aligned after bounds-check helper labels are emitted inside an IR block.
func TestEmitCheckedSliceLabelFeedsFollowingPhi(t *testing.T) {
	got := emitTestModule(t, checkedSlicePhiModule())
	want := "%kizu.result = phi i1 [ %kizu.ok, %kizu.byte.index.ok ], [ false, %logical.const ]"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant substring %q", got, want)
	}
}

// TestEmitMapGetLabelFeedsFollowingPhi keeps Map.get helper labels visible to
// later phi predecessors because selfhost package diagnostics use maps heavily.
func TestEmitMapGetLabelFeedsFollowingPhi(t *testing.T) {
	got := emitTestModule(t, mapGetPhiModule())
	want := "%kizu.result = phi %kizu.error.i64 [ %kizu.found, %kizu.found.array.join ], " +
		"[ %kizu.fallback, %alt ]"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant substring %q", got, want)
	}
}

// emitTestModule emits LLVM IR or fails the current test.
func emitTestModule(t *testing.T, module *ir.Module) string {
	t.Helper()
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	return got
}

// trySuccessPhiModule returns IR where error.try feeds a later phi predecessor.
func trySuccessPhiModule() *ir.Module {
	return &ir.Module{Functions: []*ir.Function{readOkFunction(), tryPhiMainFunction()}}
}

// readOkFunction returns a small fallible helper for try/phi tests.
func readOkFunction() *ir.Function {
	return &ir.Function{Name: "read", Return: "!i64", Blocks: []*ir.Block{{
		Name: "entry",
		Instrs: []*ir.Instr{
			{Result: ir.Value{Name: "%zero", Type: "i64"}, Op: "const", Immediate: "0"},
			{
				Result: ir.Value{Name: "%ok", Type: "!i64"},
				Op:     "error.ok",
				Args:   []ir.Value{{Name: "%zero", Type: "i64"}},
			},
		},
		Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "%ok", Type: "!i64"}},
	}}}
}

// tryPhiMainFunction returns a main function with a try before a loop phi.
func tryPhiMainFunction() *ir.Function {
	return &ir.Function{Name: "main", Return: "!i64", Blocks: []*ir.Block{
		tryPhiEntryBlock(),
		tryPhiHeaderBlock(),
		tryPhiBodyBlock(),
		{Name: "exit", Terminator: ir.Terminator{
			Op: "return", Value: ir.Value{Name: "%loop", Type: "i64"},
		}},
	}}
}

// tryPhiEntryBlock returns the fallible predecessor block.
func tryPhiEntryBlock() *ir.Block {
	return &ir.Block{Name: "entry", Instrs: []*ir.Instr{
		{Result: ir.Value{Name: "%read", Type: "!i64"}, Op: "call.read"},
		{
			Result: ir.Value{Name: "%value", Type: "i64"},
			Op:     "error.try",
			Args:   []ir.Value{{Name: "%read", Type: "!i64"}},
		},
	}, Terminator: ir.Terminator{Op: "jump", Target: "header"}}
}

// tryPhiHeaderBlock returns the loop header phi block.
func tryPhiHeaderBlock() *ir.Block {
	return &ir.Block{Name: "header", Instrs: []*ir.Instr{{
		Result: ir.Value{Name: "%loop", Type: "i64"},
		Op:     "phi",
		Incoming: []ir.Incoming{
			{Block: "entry", Value: ir.Value{Name: "%value", Type: "i64"}},
			{Block: "body", Value: ir.Value{Name: "%next", Type: "i64"}},
		},
	}}, Terminator: ir.Terminator{
		Op: "branch", Cond: ir.Value{Name: "false", Type: "bool"}, Target: "body", Else: "exit",
	}}
}

// tryPhiBodyBlock returns the loop backedge block.
func tryPhiBodyBlock() *ir.Block {
	return &ir.Block{Name: "body", Instrs: []*ir.Instr{
		{Result: ir.Value{Name: "%one", Type: "i64"}, Op: "const", Immediate: "1"},
		{
			Result: ir.Value{Name: "%next", Type: "i64"},
			Op:     "binary.+",
			Args: []ir.Value{
				{Name: "%loop", Type: "i64"},
				{Name: "%one", Type: "i64"},
			},
		},
	}, Terminator: ir.Terminator{Op: "jump", Target: "header"}}
}

// checkedSlicePhiModule returns IR where slice bounds labels feed a phi.
func checkedSlicePhiModule() *ir.Module {
	return &ir.Module{Functions: []*ir.Function{{
		Name:   "is_word_at",
		Params: checkedSlicePhiParams(),
		Return: "bool",
		Blocks: []*ir.Block{
			checkedSlicePhiEntryBlock(),
			checkedSlicePhiRightBlock(),
			{Name: "logical.const", Terminator: ir.Terminator{Op: "jump", Target: "logical.end"}},
			checkedSlicePhiEndBlock(),
		},
	}}}
}

// checkedSlicePhiParams returns parameters for checkedSlicePhiModule.
func checkedSlicePhiParams() []ir.Value {
	return []ir.Value{
		{Name: "%source", Type: "[]u8"},
		{Name: "%index", Type: "i64"},
		{Name: "%target", Type: "u8"},
	}
}

// checkedSlicePhiEntryBlock returns the short-circuit entry block.
func checkedSlicePhiEntryBlock() *ir.Block {
	return &ir.Block{Name: "entry", Terminator: ir.Terminator{
		Op:     "branch",
		Cond:   ir.Value{Name: "true", Type: "bool"},
		Target: "logical.right",
		Else:   "logical.const",
	}}
}

// checkedSlicePhiRightBlock returns the block that emits a slice bounds check.
func checkedSlicePhiRightBlock() *ir.Block {
	return &ir.Block{Name: "logical.right", Instrs: []*ir.Instr{
		{
			Result: ir.Value{Name: "%byte", Type: "u8"},
			Op:     "slice.index",
			Args: []ir.Value{
				{Name: "%source", Type: "[]u8"},
				{Name: "%index", Type: "i64"},
			},
		},
		{
			Result: ir.Value{Name: "%ok", Type: "bool"},
			Op:     "binary.==",
			Args: []ir.Value{
				{Name: "%byte", Type: "u8"},
				{Name: "%target", Type: "u8"},
			},
		},
	}, Terminator: ir.Terminator{Op: "jump", Target: "logical.end"}}
}

// checkedSlicePhiEndBlock returns the merge phi block.
func checkedSlicePhiEndBlock() *ir.Block {
	return &ir.Block{Name: "logical.end", Instrs: []*ir.Instr{{
		Result: ir.Value{Name: "%result", Type: "bool"},
		Op:     "phi",
		Incoming: []ir.Incoming{
			{Block: "logical.right", Value: ir.Value{Name: "%ok", Type: "bool"}},
			{Block: "logical.const", Value: ir.Value{Name: "false", Type: "bool"}},
		},
	}}, Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "%result", Type: "bool"}}}
}

// mapGetPhiModule returns IR where Map.get emits a helper join before a phi.
func mapGetPhiModule() *ir.Module {
	mapType := "std::map::Map<[]u8,i64>"
	return &ir.Module{Functions: []*ir.Function{{
		Name:   "lookup",
		Params: []ir.Value{{Name: "%map", Type: mapType}},
		Return: "!i64",
		Blocks: []*ir.Block{
			mapGetPhiEntryBlock(mapType),
			mapGetPhiAltBlock(),
			mapGetPhiMergeBlock(),
		},
	}}}
}

// mapGetPhiEntryBlock returns the map lookup predecessor block.
func mapGetPhiEntryBlock(mapType string) *ir.Block {
	return &ir.Block{Name: "entry", Instrs: []*ir.Instr{
		{Result: ir.Value{Name: "%key", Type: "[]u8"}, Op: "const", Immediate: `"answer"`},
		{
			Result: ir.Value{Name: "%found", Type: "!i64"},
			Op:     "map.get",
			Args: []ir.Value{
				{Name: "%map", Type: mapType},
				{Name: "%key", Type: "[]u8"},
			},
		},
	}, Terminator: ir.Terminator{Op: "jump", Target: "merge"}}
}

// mapGetPhiAltBlock returns an ordinary fallback predecessor block.
func mapGetPhiAltBlock() *ir.Block {
	return &ir.Block{Name: "alt", Instrs: []*ir.Instr{
		{Result: ir.Value{Name: "%zero", Type: "i64"}, Op: "const", Immediate: "0"},
		{
			Result: ir.Value{Name: "%fallback", Type: "!i64"},
			Op:     "error.ok",
			Args:   []ir.Value{{Name: "%zero", Type: "i64"}},
		},
	}, Terminator: ir.Terminator{Op: "jump", Target: "merge"}}
}

// mapGetPhiMergeBlock returns the merge block that consumes Map.get output.
func mapGetPhiMergeBlock() *ir.Block {
	return &ir.Block{Name: "merge", Instrs: []*ir.Instr{{
		Result: ir.Value{Name: "%result", Type: "!i64"},
		Op:     "phi",
		Incoming: []ir.Incoming{
			{Block: "entry", Value: ir.Value{Name: "%found", Type: "!i64"}},
			{Block: "alt", Value: ir.Value{Name: "%fallback", Type: "!i64"}},
		},
	}}, Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "%result", Type: "!i64"}}}
}

// TestEmitErrorUnionFailure checks error("message") lowers to failed !T.
func TestEmitErrorUnionFailure(t *testing.T) {
	module := lowerSource(t, errorUnionFailureSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"define %kizu.error.i64 @read()",
		"%kizu.2 = insertvalue %kizu.error.i64 %kizu.2.base, %kizu.slice.u8 %kizu.1, 2",
		"ret %kizu.error.i64 %kizu.2",
		"kizu.2.try.err:\n  ret i32 1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitErrorUnionSliceSuccess checks ![]u8 carries the full slice value.
func TestEmitErrorUnionSliceSuccess(t *testing.T) {
	module := lowerSource(t, errorUnionSliceSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"%kizu.error.slice.u8 = type { i8, %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.2 = insertvalue %kizu.error.slice.u8 %kizu.2.ok, %kizu.slice.u8 %kizu.1, 1",
		"%kizu.2 = extractvalue %kizu.error.slice.u8 %kizu.1, 1",
		"call void @kizu_print_string(ptr %kizu.print.slice.ptr.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitSliceFunctionABI checks []u8 is passed and returned as a slice value.
func TestEmitSliceFunctionABI(t *testing.T) {
	module := lowerSource(t, sliceFunctionSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"%kizu.slice.u8 = type { ptr, i64 }",
		"define %kizu.slice.u8 @identity(%kizu.slice.u8 %kizu.value)",
		"ret %kizu.slice.u8 %kizu.value",
		"define %kizu.slice.u8 @message()",
		"%kizu.1 = insertvalue %kizu.slice.u8 %kizu.1.base, i64 5, 1",
		"%kizu.2 = call %kizu.slice.u8 @identity(%kizu.slice.u8 %kizu.1)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitCheckedSliceAccess lowers indexing and slicing with bounds traps.
func TestEmitCheckedSliceAccess(t *testing.T) {
	module := lowerSource(t, sliceAccessSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"declare void @llvm.trap()",
		"br i1 %kizu.3.index.bad, label %kizu.3.index.oob, label %kizu.3.index.ok",
		"kizu.3.index.oob:\n  call void @llvm.trap()\n  unreachable",
		"%kizu.3 = load i8, ptr %kizu.3.elem.ptr",
		"br i1 %kizu.6.bad, label %kizu.6.slice.oob, label %kizu.6.slice.ok",
		"%kizu.6 = insertvalue %kizu.slice.u8 %kizu.6.base, i64 %kizu.6.len, 1",
		"= zext i8 %kizu.3 to i64",
		"call void @kizu_print_string(ptr %kizu.print.slice.ptr.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitErrorUnionPropagatesMessage checks try preserves failure diagnostics.
func TestEmitErrorUnionPropagatesMessage(t *testing.T) {
	module := lowerSource(t, errorUnionMessagePropagationSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"= extractvalue %kizu.error.i64 %kizu.1, 2",
		"= insertvalue %kizu.error.void zeroinitializer, i8 0, 0",
		"= insertvalue %kizu.error.void %kizu.try.err.",
		"%kizu.slice.u8 %kizu.try.err.",
		"ret %kizu.error.void %kizu.try.err.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitTypedErrorCast adapts untyped !T into a concrete typed Error!T payload.
func TestEmitTypedErrorCast(t *testing.T) {
	module := lowerSource(t, typedErrorCastSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"%kizu.error.CompileError.i64 = type { i8, i64, %kizu.union.CompileError }",
		"%kizu.2.failure.extracted = extractvalue %kizu.error.i64 %kizu.1, 2",
		"%kizu.2.failure.typed.box = call ptr @kizu_union_box",
		"%kizu.2 = insertvalue %kizu.error.CompileError.i64 %kizu.2.value.base, " +
			"%kizu.union.CompileError %kizu.2.failure.typed, 2",
		"ret %kizu.error.CompileError.i64 %kizu.4",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitRejectsMismatchedErrorPropagation keeps malformed IR from crossing typed errors.
func TestEmitRejectsMismatchedErrorPropagation(t *testing.T) {
	module := &ir.Module{Functions: []*ir.Function{{
		Name:   "main",
		Return: "NetworkError!void",
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{{
				Result: ir.Value{Name: "%1", Type: "i64"},
				Op:     "error.try",
				Args:   []ir.Value{{Name: "%source", Type: "ConfigError!i64"}},
			}},
			Terminator: ir.Terminator{
				Op:    "return",
				Value: ir.Value{Name: "void", Type: "void"},
			},
		}},
	}}}
	_, err := Emit(module)
	if err == nil {
		t.Fatal("expected emit to reject mismatched error propagation")
	}
	want := "error.try cannot propagate ConfigError!i64 into NetworkError!void"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q", err.Error())
	}
}

// lowerSource parses, checks, and lowers a source snippet.
func lowerSource(t *testing.T, source string) *ir.Module {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	module, err := ir.Lower(program)
	if err != nil {
		t.Fatalf("lower failed: %v", err)
	}
	if module == nil {
		t.Fatal(errors.New("nil module"))
	}
	return module
}

const functionsSource = `fn add(a: i64, b: i64) -> i64 {
    return a + b;
}
fn main() {
    print(add(1, 2));
}`

const variablesSource = `fn main() {
    let name = "alice";
    var age = 30;
    age = age + 1;
    print(name);
    print(age);
}`

const ifSource = `fn main() {
    let age = 20;
    if age >= 20 { print("adult"); } else { print("minor"); }
}`

const whileSource = `fn main() {
    var i = 0;
    while i < 3 {
        print(i);
        i = i + 1;
    }
}`

const structSource = `struct User { age: i64, }
fn main() {
    let user = User { age: 30 };
    print(user.age);
}`

const errorUnionSource = `fn read() -> !i64 {
    return 1;
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`

const errorUnionFailureSource = `fn read() -> !i64 {
    return error("bad");
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`

const errorUnionMessagePropagationSource = `fn read() -> !i64 {
    return error("bad");
}
fn wrap() -> !void {
    let value = try read();
    print(value);
    return;
}
fn main() -> !void {
    try wrap();
    return;
}`

const typedErrorCastSource = `union CompileError {
    Message([]u8),
}
fn lower(ok: bool) -> !i64 {
    if ok {
        return 7;
    }
    return error("bad");
}
fn parse(ok: bool) -> CompileError!i64 {
    let value = try cast<CompileError!i64>(lower(ok));
    return value;
}
fn main() -> CompileError!void {
    let value = try parse(true);
    print(value);
    return;
}`

const errorUnionSliceSource = `fn read() -> ![]u8 {
    return "bad";
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`

const sliceFunctionSource = `fn identity(value: []u8) -> []u8 {
    return value;
}
fn message() -> []u8 {
    return "hello";
}
fn main() {
    print(identity(message()));
}`

const sliceAccessSource = `fn main() {
    let bytes = "hello";
    let byte = bytes[1];
    let part = bytes[1..4];
    print(byte);
    print(part);
}`

const helloLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
@.str.0 = private unnamed_addr constant [12 x i8] c"hello, kizu\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.1.ptr = getelementptr inbounds [12 x i8], ptr @.str.0, i64 0, i64 0
  %kizu.1.base = insertvalue %kizu.slice.u8 poison, ptr %kizu.1.ptr, 0
  %kizu.1 = insertvalue %kizu.slice.u8 %kizu.1.base, i64 11, 1
  %kizu.print.slice.ptr.1 = extractvalue %kizu.slice.u8 %kizu.1, 0
  %kizu.print.slice.len.2 = extractvalue %kizu.slice.u8 %kizu.1, 1
  call void @kizu_print_string(ptr %kizu.print.slice.ptr.1, i64 %kizu.print.slice.len.2)
  ret i32 0
}`

const functionsLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i64 @add(i64 %kizu.a, i64 %kizu.b) {
entry:
  %kizu.1 = add i64 %kizu.a, %kizu.b
  ret i64 %kizu.1
}

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.3 = call i64 @add(i64 1, i64 2)
  call void @kizu_print_int(i64 %kizu.3)
  ret i32 0
}`

const variablesLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
@.str.0 = private unnamed_addr constant [6 x i8] c"alice\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.1.ptr = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  %kizu.1.base = insertvalue %kizu.slice.u8 poison, ptr %kizu.1.ptr, 0
  %kizu.1 = insertvalue %kizu.slice.u8 %kizu.1.base, i64 5, 1
  %kizu.4 = add i64 30, 1
  %kizu.print.slice.ptr.1 = extractvalue %kizu.slice.u8 %kizu.1, 0
  %kizu.print.slice.len.2 = extractvalue %kizu.slice.u8 %kizu.1, 1
  call void @kizu_print_string(ptr %kizu.print.slice.ptr.1, i64 %kizu.print.slice.len.2)
  call void @kizu_print_int(i64 %kizu.4)
  ret i32 0
}`

const ifLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
@.str.0 = private unnamed_addr constant [6 x i8] c"adult\00"
@.str.1 = private unnamed_addr constant [6 x i8] c"minor\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.3 = icmp sge i64 20, 20
  br i1 %kizu.3, label %if.then.1, label %if.else.2
if.then.1:
  %kizu.4.ptr = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  %kizu.4.base = insertvalue %kizu.slice.u8 poison, ptr %kizu.4.ptr, 0
  %kizu.4 = insertvalue %kizu.slice.u8 %kizu.4.base, i64 5, 1
  %kizu.print.slice.ptr.1 = extractvalue %kizu.slice.u8 %kizu.4, 0
  %kizu.print.slice.len.2 = extractvalue %kizu.slice.u8 %kizu.4, 1
  call void @kizu_print_string(ptr %kizu.print.slice.ptr.1, i64 %kizu.print.slice.len.2)
  br label %if.end.3
if.else.2:
  %kizu.6.ptr = getelementptr inbounds [6 x i8], ptr @.str.1, i64 0, i64 0
  %kizu.6.base = insertvalue %kizu.slice.u8 poison, ptr %kizu.6.ptr, 0
  %kizu.6 = insertvalue %kizu.slice.u8 %kizu.6.base, i64 5, 1
  %kizu.print.slice.ptr.3 = extractvalue %kizu.slice.u8 %kizu.6, 0
  %kizu.print.slice.len.4 = extractvalue %kizu.slice.u8 %kizu.6, 1
  call void @kizu_print_string(ptr %kizu.print.slice.ptr.3, i64 %kizu.print.slice.len.4)
  br label %if.end.3
if.end.3:
  ret i32 0
}`

const whileLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  br label %while.header.1
while.header.1:
  %kizu.2 = phi i64 [ 0, %entry ], [ %kizu.7, %while.body.2 ]
  %kizu.4 = icmp slt i64 %kizu.2, 3
  br i1 %kizu.4, label %while.body.2, label %while.end.3
while.body.2:
  call void @kizu_print_int(i64 %kizu.2)
  %kizu.7 = add i64 %kizu.2, 1
  br label %while.header.1
while.end.3:
  ret i32 0
}`

const structLLVM = `; Kizu LLVM IR
%kizu.struct.User = type { i64 }

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.2 = insertvalue %kizu.struct.User zeroinitializer, i64 30, 0
  %kizu.3 = extractvalue %kizu.struct.User %kizu.2, 0
  call void @kizu_print_int(i64 %kizu.3)
  ret i32 0
}`

const errorUnionLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
%kizu.error.i64 = type { i8, i64, %kizu.slice.u8 }
%kizu.error.void = type { i8, %kizu.slice.u8 }

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare void @kizu_runtime_init_args(i32, ptr)

define %kizu.error.i64 @read() {
entry:
  %kizu.2.ok = insertvalue %kizu.error.i64 zeroinitializer, i8 1, 0
  %kizu.2 = insertvalue %kizu.error.i64 %kizu.2.ok, i64 1, 1
  ret %kizu.error.i64 %kizu.2
}

define i32 @main(i32 %kizu.argc, ptr %kizu.argv) {
entry:
  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)
  %kizu.1 = call %kizu.error.i64 @read()
  %kizu.2.ok = extractvalue %kizu.error.i64 %kizu.1, 0
  %kizu.2.ok.bool = icmp ne i8 %kizu.2.ok, 0
  br i1 %kizu.2.ok.bool, label %kizu.2.try.ok, label %kizu.2.try.err
kizu.2.try.err:
  ret i32 1
kizu.2.try.ok:
  %kizu.2 = extractvalue %kizu.error.i64 %kizu.1, 1
  call void @kizu_print_int(i64 %kizu.2)
  %kizu.4 = insertvalue %kizu.error.void zeroinitializer, i8 1, 0
  %kizu.main.ok.1 = extractvalue %kizu.error.void %kizu.4, 0
  %kizu.main.ok.1.bool = icmp ne i8 %kizu.main.ok.1, 0
  %kizu.main.code.2 = select i1 %kizu.main.ok.1.bool, i32 0, i32 1
  ret i32 %kizu.main.code.2
}`
