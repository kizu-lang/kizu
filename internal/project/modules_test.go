package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/types"
)

// TestModuleConformanceFixture resolves, parses, and checks the basic fixture.
func TestModuleConformanceFixture(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "conformance", "modules", "basic")
	pkg, err := LoadPackage(root)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	got := parsedModulePaths(pkg.Modules)
	want := []string{"app", "app::lexer", "app::parser::ast"}
	if !sameStrings(got, want) {
		t.Fatalf("got modules %#v, want %#v", got, want)
	}
	for _, module := range pkg.Modules {
		checkParsedModule(t, module)
	}
}

// TestResolvePackageRejectsMissingImport checks unknown module imports.
func TestResolvePackageRejectsMissingImport(t *testing.T) {
	root := packageFixture(t, map[string]string{
		"src/main.kizu": `import app::missing;
fn main() -> void { return; }`,
	})
	_, err := LoadPackage(root)
	requireErrorContains(t, err, "missing module `app::missing`")
}

// TestResolvePackageRejectsDuplicateImportName checks same last-segment imports.
func TestResolvePackageRejectsDuplicateImportName(t *testing.T) {
	root := packageFixture(t, map[string]string{
		"src/main.kizu": `import app::left::ast;
import app::right::ast;
fn main() -> void { return; }`,
		"src/left/ast.kizu":  `pub fn left() -> void { return; }`,
		"src/right/ast.kizu": `pub fn right() -> void { return; }`,
	})
	_, err := LoadPackage(root)
	requireErrorContains(t, err, "imports `app::left::ast` and `app::right::ast` as `ast`")
}

// TestResolvePackageRejectsImportCycles checks direct import cycles.
func TestResolvePackageRejectsImportCycles(t *testing.T) {
	root := packageFixture(t, map[string]string{
		"src/main.kizu":  `import app::lexer; fn main() -> void { return; }`,
		"src/lexer.kizu": `import app; pub fn lex() -> void { return; }`,
	})
	_, err := LoadPackage(root)
	requireErrorContains(t, err, "cyclic import")
}

// TestResolvePackageRejectsImportShadowing checks declaration/import name clashes.
func TestResolvePackageRejectsImportShadowing(t *testing.T) {
	root := packageFixture(t, map[string]string{
		"src/main.kizu":  `import app::lexer; fn lexer() -> void { return; }`,
		"src/lexer.kizu": `pub fn lex() -> void { return; }`,
	})
	_, err := LoadPackage(root)
	requireErrorContains(t, err, "shadows an import")
}

// checkParsedModule runs static checks for one parsed module.
func checkParsedModule(t *testing.T, module ParsedModule) {
	t.Helper()
	if err := types.New().Check(module.Program); err != nil {
		t.Fatalf("type check failed in %s: %v", module.Module.Path, err)
	}
	if err := ownership.New().Check(module.Program); err != nil {
		t.Fatalf("ownership check failed in %s: %v", module.Module.Path, err)
	}
}

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

// packageFixture creates a manifest package with the supplied source files.
func packageFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTextFile(t, root, "kizu.toml", `[package]
name = "app"
version = "0.3.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`)
	for rel, source := range files {
		writeTextFile(t, root, rel, source)
	}
	return root
}

// writeTextFile creates a source file with content under root.
func writeTextFile(t *testing.T, root string, rel string, source string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
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

// parsedModulePaths returns only parsed module path strings.
func parsedModulePaths(modules []ParsedModule) []string {
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		paths = append(paths, module.Module.Path)
	}
	return paths
}

// requireErrorContains checks the error includes a stable diagnostic fragment.
func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got error %q, want substring %q", err, want)
	}
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
