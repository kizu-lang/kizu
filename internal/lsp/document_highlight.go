package lsp

import "github.com/kizu-lang/kizu/internal/token"

// documentHighlights returns highlight ranges for the symbol under the cursor,
// restricted to the current document. Editors use these to highlight every
// occurrence of the identifier the caret currently rests on.
func (s *Server) documentHighlights(uri string, position Position) []documentHighlight {
	source, ok := s.documents[uri]
	if !ok {
		return []documentHighlight{}
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	target, ok := referenceTargetAt(tokens, position, uri, source, index, sources)
	if !ok {
		return []documentHighlight{}
	}
	highlights := []documentHighlight{}
	seen := map[string]bool{}
	if target.uri == uri {
		highlights = appendHighlight(highlights, seen, target.rng)
	}
	for _, tok := range tokens {
		if tok.Type != token.Ident {
			continue
		}
		pos := Position{Line: tok.Line - 1, Character: tok.Column - 1}
		decl, ok := definitionAt(tokens, pos, uri, source, index, sources)
		if !ok || !sameDeclaration(decl, target) {
			continue
		}
		highlights = appendHighlight(highlights, seen, tokenRange(tok))
	}
	return highlights
}

// appendHighlight adds a Text-kind highlight for rng once.
func appendHighlight(
	highlights []documentHighlight,
	seen map[string]bool,
	rng Range,
) []documentHighlight {
	key := itoa(rng.Start.Line) + ":" +
		itoa(rng.Start.Character) + ":" +
		itoa(rng.End.Line) + ":" +
		itoa(rng.End.Character)
	if seen[key] {
		return highlights
	}
	seen[key] = true
	return append(highlights, documentHighlight{Range: rng, Kind: documentHighlightKindText})
}
