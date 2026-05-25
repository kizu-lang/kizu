package diagnostic

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestRenderText checks stable text rendering from structured fields.
func TestRenderText(t *testing.T) {
	diag := New(
		SeverityError,
		CategoryType,
		"type.operator_type_mismatch",
		"operator `==` operands must have same type",
		ast.Span{Start: ast.Position{Line: 3, Column: 14}},
	).WithNote("left operand has type Color").
		WithNote("right operand has type Animal").
		WithHelp("use matching enum types")

	want := "type error: operator `==` operands must have same type at 3:14\n" +
		"note: left operand has type Color\n" +
		"note: right operand has type Animal\n" +
		"help: use matching enum types"
	if got := diag.RenderText(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFromRendered keeps the migration path from existing rendered messages structured.
func TestFromRendered(t *testing.T) {
	diag := FromRendered(
		"type error: unknown namespace `Color`\n"+
			"note: known namespaces: Animal\n"+
			"help: import a module that defines this namespace",
		ast.Span{Start: ast.Position{Line: 2, Column: 19}},
	)
	if diag.Category != CategoryType {
		t.Fatalf("category = %q, want %q", diag.Category, CategoryType)
	}
	if diag.Summary != "unknown namespace `Color`" {
		t.Fatalf("summary = %q", diag.Summary)
	}
	if len(diag.Details) != 2 || diag.Details[0].Kind != DetailNote ||
		diag.Details[1].Kind != DetailHelp {
		t.Fatalf("details = %#v", diag.Details)
	}
	want := "type error: unknown namespace `Color` at 2:19\n" +
		"note: known namespaces: Animal\n" +
		"help: import a module that defines this namespace"
	if got := diag.RenderText(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
