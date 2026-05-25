package lsp

import (
	"strings"
	"testing"
)

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
	for _, want := range []string{
		"expected declaration",
		"fn, test, import",
		"`let`",
	} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message = %q, want substring %q", got.Message, want)
		}
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

// TestAnalyzeReportsBinaryCheckDiagnosticPosition checks semantic diagnostics use checker spans.
func TestAnalyzeReportsBinaryCheckDiagnosticPosition(t *testing.T) {
	source := `enum Color { Red, Green }
enum Animal { Cat, Dog }

fn main() {
    let color = Color::Green;
    if color == Animal::Cat { return; }
}
`
	diagnostics := Analyze(source)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Range.Start.Line != 5 || got.Range.Start.Character != 13 {
		t.Fatalf("got start %d:%d, want 5:13", got.Range.Start.Line, got.Range.Start.Character)
	}
	if got.Range.End.Line != 5 || got.Range.End.Character != 15 {
		t.Fatalf("got end %d:%d, want 5:15", got.Range.End.Line, got.Range.End.Character)
	}
	for _, want := range []string{
		"operator `==` operands must have same type",
		"at 6:14",
		"left operand has type Color",
		"right operand has type Animal",
	} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message = %q, want substring %q", got.Message, want)
		}
	}
}

// TestAnalyzeReportsUnsafeCapabilityHelp checks missing unsafe caps explain the permission.
func TestAnalyzeReportsUnsafeCapabilityHelp(t *testing.T) {
	source := `fn read(p: ptr<u8>) -> u8 {
    return ptr_read(p);
}
`
	diagnostics := Analyze(source)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
	for _, want := range []string{
		"`ptr_read` requires @unsafe(ptr_read)",
		"at 2:12",
		"permits raw pointer reads with `ptr_read(p)`",
	} {
		if !strings.Contains(diagnostics[0].Message, want) {
			t.Fatalf("message = %q, want substring %q", diagnostics[0].Message, want)
		}
	}
	got := diagnostics[0].Range
	if got.Start.Line != 1 || got.Start.Character != 11 {
		t.Fatalf("got start %d:%d, want 1:11", got.Start.Line, got.Start.Character)
	}
}

// TestAnalyzeReportsUnknownNamespaceHelp checks namespace diagnostics guide imports.
func TestAnalyzeReportsUnknownNamespaceHelp(t *testing.T) {
	source := `enum Animal { Cat }
fn main() { print(Color::Red); }
`
	diagnostics := Analyze(source)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
	for _, want := range []string{
		"unknown namespace `Color`",
		"at 2:19",
		"enum/union name or import a module",
		"known namespaces: Animal",
	} {
		if !strings.Contains(diagnostics[0].Message, want) {
			t.Fatalf("message = %q, want substring %q", diagnostics[0].Message, want)
		}
	}
	got := diagnostics[0].Range
	if got.Start.Line != 1 || got.Start.Character != 18 {
		t.Fatalf("got start %d:%d, want 1:18", got.Start.Line, got.Start.Character)
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

// TestAnalyzeAcceptsStdTestingSource checks LSP diagnostics share CLI std wrappers.
func TestAnalyzeAcceptsStdTestingSource(t *testing.T) {
	source := `test "vscode test command" {
    std::testing::expect(true);
    std::testing::expect_equal<i64>(3, 1 + 2);
}
`
	diagnostics := Analyze(source)
	if len(diagnostics) != 0 {
		t.Fatalf("got diagnostics %#v, want none", diagnostics)
	}
}
