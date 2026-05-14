package wasm

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
)

// TestEmitPhase2Subsets checks stable WASI WAT generation for core examples.
func TestEmitPhase2Subsets(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		wants []string
	}{
		{name: "hello", src: `fn main() { print("hello, kizu") }`, wants: []string{
			`(import "wasi_snapshot_preview1" "fd_write"`,
			`(data (i32.const 4096) "hello, kizu")`,
			`(func $_start (export "_start")`,
		}},
		{name: "functions", src: functionsSource, wants: []string{
			`(func $add (param $a i64) (param $b i64) (result i64)`,
			`(return (local.get $v1))`,
			`(call $__print_i64 (local.get $v3))`,
		}},
		{name: "while", src: whileSource, wants: []string{
			`(local.set $v2 (i64.const 0))`,
			`(local.set $pc (i32.const 1))`,
			`(call $__print_i64 (local.get $v2))`,
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Emit(lowerSource(t, tt.src))
			if err != nil {
				t.Fatalf("emit failed: %v", err)
			}
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
				}
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
	return module
}

const functionsSource = `fn add(a: i64, b: i64) -> i64 {
    return a + b
}
fn main() {
    print(add(1, 2))
}`

const whileSource = `fn main() {
    var i = 0
    while i < 3 {
        print(i)
        i = i + 1
    }
}`
