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
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "arena", source: arenaSource, want: "`arena.new` is not supported"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			module := lowerSource(t, tt.source)
			_, err := Emit(module)
			if err == nil {
				t.Fatal("expected emit to reject unsupported instruction")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tt.want)
			}
		})
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
		"try.err.2:\n  ret i32 1",
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
		"%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.2 = insertvalue %kizu.error.slice.u8 %kizu.2.ok, %kizu.slice.u8 %kizu.1, 1",
		"%kizu.2 = extractvalue %kizu.error.slice.u8 %kizu.1, 1",
		"call void @kizu_print_string(ptr %kizu.print.slice.ptr.3, i64 %kizu.print.slice.len.4)",
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
		"define %kizu.slice.u8 @identity(%kizu.slice.u8 %value)",
		"ret %kizu.slice.u8 %value",
		"define %kizu.slice.u8 @message()",
		"%kizu.1 = insertvalue %kizu.slice.u8 %kizu.1.base, i64 5, 1",
		"%kizu.2 = call %kizu.slice.u8 @identity(%kizu.slice.u8 %kizu.1)",
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
		"%kizu.try.err.message.3 = extractvalue %kizu.error.i64 %kizu.1, 2",
		"%kizu.try.err.4.base = insertvalue %kizu.error.void zeroinitializer, i1 false, 0",
		"%kizu.try.err.4 = insertvalue %kizu.error.void %kizu.try.err.4.base, " +
			"%kizu.slice.u8 %kizu.try.err.message.3, 1",
		"ret %kizu.error.void %kizu.try.err.4",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring %q", got, want)
		}
	}
}

// TestEmitTypedErrorCast adapts untyped !T into typed Error!T explicitly.
func TestEmitTypedErrorCast(t *testing.T) {
	module := lowerSource(t, typedErrorCastSource)
	got, err := Emit(module)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"%kizu.error.CompileError.i64 = type { i1, i64, %kizu.slice.u8 }",
		"%kizu.2.message = extractvalue %kizu.error.i64 %kizu.1, 2",
		"%kizu.2 = insertvalue %kizu.error.CompileError.i64 %kizu.2.value.base, " +
			"%kizu.slice.u8 %kizu.2.message, 2",
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

const arenaSource = `struct User { age: i64, }
fn main(allocator: Allocator) {
    let users = std::arena::Arena<User>(allocator);
    print(1);
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

const helloLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
@.str.0 = private unnamed_addr constant [12 x i8] c"hello, kizu\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define i32 @main() {
entry:
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

define i64 @add(i64 %a, i64 %b) {
entry:
  %kizu.1 = add i64 %a, %b
  ret i64 %kizu.1
}

define i32 @main() {
entry:
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

define i32 @main() {
entry:
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

define i32 @main() {
entry:
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

define i32 @main() {
entry:
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

define i32 @main() {
entry:
  %kizu.2 = insertvalue %kizu.struct.User zeroinitializer, i64 30, 0
  %kizu.3 = extractvalue %kizu.struct.User %kizu.2, 0
  call void @kizu_print_int(i64 %kizu.3)
  ret i32 0
}`

const errorUnionLLVM = `; Kizu LLVM IR
%kizu.slice.u8 = type { ptr, i64 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.void = type { i1, %kizu.slice.u8 }

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define %kizu.error.i64 @read() {
entry:
  %kizu.2.ok = insertvalue %kizu.error.i64 zeroinitializer, i1 true, 0
  %kizu.2 = insertvalue %kizu.error.i64 %kizu.2.ok, i64 1, 1
  ret %kizu.error.i64 %kizu.2
}

define i32 @main() {
entry:
  %kizu.1 = call %kizu.error.i64 @read()
  %kizu.2.ok = extractvalue %kizu.error.i64 %kizu.1, 0
  br i1 %kizu.2.ok, label %try.ok.1, label %try.err.2
try.err.2:
  ret i32 1
try.ok.1:
  %kizu.2 = extractvalue %kizu.error.i64 %kizu.1, 1
  call void @kizu_print_int(i64 %kizu.2)
  %kizu.4 = insertvalue %kizu.error.void zeroinitializer, i1 true, 0
  %kizu.main.ok.3 = extractvalue %kizu.error.void %kizu.4, 0
  %kizu.main.code.4 = select i1 %kizu.main.ok.3, i32 0, i32 1
  ret i32 %kizu.main.code.4
}`
