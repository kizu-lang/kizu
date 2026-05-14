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
	impls     map[string]map[string]*ast.FunctionDecl
	enums     map[string]map[string]bool
	unions    map[string]map[string]string
}

type trySignal struct {
	value Value
}

// Error marks trySignal as an internal interpreter control signal.
func (s trySignal) Error() string {
	return "try signal"
}

type loopSignal struct {
	kind  string
	label string
}

// Error marks loopSignal as an internal interpreter control signal.
func (s loopSignal) Error() string {
	return s.kind + " signal"
}

// New creates an interpreter that writes builtin output to out.
func New(out io.Writer) *Interpreter {
	return &Interpreter{
		out:       out,
		functions: map[string]*ast.FunctionDecl{},
		impls:     map[string]map[string]*ast.FunctionDecl{},
		enums:     map[string]map[string]bool{},
		unions:    map[string]map[string]string{},
	}
}

// Run registers top-level declarations and calls main.
func (i *Interpreter) Run(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.EnumDecl:
			i.enums[d.Name] = enumTags(d.Tags)
		case *ast.UnionDecl:
			i.unions[d.Name] = unionVariants(d.Variants)
		case *ast.FunctionDecl:
			if d.ExternABI != "" {
				continue
			}
			i.functions[d.Name] = d
		case *ast.ImplDecl:
			i.registerImpl(d)
		default:
			continue
		}
	}
	_, err := i.callFunction("main", nil)
	return err
}

// registerImpl records concrete methods for runtime dispatch.
func (i *Interpreter) registerImpl(decl *ast.ImplDecl) {
	methods := i.impls[decl.TypeName]
	if methods == nil {
		methods = map[string]*ast.FunctionDecl{}
		i.impls[decl.TypeName] = methods
	}
	for _, method := range decl.Methods {
		methods[method.Name] = method
	}
}

// enumTags returns a lookup set for runtime enum tag validation.
func enumTags(tags []string) map[string]bool {
	out := map[string]bool{}
	for _, tag := range tags {
		out[tag] = true
	}
	return out
}

