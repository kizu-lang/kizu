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
	cleanup, err := l.cleanupFromExpr(stmt.Expr)
	if err != nil {
		return err
	}
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
	receiver, ok := l.env[ident.Name]
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

// activeCleanups returns all currently active cleanups in execution order.
func (l *lowerer) activeCleanups() []Cleanup {
	return l.cleanupsFrom(0)
}

// cleanupsFrom returns cleanups from the requested frame depth inward.
func (l *lowerer) cleanupsFrom(depth int) []Cleanup {
	cleanups := []Cleanup{}
	for frame := len(l.deferFrames) - 1; frame >= depth; frame-- {
		for index := len(l.deferFrames[frame]) - 1; index >= 0; index-- {
			cleanups = append(cleanups, l.deferFrames[frame][index])
		}
	}
	return cleanups
}

// emitActiveCleanups emits all currently active cleanups before function exit.
func (l *lowerer) emitActiveCleanups() {
	l.emitCleanups(l.activeCleanups())
}

// emitCleanupFrame emits normal fallthrough cleanups for one lexical block.
func (l *lowerer) emitCleanupFrame(frame int) {
	cleanups := []Cleanup{}
	for index := len(l.deferFrames[frame]) - 1; index >= 0; index-- {
		cleanups = append(cleanups, l.deferFrames[frame][index])
	}
	l.emitCleanups(cleanups)
}

// emitCleanups appends cleanup instructions to the current block.
func (l *lowerer) emitCleanups(cleanups []Cleanup) {
	for _, cleanup := range cleanups {
		l.emit(cleanup.Op, "void", cleanup.Args, "")
	}
}
