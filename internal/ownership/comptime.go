package ownership

import (
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/typ"
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

// comptimeLiteralType names the type of a literal a compile-time expression
// may hold, reporting false for anything that is not a literal.
func comptimeLiteralType(expr ast.Expression) (string, bool) {
	switch expr.(type) {
	case *ast.IntExpr:
		return "i64", true
	case *ast.FloatExpr:
		return "f64", true
	case *ast.StringExpr:
		return "[]u8", true
	case *ast.BoolExpr:
		return "bool", true
	case *ast.TypeExpr:
		return "type", true
	default:
		return "", false
	}
}

// readComptimeOnly rejects runtime locals inside compile-time expressions.
func (c *Checker) readComptimeOnly(expr ast.Expression) (string, error) {
	if typ, ok := comptimeLiteralType(expr); ok {
		return typ, nil
	}
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.readComptimeOnly(e.Expr)
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
	case *ast.CallExpr:
		if _, ok := c.targetPredicateCall(e); ok {
			return "bool", nil
		}
		if _, ok := c.metaPredicateCall(e); ok {
			return "bool", nil
		}
		return "", errorf("borrow error: runtime value cannot cross comptime boundary")
	default:
		return "", errorf("borrow error: runtime value cannot cross comptime boundary")
	}
}

// targetPredicateCall answers a compiler-defined `std::target` predicate and
// reports whether the call was one.
func (c *Checker) targetPredicateCall(expr *ast.CallExpr) (bool, bool) {
	name, ok := qualifiedName(expr.Callee)
	if !ok || len(expr.Args) != 0 {
		return false, false
	}
	predicate, ok := stdtarget.Identify(name)
	if !ok {
		return false, false
	}
	return stdtarget.Evaluate(c.target, predicate), true
}

// declaredKindPredicate answers the predicates that ask which declaration a
// type came from, and reports whether the form was one of them.
func (c *Checker) declaredKindPredicate(form stdmeta.Form, subject string) (bool, bool) {
	switch form {
	case stdmeta.IsStruct:
		_, known := c.structs[subject]
		return known, true
	case stdmeta.IsEnum:
		_, known := c.enums[subject]
		return known, true
	case stdmeta.IsUnion:
		_, known := c.unions[subject]
		return known, true
	default:
		return false, false
	}
}

// metaPredicateCall answers a `std::meta` predicate written as a compile-time
// condition, and reports whether the call was one.
func (c *Checker) metaPredicateCall(expr *ast.CallExpr) (bool, bool) {
	apply, ok := expr.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return false, false
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok || !stdmeta.Predicate(name) || len(expr.Args) != 0 {
		return false, false
	}
	subject := c.instantiateTypeArgText(apply.TypeArg)
	form := stdmeta.Form(name)
	if form == stdmeta.HasPayload {
		args, err := typ.SplitArgs(subject)
		if err != nil {
			return false, false
		}
		variant, err := c.metaCapture(form, args)
		if err != nil {
			return false, false
		}
		return variant.typ != "", true
	}
	if known, ok := c.declaredKindPredicate(form, subject); ok {
		return known, true
	}
	return c.shapePredicate(form, subject)
}

// shapePredicate answers the predicates a type's own spelling or its cleanup
// contract decides, without reading a declaration.
func (c *Checker) shapePredicate(form stdmeta.Form, subject string) (bool, bool) {
	switch form {
	case stdmeta.IsOptional:
		_, known := typ.OptionalElem(subject)
		return known, true
	case stdmeta.IsArray:
		return metaGenericBase(subject) == "std::array::Array", true
	case stdmeta.IsBox:
		return metaGenericBase(subject) == "std::mem::Box", true
	case stdmeta.IsMap:
		return metaGenericBase(subject) == "std::map::Map", true
	case stdmeta.IsOwner:
		return ast.OwnerType(c.deinitOwners, subject), true
	case stdmeta.ReleaseNamesAllocator:
		return ast.ReleaseNames(c.releaseAllocators, subject), true
	case stdmeta.HasPublicFields:
		return len(c.structPublicOrder[subject]) > 0, true
	default:
		return false, false
	}
}

// comptimeBool evaluates simple compile-time boolean expressions for ownership branch checks.
func (c *Checker) comptimeBool(expr ast.Expression) (bool, bool) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.comptimeBool(e.Expr)
	case *ast.BoolExpr:
		return e.Value, true
	case *ast.CallExpr:
		if value, ok := c.targetPredicateCall(e); ok {
			return value, true
		}
		return c.metaPredicateCall(e)
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
		name := c.instantiateTypeArgText(e.TypeName)
		return name, name != ""
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
