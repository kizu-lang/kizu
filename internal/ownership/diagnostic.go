package ownership

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
)

// errorAt builds one structured ownership diagnostic with source span information.
func errorAt(span ast.Span, format string, args ...any) error {
	return diag.FromText(diag.SeverityError, span, fmt.Sprintf(format, args...))
}

// errorf builds one structured ownership diagnostic without source span information.
func errorf(format string, args ...any) error {
	return diag.FromText(diag.SeverityError, ast.Span{}, fmt.Errorf(format, args...).Error())
}

// expressionSpan returns the best source span stored on an expression.
func expressionSpan(expr ast.Expression) ast.Span {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Span
	case *ast.BinaryExpr:
		return e.OperatorSpan
	case *ast.CastExpr:
		return e.KeywordSpan
	case *ast.FieldExpr:
		return e.Span
	case *ast.DerefExpr:
		return e.OperatorSpan
	}
	return nestedExpressionSpan(expr)
}

// nestedExpressionSpan unwraps compound expressions until it finds a stored span.
func nestedExpressionSpan(expr ast.Expression) ast.Span {
	if inner, ok := transparentExprValue(expr); ok {
		return expressionSpan(inner)
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		return expressionSpan(e.Callee)
	case *ast.TypeApplyExpr:
		return expressionSpan(e.Callee)
	case *ast.TryExpr:
		return expressionSpan(e.Value)
	case *ast.IndexExpr:
		return firstNonZeroSpan(e.Target, e.Index, e.Start, e.End)
	case *ast.ArenaNewExpr:
		if e.Allocator != nil {
			return expressionSpan(e.Allocator)
		}
	case *ast.StructLiteralExpr:
		if len(e.Fields) > 0 {
			return expressionSpan(e.Fields[0].Value)
		}
	case *ast.ComptimeExpr:
		return expressionSpan(e.Expr)
	}
	return ast.Span{}
}

// firstNonZeroSpan returns the first nested expression that carries source info.
func firstNonZeroSpan(exprs ...ast.Expression) ast.Span {
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		if span := expressionSpan(expr); span != (ast.Span{}) {
			return span
		}
	}
	return ast.Span{}
}
