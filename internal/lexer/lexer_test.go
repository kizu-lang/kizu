package lexer

import (
	"slices"
	"testing"

	"github.com/kizu-lang/kizu/internal/token"
)

// TestNextToken checks the lexer token stream for representative syntax.
func TestNextToken(t *testing.T) {
	input := `fn main() {
    let name = "alice";
    var age = 30;
    update(&var user);
    age = age + 1;
    match Color::Red { Red => return age >= 20 ;,}
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
		{token.Var, "var"},
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
		{token.DoubleColon, "::"},
		{token.Ident, "Red"},
		{token.LBrace, "{"},
		{token.Ident, "Red"},
		{token.FatArrow, "=>"},
		{token.Return, "return"},
		{token.Ident, "age"},
		{token.GTE, ">="},
		{token.Int, "20"},
		{token.Semicolon, ";"},
		{token.Comma, ","},
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

// TestDocCommentsAttachToNextToken checks `///` blocks attach as token metadata.
func TestDocCommentsAttachToNextToken(t *testing.T) {
	l := New(`/// Parses a source file.
/// Returns an AST.
fn parse() {}
`)
	tok := l.NextToken()
	if tok.Type != token.Function {
		t.Fatalf("got %s, want fn", tok.Type)
	}
	requireComments(t, "doc", tok.DocComments, []string{"Parses a source file.", "Returns an AST."})
}

// TestDocCommentsRequireExactThreeSlashesAndAdjacency checks non-doc comments break attachment.
func TestDocCommentsRequireExactThreeSlashesAndAdjacency(t *testing.T) {
	for _, input := range []string{
		"//// ordinary\nfn main() {}\n",
		"/// doc\n\nfn main() {}\n",
		"/// doc\n// ordinary\nfn main() {}\n",
	} {
		l := New(input)
		tok := l.NextToken()
		if tok.Type != token.Function {
			t.Fatalf("got %s, want fn for %q", tok.Type, input)
		}
		requireComments(t, "doc", tok.DocComments, nil)
	}
}

// TestSafetyCommentsAttachToNextToken checks `// SAFETY:` lines survive as
// token metadata. Every other `//` line is still read and dropped.
func TestSafetyCommentsAttachToNextToken(t *testing.T) {
	l := New("// SAFETY: the caller checked the bound\n" +
		"//\tSAFETY: tabs and extra spacing still count\n" +
		"unsafe ptr_write(p, 1);\n")
	tok := l.NextToken()
	if tok.Type != token.Unsafe {
		t.Fatalf("got %s, want unsafe", tok.Type)
	}
	requireComments(t, "safety", tok.Safety, []string{
		"the caller checked the bound",
		"tabs and extra spacing still count",
	})
}

// TestSafetyCommentsRequireExactPrefixAndAdjacency checks what does not count
// as a justification: a different spelling, and one held off by a blank line.
func TestSafetyCommentsRequireExactPrefixAndAdjacency(t *testing.T) {
	for _, input := range []string{
		"// safety: lowercase\nunsafe ptr_write(p, 1);\n",
		"// SAFETY the colon is part of the prefix\nunsafe ptr_write(p, 1);\n",
		"// SAFETY: held off\n\nunsafe ptr_write(p, 1);\n",
		"// SAFETY: dropped\n// ordinary\nunsafe ptr_write(p, 1);\n",
	} {
		l := New(input)
		tok := l.NextToken()
		if tok.Type != token.Unsafe {
			t.Fatalf("got %s, want unsafe for %q", tok.Type, input)
		}
		requireComments(t, "safety", tok.Safety, nil)
	}
}

// TestSafetyAndDocCommentsStaySeparate checks the two comment kinds do not mix.
func TestSafetyAndDocCommentsStaySeparate(t *testing.T) {
	l := New("/// what it promises\n// SAFETY: why it holds\nunsafe fn poke() {}\n")
	tok := l.NextToken()
	requireComments(t, "doc", tok.DocComments, []string{"what it promises"})
	requireComments(t, "safety", tok.Safety, []string{"why it holds"})
}

// requireComments checks one kind of token comment metadata exactly.
func requireComments(t *testing.T, kind string, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s comments = %#v, want %#v", kind, got, want)
	}
}

