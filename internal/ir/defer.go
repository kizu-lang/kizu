package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
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
	cleanup.Receiver = cleanupReceiverName(expr)
	frame := len(l.deferFrames) - 1
	l.deferFrames[frame] = append(l.deferFrames[frame], cleanup)
	return nil
}

// cleanupReceiverName reads the local a cleanup call consumes. The shape is the
// one cleanupFromExpr accepts, so anything else has already failed there.
func cleanupReceiverName(expr ast.Expression) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok {
		return ""
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return ""
	}
	return ident.Name
}

// cleanupFromExpr converts a defer expression into a void cleanup.
//
// A cleanup may carry arguments beyond its receiver. `Box.deinit` takes the
// allocator that handed out the cell, because the cell does not store one:
// what a release needs and what the value spells are separate questions, and
// keeping a copy of the answer in every cell is what a `deinit(allocator)`
// avoids. The arguments are read where the defer is written, not where it
// runs, so what runs at scope exit is settled at the point the source names.
func (l *lowerer) cleanupFromExpr(expr ast.Expression) (Cleanup, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
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
	rest := make([]Value, 0, len(call.Args))
	for _, arg := range call.Args {
		value, err := l.lowerExpr(arg)
		if err != nil {
			return Cleanup{}, err
		}
		rest = append(rest, value)
	}
	return l.cleanupFromMethod(receiver, field.Name, rest)
}

// containerCleanup names what one std container's cleanup needs: the std type
// its wrapper is declared on, the runtime op a shallow cleanup lowers to, and
// the type argument that decides which of the two runs.
type containerCleanup struct {
	name      string
	shallowOp string
	typeArg   string
	// shallowNamesAllocator is whether the runtime op takes the allocator the
	// release names. A Box cell stores nothing but its payload and an Array
	// header -- an Arena's too -- is `{data, len, cap}`, so their ops are
	// handed one; a Map still keeps an allocator in the heap header its op
	// reads.
	shallowNamesAllocator bool
}

// stdContainerCleanup reports the cleanup shape of a std container type, and
// false for anything else.
func stdContainerCleanup(receiverType string) (containerCleanup, bool) {
	if elem, ok := arrayElementType(receiverType); ok {
		return containerCleanup{arrayTypeName, "array.deinit", elem, true}, true
	}
	if args, ok := mapStaticArgs(receiverType); ok {
		return containerCleanup{mapTypeName, "map.deinit", args, false}, true
	}
	if elem, ok := boxElementType(receiverType); ok {
		return containerCleanup{boxTypeName, "box.deinit", elem, true}, true
	}
	if elem := arenaElementType(receiverType); elem != "unknown" {
		return containerCleanup{arenaTypeName, "arena.deinit", elem, true}, true
	}
	return containerCleanup{}, false
}

// cleanupFromMethod resolves one receiver.method() cleanup into an IR instruction.
func (l *lowerer) cleanupFromMethod(receiver Value, method string, rest []Value) (Cleanup, error) {
	receiverType := derefType(receiver.Type)
	if container, ok := stdContainerCleanup(receiverType); ok && method == typ.CleanupMethod {
		// A container whose contents own something releases them first, and
		// that loop lives in the std wrapper rather than in a runtime op: the
		// cleanup calls its instance exactly like a direct call would. Plain
		// contents have no loop to run, so they keep the runtime op.
		if !ast.OwnerType(l.deinitOwners, container.typeArg) {
			args := []Value{receiver}
			if container.shallowNamesAllocator {
				args = append(args, rest...)
			}
			return Cleanup{Op: container.shallowOp, Args: args}, nil
		}
		op, _, err := l.stdContainerCallOp(container.name, method, container.typeArg)
		if err != nil {
			return Cleanup{}, err
		}
		return Cleanup{Op: op, Args: append([]Value{receiver}, rest...)}, nil
	}
	if methodName, ok := l.implMethodCalleeName(receiver.Type, method); ok {
		sig := l.signatures[methodName]
		if sig.Return != "void" {
			return Cleanup{}, fmt.Errorf(
				"ir error: defer cleanup must return void, got %s",
				sig.Return,
			)
		}
		return Cleanup{Op: "call." + methodName, Args: append([]Value{receiver}, rest...)}, nil
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

// emitErrorCleanups emits error-path cleanups before an error-return exit,
// minus the ones a move retired before it.
func (l *lowerer) emitErrorCleanups(retired []string) {
	l.emitCleanups(retireCleanups(l.errorCleanups(), retired))
}

// retireCleanups drops the errdefer cleanups whose receiver was moved before
// this error exit. The ownership checker decides which; lowering obeys, so the
// move rule has one implementation (ADR-0114).
func retireCleanups(cleanups []Cleanup, retired []string) []Cleanup {
	if len(retired) == 0 {
		return cleanups
	}
	moved := make(map[string]bool, len(retired))
	for _, name := range retired {
		moved[name] = true
	}
	kept := make([]Cleanup, 0, len(cleanups))
	for _, cleanup := range cleanups {
		if cleanup.OnError && cleanup.Receiver != "" && moved[cleanup.Receiver] {
			continue
		}
		kept = append(kept, cleanup)
	}
	return kept
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

// emitCleanups appends cleanup instructions to the current block. A cleanup
// receiver that lives in a slot arrives as `&var T` storage; every cleanup
// consumes the value, so the slot is loaded at the exit that runs it.
func (l *lowerer) emitCleanups(cleanups []Cleanup) {
	for _, cleanup := range cleanups {
		l.emit(cleanup.Op, "void", l.loadCleanupArgs(cleanup.Args), "")
	}
}

// loadCleanupArgs loads slot-backed cleanup receivers into values. Args without
// a slot are returned as they are, unallocated.
func (l *lowerer) loadCleanupArgs(args []Value) []Value {
	needsLoad := false
	for _, arg := range args {
		if isMutableReferenceType(arg.Type) {
			needsLoad = true
			break
		}
	}
	if !needsLoad {
		return args
	}
	out := make([]Value, len(args))
	for index, arg := range args {
		if isMutableReferenceType(arg.Type) {
			arg = l.emit("ref.load", derefType(arg.Type), []Value{arg}, "")
		}
		out[index] = arg
	}
	return out
}
