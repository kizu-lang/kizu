package ownership

import (
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
)

// checkComptimeIfStmt checks ownership effects for the selected compile-time branch.
func (c *Checker) checkComptimeIfStmt(stmt *ast.ComptimeIfStmt, env *scope) error {
	if _, err := c.readComptimeOnly(stmt.Condition); err != nil {
		return err
	}
	selected := stmt.Consequence
	value, known := c.comptimeBool(stmt.Condition)
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
		return "[]u8", nil
	case *ast.BoolExpr:
		return "bool", nil
	case *ast.TypeExpr:
		return "type", nil
	case *ast.IdentExpr:
		if _, ok := c.typeArgValues[e.Name]; ok {
			return "type", nil
		}
		return "", errorf("borrow error: runtime value cannot cross comptime boundary")
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
		return "", errorf("borrow error: runtime value cannot cross comptime boundary")
	}
}

// comptimeBool evaluates simple compile-time boolean expressions for ownership branch checks.
func (c *Checker) comptimeBool(expr ast.Expression) (bool, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.comptimeBool(e.Expr)
	case *ast.BoolExpr:
		return e.Value, true
	case *ast.PrefixExpr:
		value, ok := c.comptimeBool(e.Right)
		return !value, ok && e.Operator == "!"
	case *ast.BinaryExpr:
		if e.Operator == "and" || e.Operator == "or" {
			return c.comptimeLogicalBool(e)
		}
		leftType, leftTypeOK := c.comptimeTypeValue(e.Left)
		rightType, rightTypeOK := c.comptimeTypeValue(e.Right)
		if leftTypeOK && rightTypeOK {
			return compareComptimeTypes(e.Operator, leftType, rightType)
		}
		left, leftOK := intLiteral(e.Left)
		right, rightOK := intLiteral(e.Right)
		if leftOK && rightOK {
			return compareComptimeInts(e.Operator, left, right)
		}
	}
	return false, false
}

// comptimeTypeValue returns a type value from the minimal compile-time type subset.
func (c *Checker) comptimeTypeValue(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.comptimeTypeValue(e.Expr)
	case *ast.TypeExpr:
		return e.TypeName, e.TypeName != ""
	case *ast.IdentExpr:
		value, ok := c.typeArgValues[e.Name]
		return value, ok
	default:
		return "", false
	}
}

// compareComptimeTypes evaluates equality on compile-time type values.
func compareComptimeTypes(op string, left string, right string) (bool, bool) {
	switch op {
	case "==":
		return left == right, true
	case "!=":
		return left != right, true
	default:
		return false, false
	}
}

// comptimeLogicalBool evaluates constant boolean logical expressions.
func (c *Checker) comptimeLogicalBool(expr *ast.BinaryExpr) (bool, bool) {
	left, leftOK := c.comptimeBool(expr.Left)
	if !leftOK {
		return false, false
	}
	if expr.Operator == "and" && !left {
		return false, true
	}
	if expr.Operator == "or" && left {
		return true, true
	}
	right, rightOK := c.comptimeBool(expr.Right)
	if !rightOK {
		return false, false
	}
	if expr.Operator == "and" {
		return left && right, true
	}
	return left || right, true
}
