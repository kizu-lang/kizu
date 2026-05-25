// Package diagnostic defines structured compiler diagnostics and text rendering.
package diagnostic

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Severity describes whether a diagnostic stops compilation.
type Severity string

const (
	// SeverityError stops the current compile/check/run operation.
	SeverityError Severity = "error"
	// SeverityWarning reports a non-fatal issue.
	SeverityWarning Severity = "warning"
)

// Category names the first user-facing diagnostic prefix.
type Category string

const (
	// CategoryParse is used by parser diagnostics rendered with the generic error prefix.
	CategoryParse Category = "error"
	// CategoryType is used by type-checker diagnostics.
	CategoryType Category = "type error"
	// CategoryMove is used by ownership / move-checker diagnostics.
	CategoryMove Category = "move error"
	// CategoryUnsafe is used by unsafe capability diagnostics.
	CategoryUnsafe Category = "unsafe error"
)

// DetailKind names a structured follow-up line.
type DetailKind string

const (
	// DetailNote explains why the compiler reached the diagnostic.
	DetailNote DetailKind = "note"
	// DetailHelp describes an actionable next step.
	DetailHelp DetailKind = "help"
)

// Detail is one structured follow-up line such as note or help.
type Detail struct {
	Kind    DetailKind
	Message string
}

// RelatedSpan points at an additional source range related to a diagnostic.
type RelatedSpan struct {
	Message string
	Span    ast.Span
}

// Diagnostic is the compiler-internal diagnostic model.
type Diagnostic struct {
	Severity Severity
	Category Category
	Code     string
	Summary  string
	Primary  ast.Span
	Details  []Detail
	Related  []RelatedSpan
}

// New builds a diagnostic without rendering it.
func New(
	severity Severity,
	category Category,
	code string,
	summary string,
	primary ast.Span,
) Diagnostic {
	return Diagnostic{
		Severity: severity,
		Category: category,
		Code:     code,
		Summary:  summary,
		Primary:  primary,
	}
}

// FromRendered parses an existing rendered message into a structured diagnostic.
func FromRendered(message string, primary ast.Span) Diagnostic {
	lines := strings.Split(message, "\n")
	summary := ""
	if len(lines) > 0 {
		summary = lines[0]
	}
	category, summary := splitCategory(summary)
	diag := New(SeverityError, category, "", summary, primary)
	for _, line := range lines[1:] {
		kind, detail, ok := splitDetail(line)
		if ok {
			diag.Details = append(diag.Details, Detail{Kind: kind, Message: detail})
			continue
		}
		if line != "" {
			diag.Details = append(diag.Details, Detail{Message: line})
		}
	}
	return diag
}

// WithNote appends explanatory context.
func (d Diagnostic) WithNote(message string) Diagnostic {
	d.Details = append(d.Details, Detail{Kind: DetailNote, Message: message})
	return d
}

// WithNotef appends formatted explanatory context.
func (d Diagnostic) WithNotef(format string, args ...any) Diagnostic {
	return d.WithNote(fmt.Sprintf(format, args...))
}

// WithHelp appends an actionable next step.
func (d Diagnostic) WithHelp(message string) Diagnostic {
	d.Details = append(d.Details, Detail{Kind: DetailHelp, Message: message})
	return d
}

// WithHelpf appends a formatted actionable next step.
func (d Diagnostic) WithHelpf(format string, args ...any) Diagnostic {
	return d.WithHelp(fmt.Sprintf(format, args...))
}

// AsError returns d as an error value.
func (d Diagnostic) AsError() *Error {
	return &Error{Diagnostic: d, Span: d.Primary}
}

// RenderText renders the stable human-readable diagnostic text.
func (d Diagnostic) RenderText() string {
	first := d.Summary
	if d.Category != "" {
		first = string(d.Category) + ": " + first
	}
	if !d.Primary.IsZero() {
		first = fmt.Sprintf("%s at %d:%d", first, d.Primary.Start.Line, d.Primary.Start.Column)
	}
	lines := []string{first}
	for _, detail := range d.Details {
		if detail.Kind == "" {
			lines = append(lines, detail.Message)
			continue
		}
		lines = append(lines, string(detail.Kind)+": "+detail.Message)
	}
	return strings.Join(lines, "\n")
}

// Error carries a structured compiler diagnostic as an error.
type Error struct {
	Diagnostic Diagnostic
	Span       ast.Span
}

// Error renders the diagnostic as stable text.
func (e *Error) Error() string {
	return e.Diagnostic.RenderText()
}

// SourceSpan returns the primary span associated with the diagnostic.
func (e *Error) SourceSpan() ast.Span {
	return e.Span
}

// CompilerDiagnostic returns the structured diagnostic payload.
func (e *Error) CompilerDiagnostic() Diagnostic {
	diag := e.Diagnostic
	diag.Primary = e.Span
	return diag
}

// splitCategory removes a known rendered category prefix from summary.
func splitCategory(summary string) (Category, string) {
	for _, category := range []Category{
		CategoryType,
		CategoryMove,
		CategoryUnsafe,
		CategoryParse,
	} {
		prefix := string(category) + ": "
		if strings.HasPrefix(summary, prefix) {
			return category, strings.TrimPrefix(summary, prefix)
		}
	}
	return "", summary
}

// splitDetail removes a known rendered detail prefix from line.
func splitDetail(line string) (DetailKind, string, bool) {
	for _, kind := range []DetailKind{DetailNote, DetailHelp} {
		prefix := string(kind) + ": "
		if strings.HasPrefix(line, prefix) {
			return kind, strings.TrimPrefix(line, prefix), true
		}
	}
	return "", "", false
}
