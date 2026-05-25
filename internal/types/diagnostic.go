package types

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	compilerdiag "github.com/kizu-lang/kizu/internal/diagnostic"
)

// DiagnosticError carries a type-checking error with a source span.
type DiagnosticError = compilerdiag.Error

// typeDiagnosticAt builds a structured type-checking diagnostic.
func typeDiagnosticAt(
	span ast.Span,
	code string,
	format string,
	args ...any,
) compilerdiag.Diagnostic {
	return compilerdiag.New(
		compilerdiag.SeverityError,
		compilerdiag.CategoryType,
		code,
		fmt.Sprintf(format, args...),
		span,
	)
}

// unsafeDiagnosticAt builds a structured unsafe capability diagnostic.
func unsafeDiagnosticAt(
	span ast.Span,
	code string,
	format string,
	args ...any,
) compilerdiag.Diagnostic {
	return compilerdiag.New(
		compilerdiag.SeverityError,
		compilerdiag.CategoryUnsafe,
		code,
		fmt.Sprintf(format, args...),
		span,
	)
}
