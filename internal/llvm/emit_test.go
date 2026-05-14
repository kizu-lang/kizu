package llvm

import (
	"errors"
	"testing"

	"tiny-safe/internal/ir"
	"tiny-safe/internal/lexer"
	"tiny-safe/internal/ownership"
	"tiny-safe/internal/parser"
	"tiny-safe/internal/types"
)

// TestEmitSnapshots checks stable LLVM IR generation for phase 2 examples.
func TestEmitSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "hello", source: `fn main() { print("hello, kizu") }`, want: helloLLVM},
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
    return a + b
}
fn main() {
    print(add(1, 2))
}`

const variablesSource = `fn main() {
    let name = "alice"
    var age = 30
    age = age + 1
    print(name)
    print(age)
}`

const ifSource = `fn main() {
    let age = 20
    if age >= 20 { print("adult") } else { print("minor") }
}`

const whileSource = `fn main() {
    var i = 0
    while i < 3 {
        print(i)
        i = i + 1
    }
}`

const helloLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [12 x i8] c"hello, kizu\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define void @main() {
entry:
  %1 = getelementptr inbounds [12 x i8], ptr @.str.0, i64 0, i64 0
  call void @kizu_print_string(ptr %1, i64 11)
  ret void
}`

const functionsLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define i64 @add(i64 %a, i64 %b) {
entry:
  %1 = add i64 %a, %b
  ret i64 %1
}

define void @main() {
entry:
  %3 = call i64 @add(i64 1, i64 2)
  call void @kizu_print_int(i64 %3)
  ret void
}`

const variablesLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [6 x i8] c"alice\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define void @main() {
entry:
  %1 = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  %4 = add i64 30, 1
  call void @kizu_print_string(ptr %1, i64 5)
  call void @kizu_print_int(i64 %4)
  ret void
}`

const ifLLVM = `; Kizu LLVM IR
@.str.0 = private unnamed_addr constant [6 x i8] c"adult\00"
@.str.1 = private unnamed_addr constant [6 x i8] c"minor\00"

declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define void @main() {
entry:
  %3 = icmp sge i64 20, 20
  br i1 %3, label %if.then.1, label %if.else.2
if.then.1:
  %4 = getelementptr inbounds [6 x i8], ptr @.str.0, i64 0, i64 0
  call void @kizu_print_string(ptr %4, i64 5)
  br label %if.end.3
if.else.2:
  %6 = getelementptr inbounds [6 x i8], ptr @.str.1, i64 0, i64 0
  call void @kizu_print_string(ptr %6, i64 5)
  br label %if.end.3
if.end.3:
  ret void
}`

const whileLLVM = `; Kizu LLVM IR
declare void @kizu_print_string(ptr, i64)
declare void @kizu_print_int(i64)
declare void @kizu_print_bool(i1)

define void @main() {
entry:
  br label %while.header.1
while.header.1:
  %2 = phi i64 [ 0, %entry ], [ %7, %while.body.2 ]
  %4 = icmp slt i64 %2, 3
  br i1 %4, label %while.body.2, label %while.end.3
while.body.2:
  call void @kizu_print_int(i64 %2)
  %7 = add i64 %2, 1
  br label %while.header.1
while.end.3:
  ret void
}`
