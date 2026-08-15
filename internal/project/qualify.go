package project

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// qualifyModule rewrites one parsed module into package-qualified names.
func (c *graphChecker) qualifyModule(module *moduleUnit) (*ast.Program, error) {
	out := &ast.Program{}
	for _, decl := range module.program.Decls {
		qualified, err := c.qualifyDecl(module, decl)
		if err != nil {
			return nil, err
		}
		if qualified != nil {
			out.Decls = append(out.Decls, qualified)
		}
	}
	return out, nil
}

// qualifyDecl rewrites declaration type references for a package check.
func (c *graphChecker) qualifyDecl(module *moduleUnit, decl ast.Decl) (ast.Decl, error) {
	switch d := decl.(type) {
	case *ast.ImportDecl:
		return nil, nil
	case *ast.StructDecl:
		return c.qualifyStruct(module, d)
	case *ast.EnumDecl:
		cp := *d
		cp.Name = module.qualify(d.Name)
		return &cp, nil
	case *ast.ErrorSetDecl:
		cp := *d
		cp.Name = module.qualify(d.Name)
		return &cp, nil
	case *ast.UnionDecl:
		return c.qualifyUnion(module, d)
	case *ast.ContractDecl:
		return c.qualifyContract(module, d)
	case *ast.ImplDecl:
		return c.qualifyImpl(module, d)
	case *ast.FunctionDecl:
		return c.qualifyFunction(module, d, module.qualify(d.Name))
	case *ast.TestDecl:
		return c.qualifyTestDecl(module, d)
	default:
		return decl, nil
	}
}

// qualifyStruct rewrites a struct declaration and its field types.
func (c *graphChecker) qualifyStruct(
	module *moduleUnit,
	decl *ast.StructDecl,
) (*ast.StructDecl, error) {
	cp := *decl
	cp.Name = module.qualify(decl.Name)
	cp.Fields = append([]ast.Field(nil), decl.Fields...)
	for idx := range cp.Fields {
		resolved, err := c.resolveTypeNode(module, cp.Fields[idx].TypeName)
		if err != nil {
			return nil, err
		}
		cp.Fields[idx].TypeName = resolved
	}
	return &cp, nil
}

// qualifyUnion rewrites a union declaration and its payload types.
func (c *graphChecker) qualifyUnion(
	module *moduleUnit,
	decl *ast.UnionDecl,
) (*ast.UnionDecl, error) {
	cp := *decl
	cp.Name = module.qualify(decl.Name)
	cp.Variants = append([]ast.UnionVariant(nil), decl.Variants...)
	for idx := range cp.Variants {
		if cp.Variants[idx].Payload == nil {
			continue
		}
		resolved, err := c.resolveTypeNode(module, cp.Variants[idx].Payload)
		if err != nil {
			return nil, err
		}
		cp.Variants[idx].Payload = resolved
	}
	return &cp, nil
}

// qualifyContract rewrites contract method signature type references.
func (c *graphChecker) qualifyContract(
	module *moduleUnit,
	decl *ast.ContractDecl,
) (*ast.ContractDecl, error) {
	cp := *decl
	cp.Name = module.qualify(decl.Name)
	cp.Methods = append([]*ast.FunctionDecl(nil), decl.Methods...)
	for idx, method := range cp.Methods {
		qualified, err := c.qualifyFunction(module, method, method.Name)
		if err != nil {
			return nil, err
		}
		cp.Methods[idx] = qualified
	}
	return &cp, nil
}

// qualifyImpl rewrites an impl receiver, contract name, and method bodies.
func (c *graphChecker) qualifyImpl(module *moduleUnit, decl *ast.ImplDecl) (*ast.ImplDecl, error) {
	cp := *decl
	typeName, err := c.resolveType(module, decl.TypeName)
	if err != nil {
		return nil, err
	}
	cp.TypeName = typeName
	if decl.ContractName != "" {
		contractName, err := c.resolveType(module, decl.ContractName)
		if err != nil {
			return nil, err
		}
		cp.ContractName = contractName
	}
	cp.Methods = append([]*ast.FunctionDecl(nil), decl.Methods...)
	for idx, method := range cp.Methods {
		qualified, err := c.qualifyFunction(module, method, method.Name)
		if err != nil {
			return nil, err
		}
		cp.Methods[idx] = qualified
	}
	return &cp, nil
}

