package project

import (
	"fmt"
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
	Root    string
	Modules []Module
}

// ResolveModules maps configured source files to Kizu module paths.
func ResolveModules(baseDir string, manifest Manifest) (Graph, error) {
	rootFile := filepath.Clean(filepath.Join(baseDir, manifest.Root))
	modules := map[string]string{}
	for _, sourceRoot := range manifest.Paths {
		root := filepath.Clean(filepath.Join(baseDir, sourceRoot))
		if err := collectSourceRoot(modules, manifest, rootFile, root); err != nil {
			return Graph{}, err
		}
	}
	rootModule := manifest.PackageName
	if _, ok := modules[rootModule]; !ok {
		return Graph{}, fmt.Errorf("module error: root module `%s` was not found", manifest.Root)
	}
	return Graph{Root: rootModule, Modules: sortedModules(modules)}, nil
}

// collectSourceRoot walks one source root and records Kizu source modules.
func collectSourceRoot(
	modules map[string]string,
	manifest Manifest,
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
