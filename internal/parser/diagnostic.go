package parser

import (
	"unicode/utf8"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/token"
)

// Diagnostic carries one parser failure with a source span.
type Diagnostic = diag.Diagnostic

// diagnosticAtToken builds a half-open span from the current parser token.
func diagnosticAtToken(tok token.Token, message string) Diagnostic {
	width := utf8.RuneCountInString(tok.Literal)
	if width < 1 {
		width = 1
	}
	start := ast.Position{Line: tok.Line, Column: tok.Column}
	end := ast.Position{Line: tok.Line, Column: tok.Column + width}
	return *diag.New(diag.SeverityError, "", ast.Span{Start: start, End: end}, message)
}
