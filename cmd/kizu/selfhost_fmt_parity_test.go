package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fmtParityCase struct {
	name          string
	command       string
	fixture       string
	exitCode      int
	stdoutGolden  string
	stderrGolden  string
	contentGolden string
	mutation      string
}

type fmtParityResult struct {
	command            bootstrapCommandResult
	content            string
	contentChecked     bool
	targetPath         string
	writeMirror        bootstrapCommandResult
	writeMirrorContent string
	writeMirrorChecked bool
	writeMirrorTarget  string
}

// TestSelfhostFmtParityGate runs #1073 fmt cases through the stage2 artifact.
func TestSelfhostFmtParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_FMT_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_FMT_PARITY=1 to run selfhost fmt parity")
	}
	report, failures := runSelfhostFmtParity(t)
	if failures > 0 {
		t.Fatalf("selfhost fmt parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost fmt parity report:\n%s", report)
}

// TestSelfhostFmtParityRecipes keeps the fast gate separate from bootstrap.
func TestSelfhostFmtParityRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-fmt-parity-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_FMT_PARITY=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostFmtParityGate$")
	requireNoRecipeFragment(t, gate, "just selfhost-bootstrap")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")

	fromScratch := justRecipe(content, "selfhost-fmt-parity-gate-from-scratch")
	requireRecipeFragment(t, fromScratch, "just selfhost-bootstrap")
	requireRecipeFragment(t, fromScratch, "just selfhost-fmt-parity-gate")

	fastGate := justRecipe(content, "selfhost-fast-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-fmt-parity-gate")
	requireNoRecipeFragment(t, fastGate, "just selfhost-bootstrap")
	requireNoRecipeFragment(t, fastGate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")
}