// TestDynToken checks the dynamic dispatch keyword.
func TestDynToken(t *testing.T) {
	l := New(`dyn Writer`)
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Dyn, "dyn"},
		{token.Ident, "Writer"},
		{token.EOF, ""},
	}
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestAtToken checks compiler directive punctuation.
func TestAtToken(t *testing.T) {
	l := New(`@unsafe`)
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.At, "@"},
		{token.Unsafe, "unsafe"},
		{token.EOF, ""},
	}
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestDeferToken checks the block cleanup keyword.
func TestDeferToken(t *testing.T) {
	l := New(`defer values.deinit();`)
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Defer, "defer"},
		{token.Ident, "values"},
		{token.Dot, "."},
		{token.Ident, "deinit"},
		{token.LParen, "("},
		{token.RParen, ")"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestLogicalTokens checks and and or without changing identifiers.
func TestLogicalTokens(t *testing.T) {
	input := `age >= 20 and age < 130 or false
for 0..1 |i| { update(&value); }`
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Ident, "age"},
		{token.GTE, ">="},
		{token.Int, "20"},
		{token.And, "and"},
		{token.Ident, "age"},
		{token.LT, "<"},
		{token.Int, "130"},
		{token.Or, "or"},
		{token.False, "false"},
		{token.For, "for"},
		{token.Int, "0"},
		{token.Range, ".."},
		{token.Int, "1"},
		{token.Pipe, "|"},
		{token.Ident, "i"},
		{token.Pipe, "|"},
		{token.LBrace, "{"},
		{token.Ident, "update"},
		{token.LParen, "("},
		{token.Amp, "&"},
		{token.Ident, "value"},
		{token.RParen, ")"},
		{token.Semicolon, ";"},
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

// TestLoopControlTokens checks loop-control syntax.
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

// TestLoopIsIdentifier documents that Kizu has no loop keyword.
func TestLoopIsIdentifier(t *testing.T) {
	l := New("loop")
	tok := l.NextToken()
	if tok.Type != token.Ident || tok.Literal != "loop" {
		t.Fatalf("got (%q, %q), want IDENT loop", tok.Type, tok.Literal)
	}
}

// TestMultilineString verifies `\\`-prefixed multi-line string literals.
func TestMultilineString(t *testing.T) {
	input := "let help =\n" +
		"    \\\\Usage: kizu <command>\n" +
		"    \\\\\n" +
		"    \\\\Commands:\n" +
		"    \\\\  build    Build the project\n"
	want := "Usage: kizu <command>\n\nCommands:\n  build    Build the project"
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Let, "let"},
		{token.Ident, "help"},
		{token.Assign, "="},
		{token.String, want},
		{token.EOF, ""},
	}
	l := New(input)
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)",
				i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestMultilineStringFollowedByStatement verifies the lexer resumes after multi-line strings.
func TestMultilineStringFollowedByStatement(t *testing.T) {
	input := "let text =\n" +
		"    \\\\hello\n" +
		"    \\\\world\n" +
		";\n" +
		"print(text);"
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Let, "let"},
		{token.Ident, "text"},
		{token.Assign, "="},
		{token.String, "hello\nworld"},
		{token.Semicolon, ";"},
		{token.Ident, "print"},
		{token.LParen, "("},
		{token.Ident, "text"},
		{token.RParen, ")"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}
	l := New(input)
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)",
				i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}

// TestModuleVisibilityTokens checks import and public visibility keywords.
func TestModuleVisibilityTokens(t *testing.T) {
	input := `import app::lexer;
pub struct Token {}`
	tests := []struct {
		typ token.Type
		lit string
	}{
		{token.Import, "import"},
		{token.Ident, "app"},
		{token.DoubleColon, "::"},
		{token.Ident, "lexer"},
		{token.Semicolon, ";"},
		{token.Public, "pub"},
		{token.Struct, "struct"},
		{token.Ident, "Token"},
		{token.LBrace, "{"},
		{token.RBrace, "}"},
		{token.EOF, ""},
	}
	l := New(input)
	for i, tt := range tests {
		tok := l.NextToken()
		if tok.Type != tt.typ || tok.Literal != tt.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)",
				i, tok.Type, tok.Literal, tt.typ, tt.lit)
		}
	}
}
