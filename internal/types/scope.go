package types

import (
	"github.com/kizu-lang/kizu/internal/ast"
)

// scopeBinding keeps every fact about one lexical name in one record. A name
// is present in exactly one map, so lookup cannot observe metadata without its
// type or leave metadata behind after the binding changes.
type scopeBinding struct {
	typ            Type
	mutable        bool
	borrowed       bool
	mutBorrow      bool
	signatureParam bool
	borrowSources  []string
	unread         bool
	declaration    ast.Span
}

type scope struct {
	parent   *scope
	bindings map[string]scopeBinding
}

// newScope creates a lexical type scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, bindings: map[string]scopeBinding{}}
}

// child creates a nested lexical type scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// declareLocal records a binding this scope will be asked about. `_` is the
// name for a value that is deliberately dropped, so it is never asked about.
func (s *scope) declareLocal(name string, span ast.Span) {
	if name == discardName {
		return
	}
	binding, exists := s.bindings[name]
	if !exists {
		return
	}
	binding.unread = true
	binding.declaration = span
	s.bindings[name] = binding
}

// discardName is written where a value is produced on purpose and not kept.
const discardName = "_"

// define binds a local name to a type in the current scope. False reports a
// duplicate; the checker that owns source context constructs its diagnostic.
func (s *scope) define(name string, typ Type, mutable bool) bool {
	return s.defineBinding(name, scopeBinding{typ: typ, mutable: mutable})
}

// defineWithSource binds a non-borrow local while preserving view provenance.
func (s *scope) defineWithSource(name string, typ Type, mutable bool, sources []string) bool {
	return s.defineBinding(name, scopeBinding{
		typ: typ, mutable: mutable, borrowSources: sources,
	})
}

// defineParamWithSource binds a borrowed local and records its source owners.
func (s *scope) defineParamWithSource(
	name string,
	typ Type,
	borrowed bool,
	mutBorrow bool,
	sources []string,
	signatureParam bool,
) bool {
	return s.defineBinding(name, scopeBinding{
		typ: typ, borrowed: borrowed, mutBorrow: mutBorrow,
		signatureParam: signatureParam, borrowSources: sources,
	})
}

// defineBinding retains one complete name record without spreading its fields
// across parallel maps.
func (s *scope) defineBinding(name string, binding scopeBinding) bool {
	if name == discardName {
		return true
	}
	if _, exists := s.bindings[name]; exists {
		return false
	}
	s.bindings[name] = binding
	return true
}

// lookup resolves a local name by walking parent scopes and marks a local read.
func (s *scope) lookup(name string) (Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		binding, ok := cur.bindings[name]
		if !ok {
			continue
		}
		if binding.unread {
			binding.unread = false
			cur.bindings[name] = binding
		}
		return binding.typ, true
	}
	return "", false
}

// binding resolves metadata without counting the operation as a value read.
func (s *scope) binding(name string) (scopeBinding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if binding, ok := cur.bindings[name]; ok {
			return binding, true
		}
	}
	return scopeBinding{}, false
}

// isMutable reports whether a resolved local name may be assigned.
func (s *scope) isMutable(name string) bool {
	binding, ok := s.binding(name)
	return ok && binding.mutable
}

// isMutBorrowedParam reports whether a resolved name is a `&var` signature
// parameter: the one mut-borrow binding that is the caller's storage.
func (s *scope) isMutBorrowedParam(name string) bool {
	binding, ok := s.binding(name)
	return ok && binding.mutBorrow && binding.signatureParam
}

// isBorrowed reports whether a local name is an &T or &var T binding.
func (s *scope) isBorrowed(name string) bool {
	binding, ok := s.binding(name)
	return ok && binding.borrowed
}

// isMutBorrowed reports whether a local name is an &var T binding.
func (s *scope) isMutBorrowed(name string) bool {
	binding, ok := s.binding(name)
	return ok && binding.mutBorrow
}
