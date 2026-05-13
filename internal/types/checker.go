package types

import (
	"fmt"

	"tiny-safe/internal/ast"
)

// Type is the static type name used by the v0 checker.
type Type string

const (
	typeBool   Type = "bool"
	typeInt    Type = "int"
	typeString Type = "string"
	typeVoid   Type = "void"
)

var knownTypes = map[Type]bool{
	typeBool:   true,
	typeInt:    true,
	typeString: true,
	typeVoid:   true,
	"i8":       true,
	"i16":      true,
	"i32":      true,
	"i64":      true,
	"u8":       true,
	"u16":      true,
	"u32":      true,
	"u64":      true,
	"usize":    true,
	"isize":    true,
	"f32":      true,
	"f64":      true,
}

var numericTypes = map[Type]bool{
	typeInt: true,
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"u8":    true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var signedNumericTypes = map[Type]bool{
	typeInt: true,
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var integerTypes = map[Type]bool{
	typeInt: true,
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"u8":    true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
}

// Checker validates type rules for a parsed program.
type Checker struct {
	functions map[string]*functionType
}

type functionType struct {
	name       string
	params     []Type
	returnType Type
	decl       *ast.FunctionDecl
}

type scope struct {
	parent *scope
	values map[string]Type
}

// New creates an empty type checker.
func New() *Checker {
	return &Checker{functions: map[string]*functionType{}}
}

// Check validates the program and returns the first type error.
func (c *Checker) Check(program *ast.Program) error {
	if err := c.collectFunctions(program); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		fnDecl, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if err := c.checkFunction(c.functions[fnDecl.Name]); err != nil {
			return err
		}
	}
	return nil
}

// collectFunctions registers top-level function signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			return fmt.Errorf("type error: unsupported declaration %T", decl)
		}
		if _, exists := c.functions[fn.Name]; exists {
			return fmt.Errorf("type error: duplicate function `%s`", fn.Name)
		}
		fnType, err := newFunctionType(fn)
		if err != nil {
			return err
		}
		c.functions[fn.Name] = fnType
	}
	return nil
}

// newFunctionType converts a parsed function declaration into its static type.
func newFunctionType(fn *ast.FunctionDecl) (*functionType, error) {
	params := make([]Type, 0, len(fn.Params))
	for _, param := range fn.Params {
		paramType, err := parseType(param.TypeName)
		if err != nil {
			return nil, err
		}
		if paramType == typeVoid {
			return nil, fmt.Errorf("type error: parameter `%s` cannot have type void", param.Name)
		}
		params = append(params, paramType)
	}
	ret := typeVoid
	if fn.ReturnType != "" {
		var err error
		ret, err = parseType(fn.ReturnType)
		if err != nil {
			return nil, err
		}
	}
	return &functionType{name: fn.Name, params: params, returnType: ret, decl: fn}, nil
}

// parseType validates a source-level type name.
func parseType(name string) (Type, error) {
	typ := Type(name)
	if !knownTypes[typ] {
		return "", fmt.Errorf("type error: unknown type `%s`", name)
	}
	return typ, nil
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		if err := env.define(param.Name, fn.params[idx]); err != nil {
			return err
		}
	}
	returns, err := c.checkBlock(fn.decl.Body, env, fn.returnType)
	if err != nil {
		return err
	}
	if fn.returnType != typeVoid && !returns {
		return fmt.Errorf("type error: function `%s` must return %s", fn.name, fn.returnType)
	}
	return nil
}

// checkBlock validates statements and reports whether the block always returns.
func (c *Checker) checkBlock(block *ast.BlockStmt, env *scope, wantReturn Type) (bool, error) {
	for _, stmt := range block.Statements {
		returns, err := c.checkStmt(stmt, env, wantReturn)
		if err != nil || returns {
			return returns, err
		}
	}
	return false, nil
}

// checkStmt validates a statement and reports explicit return flow.
func (c *Checker) checkStmt(stmt ast.Statement, env *scope, wantReturn Type) (bool, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		typ, err := c.checkExpr(s.Value, env)
		if err != nil {
			return false, err
		}
		return false, env.define(s.Name, typ)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env, wantReturn)
	case *ast.ExprStmt:
		_, err := c.checkExpr(s.Expr, env)
		return false, err
	case *ast.IfStmt:
		return c.checkIfStmt(s, env, wantReturn)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env, wantReturn)
	default:
		return false, fmt.Errorf("type error: unsupported statement %T", stmt)
	}
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope) (bool, error) {
	want, ok := env.lookup(stmt.Name)
	if !ok {
		return false, fmt.Errorf("type error: undefined variable `%s`", stmt.Name)
	}
	got, err := c.checkExpr(stmt.Value, env)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: cannot assign %s to `%s` of type %s", got, stmt.Name, want)
	}
	return false, nil
}

// checkReturnStmt validates that return value type matches the function result.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope, want Type) (bool, error) {
	got, err := c.checkExpr(stmt.Value, env)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: return expects %s, got %s", want, got)
	}
	return true, nil
}

