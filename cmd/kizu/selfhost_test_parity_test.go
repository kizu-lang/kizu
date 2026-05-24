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

// TestSelfhostTestParityGate runs #570 test cases through the stage2 artifact.
func TestSelfhostTestParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_TEST_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_TEST_PARITY=1 to run selfhost test parity")
	}
	report, failures := runSelfhostTestParity(t)
	if failures > 0 {
		t.Fatalf("selfhost test parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost test parity report:\n%s", report)
}

// TestSelfhostTestParityRecipes keeps the fast gate separate from bootstrap.
func TestSelfhostTestParityRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-test-parity-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_TEST_PARITY=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostTestParityGate$")
	requireNoRecipeFragment(t, gate, "just selfhost-bootstrap")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")

	fromScratch := justRecipe(content, "selfhost-test-parity-gate-from-scratch")
	requireRecipeFragment(t, fromScratch, "just selfhost-bootstrap")
	requireRecipeFragment(t, fromScratch, "just selfhost-test-parity-gate")
}

// runSelfhostTestParity executes the #570 manifest with the hosted artifact.
func runSelfhostTestParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage2/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost test parity runner: %v", err)
		return "", 1
	}
	cases, err := loadTestParityCases("selfhost/tests/cli/test-parity.tsv")
	if err != nil {
		t.Errorf("load test parity manifest: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendTestParityHeader(&report, len(cases))
	failures := countTestParityCaseFailures(t, &report, runner, cases)
	failures += countTestParityGuardFailures(t, &report, runner)
	appendTestParityFooter(&report, start, failures)
	if err := writeTestParityReport(report.String()); err != nil {
		t.Errorf("write test parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// loadTestParityCases parses the checked-in #570 manifest.
func loadTestParityCases(path string) ([]runParityCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []runParityCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseRunParityLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	return cases, nil
}

// countTestParityCaseFailures compares manifest cases to checked-in goldens.
func countTestParityCaseFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
	cases []runParityCase,
) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("selfhost test parity requires clang: %v", err)
		return 1
	}
	if err := prepareTestParityDir(); err != nil {
		t.Errorf("prepare test parity dir: %v", err)
		return 1
	}
	failures := 0
	for _, item := range cases {
		result, caseFailures := runTestParityCase(t, clang, runner, item)
		failures += caseFailures
		appendTestParityResult(report, item, result)
	}
	return failures
}

// runTestParityCase emits, links, and executes one hosted test artifact case.
func runTestParityCase(
	t *testing.T,
	clang string,
	runner string,
	item runParityCase,
) (runParityResult, int) {
	t.Helper()
	result := runParityResult{compiler: runBootstrapCommand(t, runner, item.command, item.fixture)}
	expectedOut, expectedErr, err := readRunParityGoldens(item)
	if err != nil {
		t.Errorf("read test parity goldens for %s: %v", item.name, err)
		return result, 1
	}
	if item.artifactMode != "hosted-artifact" {
		t.Errorf("test parity %s unsupported artifact mode %q", item.name, item.artifactMode)
		return result, 1
	}
	if item.artifactStem == "-" {
		return result, compareTestCompilerResult(t, item, result.compiler, expectedOut, expectedErr) +
			countUnexpectedTestArtifacts(t, item)
	}
	linkFailures := linkAndRunTestParityArtifact(t, clang, item, &result)
	checkFailures := compareTestProgramResult(t, item, result, expectedOut, expectedErr)
	return result, linkFailures + checkFailures
}

// countUnexpectedTestArtifacts rejects artifact emission after unsupported test lowering.
func countUnexpectedTestArtifacts(t *testing.T, item runParityCase) int {
	t.Helper()
	failures := 0
	for _, path := range []string{
		filepath.Join("target/selfhost/test", item.name+".ll"),
		filepath.Join("target/selfhost/test", item.name+".ll.meta"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("test parity %s emitted unexpected artifact %s", item.name, path)
			failures++
		}
	}
	return failures
}

// compareTestCompilerResult checks unsupported test command output.
func compareTestCompilerResult(
	t *testing.T,
	item runParityCase,
	result bootstrapCommandResult,
	expectedOut string,
	expectedErr string,
) int {
	t.Helper()
	if result.code != item.exitCode || result.stdout != expectedOut || result.stderr != expectedErr {
		t.Errorf("test parity %s compiler mismatch", item.name)
		return 1
	}
	return 0
}

// linkAndRunTestParityArtifact links the emitted LLVM and captures execution.
func linkAndRunTestParityArtifact(
	t *testing.T,
	clang string,
	item runParityCase,
	result *runParityResult,
) int {
	t.Helper()
	if item.artifactStem == "" || item.artifactStem == "-" {
		t.Errorf("test parity %s missing artifact stem", item.name)
		return 1
	}
	result.llPath = filepath.Join("target/selfhost/test", item.artifactStem+".ll")
	result.metadataPath = result.llPath + ".meta"
	result.exePath = filepath.Join("target/selfhost/test", item.name)
	if result.compiler.code != 0 || result.compiler.stdout != "" || result.compiler.stderr != "" {
		t.Errorf("test parity %s compiler output mismatch", item.name)
		return 1
	}
	var err error
	result.llBytes, err = fileSize(result.llPath)
	if err != nil {
		t.Errorf("test parity %s artifact missing: %v", item.name, err)
		return 1
	}
	result.metadataBytes, err = fileSize(result.metadataPath)
	if err != nil {
		t.Errorf("test parity %s metadata missing: %v", item.name, err)
		return 1
	}
	if err := linkTestParityExecutable(clang, result.llPath, result.exePath); err != nil {
		t.Errorf("link test parity %s: %v", item.name, err)
		return 1
	}
	result.program = runRunParityExecutable(t, result.exePath)
	return 0
}

// compareTestProgramResult checks executable output against checked-in goldens.
func compareTestProgramResult(
	t *testing.T,
	item runParityCase,
	result runParityResult,
	expectedOut string,
	expectedErr string,
) int {
	t.Helper()
	if result.program.code != item.exitCode ||
		result.program.stdout != expectedOut ||
		result.program.stderr != expectedErr {
		t.Errorf("test parity %s program mismatch", item.name)
		return 1
	}
	return 0
}

// prepareTestParityDir removes bounded test artifacts without precreating the leaf.
func prepareTestParityDir() error {
	if err := os.RemoveAll("target/selfhost/test"); err != nil {
		return err
	}
	return os.MkdirAll("target/selfhost/reports", 0o755)
}

// linkTestParityExecutable links one emitted test artifact with the host runtime.
func linkTestParityExecutable(clang string, llPath string, exePath string) error {
	return linkTestParityExecutableWithHost(
		clang,
		llPath,
		"target/selfhost/stage2/selfhost.host.ll",
		exePath,
	)
}

// linkTestParityExecutableWithHost links one emitted test artifact with a host runtime.
func linkTestParityExecutableWithHost(
	clang string,
	llPath string,
	hostLLPath string,
	exePath string,
) error {
	harnessPath := filepath.Join(filepath.Dir(exePath), "hosted_test_main.c")
	if err := os.WriteFile(harnessPath, []byte(hostedTestHarnessSource), 0o644); err != nil {
		return err
	}
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		llPath,
		hostLLPath,
		"selfhost/runtime/selfhost.hosted.c",
		harnessPath,
		"-o",
		exePath,
	)
	if out, err := compile.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// countTestParityGuardFailures checks usage and unsupported-command stability.
func countTestParityGuardFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
) int {
	t.Helper()
	failures := 0
	for _, item := range testParityGuardCases() {
		result := runBootstrapCommand(t, runner, item.args...)
		if result.code != item.exitCode ||
			result.stdout != item.stdout ||
			result.stderr != item.stderr {
			t.Errorf("test parity guard %s mismatch", item.name)
			failures++
		}
		appendTestParityGuardResult(report, item, result)
	}
	return failures
}

