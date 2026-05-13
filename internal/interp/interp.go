package interp

import (
	"fmt"
	"io"
	"strconv"

	"tiny-safe/internal/ast"
)

// Interpreter executes a parsed Kizu program.
type Interpreter struct {
	out       io.Writer
	functions map[string]*ast.FunctionDecl
}

// New creates an interpreter that writes builtin output to out.
func New(out io.Writer) *Interpreter {
	return &Interpreter{out: out, functions: map[string]*ast.FunctionDecl{}}
}

// Run registers top-level declarations and calls main.
func (i *Interpreter) Run(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		i.functions[fn.Name] = fn
	}
	_, err := i.callFunction("main", nil)
	return err
}

// callFunction invokes a declared function by name.
func (i *Interpreter) callFunction(name string, args []Value) (Value, error) {
	fn, ok := i.functions[name]
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: undefined function `%s`", name)
	}
	if len(args) != len(fn.Params) {
		return voidValue(), fmt.Errorf("runtime error: `%s` expected %d args", name, len(fn.Params))
	}
	env := NewEnv()
	for idx, param := range fn.Params {
		if err := env.Define(param.Name, args[idx], false); err != nil {
			return voidValue(), err
		}
	}
	result, returned, err := i.evalBlock(fn.Body, env)
	if err != nil || returned {
		return result, err
	}
	return voidValue(), nil
}

// evalBlock executes statements in a lexical block.
func (i *Interpreter) evalBlock(block *ast.BlockStmt, env *Env) (Value, bool, error) {
	for _, stmt := range block.Statements {
		result, returned, err := i.evalStmt(stmt, env)
		if err != nil || returned {
			return result, returned, err
		}
	}
	return voidValue(), false, nil
}

// evalStmt executes one statement and reports explicit return flow.
func (i *Interpreter) evalStmt(stmt ast.Statement, env *Env) (Value, bool, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return i.evalLetStmt(s, env)
	case *ast.AssignStmt:
		return i.evalAssignStmt(s, env)
	case *ast.ReturnStmt:
		value, err := i.evalExpr(s.Value, env)
		return value, true, err
	case *ast.ExprStmt:
		value, err := i.evalExpr(s.Expr, env)
		return value, false, err
	case *ast.IfStmt:
		return i.evalIfStmt(s, env)
	case *ast.WhileStmt:
		return i.evalWhileStmt(s, env)
	default:
		return voidValue(), false, fmt.Errorf("runtime error: unsupported statement %T", stmt)
	}
}

// evalLetStmt executes a let or var declaration.
func (i *Interpreter) evalLetStmt(stmt *ast.LetStmt, env *Env) (Value, bool, error) {
	value, err := i.evalExpr(stmt.Value, env)
	if err != nil {
		return voidValue(), false, err
	}
	return voidValue(), false, env.Define(stmt.Name, value, stmt.Mutable)
}

// evalAssignStmt executes assignment to a mutable binding.
func (i *Interpreter) evalAssignStmt(stmt *ast.AssignStmt, env *Env) (Value, bool, error) {
	value, err := i.evalExpr(stmt.Value, env)
	if err != nil {
		return voidValue(), false, err
	}
	return voidValue(), false, env.Assign(stmt.Name, value)
}

// evalIfStmt executes a branch after checking the condition is boolean.
func (i *Interpreter) evalIfStmt(stmt *ast.IfStmt, env *Env) (Value, bool, error) {
	cond, err := i.evalExpr(stmt.Condition, env)
	if err != nil {
		return voidValue(), false, err
	}
	if cond.kind != kindBool {
		return voidValue(), false, fmt.Errorf("runtime error: if condition must be bool")
	}
	if cond.b {
		return i.evalBlock(stmt.Consequence, env.Child())
	}
	if stmt.Alternative != nil {
		return i.evalBlock(stmt.Alternative, env.Child())
	}
	return voidValue(), false, nil
}

// evalWhileStmt executes a loop while its condition remains true.
func (i *Interpreter) evalWhileStmt(stmt *ast.WhileStmt, env *Env) (Value, bool, error) {
	for {
		cond, err := i.evalExpr(stmt.Condition, env)
		if err != nil {
			return voidValue(), false, err
		}
		if cond.kind != kindBool {
			return voidValue(), false, fmt.Errorf("runtime error: while condition must be bool")
		}
		if !cond.b {
			return voidValue(), false, nil
		}
		result, returned, err := i.evalBlock(stmt.Body, env.Child())
		if err != nil || returned {
			return result, returned, err
		}
	}
}

// evalExpr evaluates an expression to a runtime value.
func (i *Interpreter) evalExpr(expr ast.Expression, env *Env) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return parseInt(e.Value)
	case *ast.StringExpr:
		return stringValue(e.Value), nil
	case *ast.BoolExpr:
		return boolValue(e.Value), nil
	case *ast.IdentExpr:
		return evalIdent(e.Name, env)
	case *ast.PrefixExpr:
		return i.evalPrefixExpr(e, env)
	case *ast.BinaryExpr:
		return i.evalBinaryExpr(e, env)
	case *ast.CallExpr:
		return i.evalCallExpr(e, env)
	default:
		return voidValue(), fmt.Errorf("runtime error: unsupported expression %T", expr)
	}
}

