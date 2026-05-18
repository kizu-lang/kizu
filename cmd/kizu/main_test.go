package main

import (
	"encoding/json"
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
    try std::testing::expect_equal_i64(1, 1);
    return void;
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mem", "array", "string", "fmt", "testing"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
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

// TestBuildTargetNativeProcessArgCount checks hosted native process argv plumbing.
func TestBuildTargetNativeProcessArgCount(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "argc.kizu")
	code := []byte(`fn main() {
    print(std::process::arg_count());
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "argc")
	build := exec.Command(
		"go", "run", ".", "build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output, "a", "b")
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, out)
	}
	if string(out) != "3\n" {
		t.Fatalf("got %q", out)
	}
}

// TestBuildTargetNativeFSReadWrite checks hosted native explicit-Io file primitives.
func TestBuildTargetNativeFSReadWrite(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	dir := t.TempDir()
	written := filepath.Join(dir, "written.txt")
	source := filepath.Join(dir, "fs.kizu")
	code := `fn main() -> !void {
    let io = std::io::blocking();
    try std::fs::write_file(io, "` + written + `", "stage2 llvm");
    let text = try std::fs::read_file(io, "` + written + `");
    print(text);
    return;
}`
	if err := os.WriteFile(source, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "fs")
	build := exec.Command(
		"go", "run", ".", "build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, out)
	}
	if string(out) != "stage2 llvm\n" {
		t.Fatalf("got %q", out)
	}
}

// TestBuildTargetNativeStringBuilder checks hosted native String construction.
func TestBuildTargetNativeStringBuilder(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	dir := t.TempDir()
	written := filepath.Join(dir, "string.txt")
	source := filepath.Join(dir, "string.kizu")
	code := `fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let io = std::io::blocking();
    var text = std::string::String(allocator);
    try text.append_bytes("define ");
    try text.append_byte(cast<u8>(88));
    let bytes = text.as_bytes();
    try std::fs::write_file(io, "` + written + `", bytes);
    return;
}`
	if err := os.WriteFile(source, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "string")
	build := exec.Command("go", "run", ".", "build", "--target", "native", "-o", output, source)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native string build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native string executable failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "define X" {
		t.Fatalf("got %q", data)
	}
}

// TestSelfhostStage1ReadsSourceTree checks the generated native seed reads selfhost sources.
func TestSelfhostStage1ReadsSourceTree(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	dir := t.TempDir()
	stage1 := filepath.Join(dir, "kizu-stage1")
	stage2 := filepath.Join(dir, "stage2.ll")
	stage2Bin := filepath.Join(dir, "kizu-stage2")
	stage3 := filepath.Join(dir, "stage3.ll")

	build := exec.Command(
		"go", "run", "./cmd/kizu", "build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", stage1, "selfhost",
	)
	build.Dir = repoRoot
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("stage1 build failed: %v\n%s", err, out)
	}

	run := exec.Command(stage1, stage2)
	run.Dir = repoRoot
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("stage1 executable failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(stage2)
	if err != nil {
		t.Fatal(err)
	}
	assertStage2LLVMReadsSources(t, data)

	link := exec.Command("clang", stage2, "-o", stage2Bin)
	out, err = link.CombinedOutput()
	if err != nil {
		t.Fatalf("stage2 link failed: %v\n%s", err, out)
	}
	run = exec.Command(stage2Bin, stage2, stage3)
	run.Dir = repoRoot
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("stage2 executable failed: %v\n%s", err, out)
	}
	data, err = os.ReadFile(stage3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "define i32 @main") {
		t.Fatalf("stage3 artifact does not look like LLVM IR:\n%s", data)
	}
	stage2Data, err := os.ReadFile(stage2)
	if err != nil {
		t.Fatal(err)
	}
	assertStageArtifactsEqual(t, stage2Data, data)
	assertStage2CanWriteWithoutInputCopy(t, repoRoot, stage2Bin, dir, stage2Data)
}

// assertStage2LLVMReadsSources checks that the stage2 IR depends on compiler source inputs.
func assertStage2LLVMReadsSources(t *testing.T, data []byte) {
	t.Helper()
	text := string(data)
	if !strings.Contains(text, "define i32 @main") {
		t.Fatalf("stage2 artifact does not look like LLVM IR:\n%s", data)
	}
	if !strings.Contains(text, "fopen(ptr %source0, ptr %readmode)") ||
		!strings.Contains(text, "fopen(ptr %source8, ptr %readmode)") {
		t.Fatalf("stage2 artifact does not read the selfhost source tree:\n%s", data)
	}
	if !strings.Contains(text, "fgetc(ptr %srcfile0)") ||
		!strings.Contains(text, "fgetc(ptr %srcfile8)") {
		t.Fatalf("stage2 artifact does not scan source contents:\n%s", data)
	}
	if !strings.Contains(text, "read8.loop") ||
		!strings.Contains(text, "%ok8 = icmp sgt i32 %count8, 0") {
		t.Fatalf("stage2 artifact does not drain source files:\n%s", data)
	}
	if !strings.Contains(text, "%scanned = and i1 %all7, %large") ||
		!strings.Contains(text, "%ready = and i1 %scanned, %copy") {
		t.Fatalf("stage2 artifact does not gate copy on source scan:\n%s", data)
	}
	if !strings.Contains(text, "; kizu selfhost source metric ") {
		t.Fatalf("stage2 artifact does not include source-derived metric:\n%s", data)
	}
	if !strings.Contains(text, "; kizu selfhost source bytes ") {
		t.Fatalf("stage2 artifact does not include source byte total:\n%s", data)
	}
}

// assertStageArtifactsEqual checks stage output stability for the bootstrap smoke.
func assertStageArtifactsEqual(t *testing.T, stage2 []byte, stage3 []byte) {
	t.Helper()
	if string(stage2) != string(stage3) {
		t.Fatalf("stage2 and stage3 artifacts differ\nstage2:\n%s\nstage3:\n%s", stage2, stage3)
	}
}

// assertStage2CanWriteWithoutInputCopy checks stage2 can emit after source scan alone.
func assertStage2CanWriteWithoutInputCopy(
	t *testing.T,
	repoRoot string,
	stage2Bin string,
	dir string,
	stage2Data []byte,
) {
	t.Helper()
	sourceOut := filepath.Join(dir, "stage3-source.ll")
	sourceBin := filepath.Join(dir, "kizu-stage3-source")
	run := exec.Command(stage2Bin, sourceOut)
	run.Dir = repoRoot
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("stage2 source-only executable failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(sourceOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "; kizu selfhost source metric ") {
		t.Fatalf("source-only stage2 artifact unexpectedly copied stage2 input:\n%s", data)
	}
	if !strings.Contains(string(data), "; kizu stage2 source bytes ") {
		t.Fatalf("source-only stage2 artifact does not include source byte total:\n%s", data)
	}
	if !strings.Contains(string(data), "; kizu stage2 source fn count ") {
		t.Fatalf("source-only stage2 artifact does not include source fn count:\n%s", data)
	}
	stage2BytesLine := firstLineContaining(string(stage2Data), "; kizu selfhost source bytes ")
	sourceBytesLine := strings.Replace(
		firstLineContaining(string(data), "; kizu stage2 source bytes "),
		"; kizu stage2 source bytes ",
		"; kizu selfhost source bytes ",
		1,
	)
	if stage2BytesLine != sourceBytesLine {
		t.Fatalf("source byte totals differ: stage2 %q source-only %q",
			stage2BytesLine, sourceBytesLine)
	}
	stage2FnsLine := firstLineContaining(string(stage2Data), "; kizu selfhost source fn count ")
	sourceFnsLine := strings.Replace(
		firstLineContaining(string(data), "; kizu stage2 source fn count "),
		"; kizu stage2 source fn count ",
		"; kizu selfhost source fn count ",
		1,
	)
	if stage2FnsLine != sourceFnsLine {
		t.Fatalf("source fn counts differ: stage2 %q source-only %q",
			stage2FnsLine, sourceFnsLine)
	}
	link := exec.Command("clang", sourceOut, "-o", sourceBin)
	out, err = link.CombinedOutput()
	if err != nil {
		t.Fatalf("source-only stage2 link failed: %v\n%s", err, out)
	}
	run = exec.Command(sourceBin)
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("source-only stage2 output failed: %v\n%s", err, out)
	}
}

// firstLineContaining returns the first line with marker.
func firstLineContaining(text string, marker string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
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
	source := filepath.Join(t.TempDir(), "arena.kizu")
	code := []byte(`struct User { age: i64; }
fn main() {
    let users = arena<User>();
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "build", "--target", "native", source)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected native build to fail\n%s", out)
	}
	want := "llvm error: `arena.new` is not supported by the LLVM backend yet"
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
