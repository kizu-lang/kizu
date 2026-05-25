package lsp

import (
	"strings"
	"unicode/utf16"

	"github.com/kizu-lang/kizu/internal/token"
)

// InlayHints returns inferred type hints for local bindings in source.
func InlayHints(source string, rng Range) []inlayHint {
	tokens := lexCompletionTokens(source)
	hints := []inlayHint{}
	bindings := map[string]string{}
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Function:
			bindings = functionParamBindings(tokens, i)
		case token.Ident:
			if isTestDeclStart(tokens, i) {
				bindings = map[string]string{}
			}
		case token.Let, token.Var:
			hint, binding, hasHint, hasBinding := readLetInlayHint(tokens, i, bindings, rng)
			if hasHint {
				hints = append(hints, hint)
			}
			if hasBinding {
				bindings[binding.name] = binding.typ
			}
		case token.For:
			if i+5 < len(tokens) && tokens[i+3].Type == token.Pipe && tokens[i+4].Type == token.Ident {
				bindings[tokens[i+4].Literal] = "i64"
			}
		}
	}
	return hints
}

// functionParamBindings returns typed params for the function that starts at index.
func functionParamBindings(tokens []token.Token, start int) map[string]string {
	params, _ := readFunctionParams(tokens, start)
	bindings := map[string]string{}
	for _, param := range params {
		if param.typ != "" {
			bindings[param.name] = param.typ
		}
	}
	return bindings
}

// isTestDeclStart reports top-level test declarations tokenized as an identifier.
func isTestDeclStart(tokens []token.Token, start int) bool {
	return tokens[start].Literal == "test" &&
		start+2 < len(tokens) &&
		tokens[start+1].Type == token.String &&
		tokens[start+2].Type == token.LBrace
}

// readLetInlayHint reads one local binding and builds its type hint if known.
func readLetInlayHint(
	tokens []token.Token,
	start int,
	bindings map[string]string,
	rng Range,
) (inlayHint, localBinding, bool, bool) {
	if start+2 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return inlayHint{}, localBinding{}, false, false
	}
	name := tokens[start+1]
	assign := findBindingAssign(tokens, start+2)
	if assign < 0 {
		return inlayHint{}, localBinding{}, false, false
	}
	typ, _ := inferInlayExprType(tokens, assign+1, bindings)
	if typ == "" {
		return inlayHint{}, localBinding{}, false, false
	}
	binding := localBinding{name: name.Literal, typ: typ}
	if !tokenInRange(name, rng) {
		return inlayHint{}, binding, false, true
	}
	position := Position{
		Line:      name.Line - 1,
		Character: name.Column - 1 + utf16Len(name.Literal),
	}
	return inlayHint{
		Position: position,
		Label:    ": " + typ,
		Kind:     inlayHintKindType,
	}, binding, true, true
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

// tokenInRange checks whether token starts within the requested LSP range.
func tokenInRange(tok token.Token, rng Range) bool {
	line := tok.Line - 1
	character := tok.Column - 1
	if line < rng.Start.Line || line > rng.End.Line {
		return false
	}
	if line == rng.Start.Line && character < rng.Start.Character {
		return false
	}
	if line == rng.End.Line && character >= rng.End.Character {
		return false
	}
	return true
}

// utf16Len returns the UTF-16 code unit length used by LSP positions.
func utf16Len(text string) int {
	return len(utf16.Encode([]rune(text)))
}
