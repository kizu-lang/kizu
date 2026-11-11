package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSelfhostBootstrap runs the bootstrap suite of the Kizu test harness
// (compiler/tests/harness): the compiler built once by the shipping one
// builds the same source again with an empty build cache, and the two
// executables have to be identical once Mach-O link identity is normalized.
// The first stage runs the harness's own tests, which hold that
// normalization and the mismatch report, then builds and runs the harness,
// the way the harness is run without Go:
//
//	kizu test compiler/tests/harness
//	kizu run compiler/tests/harness -- bootstrap <kizu>
//
// This test only provides the first stage and calls it, so
// `go test ./cmd/kizu` keeps the gate.
func TestSelfhostBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the selfhost compiler")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	stage1 := bootstrapStage1(t, t.TempDir())
	runHarness(t, root, stage1, "test", "compiler/tests/harness")
	runHarness(t, root, stage1, "run", "compiler/tests/harness", "--", "bootstrap", stage1)
}

// runHarness runs one harness command with kizu from the repository root and
// fails with what it reported.
func runHarness(t *testing.T, root string, kizu string, args ...string) {
	t.Helper()
	harness := exec.Command(kizu, args...)
	harness.Dir = root
	harness.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := harness.CombinedOutput()
	report := selfhostNativeStderr(string(out))
	if err != nil {
		t.Fatalf("harness %v: %v\n%s", args, err, report)
	}
	t.Log(report)
}

// bootstrapStage1 returns a compiler the shipping one built from the source in
// this checkout. The shared binary is that when this run built it, but a run
// that restored it from a cache has one built at an older revision: the
// version module generated with a compiler names the revision it was built
// from, and that is not in the cache key because it is not checked in. Asking
// such a binary to reproduce itself would be asking it to reproduce a source
// it was not built from.
func bootstrapStage1(t *testing.T, root string) string {
	t.Helper()
	shared := sharedSelfhost(t)
	if selfhostBuiltHere {
		return shared
	}
	stage1 := selfhostStagePath(t, root, "stage1")
	build := kizuCommand("build", "--target", "native", "-o", stage1, "../../compiler")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stage1 from this checkout: %v\n%s", err, out)
	}
	return stage1
}

// selfhostStagePath names one bootstrap stage's executable. Every stage uses
// the same file name in a directory of its own, because the name is what the
// executable records about where it was written.
func selfhostStagePath(t *testing.T, root string, stage string) string {
	t.Helper()
	dir := filepath.Join(root, stage)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s directory: %v", stage, err)
	}
	return filepath.Join(dir, "kizu")
}