// unionVariants returns a variant payload lookup for runtime construction.
func unionVariants(variants []ast.UnionVariant) map[string]string {
	out := map[string]string{}
	for _, variant := range variants {
		out[variant.Name] = variant.Payload
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

// callFunctionExpr invokes a function and preserves local borrow aliases.
func (i *Interpreter) callFunctionExpr(
	fn *ast.FunctionDecl,
	args []ast.Expression,
	caller *Env,
) (Value, error) {
	if len(args) != len(fn.Params) {
		return voidValue(), fmt.Errorf("runtime error: `%s` expected %d args", fn.Name, len(fn.Params))
	}
	env := NewEnv()
	for idx, param := range fn.Params {
		value, err := i.evalCallArg(param, args[idx], caller)
		if err != nil {
			return voidValue(), err
		}
		if err := env.Define(param.Name, value, false); err != nil {
			return voidValue(), err
		}
	}
	result, returned, err := i.evalBlock(fn.Body, env)
	if err != nil || returned {
		return result, err
	}
	return voidValue(), nil
}

// evalCallArg evaluates owned arguments or creates a local borrow reference.
func (i *Interpreter) evalCallArg(param ast.Param, arg ast.Expression, env *Env) (Value, error) {
	if !param.Borrow {
		return i.evalExpr(arg, env)
	}
	ident, ok := arg.(*ast.IdentExpr)
	if !ok {
		if param.MutBorrow {
			return voidValue(), fmt.Errorf("runtime error: mutable borrow argument must be local")
		}
		return i.evalExpr(arg, env)
	}
	binding, ok := env.Binding(ident.Name)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: undefined binding `%s`", ident.Name)
	}
	return refValue(binding), nil
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
		if s.Value == nil {
			return voidValue(), true, nil
		}
		value, err := i.evalExpr(s.Value, env)
		return value, true, err
	case *ast.ExprStmt:
		value, err := i.evalExpr(s.Expr, env)
		return value, false, err
	case *ast.IfStmt:
		return i.evalIfStmt(s, env)
	case *ast.WhileStmt:
		return i.evalWhileStmt(s, env)
	case *ast.ForStmt:
		return i.evalForStmt(s, env)
	case *ast.BreakStmt:
		return voidValue(), false, loopSignal{kind: "break", label: s.Label}
	case *ast.ContinueStmt:
		return voidValue(), false, loopSignal{kind: "continue", label: s.Label}
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
	return voidValue(), false, i.assignTarget(stmt.Target, value, env)
}

// assignTarget writes a value into a binding, field, or dereferenced borrow.
func (i *Interpreter) assignTarget(target ast.Expression, value Value, env *Env) error {
	switch expr := target.(type) {
	case *ast.IdentExpr:
		return env.Assign(expr.Name, value)
	case *ast.FieldExpr:
		return i.assignField(expr, value, env)
	case *ast.DerefExpr:
		ref, err := i.evalExpr(expr.Receiver, env)
		if err != nil {
			return err
		}
		return assignRef(ref, value)
	default:
		return fmt.Errorf("runtime error: invalid assignment target `%s`", target.String())
	}
}

// assignField writes a struct field through a mutable binding or &mut dereference.
func (i *Interpreter) assignField(expr *ast.FieldExpr, value Value, env *Env) error {
	switch receiver := expr.Receiver.(type) {
	case *ast.IdentExpr:
		return assignBindingField(receiver.Name, expr.Name, value, env)
	case *ast.DerefExpr:
		base, err := i.evalExpr(receiver.Receiver, env)
		if err != nil {
			return err
		}
		if base.kind != kindRef {
			return fmt.Errorf("runtime error: `%s` is not a borrow", receiver.Receiver.String())
		}
		return assignStructField(&base.ref.value, expr.Name, value)
	default:
		return fmt.Errorf("runtime error: invalid field assignment target `%s`", expr.String())
	}
}

// assignBindingField writes a field on a mutable local struct binding.
func assignBindingField(name string, field string, value Value, env *Env) error {
	binding, ok := env.Binding(name)
	if !ok {
		return fmt.Errorf("runtime error: undefined binding `%s`", name)
	}
	if !binding.mutable {
		return fmt.Errorf("runtime error: cannot assign field of immutable binding `%s`", name)
	}
	return assignStructField(&binding.value, field, value)
}

// assignStructField writes one field on a runtime struct value.
func assignStructField(target *Value, field string, value Value) error {
	if target.kind != kindStruct {
		return fmt.Errorf("runtime error: field assignment expects struct")
	}
	if _, ok := target.fields[field]; !ok {
		return fmt.Errorf("runtime error: unknown field `%s`", field)
	}
	target.fields[field] = value
	return nil
}

// assignRef writes through a local borrow reference.
func assignRef(target Value, value Value) error {
	if target.kind != kindRef {
		return fmt.Errorf("runtime error: assignment target is not a borrow")
	}
	target.ref.value = value
	return nil
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
		if signal, ok := err.(loopSignal); ok {
			if handledLoopSignal(signal, stmt.Label) {
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

// evalForStmt executes a bounded i64 range loop.
func (i *Interpreter) evalForStmt(stmt *ast.ForStmt, env *Env) (Value, bool, error) {
	start, end, err := i.evalForBounds(stmt, env)
	if err != nil {
		return voidValue(), false, err
	}
	for idx := start; idx < end; idx++ {
		child := env.Child()
		if err := child.Define(stmt.Name, intValue(idx), false); err != nil {
			return voidValue(), false, err
		}
		result, returned, err := i.evalBlock(stmt.Body, child)
		if signal, ok := err.(loopSignal); ok {
			if handledLoopSignal(signal, stmt.Label) {
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
	return voidValue(), false, nil
}

// evalForBounds evaluates and validates for range bounds.
func (i *Interpreter) evalForBounds(stmt *ast.ForStmt, env *Env) (int64, int64, error) {
	start, err := i.evalExpr(stmt.Start, env)
	if err != nil {
		return 0, 0, err
	}
	end, err := i.evalExpr(stmt.End, env)
	if err != nil {
		return 0, 0, err
	}
	if start.kind != kindInt || end.kind != kindInt {
		return 0, 0, fmt.Errorf("runtime error: for range expects integers")
	}
	return start.i, end.i, nil
}

// handledLoopSignal reports whether a loop consumes a break or continue signal.
func handledLoopSignal(signal loopSignal, label string) bool {
	return signal.label == "" || signal.label == label
}

// evalMatchStmt executes the matching enum or union tag arm.
func (i *Interpreter) evalMatchStmt(stmt *ast.MatchStmt, env *Env) (Value, bool, error) {
	value, err := i.evalExpr(stmt.Value, env)
	if err != nil {
		return voidValue(), false, err
	}
	if value.kind == kindRef {
		value = value.ref.value
	}
	if value.kind != kindEnum && value.kind != kindUnion {
		return voidValue(), false, fmt.Errorf("runtime error: match expects enum or union")
	}
	for _, arm := range stmt.Arms {
		if arm.Tag == matchArmTag(value) {
			child := env.Child()
			if err := bindUnionPayload(value, arm, child); err != nil {
				return voidValue(), false, err
			}
			return i.evalStmt(arm.Body, child)
		}
	}
	return voidValue(), false, fmt.Errorf("runtime error: no match arm for `%s`", value.String())
}

// matchArmTag returns the active tag for a matchable runtime value.
func matchArmTag(value Value) string {
	if value.kind == kindEnum {
		return value.enum.tag
	}
	return value.union.tag
}

// bindUnionPayload binds a tagged union payload into a matching arm scope.
func bindUnionPayload(value Value, arm ast.MatchArm, env *Env) error {
	if value.kind != kindUnion || arm.Binding == "" {
		return nil
	}
	if value.union.payload == nil {
		return fmt.Errorf("runtime error: union variant `%s.%s` has no payload",
			value.union.typeName, value.union.tag)
	}
	return env.Define(arm.Binding, *value.union.payload, false)
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
	case *ast.DerefExpr:
		return i.evalDerefExpr(e, env)
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
	if ok {
		return value, nil
	}
	if name == "void" {
		return voidValue(), nil
	}
	return voidValue(), fmt.Errorf("runtime error: undefined binding `%s`", name)
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
			return voidValue(), fmt.Errorf("runtime error: unary - expects integer")
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
		return voidValue(), fmt.Errorf("runtime error: operator `%s` expects integers", expr.Operator)
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
		if value, ok, err := i.evalUnionConstructor(field, expr.Args, env); ok || err != nil {
			return value, err
		}
		return i.evalMethodCallExpr(field, expr.Args, env)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: callee must be a function name")
	}
	if fn, ok := i.functions[name.Name]; ok {
		return i.callFunctionExpr(fn, expr.Args, env)
	}
	args, err := i.evalArgs(expr.Args, env)
	if err != nil {
		return voidValue(), err
	}
	if name.Name == "print" {
		return i.callPrint(args)
	}
	if name.Name == "error" {
		return callError(args)
	}
	if name.Name == "Io" {
		return callIo(args)
	}
	if name.Name == "TaskGroup" {
		return callTaskGroup(args)
	}
	return i.callFunction(name.Name, args)
}

// evalTryExpr unwraps a successful !T value or propagates an error.
func (i *Interpreter) evalTryExpr(expr *ast.TryExpr, env *Env) (Value, error) {
	value, err := i.evalExpr(expr.Value, env)
	if err != nil {
		return voidValue(), err
	}
	if value.kind != kindErrorUnion {
		return value, nil
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
	return structValue(expr.TypeName, fields), nil
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
		if variants, exists := i.unions[ident.Name]; exists {
			payload, ok := variants[expr.Name]
			if !ok {
				return voidValue(), fmt.Errorf("runtime error: unknown union variant `%s.%s`",
					ident.Name, expr.Name)
			}
			if payload != "" {
				return voidValue(), fmt.Errorf("runtime error: union variant `%s.%s` expects payload",
					ident.Name, expr.Name)
			}
			return unionValue(ident.Name, expr.Name, nil), nil
		}
	}
	receiver, err := i.evalExpr(expr.Receiver, env)
	if err != nil {
		return voidValue(), err
	}
	if receiver.kind == kindRef {
		receiver = receiver.ref.value
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

// evalDerefExpr reads the value behind a local borrow reference.
func (i *Interpreter) evalDerefExpr(expr *ast.DerefExpr, env *Env) (Value, error) {
	value, err := i.evalExpr(expr.Receiver, env)
	if err != nil {
		return voidValue(), err
	}
	if value.kind != kindRef {
		return voidValue(), fmt.Errorf("runtime error: `%s` is not a borrow", value.String())
	}
	return value.ref.value, nil
}

// evalUnionConstructor evaluates Union.Variant(payload) construction.
func (i *Interpreter) evalUnionConstructor(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return voidValue(), false, nil
	}
	variants, exists := i.unions[ident.Name]
	if !exists {
		return voidValue(), false, nil
	}
	payloadType, exists := variants[field.Name]
	if !exists {
		return voidValue(), true, fmt.Errorf("runtime error: unknown union variant `%s.%s`",
			ident.Name, field.Name)
	}
	if payloadType == "" {
		return voidValue(), true, fmt.Errorf("runtime error: union variant `%s.%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return voidValue(), true, fmt.Errorf("runtime error: union variant `%s.%s` expects 1 arg",
			ident.Name, field.Name)
	}
	payload, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), true, err
	}
	return unionValue(ident.Name, field.Name, &payload), true, nil
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
	if receiver.kind == kindRef {
		receiver = receiver.ref.value
	}
	if receiver.kind != kindArena {
		if receiver.kind == kindTaskGroup {
			return i.evalTaskGroupMethod(field.Name, args, env)
		}
		if receiver.kind == kindTask {
			return evalTaskMethod(receiver, field.Name, args)
		}
		if receiver.kind == kindStruct {
			return i.evalImplMethod(receiver, field.Name, args, env)
		}
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

// evalTaskGroupMethod executes the v0.1 synchronous spawn model.
func (i *Interpreter) evalTaskGroupMethod(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if name != "spawn" {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 2 {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup.spawn expects io, function, and args")
	}
	ioValue, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup.spawn expects function name")
	}
	values, err := i.evalArgs(args[2:], env)
	if err != nil {
		return voidValue(), err
	}
	callArgs := append([]Value{ioValue}, values...)
	result, err := i.callFunction(target.Name, callArgs)
	if err != nil {
		return voidValue(), err
	}
	return taskValue(result), nil
}

// evalTaskMethod awaits or cancels a synchronous task value.
func evalTaskMethod(task Value, name string, args []ast.Expression) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: task.%s expected 0 args", name)
	}
	switch name {
	case "await":
		task.task.done = true
		return task.task.value, nil
	case "cancel":
		task.task.done = true
		return voidValue(), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: Task has no method `%s`", name)
	}
}

// evalImplMethod dispatches a concrete impl method with implicit self.
func (i *Interpreter) evalImplMethod(
	receiver Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	methods := i.impls[receiver.typeName]
	if methods == nil || methods[name] == nil {
		return voidValue(), fmt.Errorf("runtime error: `%s` has no method `%s`", receiver.typeName, name)
	}
	values, err := i.evalArgs(args, env)
	if err != nil {
		return voidValue(), err
	}
	callArgs := append([]Value{receiver}, values...)
	return i.callMethod(methods[name], callArgs)
}

// callMethod invokes an impl method declaration.
func (i *Interpreter) callMethod(fn *ast.FunctionDecl, args []Value) (Value, error) {
	if len(args) != len(fn.Params) {
		return voidValue(), fmt.Errorf("runtime error: `%s` expected %d args", fn.Name, len(fn.Params))
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

// callError constructs an error-union error value.
func callError(args []Value) (Value, error) {
	if len(args) != 1 || args[0].kind != kindString {
		return voidValue(), fmt.Errorf("runtime error: error expected []const u8")
	}
	return errorUnionValue(args[0].s), nil
}

// callIo constructs an explicit I/O capability value.
func callIo(args []Value) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: Io expected 0 args")
	}
	return ioValue(), nil
}

// callTaskGroup constructs a structured task group value.
func callTaskGroup(args []Value) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup expected 0 args")
	}
	return taskGroupValue(), nil
}
