package interp

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
)

// exprEval evaluates one precompiled expression. Compilation happens once, after
// resolution, so the hot loop dispatches through a captured closure instead of
// re-walking the AST and re-running the evalExpr type switch on every node.
type exprEval func(env *Env) (Value, error)

// stmtEval evaluates one precompiled statement, mirroring evalStmt's
// (value, returned, error) contract so compiled and interpreted statements
// compose identically.
type stmtEval func(env *Env) (Value, bool, error)

// compileFunction precompiles a function or method body. It runs from the
// resolver once slots are assigned, and is a no-op for bodyless externs.
func (i *Interpreter) compileFunction(fn *ast.FunctionDecl) {
	if fn.Body == nil {
		return
	}
	i.compiledBodies[fn] = i.compileBlock(fn.Body)
}

// runBody evaluates a function body through its compiled form, falling back to
// the tree-walker if the body was never compiled.
func (i *Interpreter) runBody(fn *ast.FunctionDecl, env *Env) (Value, bool, error) {
	if body := i.compiledBodies[fn]; body != nil {
		return body(env)
	}
	return i.evalBlock(fn.Body, env)
}

// compileBlock compiles a block's statements. Blocks that register defers are
// delegated wholesale to evalBlock so cleanup ordering stays exact; the common
// defer-free block runs its compiled statements inline.
func (i *Interpreter) compileBlock(block *ast.BlockStmt) stmtEval {
	for _, stmt := range block.Statements {
		if _, ok := stmt.(*ast.DeferStmt); ok {
			return func(env *Env) (Value, bool, error) { return i.evalBlock(block, env) }
		}
	}
	stmts := make([]stmtEval, len(block.Statements))
	for idx, stmt := range block.Statements {
		stmts[idx] = i.compileStmt(stmt)
	}
	return func(env *Env) (Value, bool, error) {
		result := voidValue()
		for _, stmt := range stmts {
			value, returned, err := stmt(env)
			if signal, ok := err.(trySignal); ok {
				return signal.value, true, nil
			}
			if err != nil || returned {
				return value, returned, err
			}
			result = value
		}
		return result, false, nil
	}
}

// compileScopedBlock mirrors evalScopedBlock: a child scope is created only when
// the block declares direct locals.
func (i *Interpreter) compileScopedBlock(block *ast.BlockStmt) stmtEval {
	body := i.compileBlock(block)
	if !i.blockNeedsScope(block) {
		return body
	}
	return func(env *Env) (Value, bool, error) {
		child := env.Child()
		value, returned, err := body(child)
		child.Release()
		return value, returned, err
	}
}

// compileStmt compiles the hot statement kinds and delegates the rest to
// evalStmt, keeping their exact semantics on the tree-walker.
func (i *Interpreter) compileStmt(stmt ast.Statement) stmtEval {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return i.compileLet(s)
	case *ast.ReturnStmt:
		return i.compileReturn(s)
	case *ast.ExprStmt:
		return i.compileExprStmt(s)
	case *ast.AssignStmt:
		return i.compileAssign(s)
	case *ast.IfStmt:
		return i.compileIf(s)
	case *ast.WhileStmt:
		return i.compileWhile(s)
	default:
		return func(env *Env) (Value, bool, error) { return i.evalStmt(stmt, env) }
	}
}

// compileLet compiles a let/var declaration. Borrow initializers keep their
// aliasing semantics on the tree-walker.
func (i *Interpreter) compileLet(s *ast.LetStmt) stmtEval {
	if _, ok := borrowPrefix(s.Value); ok {
		return func(env *Env) (Value, bool, error) { return i.evalLetStmt(s, env) }
	}
	value := i.compileExpr(s.Value)
	name, mutable := s.Name, s.Mutable
	return func(env *Env) (Value, bool, error) {
		v, err := value(env)
		if err != nil {
			return voidValue(), false, err
		}
		return voidValue(), false, env.Define(name, v, mutable)
	}
}

// compileReturn compiles a return statement.
func (i *Interpreter) compileReturn(s *ast.ReturnStmt) stmtEval {
	if s.Value == nil {
		return func(_ *Env) (Value, bool, error) { return voidValue(), true, nil }
	}
	value := i.compileExpr(s.Value)
	return func(env *Env) (Value, bool, error) {
		v, err := value(env)
		return v, true, err
	}
}

// compileExprStmt compiles an expression statement, dropping the value when the
// statement is terminated by a semicolon.
func (i *Interpreter) compileExprStmt(s *ast.ExprStmt) stmtEval {
	value := i.compileExpr(s.Expr)
	semicolon := s.Semicolon
	return func(env *Env) (Value, bool, error) {
		v, err := value(env)
		if semicolon {
			v = voidValue()
		}
		return v, false, err
	}
}

// compileAssign compiles an assignment, specializing the common bare-identifier
// target and delegating field/deref/call targets to assignTarget.
func (i *Interpreter) compileAssign(s *ast.AssignStmt) stmtEval {
	value := i.compileExpr(s.Value)
	if ident, ok := s.Target.(*ast.IdentExpr); ok {
		name := ident.Name
		return func(env *Env) (Value, bool, error) {
			v, err := value(env)
			if err != nil {
				return voidValue(), false, err
			}
			return voidValue(), false, env.Assign(name, v)
		}
	}
	target := s.Target
	return func(env *Env) (Value, bool, error) {
		v, err := value(env)
		if err != nil {
			return voidValue(), false, err
		}
		return voidValue(), false, i.assignTarget(target, v, env)
	}
}

