package interp

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Interpreter executes a parsed Kizu program.
type Interpreter struct {
	out         io.Writer
	outMu       sync.Mutex
	functions   map[string]*ast.FunctionDecl
	impls       map[string]map[string]*ast.FunctionDecl
	enums       map[string]map[string]bool
	unions      map[string]map[string]string
	typeArgs    map[string]string
	processArgs []string
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
	return NewWithProcessArgs(out, nil)
}

// NewWithProcessArgs creates an interpreter with explicit process arguments.
func NewWithProcessArgs(out io.Writer, args []string) *Interpreter {
	return &Interpreter{
		out:         out,
		functions:   map[string]*ast.FunctionDecl{},
		impls:       map[string]map[string]*ast.FunctionDecl{},
		enums:       map[string]map[string]bool{},
		unions:      map[string]map[string]string{},
		typeArgs:    map[string]string{},
		processArgs: append([]string{}, args...),
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
	value, err := i.callFunction("main", nil)
	if err != nil {
		return err
	}
	if value.kind == kindErrorUnion {
		return fmt.Errorf("runtime error: %s", errorUnionMessage(value))
	}
	return nil
}

// errorUnionMessage extracts a readable message from an unhandled !T value.
func errorUnionMessage(value Value) string {
	if value.errUnion == nil {
		return "error"
	}
	if value.errUnion.payload != nil {
		return value.errUnion.payload.String()
	}
	return value.errUnion.message
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
		return wrapTypedErrorReturn(fn.ReturnType, result), err
	}
	return voidValue(), nil
}

// wrapTypedErrorReturn converts Error!T error payloads into propagated errors.
func wrapTypedErrorReturn(returnType string, value Value) Value {
	errorType, _, ok := errorUnionParts(returnType)
	if !ok || errorType == "" || value.kind != kindUnion {
		return value
	}
	if value.union.typeName != errorType {
		return value
	}
	return typedErrorUnionValue(value)
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
	if param.Comptime && param.TypeName == "Function" {
		target, ok := arg.(*ast.IdentExpr)
		if !ok {
			return voidValue(), fmt.Errorf("runtime error: Function argument must be a function name")
		}
		if _, ok := i.functions[target.Name]; !ok {
			return voidValue(), fmt.Errorf("runtime error: undefined function `%s`", target.Name)
		}
		return functionNameValue(target.Name), nil
	}
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
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		value, err := i.evalBorrowPrefix(borrow, env)
		if err != nil {
			return voidValue(), false, err
		}
		return voidValue(), false, env.Define(stmt.Name, value, stmt.Mutable)
	}
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
	case *ast.CallExpr:
		return i.assignCallTarget(expr, value, env)
	default:
		return fmt.Errorf("runtime error: invalid assignment target `%s`", target.String())
	}
}

// assignCallTarget writes through an assignable method result such as partition.at.
func (i *Interpreter) assignCallTarget(expr *ast.CallExpr, value Value, env *Env) error {
	target, err := i.evalExpr(expr, env)
	if err != nil {
		return err
	}
	if target.kind != kindPartitionSlot {
		return fmt.Errorf("runtime error: invalid assignment target `%s`", expr.String())
	}
	target.slot.partition.values[target.slot.index] = value
	return nil
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
	if target.ref.fieldParent != nil {
		target.ref.fieldParent.value.fields[target.ref.fieldName] = value
		target.ref.value = value
		return nil
	}
	if target.ref.arrayParent != nil {
		target.ref.arrayParent.values[target.ref.arrayIndex] = value
		target.ref.value = value
		return nil
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
	return i.evalRangeBounds(stmt.Start, stmt.End, env)
}

// evalRangeBounds evaluates and validates i64 range bounds.
func (i *Interpreter) evalRangeBounds(
	startExpr ast.Expression,
	endExpr ast.Expression,
	env *Env,
) (int64, int64, error) {
	start, err := i.evalExpr(startExpr, env)
	if err != nil {
		return 0, 0, err
	}
	end, err := i.evalExpr(endExpr, env)
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
		return fmt.Errorf("runtime error: union variant `%s::%s` has no payload",
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
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return evalLiteralExpr(e)
	case *ast.IfExpr:
		return i.evalIfExpr(e, env)
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
	case *ast.IndexExpr:
		return i.evalIndexExpr(e, env)
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

// evalIndexExpr evaluates checked one-dimensional byte indexing and slicing.
func (i *Interpreter) evalIndexExpr(expr *ast.IndexExpr, env *Env) (Value, error) {
	target, err := i.evalExpr(expr.Target, env)
	if err != nil {
		return voidValue(), err
	}
	if target.kind != kindString {
		return voidValue(), fmt.Errorf("runtime error: index/slice target expects []const u8")
	}
	if !expr.Slice {
		return i.evalByteIndex(target.s, expr.Index, env, true)
	}
	return i.evalByteSlice(target.s, expr, env, true)
}

// evalByteIndex returns one checked byte or a recoverable error-union payload.
func (i *Interpreter) evalByteIndex(
	bytes string,
	indexExpr ast.Expression,
	env *Env,
	trap bool,
) (Value, error) {
	index, err := i.evalIndexBound("index", indexExpr, env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index >= int64(len(bytes)) {
		if trap {
			return voidValue(), fmt.Errorf("runtime error: index out of bounds")
		}
		return errorUnionValue("index out of bounds"), nil
	}
	return intValue(int64(bytes[int(index)])), nil
}

// evalByteSlice returns one checked sub-slice or a recoverable error-union payload.
func (i *Interpreter) evalByteSlice(
	bytes string,
	expr *ast.IndexExpr,
	env *Env,
	trap bool,
) (Value, error) {
	start, end := int64(0), int64(len(bytes))
	if expr.Start != nil {
		value, err := i.evalIndexBound("slice start", expr.Start, env)
		if err != nil {
			return voidValue(), err
		}
		start = value
	}
	if expr.End != nil {
		value, err := i.evalIndexBound("slice end", expr.End, env)
		if err != nil {
			return voidValue(), err
		}
		end = value
	}
	if start < 0 || end < start || end > int64(len(bytes)) {
		if trap {
			return voidValue(), fmt.Errorf("runtime error: slice range out of bounds")
		}
		return errorUnionValue("slice range out of bounds"), nil
	}
	return stringValue(bytes[int(start):int(end)]), nil
}

// evalIndexBound evaluates one i64 index or slice bound.
func (i *Interpreter) evalIndexBound(name string, expr ast.Expression, env *Env) (int64, error) {
	if expr == nil {
		return 0, fmt.Errorf("runtime error: %s is missing", name)
	}
	value, err := i.evalExpr(expr, env)
	if err != nil {
		return 0, err
	}
	if value.kind != kindInt {
		return 0, fmt.Errorf("runtime error: %s expects i64", name)
	}
	return value.i, nil
}

// evalLiteralExpr evaluates scalar literal expressions.
func evalLiteralExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return parseInt(e.Value)
	case *ast.StringExpr:
		return stringValue(e.Value), nil
	case *ast.BoolExpr:
		return boolValue(e.Value), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: unsupported literal %T", expr)
	}
}

// evalIfExpr evaluates the selected branch and returns its final expression value.
func (i *Interpreter) evalIfExpr(expr *ast.IfExpr, env *Env) (Value, error) {
	cond, err := i.evalExpr(expr.Condition, env)
	if err != nil {
		return voidValue(), err
	}
	if cond.kind != kindBool {
		return voidValue(), fmt.Errorf("runtime error: if expression condition must be bool")
	}
	branch := expr.Consequence
	if !cond.b {
		branch = expr.Alternative
	}
	return i.evalIfExprBlock(branch, env.Child())
}

// evalIfExprBlock executes statements before the final branch value.
func (i *Interpreter) evalIfExprBlock(block *ast.BlockStmt, env *Env) (Value, error) {
	if block == nil || len(block.Statements) == 0 {
		return voidValue(), fmt.Errorf("runtime error: if expression branch has no value")
	}
	last := len(block.Statements) - 1
	for _, stmt := range block.Statements[:last] {
		result, returned, err := i.evalStmt(stmt, env)
		if err != nil || returned {
			return result, err
		}
	}
	exprStmt, ok := block.Statements[last].(*ast.ExprStmt)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: if expression branch has no value")
	}
	return i.evalExpr(exprStmt.Expr, env)
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
	if expr.Operator == "&" || expr.Operator == "&mut" {
		return i.evalBorrowPrefix(expr, env)
	}
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

// evalBorrowPrefix creates a local runtime borrow reference.
func (i *Interpreter) evalBorrowPrefix(expr *ast.PrefixExpr, env *Env) (Value, error) {
	switch target := expr.Right.(type) {
	case *ast.IdentExpr:
		binding, ok := env.Binding(target.Name)
		if !ok {
			return voidValue(), fmt.Errorf("runtime error: undefined binding `%s`", target.Name)
		}
		return refValue(binding), nil
	case *ast.FieldExpr:
		ident, ok := target.Receiver.(*ast.IdentExpr)
		if !ok {
			return voidValue(), fmt.Errorf("runtime error: v0.1 field borrow only supports one direct field")
		}
		ownerBinding, ok := env.Binding(ident.Name)
		if !ok {
			return voidValue(), fmt.Errorf("runtime error: undefined binding `%s`", ident.Name)
		}
		if ownerBinding.value.kind != kindStruct {
			return voidValue(), fmt.Errorf("runtime error: field borrow expects struct")
		}
		fieldValue, ok := ownerBinding.value.fields[target.Name]
		if !ok {
			return voidValue(), fmt.Errorf("runtime error: unknown field `%s`", target.Name)
		}
		cell := &binding{
			value: fieldValue, mutable: expr.Operator == "&mut",
			fieldParent: ownerBinding, fieldName: target.Name,
		}
		return refValue(cell), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: borrow target must be local")
	}
}

// evalBinaryExpr evaluates arithmetic, logical, equality, and comparison operators.
func (i *Interpreter) evalBinaryExpr(expr *ast.BinaryExpr, env *Env) (Value, error) {
	if expr.Operator == "and" || expr.Operator == "or" {
		return i.evalLogicalExpr(expr, env)
	}
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

// evalLogicalExpr evaluates short-circuit boolean operators.
func (i *Interpreter) evalLogicalExpr(expr *ast.BinaryExpr, env *Env) (Value, error) {
	left, err := i.evalExpr(expr.Left, env)
	if err != nil {
		return voidValue(), err
	}
	if left.kind != kindBool {
		return voidValue(), fmt.Errorf("runtime error: operator `%s` expects bools", expr.Operator)
	}
	if expr.Operator == "and" && !left.b {
		return boolValue(false), nil
	}
	if expr.Operator == "or" && left.b {
		return boolValue(true), nil
	}
	right, err := i.evalExpr(expr.Right, env)
	if err != nil {
		return voidValue(), err
	}
	if right.kind != kindBool {
		return voidValue(), fmt.Errorf("runtime error: operator `%s` expects bools", expr.Operator)
	}
	return boolValue(right.b), nil
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
		return i.evalFieldCallExpr(field, expr.Args, env)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return i.evalTypeApplyCallExpr(typeApply, expr.Args, env)
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

// evalFieldCallExpr evaluates qualified, union, and method calls.
func (i *Interpreter) evalFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if value, ok, err := i.evalUnionConstructor(field, args, env); ok || err != nil {
		return value, err
	}
	if value, ok, err := i.evalQualifiedUserCall(field, args, env); ok || err != nil {
		return value, err
	}
	if value, ok, err := i.evalQualifiedBuiltin(field, args, env); ok || err != nil {
		return value, err
	}
	return i.evalMethodCallExpr(field, args, env)
}

// evalQualifiedUserCall evaluates source-loaded qualified functions.
func (i *Interpreter) evalQualifiedUserCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return voidValue(), false, nil
	}
	fn, ok := i.functions[name]
	if !ok {
		return voidValue(), false, nil
	}
	value, err := i.callFunctionExpr(fn, args, env)
	return value, true, err
}

// evalQualifiedBuiltin evaluates std:: namespace prototype calls without a module system.
func (i *Interpreter) evalQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return voidValue(), false, nil
	}
	if value, ok, err := i.evalQualifiedCoreBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	return i.evalQualifiedRuntimeBuiltin(name, args, env)
}

