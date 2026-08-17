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

// CleanupMethodName picks the one cleanup call a type accepts (ADR-0091):
// `deinit_all` for an owner-element container, `deinit` for every other owner.
func CleanupMethodName(typeText string, owners map[string]bool) string {
	base, arg, ok := typ.SplitApply(typeText)
	if !ok || (base != "std::array::Array" && base != "std::mem::Box") {
		return "deinit"
	}
	if owners[baseTypeName(arg)] {
		return "deinit_all"
	}
	return "deinit"
}
