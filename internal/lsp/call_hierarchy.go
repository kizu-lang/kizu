package lsp

import "github.com/kizu-lang/kizu/internal/token"

// callHierarchyFunction is a function definition with the token span of its
// body, used to find both the calls it makes and the calls made to it.
type callHierarchyFunction struct {
	name      string
	uri       string
	selection Range
	full      Range
	bodyStart int
	bodyEnd   int
}

// item renders the function as the wire-level call hierarchy node.
func (f callHierarchyFunction) item() callHierarchyItem {
	return callHierarchyItem{
		Name:           f.name,
		Kind:           symbolKindFunction,
		URI:            f.uri,
		Range:          f.full,
		SelectionRange: f.selection,
	}
}

// callSite is one call expression: the callee name and the range of its name.
type callSite struct {
	name string
	rng  Range
}

// prepareCallHierarchy resolves the identifier under the cursor to the function
// definitions that bear that name, seeding the call hierarchy view.
func (s *Server) prepareCallHierarchy(uri string, position Position) []callHierarchyItem {
	source, ok := s.documents[uri]
	if !ok {
		return []callHierarchyItem{}
	}
	tokens := lexCompletionTokens(source)
	index := tokenIndexAtPosition(tokens, position)
	if index < 0 || tokens[index].Type != token.Ident {
		return []callHierarchyItem{}
	}
	name := tokens[index].Literal
	_, sources := s.navigationIndex(uri, source)
	items := []callHierarchyItem{}
	for _, fn := range allFunctions(sources) {
		if fn.name == name {
			items = append(items, fn.item())
		}
	}
	return items
}

// incomingCalls reports every function that calls the given item, with the
// ranges of each call expression inside that caller.
func (s *Server) incomingCalls(item callHierarchyItem) []callHierarchyIncomingCall {
	_, sources := s.navigationIndex(item.URI, s.documents[item.URI])
	calls := []callHierarchyIncomingCall{}
	byCaller := map[string]int{}
	for _, src := range sources {
		tokens := lexCompletionTokens(src.source)
		for _, fn := range scanFunctions(src.uri, tokens) {
			for _, site := range callSitesIn(tokens, fn.bodyStart, fn.bodyEnd) {
				if site.name != item.Name {
					continue
				}
				key := callerKey(fn)
				idx, ok := byCaller[key]
				if !ok {
					idx = len(calls)
					byCaller[key] = idx
					calls = append(calls, callHierarchyIncomingCall{From: fn.item()})
				}
				calls[idx].FromRanges = append(calls[idx].FromRanges, site.rng)
			}
		}
	}
	return calls
}

// outgoingCalls reports every resolvable function the given item calls, with the
// ranges of each call expression inside the item's body.
func (s *Server) outgoingCalls(item callHierarchyItem) []callHierarchyOutgoingCall {
	_, sources := s.navigationIndex(item.URI, s.documents[item.URI])
	functions := allFunctions(sources)
	byName := map[string]callHierarchyFunction{}
	for _, fn := range functions {
		if _, exists := byName[fn.name]; !exists {
			byName[fn.name] = fn
		}
	}
	caller, ok := findFunction(functions, item)
	if !ok {
		return []callHierarchyOutgoingCall{}
	}
	tokens := lexCompletionTokens(sourceForURI(sources, item.URI))
	calls := []callHierarchyOutgoingCall{}
	byCallee := map[string]int{}
	for _, site := range callSitesIn(tokens, caller.bodyStart, caller.bodyEnd) {
		callee, known := byName[site.name]
		if !known {
			continue
		}
		idx, seen := byCallee[site.name]
		if !seen {
			idx = len(calls)
			byCallee[site.name] = idx
			calls = append(calls, callHierarchyOutgoingCall{To: callee.item()})
		}
		calls[idx].FromRanges = append(calls[idx].FromRanges, site.rng)
	}
	return calls
}

// allFunctions scans every package source for function definitions.
func allFunctions(sources []navigationSource) []callHierarchyFunction {
	functions := []callHierarchyFunction{}
	for _, src := range sources {
		functions = append(functions, scanFunctions(src.uri, lexCompletionTokens(src.source))...)
	}
	return functions
}

// scanFunctions collects each function definition (one with a body) in a source.
func scanFunctions(uri string, tokens []token.Token) []callHierarchyFunction {
	functions := []callHierarchyFunction{}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Function || i+1 >= len(tokens) || tokens[i+1].Type != token.Ident {
			continue
		}
		headerEnd := declarationHeaderEnd(tokens, i)
		if headerEnd >= len(tokens) || tokens[headerEnd].Type != token.LBrace {
			i = skipDeclarationBody(tokens, i+1)
			continue
		}
		bodyEnd := skipBalanced(tokens, headerEnd, token.LBrace, token.RBrace)
		functions = append(functions, callHierarchyFunction{
			name:      tokens[i+1].Literal,
			uri:       uri,
			selection: tokenRange(tokens[i+1]),
			full:      rangeFromTokenSpan(tokens, i, bodyEnd),
			bodyStart: headerEnd,
			bodyEnd:   bodyEnd,
		})
		i = bodyEnd
	}
	return functions
}

// callSitesIn finds every call expression — an identifier directly followed by
// "(" that is not a function declaration — within the token range [start, end].
func callSitesIn(tokens []token.Token, start int, end int) []callSite {
	sites := []callSite{}
	for i := start; i <= end && i < len(tokens); i++ {
		if tokens[i].Type != token.Ident {
			continue
		}
		if i+1 >= len(tokens) || tokens[i+1].Type != token.LParen {
			continue
		}
		if i > 0 && tokens[i-1].Type == token.Function {
			continue
		}
		sites = append(sites, callSite{name: tokens[i].Literal, rng: tokenRange(tokens[i])})
	}
	return sites
}

// findFunction locates the scanned function matching a wire-level item by uri
// and selection range, so outgoing calls scan the exact definition requested.
func findFunction(
	functions []callHierarchyFunction,
	item callHierarchyItem,
) (callHierarchyFunction, bool) {
	for _, fn := range functions {
		if fn.uri == item.URI && fn.name == item.Name && fn.selection == item.SelectionRange {
			return fn, true
		}
	}
	return callHierarchyFunction{}, false
}

// callerKey uniquely identifies a caller by location for grouping.
func callerKey(fn callHierarchyFunction) string {
	return fn.uri + "#" + fn.name + ":" +
		itoa(fn.selection.Start.Line) + ":" + itoa(fn.selection.Start.Character)
}

// sourceForURI returns the source text of a navigation source by uri.
func sourceForURI(sources []navigationSource, uri string) string {
	for _, src := range sources {
		if src.uri == uri {
			return src.source
		}
	}
	return ""
}
