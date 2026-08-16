package ast

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/typ"
)

// ExpandFieldsDeinit replaces every `= fields;` deinit declaration with its
// generated body: a struct receiver deinitializes its owner fields in
// declaration order, a union receiver dispatches its owner payloads through a
// match. It runs once, in the project loader every frontend shares, before any
// checker reads bodies — the generated statements are then checked exactly
// like handwritten ones (ADR-0091). Only the body is generated; every call to
// it stays written in source.
func ExpandFieldsDeinit(program *Program) error {
	if !hasFieldsDeinit(program) {
		return nil
	}
	owners := DeinitOwners(program)
	structs := map[string]*StructDecl{}
	unions := map[string]*UnionDecl{}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			structs[d.Name] = d
		case *UnionDecl:
			unions[d.Name] = d
		}
	}
	for _, decl := range program.Decls {
		fn, ok := decl.(*FunctionDecl)
		if !ok || !fn.FieldsBody || fn.Body != nil {
			continue
		}
		receiver, err := fieldsDeinitReceiver(fn)
		if err != nil {
			return err
		}
		if structDecl, ok := structs[receiver]; ok {
			fn.Body = fieldsDeinitStructBody(fn.Params[0].Name, structDecl, owners)
			continue
		}
		if unionDecl, ok := unions[receiver]; ok {
			fn.Body = fieldsDeinitUnionBody(fn.Params[0].Name, unionDecl, owners)
			continue
		}
		return fmt.Errorf(
			"type error: `= fields` deinit requires a declared struct or union receiver, got `%s`",
			receiver)
	}
	return nil
}

// hasFieldsDeinit reports whether any declaration still awaits its body.
func hasFieldsDeinit(program *Program) bool {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*FunctionDecl); ok && fn.FieldsBody && fn.Body == nil {
			return true
		}
	}
	return false
}

// DeinitOwners returns the base type names whose values carry a deinit
// contract: every receiver a declared deinit names, plus Arena, whose deinit is
// builtin-only and never declared in kizu source. This is the one definition of
// owner-ness; the ownership checker seeds its own lookup from it.
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

// baseTypeName strips a generic application down to the applied name.
func baseTypeName(name string) string {
	if base, _, ok := typ.SplitApply(name); ok {
		return base
	}
	return name
}

// fieldsDeinitReceiver validates the one declaration shape `= fields` accepts.
func fieldsDeinitReceiver(fn *FunctionDecl) (string, error) {
	receiver, method, ok := typ.SplitMethodName(fn.Name)
	if !fn.Receiver || !ok || len(fn.Params) != 1 {
		return "", fmt.Errorf(
			"type error: `= fields` requires a deinit method with a receiver and no arguments")
	}
	returns := "void"
	if fn.ReturnType != nil {
		returns = typ.Text(fn.ReturnType)
	}
	if method != "deinit" || returns != "void" {
		return "", fmt.Errorf("type error: `= fields` is only for `deinit() -> void`")
	}
	return receiver, nil
}

// deinitCallStmt builds the one statement both generators emit: a deinit call
// on the given receiver expression.
func deinitCallStmt(receiver Expression) Statement {
	return &ExprStmt{
		Expr:      &CallExpr{Callee: &FieldExpr{Receiver: receiver, Name: "deinit"}},
		Semicolon: true,
	}
}

// fieldsDeinitStructBody deinitializes owner fields in declaration order.
func fieldsDeinitStructBody(self string, decl *StructDecl, owners map[string]bool) *BlockStmt {
	stmts := []Statement{}
	for _, field := range decl.Fields {
		if !owners[baseTypeName(typ.Text(field.TypeName))] {
			continue
		}
		stmts = append(stmts, deinitCallStmt(
			&FieldExpr{Receiver: &IdentExpr{Name: self}, Name: field.Name}))
	}
	stmts = append(stmts, &ReturnStmt{})
	return &BlockStmt{Statements: stmts}
}

// fieldsDeinitUnionBody dispatches the active owner payload through a match.
// The owner-union cleanup contract (ADR-0075) wants every variant spelled out,
// so variants without an owner payload get an explicit no-op return arm.
func fieldsDeinitUnionBody(self string, decl *UnionDecl, owners map[string]bool) *BlockStmt {
	arms := []MatchArm{}
	hasOwner := false
	for _, variant := range decl.Variants {
		arm := MatchArm{Tag: variant.Name, Body: &ReturnStmt{}}
		switch {
		case variant.Payload == nil:
		case owners[baseTypeName(typ.Text(variant.Payload))]:
			hasOwner = true
			arm.Binding = "payload"
			arm.Body = deinitCallStmt(&IdentExpr{Name: "payload"})
		default:
			arm.Binding = "_"
		}
		arms = append(arms, arm)
	}
	if !hasOwner {
		return &BlockStmt{Statements: []Statement{&ReturnStmt{}}}
	}
	return &BlockStmt{Statements: []Statement{
		&MatchStmt{Value: &IdentExpr{Name: self}, Arms: arms},
		&ReturnStmt{},
	}}
}
