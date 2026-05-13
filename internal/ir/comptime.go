package ir

import (
	"fmt"
	"strconv"

	"tiny-safe/internal/ast"
)

// lowerComptimeIfStmt lowers only the branch selected during compilation.
func (l *lowerer) lowerComptimeIfStmt(stmt *ast.ComptimeIfStmt) error {
	cond, ok := constBool(stmt.Condition)
	if !ok {
		return fmt.Errorf("ir error: comptime if condition must be constant bool")
	}
	if cond {
		return l.lowerBlock(stmt.Consequence)
	}
	if stmt.Alternative == nil {
		return nil
	}
	return l.lowerBlock(stmt.Alternative)
}

// constBool evaluates constant boolean expressions used by comptime if.
func constBool(expr ast.Expression) (bool, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return constBool(e.Expr)
	case *ast.BoolExpr:
		return e.Value, true
	case *ast.PrefixExpr:
		value, ok := constBool(e.Right)
		return !value, ok && e.Operator == "!"
	case *ast.BinaryExpr:
		left, leftOK := constInt(e.Left)
		right, rightOK := constInt(e.Right)
		if leftOK && rightOK {
			return compareConstInts(e.Operator, left, right)
		}
	}
	return false, false
}

// constInt evaluates constant integer arithmetic used by comptime branches.
func constInt(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return constInt(e.Expr)
	case *ast.IntExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		return value, err == nil
	case *ast.PrefixExpr:
		value, ok := constInt(e.Right)
		if e.Operator == "-" {
			return -value, ok
		}
	case *ast.BinaryExpr:
		left, leftOK := constInt(e.Left)
		right, rightOK := constInt(e.Right)
		if leftOK && rightOK {
			return evalConstInt(e.Operator, left, right)
		}
	}
	return 0, false
}

// evalConstInt evaluates integer arithmetic for comptime branch selection.
func evalConstInt(op string, left int64, right int64) (int64, bool) {
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

// compareConstInts evaluates integer comparisons for comptime branch selection.
func compareConstInts(op string, left int64, right int64) (bool, bool) {
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