// evalQualifiedCoreBuiltin evaluates pure, fs, I/O, and process std calls.
func (i *Interpreter) evalQualifiedCoreBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	if value, ok := evalIoBuiltin(name, args); ok {
		return value, true, nil
	}
	if value, ok, err := i.evalFsBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	if value, ok, err := i.evalMemBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	if value, ok, err := i.evalIoHelperBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	if value, ok, err := i.evalProcessBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	return voidValue(), false, nil
}

// evalQualifiedRuntimeBuiltin evaluates constructors, tasks, and misc calls.
func (i *Interpreter) evalQualifiedRuntimeBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	if value, ok, err := i.evalTaskBuiltin(name, args, env); ok || err != nil {
		return value, ok, err
	}
	return i.evalMiscQualifiedBuiltin(name)
}

// evalMiscQualifiedBuiltin evaluates remaining qualified std constructor stubs.
func (i *Interpreter) evalMiscQualifiedBuiltin(
	name string,
) (Value, bool, error) {
	switch name {
	case "std.channel.Channel":
		return errorUnionValue("use std::channel::Channel<T>()"), true, nil
	case "std.atomic.Atomic":
		return errorUnionValue("use std::atomic::Atomic<T>(value)"), true, nil
	case "std.atomic.AtomicI64":
		return errorUnionValue("use std::atomic::Atomic<i64>(value)"), true, nil
	case "std.sync.Mutex":
		return errorUnionValue("use std::sync::Mutex<T>(value)"), true, nil
	default:
		return voidValue(), false, nil
	}
}

// evalMemBuiltin evaluates allocation-free std::mem byte-slice helpers.
func (i *Interpreter) evalMemBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.mem_page_allocator":
		return callAllocatorFromExprs(args), true, nil
	case "std.builtin.mem_len":
		value, err := i.evalMemLen(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalMemLen returns the byte length of a read-only byte slice.
func (i *Interpreter) evalMemLen(args []ast.Expression, env *Env) (Value, error) {
	bytes, err := i.evalMemOneBytes("std::mem::len", args, env)
	if err != nil {
		return voidValue(), err
	}
	return intValue(int64(len(bytes))), nil
}

// evalMemOneBytes evaluates one []const u8 argument.
func (i *Interpreter) evalMemOneBytes(
	name string,
	args []ast.Expression,
	env *Env,
) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("runtime error: %s expects 1 arg", name)
	}
	bytes, err := i.evalExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if bytes.kind != kindString {
		return "", fmt.Errorf("runtime error: %s expects []const u8", name)
	}
	return bytes.s, nil
}

