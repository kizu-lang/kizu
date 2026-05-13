package ownership

import (
	"fmt"

	"tiny-safe/internal/ast"
)

// Checker validates ownership and move rules for a parsed program.
type Checker struct {
	functions map[string]*functionInfo
	nextID    int
}

type functionInfo struct {
	name       string
	params     []paramInfo
	returnType string
	decl       *ast.FunctionDecl
}

type paramInfo struct {
	typeName string
	borrow   bool
}

type binding struct {
	id       int
	name     string
	typeName string
	moved    bool
}

type scope struct {
	parent *scope
	values map[string]*binding
}

// New creates an empty ownership checker.
func New() *Checker {
	return &Checker{functions: map[string]*functionInfo{}}
}

// Check validates ownership rules and returns the first move error.
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

// collectFunctions registers top-level signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			return fmt.Errorf("move error: unsupported declaration %T", decl)
		}
		params := make([]paramInfo, 0, len(fn.Params))
		for _, param := range fn.Params {
			params = append(params, paramInfo{typeName: param.TypeName, borrow: param.Borrow})
		}
		c.functions[fn.Name] = &functionInfo{
			name: fn.Name, params: params, returnType: fn.ReturnType, decl: fn,
		}
	}
	return nil
}

// checkFunction validates one function body.
func (c *Checker) checkFunction(fn *functionInfo) error {
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		env.define(c.newBinding(param.Name, fn.params[idx].typeName))
	}
	return c.checkBlock(fn.decl.Body, env)
}

// checkBlock validates statements in a lexical block.
func (c *Checker) checkBlock(block *ast.BlockStmt, env *scope) error {
	for _, stmt := range block.Statements {
		if err := c.checkStmt(stmt, env); err != nil {
			return err
		}
	}
	return nil
}

// checkStmt validates one statement.
func (c *Checker) checkStmt(stmt ast.Statement, env *scope) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return c.checkLetStmt(s, env)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env)
	case *ast.ReturnStmt:
		_, err := c.moveExpr(s.Value, env)
		return err
	case *ast.ExprStmt:
		return c.checkExprStmt(s, env)
	case *ast.IfStmt:
		return c.checkIfStmt(s, env)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env)
	default:
		return fmt.Errorf("move error: unsupported statement %T", stmt)
	}
}

// checkLetStmt moves the initializer into a new binding when needed.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope) error {
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	env.define(c.newBinding(stmt.Name, typeName))
	return nil
}

// checkAssignStmt moves the assigned value into an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope) error {
	target, ok := env.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("move error: undefined variable `%s`", stmt.Name)
	}
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	target.typeName = typeName
	target.moved = false
	return nil
}

// checkExprStmt reads standalone expressions, except normal calls handle argument moves.
func (c *Checker) checkExprStmt(stmt *ast.ExprStmt, env *scope) error {
	_, err := c.readExpr(stmt.Expr, env)
	return err
}

// checkIfStmt merges possible moves from either branch into the outer scope.
func (c *Checker) checkIfStmt(stmt *ast.IfStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return err
	}
	left := env.clone()
	if err := c.checkBlock(stmt.Consequence, left.child()); err != nil {
		return err
	}
	right := env.clone()
	if stmt.Alternative != nil {
		if err := c.checkBlock(stmt.Alternative, right.child()); err != nil {
			return err
		}
	}
	env.mergeMovedFrom(left)
	env.mergeMovedFrom(right)
	return nil
}

// checkWhileStmt treats moves in the body as possible after the loop.
func (c *Checker) checkWhileStmt(stmt *ast.WhileStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return err
	}
	body := env.clone()
	if err := c.checkBlock(stmt.Body, body.child()); err != nil {
		return err
	}
	env.mergeMovedFrom(body)
	return nil
}

