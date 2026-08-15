package main

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/kizu-lang/kizu/internal/conformance"
)

// repoRoot is where cases are read from and where the CLI is run.
const repoRoot = "../.."

var conformanceProcessMu sync.Mutex

// TestConformance runs every case the tree declares. Discovery walks the tree
// rather than a registry, so an example is a case by existing: there is no
// list it can be left out of.
func TestConformance(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.Path, func(t *testing.T) {
			runConformanceCase(t, tt)
		})
	}
}

// runConformanceCase runs one case and checks what it promised.
func runConformanceCase(t *testing.T, tt conformance.Case) {
	t.Helper()
	if tt.Pending != "" {
		runPendingCase(t, tt)
		return
	}
	if tt.MustFail {
		runFailingCase(t, tt)
		return
	}
	if tt.Command == "run" || tt.Command == "check" {
		// A run case goes through the reference checker first, so a program the
		// backend accepts but the checker would reject fails here rather than
		// standing as a passing example of an unchecked program.
		runReferenceCheckOK(t, tt.Path)
	}
	if tt.Command == "check" {
		return
	}
	out := runKizuOK(t, caseArgs(tt)...)
	if out != *tt.Stdout {
		t.Fatalf("got %q, want %q", out, *tt.Stdout)
	}
}

// runFailingCase checks one case whose promise is that it fails.
func runFailingCase(t *testing.T, tt conformance.Case) {
	t.Helper()
	out, err := runCaseCommand(tt)
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
func runPendingCase(t *testing.T, tt conformance.Case) {
	t.Helper()
	if casePasses(tt) {
		t.Fatalf("passes now; remove its `pending:` line (%s)", tt.Pending)
	}
}

// casePasses reports whether a case already keeps its promise, without failing
// the test when it does not.
func casePasses(tt conformance.Case) bool {
	if tt.MustFail {
		out, err := runCaseCommand(tt)
		return err != nil && strings.Contains(out, "error:") && strings.Contains(out, tt.ErrorText)
	}
	if tt.Command == "run" || tt.Command == "check" {
		if _, err := runReferenceCheck(tt.Path); err != nil {
			return false
		}
	}
	if tt.Command == "check" {
		return true
	}
	out, err := runCaseCommand(tt)
	return err == nil && out == *tt.Stdout
}

// runCaseCommand runs the CLI verb a case names.
func runCaseCommand(tt conformance.Case) (string, error) {
	switch tt.Command {
	case "check":
		return runReferenceCheck(tt.Path)
	case "parse":
		return runReferenceParse(tt.Path)
	default:
		return runKizu(caseArgs(tt)...)
	}
}

// caseArgs returns the CLI arguments one case is run with.
func caseArgs(tt conformance.Case) []string {
	return append([]string{tt.Command, tt.Path}, tt.Args...)
}

// runKizuOK runs the Kizu CLI and fails the test on errors.
func runKizuOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runKizu(args...)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	return out
}

// runReferenceCheckOK validates a source against the Go reference checker.
func runReferenceCheckOK(t *testing.T, path string) string {
	t.Helper()
	out, err := runReferenceCheck(path)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	return out
}

// runReferenceCheck keeps conformance cases tied to the full reference checker.
func runReferenceCheck(path string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	return runWithCapture(func() error {
		return checkFile(path)
	})
}

// runReferenceParse keeps parse conformance tied to the Go reference parser.
func runReferenceParse(path string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	return runWithCapture(func() error {
		return parseFile(path)
	})
}

// runKizu runs the Kizu CLI from the repository root.
func runKizu(args ...string) (string, error) {
	conformanceProcessMu.Lock()
	defer conformanceProcessMu.Unlock()

	if len(args) == 0 {
		return runDispatchWithCapture("", nil)
	}
	return runDispatchWithCapture(args[0], args[1:])
}

// runDispatchWithCapture runs dispatch while capturing process-global output.
func runDispatchWithCapture(command string, args []string) (string, error) {
	return runWithCapture(func() error {
		return dispatch(command, args)
	})
}

// runWithCapture runs one in-process command while capturing process-global output.
func runWithCapture(run func() error) (string, error) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldWd, wdErr := os.Getwd()
	if wdErr != nil {
		return "", wdErr
	}
	envValue, envWasSet := os.LookupEnv("KIZU_TEST_ENV")

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
	_ = os.Setenv("KIZU_TEST_ENV", "env-ok")
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
	restoreEnv("KIZU_TEST_ENV", envValue, envWasSet)
	_ = writer.Close()
	return <-output, err
}

// restoreEnv restores an environment variable after an in-process CLI run.
func restoreEnv(name string, value string, wasSet bool) {
	if wasSet {
		_ = os.Setenv(name, value)
		return
	}
	_ = os.Unsetenv(name)
}
