package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
)

// pushDeferFrame opens a lexical cleanup frame for one block.
func (l *lowerer) pushDeferFrame() int {
	l.deferFrames = append(l.deferFrames, nil)
	return len(l.deferFrames) - 1
}

// popDeferFrame closes the innermost lexical cleanup frame.
func (l *lowerer) popDeferFrame() {
	l.deferFrames = l.deferFrames[:len(l.deferFrames)-1]
}

// lowerDeferStmt records a checked deferred cleanup without emitting it now.
func (l *lowerer) lowerDeferStmt(stmt *ast.DeferStmt) error {
	return l.recordCleanup(stmt.Expr, false)
}

// lowerErrDeferStmt records an error-path cleanup into the shared cleanup stack.
func (l *lowerer) lowerErrDeferStmt(stmt *ast.ErrDeferStmt) error {
	return l.recordCleanup(stmt.Expr, true)
}

// recordCleanup resolves a cleanup expression and registers it in the current
// frame. onError marks errdefer entries that run only on error-return paths.
func (l *lowerer) recordCleanup(expr ast.Expression, onError bool) error {
	cleanup, err := l.cleanupFromExpr(expr)
	if err != nil {
		return err
	}
	cleanup.OnError = onError
	frame := len(l.deferFrames) - 1
	l.deferFrames[frame] = append(l.deferFrames[frame], cleanup)
	return nil
}

// cleanupFromExpr converts the narrow v0 defer expression into a void cleanup.
func (l *lowerer) cleanupFromExpr(expr ast.Expression) (Cleanup, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return Cleanup{}, fmt.Errorf("ir error: defer expects cleanup method call")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace {
		return Cleanup{}, fmt.Errorf("ir error: defer expects cleanup method call")
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: defer cleanup receiver must be a local")
	}
	receiver, ok := l.env.get(ident.Name)
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: undefined defer receiver `%s`", ident.Name)
	}
	return l.cleanupFromMethod(receiver, field.Name)
}

// cleanupFromMethod resolves one receiver.method() cleanup into an IR instruction.
func (l *lowerer) cleanupFromMethod(receiver Value, method string) (Cleanup, error) {
	receiverType := derefType(receiver.Type)
	if _, ok := arrayElementType(receiverType); ok && method == "deinit" {
		return Cleanup{Op: "array.deinit", Args: []Value{receiver}}, nil
	}
	if _, ok := mapValueType(receiverType); ok && method == "deinit" {
		return Cleanup{Op: "map.deinit", Args: []Value{receiver}}, nil
	}
	if methodName, ok := l.implMethodCalleeName(receiver.Type, method); ok {
		sig := l.signatures[methodName]
		if sig.Return != "void" {
			return Cleanup{}, fmt.Errorf(
				"ir error: defer cleanup must return void, got %s",
				sig.Return,
			)
		}
		return Cleanup{Op: "call." + methodName, Args: []Value{receiver}}, nil
	}
	if method == "deinit" && arenaElementType(receiverType) != "unknown" {
		return Cleanup{Op: "arena.deinit", Args: []Value{receiver}}, nil
	}
	return Cleanup{}, fmt.Errorf("ir error: unknown cleanup method `%s`", method)
}

// errorCleanups returns all active cleanups that run on an error-return path,
// including both defer and errdefer entries, in execution order.
func (l *lowerer) errorCleanups() []Cleanup {
	return l.cleanupsFrom(0, true)
}

// normalCleanups returns active defer cleanups for a success exit. errdefer
// entries are skipped because they do not run when the block exits normally.
func (l *lowerer) normalCleanups() []Cleanup {
	return l.cleanupsFrom(0, false)
}

// cleanupsFrom returns cleanups from the requested frame depth inward. When
// includeError is false, errdefer (error-path) entries are skipped.
func (l *lowerer) cleanupsFrom(depth int, includeError bool) []Cleanup {
	cleanups := []Cleanup{}
	for frame := len(l.deferFrames) - 1; frame >= depth; frame-- {
		for index := len(l.deferFrames[frame]) - 1; index >= 0; index-- {
			cleanup := l.deferFrames[frame][index]
			if cleanup.OnError && !includeError {
				continue
			}
			cleanups = append(cleanups, cleanup)
		}
	}
	return cleanups
}

// emitNormalCleanups emits success-path cleanups before a normal function exit.
func (l *lowerer) emitNormalCleanups() {
	l.emitCleanups(l.normalCleanups())
}

// emitErrorCleanups emits error-path cleanups before an error-return exit.
func (l *lowerer) emitErrorCleanups() {
	l.emitCleanups(l.errorCleanups())
}

// emitCleanupFrame emits normal fallthrough cleanups for one lexical block.
// errdefer entries are skipped because the block exited without an error.
func (l *lowerer) emitCleanupFrame(frame int) {
	cleanups := []Cleanup{}
	for index := len(l.deferFrames[frame]) - 1; index >= 0; index-- {
		cleanup := l.deferFrames[frame][index]
		if cleanup.OnError {
			continue
		}
		cleanups = append(cleanups, cleanup)
	}
	l.emitCleanups(cleanups)
}

// emitCleanups appends cleanup instructions to the current block.
func (l *lowerer) emitCleanups(cleanups []Cleanup) {
	for _, cleanup := range cleanups {
		l.emit(cleanup.Op, "void", cleanup.Args, "")
	}
}
