package ownership

import "github.com/kizu-lang/kizu/internal/ast"

// Result carries ownership facts consumed by later compiler phases. The AST
// remains syntax owned by parsing; checking records its phase-specific output
// here instead of mutating syntax nodes.
type Result struct {
	// Retired errdefer cleanups are named by the receiver expression the
	// `errdefer` statement was written with: one node per registration, so
	// two registrations on one name stay apart.
	returnRetiredErrDefers map[*ast.ReturnStmt][]ast.Expression
	tryRetiredErrDefers    map[*ast.TryExpr][]ast.Expression
	// functionPointerMutBorrows marks the argument places an indirect call
	// lends as &var. IR slot analysis cannot recover a local pointer's type
	// from syntax alone, so ownership carries that checked fact forward.
	functionPointerMutBorrows map[ast.Expression]bool
	// methodCallees names the declarations each method call resolved to, one
	// per receiver type a generic instantiation gave it. Which `&var`
	// parameter a call lends is a fact of the method it reaches, and only the
	// checked receiver type says which method that is: two types may declare
	// the same method name with `&var` at different positions.
	methodCallees map[*ast.FieldExpr][]string
}

// newResult creates an empty ownership result.
func newResult() Result {
	return Result{
		returnRetiredErrDefers:    map[*ast.ReturnStmt][]ast.Expression{},
		tryRetiredErrDefers:       map[*ast.TryExpr][]ast.Expression{},
		functionPointerMutBorrows: map[ast.Expression]bool{},
		methodCallees:             map[*ast.FieldExpr][]string{},
	}
}

// FunctionPointerMutablyBorrows reports whether expr is an argument place an
// indirect call hands over as caller storage.
func (r Result) FunctionPointerMutablyBorrows(expr ast.Expression) bool {
	return r.functionPointerMutBorrows[expr]
}

// MethodCallees lists the declarations the method call whose callee is field
// resolved to, and reports whether the checker resolved it at all.
func (r Result) MethodCallees(field *ast.FieldExpr) ([]string, bool) {
	callees, ok := r.methodCallees[field]
	return callees, ok
}

// recordMethodCallee adds callee to the declarations field resolved to. A body
// checked twice -- a loop to its fixed point, a generic for one type twice --
// resolves the same call to the same declaration, which is recorded once.
func (r Result) recordMethodCallee(field *ast.FieldExpr, callee string) {
	for _, known := range r.methodCallees[field] {
		if known == callee {
			return
		}
	}
	r.methodCallees[field] = append(r.methodCallees[field], callee)
}

// RetiredErrDefersForReturn lists the receiver expressions of the active
// errdefer cleanups an error return must skip because an earlier move
// retired them.
func (r Result) RetiredErrDefersForReturn(stmt *ast.ReturnStmt) []ast.Expression {
	return r.returnRetiredErrDefers[stmt]
}

// RetiredErrDefersForTry lists the receiver expressions of the active errdefer
// cleanups a try error path must skip because an earlier move retired them.
func (r Result) RetiredErrDefersForTry(expr *ast.TryExpr) []ast.Expression {
	return r.tryRetiredErrDefers[expr]
}
