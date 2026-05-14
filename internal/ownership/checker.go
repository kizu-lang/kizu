package ownership

import (
	"fmt"

	"tiny-safe/internal/ast"
)

// Checker validates ownership and move rules for a parsed program.
type Checker struct {
	functions map[string]*functionInfo
	structs   map[string]map[string]string
	enums     map[string]map[string]bool
	unions    map[string]map[string]string
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
	comptime bool
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
	taskDone      bool
}

type scope struct {
	parent *scope
	values map[string]*binding
}

// New creates an empty ownership checker.
func New() *Checker {
	return &Checker{
		functions: map[string]*functionInfo{},
		structs:   map[string]map[string]string{},
		enums:     map[string]map[string]bool{},
		unions:    map[string]map[string]string{},
	}
}

// Check validates ownership rules and returns the first move error.
func (c *Checker) Check(program *ast.Program) error {
	if err := c.checkStructs(program); err != nil {
		return err
	}
	c.collectEnums(program)
	c.collectUnions(program)
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

// collectEnums records tag enum declarations for enum value reads.
func (c *Checker) collectEnums(program *ast.Program) {
	for _, decl := range program.Decls {
		enumDecl, ok := decl.(*ast.EnumDecl)
		if !ok {
			continue
		}
		tags := map[string]bool{}
		for _, tag := range enumDecl.Tags {
			tags[tag] = true
		}
		c.enums[enumDecl.Name] = tags
	}
}

// collectUnions records tagged union declarations for variant construction and matches.
func (c *Checker) collectUnions(program *ast.Program) {
	for _, decl := range program.Decls {
		unionDecl, ok := decl.(*ast.UnionDecl)
		if !ok {
			continue
		}
		variants := map[string]string{}
		for _, variant := range unionDecl.Variants {
			variants[variant.Name] = variant.Payload
		}
		c.unions[unionDecl.Name] = variants
	}
}

// checkStructs rejects struct fields that would store a local borrow.
func (c *Checker) checkStructs(program *ast.Program) error {
	for _, decl := range program.Decls {
		st, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}
		fields := map[string]string{}
		for _, field := range st.Fields {
			if field.Borrow {
				return fmt.Errorf("borrow error: struct field `%s.%s` cannot store borrow",
					st.Name, field.Name)
			}
			fields[field.Name] = field.TypeName
		}
		c.structs[st.Name] = fields
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
			params = append(params, paramInfo{
				typeName: param.TypeName, borrow: param.Borrow, comptime: param.Comptime,
			})
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
	return env.checkPendingTasks()
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
	case *ast.MatchStmt:
		return c.checkMatchStmt(s, env)
	case *ast.UnsafeStmt:
		return c.checkBlock(s.Body, env.child())
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env)
	default:
		return fmt.Errorf("move error: unsupported statement %T", stmt)
	}
}

// checkReturnStmt rejects borrowed values before applying normal move rules.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope) error {
	if stmt.Value == nil {
		return nil
	}
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

// checkMatchStmt merges possible moves from enum or union match arms into the outer scope.
func (c *Checker) checkMatchStmt(stmt *ast.MatchStmt, env *scope) error {
	valueType, err := c.readExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	tags, unionPayloads, ok := c.matchTags(valueType)
	if !ok {
		return fmt.Errorf("move error: match expects enum or union, got %s", valueType)
	}
	for _, arm := range stmt.Arms {
		if !tags[arm.Tag] {
			return fmt.Errorf("move error: unknown match tag `%s.%s`", valueType, arm.Tag)
		}
		armEnv := env.clone()
		child := armEnv.child()
		if payload := unionPayloads[arm.Tag]; payload != "" && arm.Binding != "" {
			value := c.newBinding(arm.Binding, payload)
			value.borrowedParam = true
			child.define(value)
		}
		if err := c.checkStmt(arm.Body, child); err != nil {
			return err
		}
		env.mergeMovedFrom(armEnv)
	}
	return nil
}

// matchTags returns known tags for enum and union match ownership checks.
func (c *Checker) matchTags(typeName string) (map[string]bool, map[string]string, bool) {
	if tags := c.enums[typeName]; tags != nil {
		return tags, nil, true
	}
	payloads := c.unions[typeName]
	if payloads == nil {
		return nil, nil, false
	}
	tags := map[string]bool{}
	for tag := range payloads {
		tags[tag] = true
	}
	return tags, payloads, true
}

