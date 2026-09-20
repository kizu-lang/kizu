package ir

import (
	"fmt"
	"strings"

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
	cleanup.Receiver = cleanupReceiver(expr)
	frame := len(l.deferFrames) - 1
	l.deferFrames[frame] = append(l.deferFrames[frame], cleanup)
	return nil
}

// cleanupReceiver reads the expression naming the one owner a deferred call
// consumes: the receiver of a method call, or the binding an argument
// marks `move`. The ownership checker names the same node when it retires
// an errdefer, which is how the two agree on which entry it was. A call
// that consumes nothing has none.
func cleanupReceiver(expr ast.Expression) ast.Expression {
	call, ok := deferredCall(expr)
	if !ok {
		return nil
	}
	if field, ok := call.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		return field.Receiver
	}
	for _, arg := range call.Args {
		if marker, ok := arg.(*ast.MoveExpr); ok {
			return marker.Value
		}
	}
	return nil
}

// deferredCall is the call a defer statement registers, under whatever
// markers it was written with.
func deferredCall(expr ast.Expression) (*ast.CallExpr, bool) {
	for {
		inner, ok := ast.MarkerValue(expr)
		if !ok {
			break
		}
		expr = inner
	}
	call, ok := expr.(*ast.CallExpr)
	return call, ok
}

// cleanupFromExpr converts a deferred call into a void cleanup: the callee
// resolved now, the arguments read now, the call emitted at each exit.
//
// The arguments are read where the defer is written, not where it runs, so
// what runs at scope exit is settled at the point the source names. The one
// exception is an owner the call consumes -- a by-value receiver, or an
// argument marked `move` -- which stays in its slot until the exit that
// runs the call, since the frame may still use it until then and the exit
// is what hands it over (loadCleanupArgs).
func (l *lowerer) cleanupFromExpr(expr ast.Expression) (Cleanup, error) {
	call, ok := deferredCall(expr)
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: defer expects a void call")
	}
	if field, ok := call.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		ident, ok := field.Receiver.(*ast.IdentExpr)
		if !ok {
			return Cleanup{}, fmt.Errorf("ir error: defer method receiver must be a local")
		}
		receiver, ok := l.env.get(ident.Name)
		if !ok {
			return Cleanup{}, fmt.Errorf("ir error: undefined defer receiver `%s`", ident.Name)
		}
		var params []Param
		if methodName, ok := l.implMethodCalleeName(receiver.Type, field.Name); ok {
			if sig, ok := l.signatures[methodName]; ok && len(sig.Params) > 0 {
				params = sig.Params[1:]
			}
		}
		args, err := l.lowerCleanupArgs(params, call.Args)
		if err != nil {
			return Cleanup{}, err
		}
		return l.cleanupFromMethod(receiver, field.Name, args)
	}
	name, ok := l.functionCalleeName(call.Callee)
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: unsupported defer callee `%s`", call.Callee.String())
	}
	sig, ok := l.signatures[name]
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: unsupported defer callee `%s`", name)
	}
	args, err := l.lowerCleanupArgs(sig.Params, call.Args)
	if err != nil {
		return Cleanup{}, err
	}
	return l.cleanupFromFunction(name, args)
}

// lowerCleanupArgs lowers a deferred call's arguments at the types the
// callee declares, the way a direct call does: a borrow parameter takes the
// caller's storage, a value parameter the value read now. A `move x`
// argument is the slot x lives in, loaded at the exit that runs the call.
func (l *lowerer) lowerCleanupArgs(params []Param, exprs []ast.Expression) ([]Value, error) {
	args := make([]Value, 0, len(exprs))
	for index, arg := range exprs {
		if marker, ok := arg.(*ast.MoveExpr); ok {
			if ident, ok := marker.Value.(*ast.IdentExpr); ok {
				if slot, ok := l.env.get(ident.Name); ok {
					args = append(args, slot)
					continue
				}
			}
		}
		want := Param{}
		if index < len(params) {
			want = params[index]
		}
		value, err := l.lowerContextualExpr(arg, want.Type)
		if err != nil {
			return nil, err
		}
		args = append(args, value)
	}
	return args, nil
}

// cleanupFromFunction resolves a deferred call to a declared function or an
// extern one. The signature says the call returns void; a std primitive
// with no declared signature is not a thing a defer can name.
func (l *lowerer) cleanupFromFunction(name string, args []Value) (Cleanup, error) {
	sig, ok := l.signatures[name]
	if !ok {
		return Cleanup{}, fmt.Errorf("ir error: unsupported defer callee `%s`", name)
	}
	if sig.Return != "void" {
		return Cleanup{}, fmt.Errorf("ir error: defer call must return void, got %s", sig.Return)
	}
	cleanup := Cleanup{Op: "call." + name, Args: args, Loads: ownerLoads(args, sig.Params)}
	if external, ok := l.externDecls[name]; ok {
		cleanup.Op = "call." + external.name
		cleanup.ExternABI = external.abi
		cleanup.ExternName = external.name
	}
	return cleanup, nil
}

