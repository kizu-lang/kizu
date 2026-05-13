package types

import (
	"fmt"
	"strings"

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
	structs   map[string]*ast.StructDecl
}

type functionType struct {
	name       string
	params     []Type
	returnType Type
	decl       *ast.FunctionDecl
	unsafe     bool
	externABI  string
}

type scope struct {
	parent *scope
	values map[string]Type
}

// New creates an empty type checker.
func New() *Checker {
	return &Checker{functions: map[string]*functionType{}, structs: map[string]*ast.StructDecl{}}
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
		switch d := decl.(type) {
		case *ast.StructDecl:
			if err := c.collectStruct(d); err != nil {
				return err
			}
		case *ast.FunctionDecl:
			continue
		default:
			return fmt.Errorf("type error: unsupported declaration %T", decl)
		}
	}
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if _, exists := c.functions[fn.Name]; exists {
			return fmt.Errorf("type error: duplicate function `%s`", fn.Name)
		}
		fnType, err := c.newFunctionType(fn)
		if err != nil {
			return err
		}
		c.functions[fn.Name] = fnType
	}
	return nil
}

// collectStruct registers and validates a struct declaration.
func (c *Checker) collectStruct(decl *ast.StructDecl) error {
	if _, exists := c.structs[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate struct `%s`", decl.Name)
	}
	c.structs[decl.Name] = decl
	for _, field := range decl.Fields {
		if _, err := c.parseType(field.TypeName); err != nil {
			return err
		}
	}
	return nil
}

// newFunctionType converts a parsed function declaration into its static type.
func (c *Checker) newFunctionType(fn *ast.FunctionDecl) (*functionType, error) {
	params := make([]Type, 0, len(fn.Params))
	for _, param := range fn.Params {
		paramType, err := c.parseType(param.TypeName)
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
		ret, err = c.parseType(fn.ReturnType)
		if err != nil {
			return nil, err
		}
	}
	return &functionType{
		name: fn.Name, params: params, returnType: ret, decl: fn,
		unsafe: fn.Unsafe, externABI: fn.ExternABI,
	}, nil
}

// parseType validates a source-level type name.
func (c *Checker) parseType(name string) (Type, error) {
	if strings.HasPrefix(name, "?") {
		return c.parseNullableType(name)
	}
	if base, arg, ok := splitGenericType(name); ok {
		if base == "ptr" {
			return c.parsePointerType(name, arg)
		}
		if base != "arena" && base != "handle" {
			return "", fmt.Errorf("type error: unknown generic type `%s`", base)
		}
		if _, err := c.parseType(arg); err != nil {
			return "", err
		}
		return Type(name), nil
	}
	typ := Type(name)
	if !knownTypes[typ] && c.structs[name] == nil {
		return "", fmt.Errorf("type error: unknown type `%s`", name)
	}
	return typ, nil
}

// parseNullableType validates nullable pointer types.
func (c *Checker) parseNullableType(name string) (Type, error) {
	inner := strings.TrimPrefix(name, "?")
	base, arg, ok := splitGenericType(inner)
	if !ok || base != "ptr" {
		return "", fmt.Errorf("type error: nullable type `%s` must wrap ptr<T>", name)
	}
	if _, err := c.parsePointerType(inner, arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parsePointerType validates raw pointer element types.
func (c *Checker) parsePointerType(name string, arg string) (Type, error) {
	if strings.HasPrefix(arg, "const ") {
		arg = strings.TrimPrefix(arg, "const ")
	}
	if _, err := c.parseType(arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	if fn.externABI != "" {
		return nil
	}
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		if err := env.define(param.Name, fn.params[idx]); err != nil {
			return err
		}
	}
	returns, err := c.checkBlock(fn.decl.Body, env, fn.returnType, fn.unsafe)
	if err != nil {
		return err
	}
	if fn.returnType != typeVoid && !returns {
		return fmt.Errorf("type error: function `%s` must return %s", fn.name, fn.returnType)
	}
	return nil
}

// checkBlock validates statements and reports whether the block always returns.
func (c *Checker) checkBlock(
	block *ast.BlockStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	for _, stmt := range block.Statements {
		returns, err := c.checkStmt(stmt, env, wantReturn, unsafe)
		if err != nil || returns {
			return returns, err
		}
	}
	return false, nil
}

// checkStmt validates a statement and reports explicit return flow.
func (c *Checker) checkStmt(
	stmt ast.Statement,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		typ, err := c.checkExpr(s.Value, env, unsafe)
		if err != nil {
			return false, err
		}
		return false, env.define(s.Name, typ)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env, unsafe)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env, wantReturn, unsafe)
	case *ast.ExprStmt:
		_, err := c.checkExpr(s.Expr, env, unsafe)
		return false, err
	case *ast.IfStmt:
		return c.checkIfStmt(s, env, wantReturn, unsafe)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env, wantReturn, unsafe)
	case *ast.UnsafeStmt:
		return c.checkBlock(s.Body, env.child(), wantReturn, true)
	default:
		return false, fmt.Errorf("type error: unsupported statement %T", stmt)
	}
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope, unsafe bool) (bool, error) {
	want, ok := env.lookup(stmt.Name)
	if !ok {
		return false, fmt.Errorf("type error: undefined variable `%s`", stmt.Name)
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: cannot assign %s to `%s` of type %s", got, stmt.Name, want)
	}
	return false, nil
}

