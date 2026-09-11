package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDerivedCorpusCasesMatchTheirSources checks that a corpus case copied
// from an example (`ex_<name>`) still carries that source. The copy is what
// the selfhost compiler is compared against on a real program, and a copy
// that drifts pins the output of a program nobody runs any more.
//
// The behavior tests have no copies: TestSelfhostFrontend compares the two
// compilers on the whole tests/behavior package, which covers every module
// in it at once.
func TestDerivedCorpusCasesMatchTheirSources(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, corpus := range []string{"check", "ir", "llvm"} {
		dir := filepath.Join(root, "compiler", "tests", corpus)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		marker := "\n\n// " + corpus + "\n"
		for _, entry := range entries {
			name := entry.Name()
			source := derivedSource(root, name)
			if source == "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			index := strings.LastIndex(string(data), marker)
			if index < 0 {
				t.Fatalf("%s/%s: no %q marker", corpus, name, strings.TrimSpace(marker))
			}
			want, err := os.ReadFile(source)
			if err != nil {
				t.Fatalf("%s/%s: its source is gone: %v", corpus, name, err)
			}
			if string(data[:index]) != strings.TrimRight(string(want), "\n") {
				t.Errorf("%s/%s differs from %s; copy the source over its input "+
					"and rerun the corpus with -update", corpus, name, source)
			}
		}
	}
}

// derivedSource names the file a corpus case was copied from, or "" for a
// case written for the corpus itself.
func derivedSource(root string, name string) string {
	stem := strings.TrimSuffix(name, ".kizu")
	if strings.HasPrefix(stem, "ex_") {
		return filepath.Join(root, "examples", stem[len("ex_"):]+".kizu")
	}
	return ""
}
