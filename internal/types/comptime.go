package types

import (
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmath"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/typ"
)

type comptimeValue struct {
	typ Type
	i   int64
	f   float64
	b   bool
	s   string
}

// text spells an integer or bool value the way a static argument does, which
// is how an instance key tells two bindings apart.
func (v comptimeValue) text() string {
	if v.typ == typeBool {
		return strconv.FormatBool(v.b)
	}
	return strconv.FormatInt(v.i, 10)
}

// enterComptimeValues replaces the comptime-readable names with the values an
// instance binds and returns the call that puts back the ones in force. It
// replaces rather than adds: an instance body is its own function, and the
// captures and static values of the body that called it are not in scope.
func (c *Checker) enterComptimeValues(values map[string]comptimeValue) func() {
	previous := c.comptimeValues
	c.comptimeValues = make(map[string]comptimeValue, len(values))
	for name, value := range values {
		c.comptimeValues[name] = value
	}
	return func() { c.comptimeValues = previous }
}

// checkComptimeExpr validates and evaluates a compile-time expression.
func (c *Checker) checkComptimeExpr(
	expr *ast.ComptimeExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	typ, err := c.checkExpr(expr.Expr, env, unsafe)
	if err != nil {
		return "", err
	}
	value, err := c.evalComptime(expr.Expr)
	if err != nil {
		return "", err
	}
	if value.typ != typ {
		return "", errorf("comptime error: expected %s, got %s", typ, value.typ)
	}
	return typ, nil
}

// checkComptimeIfStmt checks only the branch selected by a compile-time bool.
func (c *Checker) checkComptimeIfStmt(
	stmt *ast.ComptimeIfStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	cond, err := c.evalComptime(stmt.Condition)
	if err != nil {
		return false, err
	}
	if cond.typ != typeBool {
		return false, errorf("comptime error: if condition must be bool, got %s", cond.typ)
	}
	if cond.b {
		return c.checkBlock(stmt.Consequence, env.child(), wantReturn, unsafe)
	}
	if stmt.Alternative == nil {
		return false, nil
	}
	return c.checkBlock(stmt.Alternative, env.child(), wantReturn, unsafe)
}

// evalComptime evaluates the side-effect-free expression subset allowed at compile time.
func (c *Checker) evalComptime(expr ast.Expression) (comptimeValue, error) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.evalComptime(e.Expr)
	case *ast.IntExpr, *ast.FloatExpr:
		return evalComptimeNumber(e)
	case *ast.CastExpr:
		return c.evalComptimeCast(e)
	case *ast.BoolExpr:
		return comptimeValue{typ: typeBool, b: e.Value}, nil
	case *ast.StringExpr:
		return comptimeValue{typ: typeByteString, s: e.Value}, nil
	case *ast.TypeExpr:
		typ, err := c.parseType(e.TypeName)
		if err != nil {
			return comptimeValue{}, err
		}
		return comptimeValue{typ: typeType, s: string(typ)}, nil
	case *ast.IdentExpr:
		typ, ok := c.typeArgValues[e.Name]
		if ok {
			return comptimeValue{typ: typeType, s: string(typ)}, nil
		}
		if value, ok := c.comptimeValues[e.Name]; ok {
			return value, nil
		}
		return comptimeValue{}, errorf("comptime error: runtime value cannot be used")
	case *ast.PrefixExpr:
		return c.evalComptimePrefix(e)
	case *ast.BinaryExpr:
		return c.evalComptimeBinary(e)
	case *ast.CallExpr:
		return c.evalComptimeCall(e)
	default:
		return comptimeValue{}, errorf("comptime error: runtime value cannot be used")
	}
}