// runSelfhostFmtParity executes the #1073 manifest with the hosted artifact.
func runSelfhostFmtParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage2/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost fmt parity runner: %v", err)
		return "", 1
	}
	cases, err := loadFmtParityCases("selfhost/tests/cli/fmt-parity.tsv")
	if err != nil {
		t.Errorf("load fmt parity manifest: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendFmtParityHeader(&report, len(cases))
	failures := countFmtParityCaseFailures(t, &report, runner, cases)
	appendFmtParityFooter(&report, start, failures)
	if err := os.WriteFile(
		"target/selfhost/reports/fmt-parity.txt",
		[]byte(report.String()),
		0o644,
	); err != nil {
		t.Errorf("write fmt parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// loadFmtParityCases parses the checked-in #1073 manifest.
func loadFmtParityCases(path string) ([]fmtParityCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []fmtParityCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseFmtParityLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	return cases, nil
}

// parseFmtParityLine parses one manifest row.
func parseFmtParityLine(line string) (fmtParityCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return fmtParityCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 8 {
		return fmtParityCase{}, false, fmt.Errorf("expected 8 fields")
	}
	code, err := strconv.Atoi(fields[3])
	if err != nil {
		return fmtParityCase{}, false, err
	}
	return fmtParityCase{
		name:          fields[0],
		command:       fields[1],
		fixture:       fields[2],
		exitCode:      code,
		stdoutGolden:  fields[4],
		stderrGolden:  fields[5],
		contentGolden: fields[6],
		mutation:      fields[7],
	}, true, nil
}

// countFmtParityCaseFailures compares manifest cases to checked-in goldens.
func countFmtParityCaseFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
	cases []fmtParityCase,
) int {
	t.Helper()
	failures := 0
	for _, item := range cases {
		result, caseFailures := runFmtParityCase(t, runner, item)
		failures += caseFailures
		appendFmtParityResult(report, item, result)
	}
	return failures
}

// runFmtParityCase executes one fmt or fmt-write case through the stage2 artifact.
func runFmtParityCase(t *testing.T, runner string, item fmtParityCase) (fmtParityResult, int) {
	t.Helper()
	expectedOut, expectedErr, err := readFmtParityGoldens(item)
	if err != nil {
		t.Errorf("read fmt parity goldens for %s: %v", item.name, err)
		return fmtParityResult{}, 1
	}
	result, expectedContent, failures := executeFmtParityCase(t, runner, item)
	if result.command.code != item.exitCode ||
		result.command.stdout != expectedOut ||
		result.command.stderr != expectedErr {
		t.Errorf("fmt parity %s output mismatch", item.name)
		failures++
	}
	if result.contentChecked && result.content != expectedContent {
		t.Errorf(
			"fmt parity %s content mismatch\nwant:\n%sgot:\n%s",
			item.name,
			expectedContent,
			result.content,
		)
		failures++
	}
	if item.command == "fmt" && item.exitCode == 0 {
		failures += countFmtWriteMirrorFailures(t, runner, item, expectedOut, &result)
	}
	return result, failures
}

// executeFmtParityCase runs one manifest item and returns its expected file bytes.
func executeFmtParityCase(
	t *testing.T,
	runner string,
	item fmtParityCase,
) (fmtParityResult, string, int) {
	t.Helper()
	switch item.command {
	case "fmt":
		if item.mutation != "none" || item.contentGolden != "-" {
			t.Errorf("fmt parity %s has invalid read-only metadata", item.name)
			return fmtParityResult{}, "", 1
		}
		return fmtParityResult{
			command:    runBootstrapCommand(t, runner, "fmt", item.fixture),
			targetPath: item.fixture,
		}, "", 0
	case "fmt-write":
		return executeFmtWriteParityCase(t, runner, item)
	default:
		t.Errorf("fmt parity %s unsupported command %q", item.name, item.command)
		return fmtParityResult{}, "", 1
	}
}

// countFmtWriteMirrorFailures checks stdout and --write share formatted bytes.
func countFmtWriteMirrorFailures(
	t *testing.T,
	runner string,
	item fmtParityCase,
	expectedOut string,
	result *fmtParityResult,
) int {
	t.Helper()
	original, err := os.ReadFile(item.fixture)
	if err != nil {
		t.Errorf("read fmt parity fixture %s: %v", item.fixture, err)
		return 1
	}
	target := filepath.Join(t.TempDir(), filepath.Base(item.fixture))
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Errorf("write fmt parity mirror fixture %s: %v", target, err)
		return 1
	}
	result.writeMirrorTarget = target
	result.writeMirror = runBootstrapCommand(t, runner, "fmt", "--write", target)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Errorf("read fmt parity mirror fixture %s: %v", target, err)
		return 1
	}
	result.writeMirrorContent = string(written)
	result.writeMirrorChecked = true

	failures := 0
	if result.writeMirror.code != 0 ||
		result.writeMirror.stdout != "" ||
		result.writeMirror.stderr != "" {
		t.Errorf("fmt parity %s --write mirror output mismatch", item.name)
		failures++
	}
	expectedContent := strings.TrimSuffix(expectedOut, "\n")
	if result.writeMirrorContent != expectedContent {
		t.Errorf(
			"fmt parity %s --write mirror content mismatch\nwant:\n%sgot:\n%s",
			item.name,
			expectedContent,
			result.writeMirrorContent,
		)
		failures++
	}
	return failures
}

// executeFmtWriteParityCase copies the fixture so --write never mutates sources.
func executeFmtWriteParityCase(
	t *testing.T,
	runner string,
	item fmtParityCase,
) (fmtParityResult, string, int) {
	t.Helper()
	original, err := os.ReadFile(item.fixture)
	if err != nil {
		t.Errorf("read fmt parity fixture %s: %v", item.fixture, err)
		return fmtParityResult{}, "", 1
	}
	target := filepath.Join(t.TempDir(), filepath.Base(item.fixture))
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Errorf("write fmt parity temp fixture %s: %v", target, err)
		return fmtParityResult{}, "", 1
	}
	result := fmtParityResult{
		command:        runBootstrapCommand(t, runner, "fmt", "--write", target),
		contentChecked: true,
		targetPath:     target,
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Errorf("read fmt parity temp fixture %s: %v", target, err)
		return result, "", 1
	}
	result.content = string(written)
	expected, err := expectedFmtWriteContent(item, string(original))
	if err != nil {
		t.Errorf("resolve fmt parity expected content for %s: %v", item.name, err)
		return result, "", 1
	}
	return result, expected, 0
}

