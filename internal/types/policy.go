package types

import (
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
// `binding.deinit()`, which releases the payload with it. Which the payload
// type accepts is enforced where the arm body is checked.
func ownerUnionDeinitCall(expr ast.Expression, binding string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace || field.Name != typ.CleanupMethod {
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

// reservedFunctionStaticParamIndex finds a `<...>` entry that may not be
// declared here. `Function`
// is a function-name token a std wrapper forwards to a trusted primitive, not a
// value a body can call, so declaring one outside std is rejected where it is
// written rather than where the name fails to resolve.
//
// `Field` is not restricted. It is what a `std::meta::construct` worker takes,
// and a decoder anyone can write is the test of whether construct belongs in
// the language at all: a form only std can use would make std privileged over
// the thing its own users need to write. The caller owns the diagnostic because
// it owns the surrounding function-checking phase.
func reservedFunctionStaticParamIndex(fn ast.FunctionSignature) (int, bool) {
	for index, param := range fn.StaticParams {
		if Type(typ.Text(param.Type)) != typeFunction || fn.Std {
			continue
		}
		return index, true
	}
	return 0, false
}

// compileTimeOnlyType reports whether a spelling holds a compile-time token
// rather than a type values can have. `Function` names a function and `Field`
// names one field of a struct; both are read where they are written and have
// no runtime representation, so neither can be stored, returned, or passed --
// nor wrapped, since `?Function` is a value of a type that has no values.
func compileTimeOnlyType(table *typeTable, typ Type) bool {
	return table.containsCompileTimeOnly(typ)
}

// rawPointerFieldRequiresUnsafe reports whether a raw-pointer field belongs to
// a struct that has not named the invariant the compiler cannot check.
func rawPointerFieldRequiresUnsafe(
	table *typeTable,
	requiresUnsafe bool,
	fieldType Type,
) bool {
	return !requiresUnsafe && table.containsRawPointer(fieldType)
}

// isBorrowPayload reports whether a union payload is structurally a borrow.
func isBorrowPayload(payload typ.Type) bool {
	_, ok := payload.(*typ.Borrow)
	return ok
}
