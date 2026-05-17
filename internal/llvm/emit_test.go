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

// TestEmitNativeStructAndArrayLowering checks the native selfhost layout blockers.
func TestEmitNativeStructAndArrayLowering(t *testing.T) {
	got, err := Emit(nativeStructArrayModule())
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"%struct.Token = type { ptr }",
		"call ptr @malloc(i64 8)",
		"call ptr @kizu_array_new()",
		"call void @kizu_array_append",
		"call ptr @kizu_array_at",
		"getelementptr inbounds %struct.Token",
		"load ptr",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("LLVM output missing %q:\n%s", want, got)
		}
	}
}

// TestEmitNativeTransparentAliases checks try and borrow define concrete SSA values.
func TestEmitNativeTransparentAliases(t *testing.T) {
	got, err := Emit(nativeAliasModule())
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	for _, want := range []string{
		"define i64 @count()",
		"%v2 = add i64 %v1, 0",
		"%v4 = select i1 true, ptr %v3, ptr null",
		"call i1 @uses_ref(ptr %v4)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("LLVM output missing %q:\n%s", want, got)
		}
	}
}

// TestEmitNativeUnaryMinus checks signed sentinel values stay negative.
func TestEmitNativeUnaryMinus(t *testing.T) {
	got, err := Emit(nativeUnaryMinusModule())
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if !strings.Contains(got, "%v2 = sub i64 0, 1") {
		t.Fatalf("LLVM output missing unary minus:\n%s", got)
	}
}

// nativeAliasModule returns scalar error-union and borrow alias operations.
func nativeAliasModule() *ir.Module {
	return &ir.Module{
		Structs: map[string]ir.Struct{
			"Token": {Name: "Token"},
		},
		Functions: []*ir.Function{
			{
				Name: "count", Return: "!i64",
				Blocks: []*ir.Block{{Name: "entry",
					Instrs: []*ir.Instr{{Result: ir.Value{Name: "%1", Type: "i64"},
						Op: "const", Immediate: "3"}},
					Terminator: ir.Terminator{Op: "return", Value: ir.Value{Name: "%1", Type: "i64"}},
				}},
			},
			nativeAliasMainFunction(),
			{Name: "uses_ref", Return: "bool", Params: []ir.Value{{Name: "%arg", Type: "Token"}}},
		}}
}

// nativeUnaryMinusModule returns the sentinel shape used by manifest parsing.
func nativeUnaryMinusModule() *ir.Module {
	return &ir.Module{
		Functions: []*ir.Function{
			{
				Name: "missing", Return: "i64",
				Blocks: []*ir.Block{{Name: "entry",
					Instrs: []*ir.Instr{
						{Result: ir.Value{Name: "%1", Type: "i64"}, Op: "const", Immediate: "1"},
						{Result: ir.Value{Name: "%2", Type: "i64"}, Op: "unary.-",
							Args: []ir.Value{{Name: "%1", Type: "i64"}}},
					},
					Terminator: ir.Terminator{
						Op:    "return",
						Value: ir.Value{Name: "%2", Type: "i64"},
					},
				}},
			},
		},
	}
}

// nativeAliasMainFunction returns a function using try and borrow aliases.
func nativeAliasMainFunction() *ir.Function {
	return &ir.Function{Name: "main", Return: "void", Blocks: []*ir.Block{{Name: "entry",
		Instrs: []*ir.Instr{
			{Result: ir.Value{Name: "%1", Type: "!i64"}, Op: "call.count"},
			{Result: ir.Value{Name: "%2", Type: "i64"}, Op: "error.try",
				Args: []ir.Value{{Name: "%1", Type: "!i64"}}},
			{Result: ir.Value{Name: "%3", Type: "Token"}, Op: "struct.new"},
			{Result: ir.Value{Name: "%4", Type: "Token"}, Op: "unary.&",
				Args: []ir.Value{{Name: "%3", Type: "Token"}}},
			{Result: ir.Value{Name: "%5", Type: "bool"}, Op: "call.uses_ref",
				Args: []ir.Value{{Name: "%4", Type: "Token"}}},
		},
		Terminator: ir.Terminator{Op: "return"},
	}}}
}

