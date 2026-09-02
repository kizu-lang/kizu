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
}

// newResult creates an empty ownership result.
func newResult() Result {
	return Result{
		returnRetiredErrDefers:    map[*ast.ReturnStmt][]ast.Expression{},
		tryRetiredErrDefers:       map[*ast.TryExpr][]ast.Expression{},
		functionPointerMutBorrows: map[ast.Expression]bool{},
	}
}

// FunctionPointerMutablyBorrows reports whether expr is an argument place an
// indirect call hands over as caller storage.
func (r Result) FunctionPointerMutablyBorrows(expr ast.Expression) bool {
	return r.functionPointerMutBorrows[expr]
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
