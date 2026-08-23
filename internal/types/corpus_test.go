package types

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/project"
)

var updateCorpus = flag.Bool("update", false,
	"rewrite the check corpus expectations from this loader and checker")

// corpusDir holds single-file programs both compilers load and type-check: one
// source file each, ending in a comment block that states what the loader
// produced (the qualified user declarations, or the module error) and what the
// checker said (ok, or its diagnostics). The selfhost pipeline reads the same
// files.
const corpusDir = "../../compiler/tests/check"

// corpusMarker separates the input from the expectation block.
const corpusMarker = "\n\n// check\n"

// TestCheckCorpus checks every corpus case against this loader and checker
// and, with -update, rewrites each expectation block from what they do now.
func TestCheckCorpus(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".kizu" {
			continue
		}
		path := filepath.Join(corpusDir, entry.Name())
		t.Run(strings.TrimSuffix(entry.Name(), ".kizu"), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			index := strings.LastIndex(string(data), corpusMarker)
			if index < 0 {
				t.Fatalf("%s: no %q marker", path, strings.TrimSpace(corpusMarker))
			}
			input, want := string(data[:index]), string(data[index+len(corpusMarker):])
			got := renderCheckCase(input)
			if *updateCorpus {
				if err := os.WriteFile(path, []byte(input+corpusMarker+got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if got != want {
				t.Errorf("%s: expectation differs\n--- want\n%s--- got\n%s", path, want, got)
			}
		})
	}
}

// renderCheckCase loads input as a program outside any package and renders the
// expectation block: `// load:` with the qualified user declarations (std
// declarations are loaded but not listed) or the module error, then `// check:`
// with `ok` or one line per CheckAll diagnostic, with its notes and help.
func renderCheckCase(input string) string {
	var out bytes.Buffer
	out.WriteString("// load:\n")
	program, err := project.LoadSource("", input)
	if err != nil {
		writeErrorLines(&out, err.Error())
		return out.String()
	}
	for _, decl := range program.Decls {
		if isStdDecl(decl) {
			continue
		}
		for _, line := range strings.Split(decl.String(), "\n") {
			out.WriteString("// ")
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	out.WriteString("// check:\n")
	errs := New().CheckAll(program)
	if len(errs) == 0 {
		out.WriteString("// ok\n")
		return out.String()
	}
	for _, err := range errs {
		var d *diag.Diagnostic
		if errors.As(err, &d) {
			writeDiagnostic(&out, d)
			continue
		}
		writeErrorLines(&out, err.Error())
	}
	return out.String()
}

// writeDiagnostic renders one structured diagnostic as
// `// L:C-L:C [code] category: message` followed by its notes and help.
func writeDiagnostic(out *bytes.Buffer, d *diag.Diagnostic) {
	out.WriteString("// ")
	if d.Span.IsZero() {
		out.WriteString("-")
	} else {
		fmt.Fprintf(out, "%d:%d-%d:%d", d.Span.Start.Line, d.Span.Start.Column,
			d.Span.End.Line, d.Span.End.Column)
	}
	if d.Code != "" {
		fmt.Fprintf(out, " [%s]", d.Code)
	}
	out.WriteByte(' ')
	if d.Category != "" {
		out.WriteString(d.Category)
		out.WriteString(": ")
	}
	writeFolded(out, d.Message)
	for _, note := range d.Notes {
		out.WriteString("//   note: ")
		writeFolded(out, note)
	}
	if d.Help != "" {
		out.WriteString("//   help: ")
		writeFolded(out, d.Help)
	}
}

// writeErrorLines renders an unstructured error, one `// ` line per text line.
func writeErrorLines(out *bytes.Buffer, text string) {
	out.WriteString("// ")
	writeFolded(out, text)
}

// writeFolded writes text as one expectation line; embedded newlines become
// `\n` so that one diagnostic stays one line.
func writeFolded(out *bytes.Buffer, text string) {
	out.WriteString(strings.ReplaceAll(text, "\n", "\\n"))
	out.WriteByte('\n')
}

// isStdDecl reports whether a merged declaration came from the standard
// library: std functions carry the flag, and std types carry the std path.
func isStdDecl(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return d.Std || strings.HasPrefix(d.Name, "std::")
	case *ast.StructDecl:
		return strings.HasPrefix(d.Name, "std::")
	case *ast.EnumDecl:
		return strings.HasPrefix(d.Name, "std::")
	case *ast.ErrorSetDecl:
		return strings.HasPrefix(d.Name, "std::")
	case *ast.UnionDecl:
		return strings.HasPrefix(d.Name, "std::")
	case *ast.ContractDecl:
		return strings.HasPrefix(d.Name, "std::")
	case *ast.ImplDecl:
		return strings.HasPrefix(d.ContractName, "std::") || strings.HasPrefix(d.TypeName, "std::")
	}
	return false
}
