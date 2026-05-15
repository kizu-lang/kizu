package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const conformanceManifestPath = "../../tests/conformance/v0_1.json"

type conformanceManifest struct {
	Version string            `json:"version"`
	Cases   []conformanceCase `json:"cases"`
}

type conformanceCase struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	Command        string   `json:"command"`
	Path           string   `json:"path"`
	Stdout         string   `json:"stdout"`
	StderrContains string   `json:"stderr_contains"`
	Features       []string `json:"features"`
}

// TestV01ConformanceManifest runs the reusable v0.1 conformance manifest.
func TestV01ConformanceManifest(t *testing.T) {
	manifest := loadConformanceManifest(t)
	for _, tt := range manifest.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			runConformanceCase(t, tt)
		})
	}
}

// TestV01ConformanceManifestCoversExamples keeps examples in the reusable corpus.
func TestV01ConformanceManifestCoversExamples(t *testing.T) {
	manifest := loadConformanceManifest(t)
	got := manifestPaths(manifest)
	want := examplePaths(t)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("manifest paths do not match examples\nmissing:\n%s\nextra:\n%s",
			strings.Join(diffStrings(want, got), "\n"),
			strings.Join(diffStrings(got, want), "\n"))
	}
}

// TestV01ConformanceManifestShape validates fields needed by self-host runners.
func TestV01ConformanceManifestShape(t *testing.T) {
	manifest := loadConformanceManifest(t)
	seen := map[string]bool{}
	for _, tt := range manifest.Cases {
		validateConformanceCase(t, tt, seen)
		seen[tt.Name] = true
	}
}

// runConformanceCase dispatches one manifest entry.
func runConformanceCase(t *testing.T, tt conformanceCase) {
	t.Helper()
	switch tt.Mode {
	case "run":
		runKizuOK(t, "check", tt.Path)
		out := runKizuOK(t, "run", tt.Path)
		if out != tt.Stdout {
			t.Fatalf("got %q, want %q", out, tt.Stdout)
		}
	case "check":
		runKizuOK(t, "check", tt.Path)
	case "error":
		runConformanceErrorCase(t, tt)
	default:
		t.Fatalf("unknown conformance mode %q", tt.Mode)
	}
}

// runConformanceErrorCase checks one expected failure entry.
func runConformanceErrorCase(t *testing.T, tt conformanceCase) {
	t.Helper()
	command := tt.Command
	if command == "" {
		command = "check"
	}
	out, err := runKizu(command, tt.Path)
	if err == nil {
		t.Fatalf("expected command to fail\n%s", out)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("got %q, want readable error prefix", out)
	}
	if !strings.Contains(out, tt.StderrContains) {
		t.Fatalf("got %q, want substring %q", out, tt.StderrContains)
	}
}

// validateConformanceCase rejects ambiguous or incomplete manifest entries.
func validateConformanceCase(t *testing.T, tt conformanceCase, seen map[string]bool) {
	t.Helper()
	if tt.Name == "" || seen[tt.Name] {
		t.Fatalf("case names must be non-empty and unique: %q", tt.Name)
	}
	if tt.Path == "" || filepath.IsAbs(tt.Path) {
		t.Fatalf("%s: path must be relative to repo root", tt.Name)
	}
	if len(tt.Features) == 0 {
		t.Fatalf("%s: features must not be empty", tt.Name)
	}
	switch tt.Mode {
	case "run":
		if tt.Stdout == "" {
			t.Fatalf("%s: run case must declare stdout", tt.Name)
		}
	case "check":
	case "error":
		if tt.StderrContains == "" {
			t.Fatalf("%s: error case must declare stderr_contains", tt.Name)
		}
	default:
		t.Fatalf("%s: unknown mode %q", tt.Name, tt.Mode)
	}
}

// loadConformanceManifest reads the machine-usable v0.1 test manifest.
func loadConformanceManifest(t *testing.T) conformanceManifest {
	t.Helper()
	data, err := os.ReadFile(conformanceManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest conformanceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v0.1" {
		t.Fatalf("unexpected conformance version %q", manifest.Version)
	}
	return manifest
}

// manifestPaths returns sorted Kizu file paths declared by the manifest.
func manifestPaths(manifest conformanceManifest) []string {
	paths := make([]string, 0, len(manifest.Cases))
	for _, tt := range manifest.Cases {
		paths = append(paths, tt.Path)
	}
	sort.Strings(paths)
	return paths
}

// examplePaths returns all Kizu source examples that must stay in the manifest.
func examplePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../../examples/**/*.kizu")
	if err != nil {
		t.Fatal(err)
	}
	top, err := filepath.Glob("../../examples/*.kizu")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, top...)
	for idx, path := range paths {
		paths[idx] = strings.TrimPrefix(filepath.ToSlash(path), "../../")
	}
	sort.Strings(paths)
	return paths
}

// diffStrings returns entries in left that do not appear in right.
func diffStrings(left []string, right []string) []string {
	rightSet := map[string]bool{}
	for _, item := range right {
		rightSet[item] = true
	}
	missing := []string{}
	for _, item := range left {
		if !rightSet[item] {
			missing = append(missing, item)
		}
	}
	return missing
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

// runKizu runs the Kizu CLI from the repository root.
func runKizu(args ...string) (string, error) {
	cmdArgs := append([]string{"run", "./cmd/kizu"}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	return string(out), err
}
