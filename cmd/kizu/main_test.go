package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRunCommandSmoke checks the CLI can execute the hello example.
func TestRunCommandSmoke(t *testing.T) {
	cmd := kizuCommand("run", "../../examples/hello.kizu")
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
	cmd := kizuCommand("run", "../../examples/borrow.kizu")
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
	cmd := kizuCommand("run", "../../examples/arena.kizu")
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
	cmd := kizuCommand("ir", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() -> void"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestFmtWriteUpdatesFile checks --write rewrites through the fmt CLI path.
func TestFmtWriteUpdatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unformatted.kizu")
	if err := os.WriteFile(path, []byte("fn main(){print(\"hello, kizu\");}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, runErr := runDispatchCaptureStderr(t, "fmt", []string{"--write", path})
	if runErr != nil {
		t.Fatalf("command failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "fn main() {\n    print(\"hello, kizu\");\n}\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFmtWritesMoveMarkers checks fmt brings a file that predates the marker up
// to date, at every hand-off and only there, and leaves an already marked file
// alone.
func TestFmtWritesMoveMarkers(t *testing.T) {
	source := `import std::mem;
import std::string;

fn build(allocator: Allocator) -> !string::String {
    var name = string::new(allocator);
    errdefer name.deinit();
    try name.append_byte(cast<u8>(97));
    return name;
}

fn keep(allocator: Allocator) -> !void {
    var held = try build(allocator);
    defer held.deinit();
    print(held.len());
    return;
}

fn main() -> !void {
    try keep(mem::page_allocator());
    return;
}
`
	path := filepath.Join(t.TempDir(), "unmarked.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		out, runErr := runDispatchCaptureStderr(t, "fmt", []string{"--write", path})
		if runErr != nil {
			t.Fatalf("round %d failed: %v\n%s", round, runErr, out)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "return move name;") {
			t.Fatalf("round %d: got %q, want the hand-off marked", round, got)
		}
		// `held` is consumed by its own deinit, not handed off, so the second
		// round proves the pass is idempotent rather than additive.
		if n := strings.Count(string(got), "move "); n != 1 {
			t.Fatalf("round %d: got %d markers, want 1:\n%s", round, n, got)
		}
	}
}

// TestInitCommandCreatesRunnablePackage checks init scaffolds a minimal package.
func TestInitCommandCreatesRunnablePackage(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "my-app")

	stdout, stderr, runErr := runDispatchCaptureOutput(t, "init", []string{target})
	if runErr != nil {
		t.Fatalf("init failed: %v\nstderr:%s", runErr, stderr)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "initialized Kizu package `my_app`") {
		t.Fatalf("got stdout %q, want package name", stdout)
	}
	assertFileContent(t, filepath.Join(target, "kizu.toml"), `[package]
name = "my_app"
version = "0.1.0"

[modules]
paths = ["src"]
`)
	assertFileContent(t, filepath.Join(target, "src", "main.kizu"), initMainSource)

	runStdout, runStderr, runErr := runDispatchCaptureOutput(t, "run", []string{target})
	if runErr != nil {
		t.Fatalf("run generated package: %v\nstderr:%s", runErr, runStderr)
	}
	if runStderr != "" {
		t.Fatalf("got run stderr %q, want empty", runStderr)
	}
	if runStdout != "hello, kizu\n" {
		t.Fatalf("got run stdout %q, want hello", runStdout)
	}
}

// TestInitCommandUsesCurrentDirectory checks `kizu init` with no path.
func TestInitCommandUsesCurrentDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "current-project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := chdirForTest(t, root)
	defer restore()

	stdout, stderr, runErr := runDispatchCaptureOutput(t, "init", nil)
	if runErr != nil {
		t.Fatalf("init failed: %v\nstderr:%s", runErr, stderr)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "initialized Kizu package `current_project`") {
		t.Fatalf("got stdout %q, want normalized package name", stdout)
	}
	assertFileContent(t, filepath.Join(root, "kizu.toml"), `[package]
name = "current_project"
version = "0.1.0"

[modules]
paths = ["src"]
`)
}

// TestInitCommandRefusesOverwrite checks init does not replace user files.
func TestInitCommandRefusesOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "existing-project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, runErr := runDispatchCaptureOutput(t, "init", []string{root})
	if runErr == nil || !strings.Contains(runErr.Error(), "already exists") {
		t.Fatalf("got error %v, want already exists", runErr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("got stdout %q stderr %q, want empty", stdout, stderr)
	}
	assertFileContent(t, filepath.Join(root, "kizu.toml"), "keep me\n")
	if _, err := os.Stat(filepath.Join(root, "src", "main.kizu")); !os.IsNotExist(err) {
		t.Fatalf("src/main.kizu stat error = %v, want not exist", err)
	}
}

// TestInitPackageNameNormalization checks path basenames become valid namespaces.
func TestInitPackageNameNormalization(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "my-app", want: "my_app"},
		{name: "My--App", want: "my_app"},
		{name: "my_app", want: "my_app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeInitPackageName(tt.name)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInitPackageNameRejectsInvalidNames keeps manifest namespaces parseable.
func TestInitPackageNameRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"123-app", "std", "bad name"} {
		t.Run(name, func(t *testing.T) {
			_, err := initPackageName(filepath.Join(t.TempDir(), name))
			if err == nil {
				t.Fatalf("got nil error for %q", name)
			}
		})
	}
}

// TestInitCommandCanRunWithoutTarget checks main accepts the init command shape.
func TestInitCommandCanRunWithoutTarget(t *testing.T) {
	if !commandAllowsNoTarget("init") {
		t.Fatal("init should be accepted without a path argument")
	}
	if commandAllowsNoTarget("run") {
		t.Fatal("run should still require a path argument")
	}
}

// TestCheckPackageCommandWithoutModuleRoot keeps a module of the package's own
// name optional. A library package has no reason to hold one, and std holds
// none: everything it has is `std::mem` or below.
func TestCheckPackageCommandWithoutModuleRoot(t *testing.T) {
	root := t.TempDir()
	manifest := []byte("[package]\nname = \"app\"\n")
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(srcDir, "lexer.kizu"),
		[]byte("pub fn token() -> i64 {\n    return 1;\n}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, runErr := runDispatchCaptureStderr(t, "check", []string{root}); runErr != nil {
		t.Fatalf("check failed: %v", runErr)
	}
}

// TestCheckPackageCommandUsesManifestPaths keeps package loading manifest-driven.
func TestCheckPackageCommandUsesManifestPaths(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(
		"[package]\nname = \"frontend\"\n\n[modules]\npaths = [\"lib\"]\n",
	)
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(root, "lib")
	if err := os.Mkdir(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checksDir := filepath.Join(libDir, "checks")
	if err := os.Mkdir(checksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checks := "pub fn touch() -> void {\n    return;\n}\n"
	checksPath := filepath.Join(checksDir, "checks.kizu")
	if err := os.WriteFile(checksPath, []byte(checks), 0o644); err != nil {
		t.Fatal(err)
	}
	parserDir := filepath.Join(libDir, "parser")
	if err := os.Mkdir(parserDir, 0o755); err != nil {
		t.Fatal(err)
	}
	astDir := filepath.Join(parserDir, "ast")
	if err := os.Mkdir(astDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := "pub fn touch() -> void {\n    return;\n}\n"
	if err := os.WriteFile(filepath.Join(astDir, "ast.kizu"), []byte(nested), 0o644); err != nil {
		t.Fatal(err)
	}
	source := "import frontend::checks;\n" +
		"import frontend::parser::ast;\n\n" +
		"pub fn root_touch() -> void {\n" +
		"    return;\n" +
		"}\n\n" +
		"fn main() {\n" +
		"    checks::touch();\n" +
		"    ast::touch();\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(libDir, "main.kizu"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	nested = "pub fn touch() -> void {\n    frontend::root_touch();\n}\n"
	if err := os.WriteFile(filepath.Join(astDir, "ast.kizu"), []byte(nested), 0o644); err != nil {
		t.Fatal(err)
	}

	out, runErr := runDispatchCaptureStderr(t, "check", []string{root})
	if runErr != nil {
		t.Fatalf("check failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
}

// TestCheckPackageCommandCombinesDirectoryFiles checks one directory is one module.
func TestCheckPackageCommandCombinesDirectoryFiles(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(
		"[package]\nname = \"frontend\"\n\n[modules]\npaths = [\"lib\"]\n",
	)
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(root, "lib")
	if err := os.Mkdir(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parserDir := filepath.Join(libDir, "parser")
	if err := os.Mkdir(parserDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mainSource := `import frontend::parser;

fn main() {
    print(parser::answer());
}
`
	if err := os.WriteFile(filepath.Join(libDir, "main.kizu"), []byte(mainSource), 0o644); err != nil {
		t.Fatal(err)
	}
	privateHelper := "fn value() -> i64 {\n    return 42;\n}\n"
	valuePath := filepath.Join(parserDir, "value.kizu")
	if err := os.WriteFile(valuePath, []byte(privateHelper), 0o644); err != nil {
		t.Fatal(err)
	}
	publicAPI := "pub fn answer() -> i64 {\n    return value();\n}\n"
	parserPath := filepath.Join(parserDir, "parser.kizu")
	if err := os.WriteFile(parserPath, []byte(publicAPI), 0o644); err != nil {
		t.Fatal(err)
	}

	out, runErr := runDispatchCaptureStderr(t, "check", []string{root})
	if runErr != nil {
		t.Fatalf("check failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
}

// TestCheckCommandAcceptsMatchArmBinding keeps payload bindings visible in arm bodies.
func TestCheckCommandAcceptsMatchArmBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "match_binding.kizu")
	source := `pub union Expr {
    Ident(i64),
    Other(i64),
}

fn main() {
    let expr = Expr::Ident(1);
    match expr {
        Ident(value) => print(value),
        Other(other) => print(other),
    }
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr != nil {
		t.Fatalf("check failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
}

// TestCheckBranchMoveAllowsSiblingUse keeps branch state isolated while checking arms.
func TestCheckBranchMoveAllowsSiblingUse(t *testing.T) {
	path := writeTempKizuSource(t, "branch_sibling_use.kizu", `struct Name {
    value: []u8,
}

fn take(name: Name) {
    print(name.value);
}

fn main() {
    let name = Name { value: "alice" };
    if true {
        take(move name);
    } else {
        print(name.value);
    }
}
`)
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr != nil {
		t.Fatalf("check failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
}

// TestCheckBranchMoveRejectsPostIfUse checks moves from one arm escape the branch.
func TestCheckBranchMoveRejectsPostIfUse(t *testing.T) {
	path := writeTempKizuSource(t, "branch_one_arm_move.kizu", `struct Name {
    value: []u8,
}

fn take(name: Name) {
    print(name.value);
}

fn main() {
    let name = Name { value: "alice" };
    if true {
        take(move name);
    } else {
        print("kept");
    }
    print(name.value);
}
`)
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr == nil {
		t.Fatal("check unexpectedly succeeded")
	}
	// The diagnostic names the file it points into, and that path is a temp dir
	// here, so match the parts the test is actually about.
	want := "error: move error: moved value `name` was used at "
	if !strings.HasPrefix(out, want) || !strings.HasSuffix(out, ":16:11\n") {
		t.Fatalf("got %q, want %q ... :16:11", out, want)
	}
}

// TestCheckBranchMoveRejectsPostIfUseAfterBothArms checks both-arm moves remain moved.
func TestCheckBranchMoveRejectsPostIfUseAfterBothArms(t *testing.T) {
	path := writeTempKizuSource(t, "branch_both_arm_move.kizu", `struct Name {
    value: []u8,
}

fn take(name: Name) {
    print(name.value);
}

fn main() {
    let name = Name { value: "alice" };
    if true {
        take(move name);
    } else {
        take(move name);
    }
    print(name.value);
}
`)
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr == nil {
		t.Fatal("check unexpectedly succeeded")
	}
	// The diagnostic names the file it points into, and that path is a temp dir
	// here, so match the parts the test is actually about.
	want := "error: move error: moved value `name` was used at "
	if !strings.HasPrefix(out, want) || !strings.HasSuffix(out, ":16:11\n") {
		t.Fatalf("got %q, want %q ... :16:11", out, want)
	}
}

// TestCheckBranchMoveKeepsUnmovedValueUsable checks unaffected values survive branches.
func TestCheckBranchMoveKeepsUnmovedValueUsable(t *testing.T) {
	path := writeTempKizuSource(t, "branch_unmoved_use.kizu", `struct Name {
    value: []u8,
}

fn main() {
    let name = Name { value: "alice" };
    if true {
        print("then");
    } else {
        print("else");
    }
    print(name.value);
}
`)
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr != nil {
		t.Fatalf("check failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
}

// TestCheckWhileMoveRejectsPostLoopUse keeps loop body moves conservative.
func TestCheckWhileMoveRejectsPostLoopUse(t *testing.T) {
	path := writeTempKizuSource(t, "while_move.kizu", `struct Name {
    value: []u8,
}

fn take(name: Name) {
    print(name.value);
}

fn main() {
    let name = Name { value: "alice" };
    while false {
        take(move name);
    }
    print(name.value);
}
`)
	out, runErr := runDispatchCaptureStderr(t, "check", []string{path})
	if runErr == nil {
		t.Fatal("check unexpectedly succeeded")
	}
	// The diagnostic names the file it points into, and that path is a temp dir
	// here, so match the parts the test is actually about.
	want := "error: move error: moved value `name` was used at "
	if !strings.HasPrefix(out, want) || !strings.HasSuffix(out, ":14:11\n") {
		t.Fatalf("got %q, want %q ... :14:11", out, want)
	}
}

// TestCheckPackageCommandRejectsInvalidManifestPaths avoids silent path fallback.
func TestCheckPackageCommandRejectsInvalidManifestPaths(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(
		"[package]\nname = \"frontend\"\n\n[modules]\npaths = [lib]\n",
	)
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(root, "lib")
	if err := os.Mkdir(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(libDir, "main.kizu"),
		[]byte("fn main() {}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, runErr := runDispatchCaptureStderr(t, "check", []string{root})
	if runErr == nil || !strings.Contains(runErr.Error(), "manifest error") {
		t.Fatalf("got error %v, want a manifest error", runErr)
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
		// main() prints a plain error before exiting 1; mirror the printing so
		// these tests see the CLI's real stderr, but keep the original error so
		// callers can still assert on what went wrong.
		err := dispatch(command, args)
		var status exitStatus
		if err == nil || errors.As(err, &status) {
			return err
		}
		printError(err)
		return err
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

// runDispatchCaptureOutput runs dispatch with process-global stdout/stderr captured.
func runDispatchCaptureOutput(t *testing.T, command string, args []string) (string, string, error) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdoutReader.Close()
		_ = stderrReader.Close()
	}()
	runErr := func() error {
		os.Stdout = stdoutWriter
		os.Stderr = stderrWriter
		defer func() {
			os.Stdout = oldStdout
			os.Stderr = oldStderr
		}()
		return dispatch(command, args)
	}()
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), runErr
}

// assertFileContent checks a generated file matches expected content exactly.
func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// chdirForTest changes the process working directory until cleanup runs.
func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFmtCommandSmoke checks the CLI can print stable formatted Kizu source.
// The example is compared against itself rather than against a copy of its
// bytes: what `fmt` promises is that formatted source comes back unchanged, and
// pinning the text here would make editing the example a test failure.
func TestFmtCommandSmoke(t *testing.T) {
	const path = "../../examples/hello.kizu"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := kizuCommand("fmt", path).CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != string(want) {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestFmtCommandIsIdempotent keeps token formatting stable on representative sources.
func TestFmtCommandIsIdempotent(t *testing.T) {
	for _, tt := range fmtRepresentativeFixtures() {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempKizuSource(t, tt.name+".kizu", tt.source)
			first, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{path})
			if runErr != nil {
				t.Fatalf("first fmt failed: %v\n%s", runErr, stderr)
			}
			if stderr != "" {
				t.Fatalf("got stderr %q, want empty", stderr)
			}
			formattedPath := writeTempKizuSource(t, tt.name+"_formatted.kizu", first)
			second, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{formattedPath})
			if runErr != nil {
				t.Fatalf("second fmt failed: %v\n%s", runErr, stderr)
			}
			if stderr != "" {
				t.Fatalf("got stderr %q, want empty", stderr)
			}
			if second != first {
				t.Fatalf("fmt is not idempotent\n--- first ---\n%s\n--- second ---\n%s", first, second)
			}
		})
	}
}

// TestFmtCommandOutputParses checks formatting preserves parseability.
func TestFmtCommandOutputParses(t *testing.T) {
	for _, tt := range fmtRepresentativeFixtures() {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempKizuSource(t, tt.name+".kizu", tt.source)
			formatted, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{path})
			if runErr != nil {
				t.Fatalf("fmt failed: %v\n%s", runErr, stderr)
			}
			if stderr != "" {
				t.Fatalf("got stderr %q, want empty", stderr)
			}
			formattedPath := writeTempKizuSource(t, tt.name+"_formatted.kizu", formatted)
			_, stderr, runErr = runDispatchCaptureOutput(t, "parse", []string{formattedPath})
			if runErr != nil {
				t.Fatalf("parse after fmt failed: %v\n%s", runErr, stderr)
			}
			if stderr != "" {
				t.Fatalf("got stderr %q, want empty", stderr)
			}
		})
	}
}

// TestFmtCommandSortsLeadingImports keeps the public formatter import block canonical.
func TestFmtCommandSortsLeadingImports(t *testing.T) {
	path := writeTempKizuSource(t, "imports.kizu", `import app::parser;
import app;
import app::lexer;
fn main(){return;}
`)
	got, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{path})
	if runErr != nil {
		t.Fatalf("fmt failed: %v\n%s", runErr, stderr)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
	want := "import app;\n" +
		"import app::lexer;\n" +
		"import app::parser;\n" +
		"\n" +
		"fn main() {\n" +
		"    return;\n" +
		"}\n"
	if got != want {
		t.Fatalf("fmt imports:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// fmtRepresentativeFixtures returns sources that exercise stable formatter shapes.
func fmtRepresentativeFixtures() []struct {
	name   string
	source string
} {
	return []struct {
		name   string
		source string
	}{
		{
			name: "simple_function",
			source: `fn main(){print("hello, kizu");}
`,
		},
		{
			name: "imports_and_top_level_decls",
			source: `import std::testing;

pub struct User { name: []u8, }

pub fn make_user() -> User { return User { name: "alice" }; }

fn main(){let user = make_user(); print(user.name);}
`,
		},
		{
			name: "struct_literal_and_match_arms",
			source: `pub union Value { Text([]u8), Count(i64), }

fn main() {
    let value = Value::Count(1);
    match value { Text(text) => print(text), Count(count) => print(count), }
}
`,
		},
	}
}

// TestFmtCommandPreservesLeadingLineComments keeps formatter output from dropping trivia.
func TestFmtCommandPreservesLeadingLineComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commented.kizu")
	src := "// keep this comment\nfn main(){print(\"hello, kizu\");}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("fmt", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "// keep this comment\nfn main() {\n    print(\"hello, kizu\");\n}\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestFmtCommandPreservesFunctionDocComments keeps fmt from dropping doc comments.
func TestFmtCommandPreservesFunctionDocComments(t *testing.T) {
	path := writeTempKizuSource(t, "doc-commented.kizu",
		"/// keep this doc\nfn main(){print(\"hello, kizu\");}\n")
	got, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{path})
	if runErr != nil {
		t.Fatalf("fmt failed: %v\n%s", runErr, stderr)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
	want := "/// keep this doc\nfn main() {\n    print(\"hello, kizu\");\n}\n"
	if got != want {
		t.Fatalf("fmt doc comments:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFmtCommandPreservesBlockLineComments keeps non-leading full-line comments.
func TestFmtCommandPreservesBlockLineComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "block-comment.kizu")
	src := "fn main(){\n    // keep this comment\n    print(\"hello, kizu\");\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("fmt", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	want := "fn main() {\n    // keep this comment\n    print(\"hello, kizu\");\n}\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// TestFmtCommandPreservesInlineLineComments keeps inline comments as canonical trivia.
func TestFmtCommandPreservesInlineLineComments(t *testing.T) {
	path := writeTempKizuSource(t, "inline-comment.kizu",
		"fn main() { // keep this comment\n    print(\"hello, kizu\");\n}\n")
	got, stderr, runErr := runDispatchCaptureOutput(t, "fmt", []string{path})
	if runErr != nil {
		t.Fatalf("fmt failed: %v\n%s", runErr, stderr)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
	want := "fn main() {\n    // keep this comment\n    print(\"hello, kizu\");\n}\n"
	if got != want {
		t.Fatalf("fmt inline comments:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFmtCommandRejectsInvalidSyntax checks fmt reports parser failures through the CLI.
func TestFmtCommandRejectsInvalidSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.kizu")
	if err := os.WriteFile(path, []byte("fn main( { return; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, runErr := runDispatchCaptureStderr(t, "fmt", []string{path})
	if runErr == nil {
		t.Fatal("check unexpectedly succeeded")
	}
	// The exact diagnostics are the parser's to choose; what fmt owes is a
	// refusal instead of formatting source that does not parse.
	if !strings.Contains(out, "error: parse failed\n") {
		t.Fatalf("got %q, want a parse failure", out)
	}
}

// TestFmtWritePreservesLeadingLineComments checks --write keeps supported comment trivia.
func TestFmtWritePreservesLeadingLineComments(t *testing.T) {
	src := "// keep this comment\nfn main(){print(\"hello, kizu\");}\n"
	path := filepath.Join(t.TempDir(), "commented.kizu")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, runErr := runDispatchCaptureStderr(t, "fmt", []string{"--write", path})
	if runErr != nil {
		t.Fatalf("command failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := "// keep this comment\nfn main() {\n    print(\"hello, kizu\");\n}\n"
	if string(got) != want {
		t.Fatalf("file changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFmtWritePreservesInlineLineComments checks --write keeps inline comment trivia.
func TestFmtWritePreservesInlineLineComments(t *testing.T) {
	src := "fn main() { // keep this comment\n    print(\"hello, kizu\");\n}\n"
	path := filepath.Join(t.TempDir(), "inline-comment.kizu")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, runErr := runDispatchCaptureStderr(t, "fmt", []string{"--write", path})
	if runErr != nil {
		t.Fatalf("command failed: %v\n%s", runErr, out)
	}
	if out != "" {
		t.Fatalf("got stderr %q, want empty", out)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := "fn main() {\n    // keep this comment\n    print(\"hello, kizu\");\n}\n"
	if string(got) != want {
		t.Fatalf("file changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestCheckPackageCommandSmoke checks package roots can be statically checked.
func TestCheckPackageCommandSmoke(t *testing.T) {
	cmd := kizuCommand("check", "../../examples/modules/same_module_helper_lookup")
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
	cmd := kizuCommand("run", "../../examples/modules/compiler_phases")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "7\n" {
		t.Fatalf("got %q, want package run output", out)
	}
}

// TestTestPackageCommandSmoke checks package roots can run assertion tests.
func TestTestPackageCommandSmoke(t *testing.T) {
	cmd := kizuCommand("test", "../../examples/modules/same_module_helper_lookup")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "test: ok\n" {
		t.Fatalf("got %q, want package test output", out)
	}
}

// TestTestFileCommandSmoke checks a file test block reports success.
func TestTestFileCommandSmoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample_test.kizu")
	source := `import std;

test "sample" {
    std::testing::expect(true);
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, runErr := runDispatchCaptureOutput(t, "test", []string{path})
	if runErr != nil {
		t.Fatalf("command failed: %v\nstdout:\n%s\nstderr:\n%s", runErr, stdout, stderr)
	}
	if stdout != "test: ok\n" {
		t.Fatalf("got stdout %q, want test ok", stdout)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
}

// TestTestFileCommandDoesNotRunMain keeps kizu test scoped to test blocks.
func TestTestFileCommandDoesNotRunMain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main_only.kizu")
	source := `fn main() {
    print("main");
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, runErr := runDispatchCaptureOutput(t, "test", []string{path})
	if runErr == nil || !strings.Contains(runErr.Error(), "test error: no tests found") {
		t.Fatalf("got error %v, want no tests found", runErr)
	}
	if stdout != "" {
		t.Fatalf("got stdout %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("got stderr %q, want empty", stderr)
	}
}

// TestRunCompilerPhasesPackageSmoke checks the compiler-phase-shaped module APIs.
func TestRunCompilerPhasesPackageSmoke(t *testing.T) {
	cmd := kizuCommand("run", "../../examples/modules/compiler_phases")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if string(out) != "7\n" {
		t.Fatalf("got %q, want compiler phase output", out)
	}
}

// TestRunCompilerPhasesStopsAfterParseError checks try prevents later phases.
//
// The typed diagnostic itself is not asserted here: a returned error union is
// not surfaced by the native path yet, which the `negative_compiler_phases_fail`
// pending entry tracks. Restore that assertion together with the entry.
func TestRunCompilerPhasesStopsAfterParseError(t *testing.T) {
	cmd := kizuCommand("run", "../../examples/modules/compiler_phases_fail")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	if strings.Contains(string(out), "lowered") {
		t.Fatalf("got %q, want lowering output to be skipped", out)
	}
}

// TestIROptCommandSmoke checks the CLI can dump optimized typed SSA IR.
func TestIROptCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(source, []byte(`fn main() { print(1 + 2); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("ir", "--opt", source)
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
	cmd := kizuCommand("build", "--emit-llvm", "../../examples/hello.kizu")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"declare void @kizu_runtime_init_args(i32, ptr)",
		"define i32 @main(i32 %kizu.argc, ptr %kizu.argv)",
		"call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildEmitLLVMPackageCommandSmoke checks package graphs can reach LLVM lowering.
func TestBuildEmitLLVMPackageCommandSmoke(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(
		"[package]\nname = \"app\"\n\n[modules]\npaths = [\"src\"]\n",
	)
	if err := os.WriteFile(filepath.Join(root, "kizu.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mainSource := []byte(`import app::math;

pub fn main() -> void {
    print(math::answer());
    return;
}
`)
	if err := os.WriteFile(filepath.Join(srcDir, "main.kizu"), mainSource, 0o644); err != nil {
		t.Fatal(err)
	}
	helperSource := []byte(`pub fn answer() -> i64 {
    return 42;
}
`)
	mathDir := filepath.Join(srcDir, "math")
	if err := os.Mkdir(mathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mathDir, "math.kizu"), helperSource, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := kizuCommand("build", "--emit-llvm", root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"define i64 @app__math__answer()",
		"define void @app__main()",
		"call i64 @app__math__answer()",
		"call void @kizu_print_int(i64",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildEmitLLVMStructCommandSmoke checks struct values reach LLVM lowering.
func TestBuildEmitLLVMStructCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "struct.kizu")
	code := []byte(`struct User { age: i64, }
fn main() {
    let user = User { age: 30 };
    print(user.age);
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("build", "--emit-llvm", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"%kizu.struct.User = type { i64 }",
		"insertvalue %kizu.struct.User zeroinitializer, i64 30, 0",
		"extractvalue %kizu.struct.User",
		"call void @kizu_print_int(i64",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildEmitLLVMErrorUnionCommandSmoke checks scalar !T values reach LLVM lowering.
func TestBuildEmitLLVMErrorUnionCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "error_union.kizu")
	code := []byte(`fn read() -> !i64 {
    return 1;
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("build", "--emit-llvm", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"%kizu.error.i64 = type { i8, i64, i64 }",
		"define %kizu.error.i64 @read()",
		"insertvalue %kizu.error.i64 zeroinitializer, i8 1, 0",
		"br i1 %kizu.2.ok.bool, label %kizu.2.try.ok, label %kizu.2.try.err",
		// A failed try in main reports its message before exiting 1 instead of
		// failing silently.
		"call void @kizu_main_error_message(",
		"  ret i32 1",
		"call void @kizu_print_int(i64 %kizu.2)",
		"  ret i32 0",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildEmitLLVMSliceCommandSmoke checks []u8 values use the slice ABI.
func TestBuildEmitLLVMSliceCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "slice.kizu")
	code := []byte(`fn identity(value: []u8) -> []u8 {
    return value;
}
fn message() -> []u8 {
    return "hello";
}
fn read() -> ![]u8 {
    return identity(message());
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("build", "--emit-llvm", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"%kizu.slice.u8 = type { ptr, i64 }",
		"%kizu.error.slice.u8 = type { i8, %kizu.slice.u8, i64 }",
		"define %kizu.slice.u8 @identity(%kizu.slice.u8 %kizu.value)",
		"define %kizu.error.slice.u8 @read()",
		"call %kizu.slice.u8 @identity(%kizu.slice.u8",
		"insertvalue %kizu.error.slice.u8",
		"extractvalue %kizu.slice.u8 %kizu.2, 0",
		"call void @kizu_print_string(ptr",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("got %q, want substring %q", out, want)
		}
	}
}

// TestBuildEmitLLVMOptCommandSmoke checks LLVM build can use optimized IR.
func TestBuildEmitLLVMOptCommandSmoke(t *testing.T) {
	source := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(source, []byte(`fn main() { print(1 + 2); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("build", "--emit-llvm", "--opt", source)
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
	cmd := kizuCommand(
		"build", "--target", "wasm32-wasi", "../../examples/hello.kizu",
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
	build := kizuCommand(
		"build", "--target", "native",
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

// TestBuildTargetNativeOptCommandSmoke checks --opt reaches the native linker
// command and still produces a runnable executable.
func TestBuildTargetNativeOptCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	output := filepath.Join(t.TempDir(), "hello-opt")
	build := kizuCommand(
		"build", "--target", "native", "--opt",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, "../../examples/hello.kizu",
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native opt build failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != output {
		t.Fatalf("got %q, want output path %q", out, output)
	}
	assertNativeMetadata(t, output+".kizu-build.json", output)
	run := exec.Command(output)
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native opt executable failed: %v\n%s", err, out)
	}
	if string(out) != "hello, kizu\n" {
		t.Fatalf("got %q", out)
	}
}

// TestBuildTargetNativeErrorUnionCommandSmoke checks native builds preserve try control flow.
func TestBuildTargetNativeErrorUnionCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "error_union.kizu")
	code := []byte(`fn read() -> !i64 {
    return 7;
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "error_union")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "7\n" {
		t.Fatalf("got %q", got)
	}
}

// TestBuildTargetNativeErrorUnionFailureSmoke checks a failing main names its
// error on stderr and exits 1.
func TestBuildTargetNativeErrorUnionFailureSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	failSource := filepath.Join(t.TempDir(), "error_union_fail.kizu")
	failCode := []byte(`error ReadError {
    Bad,
}
fn read() -> !i64 {
    return ReadError::Bad;
}
fn main() -> !void {
    let value = try read();
    print(value);
    return;
}`)
	if err := os.WriteFile(failSource, failCode, 0o644); err != nil {
		t.Fatal(err)
	}
	failOutput := filepath.Join(t.TempDir(), "error_union_fail")
	failBuild := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", failOutput, failSource,
	)
	if failOut, err := failBuild.CombinedOutput(); err != nil {
		t.Fatalf("native failure build failed: %v\n%s", err, failOut)
	}
	failRun := exec.Command(failOutput)
	failRunOut, err := failRun.CombinedOutput()
	if err == nil {
		t.Fatalf("expected native executable to fail, got output %q", failRunOut)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("got err=%v output=%q, want exit 1", err, failRunOut)
	}
	// A failed main names its error on stderr before exiting 1.
	if string(failRunOut) != "runtime error: ReadError::Bad\n" {
		t.Fatalf("got output %q, want %q", failRunOut, "runtime error: ReadError::Bad\n")
	}
}

// TestBuildTargetNativeStdoutWriteFailure checks hosted writes report a closed
// output pipe through std::io instead of terminating on SIGPIPE or returning ok.
func TestBuildTargetNativeStdoutWriteFailure(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "stdout_write_failure.kizu")
	code := []byte(`import std;

fn main() -> !void {
    let io = std::io::blocking();
    try std::io::write_stdout(io, "hello");
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "stdout_write_failure")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	if buildOut, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native write failure build failed: %v\n%s", err, buildOut)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	var stderr strings.Builder
	run := exec.Command(output)
	run.Stdout = writer
	run.Stderr = &stderr
	err = run.Run()
	if err == nil {
		t.Fatal("expected native executable to report stdout write failure")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("got err=%v stderr=%q, want exit 1", err, stderr.String())
	}
	if got, want := stderr.String(), "runtime error: std::io::Error::WriteFailed\n"; got != want {
		t.Fatalf("got stderr %q, want %q", got, want)
	}
}

// TestBuildTargetNativeProcessSpawnCommandSmoke checks the hosted native runtime spawn shim.
func TestBuildTargetNativeProcessSpawnCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	truePath := "/usr/bin/true"
	if _, err := os.Stat(truePath); err != nil {
		t.Skipf("%s is required for native process spawn smoke", truePath)
	}
	source := filepath.Join(t.TempDir(), "spawn.kizu")
	code := []byte(`import std;

fn main() -> !void {
    let code = try std::process::spawn_wait8(1, "/usr/bin/true", "", "", "", "", "", "", "");
    print(code);
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "spawn")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "0\n" {
		t.Fatalf("got %q", got)
	}
}

// TestBuildTargetNativeProcessProfileHelpersSmoke checks profile helper primitives.
func TestBuildTargetNativeProcessProfileHelpersSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "process_profile_helpers.kizu")
	code := []byte(`import std;

fn main() -> void {
    let missing = std::process::env("KIZU_TEST_PROCESS_PROFILE_MISSING") orelse "";
    print(std::mem::len(missing));
    let present = std::process::env("KIZU_TEST_PROCESS_PROFILE_PRESENT") orelse "";
    print(present);
    let before = std::process::monotonic_millis();
    let after = std::process::monotonic_millis();
    if after < before {
        print("time-regressed");
    } else {
        print("time-ok");
    }
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "process_profile_helpers")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	run.Env = append(os.Environ(), "KIZU_TEST_PROCESS_PROFILE_PRESENT=profile-ok")
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "0\nprofile-ok\ntime-ok\n" {
		t.Fatalf("got %q", got)
	}
}

// TestBuildTargetNativeReturnedArrayFieldCommandSmoke keeps runtime-backed Array
// handles intact when they are stored in a struct returned from a native function.
func TestBuildTargetNativeReturnedArrayFieldCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "returned_array_field.kizu")
	code := []byte(`import std;

struct Bag { values: std::array::Array<i64>, }
fn (self: Bag) deinit() -> void {
    self.values.deinit();
    return;
}
fn make_bag() -> !Bag {
    var values = std::array::new<i64>(std::mem::page_allocator());
    errdefer values.deinit();
    try values.append(10);
    try values.append(20);
    return Bag { values: move values };
}
fn print_bag_len(bag: &Bag) -> void {
    print(bag.values.len());
    return;
}
fn main() -> !void {
    let bag = try make_bag();
    print(bag.values.len());
    print_bag_len(&bag);
    bag.deinit();
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "returned_array_field")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "2\n2\n" {
		t.Fatalf("got %q", got)
	}
}

// TestBuildTargetNativeReturnedUnionArrayBorrowCommandSmoke keeps Array.at()
// borrows as pointer values when dispatching on owner-union elements.
func TestBuildTargetNativeReturnedUnionArrayBorrowCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "returned_union_array_borrow.kizu")
	code := []byte(`import std;

union Stmt { Add(i64), Done(i64), }
struct Bag { stmts: std::array::Array<Stmt>, }
fn (self: Bag) deinit() -> void {
    self.stmts.deinit();
    return;
}
fn make_bag() -> !Bag {
    var stmts = std::array::new<Stmt>(std::mem::page_allocator());
    errdefer stmts.deinit();
    try stmts.append(Stmt::Add(10));
    try stmts.append(Stmt::Done(20));
    return Bag { stmts: move stmts };
}
fn print_stmt(stmt: &Stmt) -> void {
    match stmt {
        Add(value) => print(value),
        Done(value) => print(value),
    }
    return;
}
fn render_bag(bag: &Bag) -> !void {
    let stmts = &bag.stmts;
    var index = 0;
    while stmts.at(index) |stmt| {
        print_stmt(stmt);
        index = index + 1;
    }
    return;
}
fn main() -> !void {
    let bag = try make_bag();
    defer bag.deinit();
    try render_bag(&bag);
    return;
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "returned_union_array_borrow")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "10\n20\n" {
		t.Fatalf("got %q", got)
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
		OptMode string   `json:"optimization_mode"`
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
	if got.OptMode != "debug" && got.OptMode != "opt" {
		t.Fatalf("unexpected optimization metadata: %+v", got)
	}
	if len(got.Command) == 0 || got.Command[0] != "clang" {
		t.Fatalf("unexpected command metadata: %+v", got.Command)
	}
	wantFlag := "-O0"
	if got.OptMode == "opt" {
		wantFlag = "-O2"
	}
	if !slices.Contains(got.Command, wantFlag) {
		t.Fatalf("metadata command %v missing %s", got.Command, wantFlag)
	}
}

// TestBuildTargetNativeArenaCommandSmoke checks native builds can lower arena storage.
func TestBuildTargetNativeArenaCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "arena.kizu")
	code := []byte(`import std;

struct User { age: i64, }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let alice = users.add(User { age: 41 });
    print(users.at(alice).age);
    users.deinit();
}`)
	if err := os.WriteFile(source, code, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "arena")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "41\n" {
		t.Fatalf("got %q", got)
	}
}

// TestBuildTargetNativeWhileContinuePhiCommandSmoke keeps explicit continue
// edges wired into loop phi nodes before LLVM verification.
func TestBuildTargetNativeWhileContinuePhiCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "while_continue_phi.kizu")
	if err := os.WriteFile(source, []byte(nativeWhileContinuePhiSource), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "while_continue_phi")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "true\n" {
		t.Fatalf("got %q", got)
	}
}

const nativeWhileContinuePhiSource = `fn main() {
    var index = 0;
    var in_string = false;
    while index < 3 {
        if in_string {
            in_string = false;
            index = index + 1;
            continue;
        }
        in_string = true;
        index = index + 1;
    }
    print(in_string);
}`

// TestBuildTargetNativeWhileBreakPhiCommandSmoke keeps values assigned before
// explicit break edges visible after a loop.
func TestBuildTargetNativeWhileBreakPhiCommandSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	source := filepath.Join(t.TempDir(), "while_break_phi.kizu")
	if err := os.WriteFile(source, []byte(nativeWhileBreakPhiSource), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "while_break_phi")
	build := kizuCommand(
		"build", "--target", "native",
		"--libc", "on", "--runtime", "hosted", "--emit", "exe",
		"-o", output, source,
	)
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native build failed: %v\n%s", err, out)
	}
	run := exec.Command(output)
	runOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("native executable failed: %v\n%s", err, runOut)
	}
	if got := string(runOut); got != "true\n" {
		t.Fatalf("got %q", got)
	}
}

const nativeWhileBreakPhiSource = `fn main() {
    var index = 0;
    var found = false;
    while index < 3 {
        if index == 1 {
            found = true;
            break;
        }
        index = index + 1;
    }
    print(found);
}`

// TestCacheCommands checks cache status and prune.
func TestCacheCommands(t *testing.T) {
	cacheDir := t.TempDir()
	build := kizuCommand("build", "--emit-llvm", "../../examples/hello.kizu")
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	status := kizuCommand("cache", "status")
	status.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "entries: 1") {
		t.Fatalf("got %q", out)
	}
	prune := kizuCommand("cache", "prune")
	prune.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err = prune.CombinedOutput(); err != nil {
		t.Fatalf("prune failed: %v\n%s", err, out)
	}
}

// TestBuildOptUsesSeparateCacheEntry checks optimization level shapes cache keys.
func TestBuildOptUsesSeparateCacheEntry(t *testing.T) {
	cacheDir := t.TempDir()
	source := "../../examples/hello.kizu"
	plain := kizuCommand("build", "--emit-llvm", source)
	plain.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := plain.CombinedOutput(); err != nil {
		t.Fatalf("plain build failed: %v\n%s", err, out)
	}
	opt := kizuCommand("build", "--emit-llvm", "--opt", source)
	opt.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := opt.CombinedOutput(); err != nil {
		t.Fatalf("opt build failed: %v\n%s", err, out)
	}
	status := kizuCommand("cache", "status")
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
	cmd := kizuCommand("import-c-header", header)
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
	cmd := kizuCommand("import-c-header", header)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected import to fail\n%s", out)
	}
	want := "c import error: variadic functions are unsupported"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// TestRunCommandRejectsMoveError checks run does not bypass static move checks.
func TestRunCommandRejectsMoveError(t *testing.T) {
	cmd := kizuCommand("run", "../../examples/move_error.kizu")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	want := "move error: moved value `name` was used"
	if !strings.Contains(string(out), want) {
		t.Fatalf("got %q, want substring %q", out, want)
	}
}

// writeTempKizuSource writes one temporary Kizu source fixture.
func writeTempKizuSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