// containerCleanup names what one std container's cleanup needs: the std type
// its wrapper is declared on, the runtime op a shallow cleanup lowers to, and
// the type argument that decides which of the two runs.
type containerCleanup struct {
	name      string
	shallowOp string
	typeArg   string
	// ownerArg is the type argument whose values the container holds and
	// would have to release: the element for an array, box, or arena, the
	// value for a map. typeArg spells the whole instantiation, which for a
	// map is `K, V` and names no type at all.
	ownerArg string
	// shallowNamesAllocator is whether the runtime op takes the allocator the
	// release names. Every container header is now the value, and none of
	// them keeps an allocator, so every op is handed one; the field is what
	// says a container's release reads it (ADR-0132).
	shallowNamesAllocator bool
}

// stdContainerCleanup reports the cleanup shape of a std container type, and
// false for anything else.
func stdContainerCleanup(receiverType string) (containerCleanup, bool) {
	if elem, ok := arrayElementType(receiverType); ok {
		return containerCleanup{arrayTypeName, "array.deinit", elem, elem, true}, true
	}
	if _, value, ok := mapTypeArgs(receiverType); ok {
		args, _ := mapStaticArgs(receiverType)
		return containerCleanup{mapTypeName, "map.deinit", args, value, true}, true
	}
	if elem, ok := boxElementType(receiverType); ok {
		return containerCleanup{boxTypeName, "box.deinit", elem, elem, true}, true
	}
	if elem := arenaElementType(receiverType); elem != "unknown" {
		return containerCleanup{arenaTypeName, "arena.deinit", elem, elem, true}, true
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
		if !ast.OwnerType(l.deinitOwners, container.ownerArg) {
			args := []Value{receiver}
			if container.shallowNamesAllocator {
				args = append(args, rest...)
			}
			return Cleanup{Op: container.shallowOp, Args: args, Loads: ownerLoads(args, nil)}, nil
		}
		op, _, err := l.stdContainerCallOp(container.name, method, container.typeArg)
		if err != nil {
			return Cleanup{}, err
		}
		args := append([]Value{receiver}, rest...)
		return Cleanup{Op: op, Args: args, Loads: ownerLoads(args, nil)}, nil
	}
	if methodName, ok := l.implMethodCalleeName(receiver.Type, method); ok {
		return l.cleanupFromFunction(methodName, append([]Value{receiver}, rest...))
	}
	return Cleanup{}, fmt.Errorf("ir error: unknown cleanup method `%s`", method)
}

// ownerLoads marks the arguments a cleanup loads at exit: a slot whose
// parameter takes the value itself. A slot handed to a `&` / `&var`
// parameter is what that parameter wants and is passed as it is. With no
// signature every slot is an owner, which is what a std container's runtime
// release takes.
func ownerLoads(args []Value, params []Param) []bool {
	loads := make([]bool, len(args))
	for index, arg := range args {
		if !isMutableReferenceType(arg.Type) {
			continue
		}
		if index < len(params) && strings.HasPrefix(params[index].Type, "&") {
			continue
		}
		loads[index] = true
	}
	return loads
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
func (l *lowerer) emitErrorCleanups(retired []ast.Expression) {
	l.emitCleanups(retireCleanups(l.errorCleanups(), retired))
}

// retireCleanups drops the errdefer cleanups whose receiver was moved before
// this error exit. The ownership checker decides which; lowering obeys, so the
// move rule has one implementation (ADR-0114).
func retireCleanups(cleanups []Cleanup, retired []ast.Expression) []Cleanup {
	if len(retired) == 0 {
		return cleanups
	}
	moved := make(map[ast.Expression]bool, len(retired))
	for _, receiver := range retired {
		moved[receiver] = true
	}
	kept := make([]Cleanup, 0, len(cleanups))
	for _, cleanup := range cleanups {
		if cleanup.OnError && cleanup.Receiver != nil && moved[cleanup.Receiver] {
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
		l.emit(cleanup.Op, "void", l.loadCleanupArgs(cleanup.Args, cleanup.Loads), "")
		if cleanup.ExternABI != "" {
			instr := l.block.Instrs[len(l.block.Instrs)-1]
			instr.ExternABI = cleanup.ExternABI
			instr.ExternName = cleanup.ExternName
		}
	}
}

// loadCleanupArgs loads the slot-backed owners a cleanup consumes into
// values, and passes every other argument as it is.
func (l *lowerer) loadCleanupArgs(args []Value, loads []bool) []Value {
	needsLoad := false
	for _, load := range loads {
		if load {
			needsLoad = true
			break
		}
	}
	if !needsLoad {
		return args
	}
	out := make([]Value, len(args))
	for index, arg := range args {
		if loads[index] {
			arg = l.emit("ref.load", derefType(arg.Type), []Value{arg}, "")
		}
		out[index] = arg
	}
	return out
}
