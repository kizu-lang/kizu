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
	enums     map[string]map[string]bool
}

type trySignal struct {
	value Value
}

// Error marks trySignal as an internal interpreter control signal.
func (s trySignal) Error() string {
	return "try signal"
}

// New creates an interpreter that writes builtin output to out.
func New(out io.Writer) *Interpreter {
	return &Interpreter{
		out:       out,
		functions: map[string]*ast.FunctionDecl{},
		enums:     map[string]map[string]bool{},
	}
}

// Run registers top-level declarations and calls main.
func (i *Interpreter) Run(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.EnumDecl:
			i.enums[d.Name] = enumTags(d.Tags)
		case *ast.FunctionDecl:
			if d.ExternABI != "" {
				continue
			}
			i.functions[d.Name] = d
		default:
			continue
		}
	}
	_, err := i.callFunction("main", nil)
	return err
}

// enumTags returns a lookup set for runtime enum tag validation.
func enumTags(tags []string) map[string]bool {
	out := map[string]bool{}
	for _, tag := range tags {
		out[tag] = true
	}
	return out
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
		if signal, ok := err.(trySignal); ok {
			return signal.value, true, nil
		}
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
	case *ast.MatchStmt:
		return i.evalMatchStmt(s, env)
	case *ast.UnsafeStmt:
		return i.evalBlock(s.Body, env.Child())
	case *ast.ComptimeIfStmt:
		return i.evalComptimeIfStmt(s, env)
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

// evalMatchStmt executes the matching enum tag arm.
func (i *Interpreter) evalMatchStmt(stmt *ast.MatchStmt, env *Env) (Value, bool, error) {
	value, err := i.evalExpr(stmt.Value, env)
	if err != nil {
		return voidValue(), false, err
	}
	if value.kind != kindEnum {
		return voidValue(), false, fmt.Errorf("runtime error: match expects enum")
	}
	for _, arm := range stmt.Arms {
		if arm.Tag == value.enum.tag {
			return i.evalStmt(arm.Body, env.Child())
		}
	}
	return voidValue(), false, fmt.Errorf("runtime error: no match arm for `%s`", value.String())
}

// evalComptimeIfStmt executes the branch selected by a compile-time condition.
func (i *Interpreter) evalComptimeIfStmt(stmt *ast.ComptimeIfStmt, env *Env) (Value, bool, error) {
	cond, err := i.evalExpr(stmt.Condition, env)
	if err != nil {
		return voidValue(), false, err
	}
	if cond.kind != kindBool {
		return voidValue(), false, fmt.Errorf("runtime error: comptime if condition must be bool")
	}
	if cond.b {
		return i.evalBlock(stmt.Consequence, env.Child())
	}
	if stmt.Alternative != nil {
		return i.evalBlock(stmt.Alternative, env.Child())
	}
	return voidValue(), false, nil
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
	case *ast.ComptimeExpr:
		return i.evalExpr(e.Expr, env)
	case *ast.IdentExpr:
		return evalIdent(e.Name, env)
	case *ast.PrefixExpr:
		return i.evalPrefixExpr(e, env)
	case *ast.BinaryExpr:
		return i.evalBinaryExpr(e, env)
	case *ast.CallExpr:
		return i.evalCallExpr(e, env)
	case *ast.CastExpr:
		return i.evalExpr(e.Value, env)
	case *ast.TryExpr:
		return i.evalTryExpr(e, env)
	case *ast.ArenaNewExpr:
		return arenaValue(), nil
	case *ast.StructLiteralExpr:
		return i.evalStructLiteralExpr(e, env)
	case *ast.FieldExpr:
		return i.evalFieldExpr(e, env)
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
	equal := valuesEqual(left, right)
	if op == "!=" {
		equal = !equal
	}
	return boolValue(equal), nil
}

// valuesEqual compares scalar values supported by equality operators.
func valuesEqual(left Value, right Value) bool {
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case kindVoid:
		return true
	case kindInt:
		return left.i == right.i
	case kindBool:
		return left.b == right.b
	case kindString:
		return left.s == right.s
	case kindHandle:
		return left.handle == right.handle
	case kindEnum:
		return left.enum == right.enum
	default:
		return false
	}
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
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return i.evalMethodCallExpr(field, expr.Args, env)
	}
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
	if name.Name == "ok" {
		return callOk(args)
	}
	if name.Name == "error" {
		return callError(args)
	}
	return i.callFunction(name.Name, args)
}

