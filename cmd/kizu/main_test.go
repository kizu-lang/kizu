package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goast "github.com/kizu-lang/kizu/internal/ast"
	gobuildcache "github.com/kizu-lang/kizu/internal/buildcache"
	golexer "github.com/kizu-lang/kizu/internal/lexer"
	goparser "github.com/kizu-lang/kizu/internal/parser"
	goproject "github.com/kizu-lang/kizu/internal/project"
	gotoken "github.com/kizu-lang/kizu/internal/token"
	gotypes "github.com/kizu-lang/kizu/internal/types"
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

// TestSelfHostV2LexerMatchesGoForConformance checks module-first lexer parity.
func TestSelfHostV2LexerMatchesGoForConformance(t *testing.T) {
	for _, sourcePath := range selfHostV2ConformanceSources(t) {
		t.Run(filepath.ToSlash(sourcePath), func(t *testing.T) {
			out := runSelfHostV2Function(t, "compiler.lex_file_snapshot", sourcePath)
			got := parseSelfHostLexOutput(t, out)
			want := goLexerSnapshots(readTestSource(t, sourcePath))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("selfhost v2 lexer got %#v, want %#v", got, want)
			}
		})
	}
}

// TestSelfHostV2ParserMatchesGoForSelectedCorpus checks module-first parser parity.
func TestSelfHostV2ParserMatchesGoForSelectedCorpus(t *testing.T) {
	for _, sourcePath := range selfHostV2ParserParitySources() {
		t.Run(filepath.ToSlash(sourcePath), func(t *testing.T) {
			source := readTestSource(t, sourcePath)
			out := runSelfHostV2Function(t, "compiler.parse_file_snapshot", sourcePath)
			got := parseSelfHostParserDetailOutput(t, out)
			want := goParserDetailSnapshot(t, source)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("selfhost v2 parser detail got %#v, want %#v", got, want)
			}
		})
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

// TestSelfHostResolveCommandMatchesGoResolver checks resolver graph parity.
func TestSelfHostResolveCommandMatchesGoResolver(t *testing.T) {
	cases := []string{
		"basic",
		"imported_types",
		"missing_import",
		"private_module_access",
		"private_type_leak",
		"private_field_construction",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("..", "..", "tests", "conformance", "modules", name)
			cmd := exec.Command("go", "run", ".", "selfhost-resolve", root)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, out)
			}
			got := parseSelfHostResolverGraphOutput(t, string(out))
			want := goResolverGraphSnapshot(root)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("selfhost resolver graph got %#v, want %#v", got, want)
			}
			gotDiagnostics := parseSelfHostResolverDiagnosticsOutput(t, string(out))
			wantDiagnostics := goResolverDiagnosticSnapshots(t, resolverRootSource(root))
			if !reflect.DeepEqual(gotDiagnostics, wantDiagnostics) {
				t.Fatalf("selfhost resolver diagnostics got %#v, want %#v",
					gotDiagnostics, wantDiagnostics)
			}
		})
	}
}

// TestCheckCommandSelfHostResolverSwitch checks the opt-in resolver switch.
func TestCheckCommandSelfHostResolverSwitch(t *testing.T) {
	source := filepath.Join("..", "..", "tests", "conformance", "modules", "basic")
	cmd := exec.Command("go", "run", ".", "check", source)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_RESOLVER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "selfhost resolver module graph snapshot") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(string(out), "selfhost resolver diagnostic snapshot") {
		t.Fatalf("got %q", out)
	}
}

