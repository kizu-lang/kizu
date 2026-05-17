package main

import (
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
	for _, want := range []string{"define ptr @main()", "define ptr @compiler.compile_source"} {
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
