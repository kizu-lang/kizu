package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdprim"
)

// A local is an SSA value, which is enough until something writes through a
// borrow of it: the callee stores into the borrowed storage, and the caller has
// to read the store back. An SSA value has no storage to write into, so a local
// that is passed as `&var` gets a slot and lives in memory for the whole
// function. A `&var` parameter arrives as such storage already, so it joins the
// same set: either way the name means storage — reads load, assignments store,
// and the places that need the address take the pointer itself.
//
// The decision is made once, before the body is lowered, because it has to hold
// everywhere the name is used. Deciding it where the address is first needed
// would put the slot inside whatever `if` or loop asked for it, and the name
// would mean a value on one path and storage on another.

// mutablyBorrowedLocals returns the local names fn passes to a `&var` parameter.
// Parameters are left in the set: one that arrives as caller storage already
// has storage, which lowerFunctionNamed hands it, and one that arrives as a
// value is the callee's own copy, which can live in a slot like any local.
func (l *lowerer) mutablyBorrowedLocals(fn *ast.FunctionDecl) (map[string]bool, error) {
	found := map[string]bool{}
	if err := l.collectMutBorrowsStmt(fn.Body, found); err != nil {
		return nil, err
	}
	return found, nil
}

// collectMutBorrowsStmt walks one statement for names passed as `&var`.
func (l *lowerer) collectMutBorrowsStmt(stmt ast.Statement, found map[string]bool) error {
	if stmt == nil {
		return nil
	}
	exprs, stmts, known := statementChildren(stmt)
	if !known {
		return fmt.Errorf("ir error: slot analysis does not know statement `%T`", stmt)
	}
	for _, expr := range exprs {
		if err := l.collectMutBorrowsExpr(expr, found); err != nil {
			return err
		}
	}
	for _, inner := range stmts {
		if err := l.collectMutBorrowsStmt(inner, found); err != nil {
			return err
		}
	}
	return nil
}

// statementChildren returns the expressions and statements written inside stmt,
// and reports whether the node is one this walk knows. An unknown node is an
// error rather than an empty answer: a name missed here is a local that keeps
// its value while the callee writes into storage nobody reads.
func statementChildren(stmt ast.Statement) ([]ast.Expression, []ast.Statement, bool) {
	if exprs, ok := flatStatementExprs(stmt); ok {
		return exprs, nil, true
	}
	return nestingStatementChildren(stmt)
}

// flatStatementExprs returns the expressions of a statement that holds no other
// statement, and reports whether stmt is one of those.
func flatStatementExprs(stmt ast.Statement) ([]ast.Expression, bool) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return []ast.Expression{s.Value}, true
	case *ast.AssignStmt:
		return []ast.Expression{s.Target, s.Value}, true
	case *ast.ReturnStmt:
		return []ast.Expression{s.Value}, true
	case *ast.DeferStmt:
		return []ast.Expression{s.Expr}, true
	case *ast.ErrDeferStmt:
		return []ast.Expression{s.Expr}, true
	case *ast.ExprStmt:
		return []ast.Expression{s.Expr}, true
	case *ast.BreakStmt, *ast.ContinueStmt:
		return nil, true
	default:
		return nil, false
	}
}

// nestingStatementChildren returns the children of a statement that holds other
// statements, and reports whether stmt is one this walk knows.
func nestingStatementChildren(stmt ast.Statement) ([]ast.Expression, []ast.Statement, bool) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		if s == nil {
			return nil, nil, true
		}
		return nil, s.Statements, true
	case *ast.IfStmt:
		return []ast.Expression{s.Condition}, blocks(s.Consequence, s.Alternative), true
	case *ast.ComptimeIfStmt:
		return []ast.Expression{s.Condition}, blocks(s.Consequence, s.Alternative), true
	case *ast.ComptimeForStmt:
		return nil, blocks(s.Body), true
	case *ast.ComptimeMatchStmt:
		return []ast.Expression{s.Value}, blocks(s.Body), true
	case *ast.WhileStmt:
		return []ast.Expression{s.Condition}, blocks(s.Body), true
	case *ast.ForStmt:
		return []ast.Expression{s.Start, s.End}, blocks(s.Body), true
	case *ast.MatchStmt:
		return []ast.Expression{s.Value}, matchArmBodies(s), true
	default:
		return nil, nil, false
	}
}