// qualifyFunction rewrites a function signature and body type references.
func (c *graphChecker) qualifyFunction(
	module *moduleUnit,
	decl *ast.FunctionDecl,
	name string,
) (*ast.FunctionDecl, error) {
	cp := *decl
	cp.Name = name
	cp.Params = append([]ast.Param(nil), decl.Params...)
	for idx := range cp.Params {
		resolved, err := c.resolveTypeNode(module, cp.Params[idx].TypeName)
		if err != nil {
			return nil, err
		}
		cp.Params[idx].TypeName = resolved
	}
	if cp.ReturnType != nil {
		resolved, err := c.resolveTypeNode(module, cp.ReturnType)
		if err != nil {
			return nil, err
		}
		cp.ReturnType = resolved
	}
	body, err := c.qualifyBlock(module, decl.Body)
	if err != nil {
		return nil, err
	}
	cp.Body = body
	return &cp, nil
}

// qualifyTestDecl rewrites type-bearing expressions inside a test block.
func (c *graphChecker) qualifyTestDecl(
	module *moduleUnit,
	decl *ast.TestDecl,
) (*ast.TestDecl, error) {
	cp := *decl
	body, err := c.qualifyBlock(module, decl.Body)
	cp.Body = body
	return &cp, err
}

// qualifyBlock rewrites type-bearing expressions inside a block.
func (c *graphChecker) qualifyBlock(
	module *moduleUnit,
	block *ast.BlockStmt,
) (*ast.BlockStmt, error) {
	if block == nil {
		return nil, nil
	}
	cp := &ast.BlockStmt{Statements: append([]ast.Statement(nil), block.Statements...)}
	for idx, stmt := range cp.Statements {
		qualified, err := c.qualifyStmt(module, stmt)
		if err != nil {
			return nil, err
		}
		cp.Statements[idx] = qualified
	}
	return cp, nil
}

// qualifyStmt rewrites type-bearing expressions inside one statement.
func (c *graphChecker) qualifyStmt(module *moduleUnit, stmt ast.Statement) (ast.Statement, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		cp := *s
		value, err := c.qualifyExpr(module, s.Value)
		cp.Value = value
		return &cp, err
	case *ast.AssignStmt:
		return c.qualifyAssignStmt(module, s)
	case *ast.ReturnStmt:
		cp := *s
		value, err := c.qualifyExpr(module, s.Value)
		cp.Value = value
		return &cp, err
	case *ast.DeferStmt:
		return c.qualifyDeferStmt(module, s)
	case *ast.ErrDeferStmt:
		return c.qualifyErrDeferStmt(module, s)
	case *ast.ExprStmt:
		cp := *s
		expr, err := c.qualifyExpr(module, s.Expr)
		cp.Expr = expr
		return &cp, err
	case *ast.IfStmt:
		return c.qualifyIfStmt(module, s)
	case *ast.WhileStmt:
		cp := *s
		condition, err := c.qualifyExpr(module, s.Condition)
		if err != nil {
			return nil, err
		}
		cp.Condition = condition
		cp.Body, err = c.qualifyBlock(module, s.Body)
		return &cp, err
	case *ast.ForStmt:
		return c.qualifyForStmt(module, s)
	case *ast.MatchStmt:
		return c.qualifyMatchStmt(module, s)
	case *ast.UnsafeStmt:
		cp := *s
		body, err := c.qualifyBlock(module, s.Body)
		cp.Body = body
		return &cp, err
	case *ast.ComptimeIfStmt:
		return c.qualifyComptimeIfStmt(module, s)
	default:
		return stmt, nil
	}
}

// qualifyDeferStmt rewrites type-bearing expressions in a deferred cleanup.
func (c *graphChecker) qualifyDeferStmt(
	module *moduleUnit,
	stmt *ast.DeferStmt,
) (*ast.DeferStmt, error) {
	cp := *stmt
	expr, err := c.qualifyExpr(module, stmt.Expr)
	cp.Expr = expr
	return &cp, err
}

