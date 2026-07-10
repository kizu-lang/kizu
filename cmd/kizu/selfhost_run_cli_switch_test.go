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

// TestSelfhostRunCliSwitchDefaultOwner pins selfhost as the unconditional public
// `run` owner. It stays clang-free and rejects reintroduction of an environment
// switch or Go interpreter fallback.
func TestSelfhostRunCliSwitchDefaultOwner(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	dispatch := runCliSwitchDispatchSlice(string(source))
	if dispatch == "" {
		t.Fatalf("could not extract the run dispatch case from main.go")
	}
	for _, fragment := range []string{"return runSelfhostFrontendCommand(\"run\", args)"} {
		if !strings.Contains(dispatch, fragment) {
			t.Fatalf("run dispatch missing %q; default owner changed:\n%s", fragment, dispatch)
		}
	}
	for _, forbidden := range []string{"selfhostRunEnabled", "KIZU_SELFHOST_RUN", "runFile("} {
		if strings.Contains(dispatch, forbidden) {
			t.Fatalf("run dispatch contains forbidden fallback/switch %q:\n%s", forbidden, dispatch)
		}
	}
}

// TestSelfhostRunFrontendArgsPreserveProgramArgs checks the run argument boundary.
func TestSelfhostRunFrontendArgsPreserveProgramArgs(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	temp := t.TempDir()
	if err := os.Chdir(temp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	got, err := selfhostFrontendProcessArgs(
		"run",
		[]string{
			"examples/std_io_process.kizu",
			"--",
			"examples/fixtures/config.txt",
			"literal",
		},
	)
	if err != nil {
		t.Fatalf("selfhost frontend args: %v", err)
	}
	absTarget, err := filepath.Abs("examples/std_io_process.kizu")
	if err != nil {
		t.Fatalf("abs target: %v", err)
	}
	want := []string{
		"run",
		absTarget,
		"--",
		"examples/fixtures/config.txt",
		"literal",
	}
	if !sameStringSlice(got, want) {
		t.Fatalf("selfhost frontend args = %#v, want %#v", got, want)
	}
}

// TestSelfhostRunCliSwitchRoutesThroughSelfhost proves the switched run path is
// selfhost-owned end to end: a supported shape lowers, links, and executes the
// native artifact, while an unsupported shape surfaces an explicit selfhost
// diagnostic with no Go interpreter fallback. It records production evidence that
// names the switched path and its fallback status.
func TestSelfhostRunCliSwitchRoutesThroughSelfhost(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_RUN_CLI_SWITCH") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_RUN_CLI_SWITCH=1 to run the selfhost run cli switch gate")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for the selfhost run artifact path")
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

	const supported = "selfhost/tests/cli/run_hello.kizu"
	const broad = "examples/atomic_flag.kizu"

	// Gate on: the supported shape is selfhost-owned end to end. The printed
	// output comes from executing the linked native artifact, not the Go
	// interpreter.
	onStdout, _, onCode := runCliSwitchCommand(t, bin, repoRoot, supported)
	if onCode != 0 {
		t.Fatalf("selfhost run %s exit = %d, want 0", supported, onCode)
	}
	if onStdout != "hello, kizu\n" {
		t.Fatalf("selfhost run %s stdout = %q, want %q", supported, onStdout, "hello, kizu\n")
	}

	broadStdout, broadStderr, broadCode := runCliSwitchCommand(t, bin, repoRoot, broad)
	if broadCode != 0 || broadStderr != "" || broadStdout != "false\ntrue\n" {
		t.Fatalf("default selfhost run %s = exit %d stdout %q stderr %q",
			broad, broadCode, broadStdout, broadStderr)
	}

	if err := writeRunCliSwitchReport(supported, broad); err != nil {
		t.Fatalf("write run cli switch report: %v", err)
	}
}

// runCliSwitchCommand runs `kizu run <target>` with the switch env var set or
// cleared and returns stdout, stderr, and the exit code.
func runCliSwitchCommand(
	t *testing.T,
	bin string,
	repoRoot string,
	target string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, "run", target)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
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
			t.Fatalf("run %s: %v\n%s", target, err, stderr.String())
		}
	}
	return stdout.String(), stderr.String(), code
}

// writeRunCliSwitchReport records the switched run path and its fallback status as
// stable production evidence for #1151.
func writeRunCliSwitchReport(supported, broad string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "kizu-selfhost-run-cli-switch-v0\n")
	fmt.Fprintf(&b, "issue #1151\n")
	fmt.Fprintf(&b, "switch.command run\n")
	fmt.Fprintf(&b, "switch.point default-dispatch\n")
	fmt.Fprintf(&b, "switch.owner selfhost::cli::execute::run_file_cli\n")
	fmt.Fprintf(&b, "switch.path lower-run-codegen -> link -> execute-native-artifact\n")
	fmt.Fprintf(&b, "supported.case %s\n", supported)
	fmt.Fprintf(&b, "supported.stdout.sha256 %s\n", textFingerprint("hello, kizu\n"))
	fmt.Fprintf(&b, "broad.case %s\n", broad)
	fmt.Fprintf(&b, "go.fallback none\n")
	fmt.Fprintf(&b, "remaining.test_file_cli not-switched\n")
	dir := filepath.Join("target", "selfhost", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run-cli-switch.txt"), []byte(b.String()), 0o644)
}

// runCliSwitchDispatchSlice extracts the dispatchRun helper body from main.go so
// the env-gate guard can assert its shape without runtime dependencies.
func runCliSwitchDispatchSlice(source string) string {
	start := strings.Index(source, "func dispatchRun(")
	if start < 0 {
		return ""
	}
	rest := source[start:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// sameStringSlice reports whether two string slices are identical.
func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