// evalFsBuiltin evaluates filesystem host primitives with explicit Io.
func (i *Interpreter) evalFsBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.fs_read_file":
		value, err := i.evalFsReadFile(args, env)
		return value, true, err
	case "std.builtin.fs_write_file":
		value, err := i.evalFsWriteFile(args, env)
		return value, true, err
	case "std.builtin.fs_exists":
		value, err := i.evalFsExists(args, env)
		return value, true, err
	case "std.builtin.fs_metadata":
		value, err := i.evalFsMetadata(args, env)
		return value, true, err
	case "std.builtin.fs_create_dir":
		value, err := i.evalFsCreateDir(args, env)
		return value, true, err
	case "std.builtin.fs_remove_dir":
		value, err := i.evalFsRemoveDir(args, env)
		return value, true, err
	case "std.builtin.fs_remove_file":
		value, err := i.evalFsRemoveFile(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalIoHelperBuiltin evaluates explicit-Io stdio helpers.
func (i *Interpreter) evalIoHelperBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.io_write_stdout":
		value, err := i.evalIoWrite(args, env, i.out)
		return value, true, err
	case "std.builtin.io_write_stderr":
		value, err := i.evalIoWrite(args, env, os.Stderr)
		return value, true, err
	case "std.builtin.io_read_stdin":
		value, err := i.evalIoReadStdin(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalProcessBuiltin evaluates minimal process helpers.
func (i *Interpreter) evalProcessBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.process_arg_count":
		if len(args) != 0 {
			return voidValue(), true, fmt.Errorf("runtime error: std::process::arg_count expects 0 args")
		}
		return intValue(int64(len(i.processArgs))), true, nil
	case "std.builtin.process_arg":
		value, err := i.evalProcessArg(args, env)
		return value, true, err
	case "std.builtin.process_env":
		value, err := i.evalProcessEnv(args, env)
		return value, true, err
	case "std.builtin.process_exit_code":
		value, err := i.evalProcessExitCode(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalTaskBuiltin evaluates structured task and data-parallel std functions.
func (i *Interpreter) evalTaskBuiltin(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.task_group":
		value, err := i.evalTaskGroup(args, env)
		return value, true, err
	case "std.builtin.task_queue":
		return callQueueFromExprs(args), true, nil
	case "std.builtin.task_partition_mut":
		value, err := i.evalPartitionMut(args, env)
		return value, true, err
	case "std.builtin.task_local_buffer":
		value, err := i.evalLocalBuffer(args, env)
		return value, true, err
	case "std.builtin.task_parallel_for":
		value, err := i.evalParallelFor(args, env)
		return value, true, err
	case "std.builtin.task_parallel_map":
		value, err := i.evalParallelMap(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalFsReadFile reads a file using an explicit Io capability.
func (i *Interpreter) evalFsReadFile(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 2 {
		return errorUnionValue("std::fs::read_file expected io and path"), nil
	}
	ioValue, path, err := i.evalFsIoPath(args, env, "std::fs::read_file")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return stringValue(string(data)), nil
}

// evalFsWriteFile writes a file using an explicit Io capability.
func (i *Interpreter) evalFsWriteFile(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 3 {
		return errorUnionValue("std::fs::write_file expected io, path, and bytes"), nil
	}
	ioValue, path, err := i.evalFsIoPath(args, env, "std::fs::write_file")
	if err != nil {
		return voidValue(), err
	}
	bytes, err := i.evalExpr(args[2], env)
	if err != nil {
		return voidValue(), err
	}
	bytes = unwrapRefValue(bytes)
	if bytes.kind != kindString {
		return errorUnionValue("std::fs::write_file expected []const u8 bytes"), nil
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	if err := os.WriteFile(path, []byte(bytes.s), 0o644); err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return voidValue(), nil
}

// evalFsExists reports whether a path exists using an explicit Io capability.
func (i *Interpreter) evalFsExists(args []ast.Expression, env *Env) (Value, error) {
	ioValue, target, err := i.evalFsIoPath(args, env, "std::fs::exists")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	_, statErr := os.Stat(target)
	if statErr == nil {
		return boolValue(true), nil
	}
	if os.IsNotExist(statErr) {
		return boolValue(false), nil
	}
	return errorUnionValue(statErr.Error()), nil
}

// evalFsMetadata returns minimal metadata for a filesystem path.
func (i *Interpreter) evalFsMetadata(args []ast.Expression, env *Env) (Value, error) {
	ioValue, target, err := i.evalFsIoPath(args, env, "std::fs::metadata")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return structValue("std::fs::Metadata", map[string]Value{
		"size":   intValue(info.Size()),
		"is_dir": boolValue(info.IsDir()),
	}), nil
}

// evalFsCreateDir creates a directory and reports I/O failures as !void errors.
func (i *Interpreter) evalFsCreateDir(args []ast.Expression, env *Env) (Value, error) {
	ioValue, target, err := i.evalFsIoPath(args, env, "std::fs::create_dir")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return voidValue(), nil
}

// evalFsRemoveDir removes one empty directory.
func (i *Interpreter) evalFsRemoveDir(args []ast.Expression, env *Env) (Value, error) {
	return i.evalFsRemove(args, env, "std::fs::remove_dir")
}

// evalFsRemoveFile removes one file.
func (i *Interpreter) evalFsRemoveFile(args []ast.Expression, env *Env) (Value, error) {
	return i.evalFsRemove(args, env, "std::fs::remove_file")
}

// evalFsRemove removes a filesystem path using an explicit Io capability.
func (i *Interpreter) evalFsRemove(args []ast.Expression, env *Env, name string) (Value, error) {
	ioValue, target, err := i.evalFsIoPath(args, env, name)
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	if err := os.Remove(target); err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return voidValue(), nil
}

// evalPathArg evaluates one []const u8 path helper argument.
func (i *Interpreter) evalPathArg(expr ast.Expression, env *Env, name string) (string, error) {
	value, err := i.evalExpr(expr, env)
	if err != nil {
		return "", err
	}
	if value.kind != kindString {
		return "", fmt.Errorf("runtime error: %s expects []const u8", name)
	}
	return value.s, nil
}

// evalIoWrite writes bytes to stdout or stderr through an explicit Io capability.
func (i *Interpreter) evalIoWrite(
	args []ast.Expression,
	env *Env,
	out io.Writer,
) (Value, error) {
	if len(args) != 2 {
		return errorUnionValue("std::io write expected io and bytes"), nil
	}
	ioValue, bytes, err := i.evalIoBytes(args, env, "std::io write")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	_, err = fmt.Fprint(out, bytes)
	if err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return voidValue(), nil
}

// evalIoReadStdin reads all stdin through an explicit Io capability.
func (i *Interpreter) evalIoReadStdin(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return errorUnionValue("std::io::read_stdin expected io"), nil
	}
	ioValue, err := i.evalIoArg(args[0], env, "std::io::read_stdin")
	if err != nil {
		return voidValue(), err
	}
	if failure, ok := failingIoError(ioValue); ok {
		return failure, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return errorUnionValue(err.Error()), nil
	}
	return stringValue(string(data)), nil
}

// evalIoBytes evaluates common Io plus byte-slice arguments.
func (i *Interpreter) evalIoBytes(
	args []ast.Expression,
	env *Env,
	name string,
) (Value, string, error) {
	ioValue, err := i.evalIoArg(args[0], env, name)
	if err != nil {
		return voidValue(), "", err
	}
	bytes, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), "", err
	}
	if bytes.kind != kindString {
		return voidValue(), "", fmt.Errorf("runtime error: %s expects []const u8 bytes", name)
	}
	return ioValue, bytes.s, nil
}

// evalIoArg evaluates and checks one explicit Io argument.
func (i *Interpreter) evalIoArg(expr ast.Expression, env *Env, name string) (Value, error) {
	ioValue, err := i.evalExpr(expr, env)
	if err != nil {
		return voidValue(), err
	}
	if ioValue.kind != kindIo {
		return voidValue(), fmt.Errorf("runtime error: %s expects Io", name)
	}
	return ioValue, nil
}

// evalProcessArg reads one process argument supplied by the runner.
func (i *Interpreter) evalProcessArg(args []ast.Expression, env *Env) (Value, error) {
	index, err := i.evalProcessIndex("std::process::arg", args, env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index >= len(i.processArgs) {
		return errorUnionValue("process arg index out of bounds"), nil
	}
	return stringValue(i.processArgs[index]), nil
}

// evalProcessEnv reads one environment variable by name.
func (i *Interpreter) evalProcessEnv(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return errorUnionValue("std::process::env expected name"), nil
	}
	name, err := i.evalPathArg(args[0], env, "std::process::env")
	if err != nil {
		return voidValue(), err
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return errorUnionValue("environment variable not found"), nil
	}
	return stringValue(value), nil
}

// evalProcessExitCode validates and returns a process exit code value.
func (i *Interpreter) evalProcessExitCode(args []ast.Expression, env *Env) (Value, error) {
	index, err := i.evalProcessIndex("std::process::exit_code", args, env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index > 255 {
		return voidValue(), fmt.Errorf("runtime error: exit code must be between 0 and 255")
	}
	return intValue(int64(index)), nil
}

// evalProcessIndex evaluates one i64 process helper argument.
func (i *Interpreter) evalProcessIndex(name string, args []ast.Expression, env *Env) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("runtime error: %s expects i64", name)
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return 0, err
	}
	if value.kind != kindInt {
		return 0, fmt.Errorf("runtime error: %s expects i64", name)
	}
	return int(value.i), nil
}

// evalFsIoPath evaluates the common Io and path arguments for std::fs calls.
func (i *Interpreter) evalFsIoPath(
	args []ast.Expression,
	env *Env,
	name string,
) (Value, string, error) {
	if len(args) < 2 {
		return voidValue(), "", fmt.Errorf("runtime error: %s expects io and path", name)
	}
	ioValue, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), "", err
	}
	if ioValue.kind != kindIo {
		return voidValue(), "", fmt.Errorf("runtime error: %s expects Io", name)
	}
	path, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), "", err
	}
	path = unwrapRefValue(path)
	if path.kind != kindString {
		return voidValue(), "", fmt.Errorf("runtime error: %s expects []const u8 path", name)
	}
	return ioValue, path.s, nil
}

// failingIoError converts deterministic failing Io into a runtime error-union value.
func failingIoError(ioValue Value) (Value, bool) {
	if ioValue.typeName != "failing" {
		return voidValue(), false
	}
	return errorUnionValue("io runtime is failing"), true
}

// evalIoBuiltin evaluates std::io constructor calls.
func evalIoBuiltin(name string, args []ast.Expression) (Value, bool) {
	switch name {
	case "std.builtin.io_blocking":
		return callIoFromExprs("blocking", args), true
	case "std.builtin.io_threaded":
		return callIoFromExprs("threaded", args), true
	case "std.builtin.io_failing":
		return callIoFromExprs("failing", args), true
	case "std.io.evented", "std.builtin.io_evented":
		return errorUnionValue("std::io::evented is not implemented in v0.1"), true
	default:
		return voidValue(), false
	}
}