// qualifyErrDeferStmt rewrites type-bearing expressions in an errdefer cleanup.
func (c *graphChecker) qualifyErrDeferStmt(
	module *moduleUnit,
	stmt *ast.ErrDeferStmt,
) (*ast.ErrDeferStmt, error) {
	cp := *stmt
	expr, err := c.qualifyExpr(module, stmt.Expr)
	cp.Expr = expr
	return &cp, err
}

// qualifyAssignStmt rewrites both sides of an assignment statement.
func (c *graphChecker) qualifyAssignStmt(
	module *moduleUnit,
	stmt *ast.AssignStmt,
) (*ast.AssignStmt, error) {
	cp := *stmt
	var err error
	cp.Target, err = c.qualifyExpr(module, stmt.Target)
	if err != nil {
		return nil, err
	}
	cp.Value, err = c.qualifyExpr(module, stmt.Value)
	return &cp, err
}

// qualifyIfStmt rewrites all expressions reachable from an if node.
func (c *graphChecker) qualifyIfStmt(module *moduleUnit, stmt *ast.IfStmt) (*ast.IfStmt, error) {
	cp := *stmt
	condition, err := c.qualifyExpr(module, stmt.Condition)
	if err != nil {
		return nil, err
	}
	cp.Condition = condition
	cp.Consequence, err = c.qualifyBlock(module, stmt.Consequence)
	if err != nil {
		return nil, err
	}
	cp.Alternative, err = c.qualifyBlock(module, stmt.Alternative)
	return &cp, err
}

// qualifyForStmt rewrites range bounds and loop body expressions.
func (c *graphChecker) qualifyForStmt(module *moduleUnit, stmt *ast.ForStmt) (*ast.ForStmt, error) {
	cp := *stmt
	var err error
	cp.Start, err = c.qualifyExpr(module, stmt.Start)
	if err != nil {
		return nil, err
	}
	cp.End, err = c.qualifyExpr(module, stmt.End)
	if err != nil {
		return nil, err
	}
	cp.Body, err = c.qualifyBlock(module, stmt.Body)
	return &cp, err
}

// qualifyComptimeIfStmt rewrites both branches of a compile-time conditional.
func (c *graphChecker) qualifyComptimeIfStmt(
	module *moduleUnit,
	stmt *ast.ComptimeIfStmt,
) (*ast.ComptimeIfStmt, error) {
	cp := *stmt
	condition, err := c.qualifyExpr(module, stmt.Condition)
	if err != nil {
		return nil, err
	}
	cp.Condition = condition
	cp.Consequence, err = c.qualifyBlock(module, stmt.Consequence)
	if err != nil {
		return nil, err
	}
	cp.Alternative, err = c.qualifyBlock(module, stmt.Alternative)
	return &cp, err
}

// qualifyMatchStmt rewrites the matched value and all arm bodies.
func (c *graphChecker) qualifyMatchStmt(
	module *moduleUnit,
	stmt *ast.MatchStmt,
) (*ast.MatchStmt, error) {
	cp := *stmt
	var err error
	cp.Value, err = c.qualifyExpr(module, stmt.Value)
	if err != nil {
		return nil, err
	}
	cp.Arms = append([]ast.MatchArm(nil), stmt.Arms...)
	for idx := range cp.Arms {
		cp.Arms[idx].Body, err = c.qualifyStmt(module, cp.Arms[idx].Body)
		if err != nil {
			return nil, err
		}
	}
	return &cp, nil
}

// qualifyExpr rewrites type names carried by expressions.
func (c *graphChecker) qualifyExpr(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.qualifyComptimeExpr(module, e)
	case *ast.PrefixExpr:
		return c.qualifyPrefixExpr(module, e)
	case *ast.BinaryExpr:
		return c.qualifyBinaryExpr(module, e)
	case *ast.CallExpr:
		return c.qualifyCallExpr(module, e)
	case *ast.CastExpr:
		return c.qualifyCastExpr(module, e)
	case *ast.TryExpr:
		return c.qualifyTryExpr(module, e)
	default:
		return c.qualifyTypeOrControlExpr(module, expr)
	}
}

