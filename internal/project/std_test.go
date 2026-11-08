package project

import (
	"os"
	"reflect"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestScanErrorSetsMatchesParse checks that reading std's error sets as
// tokens finds the same sets, members and member order the parser does, file
// by file over the whole of std.
func TestScanErrorSetsMatchesParse(t *testing.T) {
	graph, err := StdGraph()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, module := range graph.Modules {
		for _, file := range module.Files {
			text, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want := parsedErrorSets(t, file, string(text))
			got := scannedErrorSets(t, string(text))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: scanned %v, parsed %v", file, got, want)
			}
			found += len(want)
		}
	}
	if found == 0 {
		t.Fatal("std declares no error sets; the comparison checked nothing")
	}
}

// TestScanErrorSetsSkipsNonDeclarations checks that `error` spelled where no
// declaration can start is not read as one.
func TestScanErrorSetsSkipsNonDeclarations(t *testing.T) {
	text := `// error Commented { A }
pub error First { A, B, }
fn f(error: i64) -> i64 {
    let error = "error Quoted { C }";
    return 0;
}
error Both = First or Second;
/// Docs.
error Second {
    /// Member docs.
    D,
    E
}
`
	want := []errorSetNames{
		{Name: "First", Members: []string{"A", "B"}},
		{Name: "Both", Members: nil},
		{Name: "Second", Members: []string{"D", "E"}},
	}
	if got := scannedErrorSets(t, text); !reflect.DeepEqual(got, want) {
		t.Fatalf("scanned %v, want %v", got, want)
	}
	if got := parsedErrorSets(t, "inline.kizu", text); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %v, want %v", got, want)
	}
}

// parsedErrorSets returns the error sets the parser finds in text.
func parsedErrorSets(t *testing.T, file, text string) []errorSetNames {
	t.Helper()
	p := parser.New(lexer.New(text))
	program := p.ParseProgram()
	if diagnostics := p.Diagnostics(); len(diagnostics) > 0 {
		t.Fatalf("%s: %v", file, diagnostics[0])
	}
	sets := []errorSetNames{}
	for _, decl := range program.Decls {
		if set, ok := decl.(*ast.ErrorSetDecl); ok {
			sets = append(sets, errorSetNames{Name: set.Name, Members: set.Members})
		}
	}
	return sets
}

// scannedErrorSets returns the error sets scanErrorSets finds in text.
func scannedErrorSets(t *testing.T, text string) []errorSetNames {
	t.Helper()
	sets, err := scanErrorSets(text)
	if err != nil {
		t.Fatal(err)
	}
	return sets
}
