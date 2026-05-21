package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type parseParityCase struct {
	name         string
	command      string
	fixture      string
	exitCode     int
	stdoutGolden string
	stderrGolden string
}

type parseParityGuardCase struct {
	name     string
	args     []string
	exitCode int
	stdout   string
	stderr   string
}

// TestSelfhostParseParityGate runs #525 parse cases through the stage2 artifact.
func TestSelfhostParseParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_PARSE_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_PARSE_PARITY=1 to run selfhost parse parity")
	}
	report, failures := runSelfhostParseParity(t)
	if failures > 0 {
		t.Fatalf("selfhost parse parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost parse parity report:\n%s", report)
}

// runSelfhostParseParity executes the #525 manifest with the hosted artifact.
func runSelfhostParseParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage2/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost parse parity runner: %v", err)
		return "", 1
	}
	cases, err := loadParseParityCases("selfhost/tests/cli/parse-parity.tsv")
	if err != nil {
		t.Errorf("load parse parity manifest: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendParseParityHeader(&report, len(cases))
	failures := countParseParityCaseFailures(t, &report, runner, cases)
	failures += countParseParityGuardFailures(t, &report, runner)
	appendParseParityFooter(&report, start, failures)
	if err := os.WriteFile(
		"target/selfhost/reports/parse-parity.txt",
		[]byte(report.String()),
		0o644,
	); err != nil {
		t.Errorf("write parse parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// loadParseParityCases parses the checked-in #525 manifest.
func loadParseParityCases(path string) ([]parseParityCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []parseParityCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseParseParityLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	return cases, nil
}

// parseParseParityLine parses one manifest row.
func parseParseParityLine(line string) (parseParityCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return parseParityCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 6 {
		return parseParityCase{}, false, fmt.Errorf("expected 6 fields")
	}
	code, err := strconv.Atoi(fields[3])
	if err != nil {
		return parseParityCase{}, false, err
	}
	return parseParityCase{
		name:         fields[0],
		command:      fields[1],
		fixture:      fields[2],
		exitCode:     code,
		stdoutGolden: fields[4],
		stderrGolden: fields[5],
	}, true, nil
}

// countParseParityCaseFailures compares manifest cases to checked-in goldens.
func countParseParityCaseFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
	cases []parseParityCase,
) int {
	t.Helper()
	failures := 0
	for _, item := range cases {
		expectedOut, expectedErr, err := readParseParityGoldens(item)
		if err != nil {
			t.Errorf("read parse parity goldens for %s: %v", item.name, err)
			failures++
			continue
		}
		result := runBootstrapCommand(t, runner, item.command, item.fixture)
		if result.code != item.exitCode ||
			result.stdout != expectedOut ||
			result.stderr != expectedErr {
			t.Errorf("parse parity %s mismatch", item.name)
			failures++
		}
		appendParseParityResult(report, item, result)
	}
	return failures
}

// readParseParityGoldens loads the expected stdout and stderr bytes.
func readParseParityGoldens(item parseParityCase) (string, string, error) {
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

// countParseParityGuardFailures checks usage and unsupported-command stability.
func countParseParityGuardFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
) int {
	t.Helper()
	failures := 0
	for _, item := range parseParityGuardCases() {
		result := runBootstrapCommand(t, runner, item.args...)
		if result.code != item.exitCode ||
			result.stdout != item.stdout ||
			result.stderr != item.stderr {
			t.Errorf("parse parity guard %s mismatch", item.name)
			failures++
		}
		appendParseParityGuardResult(report, item, result)
	}
	return failures
}

// parseParityGuardCases returns the #525 non-parse behavior contract.
func parseParityGuardCases() []parseParityGuardCase {
	return []parseParityGuardCase{
		{
			name:     "wrong_arity_none",
			args:     []string{},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "wrong_arity_one",
			args:     []string{"parse"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_command",
			args:     []string{"--help", "selfhost"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_target",
			args:     []string{"parse", "--help"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "unsupported_target",
			args:     []string{"parse", "selfhost/src/main.kizu"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "unsupported_command",
			args:     []string{"bad", "selfhost"},
			exitCode: 64,
			stderr:   "unsupported selfhost command\n",
		},
	}
}

// selfhostUsageStderr returns the stable hosted CLI usage line.
func selfhostUsageStderr() string {
	return "usage: selfhost <check|stage|parse|run|test> <target>\n"
}

// appendParseParityHeader writes durable #525 gate metadata.
func appendParseParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-parse-parity-v0\n")
	fmt.Fprintf(out, "issue #525\n")
	fmt.Fprintf(out, "tracker #497\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/parse-parity.tsv\n")
	fmt.Fprintf(out, "runner target/selfhost/stage2/selfhost\n")
	fmt.Fprintf(out, "bootstrap.report target/selfhost/reports/bootstrap.txt\n")
	fmt.Fprintf(out, "validation.path hosted-stage2-artifact\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendParseParityResult writes one manifest result line group.
func appendParseParityResult(
	out *strings.Builder,
	item parseParityCase,
	result bootstrapCommandResult,
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

// appendParseParityGuardResult writes one guard result line group.
func appendParseParityGuardResult(
	out *strings.Builder,
	item parseParityGuardCase,
	result bootstrapCommandResult,
) {
	fmt.Fprintf(out, "guard.%s.command %s\n", item.name, strings.Join(item.args, " "))
	fmt.Fprintf(out, "guard.%s.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "guard.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "guard.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "guard.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}

// appendParseParityFooter writes elapsed time and pass/fail status.
func appendParseParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}
