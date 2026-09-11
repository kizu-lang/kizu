package ir

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

var updateCorpus = flag.Bool("update", false,
	"rewrite the ir corpus expectations from this lowerer and optimizer")

// corpusDir holds single-file programs both compilers lower to typed SSA IR:
// one source file each, ending in a comment block that states what the
// lowerer produced and what the optimizer made of it. The selfhost pipeline
// reads the same files.
const corpusDir = "../../compiler/tests/ir"

// corpusMarker separates the input from the expectation block.
const corpusMarker = "\n\n// ir\n"

// TestIRCorpus checks every corpus case against this lowerer and optimizer
// and, with -update, rewrites each expectation block from what they do now.
func TestIRCorpus(t *testing.T) {
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
			got := renderIRCase(input)
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

// renderIRCase loads input as a program outside any package and renders the
// expectation block: `// lower:` with one line per line of the lowered
// module's dump, then `// opt:` with the dump of the same module after
// Optimize, the way the llvm corpus renders its two sections -- the front
// end, the costly half, runs once. A program the front end or the lowerer
// rejects yields the same line under both headers; one Optimize rejects
// yields its message under `// opt:` alone. Std functions are lowered but
// not listed, the way the check corpus lists no std declaration: an
// `import std;` lowers the whole library, and repeating it in every case
// would say nothing the case is about.
func renderIRCase(input string) string {
	var out bytes.Buffer
	module, err := lowerCorpusInput(input)
	if err != nil {
		for _, header := range []string{"// lower:\n", "// opt:\n"} {
			out.WriteString(header)
			out.WriteString("// ")
			writeFoldedLine(&out, err.Error())
		}
		return out.String()
	}
	out.WriteString("// lower:\n")
	writeIRDump(&out, module)
	out.WriteString("// opt:\n")
	if err := Optimize(module); err != nil {
		out.WriteString("// ")
		writeFoldedLine(&out, err.Error())
		return out.String()
	}
	writeIRDump(&out, module)
	return out.String()
}

// writeIRDump writes the dump of the user functions one `// ` line per dump
// line. A module with no user function writes no lines, so a section can be
// just its header.
func writeIRDump(out *bytes.Buffer, module *Module) {
	dump := Dump(&Module{Functions: userFunctions(module)})
	if dump == "" {
		return
	}
	for _, line := range strings.Split(dump, "\n") {
		out.WriteString("// ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
}

// lowerCorpusInput runs the front end gates in CLI order (load, type check,
// ownership check) and lowers the program. A front end failure is reported
// as a check failure so that the expectation block tells a corpus input that
// never reached the lowerer from one the lowerer rejected.
func lowerCorpusInput(input string) (*Module, error) {
	program, err := project.LoadSource("", input)
	if err != nil {
		return nil, fmt.Errorf("check failed: %w", err)
	}
	if err := types.New().Check(program); err != nil {
		return nil, fmt.Errorf("check failed: %w", err)
	}
	checker := ownership.New()
	if err := checker.Check(program); err != nil {
		return nil, fmt.Errorf("check failed: %w", err)
	}
	return Lower(program, checker.Result())
}

// userFunctions lists the module's functions that did not come from the
// standard library, in module order. A std function carries the std path as
// its symbol, and so does every instance of a std generic.
func userFunctions(module *Module) []*Function {
	var kept []*Function
	for _, fn := range module.Functions {
		if strings.HasPrefix(fn.Name, "std::") {
			continue
		}
		kept = append(kept, fn)
	}
	return kept
}

// writeFoldedLine writes text as one expectation line; embedded newlines
// become `\n` so that one error stays one line.
func writeFoldedLine(out *bytes.Buffer, text string) {
	out.WriteString(strings.ReplaceAll(text, "\n", "\\n"))
	out.WriteByte('\n')
}
