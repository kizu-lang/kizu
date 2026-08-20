package ast

import (
	"github.com/kizu-lang/kizu/internal/typ"
)

// DeinitOwners returns the base type names whose values carry a deinit
// contract: every receiver a declared deinit names, plus Arena, whose deinit is
// builtin-only and never declared in kizu source, plus every type that holds
// one of those. This is the one definition of owner-ness; the checkers seed
// their lookups from it.
//
// Holding an owner is what makes a type an owner, whether or not it says so:
// the obligation is its fields' and the body that discharges them is derived
// (DeriveDeinit). Holding one can make another one, so this repeats until the
// set stops growing.
func DeinitOwners(program *Program) map[string]bool {
	owners := map[string]bool{"std::arena::Arena": true}
	for _, decl := range program.Decls {
		fn, ok := decl.(*FunctionDecl)
		if !ok || !fn.Receiver {
			continue
		}
		if receiver, method, ok := typ.SplitMethodName(fn.Name); ok && method == typ.CleanupMethod {
			owners[baseTypeName(receiver)] = true
		}
	}
	for grew := true; grew; {
		grew = false
		for _, decl := range program.Decls {
			name, holds := declaredHolder(owners, decl)
			if !holds || owners[name] {
				continue
			}
			owners[name] = true
			grew = true
		}
	}
	return owners
}

// declaredHolder names the type a declaration declares and reports whether it
// holds something that owes cleanup.
func declaredHolder(owners map[string]bool, decl Decl) (string, bool) {
	switch d := decl.(type) {
	case *StructDecl:
		for _, field := range d.Fields {
			if holdsOwner(owners, typ.Text(field.TypeName)) {
				return d.Name, true
			}
		}
		return d.Name, false
	case *UnionDecl:
		for _, variant := range d.Variants {
			if variant.Payload != nil && holdsOwner(owners, typ.Text(variant.Payload)) {
				return d.Name, true
			}
		}
		return d.Name, false
	default:
		return "", false
	}
}

// holdsOwner reports whether a field or payload type carries an obligation.
// `?T` carries the element's, conditionally (ADR-0125).
func holdsOwner(owners map[string]bool, text string) bool {
	if elem, ok := typ.OptionalElem(text); ok {
		return OwnerType(owners, elem)
	}
	return OwnerType(owners, text)
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
