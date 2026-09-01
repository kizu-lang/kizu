package wasm

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
		{name: "hello", src: `fn main() { print("hello, kizu"); }`, wants: []string{
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

// TestForeignBoundaryRejectsWrongTarget checks direct and deferred calls use
// only the ABI their selected Wasm host provides.
func TestForeignBoundaryRejectsWrongTarget(t *testing.T) {
	browserImport := lowerSource(t, `extern "browser" fn notify(value: i32) -> void
fn main() -> void {
    // SAFETY: notify accepts a plain integer value.
    unsafe notify(1);
}`)
	if _, err := LowerTarget(browserImport, TargetWASI); err == nil ||
		!strings.Contains(err.Error(), "target wasm32-wasi does not support extern `browser`") {
		t.Fatalf("unexpected WASI result: %v", err)
	}

	cImport := lowerSource(t, `extern "c" fn notify(value: i32) -> void
fn main() -> void {
    // SAFETY: the declared C function accepts a plain integer value.
    unsafe notify(1);
}`)
	if _, err := LowerTarget(cImport, TargetBrowser); err == nil ||
		!strings.Contains(err.Error(), "target wasm32-browser does not support extern C") {
		t.Fatalf("unexpected browser result: %v", err)
	}

	deferredCImport := &ir.Module{Functions: []*ir.Function{{
		Name:   "main",
		Return: "void",
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{{
				Result: ir.Value{Name: "%1", Type: "void"},
				Op:     "error.try",
				Cleanups: []ir.Cleanup{{
					Op:         "call.release",
					ExternABI:  "c",
					ExternName: "release",
				}},
			}},
		}},
	}}}
	if _, err := LowerTarget(deferredCImport, TargetBrowser); err == nil ||
		!strings.Contains(err.Error(), "target wasm32-browser does not support extern C") {
		t.Fatalf("unexpected deferred browser result: %v", err)
	}
}

// TestBinaryRunsHello checks deterministic binary output through a real
// WebAssembly runtime rather than pinning encoder internals.
func TestBinaryRunsHello(t *testing.T) {
	wasmtime, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime is required for binary execution")
	}
	module, err := Lower(lowerSource(t, `fn main() { print("hello, binary"); }`))
	if err != nil {
		t.Fatalf("lower wasm failed: %v", err)
	}
	first, err := module.Binary()
	if err != nil {
		t.Fatalf("encode binary failed: %v", err)
	}
	second, err := module.Binary()
	if err != nil {
		t.Fatalf("encode binary twice failed: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("binary output changed for the same module")
	}
	path := filepath.Join(t.TempDir(), "hello.wasm")
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(wasmtime, "run", path)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("wasmtime failed: %v\n%s", err, output)
	}
	if got, want := string(output), "hello, binary\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
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
	checker := ownership.New()
	if err := checker.Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	module, err := ir.Lower(program, checker.Result())
	if err != nil {
		t.Fatalf("lower failed: %v", err)
	}
	return module
}

const functionsSource = `fn add(a: i64, b: i64) -> i64 {
    return a + b;
}
fn main() {
    print(add(1, 2));
}`

const whileSource = `fn main() {
    var i = 0;
    while i < 3 {
        print(i);
        i = i + 1;
    }
}`