// evalTypeApplyCallExpr evaluates typed std constructor calls.
func (i *Interpreter) evalTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: unsupported type application `%s`", expr.String())
	}
	if value, ok, err := i.evalBuiltinTypeApply(name, expr.TypeArg, args, env); ok || err != nil {
		return value, err
	}
	if value, ok, err := i.evalGenericUserTypeApply(name, expr.TypeArg, args, env); ok || err != nil {
		return value, err
	}
	return voidValue(), fmt.Errorf("runtime error: `%s` does not take a type argument", name)
}

// evalGenericUserTypeApply invokes source-defined std generic wrappers.
func (i *Interpreter) evalGenericUserTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	fn := i.functions[name]
	if fn == nil || len(fn.TypeParams) == 0 {
		return voidValue(), false, nil
	}
	typeArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(typeArgs) != len(fn.TypeParams) {
		return voidValue(), true, fmt.Errorf(
			"runtime error: `%s` expects %d type arguments", name, len(fn.TypeParams),
		)
	}
	value, err := i.callTypeApplyFunction(fn, typeArgs, args, env)
	return value, true, err
}

// evalBuiltinTypeApply evaluates std-only generic runtime primitives.
func (i *Interpreter) evalBuiltinTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	switch name {
	case "std.builtin.channel":
		return callChannelFromExprs(i.resolveTypeArg(typeArg), args), true, nil
	case "std.builtin.channel_send", "std.builtin.channel_recv":
		value, err := i.evalChannelPrimitive(name, i.resolveTypeArg(typeArg), args, env)
		return value, true, err
	case "std.builtin.atomic":
		value, err := i.evalAtomic(i.resolveTypeArg(typeArg), args, env)
		return value, true, err
	case "std.builtin.atomic_load", "std.builtin.atomic_store":
		value, err := i.evalAtomicPrimitive(name, args, env)
		return value, true, err
	case "std.builtin.mutex":
		value, err := i.evalMutex(i.resolveTypeArg(typeArg), args, env)
		return value, true, err
	case "std.builtin.mutex_get":
		value, err := i.evalMutexPrimitive(args, env)
		return value, true, err
	case "std.builtin.array":
		value, err := i.evalArrayConstructor(i.resolveTypeArg(typeArg), args, env)
		return value, true, err
	case "std.builtin.array_append", "std.builtin.array_len", "std.builtin.array_capacity",
		"std.builtin.array_get", "std.builtin.array_at", "std.builtin.array_at_mut",
		"std.builtin.array_set", "std.builtin.array_deinit":
		value, err := i.evalArrayPrimitive(name, args, env)
		return value, true, err
	case "std.builtin.map":
		value, err := i.evalMapConstructor(i.resolveTypeArg(typeArg), args, env)
		return value, true, err
	case "std.builtin.map_insert", "std.builtin.map_get", "std.builtin.map_contains",
		"std.builtin.map_len", "std.builtin.map_deinit":
		value, err := i.evalMapPrimitive(name, args, env)
		return value, true, err
	case "std.builtin.thread_scoped":
		value, err := i.evalThreadScopedTyped(args, env)
		return value, true, err
	default:
		return voidValue(), false, nil
	}
}

// evalChannelPrimitive executes reserved Channel method primitives.
func (i *Interpreter) evalChannelPrimitive(
	name string,
	_ string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	receiver, rest, err := i.evalPrimitiveReceiver(name, args, env)
	if err != nil {
		return voidValue(), err
	}
	return i.evalChannelRuntimeMethod(receiver, strings.TrimPrefix(name, "std.builtin.channel_"),
		rest, env)
}

// evalAtomicPrimitive executes reserved Atomic method primitives.
func (i *Interpreter) evalAtomicPrimitive(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	receiver, rest, err := i.evalPrimitiveReceiver(name, args, env)
	if err != nil {
		return voidValue(), err
	}
	return i.evalAtomicRuntimeMethod(receiver, strings.TrimPrefix(name, "std.builtin.atomic_"),
		rest, env)
}

// evalMutexPrimitive executes reserved Mutex method primitives.
func (i *Interpreter) evalMutexPrimitive(args []ast.Expression, env *Env) (Value, error) {
	receiver, rest, err := i.evalPrimitiveReceiver("std.builtin.mutex_get", args, env)
	if err != nil {
		return voidValue(), err
	}
	return i.evalMutexRuntimeMethod(receiver, "get", rest, env)
}

// evalArrayPrimitive executes reserved Array method primitives.
func (i *Interpreter) evalArrayPrimitive(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	receiver, rest, err := i.evalPrimitiveReceiver(name, args, env)
	if err != nil {
		return voidValue(), err
	}
	return i.evalArrayRuntimeMethod(receiver, strings.TrimPrefix(name, "std.builtin.array_"),
		rest, env)
}

// evalMapPrimitive executes reserved Map method primitives.
func (i *Interpreter) evalMapPrimitive(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	receiver, rest, err := i.evalPrimitiveReceiver(name, args, env)
	if err != nil {
		return voidValue(), err
	}
	return i.evalMapRuntimeMethod(receiver, strings.TrimPrefix(name, "std.builtin.map_"), rest, env)
}

// evalPrimitiveReceiver evaluates the explicit receiver for a std primitive.
func (i *Interpreter) evalPrimitiveReceiver(
	name string,
	args []ast.Expression,
	env *Env,
) (Value, []ast.Expression, error) {
	if len(args) == 0 {
		return voidValue(), nil, fmt.Errorf("runtime error: %s expects receiver", name)
	}
	receiver, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), nil, err
	}
	return unwrapRefValue(receiver), args[1:], nil
}

// callTypeApplyFunction invokes a generic std wrapper.
func (i *Interpreter) callTypeApplyFunction(
	fn *ast.FunctionDecl,
	typeArgs []string,
	args []ast.Expression,
	caller *Env,
) (Value, error) {
	previous := i.typeArgs
	i.typeArgs = map[string]string{}
	for idx, param := range fn.TypeParams {
		i.typeArgs[param] = typeArgs[idx]
	}
	defer func() { i.typeArgs = previous }()
	return i.callFunctionExpr(fn, args, caller)
}

// evalStdMethodWrapper invokes a Kizu std method wrapper with an explicit receiver.
func (i *Interpreter) evalStdMethodWrapper(
	name string,
	typeArgs []string,
	receiver Value,
	args []ast.Expression,
	env *Env,
) (Value, bool, error) {
	fn := i.functions[name]
	if fn == nil {
		return voidValue(), false, nil
	}
	values, err := i.evalArgs(args, env)
	if err != nil {
		return voidValue(), true, err
	}
	callArgs := append([]Value{receiver}, values...)
	previous := i.typeArgs
	i.typeArgs = map[string]string{}
	for idx, param := range fn.TypeParams {
		if idx < len(typeArgs) {
			i.typeArgs[param] = typeArgs[idx]
		}
	}
	defer func() { i.typeArgs = previous }()
	value, err := i.callFunction(name, callArgs)
	return value, true, err
}

