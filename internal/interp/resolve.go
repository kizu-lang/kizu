package interp

import "github.com/kizu-lang/kizu/internal/ast"

// resolveProgram precomputes lexical addresses for identifier references so
// evaluation can read locals by slot instead of scanning scopes by name.
//
// Resolution is deliberately conservative. A reference is annotated only when
// its name is unambiguous across the enclosing scope stack, and evaluation
// still verifies the binding name before trusting the slot. As a result, a
// mismodeled scope rule or an unhandled binding form degrades to the slow
// name-lookup path rather than ever reading the wrong binding.
func (i *Interpreter) resolveProgram(program *ast.Program) {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			i.resolveFunction(d)
		case *ast.ImplDecl:
			for _, method := range d.Methods {
				i.resolveFunction(method)
			}
		}
	}
}

// resolveScope lists the binding names declared in one runtime environment in
// definition order, so a name's position matches its runtime cell slot.
type resolveScope struct {
	names []string
}

// pushScope returns a new scope stack with sc appended, never aliasing the
// caller's backing array so sibling branches stay independent.
func pushScope(scopes []*resolveScope, sc *resolveScope) []*resolveScope {
	out := make([]*resolveScope, len(scopes)+1)
	copy(out, scopes)
	out[len(scopes)] = sc
	return out
}

// resolveFunction walks a function or method body. Params (the receiver is
// Params[0] for methods) occupy the frame's first slots, matching the order
// callFunctionExpr binds them.
func (i *Interpreter) resolveFunction(fn *ast.FunctionDecl) {
	if fn.Body == nil {
		return
	}
	frame := &resolveScope{}
	for _, param := range fn.Params {
		frame.names = append(frame.names, param.Name)
	}
	i.resolveBlockInline(fn.Body, []*resolveScope{frame})
	i.compileFunction(fn)
}

// resolveBlockInline resolves statements that run in the current (top) scope,
// mirroring evalBlock: direct locals append to that scope.
func (i *Interpreter) resolveBlockInline(block *ast.BlockStmt, scopes []*resolveScope) {
	for _, stmt := range block.Statements {
		i.resolveStmt(stmt, scopes)
	}
}

// resolveScopedBlock mirrors evalScopedBlock: a child scope appears only when
// the block declares direct locals (the same predicate the evaluator uses).
func (i *Interpreter) resolveScopedBlock(block *ast.BlockStmt, scopes []*resolveScope) {
	if !i.blockNeedsScope(block) {
		i.resolveBlockInline(block, scopes)
		return
	}
	i.resolveBlockInline(block, pushScope(scopes, &resolveScope{}))
}

// resolveStmt advances scope slots for declarations and resolves the
// identifiers a statement reads, mirroring how evalStmt creates scopes.
func (i *Interpreter) resolveStmt(stmt ast.Statement, scopes []*resolveScope) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		i.resolveExpr(s.Value, scopes)
		top := scopes[len(scopes)-1]
		top.names = append(top.names, s.Name)
	case *ast.AssignStmt:
		i.resolveExpr(s.Target, scopes)
		i.resolveExpr(s.Value, scopes)
	case *ast.ReturnStmt:
		if s.Value != nil {
			i.resolveExpr(s.Value, scopes)
		}
	case *ast.ExprStmt:
		i.resolveExpr(s.Expr, scopes)
	case *ast.DeferStmt:
		i.resolveExpr(s.Expr, scopes)
	case *ast.ErrDeferStmt:
		i.resolveExpr(s.Expr, scopes)
	case *ast.IfStmt:
		i.resolveExpr(s.Condition, scopes)
		i.resolveScopedBlock(s.Consequence, scopes)
		if s.Alternative != nil {
			i.resolveScopedBlock(s.Alternative, scopes)
		}
	case *ast.WhileStmt:
		i.resolveExpr(s.Condition, scopes)
		i.resolveScopedBlock(s.Body, scopes)
	case *ast.ForStmt:
		i.resolveExpr(s.Start, scopes)
		i.resolveExpr(s.End, scopes)
		child := &resolveScope{names: []string{s.Name}}
		i.resolveBlockInline(s.Body, pushScope(scopes, child))
	case *ast.UnsafeStmt:
		i.resolveScopedBlock(s.Body, scopes)
	case *ast.MatchStmt:
		// Match arms scope their payload binding dynamically; resolve only the
		// scrutinee and leave arm-body identifiers to the slow path.
		i.resolveExpr(s.Value, scopes)
	}
}

// resolveExpr walks an expression and annotates the identifier references it
// can resolve. Unhandled expression shapes simply leave nested identifiers on
// the slow path.
func (i *Interpreter) resolveExpr(expr ast.Expression, scopes []*resolveScope) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		resolveIdent(e, scopes)
	case *ast.BinaryExpr:
		i.resolveExpr(e.Left, scopes)
		i.resolveExpr(e.Right, scopes)
	case *ast.PrefixExpr:
		i.resolveExpr(e.Right, scopes)
	case *ast.CallExpr:
		i.resolveExpr(e.Callee, scopes)
		for _, arg := range e.Args {
			i.resolveExpr(arg, scopes)
		}
	case *ast.IndexExpr:
		i.resolveExpr(e.Target, scopes)
		if e.Index != nil {
			i.resolveExpr(e.Index, scopes)
		}
		if e.Start != nil {
			i.resolveExpr(e.Start, scopes)
		}
		if e.End != nil {
			i.resolveExpr(e.End, scopes)
		}
	case *ast.FieldExpr:
		i.resolveExpr(e.Receiver, scopes)
	case *ast.CastExpr:
		i.resolveExpr(e.Value, scopes)
	case *ast.TryExpr:
		i.resolveExpr(e.Value, scopes)
	case *ast.StructLiteralExpr:
		for _, field := range e.Fields {
			i.resolveExpr(field.Value, scopes)
		}
	}
}

// resolveIdent records a lexical address only when the name occurs in exactly
// one enclosing scope. Shadowed or absent names stay unresolved so the slow
// path resolves them, which keeps a mismodeled depth from ever aliasing a
// same-named outer binding.
func resolveIdent(e *ast.IdentExpr, scopes []*resolveScope) {
	depth, index, matches := 0, 0, 0
	for s := len(scopes) - 1; s >= 0; s-- {
		for idx, name := range scopes[s].names {
			if name == e.Name {
				if matches == 0 {
					depth = len(scopes) - 1 - s
					index = idx
				}
				matches++
				break
			}
		}
	}
	if matches == 1 {
		e.SlotDepth = depth
		e.SlotIndex = index
		e.SlotResolved = true
	}
}
