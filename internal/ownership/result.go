package ownership

import "github.com/kizu-lang/kizu/internal/ast"

// Result carries ownership facts consumed by later compiler phases. The AST
// remains syntax owned by parsing; checking records its phase-specific output
// here instead of mutating syntax nodes.
type Result struct {
	returnRetiredErrDefers map[*ast.ReturnStmt][]string
	tryRetiredErrDefers    map[*ast.TryExpr][]string
	runtimeCaptureModes    map[ast.Expression]runtimeCaptureMode
}

// runtimeCaptureMode is the source-level passing mode ownership proved for a
// trailing argument. Shared borrows may have a flat runtime representation, so
// lowering cannot recover this fact from the IR type alone.
type runtimeCaptureMode struct {
	borrow  bool
	mutable bool
}

// newResult creates an empty ownership result.
func newResult() Result {
	return Result{
		returnRetiredErrDefers: map[*ast.ReturnStmt][]string{},
		tryRetiredErrDefers:    map[*ast.TryExpr][]string{},
		runtimeCaptureModes:    map[ast.Expression]runtimeCaptureMode{},
	}
}

// RuntimeCaptureMode returns the passing mode checked for one trailing call
// argument. The concrete type still comes from lowering the expression; this
// result preserves only the borrow fact a flat shared-borrow ABI would erase.
func (r Result) RuntimeCaptureMode(expr ast.Expression) (borrow bool, mutable bool, ok bool) {
	mode, ok := r.runtimeCaptureModes[expr]
	return mode.borrow, mode.mutable, ok
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
