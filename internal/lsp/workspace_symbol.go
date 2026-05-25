package lsp

import "strings"

// workspaceSymbols returns indexed symbols from currently tracked workspaces.
func (s *Server) workspaceSymbols(query string) []symbolInformation {
	query = strings.ToLower(query)
	symbols := []symbolInformation{}
	seen := map[string]bool{}
	for uri, source := range s.documents {
		index, _ := s.navigationIndex(uri, source)
		for _, decl := range allNavigationDeclarations(index) {
			if query != "" && !strings.Contains(strings.ToLower(decl.name), query) {
				continue
			}
			loc := location{URI: decl.uri, Range: decl.rng}
			key := decl.name + ":" + locationKey(loc)
			if seen[key] {
				continue
			}
			seen[key] = true
			symbols = append(symbols, symbolInformation{
				Name:          decl.name,
				Kind:          decl.kind,
				Location:      loc,
				ContainerName: decl.container,
			})
		}
	}
	return symbols
}

// allNavigationDeclarations flattens indexed declarations for feature reuse.
func allNavigationDeclarations(index navigationIndex) []navigationDeclaration {
	decls := []navigationDeclaration{}
	for _, decl := range index.functions {
		decls = append(decls, decl)
	}
	for _, decl := range index.types {
		decls = append(decls, decl)
	}
	for _, decl := range index.modules {
		decls = append(decls, decl)
	}
	appendNestedDeclarations := func(table map[string]map[string]navigationDeclaration) {
		for owner, nested := range table {
			for _, decl := range nested {
				if decl.container == "" {
					decl.container = owner
				}
				decls = append(decls, decl)
			}
		}
	}
	appendNestedDeclarations(index.enumVariants)
	appendNestedDeclarations(index.unionVariants)
	appendNestedDeclarations(index.fields)
	appendNestedDeclarations(index.methods)
	return decls
}
