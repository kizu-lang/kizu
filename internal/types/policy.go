package types

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// ownerUnionSelfMatch returns the `match self { ... }` statement only when it is
// the first executable statement of the deinit body. Requiring it first keeps the
// shape simple and guarantees the active-variant cleanup always runs: a
// statement (such as an early `return`) cannot precede and skip it.
func ownerUnionSelfMatch(body *ast.BlockStmt, selfName string) *ast.MatchStmt {
	if body == nil || len(body.Statements) == 0 {
		return nil
	}
	switch s := body.Statements[0].(type) {
	case *ast.MatchStmt:
		if ownerUnionIdentName(s.Value) == selfName {
			return s
		}
	case *ast.ExprStmt:
		if m, ok := s.Expr.(*ast.MatchStmt); ok && ownerUnionIdentName(m.Value) == selfName {
			return m
		}
	}
	return nil
}

// matchArmCleansPayload reports whether an arm body is exactly the direct cleanup
// call `<binding>.deinit()`. Only the direct form is accepted so the
// active payload is always cleaned without path-sensitive analysis of the arm.
func matchArmCleansPayload(body ast.Statement, binding string) bool {
	expr, ok := body.(*ast.ExprStmt)
	if !ok {
		return false
	}
	return ownerUnionDeinitCall(expr.Expr, binding)
}

// ownerUnionDeinitCall reports whether expr is the cleanup call
// `binding.deinit()` or `binding.deinit_all()`. Which of the two the payload
// type accepts is enforced where the arm body is checked.
func ownerUnionDeinitCall(expr ast.Expression, binding string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace || !typ.CleanupMethod(field.Name) {
		return false
	}
	return ownerUnionIdentName(field.Receiver) == binding
}

// ownerUnionIdentName returns the identifier name of expr, or "" when not a name.
func ownerUnionIdentName(expr ast.Expression) string {
	if ident, ok := expr.(*ast.IdentExpr); ok {
		return ident.Name
	}
	return ""
}

// checkStaticParamPolicy validates what a `<...>` entry may declare. `Function`
// is a function-name token a std wrapper forwards to a trusted primitive, not a
// value a body can call, so declaring one outside std is rejected where it is
// written rather than where the name fails to resolve.
//
// `Field` is not restricted. It is what a `std::meta::construct` worker takes,
// and a decoder anyone can write is the test of whether construct belongs in
// the language at all: a form only std can use would make std privileged over
// the thing its own users need to write.
func checkStaticParamPolicy(fn ast.FunctionSignature) error {
	for _, param := range fn.StaticParams {
		if Type(typ.Text(param.Type)) != typeFunction || fn.Std {
			continue
		}
		return errorf(
			"type error: Function static parameter `%s` is reserved for std", param.Name)
	}
	return nil
}

// compileTimeOnlyType reports whether a spelling holds a compile-time token
// rather than a type values can have. `Function` names a function and `Field`
// names one field of a struct; both are read where they are written and have
// no runtime representation, so neither can be stored, returned, or passed --
// nor wrapped, since `?Function` is a value of a type that has no values.
func compileTimeOnlyType(typ Type) bool {
	return containsWrappedType(typ, func(typ Type) bool {
		return typ == typeFunction || typ == typeField
	})
}

// checkFunctionParamPolicy keeps the compile-time-only tokens out of the
// runtime argument list. A function name and a field token are both known only
// at compile time, so they are static arguments.
func checkFunctionParamPolicy(param ast.Param, typ Type) error {
	if !compileTimeOnlyType(typ) {
		return nil
	}
	return errorf(
		"type error: %s parameter `%s` belongs in `<...>`, not `(...)`", typ, param.Name)
}

// checkStructFieldBorrowPolicy rejects borrow fields until a non-lifetime model exists.
func checkStructFieldBorrowPolicy(decl *ast.StructDecl, field ast.Field) error {
	if !field.Borrow {
		return nil
	}
	return errorf("type error: borrow field `%s.%s` cannot store borrow",
		decl.Name, field.Name)
}

// checkRawPointerFieldPolicy rejects a raw pointer field on a struct that has
// not said it carries an invariant the compiler cannot check.
func checkRawPointerFieldPolicy(decl *ast.StructDecl, field ast.Field, fieldType Type) error {
	if decl.RequiresUnsafe || !containsRawPointer(fieldType) {
		return nil
	}
	return errorf("unsafe error: struct `%s` holds a raw pointer in field `%s`, "+
		"so it must be declared `unsafe struct`"+
		"\nhelp: write `unsafe struct %s` and document the invariant its fields carry",
		decl.Name, field.Name, decl.Name)
}

// checkBorrowFieldPolicy rejects borrowed payloads.
func checkBorrowFieldPolicy(typeName string, fieldName string, payload string) error {
	if !strings.HasPrefix(payload, "&") {
		return nil
	}
	return errorf("type error: borrow payload `%s.%s` cannot store borrow",
		typeName, fieldName)
}
