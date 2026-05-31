package lsp

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/token"
)

// selectionRanges returns one smart-selection hierarchy per requested position.
// Each hierarchy expands outward from the token under the cursor through every
// enclosing bracket pair up to the whole document, so editors can grow or
// shrink the selection one structural level at a time.
func (s *Server) selectionRanges(uri string, positions []Position) []*selectionRange {
	result := make([]*selectionRange, 0, len(positions))
	source, ok := s.documents[uri]
	if !ok {
		return result
	}
	tokens := lexCompletionTokens(source)
	docRange := documentRange(source)
	for _, position := range positions {
		result = append(result, selectionRangeAt(tokens, docRange, position))
	}
	return result
}

// selectionRangeAt builds the nested range chain for a single position.
func selectionRangeAt(tokens []token.Token, docRange Range, position Position) *selectionRange {
	candidates := []Range{}
	if index := tokenIndexAtPosition(tokens, position); index >= 0 {
		candidates = append(candidates, tokenRange(tokens[index]))
	}
	candidates = append(candidates, bracketRangesContaining(tokens, position)...)
	candidates = append(candidates, docRange)

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Start != candidates[j].Start {
			return positionBefore(candidates[j].Start, candidates[i].Start)
		}
		return positionBefore(candidates[i].End, candidates[j].End)
	})

	chain := []Range{}
	for _, candidate := range candidates {
		if len(chain) == 0 {
			chain = append(chain, candidate)
			continue
		}
		last := chain[len(chain)-1]
		if candidate == last || !rangeContainsRange(candidate, last) {
			continue
		}
		chain = append(chain, candidate)
	}

	var node *selectionRange
	for i := len(chain) - 1; i >= 0; i-- {
		node = &selectionRange{Range: chain[i], Parent: node}
	}
	return node
}

// bracketRangesContaining returns the span of every (), {}, or [] pair that
// encloses the position, from the brackets inward. Pairs nest, so the result is
// already a valid containment chain once sorted.
func bracketRangesContaining(tokens []token.Token, position Position) []Range {
	ranges := []Range{}
	openers := []token.Token{}
	for _, tok := range tokens {
		switch tok.Type {
		case token.LParen, token.LBrace, token.LBracket:
			openers = append(openers, tok)
		case token.RParen, token.RBrace, token.RBracket:
			if len(openers) == 0 {
				continue
			}
			open := openers[len(openers)-1]
			openers = openers[:len(openers)-1]
			rng := Range{Start: tokenRange(open).Start, End: tokenRange(tok).End}
			if rangeContainsPositionInclusive(rng, position) {
				ranges = append(ranges, rng)
			}
		}
	}
	return ranges
}

// documentRange covers the entire source from its first to last character.
func documentRange(source string) Range {
	line, column := 0, 0
	for _, r := range source {
		if r == '\n' {
			line++
			column = 0
			continue
		}
		column++
	}
	return Range{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: line, Character: column},
	}
}

// positionLessEqual reports whether left is at or before right.
func positionLessEqual(left Position, right Position) bool {
	return !positionBefore(right, left)
}

// rangeContainsPositionInclusive treats both range endpoints as inside, so a
// cursor resting on a bracket still selects that pair.
func rangeContainsPositionInclusive(rng Range, position Position) bool {
	return positionLessEqual(rng.Start, position) && positionLessEqual(position, rng.End)
}

// rangeContainsRange reports whether outer fully encloses inner.
func rangeContainsRange(outer Range, inner Range) bool {
	return positionLessEqual(outer.Start, inner.Start) &&
		positionLessEqual(inner.End, outer.End)
}
