package lsp

import (
	"strings"
	"unicode/utf16"

	"github.com/kizu-lang/kizu/internal/token"
)

// InlayHints returns inferred type hints for local bindings in source.
func InlayHints(source string, rng Range) []inlayHint {
	return inlayHintsFromTypeFacts(documentTypeFacts(source), rng)
}

// inlayHints returns cached type hints for a tracked document.
func (s *Server) inlayHints(uri string, rng Range) []inlayHint {
	doc := s.checkedDocument(uri)
	if doc.Source == "" {
		return []inlayHint{}
	}
	return inlayHintsFromTypeFacts(doc.TypeFacts, rng)
}

// inlayHintsFromTypeFacts converts cached type facts into range-filtered hints.
func inlayHintsFromTypeFacts(facts []typeFact, rng Range) []inlayHint {
	hints := []inlayHint{}
	for _, fact := range facts {
		if !fact.showInlay || !positionInRange(fact.rng.Start, rng) {
			continue
		}
		hints = append(hints, inlayHint{
			Position: fact.rng.End,
			Label:    ": " + fact.typ,
			Kind:     inlayHintKindType,
		})
	}
	return hints
}

// isTestDeclStart reports top-level test declarations tokenized as an identifier.
func isTestDeclStart(tokens []token.Token, start int) bool {
	return tokens[start].Literal == "test" &&
		start+2 < len(tokens) &&
		tokens[start+1].Type == token.String &&
		tokens[start+2].Type == token.LBrace
}

// inferInlayExprType infers a conservative display type for one initializer.
func inferInlayExprType(tokens []token.Token, start int, bindings map[string]string) (string, int) {
	typ, end := inferInlayPrimaryType(tokens, start, bindings)
	if typ == "" {
		return "", end
	}
	return applyInlayBinary(tokens, end, typ), end
}

// inferInlayPrimaryType infers a type for the first expression atom only.
func inferInlayPrimaryType(
	tokens []token.Token,
	start int,
	bindings map[string]string,
) (string, int) {
	if start >= len(tokens) {
		return "", start
	}
	if typ, end, ok := inferUnaryInlayType(tokens, start, bindings); ok {
		return typ, end
	}
	if typ, end, ok := inferCastInlayType(tokens, start); ok {
		return typ, end
	}
	switch tokens[start].Type {
	case token.Int:
		return "i64", start
	case token.String:
		return "[]u8", start
	case token.True, token.False:
		return "bool", start
	case token.Ident:
		return inferIdentInlayType(tokens, start, bindings)
	}
	return "", start
}

// inferUnaryInlayType handles unary and parenthesized expression starts.
func inferUnaryInlayType(
	tokens []token.Token,
	start int,
	bindings map[string]string,
) (string, int, bool) {
	if tokens[start].Type == token.Minus &&
		start+1 < len(tokens) &&
		tokens[start+1].Type == token.Int {
		return "i64", start + 1, true
	}
	if tokens[start].Type == token.Bang {
		return "bool", start, true
	}
	if tokens[start].Type == token.LParen {
		typ, _ := inferInlayExprType(tokens, start+1, bindings)
		close := skipBalanced(tokens, start, token.LParen, token.RParen)
		if typ == "" || close <= start {
			return typ, start, true
		}
		return typ, close, true
	}
	return "", start, false
}

// inferCastInlayType handles cast<T>(value) expressions.
func inferCastInlayType(tokens []token.Token, start int) (string, int, bool) {
	if tokens[start].Type == token.Ident && tokens[start].Literal == "cast" &&
		start+1 < len(tokens) && tokens[start+1].Type == token.LT {
		typ, end := readCastType(tokens, start+1)
		if typ == "" {
			return "", end, true
		}
		if end+1 < len(tokens) && tokens[end+1].Type == token.LParen {
			close := skipBalanced(tokens, end+1, token.LParen, token.RParen)
			if close > end+1 {
				end = close
			}
		}
		return typ, end, true
	}
	return "", start, false
}

// inferIdentInlayType handles locals, enum variants, and struct literals.
func inferIdentInlayType(
	tokens []token.Token,
	start int,
	bindings map[string]string,
) (string, int) {
	if typ := bindings[tokens[start].Literal]; typ != "" {
		return typ, start
	}
	parts := []string{tokens[start].Literal}
	i := start
	for i+2 < len(tokens) &&
		tokens[i+1].Type == token.DoubleColon &&
		tokens[i+2].Type == token.Ident {
		parts = append(parts, tokens[i+2].Literal)
		i += 2
	}
	if i+1 < len(tokens) && tokens[i+1].Type == token.LBrace {
		end := i + 1
		close := skipBalanced(tokens, end, token.LBrace, token.RBrace)
		if close > end {
			end = close
		}
		return strings.Join(parts, "::"), end
	}
	if len(parts) >= 2 {
		return strings.Join(parts[:len(parts)-1], "::"), i
	}
	return "", start
}

// applyInlayBinary adjusts a primary expression type for simple binary operators.
func applyInlayBinary(tokens []token.Token, end int, left string) string {
	if end+1 >= len(tokens) {
		return left
	}
	switch tokens[end+1].Type {
	case token.Eq, token.NotEq, token.LT, token.LTE, token.GT, token.GTE, token.And, token.Or:
		return "bool"
	case token.Plus, token.Minus, token.Asterisk, token.Slash, token.Percent:
		return left
	}
	return left
}

// readCastType reads the target type from cast<T>(value).
func readCastType(tokens []token.Token, open int) (string, int) {
	depth := 0
	for i := open; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.LT:
			depth++
		case token.GT:
			depth--
			if depth == 0 {
				return tokenText(tokens[open+1 : i]), i
			}
		case token.EOF:
			return "", i
		}
	}
	return "", open
}

// findBindingAssign returns an initializer assignment within the current binding.
func findBindingAssign(tokens []token.Token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Assign:
			if depth == 0 {
				return i
			}
		case token.Semicolon, token.RBrace, token.EOF:
			if depth == 0 {
				return -1
			}
		case token.LParen, token.LBrace, token.LBracket:
			depth++
		case token.RParen, token.RBracket:
			if depth > 0 {
				depth--
			}
		}
	}
	return -1
}

// positionInRange checks whether an LSP position starts within a range.
func positionInRange(position Position, rng Range) bool {
	if position.Line < rng.Start.Line || position.Line > rng.End.Line {
		return false
	}
	if position.Line == rng.Start.Line && position.Character < rng.Start.Character {
		return false
	}
	if position.Line == rng.End.Line && position.Character >= rng.End.Character {
		return false
	}
	return true
}

// utf16Len returns the UTF-16 code unit length used by LSP positions.
func utf16Len(text string) int {
	return len(utf16.Encode([]rune(text)))
}
