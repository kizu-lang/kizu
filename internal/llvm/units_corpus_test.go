package llvm

import (
	"bytes"
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

// unitsCorpusDir holds packages both compilers split into units: one file
// each, holding every module of a package `app` and ending in a comment block
// that states the units the emitter wrote for it. The selfhost emitter reads
// the same files.
const unitsCorpusDir = "../../compiler/tests/units"

// unitsCorpusMarker separates the input from the expectation block.
const unitsCorpusMarker = "\n\n// units\n"

// unitsModuleMarker starts each module of a case: the rest of the line is
// the module's path.
const unitsModuleMarker = "// module "

// TestUnitsCorpus checks every units corpus case against this emitter and,
// with -update, rewrites each expectation block from what it does now.
func TestUnitsCorpus(t *testing.T) {
	entries, err := os.ReadDir(unitsCorpusDir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".kizu" {
			continue
		}
		path := filepath.Join(unitsCorpusDir, entry.Name())
		t.Run(strings.TrimSuffix(entry.Name(), ".kizu"), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			index := strings.LastIndex(string(data), unitsCorpusMarker)
			if index < 0 {
				t.Fatalf("%s: no %q marker", path, strings.TrimSpace(unitsCorpusMarker))
			}
			input, want := string(data[:index]), string(data[index+len(unitsCorpusMarker):])
			got := renderUnitsCase(input)
			if *updateCorpus {
				if err := os.WriteFile(path, []byte(input+unitsCorpusMarker+got), 0o644); err != nil {
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

// renderUnitsCase loads input as the package `app`, lowers it, and renders
// the units a debug build splits it into: `// unit <module>` before each, the
// program unit's path being empty, then one `// ` line per line of its text.
// The units the same program is split into for a ThinLTO link follow, each
// after `// lto unit <module>`. The std functions are dropped first, as in
// the LLVM corpus, so std has no unit and a user unit declares the std
// functions it calls. A failure yields its message as the block's only line.
func renderUnitsCase(input string) string {
	var out bytes.Buffer
	for _, split := range []struct {
		mode   Split
		header string
	}{{PerModule, "// unit "}, {PerModuleLTO, "// lto unit "}} {
		units, err := splitUnitsCase(input, split.mode)
		if err != nil {
			out.Reset()
			out.WriteString("// ")
			writeFoldedLine(&out, err.Error())
			return out.String()
		}
		for _, unit := range units {
			out.WriteString(split.header + unit.Module + "\n")
			for _, line := range strings.Split(unit.Text, "\n") {
				out.WriteString("// ")
				out.WriteString(line)
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}

// splitUnitsCase runs the front end over the package a case holds and splits
// its user functions into units.
func splitUnitsCase(input string, split Split) ([]Unit, error) {
	graph, sources := unitsCaseGraph(input)
	program, err := project.LoadProgramWithSources(graph, sources)
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
	user := userModule(module)
	user.Modules = module.Modules
	return EmitUnits(user, false, ArchArm64, split)
}

// unitsCaseGraph reads the modules of a case: each starts at a `// module`
// line, which stays in its text, and is one file named after its path.
func unitsCaseGraph(input string) (project.Graph, map[string]string) {
	graph := project.Graph{PackageName: "app"}
	sources := map[string]string{}
	for _, part := range strings.SplitAfter(input, "\n") {
		if path, ok := strings.CutPrefix(part, unitsModuleMarker); ok {
			path = strings.TrimSpace(path)
			file := strings.ReplaceAll(path, "::", "/") + ".kizu"
			graph.Modules = append(graph.Modules, project.Module{Path: path, Files: []string{file}})
		}
		if len(graph.Modules) == 0 {
			continue
		}
		file := graph.Modules[len(graph.Modules)-1].Files[0]
		sources[file] += part
	}
	return graph, sources
}
