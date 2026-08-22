package ownership

import "github.com/kizu-lang/kizu/internal/ast"

// Result carries ownership facts consumed by later compiler phases. The AST
// remains syntax owned by parsing; checking records its phase-specific output
// here instead of mutating syntax nodes.
type Result struct {
	returnRetiredErrDefers map[*ast.ReturnStmt][]string
	tryRetiredErrDefers    map[*ast.TryExpr][]string
}

// newResult creates an empty ownership result.
func newResult() Result {
	return Result{
		returnRetiredErrDefers: map[*ast.ReturnStmt][]string{},
		tryRetiredErrDefers:    map[*ast.TryExpr][]string{},
	}
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
