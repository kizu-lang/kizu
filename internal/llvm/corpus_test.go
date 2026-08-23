package llvm

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

var updateCorpus = flag.Bool("update", false,
	"rewrite the llvm corpus expectations from this emitter")

// corpusDir holds single-file programs both compilers emit LLVM IR for: one
// source file each, ending in a comment block that states the text the
// emitter produced for the lowered module and for the optimized one. The
// selfhost emitter reads the same files.
const corpusDir = "../../compiler/tests/llvm"

// corpusMarker separates the input from the expectation block.
const corpusMarker = "\n\n// llvm\n"

// TestLLVMCorpus checks every corpus case against this emitter and, with
// -update, rewrites each expectation block from what it does now.
func TestLLVMCorpus(t *testing.T) {
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
			got := renderLLVMCase(input)
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

// renderLLVMCase loads input as a program outside any package and renders
// the expectation block: `// emit:` with one line per line of the LLVM IR
// emitted for the lowered module, then `// opt:` with the same for a fresh
// lowering after Optimize. A program the front end rejects yields one
// `check failed:` line per section; a lowering, optimizing or emitting
// error yields its message as the section's only line. The std functions a
// case pulls in are dropped before emitting, the way the IR corpus lists no
// std function: the types std declares stay, as the header names them.
func renderLLVMCase(input string) string {
	var out bytes.Buffer
	out.WriteString("// emit:\n")
	writeLLVMSection(&out, input, false)
	out.WriteString("// opt:\n")
	writeLLVMSection(&out, input, true)
	return out.String()
}

// writeLLVMSection lowers input, optimizes the module when opt is set, and
// writes the emitted text of the user functions one `// ` line per line.
func writeLLVMSection(out *bytes.Buffer, input string, opt bool) {
	module, err := lowerCorpusInput(input, opt)
	if err != nil {
		out.WriteString("// ")
		writeFoldedLine(out, err.Error())
		return
	}
	text, err := Emit(userModule(module))
	if err != nil {
		out.WriteString("// ")
		writeFoldedLine(out, err.Error())
		return
	}
	for _, line := range strings.Split(text, "\n") {
		out.WriteString("// ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
}

// lowerCorpusInput runs the front end gates in CLI order (load, type check,
// ownership check) and lowers the program, optimizing it when opt is set.
func lowerCorpusInput(input string, opt bool) (*ir.Module, error) {
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
	module, err := ir.Lower(program, checker.Result())
	if err != nil {
		return nil, err
	}
	if opt {
		if err := ir.Optimize(module); err != nil {
			return nil, err
		}
	}
	return module, nil
}

// userModule returns the module with its std functions dropped: a std
// function carries the std path as its symbol, and so does every instance
// of a std generic. The declared types stay, as the module header lists
// them whether a function reaches them or not.
func userModule(module *ir.Module) *ir.Module {
	var kept []*ir.Function
	for _, fn := range module.Functions {
		if strings.HasPrefix(fn.Name, "std::") {
			continue
		}
		kept = append(kept, fn)
	}
	return &ir.Module{
		Structs:   module.Structs,
		Enums:     module.Enums,
		ErrorSets: module.ErrorSets,
		Unions:    module.Unions,
		Functions: kept,
	}
}

// writeFoldedLine writes text as one expectation line; embedded newlines
// become `\n` so that one error stays one line.
func writeFoldedLine(out *bytes.Buffer, text string) {
	out.WriteString(strings.ReplaceAll(text, "\n", "\\n"))
	out.WriteByte('\n')
}
