package project

import (
	"fmt"
	"github.com/kizu-lang/kizu/internal/manifest"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Module identifies one source file in a resolved package graph.
type Module struct {
	Path string
	File string
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
	rootFile := ""
	if manifest.Root != "" {
		rootFile = filepath.Clean(filepath.Join(baseDir, manifest.Root))
	}
	modules := map[string]string{}
	for _, sourceRoot := range manifest.Paths {
		root := filepath.Clean(filepath.Join(baseDir, sourceRoot))
		if err := collectSourceRoot(modules, manifest, rootFile, root); err != nil {
			return Graph{}, err
		}
	}
	return Graph{PackageName: manifest.PackageName, Modules: sortedModules(modules)}, nil
}

// collectSourceRoot walks one source root and records Kizu source modules.
func collectSourceRoot(
	modules map[string]string,
	manifest manifest.Manifest,
	rootFile string,
	sourceRoot string,
) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return nil
		}
		modulePath, err := sourceModulePath(manifest.PackageName, sourceRoot, rootFile, path)
		if err != nil {
			return err
		}
		if previous, exists := modules[modulePath]; exists {
			return fmt.Errorf("module error: duplicate module `%s`: %s and %s",
				modulePath, previous, path)
		}
		modules[modulePath] = path
		return nil
	})
}

// sourceModulePath converts one source file path into a Kizu module path.
func sourceModulePath(
	packageName string,
	sourceRoot string,
	rootFile string,
	path string,
) (string, error) {
	cleanPath := filepath.Clean(path)
	if cleanPath == rootFile {
		return packageName, nil
	}
	rel, err := filepath.Rel(sourceRoot, cleanPath)
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.ToSlash(rel), ".kizu")
	name = strings.TrimSuffix(name, "/mod")
	name = strings.TrimSuffix(name, "/main")
	if name == "" {
		return packageName, nil
	}
	return packageName + "::" + strings.ReplaceAll(name, "/", "::"), nil
}

// sortedModules returns modules ordered by module path.
func sortedModules(modules map[string]string) []Module {
	paths := make([]string, 0, len(modules))
	for path := range modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]Module, 0, len(paths))
	for _, path := range paths {
		out = append(out, Module{Path: path, File: modules[path]})
	}
	return out
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