// TestSelfHostTypeCommandMatchesGoChecker checks selected type checker parity.
func TestSelfHostTypeCommandMatchesGoChecker(t *testing.T) {
	cases := []string{
		"../../examples/functions.kizu",
		"../../examples/variables.kizu",
		"../../examples/struct.kizu",
		"../../examples/negative/std_mem_wrong_type.kizu",
		"../../examples/negative/invalid_field.kizu",
	}
	for _, source := range cases {
		t.Run(filepath.Base(source), func(t *testing.T) {
			cmd := exec.Command("go", "run", ".", "selfhost-type", source)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, out)
			}
			gotTypes := markedOutputLines(t, string(out),
				"selfhost type snapshot",
				"selfhost type snapshot end",
			)
			if want := goTypeSnapshot(t, source); !reflect.DeepEqual(gotTypes, want) {
				t.Fatalf("selfhost type snapshot got %#v, want %#v", gotTypes, want)
			}
			gotEnv := markedOutputLines(t, string(out),
				"selfhost type env snapshot",
				"selfhost type env snapshot end",
			)
			if want := goTypeEnvSnapshot(t, source); !reflect.DeepEqual(gotEnv, want) {
				t.Fatalf("selfhost type env got %#v, want %#v", gotEnv, want)
			}
			gotCheck := markedOutputLines(t, string(out),
				"selfhost type check snapshot",
				"selfhost type check snapshot end",
			)
			if want := goTypeCheckSnapshot(t, source); !reflect.DeepEqual(gotCheck, want) {
				t.Fatalf("selfhost type check got %#v, want %#v", gotCheck, want)
			}
		})
	}
}

// TestCheckCommandSelfHostTypeSwitch checks the opt-in type checker switch.
func TestCheckCommandSelfHostTypeSwitch(t *testing.T) {
	source := "../../examples/functions.kizu"
	cmd := exec.Command("go", "run", ".", "check", source)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_TYPES=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "selfhost type check snapshot") {
		t.Fatalf("got %q", out)
	}
}

// TestSelfHostOwnershipCommandChecksMemorySafety checks the ownership switch command.
func TestSelfHostOwnershipCommandChecksMemorySafety(t *testing.T) {
	source := "../../examples/negative/moved_value.kizu"
	cmd := exec.Command("go", "run", ".", "selfhost-ownership", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := markedOutputLines(t, string(out), "ownership snapshot", "ownership snapshot end")
	want := []string{"status", "fail", "move error: moved value was used", "name", "166", "149"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost ownership snapshot got %#v, want %#v", got, want)
	}
}

// TestCheckCommandSelfHostOwnershipSwitch checks the opt-in ownership switch.
func TestCheckCommandSelfHostOwnershipSwitch(t *testing.T) {
	source := "../../examples/borrow.kizu"
	cmd := exec.Command("go", "run", ".", "check", source)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_OWNERSHIP=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := markedOutputLines(t, string(out), "ownership snapshot", "ownership snapshot end")
	want := []string{"status", "pass"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost ownership switch got %#v, want %#v", got, want)
	}
}

// TestSelfHostIRCommandDumpsNormalizedIR checks the IR switch command.
func TestSelfHostIRCommandDumpsNormalizedIR(t *testing.T) {
	source := "../../examples/functions.kizu"
	cmd := exec.Command("go", "run", ".", "selfhost-ir", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ir snapshot\nfunctions\n2\n") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(string(out), "ir dump snapshot\nstatus\npass\nfn\nadd\n") {
		t.Fatalf("got %q", out)
	}
}

// TestIRCommandSelfHostSwitch checks the opt-in IR switch.
func TestIRCommandSelfHostSwitch(t *testing.T) {
	source := "../../examples/functions.kizu"
	cmd := exec.Command("go", "run", ".", "ir", source)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_IR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ir dump snapshot\nstatus\npass\n") {
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

// TestBuildTargetNativeCommandSmoke checks native build when LLVM tools exist.
func TestBuildTargetNativeCommandSmoke(t *testing.T) {
	t.Cleanup(func() { _ = os.RemoveAll("target") })
	if _, err := exec.LookPath("llc"); err != nil {
		t.Skip("llc is not available")
	}
	if _, err := exec.LookPath("ld64.lld"); err != nil {
		t.Skip("ld64.lld is not available")
	}
	cmd := exec.Command(
		"go", "run", ".", "build", "--target", "aarch64-apple-darwin",
		"../../examples/hello.kizu",
	)
	cmd.Env = append(os.Environ(), "KIZU_CACHE_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "target/native/aarch64-apple-darwin/debug/hello"
	if strings.TrimSpace(string(out)) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestBuildTargetNativeSelfHostPath checks the selfhost artifact name.
func TestBuildTargetNativeSelfHostPath(t *testing.T) {
	t.Cleanup(func() { _ = os.RemoveAll("target") })
	if _, err := exec.LookPath("llc"); err != nil {
		t.Skip("llc is not available")
	}
	if _, err := exec.LookPath("ld64.lld"); err != nil {
		t.Skip("ld64.lld is not available")
	}
	cmd := exec.Command(
		"go", "run", ".", "build", "--target", "aarch64-apple-darwin", "../../selfhost",
	)
	cmd.Env = append(os.Environ(), "KIZU_CACHE_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "target/kizu-selfhost" {
		t.Fatalf("got %q", out)
	}
	runSelfHostArtifact(t, "check\nllvm\npass\ntrue",
		"./target/kizu-selfhost", "check", "../../examples/hello.kizu")
	runSelfHostArtifact(t, "build\naarch64-apple-darwin\npass\ntrue",
		"./target/kizu-selfhost", "build", "--target", "aarch64-apple-darwin",
		"../../examples/hello.kizu")
	runSelfHostArtifact(t, "check\nllvm\npass\ntrue",
		"./target/kizu-selfhost", "check", "../../selfhost")
	runSelfHostArtifactFailure(t, "type error: function `bad` must return i64",
		"./target/kizu-selfhost", "check", "../../examples/negative/missing_return.kizu")
	runSelfHostArtifactFailure(t, "type error: function `bad` must return i64",
		"./target/kizu-selfhost", "check", "../../examples/negative/missing_return_if.kizu")
	runSelfHostPackageRebuildSmoke(t)
	runSelfHostArtifact(t, "define void @main() { ret void }",
		"./target/kizu-selfhost", "build", "--emit-llvm", "../../examples/hello.kizu")
	runSelfHostLLVMArtifact(t, []string{
		"define ptr @compiler.compile_source(ptr %source, ptr %target_name)",
		"define ptr @compiler.compile_package(ptr %modules, ptr %target_name)",
		"define ptr @types.source_diagnostic(ptr %source)",
	},
		"./target/kizu-selfhost", "build", "--emit-llvm", "../../selfhost")
}

// runSelfHostArtifact executes the generated compiler and checks output facts.
func runSelfHostArtifact(t *testing.T, want string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost artifact failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q", out)
	}
}

// runSelfHostLLVMArtifact checks generated LLVM text and validates it with llc.
func runSelfHostLLVMArtifact(t *testing.T, wants []string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost artifact failed: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
	path := filepath.Join(t.TempDir(), "selfhost-symbols.ll")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write generated LLVM: %v", err)
	}
	requireLLVMLowers(t, path)
}

// runSelfHostArtifactFailure executes the compiler and checks a failing diagnostic.
func runSelfHostArtifactFailure(t *testing.T, want string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("selfhost artifact succeeded unexpectedly:\n%s", out)
	}
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want diagnostic containing %q", out, want)
	}
}