// resolveTypeArg maps a wrapper type parameter to the caller-provided type.
func (i *Interpreter) resolveTypeArg(typeArg string) string {
	if replacement, ok := i.typeArgs[typeArg]; ok {
		return replacement
	}
	args, ok := splitGenericArgs(typeArg)
	if ok && len(args) > 1 {
		for idx, arg := range args {
			args[idx] = i.resolveTypeArg(arg)
		}
		return strings.Join(args, ", ")
	}
	return typeArg
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
	if expr.Namespace {
		return i.evalNamespaceExpr(expr)
	}
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if _, exists := i.enums[ident.Name]; exists {
			return voidValue(), fmt.Errorf("runtime error: enum tag `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := i.unions[ident.Name]; exists {
			return voidValue(), fmt.Errorf("runtime error: union variant `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
	}
	receiver, err := i.evalExpr(expr.Receiver, env)
	if err != nil {
		return voidValue(), err
	}
	receiver = unwrapRefValue(receiver)
	if receiver.kind != kindStruct {
		return voidValue(), fmt.Errorf("runtime error: field access expects struct")
	}
	value, ok := receiver.fields[expr.Name]
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: unknown field `%s`", expr.Name)
	}
	return value, nil
}

// unwrapRefValue follows local borrow references to the current stored value.
func unwrapRefValue(value Value) Value {
	for value.kind == kindRef {
		value = value.ref.value
	}
	return value
}

// evalNamespaceExpr evaluates enum and payload-free union namespace lookup.
func (i *Interpreter) evalNamespaceExpr(expr *ast.FieldExpr) (Value, error) {
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: invalid namespace lookup `%s`", expr.String())
	}
	if tags, exists := i.enums[ident.Name]; exists {
		if !tags[expr.Name] {
			return voidValue(), fmt.Errorf("runtime error: unknown enum tag `%s::%s`",
				ident.Name, expr.Name)
		}
		return enumValue(ident.Name, expr.Name), nil
	}
	if variants, exists := i.unions[ident.Name]; exists {
		payload, exists := variants[expr.Name]
		if !exists {
			return voidValue(), fmt.Errorf("runtime error: unknown union variant `%s::%s`",
				ident.Name, expr.Name)
		}
		if payload != "" {
			return voidValue(), fmt.Errorf("runtime error: union variant `%s::%s` expects payload",
				ident.Name, expr.Name)
		}
		return unionValue(ident.Name, expr.Name, nil), nil
	}
	return voidValue(), fmt.Errorf("runtime error: unknown namespace `%s`", ident.Name)
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
	if !field.Namespace {
		if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
			if _, exists := i.enums[ident.Name]; exists {
				return voidValue(), true, fmt.Errorf("runtime error: enum tag `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
			if _, exists := i.unions[ident.Name]; exists {
				return voidValue(), true, fmt.Errorf("runtime error: union variant `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
		}
		return voidValue(), false, nil
	}
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
		return voidValue(), true, fmt.Errorf("runtime error: unknown union variant `%s::%s`",
			ident.Name, field.Name)
	}
	if payloadType == "" {
		return voidValue(), true, fmt.Errorf("runtime error: union variant `%s::%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return voidValue(), true, fmt.Errorf("runtime error: union variant `%s::%s` expects 1 arg",
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
	receiver = unwrapRefValue(receiver)
	if receiver.kind != kindArena {
		return i.evalNonArenaMethod(receiver, field.Name, args, env)
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

// evalNonArenaMethod dispatches methods for non-arena runtime values.
func (i *Interpreter) evalNonArenaMethod(
	receiver Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	switch receiver.kind {
	case kindTaskGroup:
		return i.evalTaskGroupMethod(receiver, name, args, env)
	case kindTask:
		return evalTaskMethod(receiver, name, args)
	case kindQueue:
		return i.evalQueueMethod(receiver, name, args, env)
	case kindChannel:
		return i.evalChannelMethod(receiver, name, args, env)
	case kindPartition:
		return i.evalPartitionMethod(receiver, name, args, env)
	case kindLocalBuffer:
		return i.evalLocalBufferMethod(receiver, name, args, env)
	case kindAtomic:
		return i.evalAtomicMethod(receiver, name, args, env)
	case kindMutex:
		return i.evalMutexMethod(receiver, name, args, env)
	case kindArray:
		return i.evalArrayMethod(receiver, name, args, env)
	case kindMap:
		return i.evalMapMethod(receiver, name, args, env)
	case kindStruct:
		return i.evalImplMethod(receiver, name, args, env)
	default:
		return voidValue(), fmt.Errorf("runtime error: method `%s` expects arena", name)
	}
}

// evalQueueMethod executes deterministic deferred queue operations.
func (i *Interpreter) evalQueueMethod(
	queue Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	switch name {
	case "enqueue":
		return i.evalQueueEnqueue(queue, args, env)
	case "drain":
		return i.evalQueueDrain(queue, args)
	default:
		return voidValue(), fmt.Errorf("runtime error: Queue has no method `%s`", name)
	}
}

// evalQueueEnqueue captures a function call for later deterministic drain.
func (i *Interpreter) evalQueueEnqueue(
	queue Value,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if len(args) < 2 {
		return voidValue(), fmt.Errorf("runtime error: queue.enqueue expects io and function")
	}
	ioValue, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: queue.enqueue expects function name")
	}
	values, err := i.evalArgs(args[2:], env)
	if err != nil {
		return voidValue(), err
	}
	callArgs := append([]Value{ioValue}, values...)
	queue.queue.jobs = append(queue.queue.jobs, QueuedJob{name: target.Name, args: callArgs})
	return voidValue(), nil
}

// evalQueueDrain runs queued jobs in FIFO order before returning.
func (i *Interpreter) evalQueueDrain(queue Value, args []ast.Expression) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: queue.drain expects 0 args")
	}
	for len(queue.queue.jobs) > 0 {
		job := queue.queue.jobs[0]
		queue.queue.jobs = queue.queue.jobs[1:]
		if _, err := i.callFunction(job.name, job.args); err != nil {
			return voidValue(), err
		}
	}
	return voidValue(), nil
}

// evalPartitionMut constructs v0.1 disjoint output slots.
func (i *Interpreter) evalPartitionMut(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 2 {
		return voidValue(), fmt.Errorf("runtime error: std::task::partition_mut expects 2 args")
	}
	init, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	count, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), err
	}
	if count.kind != kindInt {
		return voidValue(), fmt.Errorf("runtime error: partition count expects i64")
	}
	return partitionValue(init, count.i), nil
}

// evalLocalBuffer constructs worker-local scratch slots.
func (i *Interpreter) evalLocalBuffer(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 2 {
		return voidValue(), fmt.Errorf("runtime error: std::task::LocalBuffer expects 2 args")
	}
	count, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	init, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), err
	}
	if count.kind != kindInt {
		return voidValue(), fmt.Errorf("runtime error: LocalBuffer count expects i64")
	}
	return localBufferValue(count.i, init), nil
}

// evalParallelFor runs worker(i) sequentially in the v0.1 interpreter.
func (i *Interpreter) evalParallelFor(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 4 {
		return voidValue(), fmt.Errorf("runtime error: std::task::parallel_for expects 4 args")
	}
	if _, err := i.evalExpr(args[0], env); err != nil {
		return voidValue(), err
	}
	start, end, err := i.evalRangeBounds(args[1], args[2], env)
	if err != nil {
		return voidValue(), err
	}
	worker, ok, err := i.evalFunctionNameArg(args[3], env)
	if err != nil {
		return voidValue(), err
	}
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: parallel_for expects function name")
	}
	for idx := start; idx < end; idx++ {
		value, err := i.callFunction(worker, []Value{intValue(idx)})
		if err != nil {
			return voidValue(), err
		}
		if value.kind == kindErrorUnion {
			return value, nil
		}
	}
	return voidValue(), nil
}

// evalParallelMap writes worker(i) into partition slot i in the sequential model.
func (i *Interpreter) evalParallelMap(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 5 {
		return voidValue(), fmt.Errorf("runtime error: std::task::parallel_map expects 5 args")
	}
	if _, err := i.evalExpr(args[0], env); err != nil {
		return voidValue(), err
	}
	partition, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), err
	}
	partition = unwrapRefValue(partition)
	if partition.kind != kindPartition {
		return voidValue(), fmt.Errorf("runtime error: parallel_map expects Partition")
	}
	start, end, err := i.evalRangeBounds(args[2], args[3], env)
	if err != nil {
		return voidValue(), err
	}
	worker, ok, err := i.evalFunctionNameArg(args[4], env)
	if err != nil {
		return voidValue(), err
	}
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: parallel_map expects function name")
	}
	return i.fillPartition(partition.partition, start, end, worker)
}

// evalFunctionNameArg resolves direct function names and forwarded Function parameters.
func (i *Interpreter) evalFunctionNameArg(arg ast.Expression, env *Env) (string, bool, error) {
	target, ok := arg.(*ast.IdentExpr)
	if !ok {
		return "", false, nil
	}
	value, err := i.evalExpr(arg, env)
	if err == nil && value.kind == kindFunctionName {
		return value.s, true, nil
	}
	if _, ok := i.functions[target.Name]; ok {
		return target.Name, true, nil
	}
	if err != nil {
		return "", true, err
	}
	return "", false, nil
}

// fillPartition runs the worker over a bounds-checked slot range.
func (i *Interpreter) fillPartition(
	partition *Partition,
	start int64,
	end int64,
	worker string,
) (Value, error) {
	if start < 0 || end > int64(len(partition.values)) {
		return voidValue(), fmt.Errorf("runtime error: parallel_map range out of bounds")
	}
	for idx := start; idx < end; idx++ {
		value, err := i.callFunction(worker, []Value{intValue(idx)})
		if err != nil {
			return voidValue(), err
		}
		partition.values[int(idx)] = value
	}
	return voidValue(), nil
}

