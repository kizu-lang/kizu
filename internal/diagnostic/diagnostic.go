package diagnostic

import (
	"errors"
	"fmt"
	"sort"
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
	// Related are other places the diagnostic points at, each with what
	// happened there: where a value was borrowed, where it was moved.
	Related []Related
}

// Related is a second place a diagnostic points at and what happened there.
type Related struct {
	Span  ast.Span
	Label string
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
// summary, then where it is -- the path and position, the source lines, and
// a marker under each span, `^` for where it went wrong and `-` for a related
// place with its label -- then the notes and help under the same gutter.
func (d *Diagnostic) CLIError() string {
	var out strings.Builder
	if d.Severity != SeverityWarning {
		out.WriteString("error: ")
	}
	out.WriteString(d.summary())
	gutter := ""
	if !d.Span.IsZero() {
		marks, others := d.snippetMarks()
		width := 0
		for _, mark := range append(append([]snippetMark{}, marks...), others...) {
			width = max(width, len(strconv.Itoa(mark.span.Start.Line)))
		}
		gutter = strings.Repeat(" ", width)
		writeSnippet(&out, d.Span, marks, gutter)
		for _, other := range others {
			writeRelatedSnippet(&out, other, gutter)
		}
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

// snippetMark is one span the snippet draws a marker under.
type snippetMark struct {
	span    ast.Span
	label   string
	primary bool
}

// snippetMarks answers the marks drawn in the primary span's file, in source
// order, and the related places in other files, which get snippets of their
// own.
func (d *Diagnostic) snippetMarks() ([]snippetMark, []snippetMark) {
	marks := []snippetMark{{span: d.Span, primary: true}}
	var others []snippetMark
	for _, related := range d.Related {
		mark := snippetMark{span: related.Span, label: related.Label}
		if related.Span.Source == d.Span.Source {
			marks = append(marks, mark)
		} else {
			others = append(others, mark)
		}
	}
	sort.SliceStable(marks, func(i, j int) bool {
		left, right := marks[i].span.Start, marks[j].span.Start
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Column < right.Column
	})
	return marks, others
}

// writeSnippet writes where the primary span is and, when its source text is
// at hand, each marked line with its markers under it; lines between marks
// that are not next to each other fold into `...`:
//
//	  --> src/main.kizu:16:5
//	   |
//	14 |     let first = users.at(alice);
//	   |                 -------- `users` is borrowed here by `first`
//	...
//	16 |     users.add(allocator, bob);
//	   |     ^^^^^^^^^
func writeSnippet(out *strings.Builder, primary ast.Span, marks []snippetMark, gutter string) {
	out.WriteString("\n" + gutter + "--> ")
	writePosition(out, primary)
	text := primary.Source.Text()
	if _, ok := sourceLine(text, primary.Start.Line); !ok {
		return
	}
	writeMarkedLines(out, text, marks, gutter)
}

// writeRelatedSnippet writes a related place in another file under `:::`.
func writeRelatedSnippet(out *strings.Builder, mark snippetMark, gutter string) {
	out.WriteString("\n" + gutter + "::: ")
	writePosition(out, mark.span)
	writeMarkedLines(out, mark.span.Source.Text(), []snippetMark{mark}, gutter)
}

// writePosition writes a span's path, when it has one, and its start.
func writePosition(out *strings.Builder, span ast.Span) {
	if path := span.Source.Path(); path != "" {
		out.WriteString(path + ":")
	}
	fmt.Fprintf(out, "%d:%d", span.Start.Line, span.Start.Column)
}

// writeMarkedLines writes each line the marks start on, once, with a marker
// line per mark under it.
func writeMarkedLines(out *strings.Builder, text string, marks []snippetMark, gutter string) {
	out.WriteString("\n" + gutter + " |")
	previous := 0
	for _, mark := range marks {
		number := mark.span.Start.Line
		line, ok := sourceLine(text, number)
		if !ok {
			continue
		}
		if number != previous {
			if previous != 0 && number > previous+1 {
				out.WriteString("\n...")
			}
			number := strconv.Itoa(number)
			out.WriteString("\n" + number + gutter[len(number):] + " | " + line)
			previous = mark.span.Start.Line
		}
		marker := markerLine(line, mark.span, mark.primary)
		if mark.label != "" {
			marker += " " + mark.label
		}
		out.WriteString("\n" + gutter + " | " + marker)
	}
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

// markerLine answers the marker under span on its first line: carets for the
// primary span, dashes for a related one. Columns count
// bytes, so the lead-in keeps the line's tabs and gives every other character
// one space, not one per byte; the carets cover the span on that line, or to
// the line's last visible character when the span goes on past it.
func markerLine(line string, span ast.Span, primary bool) string {
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
	width := utf8.RuneCountInString(strings.TrimRight(line[start:], " \t"))
	if span.End.Line == span.Start.Line && span.End.Column > span.Start.Column {
		width = span.End.Column - span.Start.Column
	}
	if width < 1 {
		width = 1
	}
	if rest := utf8.RuneCountInString(line[start:]); width > rest && rest > 0 {
		width = rest
	}
	mark := "-"
	if primary {
		mark = "^"
	}
	marker.WriteString(strings.Repeat(mark, width))
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

// WithRelated adds a second place the diagnostic points at, labelled with
// what happened there, and returns the same diagnostic. A place with no
// position adds nothing.
func (d *Diagnostic) WithRelated(span ast.Span, label string) *Diagnostic {
	if !span.IsZero() {
		d.Related = append(d.Related, Related{Span: span, Label: label})
	}
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
