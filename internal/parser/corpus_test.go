package parser

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
)

var updateCorpus = flag.Bool("update", false,
	"rewrite the parser corpus expectations from this parser")

// corpusDir holds the parser cases both compilers run: one source file each,
// ending in a comment block that states the diagnostics and rendered program
// this parser produces for it. The selfhost parser reads the same files.
const corpusDir = "../../compiler/tests/parser"

// corpusMarker separates the input from the expectation block.
const corpusMarker = "\n\n// parse\n"

// TestParserCorpus checks every corpus case against this parser and, with
// -update, rewrites each expectation block from what the parser does now.
func TestParserCorpus(t *testing.T) {
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
			input, want, ok := splitCorpusCase(string(data))
			if !ok {
				t.Fatalf("%s: no %q marker", path, strings.TrimSpace(corpusMarker))
			}
			got := renderCorpusCase(input)
			if *updateCorpus {
				if err := os.WriteFile(path, []byte(input+corpusMarker+got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if got != want {
				t.Errorf("%s: expectation differs from parser\n--- want\n%s--- got\n%s", path, want, got)
			}
		})
	}
}

// splitCorpusCase separates the source input from the expectation block.
func splitCorpusCase(data string) (input, expectation string, ok bool) {
	index := strings.LastIndex(data, corpusMarker)
	if index < 0 {
		return "", "", false
	}
	return data[:index], data[index+len(corpusMarker):], true
}

// renderCorpusCase parses input and renders the expectation block. A failed
// parse yields one `// L:C-L:C message` line per diagnostic under
// `// diagnostics:`; the partial program is not part of the contract (the CLI
// and the LSP both drop it), so it is not rendered. A clean parse yields the
// rendered program under `// ast:`.
func renderCorpusCase(input string) string {
	p := New(lexer.New(input))
	program := p.ParseProgram()
	var out bytes.Buffer
	if diags := p.Diagnostics(); len(diags) > 0 {
		out.WriteString("// diagnostics:\n")
		for _, d := range diags {
			fmt.Fprintf(&out, "// %d:%d-%d:%d %s\n",
				d.Span.Start.Line, d.Span.Start.Column,
				d.Span.End.Line, d.Span.End.Column, d.Message)
		}
		return out.String()
	}
	out.WriteString("// ast:\n")
	for _, line := range strings.Split(program.String(), "\n") {
		out.WriteString("// ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}
