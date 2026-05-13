package parser

import (
	"testing"

	"tiny-safe/internal/lexer"
)

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
