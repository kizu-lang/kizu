package lsp

import "github.com/kizu-lang/kizu/internal/token"

const (
	semanticNamespace = iota
	semanticType
	semanticClass
	semanticEnum
	semanticInterface
	semanticStruct
	semanticParameter
	semanticVariable
	semanticProperty
	semanticEnumMember
	semanticFunction
	semanticMethod
	semanticKeyword
	semanticString
	semanticNumber
	semanticOperator
)

// semanticTokenLegend returns the token legend advertised during initialize.
func semanticTokenLegend() semanticTokensLegend {
	return semanticTokensLegend{
		TokenTypes: []string{
			"namespace",
			"type",
			"class",
			"enum",
			"interface",
			"struct",
			"parameter",
			"variable",
			"property",
			"enumMember",
			"function",
			"method",
			"keyword",
			"string",
			"number",
			"operator",
		},
		TokenModifiers: []string{},
	}
}

// semanticTokens returns whole-document semantic tokens for a tracked source.
func (s *Server) semanticTokens(uri string) semanticTokens {
	source, ok := s.documents[uri]
	if !ok {
		return semanticTokens{}
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	data := []int{}
	prevLine := 0
	prevChar := 0
	for _, tok := range tokens {
		typ, ok := semanticTokenType(tok, tokens, uri, source, index, sources)
		if !ok {
			continue
		}
		line := tok.Line - 1
		character := tok.Column - 1
		deltaLine := line - prevLine
		deltaStart := character
		if deltaLine == 0 {
			deltaStart = character - prevChar
		}
		data = append(data, deltaLine, deltaStart, utf16Len(tok.Literal), typ, 0)
		prevLine = line
		prevChar = character
	}
	return semanticTokens{Data: data}
}

// semanticTokenType classifies one token for semantic highlighting.
func semanticTokenType(
	tok token.Token,
	tokens []token.Token,
	uri string,
	source string,
	index navigationIndex,
	sources []navigationSource,
) (int, bool) {
	switch tok.Type {
	case token.Int:
		return semanticNumber, true
	case token.String:
		return semanticString, true
	}
	if isOperatorToken(tok.Type) {
		return semanticOperator, true
	}
	if isKeywordToken(tok.Type) {
		return semanticKeyword, true
	}
	if tok.Type != token.Ident {
		return 0, false
	}
	position := Position{Line: tok.Line - 1, Character: tok.Column - 1}
	decl, ok := definitionAt(tokens, position, uri, source, index, sources)
	if !ok {
		decl, ok = declarationAtToken(tok, index)
	}
	if !ok {
		return 0, false
	}
	return semanticTypeForSymbol(decl.kind), true
}

// semanticTypeForSymbol maps LSP symbol kinds to semantic token kinds.
func semanticTypeForSymbol(kind int) int {
	switch kind {
	case symbolKindModule:
		return semanticNamespace
	case symbolKindClass:
		return semanticClass
	case symbolKindMethod:
		return semanticMethod
	case symbolKindField:
		return semanticProperty
	case symbolKindEnum:
		return semanticEnum
	case symbolKindInterface:
		return semanticInterface
	case symbolKindFunction:
		return semanticFunction
	case symbolKindVariable:
		return semanticVariable
	case symbolKindEnumMember:
		return semanticEnumMember
	case symbolKindStruct:
		return semanticStruct
	default:
		return semanticType
	}
}

// isKeywordToken reports whether a token is a Kizu keyword.
func isKeywordToken(typ token.Type) bool {
	// A keyword token's spelling is also its type. Asking the lexer keeps
	// semantic highlighting in step when the language gains another keyword.
	return typ != token.Ident && token.LookupIdent(string(typ)) == typ
}

// isOperatorToken reports whether a token is an operator-like punctuation.
func isOperatorToken(typ token.Type) bool {
	switch typ {
	case token.Assign, token.Plus, token.Minus, token.Bang, token.Question,
		token.Amp, token.Asterisk, token.Slash, token.Percent, token.Eq,
		token.FatArrow, token.NotEq, token.LT, token.LTE, token.GT,
		token.GTE, token.Arrow, token.Dot, token.Range, token.DoubleColon,
		token.At:
		return true
	default:
		return false
	}
}
