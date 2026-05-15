package project

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Diagnostic points at a primary source span and optional related spans.
type Diagnostic struct {
	Message string
	Primary Location
	Related []Related
}

// Location identifies one module file and byte range.
type Location struct {
	Module string
	File   string
	Span   ast.Span
}

// Related attaches an explanatory source location to a diagnostic.
type Related struct {
	Message  string
	Location Location
}

// DiagnosticError wraps diagnostics so callers can return them as errors.
type DiagnosticError struct {
	Diagnostics []Diagnostic
}

// Error renders diagnostics in a stable, grep-friendly text format.
func (e DiagnosticError) Error() string {
	lines := []string{}
	for _, diag := range e.Diagnostics {
		lines = append(lines, "error: "+diag.Message)
		lines = append(lines, "  --> "+formatLocation(diag.Primary))
		for _, related := range diag.Related {
			lines = append(lines, "related: "+related.Message)
			lines = append(lines, "  --> "+formatLocation(related.Location))
		}
	}
	return strings.Join(lines, "\n")
}

// formatLocation renders the source module and byte span.
func formatLocation(loc Location) string {
	if loc.Span.Empty() {
		return fmt.Sprintf("%s:%s", loc.Module, loc.File)
	}
	if loc.Span.Line > 0 && loc.Span.Column > 0 {
		return fmt.Sprintf("%s:%s:%d:%d:%d..%d",
			loc.Module, loc.File, loc.Span.Line, loc.Span.Column, loc.Span.Start, loc.Span.End)
	}
	return fmt.Sprintf("%s:%s:%d..%d", loc.Module, loc.File, loc.Span.Start, loc.Span.End)
}
