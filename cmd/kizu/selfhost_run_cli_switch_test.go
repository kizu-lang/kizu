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

// TestSelfhostRunCliSwitchEnvGate pins the rollback-friendly switch point for the
// public `run` command (#1151 / parent #1070). It stays clang-free so it always
// guards against the gate being removed or the run dispatch being hardcoded back
// to the Go interpreter path.
func TestSelfhostRunCliSwitchEnvGate(t *testing.T) {
	// selfhostRunEnabled only reacts to the documented opt-in value.
	t.Setenv(selfhostRunEnvVar, "")
	if selfhostRunEnabled() {
		t.Fatalf("expected run switch disabled when %s is empty", selfhostRunEnvVar)
	}
	t.Setenv(selfhostRunEnvVar, "0")
	if selfhostRunEnabled() {
		t.Fatalf("expected run switch disabled when %s=0", selfhostRunEnvVar)
	}
	t.Setenv(selfhostRunEnvVar, "1")
	if !selfhostRunEnabled() {
		t.Fatalf("expected run switch enabled when %s=1", selfhostRunEnvVar)
	}

	// The run dispatch must route through the selfhost frontend only behind the
	// gate, with the Go interpreter path preserved as the default. Reading the
	// dispatch source keeps this regression guard independent of clang/runtime.
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	dispatch := runCliSwitchDispatchSlice(string(source))
	if dispatch == "" {
		t.Fatalf("could not extract the run dispatch case from main.go")
	}
	for _, fragment := range []string{
		"if selfhostRunEnabled() {",
		"return runSelfhostFrontendCommand(\"run\", args)",
		"return runFile(path, programArgs)",
	} {
		if !strings.Contains(dispatch, fragment) {
			t.Fatalf("run dispatch missing %q; gate or default path changed:\n%s", fragment, dispatch)
		}
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

	// The unsupported fixture has to be a shape selfhost genuinely defers, and its Go
	// output has to be non-empty: the leak check below is a strings.Contains, which any
	// string satisfies against "". examples/atomic_flag.kizu was this fixture until
	// selfhost learned to run it end to end, at which point the case stopped testing
	// anything and started failing. A &var struct field write is deferred with the
	// explicit "run source lowering not supported" diagnostic, and "bob\nbob\n" cannot
	// occur in that diagnostic.
	const unsupported = "examples/mutable_borrow.kizu"
	const unsupportedGoOutput = "bob\nbob\n"

	// Gate on: the supported shape is selfhost-owned end to end. The printed
	// output comes from executing the linked native artifact, not the Go
	// interpreter.
	onStdout, _, onCode := runCliSwitchCommand(t, bin, repoRoot, true, supported)
	if onCode != 0 {
		t.Fatalf("selfhost run %s exit = %d, want 0", supported, onCode)
	}
	if onStdout != "hello, kizu\n" {
		t.Fatalf("selfhost run %s stdout = %q, want %q", supported, onStdout, "hello, kizu\n")
	}

	// Gate on: the unsupported shape is an explicit selfhost diagnostic, never the
	// Go interpreter output. This is the no-Go-fallback guarantee.
	badStdout, badStderr, badCode := runCliSwitchCommand(t, bin, repoRoot, true, unsupported)
	if badCode == 0 {
		t.Fatalf("selfhost run %s exit = 0, want explicit diagnostic", unsupported)
	}
	leakedStdout := strings.Contains(badStdout, unsupportedGoOutput)
	leakedStderr := strings.Contains(badStderr, unsupportedGoOutput)
	if leakedStdout || leakedStderr {
		t.Fatalf("selfhost run %s leaked Go interpreter output:\nstdout=%q\nstderr=%q",
			unsupported, badStdout, badStderr)
	}
	diagnostic := strings.TrimSpace(badStderr)
	if diagnostic == "" {
		diagnostic = strings.TrimSpace(badStdout)
	}

	// Gate off: the same unsupported shape stays on the default Go path, which
	// runs it through the interpreter. This confirms the switch is gated rather
	// than a broadened default.
	offStdout, _, offCode := runCliSwitchCommand(t, bin, repoRoot, false, unsupported)
	if offCode != 0 {
		t.Fatalf("default Go run %s exit = %d, want 0", unsupported, offCode)
	}
	if offStdout != unsupportedGoOutput {
		t.Fatalf("default Go run %s stdout = %q, want Go interpreter output", unsupported, offStdout)
	}

	if err := writeRunCliSwitchReport(supported, unsupported, diagnostic); err != nil {
		t.Fatalf("write run cli switch report: %v", err)
	}
}

// runCliSwitchCommand runs `kizu run <target>` with the switch env var set or
// cleared and returns stdout, stderr, and the exit code.
func runCliSwitchCommand(
	t *testing.T,
	bin string,
	repoRoot string,
	selfhost bool,
	target string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, "run", target)
	cmd.Dir = repoRoot
	env := append(os.Environ(), selfhostRunEnvVar+"=0")
	if selfhost {
		env[len(env)-1] = selfhostRunEnvVar + "=1"
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
			t.Fatalf("run %s: %v\n%s", target, err, stderr.String())
		}
	}
	return stdout.String(), stderr.String(), code
}

// writeRunCliSwitchReport records the switched run path and its fallback status as
// stable production evidence for #1151.
func writeRunCliSwitchReport(supported, unsupported, diagnostic string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "kizu-selfhost-run-cli-switch-v0\n")
	fmt.Fprintf(&b, "issue #1151\n")
	fmt.Fprintf(&b, "switch.command run\n")
	fmt.Fprintf(&b, "switch.point env:%s\n", selfhostRunEnvVar)
	fmt.Fprintf(&b, "switch.owner selfhost::cli::execute::run_file_cli\n")
	fmt.Fprintf(&b, "switch.path lower-run-codegen -> link -> execute-native-artifact\n")
	fmt.Fprintf(&b, "supported.case %s\n", supported)
	fmt.Fprintf(&b, "supported.stdout.sha256 %s\n", textFingerprint("hello, kizu\n"))
	fmt.Fprintf(&b, "unsupported.case %s\n", unsupported)
	fmt.Fprintf(&b, "unsupported.diagnostic %s\n", diagnostic)
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