// qualifyTypeOrControlExpr rewrites type-bearing and control expressions.
func (c *graphChecker) qualifyTypeOrControlExpr(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	switch e := expr.(type) {
	case *ast.TypeApplyExpr:
		return c.qualifyTypeApplyExpr(module, e)
	case *ast.TypeExpr:
		cp := *e
		typ, err := c.resolveType(module, e.TypeName)
		if err != nil {
			return &cp, err
		}
		cp.TypeName = typ
		return &cp, nil
	case *ast.ArenaNewExpr:
		cp := *e
		typ, err := c.resolveType(module, e.TypeName)
		if err != nil {
			return &cp, err
		}
		cp.TypeName = typ
		cp.Allocator, err = c.qualifyExpr(module, e.Allocator)
		return &cp, err
	case *ast.StructLiteralExpr:
		return c.qualifyStructLiteral(module, e)
	case *ast.FieldExpr:
		return c.qualifyFieldExpr(module, e)
	case *ast.IndexExpr:
		return c.qualifyIndexExpr(module, e)
	case *ast.DerefExpr:
		return c.qualifyDerefExpr(module, e)
	case *ast.IfStmt:
		return c.qualifyIfStmt(module, e)
	case *ast.MatchStmt:
		return c.qualifyMatchStmt(module, e)
	default:
		return expr, nil
	}
}

// qualifyComptimeExpr rewrites the expression evaluated at compile time.
func (c *graphChecker) qualifyComptimeExpr(
	module *moduleUnit,
	expr *ast.ComptimeExpr,
) (*ast.ComptimeExpr, error) {
	cp := *expr
	value, err := c.qualifyExpr(module, expr.Expr)
	cp.Expr = value
	return &cp, err
}

// qualifyPrefixExpr rewrites the operand of a unary expression.
func (c *graphChecker) qualifyPrefixExpr(
	module *moduleUnit,
	expr *ast.PrefixExpr,
) (*ast.PrefixExpr, error) {
	cp := *expr
	right, err := c.qualifyExpr(module, expr.Right)
	cp.Right = right
	return &cp, err
}

// qualifyBinaryExpr rewrites both sides of a binary expression.
func (c *graphChecker) qualifyBinaryExpr(
	module *moduleUnit,
	expr *ast.BinaryExpr,
) (*ast.BinaryExpr, error) {
	cp := *expr
	var err error
	cp.Left, err = c.qualifyExpr(module, expr.Left)
	if err != nil {
		return nil, err
	}
	cp.Right, err = c.qualifyExpr(module, expr.Right)
	return &cp, err
}

// qualifyCastExpr rewrites the target type and value of a cast expression.
func (c *graphChecker) qualifyCastExpr(
	module *moduleUnit,
	expr *ast.CastExpr,
) (*ast.CastExpr, error) {
	cp := *expr
	resolved, err := c.resolveTypeNode(module, expr.TargetType)
	if err != nil {
		return nil, err
	}
	cp.TargetType = resolved
	cp.Value, err = c.qualifyExpr(module, expr.Value)
	return &cp, err
}

// qualifyTryExpr rewrites the fallible expression wrapped by try.
func (c *graphChecker) qualifyTryExpr(
	module *moduleUnit,
	expr *ast.TryExpr,
) (*ast.TryExpr, error) {
	cp := *expr
	value, err := c.qualifyExpr(module, expr.Value)
	cp.Value = value
	return &cp, err
}

// qualifyCallExpr rewrites callees and arguments inside a call expression.
func (c *graphChecker) qualifyCallExpr(
	module *moduleUnit,
	expr *ast.CallExpr,
) (*ast.CallExpr, error) {
	cp := *expr
	var err error
	cp.Callee, err = c.qualifyCallee(module, expr.Callee)
	if err != nil {
		return nil, err
	}
	cp.Args = append([]ast.Expression(nil), expr.Args...)
	for idx := range cp.Args {
		cp.Args[idx], err = c.qualifyExpr(module, cp.Args[idx])
		if err != nil {
			return nil, err
		}
	}
	return &cp, nil
}