// blocks returns the blocks that are present. An absent block is a nil pointer
// in a field, and putting that in an interface makes a value that is not nil.
func blocks(list ...*ast.BlockStmt) []ast.Statement {
	out := make([]ast.Statement, 0, len(list))
	for _, block := range list {
		if block != nil {
			out = append(out, block)
		}
	}
	return out
}

// matchArmBodies returns the statement each arm of a match runs.
func matchArmBodies(stmt *ast.MatchStmt) []ast.Statement {
	bodies := make([]ast.Statement, 0, len(stmt.Arms))
	for _, arm := range stmt.Arms {
		bodies = append(bodies, arm.Body)
	}
	return bodies
}

// collectMutBorrowsExpr walks one expression for names passed as `&var`.
func (l *lowerer) collectMutBorrowsExpr(expr ast.Expression, found map[string]bool) error {
	if expr == nil {
		return nil
	}
	if l.ownership.FunctionPointerMutablyBorrows(expr) {
		markIfName(expr, found)
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		l.markLentArgs(e, found)
	case *ast.PrefixExpr:
		if e.Operator == "&var" {
			markIfName(e.Right, found)
		}
	case *ast.IfStmt, *ast.MatchStmt:
		// `if` and `match` are the two nodes that are both a statement and an
		// expression, so in value position they are still walked as statements.
		return l.collectMutBorrowsStmt(expr.(ast.Statement), found)
	}
	children, known := expressionChildren(expr)
	if !known {
		return fmt.Errorf("ir error: slot analysis does not know expression `%T`", expr)
	}
	for _, child := range children {
		if err := l.collectMutBorrowsExpr(child, found); err != nil {
			return err
		}
	}
	return nil
}

// expressionChildren returns the expressions written inside expr, and reports
// whether the node is one this walk knows.
func expressionChildren(expr ast.Expression) ([]ast.Expression, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr, *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.TypeExpr,
		*ast.BufferLiteralExpr, *ast.NullExpr:
		return nil, true
	case *ast.PrefixExpr:
		return []ast.Expression{e.Right}, true
	case *ast.BinaryExpr:
		return []ast.Expression{e.Left, e.Right}, true
	case *ast.TypeApplyExpr:
		return []ast.Expression{e.Callee}, true
	case *ast.CastExpr:
		return []ast.Expression{e.Value}, true
	case *ast.TryExpr:
		return []ast.Expression{e.Value}, true
	case *ast.UnsafeExpr:
		return []ast.Expression{e.Value}, true
	case *ast.MoveExpr:
		return []ast.Expression{e.Value}, true
	case *ast.ComptimeExpr:
		return []ast.Expression{e.Expr}, true
	case *ast.DerefExpr:
		return []ast.Expression{e.Receiver}, true
	case *ast.FieldExpr:
		return []ast.Expression{e.Receiver}, true
	case *ast.IndexExpr:
		return []ast.Expression{e.Target, e.Index, e.Start, e.End}, true
	case *ast.StructLiteralExpr:
		return structLiteralValues(e), true
	case *ast.CallExpr:
		return append([]ast.Expression{e.Callee}, e.Args...), true
	default:
		return guardExpressionChildren(expr)
	}
}

// guardExpressionChildren returns the walkable children of an orelse guard:
// the condition, and the value a return exit carries.
func guardExpressionChildren(expr ast.Expression) ([]ast.Expression, bool) {
	switch guard := expr.(type) {
	case *ast.OrelseGuardExpr:
		children := []ast.Expression{guard.Cond}
		if ret, ok := guard.Exit.(*ast.ReturnStmt); ok && ret.Value != nil {
			children = append(children, ret.Value)
		}
		return children, true
	case *ast.CatchGuardExpr:
		children := []ast.Expression{guard.Cond}
		if ret, ok := guard.Exit.(*ast.ReturnStmt); ok && ret.Value != nil {
			children = append(children, ret.Value)
		}
		return children, true
	}
	return nil, false
}

