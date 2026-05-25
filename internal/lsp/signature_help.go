package lsp

import "github.com/kizu-lang/kizu/internal/token"

// signatureHelp returns call signature information at a document position.
func (s *Server) signatureHelp(uri string, position Position) *signatureHelp {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	tokens := lexCompletionTokens(source)
	open := callOpenBefore(tokens, position)
	if open < 1 {
		return nil
	}
	index, _ := s.navigationIndex(uri, source)
	decl, ok := callDeclarationAt(tokens, open, source, position, index)
	if !ok {
		return nil
	}
	return &signatureHelp{
		Signatures: []signatureInformation{{
			Label:      decl.detail,
			Parameters: signatureParameters(decl.params),
		}},
		ActiveParameter: activeCallParameter(tokens, open, position, len(decl.params)),
	}
}

// callOpenBefore returns the innermost unclosed call paren before a position.
func callOpenBefore(tokens []token.Token, position Position) int {
	stack := []int{}
	for i, tok := range tokens {
		if tok.Type == token.EOF || tokenStartsAfter(tok, position) {
			break
		}
		switch tok.Type {
		case token.LParen:
			stack = append(stack, i)
		case token.RParen:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	for len(stack) > 0 {
		open := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if open > 0 && tokens[open-1].Type == token.Ident {
			return open
		}
	}
	return -1
}

// callDeclarationAt resolves the function or method being called.
func callDeclarationAt(
	tokens []token.Token,
	open int,
	source string,
	position Position,
	index navigationIndex,
) (navigationDeclaration, bool) {
	nameIndex := open - 1
	name := tokens[nameIndex].Literal
	if nameIndex >= 2 && tokens[nameIndex-1].Type == token.Dot {
		receiver := tokens[nameIndex-2]
		if receiver.Type != token.Ident {
			return navigationDeclaration{}, false
		}
		typ := normalizeCompletionType(localTypeAt(source, position, receiver.Literal))
		decl, ok := index.methods[typ][name]
		return decl, ok
	}
	decl, ok := index.functions[name]
	return decl, ok
}

// activeCallParameter counts top-level commas in the active call.
func activeCallParameter(tokens []token.Token, open int, position Position, total int) int {
	active := 0
	depth := 0
	for i := open + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Type == token.EOF || tokenStartsAfter(tok, position) {
			break
		}
		switch tok.Type {
		case token.LParen, token.LBrace, token.LBracket:
			depth++
		case token.RParen, token.RBrace, token.RBracket:
			if depth == 0 {
				return clampActiveParameter(active, total)
			}
			depth--
		case token.Comma:
			if depth == 0 {
				active++
			}
		}
	}
	return clampActiveParameter(active, total)
}

// clampActiveParameter keeps the active parameter inside the signature list.
func clampActiveParameter(active int, total int) int {
	if total == 0 || active < total {
		return active
	}
	return total - 1
}

// signatureParameters converts labels into LSP parameter information.
func signatureParameters(labels []string) []parameterInformation {
	params := make([]parameterInformation, 0, len(labels))
	for _, label := range labels {
		params = append(params, parameterInformation{Label: label})
	}
	return params
}