// evalComptimeNumber reads an integer or float literal.
func evalComptimeNumber(expr ast.Expression) (comptimeValue, error) {
	if e, ok := expr.(*ast.FloatExpr); ok {
		value, ok := typ.ParseFloatLiteral(e.Value)
		if !ok {
			return comptimeValue{}, errorf("comptime error: invalid float `%s`", e.Value)
		}
		return comptimeValue{typ: typeF64, f: value}, nil
	}
	text := expr.(*ast.IntExpr).Value
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return comptimeValue{}, errorf("comptime error: invalid integer `%s`", text)
	}
	return comptimeValue{typ: typeI64, i: value}, nil
}

// evalComptimeCall evaluates the compiler-defined std predicates. Everything
// else a call could name runs at run time (SPEC §13.1).
func (c *Checker) evalComptimeCall(expr *ast.CallExpr) (comptimeValue, error) {
	if name, ok := qualifiedName(expr.Callee); ok {
		if predicate, known := stdtarget.Identify(name); known {
			if len(expr.Args) != 0 {
				return comptimeValue{}, errorf(
					"comptime error: `%s` takes no arguments", name)
			}
			return comptimeValue{
				typ: typeBool,
				b:   stdtarget.Evaluate(c.target, predicate),
			}, nil
		}
	}
	if value, ok, err := c.evalComptimeMathCall(expr); ok {
		return value, err
	}
	return c.evalComptimeMetaCall(expr)
}

// evalComptimeMathCall evaluates a std::math call a float comptime expression
// may make -- sin, cos, sqrt, pi, tau at f64 -- to the bits std computes on
// the target being built (internal/stdmath), and reports whether the call is
// one.
func (c *Checker) evalComptimeMathCall(expr *ast.CallExpr) (comptimeValue, bool, error) {
	apply, ok := expr.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return comptimeValue{}, false, nil
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok || !strings.HasPrefix(name, "std::math::") {
		return comptimeValue{}, false, nil
	}
	if !stdmath.Names(name) {
		return comptimeValue{}, true, errorf(
			"comptime error: `%s` is not evaluated at compile time "+
				"(std::math sin, cos, sqrt, pi and tau are)", name)
	}
	if apply.TypeArg != "f64" {
		return comptimeValue{}, true, errorf(
			"comptime error: `%s` is evaluated at compile time for f64 only, got `%s`",
			name, apply.TypeArg)
	}
	args := make([]float64, 0, len(expr.Args))
	for _, arg := range expr.Args {
		value, err := c.evalComptime(arg)
		if err != nil {
			return comptimeValue{}, true, err
		}
		if value.typ != typeF64 {
			return comptimeValue{}, true, errorf("comptime error: `%s` expects f64, got %s", name, value.typ)
		}
		args = append(args, value.f)
	}
	result, ok := stdmath.Call(name, args, c.target.IsNative())
	if !ok {
		return comptimeValue{}, true, errorf(
			"comptime error: `%s` takes %d arguments here", name, len(args))
	}
	value, err := finiteComptimeFloat(result)
	return value, true, err
}

// evalComptimeCast evaluates `cast<f64>(n)` of a comptime integer or float.
// It is the one conversion a float comptime expression needs: a capture or
// a static value is an integer, and the angle it names is a float.
func (c *Checker) evalComptimeCast(expr *ast.CastExpr) (comptimeValue, error) {
	value, err := c.evalComptime(expr.Value)
	if err != nil {
		return comptimeValue{}, err
	}
	if typ.Text(expr.TargetType) != "f64" {
		return comptimeValue{}, errorf(
			"comptime error: only `cast<f64>` is evaluated at compile time, got `cast<%s>`",
			typ.Text(expr.TargetType))
	}
	switch value.typ {
	case typeI64:
		return comptimeValue{typ: typeF64, f: float64(value.i)}, nil
	case typeF64:
		return value, nil
	default:
		return comptimeValue{}, errorf("comptime error: `cast<f64>` expects a number, got %s", value.typ)
	}
}

