package lsp

import "github.com/kizu-lang/kizu/internal/token"

// prepareRename validates that the cursor rests on a renameable identifier and
// returns the exact token range the editor should let the user edit. It returns
// nil when the position is not a resolvable symbol (keyword, literal, etc.).
func (s *Server) prepareRename(uri string, position Position) *Range {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	if _, ok := referenceTargetAt(tokens, position, uri, source, index, sources); !ok {
		return nil
	}
	tokenIndex := tokenIndexAtPosition(tokens, position)
	if tokenIndex < 0 || tokens[tokenIndex].Type != token.Ident {
		return nil
	}
	rng := tokenRange(tokens[tokenIndex])
	return &rng
}

// rename builds a workspace edit that replaces the declaration and every
// statically resolved use of the cursor symbol with newName across all known
// package sources. It returns nil when the symbol cannot be resolved.
func (s *Server) rename(uri string, position Position, newName string) *workspaceEdit {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	target, ok := referenceTargetAt(tokens, position, uri, source, index, sources)
	if !ok {
		return nil
	}
	changes := map[string][]textEdit{}
	seen := map[string]bool{}
	appendRename(changes, seen, target.uri, target.rng, newName)
	for _, src := range sources {
		srcTokens := lexCompletionTokens(src.source)
		for _, tok := range srcTokens {
			if tok.Type != token.Ident {
				continue
			}
			pos := Position{Line: tok.Line - 1, Character: tok.Column - 1}
			decl, ok := definitionAt(srcTokens, pos, src.uri, src.source, index, sources)
			if !ok || !sameDeclaration(decl, target) {
				continue
			}
			appendRename(changes, seen, src.uri, tokenRange(tok), newName)
		}
	}
	return &workspaceEdit{Changes: changes}
}

// appendRename records one replacement edit per unique uri/range.
func appendRename(
	changes map[string][]textEdit,
	seen map[string]bool,
	uri string,
	rng Range,
	newName string,
) {
	key := uri + ":" +
		itoa(rng.Start.Line) + ":" +
		itoa(rng.Start.Character) + ":" +
		itoa(rng.End.Line) + ":" +
		itoa(rng.End.Character)
	if seen[key] {
		return
	}
	seen[key] = true
	changes[uri] = append(changes[uri], textEdit{Range: rng, NewText: newName})
}
