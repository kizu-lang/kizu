package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kizu-lang/kizu/internal/conformance"
)

// repoRoot is where cases are read from and where the CLI is run.
const repoRoot = "../.."

var conformanceProcessMu sync.Mutex

type conformanceRunner func(t *testing.T, tt conformance.Case) (string, error)

// TestConformance runs every case the tree declares. Discovery walks the tree
// rather than a registry, so an example is a case by existing: there is no
// list it can be left out of.
func TestConformance(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	runConformanceCases(t, cases, runShippingConformanceCommand)
}

// TestSelfhostBehavior runs the executable behavior that must cross the
// selfhost CLI boundary: the consolidated behavior package, testing failures,
// and the positive I/O/process examples. Frontend success and diagnostics use
// the shared corpora and focused CLI parity cases; runtime failure spelling is
// owned by the byte-identical runtime source and shipping conformance.
func TestSelfhostBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the selfhost compiler")
	}
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]conformance.Case, 0, len(cases))
	for _, tt := range cases {
		if selfhostBehaviorCase(tt) {
			selected = append(selected, tt)
		}
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	selfhost := sharedSelfhost(t)
	runConformanceCases(t, selected, func(t *testing.T, tt conformance.Case) (string, error) {
		result := runNativeCLIAtEnv(
			t, root, tt.Env, selfhost, caseArgs(tt)...,
		)
		result.output.stderr = selfhostNativeStderr(result.output.stderr)
		out := result.output.stdout + result.output.stderr
		if result.output.failed {
			return out, fmt.Errorf("exit status %d", result.code)
		}
		return out, nil
	})
}

// selfhostBehaviorCase keeps cases whose behavior cannot be replaced by the
// shared frontend corpora. The behavior package carries ordinary language
// assertions in one link, while test failures exercise the runner boundary.
func selfhostBehaviorCase(tt conformance.Case) bool {
	if tt.Command == "test" {
		return true
	}
	if tt.Command != "run" || tt.MustFail {
		return false
	}
	for _, feature := range tt.Features {
		if feature == "std-io" || feature == "std-process" {
			return true
		}
	}
	return false
}

// runConformanceCases checks every discovered promise with one compiler.
func runConformanceCases(t *testing.T, cases []conformance.Case, runner conformanceRunner) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.Path, func(t *testing.T) {
			runConformanceCase(t, tt, runner)
		})
	}
}

// runConformanceCase runs one case and checks what it promised.
func runConformanceCase(t *testing.T, tt conformance.Case, runner conformanceRunner) {
	t.Helper()
	if tt.Pending != "" {
		runPendingCase(t, tt, runner)
		return
	}
	if tt.MustFail {
		runFailingCase(t, tt, runner)
		return
	}
	out, err := runner(t, tt)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	if tt.Command == "check" {
		return
	}
	if out != *tt.Stdout {
		t.Fatalf("got %q, want %q", out, *tt.Stdout)
	}
}

// runFailingCase checks one case whose promise is that it fails.
func runFailingCase(t *testing.T, tt conformance.Case, runner conformanceRunner) {
	t.Helper()
	out, err := runner(t, tt)
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("got %q, want readable error prefix", out)
	}
	if !strings.Contains(out, tt.ErrorText) {
		t.Fatalf("got %q, want substring %q", out, tt.ErrorText)
	}
}

// runPendingCase asserts a declared gap is still a gap. A case that starts
// passing has to lose its `pending:` line in the change that fixes it, so the
// list cannot outlive the gaps it names.
func runPendingCase(t *testing.T, tt conformance.Case, runner conformanceRunner) {
	t.Helper()
	if casePasses(t, tt, runner) {
		t.Fatalf("passes now; remove its `pending:` line (%s)", tt.Pending)
	}
}

// casePasses reports whether a case already keeps its promise, without failing
// the test when it does not.
func casePasses(t *testing.T, tt conformance.Case, runner conformanceRunner) bool {
	t.Helper()
	out, err := runner(t, tt)
	if tt.MustFail {
		return err != nil && strings.Contains(out, "error:") && strings.Contains(out, tt.ErrorText)
	}
	if err != nil {
		return false
	}
	if tt.Command == "check" {
		return true
	}
	return out == *tt.Stdout
}

// runShippingConformanceCommand runs one case through the shipping compiler.
func runShippingConformanceCommand(_ *testing.T, tt conformance.Case) (string, error) {
	switch tt.Command {
	case "check":
		return runReferenceCheck(tt.Path)
	case "parse":
		return runReferenceParse(tt.Path)
	default:
		return runKizuEnv(tt.Env, caseArgs(tt)...)
	}
}

// caseArgs returns the CLI arguments one case is run with.
func caseArgs(tt conformance.Case) []string {
	return append([]string{tt.Command, tt.Path}, tt.Args...)
}

// runReferenceCheck keeps conformance cases tied to the full reference checker.
func runReferenceCheck(path string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	return runWithCapture(nil, func() error {
		return checkFile(path)
	})
}

// runReferenceParse keeps parse conformance tied to the Go reference parser.
func runReferenceParse(path string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	return runWithCapture(nil, func() error {
		return parseFile(path)
	})
}

// runKizuEnv runs the Kizu CLI with the host bindings the case declares.
func runKizuEnv(env []string, args ...string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	if len(args) == 0 {
		return runDispatchWithCapture(env, "", nil)
	}
	return runDispatchWithCapture(env, args[0], args[1:])
}

// runDispatchWithCapture runs dispatch while capturing process-global output.
func runDispatchWithCapture(env []string, command string, args []string) (string, error) {
	return runWithCapture(env, func() error {
		return dispatch(command, args)
	})
}

// runWithCapture runs one in-process command while capturing process-global output.
func runWithCapture(env []string, run func() error) (string, error) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldWd, wdErr := os.Getwd()
	if wdErr != nil {
		return "", wdErr
	}
	restore := applyConformanceEnv(env)

	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		return "", pipeErr
	}

	output := make(chan string, 1)
	go func() {
		var builder strings.Builder
		_, _ = io.Copy(&builder, reader)
		output <- builder.String()
	}()

	os.Stdout = writer
	os.Stderr = writer
	chdirErr := os.Chdir(repoRoot)

	var err error
	if chdirErr != nil {
		err = chdirErr
	} else {
		err = run()
		if err != nil {
			printError(err)
		}
	}

	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_ = os.Chdir(oldWd)
	restore()
	_ = writer.Close()
	return <-output, err
}

// applyConformanceEnv applies explicit case bindings and returns their restore.
func applyConformanceEnv(env []string) func() {
	type previous struct {
		name   string
		value  string
		wasSet bool
	}
	values := make([]previous, 0, len(env))
	for _, binding := range env {
		name, value, _ := strings.Cut(binding, "=")
		old, wasSet := os.LookupEnv(name)
		values = append(values, previous{name: name, value: old, wasSet: wasSet})
		_ = os.Setenv(name, value)
	}
	return func() {
		for index := len(values) - 1; index >= 0; index-- {
			value := values[index]
			restoreEnv(value.name, value.value, value.wasSet)
		}
	}
}

// restoreEnv restores an environment variable after an in-process CLI run.
func restoreEnv(name string, value string, wasSet bool) {
	if wasSet {
		_ = os.Setenv(name, value)
		return
	}
	_ = os.Unsetenv(name)
}
