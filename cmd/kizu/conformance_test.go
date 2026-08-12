package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

const conformanceManifestGlob = "../../tests/conformance/v0_*.json"

var conformanceProcessMu sync.Mutex

type conformanceManifest struct {
	Version string            `json:"version"`
	Cases   []conformanceCase `json:"cases"`
}

type conformanceCase struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	Command        string   `json:"command"`
	Path           string   `json:"path"`
	Args           []string `json:"args"`
	Stdout         *string  `json:"stdout"`
	StderrContains string   `json:"stderr_contains"`
	Pending        string   `json:"pending"`
	Features       []string `json:"features"`
}

// TestConformanceManifests runs reusable conformance manifests.
func TestConformanceManifests(t *testing.T) {
	for _, manifest := range loadConformanceManifests(t) {
		for _, tt := range manifest.Cases {
			name := manifest.Version + "/" + tt.Name
			t.Run(name, func(t *testing.T) {
				runConformanceCase(t, tt)
			})
		}
	}
}

// TestConformanceManifestsCoverExamples keeps examples in the reusable corpus.
func TestConformanceManifestsCoverExamples(t *testing.T) {
	got := manifestPaths(loadConformanceManifests(t))
	want := examplePaths(t)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("manifest paths do not match examples\nmissing:\n%s\nextra:\n%s",
			strings.Join(diffStrings(want, got), "\n"),
			strings.Join(diffStrings(got, want), "\n"))
	}
}

// TestConformanceManifestShape validates reusable conformance manifest fields.
func TestConformanceManifestShape(t *testing.T) {
	seen := map[string]bool{}
	for _, manifest := range loadConformanceManifests(t) {
		for _, tt := range manifest.Cases {
			validateConformanceCase(t, tt, seen)
			seen[tt.Name] = true
		}
	}
}

// runConformanceCase dispatches one manifest entry.
func runConformanceCase(t *testing.T, tt conformanceCase) {
	t.Helper()
	if tt.Pending != "" {
		runPendingCase(t, tt)
		return
	}
	switch tt.Mode {
	case "run":
		runReferenceCheckOK(t, tt.Path)
		out := runKizuOK(t, runArgs(tt)...)
		want := conformanceExpectedStdout(t, tt)
		if out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	case "check":
		runReferenceCheckOK(t, tt.Path)
	case "test":
		out := runKizuOK(t, "test", tt.Path)
		want := conformanceExpectedStdout(t, tt)
		if out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	case "error":
		runConformanceErrorCase(t, tt)
	default:
		t.Fatalf("unknown conformance mode %q", tt.Mode)
	}
}

// conformanceExpectedStdout returns declared stdout, allowing explicit empty output.
func conformanceExpectedStdout(t *testing.T, tt conformanceCase) string {
	t.Helper()
	if tt.Stdout == nil {
		t.Fatalf("%s: stdout must be declared", tt.Name)
	}
	return *tt.Stdout
}

// runPendingCase asserts a declared gap is still a gap. A case that starts
// passing has to lose its `pending` entry in the change that fixes it, so the
// list cannot outlive the gaps it names.
func runPendingCase(t *testing.T, tt conformanceCase) {
	t.Helper()
	if conformanceCasePasses(tt) {
		t.Fatalf("%s passes now; remove its `pending` entry (%s)", tt.Name, tt.Pending)
	}
}

// conformanceCasePasses reports whether a case already meets its manifest
// expectation, without failing the test when it does not.
func conformanceCasePasses(tt conformanceCase) bool {
	switch tt.Mode {
	case "run", "test":
		if tt.Stdout == nil {
			return false
		}
		if _, err := runReferenceCheck(tt.Path); err != nil {
			return false
		}
		args := runArgs(tt)
		if tt.Mode == "test" {
			args = []string{"test", tt.Path}
		}
		out, err := runKizu(args...)
		return err == nil && out == *tt.Stdout
	case "error":
		out, err := runErrorCaseCommand(tt)
		return err != nil && strings.Contains(out, "error:") &&
			strings.Contains(out, tt.StderrContains)
	default:
		return false
	}
}

// runErrorCaseCommand runs the command an expected-failure entry names.
func runErrorCaseCommand(tt conformanceCase) (string, error) {
	command := tt.Command
	if command == "" {
		command = "check"
	}
	switch command {
	case "check":
		return runReferenceCheck(tt.Path)
	case "parse":
		return runReferenceParse(tt.Path)
	default:
		args := []string{command, tt.Path}
		if command == "run" || command == "test" {
			args = append(args, tt.Args...)
		}
		return runKizu(args...)
	}
}

// runConformanceErrorCase checks one expected failure entry.
func runConformanceErrorCase(t *testing.T, tt conformanceCase) {
	t.Helper()
	out, err := runErrorCaseCommand(tt)
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

// runArgs returns CLI args for a positive run case.
func runArgs(tt conformanceCase) []string {
	args := []string{"run", tt.Path}
	return append(args, tt.Args...)
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
	if tt.Pending != "" && tt.Mode == "check" {
		t.Fatalf("%s: a check case cannot be pending", tt.Name)
	}
	switch tt.Mode {
	case "run":
		if tt.Stdout == nil {
			t.Fatalf("%s: run case must declare stdout", tt.Name)
		}
	case "check":
	case "test":
		if tt.Stdout == nil {
			t.Fatalf("%s: test case must declare stdout", tt.Name)
		}
	case "error":
		if tt.StderrContains == "" {
			t.Fatalf("%s: error case must declare stderr_contains", tt.Name)
		}
	default:
		t.Fatalf("%s: unknown mode %q", tt.Name, tt.Mode)
	}
}

// loadConformanceManifests reads machine-usable versioned test manifests.
func loadConformanceManifests(t *testing.T) []conformanceManifest {
	t.Helper()
	paths, err := filepath.Glob(conformanceManifestGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no conformance manifests found")
	}
	manifests := make([]conformanceManifest, 0, len(paths))
	for _, path := range paths {
		manifests = append(manifests, loadConformanceManifest(t, path))
	}
	return manifests
}

// loadConformanceManifest reads one conformance manifest from disk.
func loadConformanceManifest(t *testing.T, path string) conformanceManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest conformanceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(filepath.Base(path), ".json")
	want = strings.ReplaceAll(want, "_", ".")
	if manifest.Version != want {
		t.Fatalf("%s: unexpected conformance version %q", path, manifest.Version)
	}
	return manifest
}

// manifestPaths returns sorted Kizu file paths declared by the manifests.
func manifestPaths(manifests []conformanceManifest) []string {
	paths := []string{}
	for _, manifest := range manifests {
		for _, tt := range manifest.Cases {
			if isPackageExamplePath(tt.Path) {
				continue
			}
			paths = append(paths, tt.Path)
		}
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
	filtered := paths[:0]
	for _, path := range paths {
		rel := strings.TrimPrefix(filepath.ToSlash(path), "../../")
		if isPackageExamplePath(rel) {
			continue
		}
		filtered = append(filtered, rel)
	}
	sort.Strings(filtered)
	return filtered
}

// isPackageExamplePath reports package roots that are manifest cases, not files.
func isPackageExamplePath(path string) bool {
	return strings.HasPrefix(path, "examples/modules/")
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

// runReferenceCheckOK validates a source against the Go reference checker.
func runReferenceCheckOK(t *testing.T, path string) string {
	t.Helper()
	out, err := runReferenceCheck(path)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	return out
}

// runReferenceCheck keeps conformance manifests tied to the full reference checker.
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
	chdirErr := os.Chdir("../..")

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