// nativeStructArrayModule returns a minimal IR module for aggregate lowering.
func nativeStructArrayModule() *ir.Module {
	return &ir.Module{
		Structs: map[string]ir.Struct{
			"Token": {
				Name: "Token",
				Fields: []ir.Field{
					{Name: "text", Type: "[]const u8"},
				},
			},
		},
		Functions: []*ir.Function{
			{
				Name:   "main",
				Return: "void",
				Blocks: []*ir.Block{
					{
						Name:       "entry",
						Instrs:     nativeStructArrayInstrs(),
						Terminator: ir.Terminator{Op: "return"},
					},
				},
			},
		},
	}
}

// nativeStructArrayInstrs returns aggregate and array operations for one block.
func nativeStructArrayInstrs() []*ir.Instr {
	return []*ir.Instr{
		{Result: ir.Value{Name: "%1", Type: "[]const u8"}, Op: "const", Immediate: `"let"`},
		{
			Result: ir.Value{Name: "%2", Type: "Token"},
			Op:     "struct.new",
			Fields: []ir.FieldArg{
				{Name: "text", Value: ir.Value{Name: "%1", Type: "[]const u8"}},
			},
		},
		{Result: ir.Value{Name: "%3", Type: "std.array.Array<Token>"},
			Op: "call.std.array.Array<Token>"},
		{
			Result: ir.Value{Name: "%4", Type: "!void"},
			Op:     "method.append",
			Args: []ir.Value{
				{Name: "%3", Type: "std.array.Array<Token>"},
				{Name: "%2", Type: "Token"},
			},
		},
		{Result: ir.Value{Name: "%5", Type: "i64"}, Op: "const", Immediate: "0"},
		nativeArrayAtInstr(),
		{Result: ir.Value{Name: "%7", Type: "Token"}, Op: "error.try",
			Args: []ir.Value{{Name: "%6", Type: "!Token"}}},
		{Result: ir.Value{Name: "%8", Type: "[]const u8"}, Op: "field.text",
			Args: []ir.Value{{Name: "%7", Type: "Token"}}},
		{Result: ir.Value{Name: "%9", Type: "void"}, Op: "call.print",
			Args: []ir.Value{{Name: "%8", Type: "[]const u8"}}},
	}
}

