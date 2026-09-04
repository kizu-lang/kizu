package types

import (
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/typ"
)

type comptimeValue struct {
	typ Type
	i   int64
	b   bool
	s   string
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
	case *ast.IntExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		if err != nil {
			return comptimeValue{}, errorf("comptime error: invalid integer `%s`", e.Value)
		}
		return comptimeValue{typ: typeI64, i: value}, nil
	case *ast.FloatExpr:
		return comptimeValue{}, errorf(
			"comptime error: a floating-point value is not evaluated at compile time")
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
	return c.evalComptimeMetaCall(expr)
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
		if right.typ != typeI64 {
			return comptimeValue{}, errorf("comptime error: unary - expects integer")
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
