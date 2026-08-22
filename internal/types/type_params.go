package types

import "github.com/kizu-lang/kizu/internal/ast"

// typeParamScope is the function or declaration type parameters currently in
// scope. Entering another generic replaces this set until its caller restores
// the previous scope; outer type parameters are not implicitly captured.
type typeParamScope map[string]bool

// typeParamStore owns the currently selected type-parameter scope.
type typeParamStore struct {
	current typeParamScope
}

// enter selects params and returns the scope the caller must restore.
func (s *typeParamStore) enter(params []string) typeParamScope {
	previous := s.current
	if len(params) == 0 {
		s.current = nil
		return previous
	}
	names := make(typeParamScope, len(params))
	for _, param := range params {
		names[param] = true
	}
	s.current = names
	return previous
}

// enterSignature selects only the type-valued static parameters. Static value
// parameters live in lexical scope and must not become type names here.
func (s *typeParamStore) enterSignature(signature ast.FunctionSignature) typeParamScope {
	previous := s.current
	var names typeParamScope
	for _, param := range signature.StaticParams {
		if !param.IsType() {
			continue
		}
		if names == nil {
			names = typeParamScope{}
		}
		names[param.Name] = true
	}
	s.current = names
	return previous
}

// restore selects the scope returned by enter.
func (s *typeParamStore) restore(previous typeParamScope) {
	s.current = previous
}

// contains reports whether name belongs to the selected scope.
func (s *typeParamStore) contains(name string) bool {
	return s.current[name]
}
