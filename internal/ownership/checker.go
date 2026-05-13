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
	id            int
	name          string
	typeName      string
	moved         bool
	borrowedParam bool
	activeBorrows int
	arenaID       int
	handleArenaID int
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
	if err := c.checkStructs(program); err != nil {
		return err
	}
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

// checkStructs rejects struct fields that would store a local borrow.
func (c *Checker) checkStructs(program *ast.Program) error {
	for _, decl := range program.Decls {
		st, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}
		for _, field := range st.Fields {
			if field.Borrow {
				return fmt.Errorf("borrow error: struct field `%s.%s` cannot store borrow",
					st.Name, field.Name)
			}
		}
	}
	return nil
}

// collectFunctions registers top-level signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
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
	if fn.decl.ExternABI != "" {
		return nil
	}
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		value := c.newBinding(param.Name, fn.params[idx].typeName)
		value.borrowedParam = fn.params[idx].borrow
		env.define(value)
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
		return c.checkReturnStmt(s, env)
	case *ast.ExprStmt:
		return c.checkExprStmt(s, env)
	case *ast.IfStmt:
		return c.checkIfStmt(s, env)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env)
	case *ast.UnsafeStmt:
		return c.checkBlock(s.Body, env.child())
	default:
		return fmt.Errorf("move error: unsupported statement %T", stmt)
	}
}

// checkReturnStmt rejects borrowed values before applying normal move rules.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope) error {
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if exists && value.borrowedParam {
			return fmt.Errorf("borrow error: borrowed value `%s` cannot escape", ident.Name)
		}
		if exists && value.handleArenaID != 0 {
			return fmt.Errorf("arena error: handle `%s` cannot outlive its arena", ident.Name)
		}
	}
	if arena := c.arenaAddReceiver(stmt.Value, env); arena != nil && arena.arenaID != 0 {
		return fmt.Errorf("arena error: handle from `%s` cannot outlive its arena", arena.name)
	}
	_, err := c.moveExpr(stmt.Value, env)
	return err
}

// checkLetStmt moves the initializer into a new binding when needed.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope) error {
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	value := c.newBinding(stmt.Name, typeName)
	c.setArenaProvenance(value, stmt.Value, env)
	env.define(value)
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
	target.arenaID = 0
	target.handleArenaID = 0
	c.setArenaProvenance(target, stmt.Value, env)
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
	case *ast.ArenaNewExpr:
		return fmt.Sprintf("arena<%s>", e.TypeName), nil
	case *ast.StructLiteralExpr:
		return c.readStructLiteralExpr(e, env)
	case *ast.FieldExpr:
		return c.readFieldExpr(e, env)
	default:
		return "", fmt.Errorf("move error: unsupported expression %T", expr)
	}
}

// moveExpr checks an expression and consumes a non-copy identifier when present.
func (c *Checker) moveExpr(expr ast.Expression, env *scope) (string, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		if st, ok := expr.(*ast.StructLiteralExpr); ok {
			return c.moveStructLiteralExpr(st, env)
		}
		return c.readExpr(expr, env)
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		return "", fmt.Errorf("move error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", ident.Name)
	}
	if value.borrowedParam {
		return "", fmt.Errorf("borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	if value.activeBorrows > 0 && !isCopyType(value.typeName) {
		return "", fmt.Errorf("borrow error: value `%s` cannot be moved while borrowed", ident.Name)
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
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkMethodCallExpr(field, expr.Args, env)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("move error: callee must be a function name")
	}
	if name.Name == "print" {
		return c.checkPrintCall(expr, env)
	}
	if name.Name == "ptr_read" || name.Name == "ptr_write" {
		return c.checkPointerBuiltin(expr, env)
	}
	fn, ok := c.functions[name.Name]
	if !ok {
		return "", fmt.Errorf("move error: undefined function `%s`", name.Name)
	}
	if len(expr.Args) != len(fn.params) {
		return "", fmt.Errorf("move error: `%s` expects %d args, got %d",
			name.Name, len(fn.params), len(expr.Args))
	}
	borrowed, err := c.activateBorrowArgs(fn, expr.Args, env)
	if err != nil {
		return "", err
	}
	defer releaseBorrows(borrowed)
	for idx, arg := range expr.Args {
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

// checkPointerBuiltin reads raw pointer builtin arguments without moving values.
func (c *Checker) checkPointerBuiltin(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	if name, ok := expr.Callee.(*ast.IdentExpr); ok && name.Name == "ptr_read" {
		return "int", nil
	}
	return "void", nil
}

// readStructLiteralExpr checks a literal without consuming field values.
func (c *Checker) readStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	for _, field := range expr.Fields {
		if _, err := c.readExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	return expr.TypeName, nil
}

// moveStructLiteralExpr moves field values into a new struct value.
func (c *Checker) moveStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	for _, field := range expr.Fields {
		if _, err := c.moveExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	return expr.TypeName, nil
}

// readFieldExpr reads a field without moving the receiver.
func (c *Checker) readFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	return c.readExpr(expr.Receiver, env)
}

// checkMethodCallExpr validates ownership effects of arena methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("arena error: arena method receiver must be a local binding")
	}
	arena, exists := env.lookup(receiver.Name)
	if !exists {
		return "", fmt.Errorf("arena error: undefined arena `%s`", receiver.Name)
	}
	if arena.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", receiver.Name)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "get":
		return c.checkArenaGet(arena, args, env)
	default:
		return "", fmt.Errorf("arena error: unknown arena method `%s`", field.Name)
	}
}

