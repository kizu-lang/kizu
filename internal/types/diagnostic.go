package types

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// DiagnosticError carries a type-checking error with a source span.
type DiagnosticError struct {
	Message string
	Span    ast.Span
}

// Error returns the diagnostic message.
func (e *DiagnosticError) Error() string {
	if e.Span.IsZero() {
		return e.Message
	}
	first, rest, ok := strings.Cut(e.Message, "\n")
	first = fmt.Sprintf("%s at %d:%d", first, e.Span.Start.Line, e.Span.Start.Column)
	if !ok {
		return first
	}
	return first + "\n" + rest
}

// SourceSpan returns the span associated with the diagnostic.
func (e *DiagnosticError) SourceSpan() ast.Span {
	return e.Span
}

// errorAt builds a span-aware type-checking error.
func errorAt(span ast.Span, format string, args ...any) error {
	return &DiagnosticError{Message: fmt.Sprintf(format, args...), Span: span}
}
