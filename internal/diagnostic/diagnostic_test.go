package diagnostic

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/source"
)

// TestFromTextKeepsADRPartsStructured keeps category, note, and help split for renderers.
func TestFromTextKeepsADRPartsStructured(t *testing.T) {
	span := ast.Span{
		Start: ast.Position{Line: 6, Column: 14},
		End:   ast.Position{Line: 6, Column: 16},
	}
	diag := FromText(
		SeverityError,
		span,
		"type error: operator `==` operands must have same type\n"+
			"note: left operand has type Color\n"+
			"note: right operand has type Animal\n"+
			"help: compare values of the same enum",
	)
	if diag.Category != "type error" {
		t.Fatalf("category = %q, want type error", diag.Category)
	}
	if diag.Message != "operator `==` operands must have same type" {
		t.Fatalf("message = %q", diag.Message)
	}
	if len(diag.Notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(diag.Notes))
	}
	if diag.Help != "compare values of the same enum" {
		t.Fatalf("help = %q", diag.Help)
	}
	want := "type error: operator `==` operands must have same type at 6:14\n" +
		"note: left operand has type Color\n" +
		"note: right operand has type Animal\n" +
		"help: compare values of the same enum"
	if diag.Error() != want {
		t.Fatalf("got %q, want %q", diag.Error(), want)
	}
	if diag.CLIError() != "error: "+want {
		t.Fatalf("got %q, want %q", diag.CLIError(), "error: "+want)
	}
}

// TestDiagnosticResolvesSourcePath checks rendering resolves an ID at the output boundary.
func TestDiagnosticResolvesSourcePath(t *testing.T) {
	sources := source.NewMap()
	id := sources.Add("src/main.kizu", "fn main( {}")
	diag := New(SeverityError, "", ast.Span{
		Source: id,
		Start:  ast.Position{Line: 1, Column: 9},
		End:    ast.Position{Line: 1, Column: 10},
	}, "expected parameter")
	want := "expected parameter at src/main.kizu:1:9"
	if got := diag.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWarningRendersSeverity keeps warning diagnostics user-facing without error wrapping.
func TestWarningRendersSeverity(t *testing.T) {
	diag := New(
		SeverityWarning,
		"",
		ast.Span{Start: ast.Position{Line: 1, Column: 1}},
		"deprecated syntax will be removed",
	)
	want := "warning: deprecated syntax will be removed at 1:1"
	if diag.Error() != want {
		t.Fatalf("got %q, want %q", diag.Error(), want)
	}
	if diag.CLIError() != want {
		t.Fatalf("got %q, want %q", diag.CLIError(), want)
	}
}

// TestQuoteBytesMatchesSelfhostFmt keeps shipping diagnostics on the same
// deterministic ASCII contract as std::fmt::append_bytes_literal.
func TestQuoteBytesMatchesSelfhostFmt(t *testing.T) {
	input := string([]byte{'A', 0x20, 0x7e, '"', '\\', '\n', '\r', '\t', 0x00, 0x1f, 0x7f, 0xc3, 0xa9})
	want := `"A ~\"\\\n\r\t\x00\x1F\x7F\xC3\xA9"`
	if got := QuoteBytes(input); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