// evalTryExpr unwraps ok results or returns an error result from the current function.
func (i *Interpreter) evalTryExpr(expr *ast.TryExpr, env *Env) (Value, error) {
	value, err := i.evalExpr(expr.Value, env)
	if err != nil {
		return voidValue(), err
	}
	if value.kind != kindResult {
		return voidValue(), fmt.Errorf("runtime error: try expects result")
	}
	if value.result.ok {
		return value.result.value, nil
	}
	return voidValue(), trySignal{value: value}
}

// evalStructLiteralExpr evaluates each field initializer into a struct value.
func (i *Interpreter) evalStructLiteralExpr(expr *ast.StructLiteralExpr, env *Env) (Value, error) {
	fields := map[string]Value{}
	for _, field := range expr.Fields {
		value, err := i.evalExpr(field.Value, env)
		if err != nil {
			return voidValue(), err
		}
		fields[field.Name] = value
	}
	return structValue(fields), nil
}

// evalFieldExpr reads a field from a struct value.
func (i *Interpreter) evalFieldExpr(expr *ast.FieldExpr, env *Env) (Value, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if tags, exists := i.enums[ident.Name]; exists {
			if !tags[expr.Name] {
				return voidValue(), fmt.Errorf("runtime error: unknown enum tag `%s.%s`",
					ident.Name, expr.Name)
			}
			return enumValue(ident.Name, expr.Name), nil
		}
	}
	receiver, err := i.evalExpr(expr.Receiver, env)
	if err != nil {
		return voidValue(), err
	}
	if receiver.kind != kindStruct {
		return voidValue(), fmt.Errorf("runtime error: field access expects struct")
	}
	value, ok := receiver.fields[expr.Name]
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: unknown field `%s`", expr.Name)
	}
	return value, nil
}

// evalMethodCallExpr evaluates arena methods.
func (i *Interpreter) evalMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	receiver, err := i.evalExpr(field.Receiver, env)
	if err != nil {
		return voidValue(), err
	}
	if receiver.kind != kindArena {
		return voidValue(), fmt.Errorf("runtime error: method `%s` expects arena", field.Name)
	}
	switch field.Name {
	case "add":
		return i.evalArenaAdd(receiver.arena, args, env)
	case "get":
		return i.evalArenaGet(receiver.arena, args, env)
	default:
		return voidValue(), fmt.Errorf("runtime error: unknown arena method `%s`", field.Name)
	}
}

// evalArenaAdd appends one value and returns an opaque handle.
func (i *Interpreter) evalArenaAdd(arena *Arena, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: arena.add expected 1 arg")
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	arena.values = append(arena.values, value)
	return handleValue(arena, len(arena.values)-1), nil
}

// evalArenaGet resolves a handle back to its arena value.
func (i *Interpreter) evalArenaGet(arena *Arena, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: arena.get expected 1 arg")
	}
	handle, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if handle.kind != kindHandle || handle.handle.arena != arena {
		return voidValue(), fmt.Errorf("runtime error: handle does not belong to arena")
	}
	if handle.handle.index < 0 || handle.handle.index >= len(arena.values) {
		return voidValue(), fmt.Errorf("runtime error: invalid arena handle")
	}
	return arena.values[handle.handle.index], nil
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

// callOk constructs a successful result value.
func callOk(args []Value) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: ok expected 1 arg")
	}
	return resultOkValue(args[0]), nil
}

// callError constructs an error result value.
func callError(args []Value) (Value, error) {
	if len(args) != 1 || args[0].kind != kindString {
		return voidValue(), fmt.Errorf("runtime error: error expected string")
	}
	return resultErrorValue(args[0].s), nil
}