// runSelfHostPackageRebuildSmoke checks package rebuild artifacts.
func runSelfHostPackageRebuildSmoke(t *testing.T) {
	t.Helper()
	runSelfHostArtifact(t, "build\naarch64-apple-darwin\npass\ntrue",
		"./target/kizu-selfhost", "build", "--target", "aarch64-apple-darwin", "../../selfhost")
	requireSelfHostCompileSourceMarkers(t)
	requireSelfHostReportMarkers(t)
	requireSelfHostLexerMarkers(t)
	requireLLVMLowers(t, "target/kizu-selfhost.ll")
	runSelfHostArtifact(t, "build\nwasm32-wasi\npass\ntrue",
		"./target/kizu-selfhost", "build", "--target", "wasm32-wasi", "../../selfhost")
	requireFileContains(t, "target/kizu-selfhost.wat", "(module (func $main))")
}

// requireSelfHostCompileSourceMarkers checks source compile report emission.
func requireSelfHostCompileSourceMarkers(t *testing.T) {
	t.Helper()
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_source(ptr %source, ptr %target_name)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%frontend = call ptr @compiler.lex_and_parse_source(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%diagnostic_text = call ptr @types.source_diagnostic(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll", "%struct.ast.ParseSummary = type")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.lex_and_parse_source(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @types.source_diagnostic(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%parsed = call ptr @parser.parse_token_stream(ptr %tokens)")
	requireFileContains(t, "target/kizu-selfhost.ll", "define ptr @lexer.lex(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @parser.parse_token_stream(ptr %tokens)")
	requireFileContains(t, "target/kizu-selfhost.ll", "store i1 %supported, ptr %backend")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_source(ptr %source, ptr %target_name) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.lex_and_parse_source(ptr %source) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @types.source_diagnostic(ptr %source) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @lexer.lex(ptr %source) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @parser.parse_token_stream(ptr %tokens) { ret ptr null }")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%struct.compiler.CompileReport = type")
}

