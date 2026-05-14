package lexer

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/token"
)

// TestNextToken checks the lexer token stream for representative syntax.
func TestNextToken(t *testing.T) {
	input := `fn main() {
    let name = "alice";
    var age = 30;
    update(&mut user);
    age = age + 1;
    match Color.Red { Red => return age >= 20 ;}
}`

	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Function, "fn"},
		{token.Ident, "main"},
		{token.LParen, "("},
		{token.RParen, ")"},
		{token.LBrace, "{"},
		{token.Let, "let"},
		{token.Ident, "name"},
		{token.Assign, "="},
		{token.String, "alice"},
		{token.Semicolon, ";"},
		{token.Var, "var"},
		{token.Ident, "age"},
		{token.Assign, "="},
		{token.Int, "30"},
		{token.Semicolon, ";"},
		{token.Ident, "update"},
		{token.LParen, "("},
		{token.Amp, "&"},
		{token.Mut, "mut"},
		{token.Ident, "user"},
		{token.RParen, ")"},
		{token.Semicolon, ";"},
		{token.Ident, "age"},
		{token.Assign, "="},
		{token.Ident, "age"},
		{token.Plus, "+"},
		{token.Int, "1"},
		{token.Semicolon, ";"},
		{token.Match, "match"},
		{token.Ident, "Color"},
		{token.Dot, "."},
		{token.Ident, "Red"},
		{token.LBrace, "{"},
		{token.Ident, "Red"},
		{token.FatArrow, "=>"},
		{token.Return, "return"},
		{token.Ident, "age"},
		{token.GTE, ">="},
		{token.Int, "20"},
		{token.Semicolon, ";"},
		{token.RBrace, "}"},
		{token.RBrace, "}"},
		{token.EOF, ""},
	}

	l := New(input)
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestLoopControlTokens checks v0.1 loop-control syntax.
func TestLoopControlTokens(t *testing.T) {
	input := `for 0..3 |i| { continue; }
break :outer;`
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.For, "for"},
		{token.Int, "0"},
		{token.Range, ".."},
		{token.Int, "3"},
		{token.Pipe, "|"},
		{token.Ident, "i"},
		{token.Pipe, "|"},
		{token.LBrace, "{"},
		{token.Continue, "continue"},
		{token.Semicolon, ";"},
		{token.RBrace, "}"},
		{token.Break, "break"},
		{token.Colon, ":"},
		{token.Ident, "outer"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}
	l := New(input)
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestLoopIsIdentifier documents that Kizu v0.1 has no loop keyword.
func TestLoopIsIdentifier(t *testing.T) {
	l := New("loop")
	tok := l.NextToken()
	if tok.Type != token.Ident || tok.Literal != "loop" {
		t.Fatalf("got (%q, %q), want IDENT loop", tok.Type, tok.Literal)
	}
}