// parseInt converts an integer literal into a runtime value.
func parseInt(lit string) (Value, error) {
	v, err := strconv.ParseInt(lit, 10, 64)
	if err != nil {
		return voidValue(), fmt.Errorf("runtime error: invalid integer `%s`", lit)
	}
	return intValue(v), nil
}

// evalIdent resolves a name from the current environment.
func evalIdent(name string, env *Env) (Value, error) {
	value, ok := env.Get(name)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: undefined binding `%s`", name)
	}
	return value, nil
}

// evalPrefixExpr evaluates supported unary operators.
func (i *Interpreter) evalPrefixExpr(expr *ast.PrefixExpr, env *Env) (Value, error) {
	right, err := i.evalExpr(expr.Right, env)
	if err != nil {
		return voidValue(), err
	}
	switch expr.Operator {
	case "-":
		if right.kind != kindInt {
			return voidValue(), fmt.Errorf("runtime error: unary - expects int")
		}
		return intValue(-right.i), nil
	case "!":
		if right.kind != kindBool {
			return voidValue(), fmt.Errorf("runtime error: unary ! expects bool")
		}
		return boolValue(!right.b), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: unsupported unary `%s`", expr.Operator)
	}
}

// evalBinaryExpr evaluates arithmetic, equality, and comparison operators.
func (i *Interpreter) evalBinaryExpr(expr *ast.BinaryExpr, env *Env) (Value, error) {
	left, err := i.evalExpr(expr.Left, env)
	if err != nil {
		return voidValue(), err
	}
	right, err := i.evalExpr(expr.Right, env)
	if err != nil {
		return voidValue(), err
	}
	if expr.Operator == "==" || expr.Operator == "!=" {
		return evalEquality(expr.Operator, left, right)
	}
	if left.kind != kindInt || right.kind != kindInt {
		return voidValue(), fmt.Errorf("runtime error: operator `%s` expects ints", expr.Operator)
	}
	return evalIntBinary(expr.Operator, left.i, right.i)
}

// evalEquality evaluates equality operators for values of the same kind.
func evalEquality(op string, left Value, right Value) (Value, error) {
	if left.kind != right.kind {
		return voidValue(), fmt.Errorf("runtime error: equality operands must have same type")
	}
	equal := left == right
	if op == "!=" {
		equal = !equal
	}
	return boolValue(equal), nil
}

// evalIntBinary evaluates integer arithmetic and comparison operators.
func evalIntBinary(op string, left int64, right int64) (Value, error) {
	switch op {
	case "+":
		return intValue(left + right), nil
	case "-":
		return intValue(left - right), nil
	case "*":
		return intValue(left * right), nil
	case "/":
		return evalDivision(left, right)
	case "%":
		return evalModulo(left, right)
	case "<":
		return boolValue(left < right), nil
	case "<=":
		return boolValue(left <= right), nil
	case ">":
		return boolValue(left > right), nil
	case ">=":
		return boolValue(left >= right), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: unsupported operator `%s`", op)
	}
}

// evalDivision evaluates checked integer division.
func evalDivision(left int64, right int64) (Value, error) {
	if right == 0 {
		return voidValue(), fmt.Errorf("runtime error: division by zero")
	}
	return intValue(left / right), nil
}

// evalModulo evaluates checked integer remainder.
func evalModulo(left int64, right int64) (Value, error) {
	if right == 0 {
		return voidValue(), fmt.Errorf("runtime error: modulo by zero")
	}
	return intValue(left % right), nil
}

// evalCallExpr evaluates builtin and user-defined function calls.
func (i *Interpreter) evalCallExpr(expr *ast.CallExpr, env *Env) (Value, error) {
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: callee must be a function name")
	}
	args, err := i.evalArgs(expr.Args, env)
	if err != nil {
		return voidValue(), err
	}
	if name.Name == "print" {
		return i.callPrint(args)
	}
	return i.callFunction(name.Name, args)
}

// evalArgs evaluates call arguments from left to right.
func (i *Interpreter) evalArgs(exprs []ast.Expression, env *Env) ([]Value, error) {
	args := make([]Value, 0, len(exprs))
	for _, expr := range exprs {
		value, err := i.evalExpr(expr, env)
		if err != nil {
			return nil, err
		}
		args = append(args, value)
	}
	return args, nil
}

// callPrint writes one value followed by a newline.
func (i *Interpreter) callPrint(args []Value) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: print expected 1 arg")
	}
	if _, err := fmt.Fprintln(i.out, args[0].String()); err != nil {
		return voidValue(), err
	}
	return voidValue(), nil
}