// evalComptimeFloatBinary evaluates arithmetic and comparison on two f64
// comptime values. Kizu converts no number implicitly, so an integer on
// either side is an error, as it is at run time.
func evalComptimeFloatBinary(op string, left, right comptimeValue) (comptimeValue, error) {
	if left.typ != typeF64 || right.typ != typeF64 {
		return comptimeValue{}, errorf(
			"comptime error: operator `%s` expects two f64 values, got %s and %s", op, left.typ, right.typ)
	}
	if result, ok := stdmath.Compare(op, left.f, right.f); ok {
		return comptimeValue{typ: typeBool, b: result}, nil
	}
	result, ok := stdmath.Binary(op, left.f, right.f)
	if !ok {
		return comptimeValue{}, errorf("comptime error: operator `%s` is not defined on f64", op)
	}
	return finiteComptimeFloat(result)
}

// finiteComptimeFloat wraps a folded float, refusing a value no literal can
// name: an infinity or NaN at compile time is the program's error.
func finiteComptimeFloat(value float64) (comptimeValue, error) {
	if !stdmath.Finite(value) {
		return comptimeValue{}, errorf("comptime error: the value is not finite (%v)", value)
	}
	return comptimeValue{typ: typeF64, f: value}, nil
}

// evalComptimeMetaCall evaluates the type-directed `std::meta` predicates.
func (c *Checker) evalComptimeMetaCall(expr *ast.CallExpr) (comptimeValue, error) {
	apply, ok := expr.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return comptimeValue{}, errorf("comptime error: runtime value cannot be used")
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok || !stdmeta.Predicate(name) {
		return comptimeValue{}, errorf("comptime error: runtime value cannot be used")
	}
	if len(expr.Args) != 0 {
		return comptimeValue{}, errorf("comptime error: `%s` takes no arguments", name)
	}
	args, err := typ.SplitArgs(c.instantiateTypeArgText(apply.TypeArg))
	if err != nil {
		return comptimeValue{}, errorf(
			"comptime error: `%s` has an unreadable static argument list", name)
	}
	value, err := c.metaPredicate(stdmeta.Form(name), args)
	if err != nil {
		return comptimeValue{}, err
	}
	return comptimeValue{typ: typeBool, b: value}, nil
}

// evalComptimePrefix evaluates compile-time unary operators.
func (c *Checker) evalComptimePrefix(expr *ast.PrefixExpr) (comptimeValue, error) {
	right, err := c.evalComptime(expr.Right)
	if err != nil {
		return comptimeValue{}, err
	}
	switch expr.Operator {
	case "-":
		if right.typ == typeF64 {
			return comptimeValue{typ: typeF64, f: -right.f}, nil
		}
		if right.typ != typeI64 {
			return comptimeValue{}, errorf("comptime error: unary - expects a number")
		}
		return comptimeValue{typ: typeI64, i: -right.i}, nil
	case "~":
		if right.typ != typeI64 {
			return comptimeValue{}, errorf("comptime error: unary ~ expects integer")
		}
		return comptimeValue{typ: typeI64, i: ^right.i}, nil
	case "!":
		if right.typ != typeBool {
			return comptimeValue{}, errorf("comptime error: unary ! expects bool")
		}
		return comptimeValue{typ: typeBool, b: !right.b}, nil
	default:
		return comptimeValue{}, errorf("comptime error: unsupported unary `%s`", expr.Operator)
	}
}

// evalComptimeBinary evaluates compile-time binary operators.
func (c *Checker) evalComptimeBinary(expr *ast.BinaryExpr) (comptimeValue, error) {
	if expr.Operator == "and" || expr.Operator == "or" {
		return c.evalComptimeLogical(expr)
	}
	left, err := c.evalComptime(expr.Left)
	if err != nil {
		return comptimeValue{}, err
	}
	right, err := c.evalComptime(expr.Right)
	if err != nil {
		return comptimeValue{}, err
	}
	if left.typ == typeF64 || right.typ == typeF64 {
		return evalComptimeFloatBinary(expr.Operator, left, right)
	}
	if expr.Operator == "==" || expr.Operator == "!=" {
		return evalComptimeEquality(expr.Operator, left, right)
	}
	if left.typ != typeI64 || right.typ != typeI64 {
		return comptimeValue{}, errorf(
			"comptime error: operator `%s` expects integers",
			expr.Operator,
		)
	}
	return evalComptimeIntBinary(expr.Operator, left.i, right.i)
}

