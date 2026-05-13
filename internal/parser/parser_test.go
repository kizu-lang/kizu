package parser

import (
	"testing"

	"tiny-safe/internal/lexer"
)

// TestParseHello checks that a minimal program parses cleanly.
func TestParseHello(t *testing.T) {
	input := `fn main() {
    print("hello, kizu")
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("got %d declarations, want 1", len(program.Decls))
	}
	got := program.String()
	want := `fn main() { print("hello, kizu") }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFunctionWithParamsAndReturn checks typed parameters and return parsing.
func TestParseFunctionWithParamsAndReturn(t *testing.T) {
	input := `fn add(a: int, b: int) -> int {
    return a + b
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn add(a: int, b: int) -> int { return (a + b) }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseIfAndWhile checks Phase 2 control-flow statement parsing.
func TestParseIfAndWhile(t *testing.T) {
	input := `fn main() {
    var i = 0
    if i < 3 {
        print(i)
    } else {
        print("done")
    }
    while i < 3 {
        i = i + 1
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn main() { var i = 0; if (i < 3) { print(i) } else { print("done") }; ` +
		`while (i < 3) { i = (i + 1) } }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseStructDecl checks top-level struct field parsing.
func TestParseStructDecl(t *testing.T) {
	input := `struct User {
    name: string
    age: int
}
fn main() {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `struct User { name: string; age: int }
fn main() {  }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRejectsExplicitLifetime checks that lifetime syntax is not accepted.
func TestParseRejectsExplicitLifetime(t *testing.T) {
	input := `fn show(s: borrow 'a string) {}`
	p := New(lexer.New(input))
	_ = p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("expected parser error")
	}
}
