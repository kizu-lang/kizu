package project

import (
	"github.com/kizu-lang/kizu/internal/manifest"
	"github.com/kizu-lang/kizu/internal/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestModuleFixture resolves and parses the basic multi-file fixture.
func TestModuleFixture(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "fixtures", "modules", "basic")
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifest.ParseManifest(string(source))
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
	if err := loadGraph(graph); err != nil {
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

	graph, err := ResolveModules(root, manifest.Manifest{
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

// TestResolveModulesWithoutRootModule checks a package needs no module of its
// own name. std has none: everything it holds is `std::mem` or below.
func TestResolveModulesWithoutRootModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/lexer.kizu")
	writeFile(t, root, "src/internal/table.kizu")

	graph, err := ResolveModules(root, manifest.Manifest{
		PackageName: "app",
		Paths:       []string{"src"},
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	got := modulePaths(graph.Modules)
	want := []string{"app::internal::table", "app::lexer"}
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

	_, err := ResolveModules(root, manifest.Manifest{
		PackageName: "app",
		Root:        "src/main.kizu",
		Paths:       []string{"src"},
	})
	if err == nil {
		t.Fatal("expected duplicate module error")
	}
}

// TestLoadGraphRejectsMissingImport checks imported modules must exist.
func TestLoadGraphRejectsMissingImport(t *testing.T) {
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

// TestLoadGraphRejectsInternalImportFromOutside checks a module below an
// `internal` directory is reachable from the subtree that directory hangs off
// and nowhere else. Nothing lists it: where the source sits is the whole rule.
func TestLoadGraphRejectsInternalImportFromOutside(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::parser::internal::table;

fn main(value: table::Entry) -> void {
    return;
}
`,
		"src/parser/internal/table.kizu": "pub struct Entry { pub value: i64, }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(),
		"`app::parser::internal::table` is internal to `app::parser`") {
		t.Fatalf("got error %v, want internal module", err)
	}
}

// TestLoadGraphAcceptsInternalImportInsideSubtree checks the other side of the
// same rule: the subtree the directory belongs to reaches it.
func TestLoadGraphAcceptsInternalImportInsideSubtree(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `fn main() -> void {
    return;
}
`,
		"src/parser/ast.kizu": `import app::parser::internal::table;

pub fn first(entry: table::Entry) -> i64 {
    return entry.value;
}
`,
		"src/parser/internal/table.kizu": "pub struct Entry { pub value: i64, }",
	})
	if err := checkTempModuleGraph(t, root); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestLoadGraphRejectsDuplicateImportAlias checks ambiguous last segments.
func TestLoadGraphRejectsDuplicateImportAlias(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;
import app::parser::lexer;

fn main(value: lexer::Token) -> void {
    return;
}
`,
		"src/lexer.kizu":        "pub struct Token { pub value: i64, }",
		"src/parser/lexer.kizu": "pub struct Token { pub value: i64, }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "duplicate import alias `lexer`") {
		t.Fatalf("got error %v, want duplicate import alias", err)
	}
}

// TestLoadGraphRejectsDuplicateFunction checks local declaration collisions.
func TestLoadGraphRejectsDuplicateFunction(t *testing.T) {
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

// TestLoadGraphRejectsImportCycle checks dependency cycles are rejected early.
func TestLoadGraphRejectsImportCycle(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

fn main() -> void {
    return;
}
`,
		"src/lexer.kizu": `import app;

pub struct Token {
    pub kind: i64,
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("got error %v, want import cycle", err)
	}
}

// TestLoadGraphRejectsImportShadowing checks local names cannot hide imports.
func TestLoadGraphRejectsImportShadowing(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

struct lexer {
    pub value: i64,
}

fn main() -> void {
    return;
}
`,
		"src/lexer.kizu": "pub struct Token { pub value: i64, }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "declaration `lexer` shadows import") {
		t.Fatalf("got error %v, want import shadowing", err)
	}
}

// TestLoadGraphRejectsPrivateImportedType checks module visibility boundaries.
func TestLoadGraphRejectsPrivateImportedType(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::lexer;

fn main(value: lexer::Token) -> void {
    return;
}
`,
		"src/lexer.kizu": "struct Token { pub value: i64, }",
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "type `lexer::Token` is private") {
		t.Fatalf("got error %v, want private imported type", err)
	}
}

// TestLoadGraphAllowsCrossModuleTokenReferences covers cross-module token types.
func TestLoadGraphAllowsCrossModuleTokenReferences(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

struct Parser {
    pub current: token::Token,
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
    pub kind: i64,
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

// TestLoadProgramWithSourcesPrefersOverride checks editor buffers can replace disk source.
func TestLoadProgramWithSourcesPrefersOverride(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": "let x = 1;\n",
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
}
`,
	})
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifest.ParseManifest(string(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ResolveModules(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProgram(graph); err == nil {
		t.Fatal("LoadProgram should read the invalid disk source")
	}
	openMain := `import app::token;

fn main(value: token::Token) -> void {
    return;
}
`
	if _, err := LoadProgramWithSources(graph, map[string]string{
		filepath.Join(root, "src", "main.kizu"): openMain,
	}); err != nil {
		t.Fatalf("load with source override failed: %v", err)
	}
}

// TestLoadGraphRejectsPrivateImportedFunction checks top-level visibility.
func TestLoadGraphRejectsPrivateImportedFunction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::make(1);
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
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

// TestLoadGraphRejectsUnknownImportedFunction checks missing imported members.
func TestLoadGraphRejectsUnknownImportedFunction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::missing(1);
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "unknown function `token::missing`") {
		t.Fatalf("got error %v, want unknown imported function", err)
	}
}

// TestLoadGraphRejectsPrivateImportedFieldConstruction checks struct literals.
func TestLoadGraphRejectsPrivateImportedFieldConstruction(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::Token { kind: 1, secret: 2 };
    print(current.kind);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
    secret: i64,
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "field `app::token::Token.secret` is private") {
		t.Fatalf("got error %v, want private imported field construction", err)
	}
}

// TestLoadGraphRejectsPrivateImportedFieldAccess checks external field reads.
func TestLoadGraphRejectsPrivateImportedFieldAccess(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::token;

fn main() -> void {
    let current = token::make();
    print(current.secret);
    return;
}
`,
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
    secret: i64,
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

// TestLoadGraphAllowsPrivateFieldConstructionInModuleTest checks that a test
// keeps the declaring module's visibility even though it lowers through a
// synthetic function name.
func TestLoadGraphAllowsPrivateFieldConstructionInModuleTest(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `struct Token {
    secret: i64,
}

test "construct private field" {
    let current = Token { secret: 2 };
    print(current.secret);
}
`,
	})
	if err := checkTempModuleGraph(t, root); err != nil {
		t.Fatal(err)
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
	manifest, err := manifest.ParseManifest(string(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ResolveModules(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Visibility across modules is decided by the type checker reading the
	// resolved program, so a test about it has to get that far.
	program, err := LoadProgram(graph)
	if err != nil {
		return err
	}
	return types.New().Check(program)
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

// loadGraph resolves a graph and drops the program, for tests that are about
// whether resolution accepts the graph rather than about what it produced.
func loadGraph(graph Graph) error {
	_, err := LoadProgram(graph)
	return err
}