// evalComptimeLogical evaluates short-circuit compile-time boolean operators.
func (c *Checker) evalComptimeLogical(expr *ast.BinaryExpr) (comptimeValue, error) {
	left, err := c.evalComptime(expr.Left)
	if err != nil {
		return comptimeValue{}, err
	}
	if left.typ != typeBool {
		return comptimeValue{}, errorf("comptime error: operator `%s` expects bools", expr.Operator)
	}
	if expr.Operator == "and" && !left.b {
		return comptimeValue{typ: typeBool, b: false}, nil
	}
	if expr.Operator == "or" && left.b {
		return comptimeValue{typ: typeBool, b: true}, nil
	}
	right, err := c.evalComptime(expr.Right)
	if err != nil {
		return comptimeValue{}, err
	}
	if right.typ != typeBool {
		return comptimeValue{}, errorf("comptime error: operator `%s` expects bools", expr.Operator)
	}
	return comptimeValue{typ: typeBool, b: right.b}, nil
}

// evalComptimeEquality compares compile-time scalar values of the same type.
func evalComptimeEquality(
	op string,
	left comptimeValue,
	right comptimeValue,
) (comptimeValue, error) {
	if left.typ != right.typ {
		return comptimeValue{}, errorf("comptime error: equality operands must have same type")
	}
	equal := left.i == right.i && left.b == right.b && left.s == right.s
	if op == "!=" {
		equal = !equal
	}
	return comptimeValue{typ: typeBool, b: equal}, nil
}

// evalComptimeIntBinary evaluates integer arithmetic and comparisons.
func evalComptimeIntBinary(op string, left int64, right int64) (comptimeValue, error) {
	switch op {
	case "+":
		return comptimeValue{typ: typeI64, i: left + right}, nil
	case "-":
		return comptimeValue{typ: typeI64, i: left - right}, nil
	case "*":
		return comptimeValue{typ: typeI64, i: left * right}, nil
	case "/":
		return evalComptimeDivision(left, right)
	case "%":
		return evalComptimeModulo(left, right)
	case "&":
		return comptimeValue{typ: typeI64, i: left & right}, nil
	case "|":
		return comptimeValue{typ: typeI64, i: left | right}, nil
	case "^":
		return comptimeValue{typ: typeI64, i: left ^ right}, nil
	case "<<", ">>":
		if right < 0 {
			return comptimeValue{}, errorf("comptime error: shift amount `%d` is negative", right)
		}
		return comptimeValue{typ: typeI64, i: typ.ShiftInt64(op, left, right)}, nil
	case "<", "<=", ">", ">=":
		return comptimeValue{typ: typeBool, b: compareInts(op, left, right)}, nil
	default:
		return comptimeValue{}, errorf("comptime error: unsupported operator `%s`", op)
	}
}

// evalComptimeDivision evaluates checked integer division.
func evalComptimeDivision(left int64, right int64) (comptimeValue, error) {
	if right == 0 {
		return comptimeValue{}, errorf("comptime error: division by zero")
	}
	return comptimeValue{typ: typeI64, i: left / right}, nil
}

// evalComptimeModulo evaluates checked integer remainder.
func evalComptimeModulo(left int64, right int64) (comptimeValue, error) {
	if right == 0 {
		return comptimeValue{}, errorf("comptime error: modulo by zero")
	}
	return comptimeValue{typ: typeI64, i: left % right}, nil
}

// compareInts evaluates a compile-time integer comparison.
func compareInts(op string, left int64, right int64) bool {
	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	default:
		return left >= right
	}
}