// structLiteralValues returns the initializer of each field in a literal.
func structLiteralValues(expr *ast.StructLiteralExpr) []ast.Expression {
	values := make([]ast.Expression, 0, len(expr.Fields))
	for _, field := range expr.Fields {
		values = append(values, field.Value)
	}
	return values
}

// markLentArgs records the names this call hands over as the caller's storage.
// It reads the answer lowerParam gave rather than deciding again from the type,
// so a parameter that starts being passed this way is marked here by having
// been passed this way.
func (l *lowerer) markLentArgs(expr *ast.CallExpr, found map[string]bool) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		l.markLentMethodArgs(field, expr.Args, found)
		return
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		l.markLentGenericArgs(typeApply, expr.Args, found)
		return
	}
	name, ok := l.functionCalleeName(expr.Callee)
	if !ok {
		return
	}
	sig, known := l.signatures[name]
	if !known {
		return
	}
	for index, arg := range expr.Args {
		if index >= len(sig.Params) {
			return
		}
		if sig.Params[index].Passing == PassCallerStorage {
			markIfName(arg, found)
		}
	}
}

// markLentMethodArgs records the names a method call may hand over as storage.
// The receiver's type is unknown before lowering, so the walk unions every
// method bearing the name: a position is marked when any method the call could
// resolve to receives it as the caller's storage. Marking a name that resolves
// elsewhere costs it its SSA form, never its meaning.
func (l *lowerer) markLentMethodArgs(
	field *ast.FieldExpr,
	args []ast.Expression,
	found map[string]bool,
) {
	for _, sig := range l.methodSigs[field.Name] {
		if len(sig.Params) > 0 && sig.Params[0].Passing == PassCallerStorage {
			markIfName(field.Receiver, found)
		}
		for index, arg := range args {
			if index+1 < len(sig.Params) && sig.Params[index+1].Passing == PassCallerStorage {
				markIfName(arg, found)
			}
		}
	}
}

// markLentGenericArgs records the names a generic call lends. Like the direct
// arms above it reads the answer lowerParam gave rather than deciding again
// from the declaration: declaredInstanceParams binds the type arguments
// without asking for the instance, so the passing is the one the call will
// use. A callee this cannot resolve lends nothing.
func (l *lowerer) markLentGenericArgs(
	typeApply *ast.TypeApplyExpr,
	args []ast.Expression,
	found map[string]bool,
) {
	name, ok := l.functionCalleeName(typeApply.Callee)
	if !ok {
		return
	}
	params, err := l.declaredInstanceParams(name, typeApply.TypeArg)
	if err != nil {
		params = l.primitiveInstanceParams(name, typeApply.TypeArg)
	}
	for index, arg := range args {
		if index < len(params) && params[index].Passing == PassCallerStorage {
			markIfName(arg, found)
		}
	}
}

// markIfName records the local whose storage expr would hand over: the name
// itself, or the root of a field path -- a `field.addr` projection needs the
// root owner in memory, so the root is the local that loses SSA form.
func markIfName(expr ast.Expression, found map[string]bool) {
	for {
		field, ok := expr.(*ast.FieldExpr)
		if !ok || field.Namespace {
			break
		}
		expr = field.Receiver
	}
	if ident, ok := expr.(*ast.IdentExpr); ok {
		found[ident.Name] = true
	}
}

// bindLocal binds a declaration. A local the function mutably borrows gets its
// storage here, once, so every later use of the name means the same place.
func (l *lowerer) bindLocal(name string, value Value) {
	if !l.slots[name] {
		l.env.set(name, value)
		return
	}
	l.env.set(name, l.emit("local.slot", "&var "+value.Type, []Value{value}, ""))
}

// bindCapture binds a capture the same way bindLocal binds a declaration: a
// name the function mutably borrows needs storage of its own, and a capture is
// a local like any other -- `if build() |parts|` then `parts.append(..)` writes
// through the header it was handed. A capture that already binds a borrow is
// an address, and one address is all it needs.
func (l *lowerer) bindCapture(name string, value Value) {
	if !l.slots[name] || isReferenceType(value.Type) {
		l.env.set(name, value)
		return
	}
	l.env.set(name, l.emit("local.slot", "&var "+value.Type, []Value{value}, ""))
}

