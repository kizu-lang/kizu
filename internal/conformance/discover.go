package conformance

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Roots are the trees a case can live in: the examples, which are programs
// worth reading, and the behavior suite, which is one package of assertions.
var Roots = []string{"examples", "tests/behavior"}

// Discover returns every case declared under Roots, read from the repository
// rooted at dir. A directory with a kizu.toml is one package and therefore one
// case, so the modules inside it are not cases of their own.
func Discover(dir string) ([]Case, error) {
	paths, err := casePaths(dir)
	if err != nil {
		return nil, err
	}
	cases := make([]Case, 0, len(paths))
	for _, path := range paths {
		entry, err := read(dir, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		cases = append(cases, entry)
	}
	return cases, nil
}

// casePaths walks the roots and returns what every case is named by.
func casePaths(dir string) ([]string, error) {
	paths := []string{}
	for _, root := range Roots {
		err := filepath.WalkDir(filepath.Join(dir, root), func(
			path string, entry fs.DirEntry, err error,
		) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if !entry.IsDir() {
				if filepath.Ext(path) == ".kizu" {
					paths = append(paths, rel)
				}
				return nil
			}
			if _, err := os.Stat(filepath.Join(path, "kizu.toml")); err != nil {
				return nil
			}
			paths = append(paths, rel)
			return fs.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// read reads the case one program declares at the end of itself.
func read(dir string, path string) (Case, error) {
	blockFile, err := blockPath(dir, path)
	if err != nil {
		return Case{}, err
	}
	source, err := os.ReadFile(filepath.Join(dir, blockFile))
	if err != nil {
		return Case{}, err
	}
	lines, err := block(string(source))
	if err != nil {
		return Case{}, err
	}
	return parse(path, lines)
}

// blockPath returns the file a case is declared in. A runnable package uses
// main.kizu; a test-only package uses main_test.kizu.
func blockPath(dir string, path string) (string, error) {
	if strings.HasSuffix(path, ".kizu") {
		return path, nil
	}
	for _, name := range []string{"main.kizu", "main_test.kizu"} {
		candidate := filepath.ToSlash(filepath.Join(path, "src", name))
		info, err := os.Stat(filepath.Join(dir, candidate))
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("package has no src/main.kizu or src/main_test.kizu case file")
}