// readExpr checks an expression without consuming owned values.
func (c *Checker) readExpr(expr ast.Expression, env *scope) (string, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return "int", nil
	case *ast.StringExpr:
		return "string", nil
	case *ast.BoolExpr:
		return "bool", nil
	case *ast.IdentExpr:
		return readIdent(e.Name, env)
	case *ast.PrefixExpr:
		return c.readExpr(e.Right, env)
	case *ast.BinaryExpr:
		return c.readBinaryExpr(e, env)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env)
	case *ast.FieldExpr:
		return "", fmt.Errorf("move error: field access is not supported in phase 4")
	default:
		return "", fmt.Errorf("move error: unsupported expression %T", expr)
	}
}

// moveExpr checks an expression and consumes a non-copy identifier when present.
func (c *Checker) moveExpr(expr ast.Expression, env *scope) (string, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return c.readExpr(expr, env)
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		return "", fmt.Errorf("move error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", ident.Name)
	}
	if !isCopyType(value.typeName) {
		value.moved = true
	}
	return value.typeName, nil
}

// readBinaryExpr reads both operands and preserves the left operand type.
func (c *Checker) readBinaryExpr(expr *ast.BinaryExpr, env *scope) (string, error) {
	left, err := c.readExpr(expr.Left, env)
	if err != nil {
		return "", err
	}
	if _, err := c.readExpr(expr.Right, env); err != nil {
		return "", err
	}
	return left, nil
}

// checkCallExpr validates ownership effects of builtin and user calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope) (string, error) {
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("move error: callee must be a function name")
	}
	if name.Name == "print" {
		return c.checkPrintCall(expr, env)
	}
	fn, ok := c.functions[name.Name]
	if !ok {
		return "", fmt.Errorf("move error: undefined function `%s`", name.Name)
	}
	for idx, arg := range expr.Args {
		var err error
		if fn.params[idx].borrow {
			_, err = c.readExpr(arg, env)
		} else {
			_, err = c.moveExpr(arg, env)
		}
		if err != nil {
			return "", err
		}
	}
	return returnTypeName(fn), nil
}

// checkPrintCall reads the printed value without taking ownership.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "void", nil
}

// newBinding creates a local ownership binding with a stable ID.
func (c *Checker) newBinding(name string, typeName string) *binding {
	c.nextID++
	return &binding{id: c.nextID, name: name, typeName: typeName}
}

// readIdent resolves a variable reference without moving it.
func readIdent(name string, env *scope) (string, error) {
	value, ok := env.lookup(name)
	if !ok {
		return "", fmt.Errorf("move error: undefined variable `%s`", name)
	}
	if value.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", name)
	}
	return value.typeName, nil
}

// returnTypeName returns void for functions without an explicit return type.
func returnTypeName(fn *functionInfo) string {
	if fn.returnType == "" {
		return "void"
	}
	return fn.returnType
}

// isCopyType reports whether values of typeName can be reused after move contexts.
func isCopyType(typeName string) bool {
	switch typeName {
	case "bool", "int", "void",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64":
		return true
	default:
		return false
	}
}

// newScope creates a lexical ownership scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, values: map[string]*binding{}}
}

// child creates a nested lexical ownership scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name in the current scope.
func (s *scope) define(value *binding) {
	s.values[value.name] = value
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if value, ok := cur.values[name]; ok {
			return value, true
		}
	}
	return nil, false
}

// clone copies a scope chain so branch checks do not interfere with each other.
func (s *scope) clone() *scope {
	if s == nil {
		return nil
	}
	cloned := &scope{values: map[string]*binding{}}
	cloned.parent = s.parent.clone()
	for name, value := range s.values {
		copyValue := *value
		cloned.values[name] = &copyValue
	}
	return cloned
}

// mergeMovedFrom marks bindings moved when a checked branch may have moved them.
func (s *scope) mergeMovedFrom(other *scope) {
	byID := map[int]*binding{}
	s.collectBindings(byID)
	other.walkBindings(func(value *binding) {
		if value.moved {
			if target, ok := byID[value.id]; ok {
				target.moved = true
			}
		}
	})
}

// collectBindings stores bindings by stable ID.
func (s *scope) collectBindings(out map[int]*binding) {
	s.walkBindings(func(value *binding) {
		out[value.id] = value
	})
}

// walkBindings visits all bindings in this scope chain.
func (s *scope) walkBindings(visit func(*binding)) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			visit(value)
		}
	}
}
