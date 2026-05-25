package lsp

import "github.com/kizu-lang/kizu/internal/token"

type typeFact struct {
	name      string
	typ       string
	rng       Range
	scope     int
	showInlay bool
}

// documentTypeFacts collects local type facts used by hover and inlay hints.
func documentTypeFacts(source string) []typeFact {
	tokens := lexCompletionTokens(source)
	facts := []typeFact{}
	bindings := map[string]string{}
	scope := 0
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Function:
			scope++
			params, next := readParamDeclarations(tokens, i, "")
			facts = append(facts, typeFactsFromDeclarations(params, scope, false)...)
			bindings = declarationsToBindings(params)
			i = next
		case token.Ident:
			if isTestDeclStart(tokens, i) {
				scope++
				bindings = map[string]string{}
			}
		case token.Let, token.Var:
			decl, binding, ok := readLocalDeclaration(tokens, i, "", bindings)
			if ok && binding.typ != "" {
				facts = appendTypedFact(facts, decl, scope, true)
				bindings[binding.name] = binding.typ
			}
		case token.For:
			decl, binding, ok := readForPipeDeclaration(tokens, i, "")
			if ok {
				facts = appendTypedFact(facts, decl, scope, false)
				bindings[binding.name] = binding.typ
			}
		}
	}
	return facts
}

// typeFactsFromDeclarations converts local declarations into type facts.
func typeFactsFromDeclarations(
	decls []navigationDeclaration,
	scope int,
	showInlay bool,
) []typeFact {
	facts := []typeFact{}
	for _, decl := range decls {
		if fact, ok := maybeTypeFactFromDeclaration(decl, scope, showInlay); ok {
			facts = append(facts, fact)
		}
	}
	return facts
}

// appendTypedFact appends declarations that carry type details.
func appendTypedFact(
	facts []typeFact,
	decl navigationDeclaration,
	scope int,
	showInlay bool,
) []typeFact {
	if fact, ok := maybeTypeFactFromDeclaration(decl, scope, showInlay); ok {
		return append(facts, fact)
	}
	return facts
}

// maybeTypeFactFromDeclaration converts declarations that carry type details.
func maybeTypeFactFromDeclaration(
	decl navigationDeclaration,
	scope int,
	showInlay bool,
) (typeFact, bool) {
	name, typ, ok := splitLocalDetail(decl.detail)
	if !ok || typ == "" {
		return typeFact{}, false
	}
	return typeFact{
		name:      name,
		typ:       typ,
		rng:       decl.rng,
		scope:     scope,
		showInlay: showInlay,
	}, true
}

// typeFactAt returns the visible type fact for the identifier at a position.
func typeFactAt(source string, position Position, facts []typeFact) (typeFact, bool) {
	tokens := lexCompletionTokens(source)
	index := tokenIndexAtPosition(tokens, position)
	if index < 0 || tokens[index].Type != token.Ident {
		return typeFact{}, false
	}
	scope := scopeAtPosition(tokens, position)
	return visibleTypeFact(facts, position, scope, tokens[index].Literal)
}

// visibleTypeFact returns the most recent matching fact visible at a position.
func visibleTypeFact(
	facts []typeFact,
	position Position,
	scope int,
	name string,
) (typeFact, bool) {
	for i := len(facts) - 1; i >= 0; i-- {
		fact := facts[i]
		if fact.scope != scope || fact.name != name {
			continue
		}
		if !positionBefore(position, fact.rng.Start) {
			return fact, true
		}
	}
	return typeFact{}, false
}

// scopeAtPosition returns the function or test scope containing a position.
func scopeAtPosition(tokens []token.Token, position Position) int {
	scope := 0
	for i := 0; i < len(tokens); i++ {
		if tokenStartsAfter(tokens[i], position) {
			return scope
		}
		if tokens[i].Type == token.Function || isTestDeclStart(tokens, i) {
			scope++
		}
	}
	return scope
}

// positionBefore reports whether left is before right.
func positionBefore(left Position, right Position) bool {
	return left.Line < right.Line ||
		left.Line == right.Line && left.Character < right.Character
}
