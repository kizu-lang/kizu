package ownership

import (
	"fmt"
	"strconv"

	"tiny-safe/internal/ast"
)

// checkComptimeIfStmt checks ownership effects for the selected compile-time branch.
func (c *Checker) checkComptimeIfStmt(stmt *ast.ComptimeIfStmt, env *scope) error {
	if _, err := c.readComptimeOnly(stmt.Condition); err != nil {
		return err
	}
	selected := stmt.Consequence
	value, known := comptimeBool(stmt.Condition)
	if known && !value {
		selected = stmt.Alternative
	}
	if selected == nil {
		return nil
	}
	return c.checkBlock(selected, env.child())
}

// intLiteral evaluates integer-only compile-time arithmetic used in branch conditions.
func intLiteral(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return intLiteral(e.Expr)
	case *ast.IntExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		return value, err == nil
	case *ast.PrefixExpr:
		value, ok := intLiteral(e.Right)
		if e.Operator == "-" {
			return -value, ok
		}
	case *ast.BinaryExpr:
		left, leftOK := intLiteral(e.Left)
		right, rightOK := intLiteral(e.Right)
		if leftOK && rightOK {
			return evalComptimeInt(e.Operator, left, right)
		}
	}
	return 0, false
}

// evalComptimeInt evaluates simple integer arithmetic for ownership branch selection.
func evalComptimeInt(op string, left int64, right int64) (int64, bool) {
	switch op {
	case "+":
		return left + right, true
	case "-":
		return left - right, true
	case "*":
		return left * right, true
	case "/":
		if right == 0 {
			return 0, false
		}
		return left / right, true
	case "%":
		if right == 0 {
			return 0, false
		}
		return left % right, true
	default:
		return 0, false
	}
}

// compareComptimeInts evaluates integer comparisons for ownership branch selection.
func compareComptimeInts(op string, left int64, right int64) (bool, bool) {
	switch op {
	case "==":
		return left == right, true
	case "!=":
		return left != right, true
	case "<":
		return left < right, true
	case "<=":
		return left <= right, true
	case ">":
		return left > right, true
	case ">=":
		return left >= right, true
	default:
		return false, false
	}
}

// readComptimeExpr reads a compile-time expression without moving runtime values.
func (c *Checker) readComptimeExpr(expr *ast.ComptimeExpr, _ *scope) (string, error) {
	return c.readComptimeOnly(expr.Expr)
}

// readComptimeOnly rejects runtime locals inside compile-time expressions.
func (c *Checker) readComptimeOnly(expr ast.Expression) (string, error) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.readComptimeOnly(e.Expr)
	case *ast.IntExpr:
		return "i64", nil
	case *ast.StringExpr:
		return "string", nil
	case *ast.BoolExpr:
		return "bool", nil
	case *ast.PrefixExpr:
		return c.readComptimeOnly(e.Right)
	case *ast.BinaryExpr:
		left, err := c.readComptimeOnly(e.Left)
		if err != nil {
			return "", err
		}
		if _, err := c.readComptimeOnly(e.Right); err != nil {
			return "", err
		}
		return left, nil
	default:
		return "", fmt.Errorf("borrow error: runtime value cannot cross comptime boundary")
	}
}

// comptimeBool evaluates simple compile-time boolean expressions for ownership branch checks.
func comptimeBool(expr ast.Expression) (bool, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return comptimeBool(e.Expr)
	case *ast.BoolExpr:
		return e.Value, true
	case *ast.PrefixExpr:
		value, ok := comptimeBool(e.Right)
		return !value, ok && e.Operator == "!"
	case *ast.BinaryExpr:
		left, leftOK := intLiteral(e.Left)
		right, rightOK := intLiteral(e.Right)
		if leftOK && rightOK {
			return compareComptimeInts(e.Operator, left, right)
		}
	}
	return false, false
}
