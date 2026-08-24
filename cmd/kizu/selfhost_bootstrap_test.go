package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSelfhostBootstrap builds the selfhost compiler with the shipping one,
// then builds the same source a second time with what the first build
// produced, and requires the two executables to be identical.
//
// It is the one gate that asks the selfhost compiler to build its own source
// the whole way down. TestSelfhostFrontend reaches compiler/ but stops at
// the emitted LLVM text, and TestSelfhostNative links and runs executables
// but for the examples, tests/behavior and the module examples -- never for
// compiler/ itself, which is the largest program in the tree.
//
// Reproducing itself byte for byte is what says the self-built compiler
// compiles that source the way the shipping compiler does, so the
// comparisons both suites make against the shipping-built compiler hold for
// the self-built one without either suite running twice.
//
// Both stages are written under the same file name in separate directories:
// the linker records the output file's name inside the executable, so two
// builds that differ only in where they were asked to write would differ by
// those bytes and by nothing else.
func TestSelfhostBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the selfhost compiler twice")
	}
	root := t.TempDir()
	stage1 := selfhostStagePath(t, root, "stage1")
	build := kizuCommand("build", "--target", "native", "-o", stage1, "../../compiler")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("shipping compiler building the selfhost compiler: %v\n%s", err, out)
	}
	stage2 := selfhostStagePath(t, root, "stage2")
	rebuild := exec.Command(stage1, "build", "--target", "native", "-o", stage2, "../../compiler")
	if out, err := rebuild.CombinedOutput(); err != nil {
		t.Fatalf("selfhost compiler building itself: %v\n%s", err, out)
	}
	first, err := os.ReadFile(stage1)
	if err != nil {
		t.Fatalf("read stage1: %v", err)
	}
	second, err := os.ReadFile(stage2)
	if err != nil {
		t.Fatalf("read stage2: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("selfhost compiler does not reproduce itself:"+
			" stage1 is %d bytes, stage2 is %d", len(first), len(second))
	}
	differing, firstAt := countDifferingBytes(first, second)
	if differing != 0 {
		t.Fatalf("selfhost compiler does not reproduce itself:"+
			" %d of %d bytes differ, first at offset %d",
			differing, len(first), firstAt)
	}
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

// countDifferingBytes reports how many bytes two equal-length builds disagree
// on and the first offset they disagree at, so a broken fixed point says how
// far from one it is rather than only that it is not one.
func countDifferingBytes(first []byte, second []byte) (int, int) {
	differing := 0
	firstAt := -1
	for index := range first {
		if first[index] == second[index] {
			continue
		}
		if firstAt < 0 {
			firstAt = index
		}
		differing++
	}
	return differing, firstAt
}
