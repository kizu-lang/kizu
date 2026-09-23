package ir

import (
	"fmt"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/typ"
)

// lowerComptimeIfStmt lowers only the branch selected during compilation.
func (l *lowerer) lowerComptimeIfStmt(stmt *ast.ComptimeIfStmt) error {
	cond, ok := l.constBool(stmt.Condition)
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
func (l *lowerer) constBool(expr ast.Expression) (bool, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return l.constBool(e.Expr)
	case *ast.BoolExpr:
		return e.Value, true
	case *ast.IdentExpr:
		return l.staticBool(e.Name)
	case *ast.CallExpr:
		if value, ok := l.targetPredicateCall(e); ok {
			return value, true
		}
		return l.metaPredicateCall(e)
	case *ast.PrefixExpr:
		value, ok := l.constBool(e.Right)
		return !value, ok && e.Operator == "!"
	case *ast.BinaryExpr:
		return l.constBinaryBool(e)
	}
	return false, false
}

// staticBool reads a bool static parameter, which reaches the instance being
// lowered as its literal.
func (l *lowerer) staticBool(name string) (bool, bool) {
	static, ok := l.staticValues[name]
	if !ok || static.typ != "bool" {
		return false, false
	}
	return static.text == "true", true
}

// constBinaryBool evaluates a constant logical operator, a type comparison, or
// an integer comparison.
func (l *lowerer) constBinaryBool(e *ast.BinaryExpr) (bool, bool) {
	if e.Operator == "and" || e.Operator == "or" {
		return l.constLogicalBool(e)
	}
	if e.Operator == "==" || e.Operator == "!=" {
		if equal, ok := l.constTypeEqual(e.Left, e.Right); ok {
			return equal == (e.Operator == "=="), true
		}
	}
	left, leftOK := l.constInt(e.Left)
	right, rightOK := l.constInt(e.Right)
	if leftOK && rightOK {
		return compareConstInts(e.Operator, left, right)
	}
	return false, false
}

// targetPredicateCall answers a compiler-defined `std::target` predicate and
// reports whether the call was one.
func (l *lowerer) targetPredicateCall(expr *ast.CallExpr) (bool, bool) {
	predicate, ok := stdtarget.Identify(expr.Callee.String())
	if !ok || len(expr.Args) != 0 {
		return false, false
	}
	return stdtarget.Evaluate(l.target, predicate), true
}

// constTypeEqual compares two type-valued expressions, which is how a generic
// body asks what its type parameter was bound to.
func (l *lowerer) constTypeEqual(left ast.Expression, right ast.Expression) (bool, bool) {
	leftName, leftOK := l.constTypeName(left)
	rightName, rightOK := l.constTypeName(right)
	if !leftOK || !rightOK {
		return false, false
	}
	return leftName == rightName, true
}

// constTypeName resolves an expression that names a type.
func (l *lowerer) constTypeName(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return l.constTypeName(e.Expr)
	case *ast.IdentExpr:
		if bound, ok := l.typeBindings[e.Name]; ok {
			return bound, true
		}
	case *ast.TypeExpr:
		return l.resolveType(e.TypeName), true
	}
	return "", false
}

// constLogicalBool evaluates boolean logic for comptime branch selection.
func (l *lowerer) constLogicalBool(expr *ast.BinaryExpr) (bool, bool) {
	left, leftOK := l.constBool(expr.Left)
	if !leftOK {
		return false, false
	}
	if expr.Operator == "and" && !left {
		return false, true
	}
	if expr.Operator == "or" && left {
		return true, true
	}
	right, rightOK := l.constBool(expr.Right)
	if !rightOK {
		return false, false
	}
	if expr.Operator == "and" {
		return left && right, true
	}
	return left || right, true
}

// constInt evaluates constant integer arithmetic used by comptime branches.
func (l *lowerer) constInt(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return l.constInt(e.Expr)
	case *ast.IntExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		return value, err == nil
	case *ast.IdentExpr:
		// An integer-range capture is bound like an `<n: i64>` static value,
		// so the expansion's integer reads through the same binding.
		static, ok := l.staticValues[e.Name]
		if !ok || static.typ != "i64" {
			return 0, false
		}
		value, err := strconv.ParseInt(static.text, 10, 64)
		return value, err == nil
	case *ast.PrefixExpr:
		value, ok := l.constInt(e.Right)
		if e.Operator == "-" {
			return -value, ok
		}
		if e.Operator == "~" {
			return ^value, ok
		}
	case *ast.BinaryExpr:
		left, leftOK := l.constInt(e.Left)
		right, rightOK := l.constInt(e.Right)
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
	case "&":
		return left & right, true
	case "|":
		return left | right, true
	case "^":
		return left ^ right, true
	case "<<", ">>":
		if right < 0 {
			return 0, false
		}
		return typ.ShiftInt64(op, left, right), true
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
