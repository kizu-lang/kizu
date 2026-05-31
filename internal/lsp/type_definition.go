package lsp

import "strings"

// typeDefinition resolves the cursor symbol to its inferred type and returns
// the location where that type is declared. It targets local bindings and
// parameters, whose types are known from the document's type facts, and falls
// back to an empty result when the type is a builtin or cannot be resolved.
func (s *Server) typeDefinition(uri string, position Position) []location {
	source, ok := s.documents[uri]
	if !ok {
		return []location{}
	}
	doc := s.checkedDocument(uri)
	fact, ok := typeFactAt(source, position, doc.TypeFacts)
	if !ok {
		return []location{}
	}
	index, _ := s.navigationIndex(uri, source)
	if loc, ok := typeDeclarationLocation(fact.typ, index); ok {
		return []location{loc}
	}
	return []location{}
}

// typeDeclarationLocation looks up the declaration of a type spelling in the
// navigation index, reducing wrappers and module paths to the base type name.
func typeDeclarationLocation(typ string, index navigationIndex) (location, bool) {
	name := baseTypeName(typ)
	if name == "" {
		return location{}, false
	}
	decl, ok := index.types[name]
	if !ok {
		return location{}, false
	}
	return location{URI: decl.uri, Range: decl.rng}, true
}

// baseTypeName reduces a type spelling such as "&var []std::pkg::Trace" to the
// bare type name "Trace" so it can be matched against declared types.
func baseTypeName(typ string) string {
	typ = strings.TrimSpace(typ)
	for {
		trimmed := strings.TrimSpace(normalizeCompletionType(typ))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "[]"))
		if trimmed == typ {
			break
		}
		typ = trimmed
	}
	if i := strings.IndexAny(typ, "<([ "); i >= 0 {
		typ = typ[:i]
	}
	if i := strings.LastIndex(typ, "::"); i >= 0 {
		typ = typ[i+2:]
	}
	return typ
}
