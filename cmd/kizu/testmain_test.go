package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"testing"

	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/stdlib/stdlibtest"
)

var (
	// kizuBinaryPath is the shared CLI binary built once for all command smokes.
	kizuBinaryPath string
	testBinaryDir  string

	selfhostBuildOnce   sync.Once
	selfhostBinaryPath  string
	selfhostBuildOutput []byte
	selfhostBuildErr    error
)

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
	testBinaryDir = dir
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

// sharedSelfhost builds the selfhost compiler lazily and returns the immutable
// binary shared by every selfhost gate. Keeping this lazy means short and
// unrelated targeted tests do not pay for it.
func sharedSelfhost(t *testing.T) string {
	t.Helper()
	selfhostBuildOnce.Do(func() {
		selfhostBinaryPath = os.Getenv("KIZU_TEST_SELFHOST")
		if selfhostBinaryPath == "" {
			selfhostBinaryPath = filepath.Join(testBinaryDir, "selfhost", "kizu")
		}
		info, statErr := os.Stat(selfhostBinaryPath)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				selfhostBuildErr = fmt.Errorf("cached selfhost path is not a file: %s", selfhostBinaryPath)
			}
			return
		}
		if !os.IsNotExist(statErr) {
			selfhostBuildErr = statErr
			return
		}
		dir := filepath.Dir(selfhostBinaryPath)
		selfhostBuildErr = os.MkdirAll(dir, 0o755)
		if selfhostBuildErr != nil {
			return
		}
		build := kizuCommand(
			"build", "--target", "native", "-o", selfhostBinaryPath, "../../compiler",
		)
		selfhostBuildOutput, selfhostBuildErr = build.CombinedOutput()
	})
	if selfhostBuildErr != nil {
		t.Fatalf("build shared selfhost compiler: %v\n%s", selfhostBuildErr, selfhostBuildOutput)
	}
	return selfhostBinaryPath
}