// evalThreadScopedTyped runs the std-only one-argument scoped thread primitive.
func (i *Interpreter) evalThreadScopedTyped(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 3 {
		return voidValue(), fmt.Errorf("runtime error: std::thread::scoped expects io, function, and arg")
	}
	if _, err := i.evalExpr(args[0], env); err != nil {
		return voidValue(), err
	}
	worker, ok, err := i.evalFunctionNameArg(args[1], env)
	if err != nil {
		return voidValue(), err
	}
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: std::thread::scoped expects function name")
	}
	value, err := i.evalExpr(args[2], env)
	if err != nil {
		return voidValue(), err
	}
	return i.callFunction(worker, []Value{value})
}

// evalAtomic constructs a seq_cst primitive atomic value.
func (i *Interpreter) evalAtomic(typeArg string, args []ast.Expression, env *Env) (Value, error) {
	if !isRuntimeAtomicSupportedType(typeArg) {
		return voidValue(), fmt.Errorf("runtime error: unsupported atomic type `%s` in v0.1", typeArg)
	}
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: std::atomic::Atomic<%s> expects 1 arg", typeArg)
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if !runtimeValueMatchesType(value, typeArg) {
		return voidValue(), fmt.Errorf("runtime error: Atomic<%s> expects %s", typeArg, typeArg)
	}
	return atomicValue(typeArg, value), nil
}

// isRuntimeAtomicSupportedType reports whether Atomic<T> can run in v0.1.
func isRuntimeAtomicSupportedType(typeName string) bool {
	return typeName == "bool" || typeName == "i64"
}

// evalArrayConstructor creates an owned Array<T> with an explicit allocator.
func (i *Interpreter) evalArrayConstructor(
	typeArg string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: std::array::Array<%s> expects allocator", typeArg)
	}
	allocator, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if allocator.kind != kindAllocator {
		return voidValue(), fmt.Errorf("runtime error: std::array::Array<%s> expects Allocator", typeArg)
	}
	return arrayValue(typeArg), nil
}

// evalMapConstructor creates an owned Map<[]const u8, V> with an allocator.
func (i *Interpreter) evalMapConstructor(
	typeArg string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	parts, ok := splitGenericArgs(typeArg)
	if !ok || len(parts) != 2 || parts[0] != "[]const u8" {
		return voidValue(), fmt.Errorf("runtime error: std::map::Map expects []const u8 key")
	}
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: std::map::Map<%s> expects allocator", typeArg)
	}
	allocator, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if allocator.kind != kindAllocator {
		return voidValue(), fmt.Errorf("runtime error: std::map::Map<%s> expects Allocator", typeArg)
	}
	return mapValue(parts[1]), nil
}

// runtimeValueMatchesType checks primitive values used by typed runtime containers.
func runtimeValueMatchesType(value Value, typeName string) bool {
	switch typeName {
	case "bool":
		return value.kind == kindBool
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return value.kind == kindInt
	case "[]const u8":
		return value.kind == kindString
	default:
		if value.kind == kindEnum {
			return value.enum.typeName == typeName
		}
		if value.kind == kindStruct {
			return value.typeName == typeName
		}
		return false
	}
}

// evalMutex constructs a synchronous protected value.
func (i *Interpreter) evalMutex(typeArg string, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: std::sync::Mutex<%s> expects 1 arg", typeArg)
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	return mutexValue(typeArg, value), nil
}

// evalTaskGroup constructs a task group bound to one Io implementation.
func (i *Interpreter) evalTaskGroup(args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: std::task::Group expects io")
	}
	ioValue, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if ioValue.kind != kindIo {
		return voidValue(), fmt.Errorf("runtime error: std::task::Group expects Io")
	}
	return taskGroupValue(ioValue), nil
}

// evalTaskGroupMethod executes the v0.1 structured spawn model.
func (i *Interpreter) evalTaskGroupMethod(
	group Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if name != "spawn" {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 1 {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup.spawn expects function and args")
	}
	target, ok := args[0].(*ast.IdentExpr)
	if !ok {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup.spawn expects function name")
	}
	values, err := i.evalArgs(args[1:], env)
	if err != nil {
		return voidValue(), err
	}
	callArgs := append([]Value{group.taskGroup.io}, values...)
	return i.spawnTask(group.taskGroup.io, target.Name, callArgs), nil
}

// spawnTask executes a task according to the group's Io implementation.
func (i *Interpreter) spawnTask(ioValue Value, name string, args []Value) Value {
	if ioValue.typeName == "threaded" {
		result := make(chan TaskResult, 1)
		go func() {
			value, err := i.callFunction(name, args)
			result <- TaskResult{value: value, err: err}
		}()
		return runningTaskValue(result)
	}
	result, err := i.callFunction(name, args)
	return completedTaskValue(result, err)
}

// evalTaskMethod awaits or cancels a task value.
func evalTaskMethod(task Value, name string, args []ast.Expression) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: task.%s expected 0 args", name)
	}
	switch name {
	case "await":
		if task.task.state == taskCanceled {
			return voidValue(), fmt.Errorf("runtime error: task was canceled")
		}
		if task.task.state == taskAwaited {
			return voidValue(), fmt.Errorf("runtime error: task was already awaited")
		}
		value, err := finishTask(task.task)
		task.task.state = taskAwaited
		return value, err
	case "cancel":
		if task.task.state == taskCanceled {
			return voidValue(), fmt.Errorf("runtime error: task was already canceled")
		}
		if task.task.state == taskAwaited {
			return voidValue(), fmt.Errorf("runtime error: task was already awaited")
		}
		waitTask(task.task)
		task.task.state = taskCanceled
		return voidValue(), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: Task has no method `%s`", name)
	}
}

// finishTask waits for a running task and returns its result.
func finishTask(task *Task) (Value, error) {
	waitTask(task)
	return task.value, task.err
}

// waitTask waits for a running task and stores its result.
func waitTask(task *Task) {
	if task.result != nil && task.state == taskOpen {
		result := <-task.result
		task.value = result.value
		task.err = result.err
		task.result = nil
	}
}

// evalChannelMethod dispatches public Channel methods through Kizu std wrappers.
func (i *Interpreter) evalChannelMethod(
	channel Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if name == "close" {
		return i.evalChannelRuntimeMethod(channel, name, args, env)
	}
	if value, ok, err := i.evalStdMethodWrapper(
		"std.channel."+name, []string{channel.typeName}, channel, args, env,
	); ok || err != nil {
		return value, err
	}
	return voidValue(), missingStdMethodWrapper("std.channel." + name)
}

// evalChannelRuntimeMethod executes owned message passing operations.
func (i *Interpreter) evalChannelRuntimeMethod(
	channel Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	switch name {
	case "send":
		if len(args) != 1 {
			return voidValue(), fmt.Errorf("runtime error: channel.send expects 1 arg")
		}
		value, err := i.evalExpr(args[0], env)
		if err != nil {
			return voidValue(), err
		}
		channel.channel.values = append(channel.channel.values, value)
		return voidValue(), nil
	case "recv":
		if len(args) != 0 {
			return voidValue(), fmt.Errorf("runtime error: channel.recv expects 0 args")
		}
		if len(channel.channel.values) == 0 {
			return voidValue(), fmt.Errorf("runtime error: channel is empty")
		}
		value := channel.channel.values[0]
		channel.channel.values = channel.channel.values[1:]
		return value, nil
	case "close":
		if len(args) != 0 {
			return voidValue(), fmt.Errorf("runtime error: channel.close expects 0 args")
		}
		channel.channel.closed = true
		return voidValue(), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: Channel has no method `%s`", name)
	}
}

// evalPartitionMethod returns a disjoint part marker for an index.
func (i *Interpreter) evalPartitionMethod(
	partition Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if name != "at" {
		return voidValue(), fmt.Errorf("runtime error: Partition has no method `%s`", name)
	}
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: partition.at expects 1 arg")
	}
	index, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if index.kind != kindInt || index.i < 0 || int(index.i) >= len(partition.partition.values) {
		return voidValue(), fmt.Errorf("runtime error: partition index out of bounds")
	}
	return partitionSlotValue(partition.partition, int(index.i)), nil
}

// evalLocalBufferMethod returns one worker-local scratch value by index.
func (i *Interpreter) evalLocalBufferMethod(
	buffer Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if name != "get" {
		return voidValue(), fmt.Errorf("runtime error: LocalBuffer has no method `%s`", name)
	}
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: LocalBuffer.get expects 1 arg")
	}
	index, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if index.kind != kindInt || index.i < 0 || int(index.i) >= len(buffer.localBuf.values) {
		return voidValue(), fmt.Errorf("runtime error: LocalBuffer index out of bounds")
	}
	return buffer.localBuf.values[int(index.i)], nil
}

