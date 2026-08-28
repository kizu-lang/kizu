package ast

import "github.com/kizu-lang/kizu/internal/typ"

// ReleaseNamesAllocator returns the base type names whose release names an
// allocator, which is to say whose `deinit` takes one (ADR-0132).
//
// Generic cleanup has to know. A container releasing owner elements calls each
// element's deinit, and an owner that frees memory and one that closes a
// descriptor do not take the same argument. Assuming the allocator is what
// kept a socket out of an Array.
//
// A derived deinit always takes one, because the body it derives hands the
// allocator to every field it consumes. Only a declared deinit gets to say
// otherwise, and it says it in its parameter list.
func ReleaseNamesAllocator(program *Program) map[string]bool {
	names := map[string]bool{"std::arena::Arena": true}
	declared := map[string]bool{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*FunctionDecl)
		if !ok || !fn.Receiver {
			continue
		}
		receiver, method, ok := typ.SplitMethodName(fn.Name)
		if !ok || method != typ.CleanupMethod {
			continue
		}
		base := baseTypeName(receiver)
		declared[base] = true
		if deinitTakesAllocator(fn) {
			names[base] = true
		}
	}
	// Everything else that owes cleanup owes it through fields, and the body
	// that discharges those is derived (DeriveDeinit) with an allocator.
	for owner := range DeinitOwners(program) {
		if !declared[owner] {
			names[owner] = true
		}
	}
	return names
}

// deinitTakesAllocator reads a declared cleanup's parameter list. The receiver
// is the first parameter, so an allocator is the one behind it.
func deinitTakesAllocator(fn *FunctionDecl) bool {
	for index, param := range fn.Params {
		if index == 0 {
			continue
		}
		if typ.Text(param.TypeName) == "Allocator" {
			return true
		}
	}
	return false
}

// ReleaseNames reports whether releasing typeName names an allocator under a
// ReleaseNamesAllocator map. A generic application answers with its base, the
// way OwnerType does.
func ReleaseNames(names map[string]bool, typeName string) bool {
	return names[baseTypeName(typeName)]
}
