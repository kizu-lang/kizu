package diagnostic

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Severity identifies whether a diagnostic blocks compilation or is advisory.
type Severity int

const (
	// SeverityError reports a diagnostic that fails the current command.
	SeverityError Severity = iota + 1
	// SeverityWarning reports a diagnostic that does not fail the current command.
	SeverityWarning
)

// Diagnostic carries structured diagnostic data shared by CLI and LSP renderers.
type Diagnostic struct {
	Severity Severity
	Category string
	Message  string
	Span     ast.Span
	Notes    []string
	Help     string
}

// New constructs one structured diagnostic from its summary fields.
func New(severity Severity, category string, span ast.Span, message string) *Diagnostic {
	return &Diagnostic{
		Severity: severity,
		Category: category,
		Message:  message,
		Span:     span,
	}
}

// FromText parses ADR-style diagnostic text into structured fields.
func FromText(severity Severity, span ast.Span, text string) *Diagnostic {
	lines := strings.Split(text, "\n")
	first := ""
	if len(lines) > 0 {
		first = lines[0]
	}
	parsedSeverity, category, message := splitFirstLine(severity, first)
	diag := New(parsedSeverity, category, span, message)
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, "note: "):
			diag.Notes = append(diag.Notes, strings.TrimPrefix(line, "note: "))
		case strings.HasPrefix(line, "help: "):
			help := strings.TrimPrefix(line, "help: ")
			if diag.Help == "" {
				diag.Help = help
			} else {
				diag.Help += "\n" + help
			}
		case line == "":
			continue
		default:
			if diag.Message == "" {
				diag.Message = line
			} else {
				diag.Notes = append(diag.Notes, line)
			}
		}
	}
	return diag
}

// Error renders the diagnostic in ADR-0072 message form without CLI severity wrapping.
func (d *Diagnostic) Error() string {
	first := d.Message
	switch {
	case d.Category != "":
		first = d.Category + ": " + d.Message
	case d.Severity == SeverityWarning:
		first = "warning: " + d.Message
	}
	if !d.Span.IsZero() {
		first += fmt.Sprintf(" at %d:%d", d.Span.Start.Line, d.Span.Start.Column)
	}
	lines := []string{first}
	for _, note := range d.Notes {
		lines = append(lines, "note: "+note)
	}
	if d.Help != "" {
		for _, line := range strings.Split(d.Help, "\n") {
			lines = append(lines, "help: "+line)
		}
	}
	return strings.Join(lines, "\n")
}

// CLIError renders the diagnostic with the CLI severity prefix when needed.
func (d *Diagnostic) CLIError() string {
	if d.Severity == SeverityWarning {
		return d.Error()
	}
	return "error: " + d.Error()
}

// SourceSpan returns the primary source span for the diagnostic.
func (d *Diagnostic) SourceSpan() ast.Span {
	return d.Span
}

// SeverityLevel returns the diagnostic severity for LSP and other renderers.
func (d *Diagnostic) SeverityLevel() Severity {
	return d.Severity
}

// WithNote appends one structured note line and returns the same diagnostic.
func (d *Diagnostic) WithNote(note string) *Diagnostic {
	d.Notes = append(d.Notes, note)
	return d
}

// WithHelp records one help message and returns the same diagnostic.
func (d *Diagnostic) WithHelp(help string) *Diagnostic {
	d.Help = help
	return d
}

// splitFirstLine extracts severity, category, and summary from the first text line.
func splitFirstLine(defaultSeverity Severity, first string) (Severity, string, string) {
	if strings.HasPrefix(first, "warning: ") {
		return SeverityWarning, "", strings.TrimPrefix(first, "warning: ")
	}
	if index := strings.Index(first, ": "); index > 0 {
		category := first[:index]
		if strings.HasSuffix(category, "error") {
			return defaultSeverity, category, first[index+2:]
		}
	}
	return defaultSeverity, "", first
}
