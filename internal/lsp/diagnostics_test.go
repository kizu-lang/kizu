package lsp

import "testing"

// TestAnalyzeReportsParsePosition checks parser diagnostics keep token positions.
func TestAnalyzeReportsParsePosition(t *testing.T) {
	diagnostics := Analyze("let x = 1;\n")
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Range.Start.Line != 0 || got.Range.Start.Character != 0 {
		t.Fatalf("got position %d:%d, want 0:0", got.Range.Start.Line, got.Range.Start.Character)
	}
	if got.Source != diagnosticSource {
		t.Fatalf("got source %q, want %q", got.Source, diagnosticSource)
	}
}

// TestAnalyzeReportsCheckDiagnostic checks semantic diagnostics reach LSP users.
func TestAnalyzeReportsCheckDiagnostic(t *testing.T) {
	source := "fn main() -> i64 { return missing; }\n"
	diagnostics := Analyze(source)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
	if diagnostics[0].Message == "" {
		t.Fatal("diagnostic message is empty")
	}
}

// TestAnalyzeAcceptsValidSource checks clean source publishes no diagnostics.
func TestAnalyzeAcceptsValidSource(t *testing.T) {
	source := "fn main() -> i64 { return 7; }\n"
	diagnostics := Analyze(source)
	if len(diagnostics) != 0 {
		t.Fatalf("got diagnostics %#v, want none", diagnostics)
	}
}
