package project

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveModules maps source files to package module paths.
func TestResolveModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.kizu")
	writeFile(t, root, "src/lexer.kizu")
	writeFile(t, root, "src/parser/mod.kizu")
	writeFile(t, root, "src/parser/ast.kizu")

	graph, err := ResolveModules(root, Manifest{
		PackageName: "app",
		Root:        "src/main.kizu",
		Paths:       []string{"src"},
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	got := modulePaths(graph.Modules)
	want := []string{"app", "app::lexer", "app::parser", "app::parser::ast"}
	if !sameStrings(got, want) {
		t.Fatalf("got modules %#v, want %#v", got, want)
	}
}

// TestResolveModulesRejectsDuplicateModulePaths checks mod/file collisions.
func TestResolveModulesRejectsDuplicateModulePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.kizu")
	writeFile(t, root, "src/parser.kizu")
	writeFile(t, root, "src/parser/mod.kizu")

	_, err := ResolveModules(root, Manifest{
		PackageName: "app",
		Root:        "src/main.kizu",
		Paths:       []string{"src"},
	})
	if err == nil {
		t.Fatal("expected duplicate module error")
	}
}

// writeFile creates an empty source file under root.
func writeFile(t *testing.T, root string, rel string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// modulePaths returns only module path strings.
func modulePaths(modules []Module) []string {
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		paths = append(paths, module.Path)
	}
	return paths
}

// sameStrings reports whether two string slices match exactly.
func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
