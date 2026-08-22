package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/manifest"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
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
		for _, file := range module.Files {
			parseConformanceModule(t, module.Path, file)
		}
	}
	if err := loadGraph(graph); err != nil {
		t.Fatalf("check graph failed: %v", err)
	}
}

// parseConformanceModule checks that one fixture source is valid Kizu syntax.
func parseConformanceModule(t *testing.T, modulePath string, file string) {
	t.Helper()
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New(string(source)))
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors in %s: %v", modulePath, p.Errors())
	}
}

// TestResolveModules groups directory files into package module paths.
func TestResolveModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.kizu")
	writeFile(t, root, "src/cli.kizu")
	writeFile(t, root, "src/lexer/lexer.kizu")
	writeFile(t, root, "src/parser/mod.kizu")
	writeFile(t, root, "src/parser/helper.kizu")
	writeFile(t, root, "src/parser/parser_test.kizu")
	writeFile(t, root, "src/parser/ast/ast.kizu")

	graph, err := ResolveModules(root, manifest.Manifest{
		PackageName: "app",
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
	if len(graph.Modules[0].Files) != 2 || len(graph.Modules[2].Files) != 2 ||
		len(graph.Modules[2].TestFiles) != 1 {
		t.Fatalf("unexpected grouped modules %#v", graph.Modules)
	}
}

// TestResolveModulesWithoutRootModule checks a package needs no module of its
// own name. std has none: everything it holds is `std::mem` or below.
func TestResolveModulesWithoutRootModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/lexer/lexer.kizu")
	writeFile(t, root, "src/internal/table/table.kizu")

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

// TestResolveModulesTreatsModAndMainAsOrdinaryFiles checks filenames add no namespace.
func TestResolveModulesTreatsModAndMainAsOrdinaryFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/parser/mod.kizu")
	writeFile(t, root, "src/parser/main.kizu")

	graph, err := ResolveModules(root, manifest.Manifest{
		PackageName: "app",
		Paths:       []string{"src"},
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(graph.Modules) != 1 || graph.Modules[0].Path != "app::parser" ||
		len(graph.Modules[0].Files) != 2 {
		t.Fatalf("unexpected modules %#v", graph.Modules)
	}
}

// TestModuleSourceFilesOrdersBothViews keeps declaration and test order stable.
func TestModuleSourceFilesOrdersBothViews(t *testing.T) {
	module := Module{
		Files:     []string{"z.kizu", "a.kizu"},
		TestFiles: []string{"m_test.kizu", "b_test.kizu"},
	}
	production := module.SourceFiles(false)
	if !sameStrings(production, []string{"a.kizu", "z.kizu"}) {
		t.Fatalf("got production files %#v", production)
	}
	tests := module.SourceFiles(true)
	if !sameStrings(tests, []string{"a.kizu", "b_test.kizu", "m_test.kizu", "z.kizu"}) {
		t.Fatalf("got test files %#v", tests)
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
		"src/parser/internal/table/table.kizu": "pub struct Entry { pub value: i64, }",
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
		"src/parser/ast/ast.kizu": `import app::parser::internal::table;

pub fn first(entry: table::Entry) -> i64 {
    return entry.value;
}
`,
		"src/parser/internal/table/table.kizu": "pub struct Entry { pub value: i64, }",
	})
	if err := checkTempModuleGraph(t, root); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestLoadGraphBindsPackageNamespaceInRootModule checks fully qualified siblings.
func TestLoadGraphBindsPackageNamespaceInRootModule(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `fn main() -> void {
    print(app::lexer::answer());
    return;
}
`,
		"src/lexer/lexer.kizu": `pub fn answer() -> i64 {
    return 42;
}
`,
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
		"src/lexer/lexer.kizu":        "pub struct Token { pub value: i64, }",
		"src/parser/lexer/lexer.kizu": "pub struct Token { pub value: i64, }",
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
`,
		"src/duplicate.kizu": `fn parse() -> void {
    return;
}

`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "duplicate function `app::parse`") {
		t.Fatalf("got error %v, want duplicate function", err)
	}
}

// TestLoadGraphSharesPrivateDeclarationsAcrossFiles checks directory module scope.
func TestLoadGraphSharesPrivateDeclarationsAcrossFiles(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": `import app::counter;

fn main() -> void {
    print(counter::value());
    return;
}
`,
		"src/counter/type.kizu": `struct Counter {
    value: i64,
}
`,
		"src/counter/value.kizu": `pub fn value() -> i64 {
    let counter = Counter { value: 7 };
    return counter.value;
}
`,
	})
	if err := checkTempModuleGraph(t, root); err != nil {
		t.Fatalf("check graph failed: %v", err)
	}
}

// TestLoadGraphKeepsImportsFileLocal checks one file cannot lend another an import.
func TestLoadGraphKeepsImportsFileLocal(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/main.kizu": "fn main() -> void { return; }\n",
		"src/token/token.kizu": `pub struct Token { pub value: i64, }
`,
		"src/parser/imported.kizu": `import app::token;

fn first(value: token::Token) -> i64 {
    return value.value;
}
`,
		"src/parser/missing_import.kizu": `fn second(value: token::Token) -> i64 {
    return value.value;
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "`token` is not imported") {
		t.Fatalf("got error %v, want file-local import failure", err)
	}
}

// TestLoadTestProgramAddsTestFilesToTheirModule checks Go-style test inclusion.
func TestLoadTestProgramAddsTestFilesToTheirModule(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/parser/parser.kizu": `fn answer() -> i64 { return 42; }
`,
		"src/parser/parser_test.kizu": `test "private helper is visible" {
    let _ = answer();
}
`,
	})
	graph := resolveTempModuleGraph(t, root)
	production, err := LoadProgram(graph)
	if err != nil {
		t.Fatalf("load production failed: %v", err)
	}
	if countTests(production) != 0 {
		t.Fatal("production load included a test file")
	}
	tests, err := LoadTestProgram(graph)
	if err != nil {
		t.Fatalf("load tests failed: %v", err)
	}
	if err := types.New().Check(tests); err != nil {
		t.Fatalf("check tests failed: %v", err)
	}
	if countTests(tests) != 1 {
		t.Fatalf("got %d tests, want 1", countTests(tests))
	}
}

// TestLoadTestProgramAddsTestOnlyModules checks black-box integration tests.
func TestLoadTestProgramAddsTestOnlyModules(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/parser/parser.kizu": `pub fn answer() -> i64 { return 42; }
`,
		"src/parser_integration/parser_integration_test.kizu": `import app::parser;

test "public parser API" {
    let _ = parser::answer();
}
`,
	})
	graph := resolveTempModuleGraph(t, root)
	program, err := LoadTestProgram(graph)
	if err != nil {
		t.Fatalf("load tests failed: %v", err)
	}
	if err := types.New().Check(program); err != nil {
		t.Fatalf("check tests failed: %v", err)
	}
	if countTests(program) != 1 {
		t.Fatalf("got %d tests, want 1", countTests(program))
	}
}

// TestLoadGraphKeepsUnsafeInvariantWritesInDeclarationFile preserves ADR-0089.
func TestLoadGraphKeepsUnsafeInvariantWritesInDeclarationFile(t *testing.T) {
	root := moduleFixture(t, map[string]string{
		"src/buffer/type.kizu": `/// data points to one live byte.
unsafe struct Buf {
    data: ptr<u8>,
}
`,
		"src/buffer/wrap.kizu": `fn wrap(data: ptr<u8>) -> Buf {
    // SAFETY: the caller supplies the live byte named by the type contract.
    return unsafe Buf { data: data };
}
`,
	})
	err := checkTempModuleGraph(t, root)
	if err == nil || !strings.Contains(err.Error(), "confined to its declaration file") {
		t.Fatalf("got error %v, want unsafe declaration-file boundary", err)
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
		"src/lexer/lexer.kizu": `import app;

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

fn main() -> void {
    return;
}
`,
		"src/collision.kizu": `struct lexer {
    pub value: i64,
}
`,
		"src/lexer/lexer.kizu": "pub struct Token { pub value: i64, }",
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
		"src/lexer/lexer.kizu": "struct Token { pub value: i64, }",
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
		"src/token/token.kizu": `pub struct Token {
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
		"src/token/token.kizu": `pub struct Token {
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
		"src/token/token.kizu": `pub struct Token {
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
		"src/token/token.kizu": `pub struct Token {
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
		"src/token/token.kizu": `pub struct Token {
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
		"src/token/token.kizu": `pub struct Token {
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
	graph := resolveTempModuleGraph(t, root)
	// Visibility across modules is decided by the type checker reading the
	// resolved program, so a test about it has to get that far.
	program, err := LoadProgram(graph)
	if err != nil {
		return err
	}
	return types.New().Check(program)
}

// resolveTempModuleGraph reads and resolves one temporary package fixture.
func resolveTempModuleGraph(t *testing.T, root string) Graph {
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
	return graph
}

// countTests returns the number of test declarations in a merged program.
func countTests(program *ast.Program) int {
	count := 0
	for _, decl := range program.Decls {
		if _, ok := decl.(*ast.TestDecl); ok {
			count++
		}
	}
	return count
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
