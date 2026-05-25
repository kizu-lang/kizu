package lsp

import "github.com/kizu-lang/kizu/internal/token"

// references returns all statically resolved references for the cursor target.
func (s *Server) references(uri string, position Position, includeDeclaration bool) []location {
	source, ok := s.documents[uri]
	if !ok {
		return []location{}
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	target, ok := referenceTargetAt(tokens, position, uri, source, index, sources)
	if !ok {
		return []location{}
	}
	locations := []location{}
	seen := map[string]bool{}
	if includeDeclaration {
		locations = appendLocation(locations, seen, location{URI: target.uri, Range: target.rng})
	}
	for _, src := range sources {
		locations = appendReferenceMatches(locations, seen, src, target, index, sources)
	}
	if !includeDeclaration {
		locations = withoutDeclaration(locations, target)
	}
	return locations
}

// referenceTargetAt resolves the declaration whose references should be found.
func referenceTargetAt(
	tokens []token.Token,
	position Position,
	uri string,
	source string,
	index navigationIndex,
	sources []navigationSource,
) (navigationDeclaration, bool) {
	if decl, ok := definitionAt(tokens, position, uri, source, index, sources); ok {
		return decl, true
	}
	tokenIndex := tokenIndexAtPosition(tokens, position)
	if tokenIndex < 0 || tokens[tokenIndex].Type != token.Ident {
		return navigationDeclaration{}, false
	}
	return declarationAtToken(tokens[tokenIndex], index)
}

// declarationAtToken returns a declaration whose range contains the token.
func declarationAtToken(tok token.Token, index navigationIndex) (navigationDeclaration, bool) {
	for _, decl := range allNavigationDeclarations(index) {
		if sameRange(decl.rng, tokenRange(tok)) {
			return decl, true
		}
	}
	return navigationDeclaration{}, false
}

// appendReferenceMatches scans one source for references resolving to target.
func appendReferenceMatches(
	locations []location,
	seen map[string]bool,
	src navigationSource,
	target navigationDeclaration,
	index navigationIndex,
	sources []navigationSource,
) []location {
	tokens := lexCompletionTokens(src.source)
	for _, tok := range tokens {
		if tok.Type != token.Ident {
			continue
		}
		pos := Position{Line: tok.Line - 1, Character: tok.Column - 1}
		decl, ok := definitionAt(tokens, pos, src.uri, src.source, index, sources)
		if !ok || !sameDeclaration(decl, target) {
			continue
		}
		locations = appendLocation(locations, seen, location{
			URI:   src.uri,
			Range: tokenRange(tok),
		})
	}
	return locations
}

// appendLocation adds a location once.
func appendLocation(locations []location, seen map[string]bool, loc location) []location {
	key := locationKey(loc)
	if seen[key] {
		return locations
	}
	seen[key] = true
	return append(locations, loc)
}

// withoutDeclaration removes a declaration location from reference results.
func withoutDeclaration(locations []location, decl navigationDeclaration) []location {
	filtered := locations[:0]
	for _, loc := range locations {
		if loc.URI == decl.uri && sameRange(loc.Range, decl.rng) {
			continue
		}
		filtered = append(filtered, loc)
	}
	return filtered
}

// sameDeclaration reports whether two resolved declarations identify one symbol.
func sameDeclaration(left navigationDeclaration, right navigationDeclaration) bool {
	return left.uri == right.uri && sameRange(left.rng, right.rng)
}

// sameRange reports whether two LSP ranges are identical.
func sameRange(left Range, right Range) bool {
	return left.Start == right.Start && left.End == right.End
}

// locationKey formats a stable dedupe key for a location.
func locationKey(loc location) string {
	return loc.URI + ":" +
		itoa(loc.Range.Start.Line) + ":" +
		itoa(loc.Range.Start.Character) + ":" +
		itoa(loc.Range.End.Line) + ":" +
		itoa(loc.Range.End.Character)
}
