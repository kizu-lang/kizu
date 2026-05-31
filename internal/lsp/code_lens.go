package lsp

// codeLenses annotates each top-level declaration with its reference count, so
// editors can show "N references" above functions and types. The lens command
// targets the declaration so a client can open the reference list on click.
func (s *Server) codeLenses(uri string) []codeLens {
	source, ok := s.documents[uri]
	if !ok {
		return []codeLens{}
	}
	lenses := []codeLens{}
	for _, symbol := range DocumentSymbols(source) {
		if !codeLensEligible(symbol.Kind) {
			continue
		}
		position := symbol.SelectionRange.Start
		count := len(s.references(uri, position, false))
		lenses = append(lenses, codeLens{
			Range: symbol.SelectionRange,
			Command: &command{
				Title:     referenceCountTitle(count),
				Command:   "kizu.showReferences",
				Arguments: []any{uri, position},
			},
		})
	}
	return lenses
}

// codeLensEligible reports whether a symbol kind carries a reference lens.
func codeLensEligible(kind int) bool {
	switch kind {
	case symbolKindFunction, symbolKindStruct, symbolKindEnum, symbolKindInterface:
		return true
	default:
		return false
	}
}

// referenceCountTitle renders the lens label for a reference count.
func referenceCountTitle(count int) string {
	if count == 1 {
		return "1 reference"
	}
	return itoa(count) + " references"
}
