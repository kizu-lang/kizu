package ast

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// AddDerivedDeinits gives every type that holds an owner and declares no
// `deinit` the one it would have written. Every phase reads the program after
// this, so the type checker, the ownership checker and lowering see one
// ordinary method rather than three private notions of what cleanup means.
func AddDerivedDeinits(program *Program) {
	owners := DeinitOwners(program)
	declared := map[string]bool{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*FunctionDecl)
		if !ok || !fn.Receiver {
			continue
		}
		if receiver, method, ok := typ.SplitMethodName(fn.Name); ok && method == typ.CleanupMethod {
			declared[baseTypeName(receiver)] = true
		}
	}
	derived := make([]Decl, 0, len(program.Decls))
	for _, decl := range program.Decls {
		name, holds := declaredHolder(owners, decl)
		if !holds || declared[name] {
			continue
		}
		if fn := DeriveDeinit(owners, decl); fn != nil {
			derived = append(derived, fn)
		}
	}
	program.Decls = append(program.Decls, derived...)
}

// DeriveDeinit returns the `deinit` a type gets when it declares none: the body
// that consumes each thing it holds, in declaration order.
//
//	fn (self: T) deinit(allocator: Allocator) -> void {
//	    self.f1.deinit(allocator);                          // owner field
//	    if self.f2 |held| { held.deinit(allocator); }       // ?Owner field
//	    return;
//	}
//
// A type whose obligations are its fields' obligations has no other body to
// write: the rule that a deinit consumes every owner field on every path
// (SPEC §8) already fixes it, and the fields cannot alias, so the order is
// free. Writing it by hand adds no information a reader does not already have
// from the field types, which is the boilerplate principle 10 folds away.
//
// A type that holds a raw resource — memory it took from an allocator, a
// descriptor — declares its own deinit, because that obligation is the type's
// and not any field's. Declaring one keeps it: this only fills in the types
// that declare none.
//
// nil means the type holds nothing that needs consuming, so it is not an owner
// and gets no deinit at all.
func DeriveDeinit(owners map[string]bool, decl Decl) *FunctionDecl {
	switch d := decl.(type) {
	case *StructDecl:
		return deriveStructDeinit(owners, d)
	case *UnionDecl:
		return deriveUnionDeinit(owners, d)
	default:
		return nil
	}
}

// deriveStructDeinit consumes each owner field of a struct receiver.
func deriveStructDeinit(owners map[string]bool, decl *StructDecl) *FunctionDecl {
	statements := make([]Statement, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		read := &FieldExpr{
			Receiver: &IdentExpr{Name: deriveReceiver},
			Name:     field.Name,
		}
		text := stdmeta.ResolveElementTypeForms(typ.Text(field.TypeName))
		if elem, ok := typ.OptionalElem(text); ok {
			if !OwnerType(owners, elem) {
				continue
			}
			// Opening the field is the only path that discharges its
			// obligation, and only `if` may open and consume it: a `while`
			// condition reads the same storage every turn (ADR-0125).
			statements = append(statements, &IfStmt{
				Condition:   read,
				Capture:     deriveCapture,
				Consequence: block(cleanupCall(&IdentExpr{Name: deriveCapture})),
			})
			continue
		}
		if success, ok := ErrorUnionSuccess(text); ok {
			if !OwnerType(owners, success) {
				continue
			}
			// An error union opens the same way, and its `if` names the
			// failure arm too (SPEC §11.1); a failed field holds nothing.
			statements = append(statements, &IfStmt{
				Condition:   read,
				Capture:     deriveCapture,
				Consequence: block(cleanupCall(&IdentExpr{Name: deriveCapture})),
				Alternative: &BlockStmt{},
				ErrCapture:  deriveErrCapture,
			})
			continue
		}
		if !OwnerType(owners, text) {
			continue
		}
		statements = append(statements, cleanupCall(read))
	}
	if len(statements) == 0 {
		return nil
	}
	return deriveDecl(decl.Name, decl.TypeParams, statements)
}

