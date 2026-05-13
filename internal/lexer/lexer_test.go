package lexer

import (
	"testing"

	"tiny-safe/internal/token"
)

func TestNextToken(t *testing.T) {
	input := `fn main() {
    let name = "alice"
    var age = 30
    age = age + 1
    return age >= 20
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
		{token.Var, "var"},
		{token.Ident, "age"},
		{token.Assign, "="},
		{token.Int, "30"},
		{token.Ident, "age"},
		{token.Assign, "="},
		{token.Ident, "age"},
		{token.Plus, "+"},
		{token.Int, "1"},
		{token.Return, "return"},
		{token.Ident, "age"},
		{token.GTE, ">="},
		{token.Int, "20"},
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