// qualifyTypeApplyExpr rewrites explicit static type arguments in constructor calls.
//
// The callee goes through qualifyCallee, not qualifyExpr: `f<T>(x)` names a
// function of this module exactly as `f(x)` does, and reading it as a plain
// expression left the name unqualified while the declaration was registered
// under the module path, so the call found nothing that takes static arguments.
func (c *graphChecker) qualifyTypeApplyExpr(
	module *moduleUnit,
	expr *ast.TypeApplyExpr,
) (*ast.TypeApplyExpr, error) {
	cp := *expr
	callee, err := c.qualifyCallee(module, expr.Callee)
	if err != nil {
		return nil, err
	}
	cp.Callee = callee
	args, err := splitTypeArgs(expr.TypeArg)
	if err != nil {
		return nil, err
	}
	for idx, arg := range args {
		args[idx], err = c.resolveType(module, arg)
		if err != nil {
			return nil, err
		}
	}
	cp.TypeArg = strings.Join(args, ", ")
	return &cp, nil
}

// qualifyCallee rewrites imported function calls to their package function name.
func (c *graphChecker) qualifyCallee(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	if ident, ok := expr.(*ast.IdentExpr); ok && declaresFunction(module, ident.Name) {
		return &ast.IdentExpr{Name: module.qualify(ident.Name)}, nil
	}
	if field, ok := expr.(*ast.FieldExpr); ok && field.Namespace {
		if _, ok := c.resolveTypeNamespaceReceiver(module, field); ok {
			return c.qualifyFieldExpr(module, field)
		}
		name, ok, err := c.resolveNamespacePath(module, field)
		if err != nil {
			return nil, err
		}
		if ok {
			return &ast.IdentExpr{Name: name}, nil
		}
	}
	return c.qualifyExpr(module, expr)
}

// declaresFunction reports whether module has a local top-level function.
func declaresFunction(module *moduleUnit, name string) bool {
	for _, decl := range module.program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if ok && fn.Name == name {
			return true
		}
	}
	return false
}

// qualifyFieldExpr rewrites namespace receivers while preserving field names.
func (c *graphChecker) qualifyFieldExpr(
	module *moduleUnit,
	expr *ast.FieldExpr,
) (*ast.FieldExpr, error) {
	cp := *expr
	if expr.Namespace {
		receiver, ok := c.resolveTypeNamespaceReceiver(module, expr)
		if ok {
			cp.Receiver = &ast.IdentExpr{Name: receiver}
			return &cp, nil
		}
	}
	receiver, err := c.qualifyExpr(module, expr.Receiver)
	cp.Receiver = receiver
	return &cp, err
}

// qualifyIndexExpr rewrites target and index expressions.
func (c *graphChecker) qualifyIndexExpr(
	module *moduleUnit,
	expr *ast.IndexExpr,
) (*ast.IndexExpr, error) {
	cp := *expr
	var err error
	cp.Target, err = c.qualifyExpr(module, expr.Target)
	if err != nil {
		return nil, err
	}
	cp.Index, err = c.qualifyExpr(module, expr.Index)
	if err != nil {
		return nil, err
	}
	cp.Start, err = c.qualifyExpr(module, expr.Start)
	if err != nil {
		return nil, err
	}
	cp.End, err = c.qualifyExpr(module, expr.End)
	return &cp, err
}

// qualifyDerefExpr rewrites the receiver before explicit pointer dereference.
func (c *graphChecker) qualifyDerefExpr(
	module *moduleUnit,
	expr *ast.DerefExpr,
) (*ast.DerefExpr, error) {
	cp := *expr
	receiver, err := c.qualifyExpr(module, expr.Receiver)
	cp.Receiver = receiver
	return &cp, err
}

// qualifyStructLiteral rewrites struct literal type and field value expressions.
func (c *graphChecker) qualifyStructLiteral(
	module *moduleUnit,
	expr *ast.StructLiteralExpr,
) (*ast.StructLiteralExpr, error) {
	cp := *expr
	typ, err := c.resolveType(module, expr.TypeName)
	if err != nil {
		return nil, err
	}
	cp.TypeName = typ
	cp.Fields = append([]ast.FieldValue(nil), expr.Fields...)
	for idx := range cp.Fields {
		value, err := c.qualifyExpr(module, cp.Fields[idx].Value)
		if err != nil {
			return nil, err
		}
		cp.Fields[idx].Value = value
	}
	return &cp, nil
}
