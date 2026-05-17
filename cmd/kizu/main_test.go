package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goast "github.com/kizu-lang/kizu/internal/ast"
	golexer "github.com/kizu-lang/kizu/internal/lexer"
	goparser "github.com/kizu-lang/kizu/internal/parser"
	gotoken "github.com/kizu-lang/kizu/internal/token"
)

// TestRunCommandSmoke checks the CLI can execute the hello example.
func TestRunCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "hello, kizu\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestRunCommandBorrowExample checks borrow parameters preserve ownership.
func TestRunCommandBorrowExample(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/borrow.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "alice\nalice\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestRunCommandArenaExample checks the CLI can execute the arena example.
func TestRunCommandArenaExample(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/arena.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "alice\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestCheckCommandPackageFixture checks manifest-based multi-file package checks.
func TestCheckCommandPackageFixture(t *testing.T) {
	source := filepath.Join("..", "..", "tests", "conformance", "modules", "basic")
	cmd := exec.Command("go", "run", ".", "check", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "check: ok\n" {
		t.Fatalf("got %q", out)
	}
}

// TestCheckCommandStdPackage checks the compiler-owned std source skeleton.
func TestCheckCommandStdPackage(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "check", "../../std")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "check: ok\n" {
		t.Fatalf("got %q", out)
	}
}

// TestCheckCommandSelfHostPackage checks the module-first self-host scaffold.
func TestCheckCommandSelfHostPackage(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "check", "../../selfhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "check: ok\n" {
		t.Fatalf("got %q", out)
	}
}

// TestTestCommandSelfHostPackage checks package component test discovery.
func TestTestCommandSelfHostPackage(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "test", "../../selfhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "test: ok (9 component tests)\n" {
		t.Fatalf("got %q", out)
	}
}

// TestTestCommandPackageRuntimeFailure checks package component tests execute.
func TestTestCommandPackageRuntimeFailure(t *testing.T) {
	root := t.TempDir()
	writePackageTestFile(t, root, "kizu.toml", `[package]
name = "app"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`)
	writePackageTestFile(t, root, "src/main.kizu", `pub fn ready() -> bool {
    return true;
}
`)
	writePackageTestFile(t, root, "src/main_test.kizu", `import app;

pub fn failing_test() -> !void {
    try std::testing::expect(app::ready() == false);
    return void;
}
`)
	cmd := exec.Command("go", "run", ".", "test", root)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected package test to fail\n%s", out)
	}
	want := "test error: main_test.failing_test"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestSelfHostLexCommandMatchesGoLexer checks the bootstrap lexer command.