// checkReturnStmt validates that return value type matches the function result.
func (c *Checker) checkReturnStmt(
	stmt *ast.ReturnStmt,
	env *scope,
	want Type,
	unsafe bool,
) (bool, error) {
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: return expects %s, got %s", want, got)
	}
	return true, nil
}

// checkIfStmt validates a branch and tracks whether both arms return.
func (c *Checker) checkIfStmt(
	stmt *ast.IfStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: if condition must be bool, got %s", cond)
	}
	leftReturns, err := c.checkBlock(stmt.Consequence, env.child(), wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	if stmt.Alternative == nil {
		return false, nil
	}
	rightReturns, err := c.checkBlock(stmt.Alternative, env.child(), wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	return leftReturns && rightReturns, nil
}

// checkWhileStmt validates loop condition and body types.
func (c *Checker) checkWhileStmt(
	stmt *ast.WhileStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: while condition must be bool, got %s", cond)
	}
	_, err = c.checkBlock(stmt.Body, env.child(), wantReturn, unsafe)
	return false, err
}

// checkExpr computes the static type of an expression.
func (c *Checker) checkExpr(expr ast.Expression, env *scope, unsafe bool) (Type, error) {
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
		return c.checkPrefixExpr(e, env, unsafe)
	case *ast.BinaryExpr:
		return c.checkBinaryExpr(e, env, unsafe)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env, unsafe)
	case *ast.ArenaNewExpr:
		return c.checkArenaNewExpr(e)
	case *ast.StructLiteralExpr:
		return c.checkStructLiteralExpr(e, env, unsafe)
	case *ast.FieldExpr:
		return c.checkFieldExpr(e, env, unsafe)
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
func (c *Checker) checkPrefixExpr(expr *ast.PrefixExpr, env *scope, unsafe bool) (Type, error) {
	right, err := c.checkExpr(expr.Right, env, unsafe)
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
func (c *Checker) checkBinaryExpr(expr *ast.BinaryExpr, env *scope, unsafe bool) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpr(expr.Right, env, unsafe)
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
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkMethodCallExpr(field, expr.Args, env, unsafe)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: callee must be a function name")
	}
	if name.Name == "print" {
		return c.checkPrintCall(expr, env, unsafe)
	}
	if name.Name == "ptr_read" {
		return c.checkPtrRead(expr, env, unsafe)
	}
	if name.Name == "ptr_write" {
		return c.checkPtrWrite(expr, env, unsafe)
	}
	fn, ok := c.functions[name.Name]
	if !ok {
		return "", fmt.Errorf("type error: undefined function `%s`", name.Name)
	}
	if (fn.unsafe || fn.externABI != "") && !unsafe {
		return "", fmt.Errorf("unsafe error: call to `%s` requires unsafe block", name.Name)
	}
	if len(expr.Args) != len(fn.params) {
		return "", fmt.Errorf("type error: `%s` expects %d args, got %d",
			name.Name, len(fn.params), len(expr.Args))
	}
	for idx, arg := range expr.Args {
		got, err := c.checkExpr(arg, env, unsafe)
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

// checkArenaNewExpr validates arena<T>() and returns the arena type.
func (c *Checker) checkArenaNewExpr(expr *ast.ArenaNewExpr) (Type, error) {
	if _, err := c.parseType(expr.TypeName); err != nil {
		return "", err
	}
	return Type(fmt.Sprintf("arena<%s>", expr.TypeName)), nil
}

// checkStructLiteralExpr validates field names and initializer types.
func (c *Checker) checkStructLiteralExpr(
	expr *ast.StructLiteralExpr,
	env *scope,
	unsafe bool,
) (Type, error) {
	decl := c.structs[expr.TypeName]
	if decl == nil {
		return "", fmt.Errorf("type error: unknown struct `%s`", expr.TypeName)
	}
	values := map[string]Type{}
	for _, field := range expr.Fields {
		got, err := c.checkExpr(field.Value, env, unsafe)
		if err != nil {
			return "", err
		}
		values[field.Name] = got
	}
	for _, field := range decl.Fields {
		got, ok := values[field.Name]
		if !ok {
			return "", fmt.Errorf("type error: missing field `%s.%s`", expr.TypeName, field.Name)
		}
		if got != Type(field.TypeName) {
			return "", fmt.Errorf("type error: field `%s.%s` expects %s, got %s",
				expr.TypeName, field.Name, field.TypeName, got)
		}
		delete(values, field.Name)
	}
	for name := range values {
		return "", fmt.Errorf("type error: unknown field `%s.%s`", expr.TypeName, name)
	}
	return Type(expr.TypeName), nil
}

// checkFieldExpr returns the declared type of a struct field access.
func (c *Checker) checkFieldExpr(expr *ast.FieldExpr, env *scope, unsafe bool) (Type, error) {
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	decl := c.structs[string(receiver)]
	if decl == nil {
		return "", fmt.Errorf("type error: `%s` has no fields", receiver)
	}
	for _, field := range decl.Fields {
		if field.Name == expr.Name {
			return Type(field.TypeName), nil
		}
	}
	return "", fmt.Errorf("type error: unknown field `%s.%s`", receiver, expr.Name)
}

// checkMethodCallExpr validates arena methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	base, arg, ok := splitGenericType(string(receiver))
	if !ok || base != "arena" {
		return "", fmt.Errorf("type error: `%s` has no method `%s`", receiver, field.Name)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arg, args, env, unsafe)
	case "get":
		return c.checkArenaGet(arg, args, env, unsafe)
	default:
		return "", fmt.Errorf("type error: unknown arena method `%s`", field.Name)
	}
}