// checkArenaAdd moves one value into an arena and returns a handle.
func (c *Checker) checkArenaAdd(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("arena error: `arena.add` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "arena" {
		return "", fmt.Errorf("arena error: `%s` is not an arena", arena.name)
	}
	got, err := c.moveExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != arg {
		return "", fmt.Errorf("arena error: `arena.add` expects %s, got %s", arg, got)
	}
	return fmt.Sprintf("handle<%s>", arg), nil
}

// checkArenaGet reads a handle and returns a local borrow-like value.
func (c *Checker) checkArenaGet(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("arena error: `arena.get` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "arena" {
		return "", fmt.Errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return arg, nil
}

// checkHandleProvenance rejects handles that came from a different known arena.
func (c *Checker) checkHandleProvenance(arena *binding, expr ast.Expression, env *scope) error {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	handle, exists := env.lookup(ident.Name)
	if !exists {
		return fmt.Errorf("arena error: undefined handle `%s`", ident.Name)
	}
	if handle.handleArenaID != 0 && handle.handleArenaID != arena.arenaID {
		return fmt.Errorf("arena error: handle `%s` does not belong to arena `%s`",
			ident.Name, arena.name)
	}
	return nil
}

// activateBorrowArgs marks identifier arguments that are borrowed for this call.
func (c *Checker) activateBorrowArgs(
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
) ([]*binding, error) {
	borrowed := []*binding{}
	for idx, arg := range args {
		if !fn.params[idx].borrow {
			continue
		}
		value, err := borrowedIdent(arg, env)
		if err != nil {
			releaseBorrows(borrowed)
			return nil, err
		}
		if value != nil {
			value.activeBorrows++
			borrowed = append(borrowed, value)
		}
	}
	return borrowed, nil
}

// borrowedIdent resolves an identifier borrow target when the argument is a name.
func borrowedIdent(expr ast.Expression, env *scope) (*binding, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, nil
	}
	value, exists := env.lookup(ident.Name)
	if !exists {
		return nil, fmt.Errorf("borrow error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return nil, fmt.Errorf("borrow error: moved value `%s` was borrowed", ident.Name)
	}
	return value, nil
}

// releaseBorrows clears temporary borrow state for a completed call.
func releaseBorrows(values []*binding) {
	for _, value := range values {
		value.activeBorrows--
	}
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

// setArenaProvenance records arena and handle origins for local bindings.
func (c *Checker) setArenaProvenance(value *binding, expr ast.Expression, env *scope) {
	if _, ok := expr.(*ast.ArenaNewExpr); ok {
		value.arenaID = value.id
		return
	}
	arena := c.arenaAddReceiver(expr, env)
	if arena != nil {
		value.handleArenaID = arena.arenaID
	}
}

// arenaAddReceiver returns the arena binding for arena.add(value) expressions.
func (c *Checker) arenaAddReceiver(expr ast.Expression, env *scope) *binding {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "add" {
		return nil
	}
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	arena, _ := env.lookup(receiver.Name)
	return arena
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
	if isRawPointerType(typeName) {
		return true
	}
	switch typeName {
	case "bool", "int", "void",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64":
		return true
	default:
		return false
	}
}

// isRawPointerType reports whether typeName is a raw pointer spelling.
func isRawPointerType(typeName string) bool {
	name := typeName
	if len(name) > 0 && name[0] == '?' {
		name = name[1:]
	}
	base, _, ok := splitGenericType(name)
	return ok && base == "ptr"
}

// splitGenericType extracts base and argument from base<arg>.
func splitGenericType(name string) (string, string, bool) {
	for idx, ch := range name {
		if ch != '<' {
			continue
		}
		if len(name) < idx+3 || name[len(name)-1] != '>' {
			return "", "", false
		}
		arg := name[idx+1 : len(name)-1]
		if arg == "" {
			return "", "", false
		}
		return name[:idx], arg, true
	}
	return "", "", false
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
