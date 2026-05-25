package lsp

import kizufmt "github.com/kizu-lang/kizu/internal/fmt"

// FormatEdits returns a full-document formatting edit for parse-valid source.
func FormatEdits(source string) []textEdit {
	_, parseErrors := parseSource(source)
	if len(parseErrors) > 0 {
		return []textEdit{}
	}
	formatted := kizufmt.Format(source)
	if formatted == source {
		return []textEdit{}
	}
	return []textEdit{{
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   documentEnd(source),
		},
		NewText: formatted,
	}}
}

// documentEnd returns the LSP position just after the final source character.
func documentEnd(source string) Position {
	line := 0
	character := 0
	for _, r := range source {
		if r == '\n' {
			line++
			character = 0
			continue
		}
		if r >= 0x10000 {
			character += 2
			continue
		}
		character++
	}
	return Position{Line: line, Character: character}
}