// nativeArrayAtInstr returns one Array.at instruction with receiver and index.
func nativeArrayAtInstr() *ir.Instr {
	return &ir.Instr{
		Result: ir.Value{Name: "%6", Type: "!Token"},
		Op:     "method.at",
		Args: []ir.Value{
			{Name: "%3", Type: "std.array.Array<Token>"},
			{Name: "%5", Type: "i64"},
		},
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

const helloLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [12 x i8] c"hello, kizu\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare i64 @kizu_process_arg_count()
declare ptr @kizu_process_arg(i64)
declare i1 @kizu_bytes_equal(ptr, ptr)
declare i1 @kizu_bytes_starts_with(ptr, ptr)
declare i64 @kizu_bytes_len(ptr)
declare i8 @kizu_byte_at(ptr, i64)
declare ptr @kizu_bytes_slice(ptr, i64, i64)
declare ptr @kizu_read_file(ptr)
declare i1 @kizu_file_exists(ptr)
declare ptr @kizu_path_join(ptr, ptr)

declare ptr @malloc(i64)
declare ptr @kizu_array_new()
declare void @kizu_array_append(ptr, ptr)
declare ptr @kizu_array_at(ptr, i64)
declare i64 @kizu_array_len(ptr)

define void @main() {
entry:
  %v1 = getelementptr inbounds [12 x i8], ptr @.str.0, i64 0, i64 0
  call void @kizu_print_string(ptr %v1, i64 11)
  ret void
}`

const functionsLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare i64 @kizu_process_arg_count()
declare ptr @kizu_process_arg(i64)
declare i1 @kizu_bytes_equal(ptr, ptr)
declare i1 @kizu_bytes_starts_with(ptr, ptr)
declare i64 @kizu_bytes_len(ptr)
declare i8 @kizu_byte_at(ptr, i64)
declare ptr @kizu_bytes_slice(ptr, i64, i64)
declare ptr @kizu_read_file(ptr)
declare i1 @kizu_file_exists(ptr)
declare ptr @kizu_path_join(ptr, ptr)

declare ptr @malloc(i64)
declare ptr @kizu_array_new()
declare void @kizu_array_append(ptr, ptr)
declare ptr @kizu_array_at(ptr, i64)
declare i64 @kizu_array_len(ptr)

define i64 @add(i64 %a, i64 %b) {
entry:
  %v1 = add i64 %a, %b
  ret i64 %v1
}

define void @main() {
entry:
  %v3 = call i64 @add(i64 1, i64 2)
  call void @kizu_print_int(i64 %v3)
  ret void
}`

const variablesLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [6 x i8] c"alice\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare i64 @kizu_process_arg_count()
declare ptr @kizu_process_arg(i64)
declare i1 @kizu_bytes_equal(ptr, ptr)
declare i1 @kizu_bytes_starts_with(ptr, ptr)
declare i64 @kizu_bytes_len(ptr)
declare i8 @kizu_byte_at(ptr, i64)
declare ptr @kizu_bytes_slice(ptr, i64, i64)
declare ptr @kizu_read_file(ptr)
declare i1 @kizu_file_exists(ptr)
declare ptr @kizu_path_join(ptr, ptr)

declare ptr @malloc(i64)
declare ptr @kizu_array_new()
declare void @kizu_array_append(ptr, ptr)
declare ptr @kizu_array_at(ptr, i64)
declare i64 @kizu_array_len(ptr)

define void @main() {
entry:
  %v1 = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  %v4 = add i64 30, 1
  call void @kizu_print_string(ptr %v1, i64 5)
  call void @kizu_print_int(i64 %v4)
  ret void
}`

const ifLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [6 x i8] c"adult\00"
@.str.1 = private unnamed_addr constant [6 x i8] c"minor\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare i64 @kizu_process_arg_count()
declare ptr @kizu_process_arg(i64)
declare i1 @kizu_bytes_equal(ptr, ptr)
declare i1 @kizu_bytes_starts_with(ptr, ptr)
declare i64 @kizu_bytes_len(ptr)
declare i8 @kizu_byte_at(ptr, i64)
declare ptr @kizu_bytes_slice(ptr, i64, i64)
declare ptr @kizu_read_file(ptr)
declare i1 @kizu_file_exists(ptr)
declare ptr @kizu_path_join(ptr, ptr)

declare ptr @malloc(i64)
declare ptr @kizu_array_new()
declare void @kizu_array_append(ptr, ptr)
declare ptr @kizu_array_at(ptr, i64)
declare i64 @kizu_array_len(ptr)

define void @main() {
entry:
  %v3 = icmp sge i64 20, 20
  br i1 %v3, label %if.then.1, label %if.else.2
if.then.1:
  %v4 = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  call void @kizu_print_string(ptr %v4, i64 5)
  br label %if.end.3
if.else.2:
  %v6 = getelementptr inbounds [6 x i8], ptr @.str.1, i64 0, i64 0
  call void @kizu_print_string(ptr %v6, i64 5)
  br label %if.end.3
if.end.3:
  ret void
}`

const whileLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

declare i64 @kizu_process_arg_count()
declare ptr @kizu_process_arg(i64)
declare i1 @kizu_bytes_equal(ptr, ptr)
declare i1 @kizu_bytes_starts_with(ptr, ptr)
declare i64 @kizu_bytes_len(ptr)
declare i8 @kizu_byte_at(ptr, i64)
declare ptr @kizu_bytes_slice(ptr, i64, i64)
declare ptr @kizu_read_file(ptr)
declare i1 @kizu_file_exists(ptr)
declare ptr @kizu_path_join(ptr, ptr)

declare ptr @malloc(i64)
declare ptr @kizu_array_new()
declare void @kizu_array_append(ptr, ptr)
declare ptr @kizu_array_at(ptr, i64)
declare i64 @kizu_array_len(ptr)

define void @main() {
entry:
  br label %while.header.1
while.header.1:
  %v2 = phi i64 [ 0, %entry ], [ %v7, %while.body.2 ]
  %v4 = icmp slt i64 %v2, 3
  br i1 %v4, label %while.body.2, label %while.end.3
while.body.2:
  call void @kizu_print_int(i64 %v2)
  %v7 = add i64 %v2, 1
  br label %while.header.1
while.end.3:
  ret void
}`