// checkArenaAdd validates arena<T>.add(value).
func (c *Checker) checkArenaAdd(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `arena.add` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != Type(arg) {
		return "", fmt.Errorf("type error: `arena.add` expects %s, got %s", arg, got)
	}
	return Type(fmt.Sprintf("handle<%s>", arg)), nil
}

// checkArenaGet validates arena<T>.get(handle<T>).
func (c *Checker) checkArenaGet(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `arena.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	want := Type(fmt.Sprintf("handle<%s>", arg))
	if got != want {
		return "", fmt.Errorf("type error: `arena.get` expects %s, got %s", want, got)
	}
	return Type(arg), nil
}

// checkPtrRead validates unsafe raw pointer reads.
func (c *Checker) checkPtrRead(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if !unsafe {
		return "", fmt.Errorf("unsafe error: ptr_read requires unsafe block")
	}
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `ptr_read` expects 1 arg, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") {
		return "", fmt.Errorf("type error: `ptr_read` expects non-null raw pointer, got %s", ptrType)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// checkPtrWrite validates unsafe raw pointer writes.
func (c *Checker) checkPtrWrite(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if !unsafe {
		return "", fmt.Errorf("unsafe error: ptr_write requires unsafe block")
	}
	if len(expr.Args) != 2 {
		return "", fmt.Errorf("type error: `ptr_write` expects 2 args, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") || strings.HasPrefix(elem, "const ") {
		return "", fmt.Errorf("type error: `ptr_write` expects mutable non-null raw pointer")
	}
	valueType, err := c.checkExpr(expr.Args[1], env, unsafe)
	if err != nil {
		return "", err
	}
	if valueType != Type(elem) {
		return "", fmt.Errorf("type error: `ptr_write` expects %s, got %s", elem, valueType)
	}
	return typeVoid, nil
}

// pointerElement extracts the element type from ptr<T> or ?ptr<T>.
func pointerElement(typ Type) (string, bool) {
	name := strings.TrimPrefix(string(typ), "?")
	base, arg, ok := splitGenericType(name)
	if !ok || base != "ptr" {
		return "", false
	}
	return arg, true
}

// checkPrintCall validates the print builtin.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `print` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env, unsafe)
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

// splitGenericType extracts base and argument from base<arg>.
func splitGenericType(name string) (string, string, bool) {
	start := strings.IndexByte(name, '<')
	if start < 1 || !strings.HasSuffix(name, ">") {
		return "", "", false
	}
	arg := name[start+1 : len(name)-1]
	if arg == "" || strings.ContainsAny(arg, "<>") {
		return "", "", false
	}
	return name[:start], arg, true
}
