package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// flipParityCase is one #1070 flip-path parity manifest row.
type flipParityCase struct {
	name    string
	fixture string
}

// flipParityResult captures both run paths for one fixture.
type flipParityResult struct {
	baseline flipCommandResult
	flip     flipCommandResult
	metaPath string
}

// flipCommandResult is a single `kizu run` invocation outcome.
type flipCommandResult struct {
	stdout string
	stderr string
	code   int
}

// flipArtifactDir is the run-codegen cache the flip path writes into.
const flipArtifactDir = "target/selfhost/cache/run"

// TestSelfhostRunFlipParityGate compares the #1070 flip path against the Go
// interpreter baseline. The flip path (KIZU_SELFHOST_RUN=1) routes `run` through
// selfhost::cli::execute::run_file_cli: full std::kizu::parser AST -> run-codegen
// tape -> native artifact. The baseline is the default Go interpreter runFile.
// Each fixture must produce byte-identical stdout and the same exit code on both
// paths, and the flip path must leave its run-codegen artifact metadata behind so
// the gate cannot silently degrade into "baseline vs baseline".
func TestSelfhostRunFlipParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_FLIP_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_FLIP_PARITY=1 to run selfhost flip parity")
	}
	report, failures := runSelfhostRunFlipParity(t)
	if failures > 0 {
		t.Fatalf("selfhost run flip parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost run flip parity report:\n%s", report)
}

// TestSelfhostRunFlipParityRecipes keeps the flip gate self-contained: no
// bootstrap dependency (selfhost source is unchanged) and a distinct env/name
// from the existing #569 run-parity gate.
func TestSelfhostRunFlipParityRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-run-flip-parity-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_FLIP_PARITY=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostRunFlipParityGate$")
	requireNoRecipeFragment(t, gate, "just selfhost-native")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_NATIVE=1")

	one := justRecipe(content, "selfhost-run-flip-one case")
	requireRecipeFragment(t, one, "KIZU_RUN_SELFHOST_FLIP_PARITY=1")
	requireRecipeFragment(t, one, "KIZU_RUN_SELFHOST_FLIP_PARITY_CASE='{{case}}'")
	requireRecipeFragment(t, one, "TestSelfhostRunFlipParityGate$")
	requireNoRecipeFragment(t, one, "just selfhost-native")
}

// TestSelectFlipParityCases covers name and fixture selectors for the inner loop.
func TestSelectFlipParityCases(t *testing.T) {
	cases := []flipParityCase{
		{name: "flip_borrow_int", fixture: "selfhost/tests/cli/flip/flip_borrow_int.kizu"},
		{name: "flip_int_print", fixture: "selfhost/tests/cli/flip/flip_int_print.kizu"},
	}
	for _, selector := range []string{
		"flip_borrow_int",
		"selfhost/tests/cli/flip/flip_borrow_int.kizu",
	} {
		got, err := selectFlipParityCases(cases, selector)
		if err != nil {
			t.Fatalf("select %q: %v", selector, err)
		}
		if len(got) != 1 || got[0].name != "flip_borrow_int" {
			t.Fatalf("select %q = %#v, want flip_borrow_int", selector, got)
		}
	}
	if _, err := selectFlipParityCases(cases, "missing"); err == nil {
		t.Fatalf("select missing succeeded")
	}
}

