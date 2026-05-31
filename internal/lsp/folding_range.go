package lsp

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/token"
)

// foldingRanges reports the collapsible line spans of a tracked document:
// every multi-line brace or bracket pair plus each run of consecutive import
// statements. It returns an empty slice for unknown documents.
func (s *Server) foldingRanges(uri string) []foldingRange {
	source, ok := s.documents[uri]
	if !ok {
		return []foldingRange{}
	}
	tokens := lexCompletionTokens(source)
	ranges := bracketFoldingRanges(tokens)
	ranges = append(ranges, importFoldingRanges(tokens)...)
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].StartLine != ranges[j].StartLine {
			return ranges[i].StartLine < ranges[j].StartLine
		}
		return ranges[i].EndLine < ranges[j].EndLine
	})
	return ranges
}

// bracketFoldingRanges pairs every { with its } and [ with its ] using a stack
// so nested blocks each yield their own region. A pair folds only when it spans
// more than one content line so the closing delimiter stays visible.
func bracketFoldingRanges(tokens []token.Token) []foldingRange {
	ranges := []foldingRange{}
	openers := []token.Token{}
	for _, tok := range tokens {
		switch tok.Type {
		case token.LBrace, token.LBracket:
			openers = append(openers, tok)
		case token.RBrace, token.RBracket:
			if len(openers) == 0 {
				continue
			}
			open := openers[len(openers)-1]
			openers = openers[:len(openers)-1]
			if rng, ok := foldingSpan(open, tok); ok {
				ranges = append(ranges, rng)
			}
		}
	}
	return ranges
}

// foldingSpan builds a region from an opening to a closing delimiter, keeping
// the closing line visible. It returns false when nothing would be hidden.
func foldingSpan(open token.Token, close token.Token) (foldingRange, bool) {
	startLine := open.Line - 1
	endLine := close.Line - 2
	if endLine <= startLine {
		return foldingRange{}, false
	}
	return foldingRange{StartLine: startLine, EndLine: endLine}, true
}

// importFoldingRanges folds each run of two or more consecutive import
// statements into a single region, matching how editors collapse import blocks.
func importFoldingRanges(tokens []token.Token) []foldingRange {
	ranges := []foldingRange{}
	firstLine, lastLine, count := 0, 0, 0
	flush := func() {
		if count >= 2 && lastLine > firstLine {
			ranges = append(ranges, foldingRange{
				StartLine: firstLine - 1,
				EndLine:   lastLine - 1,
				Kind:      "imports",
			})
		}
		count = 0
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Import {
			continue
		}
		line := tokens[i].Line
		if count > 0 && line == lastLine {
			continue
		}
		if count == 0 {
			firstLine = line
		}
		lastLine = line
		count++
		// A blank line or any non-import declaration between two imports ends
		// the run; detect that by checking the next import's line gap below.
		next := findNextToken(tokens, i+1, token.Import)
		if next < 0 || tokens[next].Line > line+1 {
			flush()
		}
	}
	return ranges
}