func TestSelfHostLexCommandMatchesGoLexer(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.kizu")
	input := `import app::lexer;
pub fn main() -> void { let name = "alice"; return void; }`
	if err := os.WriteFile(source, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "selfhost-lex", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := parseSelfHostLexOutput(t, string(out))
	want := goLexerSnapshots(input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost lexer snapshots got %#v, want %#v", got, want)
	}
}

// TestSelfHostParseCommandMatchesGoParser checks the parser bootstrap command.
func TestSelfHostParseCommandMatchesGoParser(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.kizu")
	input := `struct User { name: []const u8; }
enum Color { Red Green }
union MaybeUser { Some(User) None }
fn main() -> void { return void; }`
	if err := os.WriteFile(source, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "selfhost-parse", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := parseSelfHostParseOutput(t, string(out))
	want := goParserDeclSnapshots(t, input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost parser snapshots got %#v, want %#v", got, want)
	}
	gotDetail := parseSelfHostParserDetailOutput(t, string(out))
	wantDetail := goParserDetailSnapshot(t, input)
	if !reflect.DeepEqual(gotDetail, wantDetail) {
		t.Fatalf("selfhost parser detail got %#v, want %#v", gotDetail, wantDetail)
	}
}

// TestSelfHostParseCommandMatchesGoParserError checks negative parser parity.
func TestSelfHostParseCommandMatchesGoParserError(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.kizu")
	input := `fn main() -> void { return void }`
	if err := os.WriteFile(source, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "selfhost-parse", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := parseSelfHostParserDetailOutput(t, string(out))
	want := goParserDetailSnapshot(t, input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost parser error detail got %#v, want %#v", got, want)
	}
}

// TestParseCommandSelfHostParserSwitch checks the opt-in parser switch.
func TestParseCommandSelfHostParserSwitch(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.kizu")
	input := `fn main() -> void { return void; }`
	if err := os.WriteFile(source, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "parse", source)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_PARSER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "selfhost parser declaration snapshots") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(string(out), "selfhost parser detail snapshot") {
		t.Fatalf("got %q", out)
	}
}

// TestIRCommandSmoke checks the CLI can dump typed SSA IR.
func TestIRCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "ir", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() -> void"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestFmtCommandSmoke checks the CLI can print stable formatted Kizu source.
func TestFmtCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "fmt", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() { print(\"hello, kizu\"); }\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestIROptCommandSmoke checks the CLI can dump optimized typed SSA IR.
func TestIROptCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(source, []byte(`fn main() { print(1 + 2); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "ir", "--opt", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "%3: i64 = const 3") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(string(out), "binary.+") {
		t.Fatalf("optimized IR still contains binary.+:\n%s", out)
	}
}

// TestBuildEmitLLVMCommandSmoke checks the CLI can dump LLVM IR.
func TestBuildEmitLLVMCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "build", "--emit-llvm", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "define void @main()"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestBuildEmitLLVMOptCommandSmoke checks LLVM build can use optimized IR.
func TestBuildEmitLLVMOptCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(source, []byte(`fn main() { print(1 + 2); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "build", "--emit-llvm", "--opt", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "call void @kizu_print_int(i64 3)") {
		t.Fatalf("got %q", out)
	}
}

// TestBuildTargetWASICommandSmoke checks the CLI can dump WASI WebAssembly text.
func TestBuildTargetWASICommandSmoke(t *testing.T) {
	cmd := exec.Command(
		"go", "run", ".", "build", "--target", "wasm32-wasi", "../../examples/hello.kizu",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := `(func $_start (export "_start")`
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestBuildSelfHostPackageEmitLLVM checks package build can lower self-host sources.
func TestBuildSelfHostPackageEmitLLVM(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "build", "--emit-llvm", "../../selfhost")
	cmd.Env = append(os.Environ(), "KIZU_CACHE_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"define ptr @main()",
		"define ptr @compiler.compile_source",
		"define ptr @compiler.compile_package",
		"define ptr @compiler.compile_selfhost_package_check",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildSelfHostPackageTargetWASI checks package build emits WAT for self-host.
func TestBuildSelfHostPackageTargetWASI(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "build", "--target", "wasm32-wasi", "../../selfhost")
	cmd.Env = append(os.Environ(), "KIZU_CACHE_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{"(func $main", "(call $main)"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestCacheCommands checks cache status, why-rebuild, and prune.
func TestCacheCommands(t *testing.T) {
	cacheDir := t.TempDir()
	build := exec.Command("go", "run", ".", "build", "--emit-llvm", "../../examples/hello.kizu")
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	why := exec.Command("go", "run", ".", "why-rebuild", "../../examples/hello.kizu")
	why.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := why.CombinedOutput()
	if err != nil {
		t.Fatalf("why-rebuild failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cache hit") {
		t.Fatalf("got %q", out)
	}
	status := exec.Command("go", "run", ".", "cache", "status")
	status.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err = status.CombinedOutput()
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "entries: 1") {
		t.Fatalf("got %q", out)
	}
	prune := exec.Command("go", "run", ".", "cache", "prune")
	prune.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err = prune.CombinedOutput(); err != nil {
		t.Fatalf("prune failed: %v\n%s", err, out)
	}
}

// TestBuildOptUsesSeparateCacheEntry checks optimization level shapes cache keys.
func TestBuildOptUsesSeparateCacheEntry(t *testing.T) {
	cacheDir := t.TempDir()
	source := "../../examples/hello.kizu"
	plain := exec.Command("go", "run", ".", "build", "--emit-llvm", source)
	plain.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := plain.CombinedOutput(); err != nil {
		t.Fatalf("plain build failed: %v\n%s", err, out)
	}
	opt := exec.Command("go", "run", ".", "build", "--emit-llvm", "--opt", source)
	opt.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := opt.CombinedOutput(); err != nil {
		t.Fatalf("opt build failed: %v\n%s", err, out)
	}
	status := exec.Command("go", "run", ".", "cache", "status")
	status.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "entries: 2") {
		t.Fatalf("got %q", out)
	}
}

// TestImportCHeaderCommandSmoke checks the Phase 14 C header importer CLI.
func TestImportCHeaderCommandSmoke(t *testing.T) {
	header := filepath.Join(t.TempDir(), "c_abi.h")
	source := []byte("int puts(const char *s);\n")
	if err := os.WriteFile(header, source, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "import-c-header", header)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("import failed: %v\n%s", err, out)
	}
	want := "extern \"c\" fn puts(s: ptr<const i8>) -> i32\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestImportCHeaderCommandRejectsUnsupportedSyntax checks readable CLI errors.
func TestImportCHeaderCommandRejectsUnsupportedSyntax(t *testing.T) {
	header := filepath.Join(t.TempDir(), "bad.h")
	if err := os.WriteFile(header, []byte("int printf(const char *fmt, ...);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "import-c-header", header)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected import to fail\n%s", out)
	}
	want := "c import error: variadic functions are unsupported"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

type cliTokenSnapshot struct {
	Kind    string
	Literal string
	Start   int
	End     int
	Line    int
	Column  int
}

type cliDeclSnapshot struct {
	Kind   string
	Name   string
	Start  int
	End    int
	Line   int
	Column int
}

// parseSelfHostLexOutput parses six-line token records from selfhost-lex.
func parseSelfHostLexOutput(t *testing.T, output string) []cliTokenSnapshot {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "selfhost lexer token snapshots" {
		t.Fatalf("missing selfhost lexer snapshot header in %q", output)
	}
	snapshots := []cliTokenSnapshot{}
	for index := 1; index < len(lines); index += 6 {
		if lines[index] == "selfhost lexer token snapshots end" {
			return snapshots
		}
		if index+5 >= len(lines) {
			t.Fatalf("truncated selfhost lexer snapshot near %q", lines[index:])
		}
		snapshots = append(snapshots, cliTokenSnapshot{
			Kind:    lines[index],
			Literal: lines[index+1],
			Start:   parseSnapshotInt(t, lines[index+2]),
			End:     parseSnapshotInt(t, lines[index+3]),
			Line:    parseSnapshotInt(t, lines[index+4]),
			Column:  parseSnapshotInt(t, lines[index+5]),
		})
	}
	t.Fatal("missing selfhost lexer snapshot end marker")
	return nil
}

// parseSelfHostParseOutput parses declaration records from selfhost-parse.
func parseSelfHostParseOutput(t *testing.T, output string) []cliDeclSnapshot {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "selfhost parser declaration snapshots" {
		t.Fatalf("missing selfhost parser snapshot header in %q", output)
	}
	snapshots := []cliDeclSnapshot{}
	for index := 1; index < len(lines); index += 6 {
		if lines[index] == "selfhost parser declaration snapshots end" {
			return snapshots
		}
		if index+5 >= len(lines) {
			t.Fatalf("truncated selfhost parser snapshot near %q", lines[index:])
		}
		snapshots = append(snapshots, cliDeclSnapshot{
			Kind:   lines[index],
			Name:   lines[index+1],
			Start:  parseSnapshotInt(t, lines[index+2]),
			End:    parseSnapshotInt(t, lines[index+3]),
			Line:   parseSnapshotInt(t, lines[index+4]),
			Column: parseSnapshotInt(t, lines[index+5]),
		})
	}
	t.Fatal("missing selfhost parser snapshot end marker")
	return nil
}

// parseSelfHostParserDetailOutput returns the selected detail snapshot section.
func parseSelfHostParserDetailOutput(t *testing.T, output string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for index, line := range lines {
		if line == "selfhost parser detail snapshot" {
			return collectParserDetailLines(t, lines[index+1:])
		}
	}
	t.Fatalf("missing selfhost parser detail snapshot in %q", output)
	return nil
}

// collectParserDetailLines collects detail rows until the end marker.
func collectParserDetailLines(t *testing.T, lines []string) []string {
	t.Helper()
	snapshot := []string{}
	for _, line := range lines {
		if line == "selfhost parser detail snapshot end" {
			return snapshot
		}
		snapshot = append(snapshot, line)
	}
	t.Fatal("missing selfhost parser detail snapshot end marker")
	return nil
}

// parseSnapshotInt parses one numeric token snapshot field.
func parseSnapshotInt(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("invalid snapshot integer %q: %v", value, err)
	}
	return parsed
}

// goParserDeclSnapshots returns Go parser facts in selfhost-parse schema.
func goParserDeclSnapshots(t *testing.T, source string) []cliDeclSnapshot {
	t.Helper()
	parser := goparser.New(golexer.New(source))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		t.Fatalf("go parser errors: %v", parser.Errors())
	}
	snapshots := []cliDeclSnapshot{}
	for _, decl := range program.Decls {
		if snapshot, ok := goParserDeclSnapshot(decl); ok {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

// goParserDetailSnapshot returns selected Go AST facts in selfhost schema.
func goParserDetailSnapshot(t *testing.T, source string) []string {
	t.Helper()
	parser := goparser.New(golexer.New(source))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return []string{"status", "fail", "message", normalizeParserError(parser.Errors())}
	}
	lines := []string{"status", "pass"}
	for _, decl := range program.Decls {
		lines = append(lines, goParserDeclDetail(decl)...)
	}
	return lines
}

// goParserDeclDetail returns selected declaration details for parser parity.
func goParserDeclDetail(decl goast.Decl) []string {
	switch current := decl.(type) {
	case *goast.StructDecl:
		return goStructDeclDetail(current)
	case *goast.EnumDecl:
		return goEnumDeclDetail(current)
	case *goast.UnionDecl:
		return goUnionDeclDetail(current)
	case *goast.FunctionDecl:
		return goFunctionDeclDetail(current)
	default:
		return nil
	}
}

// goStructDeclDetail returns selected struct field facts.
func goStructDeclDetail(decl *goast.StructDecl) []string {
	lines := []string{"struct", decl.Name, "fields", strconv.Itoa(len(decl.Fields))}
	for _, field := range decl.Fields {
		lines = append(lines, "field", field.Name, field.TypeName)
	}
	return lines
}

// goEnumDeclDetail returns selected enum tag facts.
func goEnumDeclDetail(decl *goast.EnumDecl) []string {
	lines := []string{"enum", decl.Name, "tags", strconv.Itoa(len(decl.Tags))}
	for _, tag := range decl.Tags {
		lines = append(lines, "tag", tag)
	}
	return lines
}

// goUnionDeclDetail returns selected union variant facts.
func goUnionDeclDetail(decl *goast.UnionDecl) []string {
	lines := []string{"union", decl.Name, "variants", strconv.Itoa(len(decl.Variants))}
	for _, variant := range decl.Variants {
		payload := variant.Payload
		if payload == "" {
			payload = "<none>"
		}
		lines = append(lines, "variant", variant.Name, payload)
	}
	return lines
}

// goFunctionDeclDetail returns selected function signature and body facts.
func goFunctionDeclDetail(fn *goast.FunctionDecl) []string {
	lines := []string{"fn", fn.Name, "params", strconv.Itoa(len(fn.Params))}
	for _, param := range fn.Params {
		lines = append(lines, "param", param.TypeName)
	}
	lines = append(lines,
		"return", normalizeGoReturnType(fn.ReturnType),
		"returns", strconv.Itoa(countGoReturnsInBlock(fn.Body)),
	)
	lines = append(lines, goFunctionControlDetail(fn.Body)...)
	lines = append(lines, goFunctionExpressionDetail(fn.Body)...)
	return lines
}

// normalizeGoReturnType maps omitted function returns to Kizu void.
func normalizeGoReturnType(returnType string) string {
	if returnType == "" {
		return "void"
	}
	return returnType
}

// countGoReturnsInBlock counts direct return statements in one block.
func countGoReturnsInBlock(block *goast.BlockStmt) int {
	if block == nil {
		return 0
	}
	count := 0
	for _, stmt := range block.Statements {
		if _, ok := stmt.(*goast.ReturnStmt); ok {
			count++
		}
	}
	return count
}

// goFunctionControlDetail returns selected statement counts.
func goFunctionControlDetail(block *goast.BlockStmt) []string {
	counts := countGoControlsInBlock(block)
	return []string{
		"controls",
		"ifs", strconv.Itoa(counts.Ifs),
		"whiles", strconv.Itoa(counts.Whiles),
		"fors", strconv.Itoa(counts.Fors),
		"matches", strconv.Itoa(counts.Matches),
		"breaks", strconv.Itoa(counts.Breaks),
		"continues", strconv.Itoa(counts.Continues),
		"match arms", strconv.Itoa(counts.MatchArms),
	}
}

type goControlCounts struct {
	Ifs       int
	Whiles    int
	Fors      int
	Matches   int
	Breaks    int
	Continues int
	MatchArms int
}

// countGoControlsInBlock recursively counts selected control statements.
func countGoControlsInBlock(block *goast.BlockStmt) goControlCounts {
	counts := goControlCounts{}
	if block == nil {
		return counts
	}
	for _, stmt := range block.Statements {
		counts.add(countGoControlsInStatement(stmt))
	}
	return counts
}

// add accumulates control counts.
func (c *goControlCounts) add(other goControlCounts) {
	c.Ifs += other.Ifs
	c.Whiles += other.Whiles
	c.Fors += other.Fors
	c.Matches += other.Matches
	c.Breaks += other.Breaks
	c.Continues += other.Continues
	c.MatchArms += other.MatchArms
}

// countGoControlsInStatement recursively counts selected control statements.
func countGoControlsInStatement(stmt goast.Statement) goControlCounts {
	counts := goControlCounts{}
	switch current := stmt.(type) {
	case *goast.IfStmt:
		counts.Ifs++
		counts.add(countGoControlsInBlock(current.Consequence))
		counts.add(countGoControlsInBlock(current.Alternative))
	case *goast.WhileStmt:
		counts.Whiles++
		counts.add(countGoControlsInBlock(current.Body))
	case *goast.ForStmt:
		counts.Fors++
		counts.add(countGoControlsInBlock(current.Body))
	case *goast.MatchStmt:
		counts.Matches++
		counts.MatchArms += len(current.Arms)
	case *goast.BreakStmt:
		counts.Breaks++
	case *goast.ContinueStmt:
		counts.Continues++
	}
	return counts
}

// goFunctionExpressionDetail returns selected expression counts.
func goFunctionExpressionDetail(block *goast.BlockStmt) []string {
	counts := countGoExpressionsInBlock(block)
	return []string{
		"expressions",
		"locals", strconv.Itoa(counts.Locals),
		"assignments", strconv.Itoa(counts.Assignments),
		"calls", strconv.Itoa(counts.Calls),
		"field accesses", strconv.Itoa(counts.FieldAccesses),
		"struct literals", strconv.Itoa(counts.StructLiterals),
		"binary expressions", strconv.Itoa(counts.BinaryExpressions),
	}
}

type goExpressionCounts struct {
	Locals            int
	Assignments       int
	Calls             int
	FieldAccesses     int
	StructLiterals    int
	BinaryExpressions int
}

// countGoExpressionsInBlock recursively counts selected expression nodes.
func countGoExpressionsInBlock(block *goast.BlockStmt) goExpressionCounts {
	counts := goExpressionCounts{}
	if block == nil {
		return counts
	}
	for _, stmt := range block.Statements {
		counts.add(countGoExpressionsInStatement(stmt))
	}
	return counts
}

// add accumulates expression counts.
func (c *goExpressionCounts) add(other goExpressionCounts) {
	c.Locals += other.Locals
	c.Assignments += other.Assignments
	c.Calls += other.Calls
	c.FieldAccesses += other.FieldAccesses
	c.StructLiterals += other.StructLiterals
	c.BinaryExpressions += other.BinaryExpressions
}

// countGoExpressionsInStatement recursively counts selected expressions.
func countGoExpressionsInStatement(stmt goast.Statement) goExpressionCounts {
	counts := goExpressionCounts{}
	switch current := stmt.(type) {
	case *goast.LetStmt:
		counts.Locals++
		counts.add(countGoExpressionsInExpr(current.Value))
	case *goast.AssignStmt:
		counts.Assignments++
		counts.add(countGoExpressionsInExpr(current.Target))
		counts.add(countGoExpressionsInExpr(current.Value))
	case *goast.ExprStmt:
		counts.add(countGoExpressionsInExpr(current.Expr))
	case *goast.ReturnStmt:
		counts.add(countGoExpressionsInExpr(current.Value))
	case *goast.IfStmt:
		counts.add(countGoExpressionsInExpr(current.Condition))
		counts.add(countGoExpressionsInBlock(current.Consequence))
		counts.add(countGoExpressionsInBlock(current.Alternative))
	case *goast.WhileStmt:
		counts.add(countGoExpressionsInExpr(current.Condition))
		counts.add(countGoExpressionsInBlock(current.Body))
	case *goast.ForStmt:
		counts.add(countGoExpressionsInExpr(current.Start))
		counts.add(countGoExpressionsInExpr(current.End))
		counts.add(countGoExpressionsInBlock(current.Body))
	case *goast.MatchStmt:
		counts.add(countGoExpressionsInExpr(current.Value))
		for _, arm := range current.Arms {
			counts.add(countGoExpressionsInStatement(arm.Body))
		}
	}
	return counts
}

// countGoExpressionsInExpr recursively counts selected expressions.
func countGoExpressionsInExpr(expr goast.Expression) goExpressionCounts {
	counts := goExpressionCounts{}
	switch current := expr.(type) {
	case *goast.BinaryExpr:
		counts.BinaryExpressions++
		counts.add(countGoExpressionsInExpr(current.Left))
		counts.add(countGoExpressionsInExpr(current.Right))
	case *goast.CallExpr:
		counts.Calls++
		counts.add(countGoExpressionsInExpr(current.Callee))
		for _, arg := range current.Args {
			counts.add(countGoExpressionsInExpr(arg))
		}
	case *goast.FieldExpr:
		if !current.Namespace {
			counts.FieldAccesses++
		}
		counts.add(countGoExpressionsInExpr(current.Receiver))
	case *goast.StructLiteralExpr:
		counts.StructLiterals++
		for _, field := range current.Fields {
			counts.add(countGoExpressionsInExpr(field.Value))
		}
	}
	return counts
}

// normalizeParserError maps parser diagnostics into the selected self-host subset.
func normalizeParserError(errors []string) string {
	for _, item := range errors {
		if strings.Contains(item, "expected `;`") {
			return "parser error: missing semicolon"
		}
	}
	return "parser error"
}

// goParserDeclSnapshot returns one top-level declaration snapshot.
func goParserDeclSnapshot(decl goast.Decl) (cliDeclSnapshot, bool) {
	switch current := decl.(type) {
	case *goast.FunctionDecl:
		return goNamedDeclSnapshot("FunctionDecl", current.Name, current.Span), true
	case *goast.StructDecl:
		return goNamedDeclSnapshot("StructDecl", current.Name, current.Span), true
	case *goast.EnumDecl:
		return goNamedDeclSnapshot("EnumDecl", current.Name, current.Span), true
	case *goast.UnionDecl:
		return goNamedDeclSnapshot("UnionDecl", current.Name, current.Span), true
	case *goast.ContractDecl:
		return goNamedDeclSnapshot("ContractDecl", current.Name, current.Span), true
	default:
		return cliDeclSnapshot{}, false
	}
}

// goNamedDeclSnapshot builds one declaration snapshot from a Go AST span.
func goNamedDeclSnapshot(kind string, name string, span goast.Span) cliDeclSnapshot {
	return cliDeclSnapshot{
		Kind:   kind,
		Name:   name,
		Start:  span.Start,
		End:    span.End,
		Line:   span.Line,
		Column: span.Column,
	}
}

// goLexerSnapshots returns the Go oracle in the selfhost-lex output schema.
func goLexerSnapshots(source string) []cliTokenSnapshot {
	lexer := golexer.New(source)
	snapshots := []cliTokenSnapshot{}
	for {
		current := lexer.NextToken()
		snapshots = append(snapshots, cliTokenSnapshot{
			Kind:    selfHostTokenKind(current.Type),
			Literal: current.Literal,
			Start:   current.Start,
			End:     current.End,
			Line:    current.Line,
			Column:  current.Column,
		})
		if current.Type == gotoken.EOF {
			return snapshots
		}
	}
}

// selfHostTokenKind maps Go lexer token types to Kizu enum display names.
func selfHostTokenKind(kind gotoken.Type) string {
	names := map[gotoken.Type]string{
		gotoken.Illegal:     "Illegal",
		gotoken.EOF:         "Eof",
		gotoken.Ident:       "Ident",
		gotoken.Int:         "Number",
		gotoken.String:      "String",
		gotoken.Assign:      "Assign",
		gotoken.Plus:        "Plus",
		gotoken.Minus:       "Minus",
		gotoken.Bang:        "Bang",
		gotoken.Question:    "Question",
		gotoken.Amp:         "Amp",
		gotoken.Asterisk:    "Asterisk",
		gotoken.Slash:       "Slash",
		gotoken.Percent:     "Percent",
		gotoken.Eq:          "Eq",
		gotoken.FatArrow:    "FatArrow",
		gotoken.NotEq:       "NotEq",
		gotoken.LT:          "LT",
		gotoken.LTE:         "LTE",
		gotoken.GT:          "GT",
		gotoken.GTE:         "GTE",
		gotoken.Arrow:       "Arrow",
		gotoken.Dot:         "Dot",
		gotoken.Range:       "Range",
		gotoken.DoubleColon: "DoubleColon",
		gotoken.Comma:       "Comma",
		gotoken.Colon:       "Colon",
		gotoken.Semicolon:   "Semicolon",
		gotoken.Pipe:        "Pipe",
		gotoken.LParen:      "LParen",
		gotoken.RParen:      "RParen",
		gotoken.LBrace:      "LBrace",
		gotoken.RBrace:      "RBrace",
		gotoken.LBracket:    "LBracket",
		gotoken.RBracket:    "RBracket",
		gotoken.Import:      "Import",
		gotoken.Public:      "Public",
		gotoken.Function:    "Fn",
		gotoken.Let:         "Let",
		gotoken.Var:         "Var",
		gotoken.Return:      "Return",
		gotoken.If:          "If",
		gotoken.Else:        "Else",
		gotoken.While:       "While",
		gotoken.Break:       "Break",
		gotoken.Continue:    "Continue",
		gotoken.Match:       "Match",
		gotoken.Struct:      "Struct",
		gotoken.Enum:        "Enum",
		gotoken.Union:       "Union",
		gotoken.Contract:    "Contract",
		gotoken.Satisfy:     "Satisfy",
		gotoken.For:         "For",
		gotoken.Impl:        "Impl",
		gotoken.True:        "True",
		gotoken.False:       "False",
		gotoken.Mut:         "Mut",
		gotoken.Unsafe:      "Unsafe",
		gotoken.Extern:      "Extern",
		gotoken.Comptime:    "Comptime",
		gotoken.Try:         "Try",
	}
	if name, ok := names[kind]; ok {
		return "token.TokenKind::" + name
	}
	return "token.TokenKind::Illegal"
}

// writePackageTestFile writes one file in a temporary package fixture.
func writePackageTestFile(t *testing.T, root string, rel string, source string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWhyRebuildChangedSource checks CLI rebuild reasons after a single-file edit.
func TestWhyRebuildChangedSource(t *testing.T) {
	cacheDir := t.TempDir()
	source := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(source, []byte(`fn main() { print("hello"); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "run", ".", "build", "--emit-llvm", source)
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	if err := os.WriteFile(source, []byte(`fn main() { print("changed"); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	why := exec.Command("go", "run", ".", "why-rebuild", source)
	why.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := why.CombinedOutput()
	if err != nil {
		t.Fatalf("why-rebuild failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "source changed") {
		t.Fatalf("got %q", out)
	}
}

// TestRunCommandRejectsMoveError checks run does not bypass static move checks.
func TestRunCommandRejectsMoveError(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/move_error.kizu")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	want := "move error: moved value `name` was used"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}