// runSelfhostRunFlipParity executes the flip manifest with a freshly built hosted
// `cmd/kizu` binary and compares each fixture's flip path to its baseline path.
func runSelfhostRunFlipParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Errorf("selfhost flip parity requires clang: %v", err)
		return "", 1
	}
	// The flip path links generated executables against the checked-in runtime
	// templates staged into their ignored target paths.
	ensureSelfhostStage2RuntimeArtifacts(t)
	if err := os.MkdirAll("target/selfhost/reports", 0o755); err != nil {
		t.Errorf("prepare flip parity reports dir: %v", err)
		return "", 1
	}
	cases, err := loadFlipParityCases("selfhost/tests/cli/run-flip-parity.tsv")
	if err != nil {
		t.Errorf("load flip parity manifest: %v", err)
		return "", 1
	}
	selector := strings.TrimSpace(os.Getenv("KIZU_RUN_SELFHOST_FLIP_PARITY_CASE"))
	selected := selector != ""
	if selected {
		cases, err = selectFlipParityCases(cases, selector)
		if err != nil {
			t.Errorf("select flip parity case: %v", err)
			return "", 1
		}
	}
	bin, err := buildFlipParityKizu(t)
	if err != nil {
		t.Errorf("build cmd/kizu for flip parity: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendFlipParityHeader(&report, len(cases))
	if selected {
		fmt.Fprintf(&report, "selection.mode single-case\n")
		fmt.Fprintf(&report, "selection.case %s\n", selector)
	}
	failures := 0
	for _, item := range cases {
		result, caseFailures := runFlipParityCase(t, bin, item)
		failures += caseFailures
		appendFlipParityResult(&report, item, result)
	}
	appendFlipParityFooter(&report, start, failures)
	if err := writeSelfhostGateReport(
		"target/selfhost/reports/run-flip-parity.txt",
		report.String(),
	); err != nil {
		t.Errorf("write flip parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// buildFlipParityKizu compiles the hosted CLI once for the gate.
func buildFlipParityKizu(t *testing.T) (string, error) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kizu-flip-parity")
	build := exec.Command("go", "build", "-o", bin, "./cmd/kizu")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, string(out))
	}
	return bin, nil
}

// runFlipParityCase runs one fixture on both paths and proves the flip path ran.
func runFlipParityCase(
	t *testing.T,
	bin string,
	item flipParityCase,
) (flipParityResult, int) {
	t.Helper()
	metaPath := filepath.Join(flipArtifactDir, flipArtifactStem(item.fixture)+".ll.meta")
	result := flipParityResult{metaPath: metaPath}
	// Remove any stale run-codegen artifact so its later presence proves THIS
	// flip invocation produced it.
	if err := removeFlipArtifacts(item.fixture); err != nil {
		t.Errorf("flip parity %s clear artifacts: %v", item.name, err)
		return result, 1
	}
	failures := 0
	result.baseline = runFlipCommand(t, bin, item.fixture, false)
	// The Go interpreter baseline must not touch the run-codegen cache; if the
	// artifact appears here the gate is silently running the flip path twice.
	if _, err := os.Stat(metaPath); err == nil {
		t.Errorf("flip parity %s baseline unexpectedly emitted %s", item.name, metaPath)
		failures++
	}
	result.flip = runFlipCommand(t, bin, item.fixture, true)
	failures += countFlipPathProofFailures(t, item, metaPath)
	if result.flip.stdout != result.baseline.stdout {
		t.Errorf(
			"flip parity %s stdout mismatch\nbaseline=%q\nflip=%q",
			item.name, result.baseline.stdout, result.flip.stdout,
		)
		failures++
	}
	if result.flip.code != result.baseline.code {
		t.Errorf(
			"flip parity %s exit mismatch baseline=%d flip=%d",
			item.name, result.baseline.code, result.flip.code,
		)
		failures++
	}
	return result, failures
}

// countFlipPathProofFailures asserts the flip path left run-codegen metadata that
// records native codegen with no Go fallback.
func countFlipPathProofFailures(t *testing.T, item flipParityCase, metaPath string) int {
	t.Helper()
	bytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Errorf("flip parity %s missing run-codegen metadata %s: %v", item.name, metaPath, err)
		return 1
	}
	metadata := string(bytes)
	for _, fragment := range []string{
		"run.codegen-ir enabled\n",
		"go.cmd-kizu-fallback none\n",
	} {
		if !strings.Contains(metadata, fragment) {
			t.Errorf("flip parity %s metadata missing %q", item.name, fragment)
			return 1
		}
	}
	return 0
}

