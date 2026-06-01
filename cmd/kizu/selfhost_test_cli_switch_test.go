package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfhostTestCliSwitchEnvGate pins the rollback-friendly switch point for the
// public `test` command (#1157 / parent #1070). It stays clang-free so it always
// guards against the gate being removed or the test dispatch being hardcoded back
// to the Go interpreter path.
func TestSelfhostTestCliSwitchEnvGate(t *testing.T) {
	// selfhostTestEnabled only reacts to the documented opt-in value.
	t.Setenv(selfhostTestEnvVar, "")
	if selfhostTestEnabled() {
		t.Fatalf("expected test switch disabled when %s is empty", selfhostTestEnvVar)
	}
	t.Setenv(selfhostTestEnvVar, "0")
	if selfhostTestEnabled() {
		t.Fatalf("expected test switch disabled when %s=0", selfhostTestEnvVar)
	}
	t.Setenv(selfhostTestEnvVar, "1")
	if !selfhostTestEnabled() {
		t.Fatalf("expected test switch enabled when %s=1", selfhostTestEnvVar)
	}

	// The test dispatch must route through the selfhost frontend only behind the
	// gate, with the Go interpreter path preserved as the default. Reading the
	// dispatch source keeps this regression guard independent of clang/runtime.
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	dispatch := testCliSwitchDispatchSlice(string(source))
	if dispatch == "" {
		t.Fatalf("could not extract the test dispatch case from main.go")
	}
	for _, fragment := range []string{
		"if selfhostTestEnabled() {",
		"return runSelfhostFrontendCommand(\"test\", args)",
		"return testFile(path, programArgs)",
	} {
		if !strings.Contains(dispatch, fragment) {
			t.Fatalf("test dispatch missing %q; gate or default path changed:\n%s", fragment, dispatch)
		}
	}
}

// TestSelfhostTestCliSwitchRoutesThroughSelfhost proves the switched test path is
// selfhost-owned end to end: a supported shape lowers, emits, links, and executes
// the native test artifact (printing `test: ok`), while an unsupported shape
// surfaces an explicit selfhost diagnostic with no Go interpreter fallback. It
// records production evidence that names the switched path and its fallback status.
func TestSelfhostTestCliSwitchRoutesThroughSelfhost(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_TEST_CLI_SWITCH") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_TEST_CLI_SWITCH=1 to run the selfhost test cli switch gate")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for the selfhost test artifact path")
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "kizu")
	build := exec.Command("go", "build", "-o", bin, "./cmd/kizu")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build kizu: %v\n%s", err, out)
	}

	const supported = "selfhost/tests/cli/test_expect_ok.kizu"
	const unsupported = "selfhost/tests/cli/test_if_unsupported.kizu"

	// Gate on: the supported shape is selfhost-owned end to end. The printed
	// output comes from executing the linked native test artifact, not the Go
	// interpreter test runner.
	onStdout, _, onCode := testCliSwitchCommand(t, bin, repoRoot, true, supported)
	if onCode != 0 {
		t.Fatalf("selfhost test %s exit = %d, want 0", supported, onCode)
	}
	if onStdout != "test: ok\n" {
		t.Fatalf("selfhost test %s stdout = %q, want %q", supported, onStdout, "test: ok\n")
	}

	// Gate on: the unsupported shape is an explicit selfhost diagnostic, never the
	// Go interpreter output ("test: ok"). This is the no-Go-fallback guarantee.
	badStdout, badStderr, badCode := testCliSwitchCommand(t, bin, repoRoot, true, unsupported)
	if badCode == 0 {
		t.Fatalf("selfhost test %s exit = 0, want explicit diagnostic", unsupported)
	}
	if strings.Contains(badStdout, "test: ok") {
		t.Fatalf("selfhost test %s leaked Go interpreter output:\nstdout=%q\nstderr=%q",
			unsupported, badStdout, badStderr)
	}
	diagnostic := strings.TrimSpace(badStderr)
	if diagnostic == "" {
		diagnostic = strings.TrimSpace(badStdout)
	}

	// Gate off: the same unsupported shape stays on the default Go path, which runs
	// it through the interpreter test runner and prints `test: ok`. This confirms
	// the switch is gated rather than a broadened default.
	offStdout, _, offCode := testCliSwitchCommand(t, bin, repoRoot, false, unsupported)
	if offCode != 0 {
		t.Fatalf("default Go test %s exit = %d, want 0", unsupported, offCode)
	}
	if !strings.Contains(offStdout, "test: ok") {
		t.Fatalf("default Go test %s stdout = %q, want Go interpreter output", unsupported, offStdout)
	}

	if err := writeTestCliSwitchReport(supported, unsupported, diagnostic); err != nil {
		t.Fatalf("write test cli switch report: %v", err)
	}
}

// testCliSwitchCommand runs `kizu test <target>` with the switch env var set or
// cleared and returns stdout, stderr, and the exit code.
func testCliSwitchCommand(
	t *testing.T,
	bin string,
	repoRoot string,
	selfhost bool,
	target string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, "test", target)
	cmd.Dir = repoRoot
	env := append(os.Environ(), selfhostTestEnvVar+"=0")
	if selfhost {
		env[len(env)-1] = selfhostTestEnvVar + "=1"
	}
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatalf("test %s: %v\n%s", target, err, stderr.String())
		}
	}
	return stdout.String(), stderr.String(), code
}

// writeTestCliSwitchReport records the switched test path and its fallback status
// as stable production evidence for #1157.
func writeTestCliSwitchReport(supported, unsupported, diagnostic string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "kizu-selfhost-test-cli-switch-v0\n")
	fmt.Fprintf(&b, "issue #1157\n")
	fmt.Fprintf(&b, "switch.command test\n")
	fmt.Fprintf(&b, "switch.point env:%s\n", selfhostTestEnvVar)
	fmt.Fprintf(&b, "switch.owner selfhost::cli::execute::test_file_cli\n")
	fmt.Fprintf(&b, "switch.path %s\n",
		"lower-test-executable -> emit-llvm -> link -> execute-native-artifact")
	fmt.Fprintf(&b, "supported.case %s\n", supported)
	fmt.Fprintf(&b, "supported.stdout.sha256 %s\n", textFingerprint("test: ok\n"))
	fmt.Fprintf(&b, "unsupported.case %s\n", unsupported)
	fmt.Fprintf(&b, "unsupported.diagnostic %s\n", diagnostic)
	fmt.Fprintf(&b, "go.fallback none\n")
	dir := filepath.Join("target", "selfhost", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "test-cli-switch.txt"), []byte(b.String()), 0o644)
}

// testCliSwitchDispatchSlice extracts the `test` dispatch case body from main.go so
// the env-gate guard can assert its shape without runtime dependencies.
func testCliSwitchDispatchSlice(source string) string {
	start := strings.Index(source, "case \"test\":")
	if start < 0 {
		return ""
	}
	rest := source[start:]
	end := strings.Index(rest, "case \"fmt\":")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
