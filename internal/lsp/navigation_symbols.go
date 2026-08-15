package lsp

import (
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/token"
)

// documentSymbolsFromTokens builds outline symbols in source order.
func documentSymbolsFromTokens(tokens []token.Token) []documentSymbol {
	symbols := []documentSymbol{}
	for i := 0; i < len(tokens); i++ {
		symbol, next, ok := documentSymbolAt(tokens, i)
		if ok {
			symbols = append(symbols, symbol)
		}
		i = next
	}
	return symbols
}

// documentSymbolAt reads one outline symbol at the given token index.
func documentSymbolAt(tokens []token.Token, start int) (documentSymbol, int, bool) {
	switch tokens[start].Type {
	case token.Import:
		return importDocumentSymbol(tokens, start)
	case token.Function:
		return functionDocumentSymbol(tokens, start)
	case token.Ident:
		if isTestDeclStart(tokens, start) {
			symbol, next := testDocumentSymbol(tokens, start)
			return symbol, next, true
		}
	case token.Struct:
		return structDocumentSymbol(tokens, start)
	case token.Enum:
		return variantDocumentSymbol(tokens, start, "enum", symbolKindEnum)
	case token.Union:
		return variantDocumentSymbol(tokens, start, "union", symbolKindEnum)
	case token.Contract:
		return namedDocumentSymbol(tokens, start, "contract", symbolKindInterface)
	case token.Impl:
		return implDocumentSymbol(tokens, start)
	}
	return documentSymbol{}, start, false
}

// importDocumentSymbol builds an outline item for an import declaration.
func importDocumentSymbol(tokens []token.Token, start int) (documentSymbol, int, bool) {
	path, next := readImportPath(tokens, start+1)
	if len(path) == 0 {
		return documentSymbol{}, next, false
	}
	selection := tokenRange(tokens[start+1])
	return documentSymbol{
		Name:           strings.Join(path, "::"),
		Kind:           symbolKindModule,
		Range:          rangeFromTokenSpan(tokens, start, next),
		SelectionRange: selection,
	}, next, true
}

// functionDocumentSymbol builds an outline item for a function declaration.
func functionDocumentSymbol(tokens []token.Token, start int) (documentSymbol, int, bool) {
	receiver, nameAt := readReceiver(tokens, start)
	if nameAt >= len(tokens) || tokens[nameAt].Type != token.Ident {
		return documentSymbol{}, start, false
	}
	headerEnd := declarationHeaderEnd(tokens, start)
	end := skipDeclarationBody(tokens, headerEnd)
	name := tokens[nameAt].Literal
	kind := symbolKindFunction
	if len(receiver) > 0 {
		// The outline groups a method under the type it is on, which is where a
		// reader looks for it.
		name = receiverType(receiver) + "." + name
		kind = symbolKindMethod
	}
	return documentSymbol{
		Name:           name,
		Detail:         tokenText(tokens[start:headerEnd]),
		Kind:           kind,
		Range:          rangeFromTokenSpan(tokens, start, end),
		SelectionRange: tokenRange(tokens[nameAt]),
	}, end, true
}

// testDocumentSymbol builds an outline item for a test block.
func testDocumentSymbol(tokens []token.Token, start int) (documentSymbol, int) {
	end := skipDeclarationBody(tokens, start+2)
	name := "test " + tokens[start+1].Literal
	return documentSymbol{
		Name:           name,
		Kind:           symbolKindFunction,
		Range:          rangeFromTokenSpan(tokens, start, end),
		SelectionRange: tokenRange(tokens[start+1]),
	}, end
}

// structDocumentSymbol builds an outline item and field children for a struct.
func structDocumentSymbol(tokens []token.Token, start int) (documentSymbol, int, bool) {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return documentSymbol{}, start, false
	}
	brace := findNextToken(tokens, nameIndex+1, token.LBrace)
	if brace < 0 {
		return documentSymbol{}, start, false
	}
	name := tokens[nameIndex].Literal
	symbol := documentSymbol{
		Name:           name,
		Detail:         "struct",
		Kind:           symbolKindStruct,
		SelectionRange: tokenRange(tokens[nameIndex]),
	}
	fields, end := scanFieldDeclarations(navigationSource{}, tokens, brace, symbol.Name)
	symbol.Children = declarationChildren(fields)
	symbol.Range = rangeFromTokenSpan(tokens, start, end)
	return symbol, end, true
}

// variantDocumentSymbol builds an outline item and variant children.
func variantDocumentSymbol(
	tokens []token.Token,
	start int,
	detail string,
	kind int,
) (documentSymbol, int, bool) {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return documentSymbol{}, start, false
	}
	name := tokens[nameIndex].Literal
	symbol := documentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		SelectionRange: tokenRange(tokens[nameIndex]),
	}
	variants, end := scanVariantDeclarations(navigationSource{}, tokens, nameIndex+1, name, detail)
	symbol.Children = declarationChildren(variants)
	symbol.Range = rangeFromTokenSpan(tokens, start, end)
	return symbol, end, true
}

// namedDocumentSymbol builds an outline item for a simple named declaration.
func namedDocumentSymbol(
	tokens []token.Token,
	start int,
	detail string,
	kind int,
) (documentSymbol, int, bool) {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return documentSymbol{}, start, false
	}
	end := skipDeclarationBody(tokens, nameIndex)
	name := tokens[nameIndex].Literal
	return documentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		Range:          rangeFromTokenSpan(tokens, start, end),
		SelectionRange: tokenRange(tokens[nameIndex]),
	}, end, true
}

// declarationChildren converts nested declarations into sorted outline children.
func declarationChildren(decls map[string]navigationDeclaration) []documentSymbol {
	names := make([]string, 0, len(decls))
	for name := range decls {
		names = append(names, name)
	}
	sort.Strings(names)
	children := make([]documentSymbol, 0, len(names))
	for _, name := range names {
		decl := decls[name]
		children = append(children, documentSymbol{
			Name:           decl.name,
			Detail:         decl.detail,
			Kind:           decl.kind,
			Range:          decl.rng,
			SelectionRange: decl.rng,
		})
	}
	return children
}

// implDocumentSymbol builds an outline item and method children for an impl block.
func implDocumentSymbol(tokens []token.Token, start int) (documentSymbol, int, bool) {
	typeName, brace := implTarget(tokens, start)
	if brace < 0 {
		return documentSymbol{}, start, false
	}
	end := skipBalanced(tokens, brace, token.LBrace, token.RBrace)
	symbol := documentSymbol{
		Name:           "impl " + normalizeCompletionType(typeName),
		Kind:           symbolKindClass,
		Range:          rangeFromTokenSpan(tokens, start, end),
		SelectionRange: rangeFromTokenSpan(tokens, start, brace),
	}
	for i := brace + 1; i < end; i++ {
		if tokens[i].Type != token.Function {
			continue
		}
		child, next, ok := functionDocumentSymbol(tokens, i)
		if ok {
			child.Kind = symbolKindMethod
			symbol.Children = append(symbol.Children, child)
		}
		i = next
	}
	return symbol, end, true
}
