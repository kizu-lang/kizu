package interp

import (
	"bytes"
	"errors"
	"testing"

	"tiny-safe/internal/lexer"
	"tiny-safe/internal/parser"
)

// TestRunHello checks the print builtin on a minimal program.
func TestRunHello(t *testing.T) {
	got := runSource(t, `fn main() { print("hello, kizu") }`)
	want := "hello, kizu\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunFunctionsAndReturn checks user function calls and explicit return.
func TestRunFunctionsAndReturn(t *testing.T) {
	got := runSource(t, `fn add(a: int, b: int) -> int { return a + b }
fn main() { print(add(1, 2)) }`)
	want := "3\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunVariablesAndAssignment checks let, var, and mutable assignment.
func TestRunVariablesAndAssignment(t *testing.T) {
	got := runSource(t, `fn main() {
    let name = "alice"
    var age = 30
    age = age + 1
    print(name)
    print(age)
}`)
	want := "alice\n31\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunControlFlow checks if/else and while execution.
func TestRunControlFlow(t *testing.T) {
	got := runSource(t, `fn main() {
    let age = 20
    if age >= 20 { print("adult") } else { print("minor") }
    var i = 0
    while i < 3 {
        print(i)
        i = i + 1
    }
}`)
	want := "adult\n0\n1\n2\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRuntimeErrorChecksMutableAssignment checks a short readable runtime error.
func TestRuntimeErrorChecksMutableAssignment(t *testing.T) {
	_, err := parseAndRun(`fn main() {
    let x = 1
    x = 2
}`)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "runtime error: cannot assign to immutable binding `x`"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// runSource executes source and fails the test on parse or runtime errors.
func runSource(t *testing.T, source string) string {
	t.Helper()
	out, err := parseAndRun(source)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return out
}

// parseAndRun parses and executes source, returning captured stdout.
func parseAndRun(source string) (string, error) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", errors.New(p.Errors()[0])
	}
	var out bytes.Buffer
	err := New(&out).Run(program)
	return out.String(), err
}
