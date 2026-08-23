package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/types"
)

// TestSelfhostFrontend builds the selfhost compiler with the shipping one and
// runs its `parse` and `check` commands over the examples, the negative
// examples, tests/behavior, the module examples and compiler/ itself. What it
// prints must be what the Go front end (parse, load, type check, ownership
// check) prints for the same target through the same CLI paths. The selfhost
// `check` stops at the ownership checker, so the Go side here stops there
// too; the later gates join the comparison with their modules.
func TestSelfhostFrontend(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the selfhost compiler")
	}
	selfhost := filepath.Join(t.TempDir(), "selfhost-kizu")
	build := kizuCommand("build", "--target", "native", "-o", selfhost, "../../compiler")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build selfhost compiler: %v\n%s", err, out)
	}
	files := globFiles(t, "../../examples/*.kizu", "../../examples/negative/*.kizu")
	packages := []string{"../../tests/behavior", "../../compiler"}
	packages = append(packages, globDirs(t, "../../examples/modules/*")...)
	for _, file := range files {
		file := file
		name := strings.TrimPrefix(file, "../../")
		t.Run("parse/"+name, func(t *testing.T) {
			t.Parallel()
			compareSelfhost(t, selfhost, "parse", file, goParseOutput(file))
		})
		t.Run("check/"+name, func(t *testing.T) {
			t.Parallel()
			compareSelfhost(t, selfhost, "check", file, goCheckOutput(file))
		})
	}
	for _, pkg := range packages {
		pkg := pkg
		t.Run("check/"+strings.TrimPrefix(pkg, "../../"), func(t *testing.T) {
			t.Parallel()
			compareSelfhost(t, selfhost, "check", pkg, goCheckOutput(pkg))
		})
	}
}

// cliOutput is what a CLI command wrote and how it ended.
type cliOutput struct {
	stdout string
	stderr string
	failed bool
}

// compareSelfhost runs one selfhost command and compares it with the Go output.
func compareSelfhost(t *testing.T, selfhost string, command string, target string, want cliOutput) {
	t.Helper()
	cmd := exec.Command(selfhost, command, target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	got := cliOutput{
		stdout: stdout.String(),
		stderr: selfhostStderr(stderr.String()),
		failed: runErr != nil,
	}
	if got != want {
		t.Errorf("selfhost %s %s differs\n--- want (failed=%v)\nstdout:\n%sstderr:\n%s"+
			"--- got (failed=%v)\nstdout:\n%sstderr:\n%s",
			command, target, want.failed, want.stdout, want.stderr,
			got.failed, got.stdout, got.stderr)
	}
}

// selfhostStderr drops the line the runtime adds when main returns an error
// (`runtime error: compiler::Cli::Failed`); the exit status carries that fact.
func selfhostStderr(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "runtime error: compiler::Cli::") {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 || (len(kept) == 1 && kept[0] == "") {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}

// goParseOutput renders what `kizu parse` prints for a file.
func goParseOutput(file string) cliOutput {
	program, diags, err := parsePath(file)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	if len(diags) > 0 {
		var stderr strings.Builder
		for _, d := range diags {
			stderr.WriteString(d.CLIError())
			stderr.WriteByte('\n')
		}
		stderr.WriteString("error: parse failed\n")
		return cliOutput{stderr: stderr.String(), failed: true}
	}
	return cliOutput{stdout: program.String() + "\n"}
}

// goCheckOutput renders what `kizu check` prints for a target, stopping at the
// ownership checker like the selfhost command does.
func goCheckOutput(target string) cliOutput {
	program, err := loadFrontendTarget(target)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	if err := types.New().Check(program); err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	if err := ownership.New().Check(program); err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: "check: ok\n"}
}

// loadFrontendTarget loads a file or a package the way the check command does.
func loadFrontendTarget(target string) (*ast.Program, error) {
	if isPackageRoot(target) {
		_, program, err := loadPackageProgram(target)
		return program, err
	}
	return loadFileProgram(target)
}

// cliErrorLine renders an error the way printError writes it.
func cliErrorLine(err error) string {
	var structured *diag.Diagnostic
	if errors.As(err, &structured) {
		return structured.CLIError() + "\n"
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "error:") {
		return msg + "\n"
	}
	return "error: " + msg + "\n"
}

// globFiles lists the files matching patterns, sorted.
func globFiles(t *testing.T, patterns ...string) []string {
	t.Helper()
	var out []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, matches...)
	}
	sort.Strings(out)
	return out
}

// globDirs lists the package directories (ones with kizu.toml) under pattern.
func globDirs(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, match := range matches {
		if _, err := os.Stat(filepath.Join(match, "kizu.toml")); err == nil {
			out = append(out, match)
		}
	}
	sort.Strings(out)
	return out
}