// compileIf compiles a conditional, mirroring evalIfStmt.
func (i *Interpreter) compileIf(s *ast.IfStmt) stmtEval {
	cond := i.compileExpr(s.Condition)
	cons := i.compileScopedBlock(s.Consequence)
	var alt stmtEval
	if s.Alternative != nil {
		alt = i.compileScopedBlock(s.Alternative)
	}
	return func(env *Env) (Value, bool, error) {
		c, err := cond(env)
		if err != nil {
			return voidValue(), false, err
		}
		if c.kind != kindBool {
			return voidValue(), false, fmt.Errorf("runtime error: if condition must be bool")
		}
		if c.b {
			return cons(env)
		}
		if alt != nil {
			return alt(env)
		}
		return voidValue(), false, nil
	}
}

// compileWhile compiles a while loop, mirroring evalWhileStmt including loop
// signal handling.
func (i *Interpreter) compileWhile(s *ast.WhileStmt) stmtEval {
	cond := i.compileExpr(s.Condition)
	body := i.compileScopedBlock(s.Body)
	label := s.Label
	return func(env *Env) (Value, bool, error) {
		for {
			c, err := cond(env)
			if err != nil {
				return voidValue(), false, err
			}
			if c.kind != kindBool {
				return voidValue(), false, fmt.Errorf("runtime error: while condition must be bool")
			}
			if !c.b {
				return voidValue(), false, nil
			}
			result, returned, err := body(env)
			if signal, ok := err.(loopSignal); ok {
				if handledLoopSignal(signal, label) {
					if signal.kind == "continue" {
						continue
					}
					return voidValue(), false, nil
				}
			}
			if err != nil || returned {
				return result, returned, err
			}
		}
	}
}

// compileExpr compiles the hot expression kinds and delegates the rest to
// evalExpr. Delegation keeps every uncompiled shape on the proven tree-walker.
func (i *Interpreter) compileExpr(expr ast.Expression) exprEval {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		if v, err := evalLiteralExpr(e); err == nil {
			return func(_ *Env) (Value, error) { return v, nil }
		}
	case *ast.IdentExpr:
		return compileIdent(i, e)
	case *ast.BinaryExpr:
		return i.compileBinary(e)
	}
	return func(env *Env) (Value, error) { return i.evalExpr(expr, env) }
}

// compileIdent compiles an identifier read, using the resolver's slot when one
// was assigned and verifying the bound name before trusting it.
func compileIdent(i *Interpreter, e *ast.IdentExpr) exprEval {
	name := e.Name
	if e.SlotResolved {
		depth, index := e.SlotDepth, e.SlotIndex
		return func(env *Env) (Value, error) {
			if b := env.bindingAt(depth, index); b != nil && b.name == name {
				return b.value, nil
			}
			return i.evalIdent(name, env)
		}
	}
	return func(env *Env) (Value, error) {
		return i.evalIdent(name, env)
	}
}

// compileBinary compiles a binary expression, mirroring evalBinaryExpr by
// dispatching on the precomputed operator and evaluating compiled operands
// directly.
func (i *Interpreter) compileBinary(e *ast.BinaryExpr) exprEval {
	if e.Op == ast.BinaryOpAnd || e.Op == ast.BinaryOpOr {
		return i.compileLogical(e)
	}
	left := i.compileExpr(e.Left)
	right := i.compileExpr(e.Right)
	op := e.Op
	operator := e.Operator
	if op == ast.BinaryOpEq || op == ast.BinaryOpNe {
		return func(env *Env) (Value, error) {
			l, err := left(env)
			if err != nil {
				return voidValue(), err
			}
			r, err := right(env)
			if err != nil {
				return voidValue(), err
			}
			return evalEquality(op, l, r)
		}
	}
	return func(env *Env) (Value, error) {
		l, err := left(env)
		if err != nil {
			return voidValue(), err
		}
		r, err := right(env)
		if err != nil {
			return voidValue(), err
		}
		if l.kind != kindInt || r.kind != kindInt {
			return voidValue(), fmt.Errorf("runtime error: operator `%s` expects integers", operator)
		}
		return evalIntBinary(op, l.i, r.i)
	}
}

// compileLogical compiles short-circuit boolean operators, mirroring
// evalLogicalExpr.
func (i *Interpreter) compileLogical(e *ast.BinaryExpr) exprEval {
	left := i.compileExpr(e.Left)
	right := i.compileExpr(e.Right)
	op := e.Op
	operator := e.Operator
	return func(env *Env) (Value, error) {
		l, err := left(env)
		if err != nil {
			return voidValue(), err
		}
		if l.kind != kindBool {
			return voidValue(), fmt.Errorf("runtime error: operator `%s` expects bools", operator)
		}
		if op == ast.BinaryOpAnd && !l.b {
			return boolValue(false), nil
		}
		if op == ast.BinaryOpOr && l.b {
			return boolValue(true), nil
		}
		r, err := right(env)
		if err != nil {
			return voidValue(), err
		}
		if r.kind != kindBool {
			return voidValue(), fmt.Errorf("runtime error: operator `%s` expects bools", operator)
		}
		return boolValue(r.b), nil
	}
}
