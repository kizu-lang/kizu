package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestModuleConformanceFixture resolves and parses the basic multi-file fixture.
func TestModuleConformanceFixture(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "conformance", "modules", "basic")
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(string(source))
	if err != nil {
		t.Fatalf("parse manifest failed: %v", err)
	}
	graph, err := ResolveModules(root, manifest)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	got := modulePaths(graph.Modules)
	want := []string{"app", "app::lexer", "app::parser::ast", "app::token"}
	if !sameStrings(got, want) {
		t.Fatalf("got modules %#v, want %#v", got, want)
	}
	for _, module := range graph.Modules {
		parseConformanceModule(t, module)
	}
	if err := CheckGraph(graph); err != nil {
		t.Fatalf("check graph failed: %v", err)
	}
}

// parseConformanceModule checks that one fixture source is valid Kizu syntax.
func parseConformanceModule(t *testing.T, module Module) {
	t.Helper()
	source, err := os.ReadFile(module.File)
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New(string(source)))
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors in %s: %v", module.Path, p.Errors())
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

// TestCheckGraphRejectsMissingImport checks imported modules must exist.
func TestCheckGraphRejectsMissingImport(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::missing;

fn main(value: missing::Token) -> void {
    return;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "missing import `app::missing`") {
		t.Fatalf("got error %v, want missing import", err)
	}
}

// TestCheckGraphRejectsDuplicateImportAlias checks ambiguous last segments.
func TestCheckGraphRejectsDuplicateImportAlias(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;
import app::parser::lexer;

fn main(value: lexer::Token) -> void {
    return;
}
`,
		"src/lexer.kizu":        "pub struct Token { pub value: i64; }",
		"src/parser/lexer.kizu": "pub struct Token { pub value: i64; }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "duplicate import alias `lexer`") {
		t.Fatalf("got error %v, want duplicate import alias", err)
	}
}

// TestCheckGraphRejectsDuplicateFunction checks local declaration collisions.
func TestCheckGraphRejectsDuplicateFunction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `fn parse() -> void {
    return;
}

fn parse() -> void {
    return;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "duplicate function `app::parse`") {
		t.Fatalf("got error %v, want duplicate function", err)
	}
}

// TestCheckGraphRejectsImportCycle checks dependency cycles are rejected early.
func TestCheckGraphRejectsImportCycle(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

fn main() -> void {
    return;
}
`,
		"src/lexer.kizu": `import app;

pub struct Token {
    pub kind: i64;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("got error %v, want import cycle", err)
	}
}

// TestCheckGraphRejectsImportShadowing checks local names cannot hide imports.
func TestCheckGraphRejectsImportShadowing(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

struct lexer {
    pub value: i64;
}

fn main() -> void {
    return;
}
`,
		"src/lexer.kizu": "pub struct Token { pub value: i64; }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "declaration `lexer` shadows import") {
		t.Fatalf("got error %v, want import shadowing", err)
	}
}

// TestCheckGraphRejectsPrivateImportedType checks module visibility boundaries.
func TestCheckGraphRejectsPrivateImportedType(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

fn main(value: lexer::Token) -> void {
    return;
}
`,
		"src/lexer.kizu": "struct Token { pub value: i64; }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "type `lexer::Token` is private") {
		t.Fatalf("got error %v, want private imported type", err)
	}
}

// TestCheckGraphAllowsCrossModuleTokenReferences covers self-host token shapes.
func TestCheckGraphAllowsCrossModuleTokenReferences(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

struct Parser {
    pub current: token::Token;
}

fn accept(value: token::Token) -> token::Token {
    return value;
}

fn main() -> void {
    let current = token::make(1);
    let parser = Parser { current: current };
    let next = accept(parser.current);
    print(next.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64;
}

pub fn make(kind: i64) -> Token {
    return Token { kind: kind };
}
`,
	})
	if err := checkTempModuleGraph(t, root); err != nil {
		t.Fatalf("check graph failed: %v", err)
	}
}

// TestCheckGraphRejectsPrivateImportedFunction checks top-level visibility.
func TestCheckGraphRejectsPrivateImportedFunction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::make(1);
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64;
}

fn make(kind: i64) -> Token {
    return Token { kind: kind };
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "function `token::make` is private") {
		t.Fatalf("got error %v, want private imported function", err)
	}
}

// TestCheckGraphRejectsUnknownImportedFunction checks missing imported members.
func TestCheckGraphRejectsUnknownImportedFunction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::missing(1);
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "unknown function `token::missing`") {
		t.Fatalf("got error %v, want unknown imported function", err)
	}
}

// TestCheckGraphRejectsPrivateImportedFieldConstruction checks struct literals.
func TestCheckGraphRejectsPrivateImportedFieldConstruction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::Token { kind: 1, secret: 2 };
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64;
    secret: i64;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "field `app::token::Token.secret` is private") {
		t.Fatalf("got error %v, want private imported field construction", err)
	}
}

// TestCheckGraphRejectsPrivateImportedFieldAccess checks external field reads.
func TestCheckGraphRejectsPrivateImportedFieldAccess(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::make();
    print(current.secret);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64;
    secret: i64;
}

pub fn make() -> Token {
    return Token { kind: 1, secret: 2 };
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "field `app::token::Token.secret` is private") {
		t.Fatalf("got error %v, want private imported field access", err)
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

// moduleFixture writes a minimal package manifest and source files.
func moduleFixture(t *testing.T, sources map[string]string) string {
	t.Helper()
	root := t.TempDir()
	manifest := `[package]
name = "app"
version = "0.2.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`
	writeFileContent(t, root, "kizu.toml", manifest)
	for path, source := range sources {
		writeFileContent(t, root, path, source)
	}
	return root
}

// writeFileContent creates one source file with contents under root.
func writeFileContent(t *testing.T, root string, rel string, source string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkTempModuleGraph parses the temp manifest, resolves modules, and checks it.
func checkTempModuleGraph(t *testing.T, root string) error {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(string(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ResolveModules(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return CheckGraph(graph)
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
