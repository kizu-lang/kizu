package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// TestParseCommandUsesSelfhostFrontend keeps parse routed through Kizu frontend code.
func TestParseCommandUsesSelfhostFrontend(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "parse", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() {\n    print(\"hello, kizu\");\n}\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestParseCommandPropagatesSelfhostExitCode keeps Go from adding fallback diagnostics.
func TestParseCommandPropagatesSelfhostExitCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.kizu")
	if err := os.WriteFile(path, []byte("fn main() { let x = ; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, runErr := runDispatchCaptureStderr(t, "parse", []string{path})
	var status exitStatus
	if !errors.As(runErr, &status) || status.code != 1 {
		t.Fatalf("got error %v, want exit status 1", runErr)
	}
	want := "error: expected expression, got ; at 1:21\nerror: parse failed\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestCheckPackageCommandReportsSelfhostDiagnostic keeps package diagnostics detailed.
func TestCheckPackageCommandReportsSelfhostDiagnostic(t *testing.T) {
	root := t.TempDir()
	manifest := []byte("[package]\nname = \"app\"\n")
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := "fn main() {\n    missing();\n}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "main.kizu"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	out, runErr := runDispatchCaptureStderr(t, "check", []string{root})
	var status exitStatus
	if !errors.As(runErr, &status) || status.code != 1 {
		t.Fatalf("got error %v, want exit status 1", runErr)
	}
	want := "error: unknown function `missing`\nerror: check failed\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// runDispatchCaptureStderr runs dispatch with process-global stderr captured.
func runDispatchCaptureStderr(t *testing.T, command string, args []string) (string, error) {
	t.Helper()
	oldStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = reader.Close()
	}()
	runErr := func() error {
		os.Stderr = writer
		defer func() {
			os.Stderr = oldStderr
		}()
		return dispatch(command, args)
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

// TestFmtCommandSmoke checks the CLI can print stable formatted Kizu source.
func TestFmtCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "fmt", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() {\n    print(\"hello, kizu\");\n}\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestFmtCommandRejectsInvalidSyntax checks fmt does not rewrite parser failures.
func TestFmtCommandRejectsInvalidSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.kizu")
	if err := os.WriteFile(path, []byte("fn main( { return; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := fmtCommand([]string{path})
	if err == nil || err.Error() != "format failed" {
		t.Fatalf("got error %v, want format failed", err)
	}
}

// TestFmtWriteRejectsLineComments checks --write does not drop comment trivia.
func TestFmtWriteRejectsLineComments(t *testing.T) {
	src := "// keep this comment\nfn main() {}\n"
	path := filepath.Join(t.TempDir(), "commented.kizu")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	err := fmtCommand([]string{"--write", path})
	if err == nil || !strings.Contains(err.Error(), "line comments") {
		t.Fatalf("got error %v, want line comments rejection", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != src {
		t.Fatalf("file changed:\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestCheckPackageCommandSmoke checks package roots can be statically checked.
func TestCheckPackageCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "check", "../../examples/modules/same_module_helper_lookup")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "check: ok\n" {
		t.Fatalf("got %q, want check ok", out)
	}
}

// TestRunPackageCommandSmoke checks package roots can execute root module main.
func TestRunPackageCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/modules/cross_module_types")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "0\n2\nfn\nmain\ntoken\n3\n8\n3\n3\n" {
		t.Fatalf("got %q, want package run output", out)
	}
}

// TestTestPackageCommandSmoke checks package roots can run assertion tests.
func TestTestPackageCommandSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "test", "../../examples/modules/cross_module_types")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "0\n2\nfn\nmain\ntoken\n3\n8\n3\n3\ntest: ok\n" {
		t.Fatalf("got %q, want package test output", out)
	}
}

// TestRunCompilerPhasesPackageSmoke checks self-host phase-shaped APIs.
func TestRunCompilerPhasesPackageSmoke(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/modules/compiler_phases")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "7\n" {
		t.Fatalf("got %q, want compiler phase output", out)
	}
}

// TestRunCompilerPhasesStopsAfterParseError checks try prevents later phases.
func TestRunCompilerPhasesStopsAfterParseError(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "run", "../../examples/modules/compiler_phases_fail")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "app::parser::CompileError::Diagnostic") {
		t.Fatalf("got %q, want typed parser error", out)
	}
	if strings.Contains(text, "lowered") {
		t.Fatalf("got %q, want lowering output to be skipped", out)
	}
}

// TestResolveStdModulesIncludesTransitiveStdSourceDeps checks std source dependencies.
func TestResolveStdModulesIncludesTransitiveStdSourceDeps(t *testing.T) {
	got, err := resolveStdModules(`fn main() {
    let allocator = std::mem::page_allocator();
    var text = std::string::String(allocator);
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mem", "array", "string"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestResolveStdModulesOrdersTestingDeps checks dependency-before-dependent order.
func TestResolveStdModulesOrdersTestingDeps(t *testing.T) {
	got, err := resolveStdModules(`fn main() -> !void {
    std::testing::expect(1 == 1);
    return void;
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"testing"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestResolveStdModulesOrdersNestedKizuDeps checks nested std modules and deps.
func TestResolveStdModulesOrdersNestedKizuDeps(t *testing.T) {
	got, err := resolveStdModules(`fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let source = std::kizu::ast::source_file("main.kizu", "fn main");
    let node = try std::kizu::parser::parse_first_node(allocator, source);
    return;
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mem", "array", "kizu::ast", "kizu::lexer", "kizu::parser"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestResolveStdModulesLoadsPrivateStdDeps keeps internal std modules usable by std.
func TestResolveStdModulesLoadsPrivateStdDeps(t *testing.T) {
	got, err := resolveStdModules(`fn main() -> void {
    print(std::path::basename("src/main.kizu"));
    return;
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mem", "array", "string", "path_bits", "path"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestResolveStdModulesRejectsPrivateStdModule blocks user access to std internals.
func TestResolveStdModulesRejectsPrivateStdModule(t *testing.T) {
	_, err := resolveStdModules(`fn main() -> void {
    print(std::path_bits::basename("src/main.kizu"));
    return;
}`)
	if err == nil || !strings.Contains(err.Error(), "std module `std::path_bits` is not exported") {
		t.Fatalf("got error %v, want private std module rejection", err)
	}
}

// TestResolveStdModulesIgnoresStringLiterals avoids false std dependency edges.
func TestResolveStdModulesIgnoresStringLiterals(t *testing.T) {
	got, err := resolveStdModules(`fn main() -> void {
    print("std::path_bits::basename");
    return;
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got modules %v, want none", got)
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
	want := "define i32 @main()"
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

// TestBuildTargetNativeCommandSmoke checks native build produces an executable.
func TestBuildTargetNativeCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	output := filepath.Join(t.TempDir(), "hello")
	build := exec.Command(
		"go", "run", ".", "build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, "../../examples/hello.kizu",
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != output {
		t.Fatalf("got %q, want output path %q", out, output)
	}
	assertNativeMetadata(t, output+".kizu-build.json", output)
	run := exec.Command(output)
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, out)
	}
	if string(out) != "hello, kizu\n" {
		t.Fatalf("got %q", out)
	}
}

// TestBuildTargetNativeRejectsUnsupportedModes checks planned Zig-style modes are explicit.
func TestBuildTargetNativeRejectsUnsupportedModes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no libc",
			args: []string{"build", "--target", "native", "--libc", "off", "../../examples/hello.kizu"},
			want: "native error: --libc off is not implemented yet",
		},
		{
			name: "freestanding",
			args: []string{"build", "--target", "native", "--runtime", "freestanding",
				"../../examples/hello.kizu"},
			want: "native error: --runtime freestanding is not implemented yet",
		},
		{
			name: "object",
			args: []string{"build", "--target", "native", "--emit", "obj", "../../examples/hello.kizu"},
			want: "native error: --emit obj is not implemented yet",
		},
		{
			name: "cpu",
			args: []string{"build", "--target", "native", "--cpu", "baseline", "../../examples/hello.kizu"},
			want: "native error: --cpu is not implemented yet",
		},
		{
			name: "abi",
			args: []string{"build", "--target", "native", "--abi", "gnu", "../../examples/hello.kizu"},
			want: "native error: --abi is not implemented yet",
		},
		{
			name: "linker",
			args: []string{"build", "--target", "native", "--linker", "lld", "../../examples/hello.kizu"},
			want: "native error: --linker lld is not implemented yet",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", append([]string{"run", "."}, tt.args...)...)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected command to fail\n%s", out)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Fatalf("got %q, want substring %q", out, tt.want)
			}
		})
	}
}

// assertNativeMetadata checks native artifact metadata records explicit build inputs.
func assertNativeMetadata(t *testing.T, path string, output string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Target  string   `json:"target"`
		LibC    string   `json:"libc"`
		Runtime string   `json:"runtime"`
		Emit    string   `json:"emit"`
		Linker  string   `json:"linker"`
		Output  string   `json:"output"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Target != "native" || got.LibC != "on" || got.Runtime != "hosted" {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Emit != "exe" || got.Linker != "clang" || got.Output != output {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if len(got.Command) == 0 || got.Command[0] != "clang" {
		t.Fatalf("unexpected command metadata: %+v", got.Command)
	}
}

// TestBuildTargetNativeRejectsUnsupportedFeature checks native build fails before clang.
func TestBuildTargetNativeRejectsUnsupportedFeature(t *testing.T) {
	source := filepath.Join(t.TempDir(), "struct.kizu")
	code := []byte(`struct User { age: i64, }
fn main() {
    let user = User { age: 30 };
    print(user.age);
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "build", "--target", "native", source)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected native build to fail\n%s", out)
	}
	want := "llvm error: `struct.new` is not supported by the LLVM backend yet"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
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