// evalArrayMethod dispatches public Array methods through Kizu std wrappers.
func (i *Interpreter) evalArrayMethod(
	array Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if isArrayRuntimeOnlyMethod(name) {
		return i.evalArrayRuntimeMethod(array, name, args, env)
	}
	if value, ok, err := i.evalStdMethodWrapper(
		"std.array."+name, []string{array.typeName}, array, args, env,
	); ok || err != nil {
		return value, err
	}
	return voidValue(), missingStdMethodWrapper("std.array." + name)
}

// evalArrayRuntimeMethod executes owned Array<T> prototype operations.
func (i *Interpreter) evalArrayRuntimeMethod(
	array Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if array.array.deinit {
		return voidValue(), fmt.Errorf("runtime error: Array was deinitialized")
	}
	switch name {
	case "append":
		return i.evalArrayAppend(array, args, env)
	case "reserve":
		return i.evalArrayReserve(array, args, env)
	case "len":
		return intValue(int64(len(array.array.values))), requireNoArgs("Array.len", args)
	case "capacity":
		return intValue(int64(cap(array.array.values))), requireNoArgs("Array.capacity", args)
	case "as_bytes":
		return evalArrayAsBytes(array, args)
	case "get":
		return i.evalArrayGet(array, args, env)
	case "at":
		return i.evalArrayAt(array, args, env, false)
	case "at_mut":
		return i.evalArrayAt(array, args, env, true)
	case "set":
		return i.evalArraySet(array, args, env)
	case "clear":
		array.array.values = array.array.values[:0]
		return voidValue(), requireNoArgs("Array.clear", args)
	case "truncate":
		return i.evalArrayTruncate(array, args, env)
	case "deinit":
		array.array.values = nil
		array.array.deinit = true
		return voidValue(), requireNoArgs("Array.deinit", args)
	default:
		return voidValue(), fmt.Errorf("runtime error: Array has no method `%s`", name)
	}
}

// evalArrayReserve ensures additional capacity for future Array appends.
func (i *Interpreter) evalArrayReserve(
	array Value,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	additional, err := i.evalArrayCount("Array.reserve", args, env)
	if err != nil {
		return voidValue(), err
	}
	if additional < 0 {
		return errorUnionValue("Array.reserve expects non-negative i64"), nil
	}
	ensureArrayCapacity(array.array, len(array.array.values)+additional)
	return voidValue(), nil
}

// evalArrayTruncate shortens an Array while preserving capacity.
func (i *Interpreter) evalArrayTruncate(
	array Value,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	length, err := i.evalArrayCount("Array.truncate", args, env)
	if err != nil {
		return voidValue(), err
	}
	if length < 0 || length > len(array.array.values) {
		return errorUnionValue("Array.truncate length out of bounds"), nil
	}
	array.array.values = array.array.values[:length]
	return voidValue(), nil
}

// evalArrayAsBytes copies Array<u8> contents into a read-only byte slice value.
func evalArrayAsBytes(array Value, args []ast.Expression) (Value, error) {
	if err := requireNoArgs("Array.as_bytes", args); err != nil {
		return voidValue(), err
	}
	if array.typeName != "u8" {
		return voidValue(), fmt.Errorf("runtime error: Array.as_bytes requires Array<u8>")
	}
	bytes := make([]byte, 0, len(array.array.values))
	for _, value := range array.array.values {
		if value.kind != kindInt || value.i < 0 || value.i > 255 {
			return voidValue(), fmt.Errorf("runtime error: Array.as_bytes requires u8 elements")
		}
		bytes = append(bytes, byte(value.i))
	}
	return stringValue(string(bytes)), nil
}

// evalArrayAt returns a checked local borrow for one array element.
func (i *Interpreter) evalArrayAt(
	array Value,
	args []ast.Expression,
	env *Env,
	mutable bool,
) (Value, error) {
	index, err := i.evalArrayIndex("Array.at", args, env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index >= len(array.array.values) {
		return errorUnionValue("Array.at index out of bounds"), nil
	}
	cell := &binding{
		value:       array.array.values[index],
		mutable:     mutable,
		arrayParent: array.array,
		arrayIndex:  index,
	}
	return refValue(cell), nil
}

// evalArraySet replaces one element after a bounds check.
func (i *Interpreter) evalArraySet(array Value, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 2 {
		return voidValue(), fmt.Errorf("runtime error: Array.set expects 2 args")
	}
	index, err := i.evalArrayIndex("Array.set", args[:1], env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index >= len(array.array.values) {
		return errorUnionValue("Array.set index out of bounds"), nil
	}
	value, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), err
	}
	if !runtimeValueMatchesType(value, array.typeName) {
		return errorUnionValue("Array.set element type mismatch"), nil
	}
	array.array.values[index] = value
	return voidValue(), nil
}

// evalArrayAppend appends one element and reports allocation failure as !void.
func (i *Interpreter) evalArrayAppend(array Value, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: Array.append expects 1 arg")
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), err
	}
	if !runtimeValueMatchesType(value, array.typeName) {
		return errorUnionValue("Array.append element type mismatch"), nil
	}
	array.array.values = append(array.array.values, value)
	return voidValue(), nil
}

// ensureArrayCapacity grows an Array backing slice without changing its length.
func ensureArrayCapacity(array *Array, want int) {
	if want <= cap(array.values) {
		return
	}
	values := make([]Value, len(array.values), want)
	copy(values, array.values)
	array.values = values
}

// evalArrayGet reads one copyable element by checked index.
func (i *Interpreter) evalArrayGet(array Value, args []ast.Expression, env *Env) (Value, error) {
	index, err := i.evalArrayIndex("Array.get", args, env)
	if err != nil {
		return voidValue(), err
	}
	if index < 0 || index >= len(array.array.values) {
		return errorUnionValue("Array.get index out of bounds"), nil
	}
	return array.array.values[index], nil
}

// evalMapMethod dispatches public Map methods through Kizu std wrappers.
func (i *Interpreter) evalMapMethod(
	mapVal Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if value, ok, err := i.evalStdMethodWrapper(
		"std.map."+name, []string{mapVal.mapValue.valueType}, mapVal, args, env,
	); ok || err != nil {
		return value, err
	}
	return voidValue(), missingStdMethodWrapper("std.map." + name)
}

// evalMapRuntimeMethod executes owned Map<[]const u8, V> prototype operations.
func (i *Interpreter) evalMapRuntimeMethod(
	mapVal Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if mapVal.mapValue.deinit {
		return voidValue(), fmt.Errorf("runtime error: Map was deinitialized")
	}
	switch name {
	case "insert":
		return i.evalMapInsert(mapVal, args, env)
	case "get":
		return i.evalMapGet(mapVal, args, env)
	case "contains":
		return i.evalMapContains(mapVal, args, env)
	case "len":
		return intValue(int64(len(mapVal.mapValue.entries))), requireNoArgs("Map.len", args)
	case "deinit":
		mapVal.mapValue.entries = nil
		mapVal.mapValue.deinit = true
		return voidValue(), requireNoArgs("Map.deinit", args)
	default:
		return voidValue(), fmt.Errorf("runtime error: Map has no method `%s`", name)
	}
}

// evalMapInsert stores a copy of one byte-slice key and copy value.
func (i *Interpreter) evalMapInsert(mapVal Value, args []ast.Expression, env *Env) (Value, error) {
	if len(args) != 2 {
		return voidValue(), fmt.Errorf("runtime error: Map.insert expects 2 args")
	}
	key, value, err := i.evalMapEntryArgs(args, env)
	if err != nil {
		return voidValue(), err
	}
	if !runtimeValueMatchesType(value, mapVal.mapValue.valueType) {
		return errorUnionValue("Map.insert value type mismatch"), nil
	}
	mapVal.mapValue.entries[key.s] = value
	return voidValue(), nil
}

// evalMapGet reads one copyable value by byte-slice key.
func (i *Interpreter) evalMapGet(mapVal Value, args []ast.Expression, env *Env) (Value, error) {
	key, err := i.evalMapKey("Map.get", args, env)
	if err != nil {
		return voidValue(), err
	}
	value, exists := mapVal.mapValue.entries[key]
	if !exists {
		return errorUnionValue("Map.get key not found"), nil
	}
	return value, nil
}

// evalMapContains reports whether a byte-slice key is present.
func (i *Interpreter) evalMapContains(
	mapVal Value,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	key, err := i.evalMapKey("Map.contains", args, env)
	if err != nil {
		return voidValue(), err
	}
	_, exists := mapVal.mapValue.entries[key]
	return boolValue(exists), nil
}

