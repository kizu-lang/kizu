package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type checkParityCase struct {
	name         string
	command      string
	fixture      string
	exitCode     int
	stdoutGolden string
	stderrGolden string
}

type checkParityGuardCase struct {
	name     string
	args     []string
	exitCode int
	stdout   string
	stderr   string
}

// TestSelfhostCheckParityGate runs #530 check cases through the stage2 artifact.
func TestSelfhostCheckParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_CHECK_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_CHECK_PARITY=1 to run selfhost check parity")
	}
	report, failures := runSelfhostCheckParity(t)
	if failures > 0 {
		t.Fatalf("selfhost check parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost check parity report:\n%s", report)
}

// TestSelfhostCheckParityRecipes keeps the fast gate separate from bootstrap.
func TestSelfhostCheckParityRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-check-parity-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_CHECK_PARITY=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostCheckParityGate$")
	requireNoRecipeFragment(t, gate, "just selfhost-native")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_NATIVE=1")

	fromScratch := justRecipe(content, "selfhost-production-from-scratch")
	requireRecipeFragment(t, fromScratch, "just selfhost-fast-gate")

	fastGate := justRecipe(content, "selfhost-fast-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-check-parity-gate")
	requireNoRecipeFragment(t, fastGate, "just selfhost-native")
	requireNoRecipeFragment(t, fastGate, "KIZU_RUN_SELFHOST_NATIVE=1")
}

// runSelfhostCheckParity executes the #530 manifest with the hosted artifact.
func runSelfhostCheckParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage0-native/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost check parity runner: %v", err)
		return "", 1
	}
	cases, err := loadCheckParityCases("selfhost/tests/cli/check-parity.tsv")
	if err != nil {
		t.Errorf("load check parity manifest: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendCheckParityHeader(&report, len(cases))
	failures := countCheckParityCaseFailures(t, &report, runner, cases)
	failures += countCheckParityGuardFailures(t, &report, runner)
	appendCheckParityFooter(&report, start, failures)
	if err := os.WriteFile(
		"target/selfhost/reports/check-parity.txt",
		[]byte(report.String()),
		0o644,
	); err != nil {
		t.Errorf("write check parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// loadCheckParityCases parses the checked-in #530 manifest.
func loadCheckParityCases(path string) ([]checkParityCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []checkParityCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseCheckParityLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	return cases, nil
}

// parseCheckParityLine parses one manifest row.
func parseCheckParityLine(line string) (checkParityCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return checkParityCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 6 {
		return checkParityCase{}, false, fmt.Errorf("expected 6 fields")
	}
	code, err := strconv.Atoi(fields[3])
	if err != nil {
		return checkParityCase{}, false, err
	}
	return checkParityCase{
		name:         fields[0],
		command:      fields[1],
		fixture:      fields[2],
		exitCode:     code,
		stdoutGolden: fields[4],
		stderrGolden: fields[5],
	}, true, nil
}

// countCheckParityCaseFailures compares manifest cases to checked-in goldens.
func countCheckParityCaseFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
	cases []checkParityCase,
) int {
	t.Helper()
	failures := 0
	for _, item := range cases {
		expectedOut, expectedErr, err := readCheckParityGoldens(item)
		if err != nil {
			t.Errorf("read check parity goldens for %s: %v", item.name, err)
			failures++
			continue
		}
		result := runSelfhostCommand(t, runner, item.command, item.fixture)
		if result.code != item.exitCode ||
			result.stdout != expectedOut ||
			result.stderr != expectedErr {
			t.Errorf("check parity %s mismatch", item.name)
			failures++
		}
		appendCheckParityResult(report, item, result)
	}
	return failures
}

// readCheckParityGoldens loads the expected stdout and stderr bytes.
func readCheckParityGoldens(item checkParityCase) (string, string, error) {
	stdout, err := os.ReadFile(item.stdoutGolden)
	if err != nil {
		return "", "", err
	}
	stderr, err := os.ReadFile(item.stderrGolden)
	if err != nil {
		return "", "", err
	}
	return string(stdout), string(stderr), nil
}

// countCheckParityGuardFailures checks usage and unsupported-command stability.
func countCheckParityGuardFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
) int {
	t.Helper()
	failures := 0
	for _, item := range checkParityGuardCases() {
		result := runSelfhostCommand(t, runner, item.args...)
		if result.code != item.exitCode ||
			result.stdout != item.stdout ||
			result.stderr != item.stderr {
			t.Errorf("check parity guard %s mismatch", item.name)
			failures++
		}
		appendCheckParityGuardResult(report, item, result)
	}
	return failures
}

// checkParityGuardCases returns the #530/#602/#604/#646/#665 behavior contract.
func checkParityGuardCases() []checkParityGuardCase {
	return []checkParityGuardCase{
		{
			name:     "wrong_arity_none",
			args:     []string{},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "wrong_arity_one",
			args:     []string{"check"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_command",
			args:     []string{"--help", "examples/hello.kizu"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_target",
			args:     []string{"check", "--help"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			// A production selfhost module, not a hand-sized fixture: nothing else here
			// proves the artifact survives a real one. The fixture cannot be
			// selfhost/src/main.kizu, though -- `check <file>` loads that file plus the
			// std it references and never the sibling modules of its package, so
			// main.kizu's `parser::ParseError` resolves to nothing and the frontend
			// reports an unknown type. Pinning ok there would assert a package-context
			// capability `check <file>` has never had.
			name:     "real_source_target",
			args:     []string{"check", "selfhost/src/backend/data.kizu"},
			exitCode: 0,
			stdout:   "check: ok\n",
		},
		{
			name:     "unsupported_command",
			args:     []string{"bad", "selfhost"},
			exitCode: 64,
			stderr:   "usage: selfhost <check|parse|run|test|fmt> <target>\n",
		},
	}
}

// appendCheckParityHeader writes durable #530 gate metadata.
func appendCheckParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-check-parity-v0\n")
	fmt.Fprintf(out, "issue #530/#602/#604/#646/#665\n")
	fmt.Fprintf(out, "tracker #497\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/check-parity.tsv\n")
	fmt.Fprintf(out, "runner target/selfhost/stage0-native/selfhost\n")
	fmt.Fprintf(out, "runner.build stage0-native (go backend)\n")
	fmt.Fprintf(out, "validation.path stage0-native-artifact\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendCheckParityResult writes one manifest result line group.
func appendCheckParityResult(
	out *strings.Builder,
	item checkParityCase,
	result selfhostCommandResult,
) {
	fmt.Fprintf(out, "case.%s.command %s %s\n", item.name, item.command, item.fixture)
	fmt.Fprintf(out, "case.%s.fixture %s\n", item.name, item.fixture)
	fmt.Fprintf(out, "case.%s.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "case.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "case.%s.stdout.golden %s\n", item.name, item.stdoutGolden)
	fmt.Fprintf(out, "case.%s.stderr.golden %s\n", item.name, item.stderrGolden)
	fmt.Fprintf(out, "case.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "case.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}

// appendCheckParityGuardResult writes one guard result line group.
func appendCheckParityGuardResult(
	out *strings.Builder,
	item checkParityGuardCase,
	result selfhostCommandResult,
) {
	fmt.Fprintf(out, "guard.%s.command %s\n", item.name, strings.Join(item.args, " "))
	fmt.Fprintf(out, "guard.%s.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "guard.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "guard.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "guard.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}

// appendCheckParityFooter writes elapsed time and pass/fail status.
func appendCheckParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}
