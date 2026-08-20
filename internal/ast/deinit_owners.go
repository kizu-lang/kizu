package ast

import (
	"github.com/kizu-lang/kizu/internal/typ"
)

// DeinitOwners returns the base type names whose values carry a deinit
// contract: every receiver a declared deinit names, plus Arena, whose deinit is
// builtin-only and never declared in kizu source. This is the one definition of
// owner-ness; the checkers seed their lookups from it.
func DeinitOwners(program *Program) map[string]bool {
	owners := map[string]bool{"std::arena::Arena": true}
	for _, decl := range program.Decls {
		fn, ok := decl.(*FunctionDecl)
		if !ok || !fn.Receiver {
			continue
		}
		if receiver, method, ok := typ.SplitMethodName(fn.Name); ok && method == "deinit" {
			owners[baseTypeName(receiver)] = true
		}
	}
	return owners
}

// OwnerType reports whether values of typeName carry a deinit contract under a
// DeinitOwners map. A generic application is an owner when its base declares
// deinit, which is how `Array<String>` — a container that must consume its
// elements — reads as owner.
func OwnerType(owners map[string]bool, typeName string) bool {
	return owners[baseTypeName(typeName)]
}

// baseTypeName strips a generic application down to the applied name.
func baseTypeName(name string) string {
	if base, _, ok := typ.SplitApply(name); ok {
		return base
	}
	return name
}

// CleanupMethod is the cleanup call every owner accepts. There is one:
// `deinit` releases the value and whatever it holds, so a container needs no
// second name for the case where its elements own something (ADR-0119).
const CleanupMethod = "deinit"