// checkIfStmt validates a branch and tracks whether both arms return.
func (c *Checker) checkIfStmt(stmt *ast.IfStmt, env *scope, wantReturn Type) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: if condition must be bool, got %s", cond)
	}
	leftReturns, err := c.checkBlock(stmt.Consequence, env.child(), wantReturn)
	if err != nil {
		return false, err
	}
	if stmt.Alternative == nil {
		return false, nil
	}
	rightReturns, err := c.checkBlock(stmt.Alternative, env.child(), wantReturn)
	if err != nil {
		return false, err
	}
	return leftReturns && rightReturns, nil
}

// checkWhileStmt validates loop condition and body types.
func (c *Checker) checkWhileStmt(stmt *ast.WhileStmt, env *scope, wantReturn Type) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: while condition must be bool, got %s", cond)
	}
	_, err = c.checkBlock(stmt.Body, env.child(), wantReturn)
	return false, err
}

// checkExpr computes the static type of an expression.
func (c *Checker) checkExpr(expr ast.Expression, env *scope) (Type, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return typeInt, nil
	case *ast.StringExpr:
		return typeString, nil
	case *ast.BoolExpr:
		return typeBool, nil
	case *ast.IdentExpr:
		return checkIdentExpr(e, env)
	case *ast.PrefixExpr:
		return c.checkPrefixExpr(e, env)
	case *ast.BinaryExpr:
		return c.checkBinaryExpr(e, env)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env)
	case *ast.FieldExpr:
		return "", fmt.Errorf("type error: field access is not supported in phase 3")
	default:
		return "", fmt.Errorf("type error: unsupported expression %T", expr)
	}
}

// checkIdentExpr resolves a variable reference in lexical scopes.
func checkIdentExpr(expr *ast.IdentExpr, env *scope) (Type, error) {
	typ, ok := env.lookup(expr.Name)
	if !ok {
		return "", fmt.Errorf("type error: undefined variable `%s`", expr.Name)
	}
	return typ, nil
}

// checkPrefixExpr validates unary operators.
func (c *Checker) checkPrefixExpr(expr *ast.PrefixExpr, env *scope) (Type, error) {
	right, err := c.checkExpr(expr.Right, env)
	if err != nil {
		return "", err
	}
	switch expr.Operator {
	case "-":
		if !signedNumericTypes[right] {
			return "", fmt.Errorf("type error: unary - expects signed numeric, got %s", right)
		}
		return right, nil
	case "!":
		if right != typeBool {
			return "", fmt.Errorf("type error: unary ! expects bool, got %s", right)
		}
		return typeBool, nil
	default:
		return "", fmt.Errorf("type error: unsupported unary `%s`", expr.Operator)
	}
}

// checkBinaryExpr validates arithmetic, equality, and comparison operators.
func (c *Checker) checkBinaryExpr(expr *ast.BinaryExpr, env *scope) (Type, error) {
	left, err := c.checkExpr(expr.Left, env)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpr(expr.Right, env)
	if err != nil {
		return "", err
	}
	if expr.Operator == "==" || expr.Operator == "!=" {
		return checkEquality(expr.Operator, left, right)
	}
	if left != right {
		return "", fmt.Errorf("type error: operator `%s` operands must have same type", expr.Operator)
	}
	if !numericTypes[left] {
		return "", fmt.Errorf("type error: operator `%s` expects numeric operands", expr.Operator)
	}
	if expr.Operator == "%" && !integerTypes[left] {
		return "", fmt.Errorf("type error: operator `%s` expects integer operands", expr.Operator)
	}
	if isComparison(expr.Operator) {
		return typeBool, nil
	}
	return left, nil
}

// checkEquality validates equality operands.
func checkEquality(op string, left Type, right Type) (Type, error) {
	if left != right {
		return "", fmt.Errorf("type error: operator `%s` operands must have same type", op)
	}
	return typeBool, nil
}

// isComparison reports whether op returns bool for int operands.
func isComparison(op string) bool {
	return op == "<" || op == "<=" || op == ">" || op == ">="
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope) (Type, error) {
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: callee must be a function name")
	}
	if name.Name == "print" {
		return c.checkPrintCall(expr, env)
	}
	fn, ok := c.functions[name.Name]
	if !ok {
		return "", fmt.Errorf("type error: undefined function `%s`", name.Name)
	}
	if len(expr.Args) != len(fn.params) {
		return "", fmt.Errorf("type error: `%s` expects %d args, got %d",
			name.Name, len(fn.params), len(expr.Args))
	}
	for idx, arg := range expr.Args {
		got, err := c.checkExpr(arg, env)
		if err != nil {
			return "", err
		}
		if got != fn.params[idx] {
			return "", fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, name.Name, fn.params[idx], got)
		}
	}
	return fn.returnType, nil
}

// checkPrintCall validates the print builtin.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope) (Type, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `print` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env)
	if err != nil {
		return "", err
	}
	if got == typeVoid {
		return "", fmt.Errorf("type error: `print` cannot print void")
	}
	return typeVoid, nil
}

// newScope creates a lexical type scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, values: map[string]Type{}}
}

// child creates a nested lexical type scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name to a type in the current scope.
func (s *scope) define(name string, typ Type) error {
	if _, exists := s.values[name]; exists {
		return fmt.Errorf("type error: duplicate variable `%s`", name)
	}
	s.values[name] = typ
	return nil
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if typ, ok := cur.values[name]; ok {
			return typ, true
		}
	}
	return "", false
}
