package ast

// Span identifies a byte range in a source file.
type Span struct {
	Start  int
	End    int
	Line   int
	Column int
}

// Empty reports whether the span has no recorded byte range.
func (s Span) Empty() bool {
	return s.Start == 0 && s.End == 0
}
