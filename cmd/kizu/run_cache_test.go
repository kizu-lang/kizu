package main

import (
	"os"
	"os/exec"
	"testing"
)

// TestRunLinksOnceThenOnlyRuns checks the second run of an unchanged program
// has nothing left to build. The link is what `run` spends its time on, and it
// is keyed by what the executable is made of, so a run that finds one already
// made needs no C toolchain at all: the second run is given a PATH without one.
func TestRunLinksOnceThenOnlyRuns(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required to link the first run")
	}
	cacheDir := t.TempDir()
	source := writeTempKizuSource(t, "hello.kizu", greetingSource("hello, kizu"))

	if out := runCached(t, cacheDir, source, os.Getenv("PATH")); out != "hello, kizu\n" {
		t.Fatalf("first run printed %q", out)
	}
	if out := runCached(t, cacheDir, source, t.TempDir()); out != "hello, kizu\n" {
		t.Fatalf("second run printed %q", out)
	}
}

// TestRunRebuildsWhenTheProgramChanges checks the cached executable stands for
// the program rather than for the file it was read from, so an edit runs the
// edit instead of what the same path printed last time.
func TestRunRebuildsWhenTheProgramChanges(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for native build smoke")
	}
	cacheDir := t.TempDir()
	path := os.Getenv("PATH")
	source := writeTempKizuSource(t, "greeting.kizu", greetingSource("hello, kizu"))

	if out := runCached(t, cacheDir, source, path); out != "hello, kizu\n" {
		t.Fatalf("first run printed %q", out)
	}
	if err := os.WriteFile(source, []byte(greetingSource("goodbye, kizu")), 0o644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	if out := runCached(t, cacheDir, source, path); out != "goodbye, kizu\n" {
		t.Fatalf("edited run printed %q", out)
	}
}

// greetingSource writes the smallest program that prints one line.
func greetingSource(line string) string {
	return "fn main() {\n    print(\"" + line + "\");\n}\n"
}

// runCached runs one program against a cache of its own, with the PATH the
// caller wants the toolchain looked up on.
func runCached(t *testing.T, cacheDir string, source string, path string) string {
	t.Helper()

	run := kizuCommand("run", source)
	run.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir, "PATH="+path)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	return string(out)
}
