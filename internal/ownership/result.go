package ownership

import "github.com/kizu-lang/kizu/internal/ast"

// Result carries ownership facts consumed by later compiler phases. The AST
// remains syntax owned by parsing; checking records its phase-specific output
// here instead of mutating syntax nodes.
type Result struct {
	returnRetiredErrDefers map[*ast.ReturnStmt][]string
	tryRetiredErrDefers    map[*ast.TryExpr][]string
	// functionPointerMutBorrows marks the argument places an indirect call
	// lends as &var. IR slot analysis cannot recover a local pointer's type
	// from syntax alone, so ownership carries that checked fact forward.
	functionPointerMutBorrows map[ast.Expression]bool
}

// newResult creates an empty ownership result.
func newResult() Result {
	return Result{
		returnRetiredErrDefers:    map[*ast.ReturnStmt][]string{},
		tryRetiredErrDefers:       map[*ast.TryExpr][]string{},
		functionPointerMutBorrows: map[ast.Expression]bool{},
	}
}

// FunctionPointerMutablyBorrows reports whether expr is an argument place an
// indirect call hands over as caller storage.
func (r Result) FunctionPointerMutablyBorrows(expr ast.Expression) bool {
	return r.functionPointerMutBorrows[expr]
}

// RetiredErrDefersForReturn lists the active errdefer receivers an error
// return must skip because an earlier move retired them.
func (r Result) RetiredErrDefersForReturn(stmt *ast.ReturnStmt) []string {
	return r.returnRetiredErrDefers[stmt]
}

// RetiredErrDefersForTry lists the active errdefer receivers a try error path
// must skip because an earlier move retired them.
func (r Result) RetiredErrDefersForTry(expr *ast.TryExpr) []string {
	return r.tryRetiredErrDefers[expr]
}