// borrowTargetExpr unwraps explicit `&var x` call-site syntax to the name
// whose storage the callee is lent. Caller storage only ever comes from a
// `&var` parameter, so `&` needs no arm here.
func borrowTargetExpr(expr ast.Expression) ast.Expression {
	if prefix, ok := expr.(*ast.PrefixExpr); ok && prefix.Operator == "&var" {
		return prefix.Right
	}
	return expr
}

// isStorageParam reports whether name is a parameter whose storage is the
// borrow the source names. For such a name `n.*` dereferences the slot itself;
// a lent local's slot holds the binding's value instead, which `.*` first
// reads out.
func (l *lowerer) isStorageParam(name string) bool {
	for _, param := range l.current.Params {
		if param.Name == "%"+name {
			return param.Passing == PassCallerStorage
		}
	}
	return false
}

// slotPointer returns the storage behind a name, for the places that need the
// address rather than the value.
func (l *lowerer) slotPointer(expr ast.Expression) (Value, bool) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok || !l.slots[ident.Name] {
		return Value{}, false
	}
	value, bound := l.env.get(ident.Name)
	return value, bound
}

// lowerReceiverAddress lowers the receiver of a field write. A local with
// storage keeps its address, so the write lands in the local rather than in a
// value loaded out of it.
func (l *lowerer) lowerReceiverAddress(expr ast.Expression) (Value, error) {
	if slot, ok := l.slotPointer(expr); ok {
		return slot, nil
	}
	return l.lowerExpr(expr)
}

// lowerFieldStorage lowers `base.field` to the field's own storage: a
// `field.addr` projection out of the base's storage. A nested path chains one
// projection per hop. ok is false when the path is not rooted in a storage
// name -- a slot-backed local or a `&var` parameter -- which is the shape the
// checker admits for every `&var` position.
func (l *lowerer) lowerFieldStorage(expr ast.Expression) (Value, bool) {
	field, isField := expr.(*ast.FieldExpr)
	if !isField || field.Namespace {
		return Value{}, false
	}
	storage, ok := l.slotPointer(field.Receiver)
	if !ok {
		storage, ok = l.lowerFieldStorage(field.Receiver)
	}
	if !ok {
		return Value{}, false
	}
	fieldType := l.fieldType(derefType(storage.Type), field.Name)
	if fieldType == "unknown" {
		return Value{}, false
	}
	return l.emit("field.addr."+field.Name, "&var "+fieldType, []Value{storage}, ""), true
}

// lowerCallArgs lowers the arguments of a call to name.
func (l *lowerer) lowerCallArgs(name string, args []ast.Expression) ([]Value, error) {
	if sig, ok := l.signatures[name]; ok {
		return l.lowerCallArgsAs(sig.Params, args)
	}
	return l.lowerCallArgsAs(corePrimitiveParams(name), args)
}

// corePrimitiveParams names the arguments of a std::internal::builtin the
// lowerer has no declared signature for. Only the destinations matter: a
// primitive that appends into the caller's String receives that String's
// storage, so the growth the runtime does is visible to the caller. The other
// argument kinds carry their own types to the call already.
func corePrimitiveParams(name string) []Param {
	sig, ok := stdprim.SimpleCoreSignatures[name]
	if !ok {
		return nil
	}
	params := make([]Param, len(sig.Args))
	for index, arg := range sig.Args {
		if arg == stdprim.ArgStringOut {
			params[index] = Param{Type: string(arg), Passing: PassCallerStorage}
		}
	}
	return params
}

// lowerCallArgsAs lowers call arguments at the types the callee declares for
// them, which is the one place a call decides what it hands over. An argument
// the callee receives as the caller's storage is passed as the local itself:
// its declared `&var T` type is a borrow context, and lowerContextualExpr
// hands over the storage. Params the lowerer cannot name -- a callee it has no
// signature for, or a variadic tail -- leave those arguments with the types
// they carry themselves.
func (l *lowerer) lowerCallArgsAs(params []Param, args []ast.Expression) ([]Value, error) {
	values := make([]Value, 0, len(args))
	for index, arg := range args {
		want := Param{}
		if index < len(params) {
			want = params[index]
		}
		value, err := l.lowerContextualExpr(arg, want.Type)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