// deriveUnionDeinit consumes the payload of the active variant. Every variant
// needs an arm because the match must be exhaustive, and the ones carrying
// nothing get an empty body.
func deriveUnionDeinit(owners map[string]bool, decl *UnionDecl) *FunctionDecl {
	arms := make([]MatchArm, 0, len(decl.Variants))
	owned := false
	for _, variant := range decl.Variants {
		payload := stdmeta.ResolveElementTypeForms(typ.Text(variant.Payload))
		if variant.Payload == nil || !OwnerType(owners, payload) {
			arms = append(arms, MatchArm{Tag: variant.Name, Body: &BlockStmt{}})
			continue
		}
		owned = true
		arms = append(arms, MatchArm{
			Tag:     variant.Name,
			Binding: deriveCapture,
			Body:    cleanupCall(&IdentExpr{Name: deriveCapture}),
		})
	}
	if !owned {
		return nil
	}
	match := &MatchStmt{Value: &IdentExpr{Name: deriveReceiver}, Arms: arms}
	return deriveDecl(decl.Name, decl.TypeParams, []Statement{match})
}

// deriveDecl wraps a derived body in the method declaration that carries it.
func deriveDecl(owner string, typeParams []string, body []Statement) *FunctionDecl {
	params := make([]StaticParam, 0, len(typeParams))
	args := make([]typ.Type, 0, len(typeParams))
	for _, name := range typeParams {
		params = append(params, StaticParam{Name: name})
		args = append(args, typeName(name))
	}
	receiver := typeName(owner)
	receiver.Args = args
	body = append(body, &ReturnStmt{})
	return &FunctionDecl{
		Derived: true,
		FunctionSignature: FunctionSignature{
			Receiver:     true,
			Name:         owner + "." + typ.CleanupMethod,
			StaticParams: params,
			Params: []Param{
				{Name: deriveReceiver, TypeName: receiver},
				{Name: deriveAllocator, TypeName: typeName("Allocator")},
			},
			ReturnType: typeName("void"),
			// The derived body is the type's own method and reads its own
			// fields, so it carries the type's module identity: a user module
			// is read from the method name, and std is read from the path.
			Std: strings.HasPrefix(owner, "std::"),
		},
		Body: &BlockStmt{Statements: body},
	}
}

// typeName spells one `::` separated type name.
func typeName(name string) *typ.Name {
	return &typ.Name{Path: strings.Split(name, "::")}
}

// cleanupCall is `<value>.deinit(allocator);`. The allocator the derived body
// was handed is the one its fields were built from, so it is what releases
// them: a value keeps no copy of the allocator that made it (ADR-0132).
func cleanupCall(value Expression) Statement {
	return &ExprStmt{
		Expr: &CallExpr{
			Callee: &FieldExpr{Receiver: value, Name: typ.CleanupMethod},
			Args:   []Expression{&IdentExpr{Name: deriveAllocator}},
		},
		Semicolon: true,
	}
}

// block wraps one statement in a body.
func block(stmt Statement) *BlockStmt {
	return &BlockStmt{Statements: []Statement{stmt}}
}

const (
	// deriveReceiver and deriveCapture name the receiver and the opened payload
	// of a derived body. They are the names a hand-written deinit uses, and the
	// body is closed, so nothing of the author's can collide with them.
	deriveReceiver = "self"
	deriveCapture  = "held"
	// deriveErrCapture names the error member the `else |err|` of an `E!T`
	// field's open binds. The body has nothing to do with it: the field
	// held no value, so there is nothing to release.
	deriveErrCapture = "err"
	// deriveAllocator names the allocator a release is handed. It is a
	// parameter of every owner's deinit (ADR-0132), so the derived body has one
	// to pass on without keeping a copy of it in the value.
	deriveAllocator = "allocator"
)