// expectedFmtWriteContent resolves the post-command file content contract.
func expectedFmtWriteContent(item fmtParityCase, original string) (string, error) {
	switch item.mutation {
	case "trim-final-newline":
		content, err := readFmtTextGolden(item.contentGolden)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(content, "\n"), nil
	case "preserve":
		if item.contentGolden != "-" {
			return "", fmt.Errorf("preserve rows must not name content golden")
		}
		return original, nil
	default:
		return "", fmt.Errorf("unsupported mutation %q", item.mutation)
	}
}

// readFmtParityGoldens loads expected stdout and stderr bytes.
func readFmtParityGoldens(item fmtParityCase) (string, string, error) {
	stdout, err := readFmtTextGolden(item.stdoutGolden)
	if err != nil {
		return "", "", err
	}
	stderr, err := readFmtTextGolden(item.stderrGolden)
	if err != nil {
		return "", "", err
	}
	return stdout, stderr, nil
}

// readFmtTextGolden treats "-" as an empty stream.
func readFmtTextGolden(path string) (string, error) {
	if path == "-" {
		return "", nil
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// appendFmtParityHeader writes durable #1073 gate metadata.
func appendFmtParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-fmt-parity-v0\n")
	fmt.Fprintf(out, "issue #1073\n")
	fmt.Fprintf(out, "tracker #497\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/fmt-parity.tsv\n")
	fmt.Fprintf(out, "runner target/selfhost/stage2/selfhost\n")
	fmt.Fprintf(out, "bootstrap.report target/selfhost/reports/bootstrap.txt\n")
	fmt.Fprintf(out, "validation.path hosted-stage2-artifact\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendFmtParityResult writes one manifest result line group.
func appendFmtParityResult(out *strings.Builder, item fmtParityCase, result fmtParityResult) {
	fmt.Fprintf(out, "case.%s.command %s %s\n", item.name, item.command, item.fixture)
	fmt.Fprintf(out, "case.%s.fixture %s\n", item.name, item.fixture)
	fmt.Fprintf(out, "case.%s.target %s\n", item.name, result.targetPath)
	fmt.Fprintf(out, "case.%s.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "case.%s.exit %d\n", item.name, result.command.code)
	fmt.Fprintf(out, "case.%s.stdout.golden %s\n", item.name, item.stdoutGolden)
	fmt.Fprintf(out, "case.%s.stderr.golden %s\n", item.name, item.stderrGolden)
	fmt.Fprintf(out, "case.%s.content.golden %s\n", item.name, item.contentGolden)
	fmt.Fprintf(out, "case.%s.mutation %s\n", item.name, item.mutation)
	fmt.Fprintf(out, "case.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.command.stdout))
	fmt.Fprintf(out, "case.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.command.stderr))
	if result.contentChecked {
		fmt.Fprintf(out, "case.%s.content.sha256 %s\n", item.name, textFingerprint(result.content))
	}
	if result.writeMirrorChecked {
		fmt.Fprintf(out, "case.%s.write-mirror.target %s\n", item.name, result.writeMirrorTarget)
		fmt.Fprintf(out, "case.%s.write-mirror.exit %d\n", item.name, result.writeMirror.code)
		fmt.Fprintf(
			out,
			"case.%s.write-mirror.stdout.sha256 %s\n",
			item.name,
			textFingerprint(result.writeMirror.stdout),
		)
		fmt.Fprintf(
			out,
			"case.%s.write-mirror.stderr.sha256 %s\n",
			item.name,
			textFingerprint(result.writeMirror.stderr),
		)
		fmt.Fprintf(
			out,
			"case.%s.write-mirror.content.sha256 %s\n",
			item.name,
			textFingerprint(result.writeMirrorContent),
		)
	}
}

// appendFmtParityFooter writes elapsed time and pass/fail status.
func appendFmtParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}