// testParityGuardCases returns the #570 unsupported behavior contract.
func testParityGuardCases() []runParityGuardCase {
	return []runParityGuardCase{
		{name: "wrong_arity_none", args: []string{}, exitCode: 64, stderr: selfhostUsageStderr()},
		{name: "wrong_arity_one", args: []string{"test"}, exitCode: 64, stderr: selfhostUsageStderr()},
		{
			name:     "flag_command",
			args:     []string{"--help", "selfhost/tests/cli/test_expect_ok.kizu"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_target",
			args:     []string{"test", "--help"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "unsupported_target",
			args:     []string{"test", "selfhost/tests/cli/parse_ok_minimal.kizu"},
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

// appendTestParityHeader writes durable #570 gate metadata.
func appendTestParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-test-parity-v0\n")
	fmt.Fprintf(out, "issue #570\n")
	fmt.Fprintf(out, "tracker #497\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/test-parity.tsv\n")
	fmt.Fprintf(out, "runner target/selfhost/stage2/selfhost\n")
	fmt.Fprintf(out, "bootstrap.report target/selfhost/reports/bootstrap.txt\n")
	fmt.Fprintf(out, "validation.path hosted-stage2-artifact\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	fmt.Fprintf(out, "artifact.dir target/selfhost/test\n")
	fmt.Fprintf(out, "discovery none\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendTestParityResult writes one manifest result line group.
func appendTestParityResult(
	out *strings.Builder,
	item runParityCase,
	result runParityResult,
) {
	fmt.Fprintf(out, "case.%s.command %s %s\n", item.name, item.command, item.fixture)
	fmt.Fprintf(out, "case.%s.fixture %s\n", item.name, item.fixture)
	fmt.Fprintf(out, "case.%s.artifact_mode %s\n", item.name, item.artifactMode)
	fmt.Fprintf(out, "case.%s.artifact_stem %s\n", item.name, item.artifactStem)
	fmt.Fprintf(out, "case.%s.compiler.exit %d\n", item.name, result.compiler.code)
	appendTestParityCompilerResult(out, item, result)
	appendRunParityArtifactResult(out, item, result)
	if result.llPath == "" {
		fmt.Fprintf(out, "case.%s.program.executed false\n", item.name)
		return
	}
	fmt.Fprintf(out, "case.%s.program.executed true\n", item.name)
	fmt.Fprintf(out, "case.%s.program.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "case.%s.program.exit %d\n", item.name, result.program.code)
	fmt.Fprintf(out, "case.%s.program.stdout.golden %s\n", item.name, item.stdoutGolden)
	fmt.Fprintf(out, "case.%s.program.stderr.golden %s\n", item.name, item.stderrGolden)
	appendTestParityProgramResult(out, item, result)
}

// appendTestParityCompilerResult writes compiler stdout/stderr fingerprints.
func appendTestParityCompilerResult(
	out *strings.Builder,
	item runParityCase,
	result runParityResult,
) {
	fmt.Fprintf(
		out,
		"case.%s.compiler.stdout.sha256 %s\n",
		item.name,
		textFingerprint(result.compiler.stdout),
	)
	fmt.Fprintf(
		out,
		"case.%s.compiler.stderr.sha256 %s\n",
		item.name,
		textFingerprint(result.compiler.stderr),
	)
}

// appendTestParityProgramResult writes program stdout/stderr fingerprints.
func appendTestParityProgramResult(
	out *strings.Builder,
	item runParityCase,
	result runParityResult,
) {
	fmt.Fprintf(
		out,
		"case.%s.program.stdout.sha256 %s\n",
		item.name,
		textFingerprint(result.program.stdout),
	)
	fmt.Fprintf(
		out,
		"case.%s.program.stderr.sha256 %s\n",
		item.name,
		textFingerprint(result.program.stderr),
	)
}

// appendTestParityGuardResult writes one guard result line group.
func appendTestParityGuardResult(
	out *strings.Builder,
	item runParityGuardCase,
	result bootstrapCommandResult,
) {
	fmt.Fprintf(out, "guard.%s.command %s\n", item.name, strings.Join(item.args, " "))
	fmt.Fprintf(out, "guard.%s.exit.expected %d\n", item.name, item.exitCode)
	fmt.Fprintf(out, "guard.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "guard.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "guard.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}

// appendTestParityFooter writes elapsed time and pass/fail status.
func appendTestParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}

// writeTestParityReport persists the #570 gate report.
func writeTestParityReport(report string) error {
	return os.WriteFile("target/selfhost/reports/test-parity.txt", []byte(report), 0o644)
}

const hostedTestHarnessSource = `
#include <stdint.h>

void kizu_host_init(int argc, char **argv);
int64_t kizu_test_main(void);

int main(int argc, char **argv) {
    kizu_host_init(argc, argv);
    return (int)kizu_test_main();
}
`
