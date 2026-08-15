package lsp

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/token"
)

// implementation resolves the contract (or contract method) under the cursor to
// its concrete implementations. Pointing at a contract name jumps to each
// implementing type; pointing at a contract method jumps to each implementation
// of that method. It returns an empty slice when nothing implements the symbol.
func (s *Server) implementation(uri string, position Position) []location {
	source, ok := s.documents[uri]
	if !ok {
		return []location{}
	}
	tokens := lexCompletionTokens(source)
	tokIdx := tokenIndexAtPosition(tokens, position)
	if tokIdx < 0 || tokens[tokIdx].Type != token.Ident {
		return []location{}
	}
	name := tokens[tokIdx].Literal
	index, sources := s.navigationIndex(uri, source)

	contractName, methodName := contractTarget(tokens, tokIdx, name, index)
	if contractName == "" {
		return []location{}
	}
	return collectImplementations(index, sources, contractName, methodName)
}

// contractTarget decides which contract (and optionally which of its methods)
// the cursor refers to: a method name inside a contract body, or a contract
// name used anywhere else.
func contractTarget(
	tokens []token.Token,
	tokIdx int,
	name string,
	index navigationIndex,
) (contractName string, methodName string) {
	if enclosing := enclosingContractName(tokens, tokenRange(tokens[tokIdx]).Start); enclosing != "" {
		if tokIdx > 0 && tokens[tokIdx-1].Type == token.Function {
			return enclosing, name
		}
	}
	if decl, ok := index.types[name]; ok && decl.kind == symbolKindInterface {
		return name, ""
	}
	return "", ""
}

// collectImplementations gathers locations across all package sources for the
// given contract, narrowing to a single method when methodName is set.
//
// A type implements a contract by having its methods, so this looks for the
// methods rather than for a declaration naming the contract. `impl C for T;`
// asserts the same thing and needs no separate search.
func collectImplementations(
	index navigationIndex,
	sources []navigationSource,
	contractName string,
	methodName string,
) []location {
	want := contractMethodNames(sources, contractName)
	if len(want) == 0 {
		return []location{}
	}
	locations := []location{}
	for _, src := range sources {
		methods := receiverMethodsIn(lexCompletionTokens(src.source))
		for _, typeName := range sortedTypeNames(methods) {
			if !coversAll(methods[typeName], want) {
				continue
			}
			locations = append(locations,
				implementationLocation(index, src, typeName, methods[typeName], methodName)...)
		}
	}
	return locations
}

// implementationLocation points at one implementing type, or at the one method
// the cursor asked about.
func implementationLocation(
	index navigationIndex,
	src navigationSource,
	typeName string,
	methods map[string]Range,
	methodName string,
) []location {
	if methodName != "" {
		if rng, ok := methods[methodName]; ok {
			return []location{{URI: src.uri, Range: rng}}
		}
		return nil
	}
	if decl, ok := index.types[typeName]; ok {
		return []location{{URI: decl.uri, Range: decl.rng}}
	}
	return nil
}

// contractMethodNames returns the method names one contract requires.
func contractMethodNames(sources []navigationSource, contractName string) []string {
	for _, src := range sources {
		tokens := lexCompletionTokens(src.source)
		for i := 0; i < len(tokens); i++ {
			if tokens[i].Type != token.Contract {
				continue
			}
			nameIdx := nextIdentifierIndex(tokens, i+1)
			brace := -1
			if nameIdx >= 0 {
				brace = findNextToken(tokens, nameIdx+1, token.LBrace)
			}
			if brace < 0 {
				continue
			}
			end := skipBalanced(tokens, brace, token.LBrace, token.RBrace)
			if tokens[nameIdx].Literal == contractName {
				return methodNamesBetween(tokens, brace, end)
			}
			i = end
		}
	}
	return nil
}

// methodNamesBetween lists the `fn name` declarations in a contract body.
func methodNamesBetween(tokens []token.Token, brace int, end int) []string {
	names := []string{}
	for i := brace + 1; i < end && i < len(tokens); i++ {
		if tokens[i].Type != token.Function || i+1 >= len(tokens) ||
			tokens[i+1].Type != token.Ident {
			continue
		}
		names = append(names, tokens[i+1].Literal)
		i = skipDeclarationBody(tokens, i+1)
	}
	return names
}

// receiverMethodsIn maps each type to the methods one source declares on it.
func receiverMethodsIn(tokens []token.Token) map[string]map[string]Range {
	methods := map[string]map[string]Range{}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Function {
			continue
		}
		receiver, nameAt := readReceiver(tokens, i)
		if len(receiver) == 0 || nameAt >= len(tokens) || tokens[nameAt].Type != token.Ident {
			continue
		}
		typeName := receiverType(receiver)
		if methods[typeName] == nil {
			methods[typeName] = map[string]Range{}
		}
		methods[typeName][tokens[nameAt].Literal] = tokenRange(tokens[nameAt])
		i = skipDeclarationBody(tokens, declarationHeaderEnd(tokens, i))
	}
	return methods
}

// coversAll reports whether a type declares every method a contract asks for.
func coversAll(methods map[string]Range, want []string) bool {
	for _, name := range want {
		if _, ok := methods[name]; !ok {
			return false
		}
	}
	return true
}

// sortedTypeNames lists implementing types in a stable order.
func sortedTypeNames(methods map[string]map[string]Range) []string {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// enclosingContractName returns the contract whose body contains position.
func enclosingContractName(tokens []token.Token, position Position) string {
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Contract {
			continue
		}
		nameIdx := nextIdentifierIndex(tokens, i+1)
		if nameIdx < 0 {
			continue
		}
		brace := findNextToken(tokens, nameIdx+1, token.LBrace)
		if brace < 0 {
			continue
		}
		end := skipBalanced(tokens, brace, token.LBrace, token.RBrace)
		bodyStart := tokenRange(tokens[brace]).End
		bodyEnd := tokenRange(tokens[end]).Start
		if positionLessEqual(bodyStart, position) && positionBefore(position, bodyEnd) {
			return tokens[nameIdx].Literal
		}
		i = end
	}
	return ""
}