// readExpr checks an expression without consuming owned values.
func (c *Checker) readExpr(expr ast.Expression, env *scope) (string, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return "i64", nil
	case *ast.StringExpr:
		return "[]const u8", nil
	case *ast.BoolExpr:
		return "bool", nil
	case *ast.ComptimeExpr:
		return c.readComptimeExpr(e, env)
	case *ast.IdentExpr:
		return readIdent(e.Name, env)
	case *ast.PrefixExpr:
		return c.readExpr(e.Right, env)
	case *ast.BinaryExpr:
		return c.readBinaryExpr(e, env)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env)
	case *ast.CastExpr:
		return c.readCastExpr(e, env)
	case *ast.TryExpr:
		return c.readTryExpr(e, env)
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

// readCastExpr reads the source value and returns the explicit target type.
func (c *Checker) readCastExpr(expr *ast.CastExpr, env *scope) (string, error) {
	if _, err := c.readExpr(expr.Value, env); err != nil {
		return "", err
	}
	return expr.TargetType, nil
}

// moveExpr checks an expression and consumes a non-copy identifier when present.
func (c *Checker) moveExpr(expr ast.Expression, env *scope) (string, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		if c.isArenaGetExpr(expr) {
			if _, err := c.readExpr(expr, env); err != nil {
				return "", err
			}
			return "", fmt.Errorf("arena error: arena.get returns a local borrow and cannot be moved")
		}
		if st, ok := expr.(*ast.StructLiteralExpr); ok {
			return c.moveStructLiteralExpr(st, env)
		}
		if field, ok := expr.(*ast.FieldExpr); ok {
			return c.moveFieldExpr(field, env)
		}
		return c.readExpr(expr, env)
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		if ident.Name == "void" {
			return "void", nil
		}
		return "", fmt.Errorf("move error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", ident.Name)
	}
	if value.borrowedParam {
		return "", fmt.Errorf("borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	if value.activeBorrows > 0 && !c.isCopyType(value.typeName) {
		return "", fmt.Errorf("borrow error: value `%s` cannot be moved while borrowed", ident.Name)
	}
	if !c.isCopyType(value.typeName) {
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
		if typ, ok, err := c.checkUnionConstructor(field, expr.Args, env); ok || err != nil {
			return typ, err
		}
		return c.checkMethodCallExpr(field, expr.Args, env)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("move error: callee must be a function name")
	}
	if result, ok, err := c.checkBuiltinCall(name.Name, expr, env); ok || err != nil {
		return result, err
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
		if fn.params[idx].comptime {
			_, err = c.readExpr(arg, env)
		} else if fn.params[idx].borrow {
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

// checkBuiltinCall validates ownership effects for builtin calls.
func (c *Checker) checkBuiltinCall(
	name string,
	expr *ast.CallExpr,
	env *scope,
) (string, bool, error) {
	switch name {
	case "print":
		result, err := c.checkPrintCall(expr, env)
		return result, true, err
	case "ptr_read", "ptr_write":
		result, err := c.checkPointerBuiltin(expr, env)
		return result, true, err
	case "error":
		result, err := c.checkErrorCall(expr, env)
		return result, true, err
	case "Io", "TaskGroup":
		result, err := checkNoArgOwnershipCall(name, expr.Args)
		return result, true, err
	default:
		return "", false, nil
	}
}

// checkErrorCall reads the error message and returns the current error-union shape.
func (c *Checker) checkErrorCall(expr *ast.CallExpr, env *scope) (string, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("move error: `error` expects 1 arg, got %d", len(expr.Args))
	}
	if _, err := c.readExpr(expr.Args[0], env); err != nil {
		return "", err
	}
	return "!unknown", nil
}

// readTryExpr reads a !T expression and returns T.
func (c *Checker) readTryExpr(expr *ast.TryExpr, env *scope) (string, error) {
	got, err := c.readExpr(expr.Value, env)
	if err != nil {
		return "", err
	}
	arg, ok := errorUnionElement(got)
	if !ok {
		return "", fmt.Errorf("move error: try expects !T, got %s", got)
	}
	return arg, nil
}

// checkPointerBuiltin reads raw pointer builtin arguments without moving values.
func (c *Checker) checkPointerBuiltin(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	if name, ok := expr.Callee.(*ast.IdentExpr); ok && name.Name == "ptr_read" {
		return "i64", nil
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
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if tags, exists := c.enums[ident.Name]; exists {
			if !tags[expr.Name] {
				return "", fmt.Errorf("move error: unknown enum tag `%s.%s`", ident.Name, expr.Name)
			}
			return ident.Name, nil
		}
		if variants, exists := c.unions[ident.Name]; exists {
			payload, ok := variants[expr.Name]
			if !ok {
				return "", fmt.Errorf("move error: unknown union variant `%s.%s`",
					ident.Name, expr.Name)
			}
			if payload != "" {
				return "", fmt.Errorf("move error: union variant `%s.%s` expects payload",
					ident.Name, expr.Name)
			}
			return ident.Name, nil
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", err
	}
	if fields := c.structs[receiverType]; fields != nil {
		if typ, ok := fields[expr.Name]; ok {
			return typ, nil
		}
	}
	return receiverType, nil
}

// moveFieldExpr rejects partial moves from borrowed or aggregate values.
func (c *Checker) moveFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	typeName, err := c.readFieldExpr(expr, env)
	if err != nil {
		return "", err
	}
	if c.isCopyType(typeName) {
		return typeName, nil
	}
	if name, ok := c.borrowedFieldRoot(expr, env); ok {
		return "", fmt.Errorf(
			"borrow error: field `%s` cannot be moved out of borrowed value `%s`",
			expr.String(),
			name,
		)
	}
	if c.containsArenaGet(expr.Receiver) {
		return "", fmt.Errorf(
			"arena error: arena.get returns a local borrow and its fields cannot be moved",
		)
	}
	return "", fmt.Errorf("move error: field `%s` cannot be moved out of aggregate", expr.String())
}

// borrowedFieldRoot returns the borrowed identifier at the root of a field chain.
func (c *Checker) borrowedFieldRoot(expr ast.Expression, env *scope) (string, bool) {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return c.borrowedFieldRoot(e.Receiver, env)
	case *ast.IdentExpr:
		value, ok := env.lookup(e.Name)
		return e.Name, ok && value.borrowedParam
	default:
		return "", false
	}
}

// checkUnionConstructor validates ownership effects of Union.Variant(payload).
func (c *Checker) checkUnionConstructor(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", false, nil
	}
	variants := c.unions[ident.Name]
	if variants == nil {
		return "", false, nil
	}
	payload, exists := variants[field.Name]
	if !exists {
		return "", true, fmt.Errorf("move error: unknown union variant `%s.%s`",
			ident.Name, field.Name)
	}
	if payload == "" {
		return "", true, fmt.Errorf("move error: union variant `%s.%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("move error: union variant `%s.%s` expects 1 arg, got %d",
			ident.Name, field.Name, len(args))
	}
	if _, err := c.moveExpr(args[0], env); err != nil {
		return "", true, err
	}
	return ident.Name, true, nil
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
	base, _, ok := splitGenericType(arena.typeName)
	if !ok || base != "arena" {
		if arena.typeName == "TaskGroup" {
			return c.checkTaskGroupMethod(field.Name, args, env)
		}
		if elem, ok := taskElement(arena.typeName); ok {
			return c.checkTaskMethod(arena, field.Name, elem, args)
		}
		return c.checkPlainMethodArgs(args, env)
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

// checkTaskGroupMethod validates spawn ownership effects.
func (c *Checker) checkTaskGroupMethod(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name != "spawn" {
		return "", fmt.Errorf("task error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 2 {
		return "", fmt.Errorf("task error: `TaskGroup.spawn` expects io, function, and args")
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("task error: `TaskGroup.spawn` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", fmt.Errorf("task error: undefined function `%s`", target.Name)
	}
	spawnArgs := append([]ast.Expression{args[0]}, args[2:]...)
	if len(spawnArgs) != len(fn.params) {
		return "", fmt.Errorf("task error: `%s` expects %d args, got %d",
			target.Name, len(fn.params), len(spawnArgs))
	}
	for idx, arg := range spawnArgs {
		if fn.params[idx].borrow {
			return "", fmt.Errorf("task error: task cannot capture borrow parameter `%s`", target.Name)
		}
		if _, err := c.moveExpr(arg, env); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Task<%s>", returnTypeName(fn)), nil
}

// checkTaskMethod marks task completion for await/cancel.
func (c *Checker) checkTaskMethod(
	task *binding,
	name string,
	elem string,
	args []ast.Expression,
) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("task error: `task.%s` expects 0 args, got %d", name, len(args))
	}
	switch name {
	case "await":
		task.taskDone = true
		return elem, nil
	case "cancel":
		task.taskDone = true
		return "void", nil
	default:
		return "", fmt.Errorf("task error: Task has no method `%s`", name)
	}
}

// checkPlainMethodArgs reads non-arena method arguments after type checking.
func (c *Checker) checkPlainMethodArgs(args []ast.Expression, env *scope) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "!unknown", nil
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
	if arena.arenaID == 0 {
		return fmt.Errorf("arena error: arena `%s` has unknown provenance", arena.name)
	}
	if addArena := c.arenaAddReceiver(expr, env); addArena != nil {
		if addArena.arenaID != arena.arenaID {
			return fmt.Errorf("arena error: handle from `%s` does not belong to arena `%s`",
				addArena.name, arena.name)
		}
		return nil
	}
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return fmt.Errorf("arena error: handle expression has unknown arena provenance")
	}
	handle, exists := env.lookup(ident.Name)
	if !exists {
		return fmt.Errorf("arena error: undefined handle `%s`", ident.Name)
	}
	if handle.handleArenaID == 0 {
		return fmt.Errorf("arena error: handle `%s` has unknown arena provenance", ident.Name)
	}
	if handle.handleArenaID != arena.arenaID {
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

// isArenaGetExpr reports whether expr is an arena.get call.
func (c *Checker) isArenaGetExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	return ok && field.Name == "get"
}

// containsArenaGet reports whether an expression reads through arena.get.
func (c *Checker) containsArenaGet(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if c.isArenaGetExpr(e) {
			return true
		}
		for _, arg := range e.Args {
			if c.containsArenaGet(arg) {
				return true
			}
		}
	case *ast.FieldExpr:
		return c.containsArenaGet(e.Receiver)
	case *ast.PrefixExpr:
		return c.containsArenaGet(e.Right)
	case *ast.BinaryExpr:
		return c.containsArenaGet(e.Left) || c.containsArenaGet(e.Right)
	case *ast.CastExpr:
		return c.containsArenaGet(e.Value)
	case *ast.TryExpr:
		return c.containsArenaGet(e.Value)
	case *ast.ComptimeExpr:
		return c.containsArenaGet(e.Expr)
	}
	return false
}

// readIdent resolves a variable reference without moving it.
func readIdent(name string, env *scope) (string, error) {
	value, ok := env.lookup(name)
	if ok {
		if value.moved {
			return "", fmt.Errorf("move error: moved value `%s` was used", name)
		}
		return value.typeName, nil
	}
	if name == "void" {
		return "void", nil
	}
	return "", fmt.Errorf("move error: undefined variable `%s`", name)
}

// errorUnionElement extracts T from !T.
func errorUnionElement(typeName string) (string, bool) {
	if len(typeName) <= 1 || typeName[0] != '!' {
		return "", false
	}
	return typeName[1:], true
}

// returnTypeName returns void for functions without an explicit return type.
func returnTypeName(fn *functionInfo) string {
	if fn.returnType == "" {
		return "void"
	}
	return fn.returnType
}

// isCopyType reports whether values of typeName can be reused after move contexts.
func (c *Checker) isCopyType(typeName string) bool {
	if isRawPointerType(typeName) {
		return true
	}
	if c.enums[typeName] != nil {
		return true
	}
	switch typeName {
	case "bool", "void", "Io",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "[]const u8":
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

// checkNoArgOwnershipCall validates a zero-argument builtin constructor.
func checkNoArgOwnershipCall(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("move error: `%s` expects 0 args, got %d", name, len(args))
	}
	return name, nil
}

// taskElement extracts T from Task<T>.
func taskElement(typeName string) (string, bool) {
	base, arg, ok := splitGenericType(typeName)
	if !ok || base != "Task" {
		return "", false
	}
	return arg, true
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

// checkPendingTasks rejects tasks that leave scope without await or cancel.
func (s *scope) checkPendingTasks() error {
	for name, value := range s.values {
		if _, ok := taskElement(value.typeName); ok && !value.taskDone {
			return fmt.Errorf("task error: task `%s` must be awaited or canceled", name)
		}
	}
	return nil
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
