package diagnostic

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/quote"
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
	Code     string
	Message  string
	Span     ast.Span
	Notes    []string
	Help     string
}

// QuoteBytes returns the deterministic ASCII byte-literal form used by
// std::fmt::append_bytes_literal in the self-hosted compiler.
func QuoteBytes(text string) string {
	return quote.Bytes(text)
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
	first := d.summary()
	if !d.Span.IsZero() {
		if path := d.Span.Source.Path(); path != "" {
			first += fmt.Sprintf(" at %s:%d:%d", path, d.Span.Start.Line, d.Span.Start.Column)
		} else {
			first += fmt.Sprintf(" at %d:%d", d.Span.Start.Line, d.Span.Start.Column)
		}
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

// summary is the first line without its position: the category and message,
// or the warning and message.
func (d *Diagnostic) summary() string {
	switch {
	case d.Category != "":
		return d.Category + ": " + d.Message
	case d.Severity == SeverityWarning:
		return "warning: " + d.Message
	}
	return d.Message
}

// CLIError renders the diagnostic for a terminal (ADR-0072): the severity and
// summary, then where it is -- the path and position, the source line, and a
// marker under the span -- then the notes and help under the same gutter.
func (d *Diagnostic) CLIError() string {
	var out strings.Builder
	if d.Severity != SeverityWarning {
		out.WriteString("error: ")
	}
	out.WriteString(d.summary())
	gutter := ""
	if !d.Span.IsZero() {
		gutter = strings.Repeat(" ", len(strconv.Itoa(d.Span.Start.Line)))
		writeSnippet(&out, d.Span, gutter)
	}
	for _, note := range d.Notes {
		out.WriteString("\n" + gutter + "= note: " + note)
	}
	if d.Help != "" {
		for _, line := range strings.Split(d.Help, "\n") {
			out.WriteString("\n" + gutter + "= help: " + line)
		}
	}
	return out.String()
}

// writeSnippet writes where span is and, when its source text is at hand,
// the line it starts on with a marker under it:
//
//	  --> src/main.kizu:10:9
//	   |
//	10 |     try anything();
//	   |         ^^^^^^^^
func writeSnippet(out *strings.Builder, span ast.Span, gutter string) {
	out.WriteString("\n" + gutter + "--> ")
	if path := span.Source.Path(); path != "" {
		out.WriteString(path + ":")
	}
	fmt.Fprintf(out, "%d:%d", span.Start.Line, span.Start.Column)
	line, ok := sourceLine(span.Source.Text(), span.Start.Line)
	if !ok {
		return
	}
	out.WriteString("\n" + gutter + " |")
	fmt.Fprintf(out, "\n%d | %s", span.Start.Line, line)
	out.WriteString("\n" + gutter + " | " + markerLine(line, span))
}

// sourceLine answers the text of the one-based line, without its newline.
func sourceLine(text string, number int) (string, bool) {
	if text == "" || number < 1 {
		return "", false
	}
	for current := 1; ; current++ {
		end := strings.IndexByte(text, '\n')
		if current == number {
			if end < 0 {
				end = len(text)
			}
			return strings.TrimSuffix(text[:end], "\r"), true
		}
		if end < 0 {
			return "", false
		}
		text = text[end+1:]
	}
}

// markerLine answers the carets under span on its first line. Columns count
// bytes, so the lead-in keeps the line's tabs and gives every other character
// one space, not one per byte; the carets cover the span on that line, or
// one character when the span goes on past it.
func markerLine(line string, span ast.Span) string {
	start := span.Start.Column - 1
	if start > len(line) {
		start = len(line)
	}
	if start < 0 {
		start = 0
	}
	var marker strings.Builder
	for _, r := range line[:start] {
		if r == '\t' {
			marker.WriteByte('\t')
		} else {
			marker.WriteByte(' ')
		}
	}
	width := 1
	if span.End.Line == span.Start.Line && span.End.Column > span.Start.Column {
		width = span.End.Column - span.Start.Column
	}
	if rest := utf8.RuneCountInString(line[start:]); width > rest && rest > 0 {
		width = rest
	}
	marker.WriteString(strings.Repeat("^", width))
	return marker.String()
}

// SourceSpan returns the primary source span for the diagnostic.
func (d *Diagnostic) SourceSpan() ast.Span {
	return d.Span
}

// SeverityLevel returns the diagnostic severity for LSP and other renderers.
func (d *Diagnostic) SeverityLevel() Severity {
	return d.Severity
}

// WithCode records one stable machine-readable diagnostic code.
func (d *Diagnostic) WithCode(code string) *Diagnostic {
	d.Code = code
	return d
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

// Locate gives err the span when it is a diagnostic that says no place. A
// check deep inside a statement often knows what is wrong but not where, and
// the statement it is in is where to look; a diagnostic that already points
// somewhere keeps its place.
func Locate(err error, span ast.Span) error {
	var structured *Diagnostic
	if err == nil || span.IsZero() || !errors.As(err, &structured) || !structured.Span.IsZero() {
		return err
	}
	structured.Span = span
	return err
}