// requireSelfHostReportMarkers checks command and package report constructors.
func requireSelfHostReportMarkers(t *testing.T) {
	t.Helper()
	requireSelfHostCommandReportMarkers(t)
	requireSelfHostPackagePipelineMarkers(t)
	requireSelfHostPackageFallbackMarkers(t)
}

// requireSelfHostCommandReportMarkers checks report constructor symbols.
func requireSelfHostCommandReportMarkers(t *testing.T) {
	t.Helper()
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%struct.compiler.CommandReport = type")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.command_report(ptr %command, ptr %target_name, ptr %report)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"getelementptr inbounds %struct.compiler.CommandReport")
	requireFileContains(t, "target/kizu-selfhost.ll", "store ptr %command, ptr %command_slot")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.empty_package_report()")
	requireFileContains(t, "target/kizu-selfhost.ll", "store i64 0, ptr %m")
	requireFileContains(t, "target/kizu-selfhost.ll", "store ptr @.str.compiler.empty, ptr %d")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.package_failure(ptr %report, ptr %diagnostic)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"store ptr @.str.compiler.status.fail, ptr %s_out")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.finish_package_report(ptr %report, ptr %target_name)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.package_failure(ptr %report, ptr @.str.compiler.finish.missing_root)")
	requireFileContains(t, "target/kizu-selfhost.ll", "store i1 true, ptr %b_out")
	requireFileContains(t, "target/kizu-selfhost.ll", "define ptr @compiler.status_name(ptr %status)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.merge_status(ptr %left, ptr %right)")
	requireFileContains(t, "target/kizu-selfhost.ll", "icmp eq ptr %left, @.str.compiler.status.fail")
	requireFileContains(t, "target/kizu-selfhost.ll", "define ptr @compiler.check_source(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.compile_source(ptr %source, ptr @.str.compiler.target.llvm)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.build_source(ptr %source, ptr %target_name)")
}

// requireSelfHostPackagePipelineMarkers checks package pipeline symbols.
func requireSelfHostPackagePipelineMarkers(t *testing.T) {
	t.Helper()
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_package_module(ptr %module)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.compile_source(ptr %source, ptr @.str.compiler.compile.target.llvm)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define i64 @compiler.module_import_count(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.merge_package_report(ptr %current, ptr %next)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.merge_status(ptr %ls, ptr %rs)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define i1 @compiler.module_is_root(ptr %module)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call i1 @kizu_bytes_equal(ptr %path, ptr @.str.compiler.module.root)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.source_module(ptr %path, ptr %source, i1 %is_root, i1 %is_test)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"store ptr %source, ptr %source_out")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"%struct.compiler.ModuleSpec = type { ptr, ptr }")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.module_spec(ptr %path, ptr %file)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"store ptr %file, ptr %file_out")
	requireFileContains(t, "target/kizu-selfhost.ll", "declare i1 @kizu_file_exists(ptr)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.selfhost_manifest_file(ptr %root)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.selfhost_package_root(ptr %io)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call i1 @kizu_file_exists(ptr @.str.compiler.manifest.selfhost)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_package(ptr %modules, ptr %target_name)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_selfhost_package_check()")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_selfhost_package_from_files(ptr %io)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.finish_package_report(ptr %report, ptr %target_name)")
}

// requireSelfHostPackageFallbackMarkers checks eliminated package fallbacks.
func requireSelfHostPackageFallbackMarkers(t *testing.T) {
	t.Helper()
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.compile_package(ptr %modules, ptr %target_name) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.merge_package_report(ptr %current, ptr %next) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define i1 @compiler.module_is_root(ptr %module) { ret i1 false }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.source_module(ptr %path, ptr %source, "+
			"i1 %is_root, i1 %is_test) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.module_spec(ptr %path, ptr %file) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.selfhost_manifest_file(ptr %root) { ret ptr null }")
	requireFileNotContains(t, "target/kizu-selfhost.ll",
		"define ptr @compiler.selfhost_package_root(ptr %io) { ret ptr null }")
}

