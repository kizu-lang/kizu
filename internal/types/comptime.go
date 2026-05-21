package types

import (
	"fmt"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
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
	unsafe bool,
) (Type, error) {
	typ, err := c.checkExpr(expr.Expr, env, unsafe)
	if err != nil {
		return "", err
	}
	if hasExplicitNonStaticLifetime(typ) {
		return "", fmt.Errorf("type error: lifetime view cannot cross comptime boundary")
	}
	value, err := c.evalComptime(expr.Expr)
	if err != nil {
		return "", err
	}
	if value.typ != typ {
		return "", fmt.Errorf("comptime error: expected %s, got %s", typ, value.typ)
	}
	return typ, nil
}

// checkComptimeIfStmt checks only the branch selected by a compile-time bool.
func (c *Checker) checkComptimeIfStmt(
	stmt *ast.ComptimeIfStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	cond, err := c.evalComptime(stmt.Condition)
	if err != nil {
		return false, err
	}
	if cond.typ != typeBool {
		return false, fmt.Errorf("comptime error: if condition must be bool, got %s", cond.typ)
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
			return comptimeValue{}, fmt.Errorf("comptime error: invalid integer `%s`", e.Value)
		}
		return comptimeValue{typ: typeI64, i: value}, nil
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
		return comptimeValue{}, fmt.Errorf("comptime error: runtime value cannot be used")
	case *ast.PrefixExpr:
		return c.evalComptimePrefix(e)
	case *ast.BinaryExpr:
		return c.evalComptimeBinary(e)
	default:
		return comptimeValue{}, fmt.Errorf("comptime error: runtime value cannot be used")
	}
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
			return comptimeValue{}, fmt.Errorf("comptime error: unary - expects integer")
		}
		return comptimeValue{typ: typeI64, i: -right.i}, nil
	case "!":
		if right.typ != typeBool {
			return comptimeValue{}, fmt.Errorf("comptime error: unary ! expects bool")
		}
		return comptimeValue{typ: typeBool, b: !right.b}, nil
	default:
		return comptimeValue{}, fmt.Errorf("comptime error: unsupported unary `%s`", expr.Operator)
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
		return comptimeValue{}, fmt.Errorf(
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
		return comptimeValue{}, fmt.Errorf("comptime error: operator `%s` expects bools", expr.Operator)
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
		return comptimeValue{}, fmt.Errorf("comptime error: operator `%s` expects bools", expr.Operator)
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
		return comptimeValue{}, fmt.Errorf("comptime error: equality operands must have same type")
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
	case "<", "<=", ">", ">=":
		return comptimeValue{typ: typeBool, b: compareInts(op, left, right)}, nil
	default:
		return comptimeValue{}, fmt.Errorf("comptime error: unsupported operator `%s`", op)
	}
}

// evalComptimeDivision evaluates checked integer division.
func evalComptimeDivision(left int64, right int64) (comptimeValue, error) {
	if right == 0 {
		return comptimeValue{}, fmt.Errorf("comptime error: division by zero")
	}
	return comptimeValue{typ: typeI64, i: left / right}, nil
}

// evalComptimeModulo evaluates checked integer remainder.
func evalComptimeModulo(left int64, right int64) (comptimeValue, error) {
	if right == 0 {
		return comptimeValue{}, fmt.Errorf("comptime error: modulo by zero")
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
