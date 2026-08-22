package project

import (
	"fmt"
	"github.com/kizu-lang/kizu/internal/manifest"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Module identifies the ordered source files in one directory module.
type Module struct {
	Path      string
	Files     []string
	TestFiles []string
}

// Graph is the deterministic module list resolved from a manifest.
type Graph struct {
	PackageName string
	Modules     []Module
}

// ResolveModules maps configured source files to Kizu module paths.
//
// A module named after the package itself is not required. `golang.org/x/tools`
// has no package at its root and std has none either: every std module is
// `std::mem` or below, and nothing is spelled plain `std`. The package name is
// still a namespace its own modules reach each other through, whether or not a
// module answers to it.
func ResolveModules(baseDir string, manifest manifest.Manifest) (Graph, error) {
	modules := map[string]*Module{}
	seenFiles := map[string]string{}
	for _, sourceRoot := range manifest.Paths {
		root := filepath.Clean(filepath.Join(baseDir, sourceRoot))
		if err := collectSourceRoot(modules, seenFiles, manifest.PackageName, root); err != nil {
			return Graph{}, err
		}
	}
	return Graph{PackageName: manifest.PackageName, Modules: sortedModules(modules)}, nil
}

// collectSourceRoot walks one source root and groups Kizu files by directory.
func collectSourceRoot(
	modules map[string]*Module,
	seenFiles map[string]string,
	packageName string,
	sourceRoot string,
) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return nil
		}
		modulePath, err := sourceModulePath(packageName, sourceRoot, path)
		if err != nil {
			return err
		}
		cleanPath := filepath.Clean(path)
		if previous, exists := seenFiles[cleanPath]; exists {
			if previous == modulePath {
				return nil
			}
			return fmt.Errorf("module error: source `%s` belongs to both `%s` and `%s`",
				cleanPath, previous, modulePath)
		}
		seenFiles[cleanPath] = modulePath
		module := modules[modulePath]
		if module == nil {
			module = &Module{Path: modulePath}
			modules[modulePath] = module
		}
		if IsTestFile(cleanPath) {
			module.TestFiles = append(module.TestFiles, cleanPath)
		} else {
			module.Files = append(module.Files, cleanPath)
		}
		return nil
	})
}

// sourceModulePath converts a source directory into a Kizu module path.
func sourceModulePath(
	packageName string,
	sourceRoot string,
	path string,
) (string, error) {
	rel, err := filepath.Rel(sourceRoot, filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return "", err
	}
	name := filepath.ToSlash(rel)
	if name == "." || name == "" {
		return packageName, nil
	}
	return packageName + "::" + strings.ReplaceAll(name, "/", "::"), nil
}

// sortedModules returns modules ordered by module path.
func sortedModules(modules map[string]*Module) []Module {
	paths := make([]string, 0, len(modules))
	for path := range modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]Module, 0, len(paths))
	for _, path := range paths {
		module := modules[path]
		sort.Strings(module.Files)
		sort.Strings(module.TestFiles)
		out = append(out, *module)
	}
	return out
}

// IsTestFile reports whether a package source joins its module only for tests.
func IsTestFile(path string) bool {
	return strings.HasSuffix(filepath.Base(path), "_test.kizu")
}

// SourceFiles returns this module's deterministic production or test source set.
func (m Module) SourceFiles(includeTests bool) []string {
	files := make([]string, 0, len(m.Files)+len(m.TestFiles))
	files = append(files, m.Files...)
	if includeTests {
		files = append(files, m.TestFiles...)
	}
	sort.Strings(files)
	return files
}

// SourceFiles returns every graph source in module and file order.
func (g Graph) SourceFiles(includeTests bool) []string {
	files := []string{}
	for _, module := range g.Modules {
		files = append(files, module.SourceFiles(includeTests)...)
	}
	return files
}

// ContainsFile reports whether path belongs to this graph in the selected mode.
func (g Graph) ContainsFile(path string, includeTests bool) bool {
	wanted := filepath.Clean(path)
	for _, file := range g.SourceFiles(includeTests) {
		if filepath.Clean(file) == wanted {
			return true
		}
	}
	return false
}

// InternalSegment names the directory a package keeps its own modules in. A
// module below one is reachable from the subtree the directory sits in and
// nowhere else, so where the source is decides what can name it and nothing has
// to be listed anywhere.
const InternalSegment = "internal"

// ReachableFrom reports whether a module at `from` may name `path`. A path with
// no `internal` segment is reachable from everywhere; one with an `internal`
// segment is reachable from the subtree that segment hangs off.
func ReachableFrom(path string, from string) bool {
	_, scope, internal := internalModule(path)
	if !internal {
		return true
	}
	return from == scope || strings.HasPrefix(from, scope+"::")
}

// internalModule returns the module an `internal` segment hides, the path that
// module is internal to, and whether path passes through one at all. It reads
// the module out of a longer path so that a name inside the module is refused
// under the module's own name rather than its own.
func internalModule(path string) (string, string, bool) {
	// Almost no path has one, and this is asked of every name a module resolves.
	if !strings.Contains(path, InternalSegment) {
		return "", "", false
	}
	parts := strings.Split(path, "::")
	for idx, part := range parts {
		if idx == 0 || part != InternalSegment {
			continue
		}
		return strings.Join(parts[:min(idx+2, len(parts))], "::"),
			strings.Join(parts[:idx], "::"), true
	}
	return "", "", false
}
