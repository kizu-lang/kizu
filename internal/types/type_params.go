package types

import "github.com/kizu-lang/kizu/internal/ast"

// typeParamScope is the function or declaration static parameters currently
// in scope. Entering another generic replaces this set until its caller
// restores the previous scope; outer parameters are not implicitly captured.
type typeParamScope struct {
	// types are the type parameters: names a type spelling may use.
	types map[string]bool
	// lengths are the integer value parameters: names an array length may
	// use, as in `[n]f64`, until an instance binds them.
	lengths map[string]bool
}

// typeParamStore owns the currently selected type-parameter scope.
type typeParamStore struct {
	current typeParamScope
}

// enter selects params and returns the scope the caller must restore.
func (s *typeParamStore) enter(params []string) typeParamScope {
	previous := s.current
	s.current = typeParamScope{}
	if len(params) == 0 {
		return previous
	}
	s.current.types = make(map[string]bool, len(params))
	for _, param := range params {
		s.current.types[param] = true
	}
	return previous
}

// enterSignature selects the static parameters of a signature. Static value
// parameters live in lexical scope and must not become type names here; an
// integer one may still stand for an array length.
func (s *typeParamStore) enterSignature(signature ast.FunctionSignature) typeParamScope {
	previous := s.current
	s.current = typeParamScope{}
	for _, param := range signature.StaticParams {
		if param.IsType() {
			if s.current.types == nil {
				s.current.types = map[string]bool{}
			}
			s.current.types[param.Name] = true
			continue
		}
		switch Type(param.Type.String()) {
		case typeBool, typeFunction, typeField:
			continue
		}
		if s.current.lengths == nil {
			s.current.lengths = map[string]bool{}
		}
		s.current.lengths[param.Name] = true
	}
	return previous
}

// restore selects the scope returned by enter.
func (s *typeParamStore) restore(previous typeParamScope) {
	s.current = previous
}

// contains reports whether name is a type parameter in the selected scope.
func (s *typeParamStore) contains(name string) bool {
	return s.current.types[name]
}

// containsLength reports whether name is an integer static parameter in the
// selected scope.
func (s *typeParamStore) containsLength(name string) bool {
	return s.current.lengths[name]
}
