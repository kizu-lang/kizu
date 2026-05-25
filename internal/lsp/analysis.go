package lsp

// checkedDocument is the shared checked result for a tracked document.
type checkedDocument struct {
	Source      string
	Diagnostics []Diagnostic
}

// checkedDocument returns cached diagnostics for a tracked document source.
func (s *Server) checkedDocument(uri string) checkedDocument {
	source, ok := s.documents[uri]
	if !ok {
		return checkedDocument{}
	}
	if cached, ok := s.analysis[uri]; ok && cached.Source == source {
		return cached
	}
	result := checkedDocument{
		Source:      source,
		Diagnostics: s.analyzeDocument(uri),
	}
	s.analysis[uri] = result
	return result
}
