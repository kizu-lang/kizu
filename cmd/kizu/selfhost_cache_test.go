package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestSelfhostCache builds the selfhost compiler with the shipping one and
// checks the two CLIs share one build cache: an artifact stored by either
// is reused by the other in both directions, and `cache status` and
// `cache prune` print the same lines over the same cache state. Every case
// pins KIZU_CACHE_DIR to a directory of its own, so nothing here reads or
// pollutes the user's cache.
func TestSelfhostCache(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the selfhost compiler and clang")
	}
	selfhost := filepath.Join(t.TempDir(), "selfhost-kizu")
	build := kizuCommand("build", "--target", "native", "-o", selfhost, "../../compiler")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build selfhost compiler: %v\n%s", err, out)
	}
	target := "../../examples/hello.kizu"

	t.Run("go-builds-selfhost-reuses", func(t *testing.T) {
		t.Parallel()
		requireSharedRun(t, target, kizuBinaryPath, selfhost)
	})
	t.Run("selfhost-builds-go-reuses", func(t *testing.T) {
		t.Parallel()
		requireSharedRun(t, target, selfhost, kizuBinaryPath)
	})
	t.Run("status-and-prune-match", func(t *testing.T) {
		t.Parallel()
		cacheDir := t.TempDir()
		requireCacheRunOK(t, runCacheCLI(t, cacheDir, "", kizuBinaryPath, "run", target), "go run")
		goStatus := runCacheCLI(t, cacheDir, "", kizuBinaryPath, "cache", "status")
		selfStatus := runCacheCLI(t, cacheDir, "", selfhost, "cache", "status")
		if selfStatus.output != goStatus.output || selfStatus.code != goStatus.code {
			t.Errorf("cache status differs\n--- go\n%s--- selfhost\n%s",
				goStatus.output.stdout, selfStatus.output.stdout)
		}
		selfPrune := runCacheCLI(t, cacheDir, "", selfhost, "cache", "prune")
		requireCacheRunOK(t, selfPrune, "selfhost prune")
		if names := cacheFileNames(t, cacheDir); len(names) != 0 {
			t.Errorf("selfhost prune left %v", names)
		}
		requireCacheRunOK(t, runCacheCLI(t, cacheDir, "", kizuBinaryPath, "run", target), "go refill")
		goPrune := runCacheCLI(t, cacheDir, "", kizuBinaryPath, "cache", "prune")
		requireCacheRunOK(t, goPrune, "go prune")
		if normalizeByteCounts(goPrune.output.stdout) != normalizeByteCounts(selfPrune.output.stdout) {
			t.Errorf("cache prune differs\n--- go\n%s--- selfhost\n%s",
				goPrune.output.stdout, selfPrune.output.stdout)
		}
	})
}

// requireSharedRun runs target with the first CLI into a fresh cache, then
// with the second CLI against the same cache and a PATH without any
// toolchain: only a run that reuses the stored runtime and executable can
// succeed there. The entry set must not change and both runs must print
// the same program output.
func requireSharedRun(t *testing.T, target string, first string, second string) {
	t.Helper()
	cacheDir := t.TempDir()
	firstRun := runCacheCLI(t, cacheDir, "", first, "run", target)
	requireCacheRunOK(t, firstRun, "first run")
	names := cacheFileNames(t, cacheDir)
	if len(names) == 0 {
		t.Fatalf("first run stored nothing")
	}
	secondRun := runCacheCLI(t, cacheDir, t.TempDir(), second, "run", target)
	requireCacheRunOK(t, secondRun, "second run")
	if secondRun.output.stdout != firstRun.output.stdout {
		t.Errorf("run output differs\n--- first\n%s--- second\n%s",
			firstRun.output.stdout, secondRun.output.stdout)
	}
	if after := cacheFileNames(t, cacheDir); strings.Join(after, "\n") != strings.Join(names, "\n") {
		t.Errorf("second run changed the entries: %v -> %v", names, after)
	}
}

// runCacheCLI runs one CLI with the cache directory pinned, a TMPDIR of
// its own, and $PWD naming the working directory the way a shell would --
// the selfhost CLI reads it where the Go CLI reads os.Getwd. A non-empty
// path replaces PATH, which is how a case proves a reuse: a rebuild that
// needs the toolchain cannot find one there.
func runCacheCLI(
	t *testing.T,
	cacheDir string,
	path string,
	name string,
	args ...string,
) nativeCLIResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	env := append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir, "TMPDIR="+t.TempDir())
	if wd, err := os.Getwd(); err == nil {
		env = append(env, "PWD="+wd)
	}
	if path != "" {
		env = append(env, "PATH="+path)
	}
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %s: %v", name, runErr)
		}
	}
	return nativeCLIResult{
		output: cliOutput{
			stdout: stdout.String(),
			stderr: selfhostNativeStderr(stderr.String()),
			failed: runErr != nil,
		},
		code: code,
	}
}

// requireCacheRunOK stops a case whose command failed, with what it printed.
func requireCacheRunOK(t *testing.T, result nativeCLIResult, what string) {
	t.Helper()
	if result.output.failed || result.code != 0 {
		t.Fatalf("%s failed (code=%d)\nstdout:\n%sstderr:\n%s",
			what, result.code, result.output.stdout, result.output.stderr)
	}
}

// cacheFileNames lists what one cache directory holds, sorted.
func cacheFileNames(t *testing.T, dir string) []string {
	t.Helper()
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	sort.Strings(names)
	return names
}

// byteCountPattern matches the byte totals in cache command output, which
// depend on what exactly the host toolchain linked this run.
var byteCountPattern = regexp.MustCompile(`\d+ bytes`)

// normalizeByteCounts replaces byte totals so two fills compare by shape.
func normalizeByteCounts(text string) string {
	return byteCountPattern.ReplaceAllString(text, "N bytes")
}