// evalMapEntryArgs evaluates and validates Map.insert arguments.
func (i *Interpreter) evalMapEntryArgs(
	args []ast.Expression,
	env *Env,
) (Value, Value, error) {
	key, err := i.evalExpr(args[0], env)
	if err != nil {
		return voidValue(), voidValue(), err
	}
	if key.kind != kindString {
		return voidValue(), voidValue(), fmt.Errorf("runtime error: Map.insert expects []const u8 key")
	}
	value, err := i.evalExpr(args[1], env)
	if err != nil {
		return voidValue(), voidValue(), err
	}
	return key, value, nil
}

// evalMapKey evaluates one []const u8 lookup key.
func (i *Interpreter) evalMapKey(name string, args []ast.Expression, env *Env) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("runtime error: %s expects 1 arg", name)
	}
	key, err := i.evalExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if key.kind != kindString {
		return "", fmt.Errorf("runtime error: %s expects []const u8 key", name)
	}
	return key.s, nil
}

// evalArrayCount evaluates one i64 count argument.
func (i *Interpreter) evalArrayCount(name string, args []ast.Expression, env *Env) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("runtime error: %s expects 1 arg", name)
	}
	value, err := i.evalExpr(args[0], env)
	if err != nil {
		return 0, err
	}
	if value.kind != kindInt {
		return 0, fmt.Errorf("runtime error: %s expects i64", name)
	}
	return int(value.i), nil
}

// evalArrayIndex evaluates one checked i64 index argument.
func (i *Interpreter) evalArrayIndex(name string, args []ast.Expression, env *Env) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("runtime error: %s expects 1 arg", name)
	}
	index, err := i.evalExpr(args[0], env)
	if err != nil {
		return 0, err
	}
	if index.kind != kindInt {
		return 0, fmt.Errorf("runtime error: %s expects i64 index", name)
	}
	return int(index.i), nil
}

// splitGenericArgs extracts top-level comma-separated generic arguments.
func splitGenericArgs(arg string) ([]string, bool) {
	args := []string{}
	start := 0
	depth := 0
	for idx, ch := range arg {
		switch ch {
		case '<':
			depth++
		case '>':
			if depth == 0 {
				return nil, false
			}
			depth--
		case ',':
			if depth == 0 {
				item := strings.TrimSpace(arg[start:idx])
				if item == "" {
					return nil, false
				}
				args = append(args, item)
				start = idx + 1
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	item := strings.TrimSpace(arg[start:])
	if item == "" {
		return nil, false
	}
	return append(args, item), true
}

// evalAtomicMethod dispatches public Atomic methods through Kizu std wrappers.
func (i *Interpreter) evalAtomicMethod(
	atomic Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if value, ok, err := i.evalStdMethodWrapper(
		"std.atomic."+name, []string{atomic.typeName}, atomic, args, env,
	); ok || err != nil {
		return value, err
	}
	return voidValue(), missingStdMethodWrapper("std.atomic." + name)
}

// evalAtomicRuntimeMethod executes seq_cst-only integer atomic operations.
func (i *Interpreter) evalAtomicRuntimeMethod(
	atomic Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	switch name {
	case "load":
		if len(args) != 0 {
			return voidValue(), fmt.Errorf("runtime error: atomic.load expects 0 args")
		}
		return atomic.atomic.value, nil
	case "store":
		if len(args) != 1 {
			return voidValue(), fmt.Errorf("runtime error: atomic.store expects 1 arg")
		}
		value, err := i.evalExpr(args[0], env)
		if err != nil {
			return voidValue(), err
		}
		if !runtimeValueMatchesType(value, atomic.typeName) {
			return voidValue(), fmt.Errorf("runtime error: atomic.store expects %s", atomic.typeName)
		}
		atomic.atomic.value = value
		return voidValue(), nil
	default:
		return voidValue(), fmt.Errorf("runtime error: Atomic has no method `%s`", name)
	}
}

// evalMutexMethod dispatches public Mutex methods through Kizu std wrappers.
func (i *Interpreter) evalMutexMethod(
	mutex Value,
	name string,
	args []ast.Expression,
	env *Env,
) (Value, error) {
	if value, ok, err := i.evalStdMethodWrapper(
		"std.sync."+name, []string{mutex.typeName}, mutex, args, env,
	); ok || err != nil {
		return value, err
	}
	return voidValue(), missingStdMethodWrapper("std.sync." + name)
}

// missingStdMethodWrapper reports a violated std wrapper registration invariant.
func missingStdMethodWrapper(name string) error {
	return fmt.Errorf("runtime error: missing std method wrapper `%s`", name)
}

// isArrayRuntimeOnlyMethod reports private storage helpers intentionally kept primitive.
func isArrayRuntimeOnlyMethod(name string) bool {
	switch name {
	case "reserve", "truncate", "clear", "as_bytes":
		return true
	default:
		return false
	}
}

// evalMutexRuntimeMethod executes synchronous lock-free prototype operations.
func (i *Interpreter) evalMutexRuntimeMethod(
	mutex Value,
	name string,
	args []ast.Expression,
	_ *Env,
) (Value, error) {
	switch name {
	case "get":
		if len(args) != 0 {
			return voidValue(), fmt.Errorf("runtime error: mutex.get expects 0 args")
		}
		return mutex.mutex.value, nil
	default:
		return voidValue(), fmt.Errorf("runtime error: Mutex has no method `%s`", name)
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

// requireNoArgs validates zero-argument runtime methods.
func requireNoArgs(name string, args []ast.Expression) error {
	if len(args) != 0 {
		return fmt.Errorf("runtime error: %s expects 0 args", name)
	}
	return nil
}

// callPrint writes one value followed by a newline.
func (i *Interpreter) callPrint(args []Value) (Value, error) {
	if len(args) != 1 {
		return voidValue(), fmt.Errorf("runtime error: print expected 1 arg")
	}
	i.outMu.Lock()
	defer i.outMu.Unlock()
	if _, err := fmt.Fprintln(i.out, args[0].String()); err != nil {
		return voidValue(), err
	}
	return voidValue(), nil
}

// callError constructs an error-union error value by copying message bytes.
func callError(args []Value) (Value, error) {
	if len(args) != 1 || args[0].kind != kindString {
		return voidValue(), fmt.Errorf("runtime error: error expected []const u8")
	}
	return errorUnionValue(args[0].s), nil
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(typeName string) (string, string, bool) {
	if len(typeName) > 1 && typeName[0] == '!' {
		return "", typeName[1:], true
	}
	for idx, ch := range typeName {
		if ch == '!' && idx > 0 && idx < len(typeName)-1 {
			return typeName[:idx], typeName[idx+1:], true
		}
	}
	return "", "", false
}

// callIo constructs an explicit I/O capability value.
func callIo(args []Value) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: Io expected 0 args")
	}
	return voidValue(), fmt.Errorf("runtime error: use std::io::blocking()")
}

// callTaskGroup constructs a structured task group value.
func callTaskGroup(args []Value) (Value, error) {
	if len(args) != 0 {
		return voidValue(), fmt.Errorf("runtime error: TaskGroup expected 0 args")
	}
	return voidValue(), fmt.Errorf("runtime error: use std::task::Group(io)")
}

// callIoFromExprs validates std::io constructors and returns an Io value.
func callIoFromExprs(mode string, args []ast.Expression) Value {
	if len(args) != 0 {
		return errorUnionValue("std::io::" + mode + " expected 0 args")
	}
	return ioValue(mode)
}

// callAllocatorFromExprs validates explicit allocator factory calls.
func callAllocatorFromExprs(args []ast.Expression) Value {
	if len(args) != 0 {
		return errorUnionValue("std::mem::page_allocator expected 0 args")
	}
	return allocatorValue("page")
}

// callChannelFromExprs validates std::channel::Channel<T> has no constructor args.
func callChannelFromExprs(typeArg string, args []ast.Expression) Value {
	if len(args) != 0 {
		return errorUnionValue("std::channel::Channel<" + typeArg + "> expected 0 args")
	}
	return channelValue(typeArg)
}

// callQueueFromExprs validates std::task::Queue has no constructor args.
func callQueueFromExprs(args []ast.Expression) Value {
	if len(args) != 0 {
		return errorUnionValue("std::task::Queue expected 0 args")
	}
	return queueValue()
}

// qualifiedName renders a namespace chain as an internal key such as std::task::Group.
func qualifiedName(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return "", false
		}
		left, ok := qualifiedName(e.Receiver)
		if !ok {
			return "", false
		}
		return left + "." + e.Name, true
	default:
		return "", false
	}
}

// borrowPrefix reports whether an expression is &T or &mut T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&mut") {
		return nil, false
	}
	return prefix, true
}
