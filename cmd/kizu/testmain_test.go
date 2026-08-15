package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/stdlib/stdlibtest"
	"testing"
)

// kizuBinaryPath is the shared CLI binary built once for all command smokes.
var kizuBinaryPath string

// TestMain builds the kizu CLI once so command smokes exec it directly
// instead of paying a `go run .` link per test.
func TestMain(m *testing.M) {
	code, err := runTestMain(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

// runTestMain builds the shared binary, runs the tests, and cleans up.
func runTestMain(m *testing.M) (int, error) {
	// The test binary is built into a temp directory, so nothing sits next to
	// it. Point it at the repository's library tree the way a user points at an
	// installed one. Setting it here rather than on each command keeps it in
	// os.Environ(), which the tests that build their own environment copy.
	if err := os.Setenv(stdlib.LibDirEnv, stdlibtest.RepoLibDir()); err != nil {
		return 0, err
	}
	dir, err := os.MkdirTemp("", "kizu-test-bin-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "kizu")
	out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("build shared kizu binary: %w\n%s", err, out)
	}
	kizuBinaryPath = binary
	return m.Run(), nil
}

// kizuCommand returns a command that runs the shared test-built kizu CLI.
func kizuCommand(args ...string) *exec.Cmd {
	return exec.Command(kizuBinaryPath, args...)
}