// requireSelfHostLexerMarkers checks remaining lexer and resolver body markers.
func requireSelfHostLexerMarkers(t *testing.T) {
	t.Helper()
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.ready() { ret i1 true }")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i64 @lexer.source_len(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll", "call i64 @kizu_bytes_len(ptr %source)")
	requireFileContains(t, "target/kizu-selfhost.ll", "@.str.backend.llvm_module_text")
	requireFileContains(t, "target/kizu-selfhost.ll", "define ptr @backend.llvm_module_text()")
	requireFileContains(t, "target/kizu-selfhost.ll", "@.str.selfhost_wasm_artifact")
	requireFileContains(t, "target/kizu-selfhost.ll", "define ptr @compiler.compile_self_check()")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"call ptr @compiler.compile_selfhost_package_check()")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.is_ascii_digit(i8 %byte)")
	requireFileContains(t, "target/kizu-selfhost.ll", "icmp uge i8 %byte, 48")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.is_ascii_space(i8 %byte)")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.is_ascii_alpha(i8 %byte)")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.is_ident_start(i8 %byte)")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @lexer.is_ident_continue(i8 %byte)")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define i1 @is_selfhost_package_target(ptr %path)")
	requireFileContains(t, "target/kizu-selfhost.ll", "call i1 @kizu_bytes_equal")
	requireFileContains(t, "target/kizu-selfhost.ll",
		"define i1 @diagnostics.is_error(ptr %diagnostic)")
	requireFileContains(t, "target/kizu-selfhost.ll", "define i1 @types.can_copy(ptr %info)")
	requireFileNotContains(t, "target/kizu-selfhost.ll", "define ptr @lexer.is_ascii_alpha")
}

// requireFileContains checks an artifact file contains a stable marker.
func requireFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("artifact missing %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("got artifact %s: %q", path, data)
	}
}

// requireFileNotContains checks an artifact file does not contain an unstable marker.
func requireFileNotContains(t *testing.T, path string, deny string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("artifact missing %s: %v", path, err)
	}
	if strings.Contains(string(data), deny) {
		t.Fatalf("got artifact %s containing denied marker %q", path, deny)
	}
}

// requireLLVMLowers checks a standalone-emitted LLVM artifact is toolchain-valid.
func requireLLVMLowers(t *testing.T, path string) {
	t.Helper()
	object := filepath.Join(t.TempDir(), "selfhost.o")
	cmd := exec.Command("llc", "-mtriple=aarch64-apple-darwin", "-filetype=obj", "-o", object, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("llc failed for %s: %v\n%s", path, err, out)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("llc did not write object %s: %v", object, err)
	}
}

