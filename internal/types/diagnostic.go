package types

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
)

// DiagnosticError carries a structured type-checking diagnostic.
type DiagnosticError = diag.Diagnostic

// errorAt builds a span-aware structured diagnostic from existing ADR-style text.
func errorAt(span ast.Span, format string, args ...any) error {
	return diag.FromText(diag.SeverityError, span, fmt.Sprintf(format, args...))
}

// errorAtCode builds a span-aware structured diagnostic with a stable code.
func errorAtCode(span ast.Span, code string, format string, args ...any) error {
	return diag.FromText(diag.SeverityError, span, fmt.Sprintf(format, args...)).WithCode(code)
}

// errorf builds one structured diagnostic without source span information.
func errorf(format string, args ...any) error {
	return diag.FromText(diag.SeverityError, ast.Span{}, fmt.Errorf(format, args...).Error())
}