// runFlipCommand invokes `kizu run <fixture>` with or without the flip switch.
func runFlipCommand(t *testing.T, bin string, fixture string, flip bool) flipCommandResult {
	t.Helper()
	run := exec.Command(bin, "run", fixture)
	env := flipParityBaseEnv()
	if flip {
		env = append(env, selfhostRunEnvVar+"=1")
	}
	run.Env = env
	var stdout, stderr strings.Builder
	run.Stdout = &stdout
	run.Stderr = &stderr
	err := run.Run()
	return flipCommandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		code:   exitCode(err),
	}
}

// flipParityBaseEnv returns the process environment with the flip switch cleared
// so the baseline path is never accidentally routed through selfhost.
func flipParityBaseEnv() []string {
	prefix := selfhostRunEnvVar + "="
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// removeFlipArtifacts deletes the run-codegen artifacts for one fixture stem.
func removeFlipArtifacts(fixture string) error {
	stem := flipArtifactStem(fixture)
	for _, suffix := range []string{".ll", ".ll.meta", ""} {
		path := filepath.Join(flipArtifactDir, stem+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// flipArtifactStem mirrors the selfhost run path's artifact naming (source base
// name without extension).
func flipArtifactStem(fixture string) string {
	base := filepath.Base(fixture)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// loadFlipParityCases parses the checked-in flip manifest.
func loadFlipParityCases(path string) ([]flipParityCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []flipParityCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseFlipParityLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: no flip parity cases", path)
	}
	return cases, nil
}

// parseFlipParityLine parses one manifest row (name, fixture).
func parseFlipParityLine(line string) (flipParityCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return flipParityCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 2 {
		return flipParityCase{}, false, fmt.Errorf("expected 2 fields, got %d", len(fields))
	}
	return flipParityCase{name: fields[0], fixture: fields[1]}, true, nil
}

// selectFlipParityCases filters the manifest by case name or fixture path.
func selectFlipParityCases(cases []flipParityCase, selector string) ([]flipParityCase, error) {
	var selected []flipParityCase
	for _, item := range cases {
		if item.name == selector || item.fixture == selector {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no flip parity case matches %q", selector)
	}
	return selected, nil
}

// appendFlipParityHeader writes durable #1070 flip gate metadata.
func appendFlipParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-run-flip-parity-v0\n")
	fmt.Fprintf(out, "issue #1070\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/run-flip-parity.tsv\n")
	fmt.Fprintf(out, "flip.switch %s=1\n", selfhostRunEnvVar)
	fmt.Fprintf(out, "flip.path run_file_cli std::kizu::parser run-codegen native\n")
	fmt.Fprintf(out, "baseline.path go-interpreter-runFile\n")
	fmt.Fprintf(out, "artifact.dir %s\n", flipArtifactDir)
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendFlipParityResult writes one fixture's comparison record.
func appendFlipParityResult(out *strings.Builder, item flipParityCase, result flipParityResult) {
	fmt.Fprintf(out, "case.%s.fixture %s\n", item.name, item.fixture)
	fmt.Fprintf(out, "case.%s.baseline.exit %d\n", item.name, result.baseline.code)
	fmt.Fprintf(out, "case.%s.flip.exit %d\n", item.name, result.flip.code)
	fmt.Fprintf(
		out,
		"case.%s.baseline.stdout.sha256 %s\n",
		item.name,
		textFingerprint(result.baseline.stdout),
	)
	fmt.Fprintf(
		out,
		"case.%s.flip.stdout.sha256 %s\n",
		item.name,
		textFingerprint(result.flip.stdout),
	)
	fmt.Fprintf(
		out,
		"case.%s.baseline.stderr.sha256 %s\n",
		item.name,
		textFingerprint(result.baseline.stderr),
	)
	fmt.Fprintf(
		out,
		"case.%s.flip.stderr.sha256 %s\n",
		item.name,
		textFingerprint(result.flip.stderr),
	)
	if _, err := os.Stat(result.metaPath); err == nil {
		fmt.Fprintf(out, "case.%s.flip.codegen_metadata %s\n", item.name, result.metaPath)
	} else {
		fmt.Fprintf(out, "case.%s.flip.codegen_metadata missing\n", item.name)
	}
}

// appendFlipParityFooter writes elapsed time and pass/fail status.
func appendFlipParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}