// TestBuildTargetRejectsUnsupportedNative checks unsupported targets fail clearly.
func TestBuildTargetRejectsUnsupportedNative(t *testing.T) {
	cmd := exec.Command(
		"go", "run", ".", "build", "--target", "x86_64-linux-gnu", "../../examples/hello.kizu",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	if !strings.Contains(string(out), "invalid build target `x86_64-linux-gnu`") {
		t.Fatalf("got %q", out)
	}
}

// TestSelfHostWATCommandSmoke checks the Kizu-owned WAT backend command.
func TestSelfHostWATCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "selfhost-wat", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"(module",
		"(import \"wasi_snapshot_preview1\" \"fd_write\"",
		"(func $_start (export \"_start\")",
		"(call $main)",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildTargetWASISelfHostSwitch checks the opt-in Kizu-owned WAT switch.
func TestBuildTargetWASISelfHostSwitch(t *testing.T) {
	cmd := exec.Command(
		"go", "run", ".", "build", "--target", "wasm32-wasi", "../../examples/functions.kizu",
	)
	cmd.Env = append(os.Environ(), "KIZU_SELFHOST_WAT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{"(func $add)", "(func $main)", "(func $_start"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
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

// TestSelfHostCachePlanMatchesGoContract checks Kizu-owned cache planning facts.
func TestSelfHostCachePlanMatchesGoContract(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "selfhost-cache-plan", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	got := markedOutputLines(t, string(out),
		"cache contract snapshot",
		"cache contract snapshot end",
	)
	want := gobuildcache.ContractSnapshotLines()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfhost cache plan got %#v, want %#v", got, want)
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

type cliModuleGraphSnapshot struct {
	Status      string
	Message     string
	Root        string
	Modules     int
	ModulePaths []string
	Imports     []string
}

type cliDiagnosticSnapshot struct {
	File          string
	Message       string
	PrimaryStart  int
	PrimaryEnd    int
	PrimaryLine   int
	PrimaryColumn int
	RelatedStart  int
	RelatedEnd    int
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

// runSelfHostV2Function runs one module-first self-host compiler function.
func runSelfHostV2Function(t *testing.T, name string, sourcePath string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runSelfHostFunctionWithOutput(&out, name, sourcePath); err != nil {
		t.Fatalf("selfhost v2 function %s failed: %v\n%s", name, err, out.String())
	}
	return out.String()
}

// selfHostV2ConformanceSources returns unique corpus sources for v2 parity.
func selfHostV2ConformanceSources(t *testing.T) []string {
	t.Helper()
	paths := map[string]bool{}
	for _, manifest := range loadConformanceManifests(t) {
		for _, item := range manifest.Cases {
			paths[item.Path] = true
		}
	}
	for _, item := range loadModuleConformanceManifest(t).Cases {
		if item.RootSource != "" {
			paths[item.RootSource] = true
		}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, filepath.Join("..", "..", filepath.FromSlash(path)))
	}
	sortStrings(out)
	return out
}

// selfHostV2ParserParitySources returns the parser corpus currently ported 1:1.
func selfHostV2ParserParitySources() []string {
	return []string{
		"../../examples/functions.kizu",
		"../../examples/struct.kizu",
		"../../examples/enum.kizu",
		"../../examples/union.kizu",
		"../../examples/typed_error.kizu",
		"../../examples/contract_writer.kizu",
		"../../examples/if.kizu",
		"../../examples/while.kizu",
		"../../examples/for.kizu",
		"../../examples/variables.kizu",
		"../../examples/negative/missing_semicolon.kizu",
		"../../examples/negative/nullable_ptr_read.kizu",
		"../../examples/negative/arena_handle_outlive.kizu",
		"../../examples/negative/arena_unknown_handle.kizu",
		"../../examples/negative/channel_send_borrow.kizu",
		"../../examples/negative/field_borrow_owner_move.kizu",
		"../../examples/negative/if_expression_missing_else.kizu",
		"../../examples/negative/label_on_non_loop.kizu",
		"../../examples/negative/task_spawn_struct_pointer.kizu",
		"../../tests/conformance/modules/basic/src/main.kizu",
		"../../tests/conformance/modules/imported_types/src/main.kizu",
	}
}

// readTestSource reads a source path used by parity tests.
func readTestSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sortStrings keeps test corpus order deterministic without importing sort twice.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// parseSelfHostResolverGraphOutput parses the resolver graph snapshot section.
func parseSelfHostResolverGraphOutput(t *testing.T, output string) cliModuleGraphSnapshot {
	t.Helper()
	lines := markedOutputLines(t, output,
		"selfhost resolver module graph snapshot",
		"selfhost resolver module graph snapshot end",
	)
	return parseModuleGraphLines(t, lines)
}

// parseModuleGraphLines parses resolver graph rows.
func parseModuleGraphLines(t *testing.T, lines []string) cliModuleGraphSnapshot {
	t.Helper()
	snapshot := cliModuleGraphSnapshot{}
	for index := 0; index < len(lines); index++ {
		switch lines[index] {
		case "status":
			snapshot.Status = lines[index+1]
			index++
		case "message":
			snapshot.Message = lines[index+1]
			index++
		case "root":
			snapshot.Root = lines[index+1]
			index++
		case "modules":
			snapshot.Modules = parseSnapshotInt(t, lines[index+1])
			index++
		case "module":
			next, path := parsePathRecord(lines, index+1, "module end")
			snapshot.ModulePaths = append(snapshot.ModulePaths, path)
			index = next
		case "import":
			next, path := parsePathRecord(lines, index+1, "import end")
			snapshot.Imports = append(snapshot.Imports, path)
			index = next
		}
	}
	return snapshot
}

// parseSelfHostResolverDiagnosticsOutput parses resolver diagnostic records.
func parseSelfHostResolverDiagnosticsOutput(t *testing.T, output string) []cliDiagnosticSnapshot {
	t.Helper()
	lines := markedOutputLines(t, output,
		"selfhost resolver diagnostic snapshot",
		"selfhost resolver diagnostic snapshot end",
	)
	if len(lines)%8 != 0 {
		t.Fatalf("resolver diagnostic snapshot has incomplete records: %q", lines)
	}
	diagnostics := []cliDiagnosticSnapshot{}
	for index := 0; index < len(lines); index += 8 {
		diagnostics = append(diagnostics, cliDiagnosticSnapshot{
			File:          lines[index],
			Message:       lines[index+1],
			PrimaryStart:  parseSnapshotInt(t, lines[index+2]),
			PrimaryEnd:    parseSnapshotInt(t, lines[index+3]),
			PrimaryLine:   parseSnapshotInt(t, lines[index+4]),
			PrimaryColumn: parseSnapshotInt(t, lines[index+5]),
			RelatedStart:  parseSnapshotInt(t, lines[index+6]),
			RelatedEnd:    parseSnapshotInt(t, lines[index+7]),
		})
	}
	return diagnostics
}

// markedOutputLines returns output lines between two section markers.
func markedOutputLines(t *testing.T, output string, start string, end string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for index, line := range lines {
		if line == start {
			return collectMarkedLines(t, lines[index+1:], end)
		}
	}
	t.Fatalf("missing marker %q in %q", start, output)
	return nil
}

// collectMarkedLines collects lines until one end marker.
func collectMarkedLines(t *testing.T, lines []string, end string) []string {
	t.Helper()
	collected := []string{}
	for _, line := range lines {
		if line == end {
			return collected
		}
		collected = append(collected, line)
	}
	t.Fatalf("missing marker %q", end)
	return nil
}

// parsePathRecord returns a namespace path terminated by an end marker.
func parsePathRecord(lines []string, start int, end string) (int, string) {
	parts := []string{}
	index := start
	for ; index < len(lines); index++ {
		if lines[index] == end {
			return index, strings.Join(parts, "::")
		}
		parts = append(parts, lines[index])
	}
	return index, strings.Join(parts, "::")
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

// goResolverGraphSnapshot returns Go package graph facts in selfhost schema.
func goResolverGraphSnapshot(root string) cliModuleGraphSnapshot {
	pkg, err := goproject.LoadPackage(root)
	if err != nil {
		return cliModuleGraphSnapshot{Status: "fail", Message: normalizeModuleError(err.Error())}
	}
	snapshot := cliModuleGraphSnapshot{
		Status:  "pass",
		Root:    pkg.Graph.Root,
		Modules: len(pkg.Graph.Modules),
	}
	for _, module := range pkg.Graph.Modules {
		snapshot.ModulePaths = append(snapshot.ModulePaths, module.Path)
	}
	for _, module := range pkg.Modules {
		if module.Module.Path != pkg.Graph.Root {
			continue
		}
		for _, imported := range module.Imports {
			snapshot.Imports = append(snapshot.Imports, imported.Path)
		}
	}
	return snapshot
}

// normalizeModuleError maps resolver errors into the selected self-host subset.
func normalizeModuleError(message string) string {
	if strings.Contains(message, "missing module") {
		return "module error: missing module"
	}
	return "module error"
}

// goResolverDiagnosticSnapshots returns selected resolver diagnostic rows.
func goResolverDiagnosticSnapshots(t *testing.T, path string) []cliDiagnosticSnapshot {
	t.Helper()
	slashPath := filepath.ToSlash(path)
	if strings.Contains(slashPath, "missing_import") {
		return []cliDiagnosticSnapshot{
			goDiagnosticFromToken(
				path,
				"module error: missing module",
				tokenWithLiteral(t, path, "missing"),
			),
		}
	}
	if strings.Contains(slashPath, "private_module_access") {
		return []cliDiagnosticSnapshot{goDiagnosticFromToken(
			path,
			"module error",
			tokenWithLiteral(t, path, "hidden"),
		)}
	}
	if strings.Contains(slashPath, "private_type_leak") {
		return []cliDiagnosticSnapshot{goDiagnosticFromToken(
			path,
			"module error",
			tokenWithLiteral(t, path, "Secret"),
		)}
	}
	if strings.Contains(slashPath, "private_field_construction") {
		return []cliDiagnosticSnapshot{goDiagnosticFromToken(
			path,
			"module error",
			tokenWithLiteral(t, path, "secret"),
		)}
	}
	return []cliDiagnosticSnapshot{}
}

// goDiagnosticFromToken builds a diagnostic snapshot from one token.
func goDiagnosticFromToken(file string, message string, token gotoken.Token) cliDiagnosticSnapshot {
	return cliDiagnosticSnapshot{
		File:          file,
		Message:       message,
		PrimaryStart:  token.Start,
		PrimaryEnd:    token.End,
		PrimaryLine:   token.Line,
		PrimaryColumn: token.Column,
		RelatedStart:  token.Start,
		RelatedEnd:    token.End,
	}
}

// tokenWithLiteral returns the first Go token with the requested literal.
func tokenWithLiteral(t *testing.T, path string, literal string) gotoken.Token {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lexer := golexer.New(string(source))
	for {
		current := lexer.NextToken()
		if current.Literal == literal {
			return current
		}
		if current.Type == gotoken.EOF {
			break
		}
	}
	t.Fatalf("no token literal %q in %s", literal, path)
	return gotoken.Token{}
}

// resolverRootSource returns the configured root source for module fixtures.
func resolverRootSource(root string) string {
	return filepath.Join(root, "src", "main.kizu")
}

// goTypeSnapshot returns function return type facts from the Go AST.
func goTypeSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseGoProgram(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*goast.FunctionDecl)
		if !ok {
			continue
		}
		lines = append(lines, "fn", fn.Name, "return", normalizeGoReturnType(fn.ReturnType))
	}
	return lines
}

// goTypeEnvSnapshot returns selected local binding type facts from the Go AST.
func goTypeEnvSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseGoProgram(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*goast.FunctionDecl)
		if ok && fn.Body != nil {
			lines = appendGoLocalTypeEnv(lines, fn.Body)
		}
	}
	return lines
}

// appendGoLocalTypeEnv appends direct block local type facts.
func appendGoLocalTypeEnv(lines []string, block *goast.BlockStmt) []string {
	for _, stmt := range block.Statements {
		local, ok := stmt.(*goast.LetStmt)
		if !ok {
			continue
		}
		mutability := "let"
		if local.Mutable {
			mutability = "var"
		}
		lines = append(lines, mutability, local.Name, goLocalValueType(local.Value))
	}
	return lines
}

// goLocalValueType returns the selected local initializer type.
func goLocalValueType(expr goast.Expression) string {
	switch current := expr.(type) {
	case *goast.StringExpr:
		return "[]const u8"
	case *goast.IntExpr:
		return "i64"
	case *goast.BoolExpr:
		return "bool"
	case *goast.StructLiteralExpr:
		return current.TypeName
	default:
		return "unknown"
	}
}

// goTypeCheckSnapshot returns selected Go type checker pass/fail facts.
func goTypeCheckSnapshot(t *testing.T, path string) []string {
	t.Helper()
	err := gotypes.New().Check(parseGoProgram(t, path))
	if err == nil {
		return []string{"status", "pass"}
	}
	return []string{"status", "fail", "message", normalizeGoTypeError(err.Error())}
}

// normalizeGoTypeError maps Go type diagnostics into the selected subset.
func normalizeGoTypeError(message string) string {
	if strings.Contains(message, "equal_bytes") {
		return "type error: `std::mem::equal_bytes` arg 2 expects []const u8"
	}
	if strings.Contains(message, "unknown field") {
		return "type error: unknown field"
	}
	return "type error"
}

// parseGoProgram parses a source file with the Go-owned parser.
func parseGoProgram(t *testing.T, path string) *goast.Program {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parser := goparser.New(golexer.New(string(source)))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		t.Fatalf("go parser errors: %v", parser.Errors())
	}
	return program
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
