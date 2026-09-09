package fmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSourcesAreFormatted checks every checked-in Kizu source against the
// formatter, so that `kizu fmt` and the tree never disagree about the
// canonical form. Negative examples are left out: some of them are wrong on
// purpose in ways the formatter is not meant to read.
func TestSourcesAreFormatted(t *testing.T) {
	root := filepath.Join("..", "..")
	roots := []string{
		"lib/kizu/std/src",
		"examples",
		"tests/behavior/src",
		"compiler/src",
	}
	var unformatted []string
	for _, dir := range roots {
		start := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(start, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if entry.IsDir() {
				if rel == "examples/negative" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".kizu") {
				return nil
			}
			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if Format(string(source)) != string(source) {
				unformatted = append(unformatted, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(unformatted) > 0 {
		t.Fatalf("%d files differ from `kizu fmt`; run `kizu fmt --write` on:\n%s",
			len(unformatted), strings.Join(unformatted, "\n"))
	}
}
