package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type runParityCase struct {
	name         string
	command      string
	fixture      string
	exitCode     int
	stdoutGolden string
	stderrGolden string
	artifactMode string
	artifactStem string
}

type runParityGuardCase struct {
	name     string
	args     []string
	exitCode int
	stdout   string
	stderr   string
}

type runParityResult struct {
	compiler      bootstrapCommandResult
	program       bootstrapCommandResult
	llPath        string
	metadataPath  string
	exePath       string
	llBytes       int64
	metadataBytes int64
}

// TestSelfhostRunParityGate runs #569 run cases through the stage2 artifact.
func TestSelfhostRunParityGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_RUN_PARITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_RUN_PARITY=1 to run selfhost run parity")
	}
	report, failures := runSelfhostRunParity(t)
	if failures > 0 {
		t.Fatalf("selfhost run parity failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost run parity report:\n%s", report)
}

// TestSelfhostRunParityRecipes keeps the fast gate separate from bootstrap.
func TestSelfhostRunParityRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-run-parity-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_RUN_PARITY=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostRunParityGate$")
	requireNoRecipeFragment(t, gate, "just selfhost-bootstrap")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")

	fromScratch := justRecipe(content, "selfhost-run-parity-gate-from-scratch")
	requireRecipeFragment(t, fromScratch, "just selfhost-bootstrap")
	requireRecipeFragment(t, fromScratch, "just selfhost-run-parity-gate")
}

// runSelfhostRunParity executes the #569 manifest with the hosted artifact.
func runSelfhostRunParity(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage2/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost run parity runner: %v", err)
		return "", 1
	}
	cases, err := loadRunParityCases("selfhost/tests/cli/run-parity.tsv")
	if err != nil {
		t.Errorf("load run parity manifest: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendRunParityHeader(&report, len(cases))
	failures := countRunParityCaseFailures(t, &report, runner, cases)
	failures += countRunParityGuardFailures(t, &report, runner)
	appendRunParityFooter(&report, start, failures)
	if err := writeRunParityReport(report.String()); err != nil {
		t.Errorf("write run parity report: %v", err)
		failures++
	}
	return report.String(), failures
}

// loadRunParityCases parses the checked-in #569 manifest.
func loadRunParityCases(path string) ([]runParityCase, error) {
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

// parseRunParityLine parses one manifest row.
func parseRunParityLine(line string) (runParityCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return runParityCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 7 && len(fields) != 8 {
		return runParityCase{}, false, fmt.Errorf("expected 7 or 8 fields")
	}
	code, err := strconv.Atoi(fields[3])
	if err != nil {
		return runParityCase{}, false, err
	}
	item := runParityCase{
		name:         fields[0],
		command:      fields[1],
		fixture:      fields[2],
		exitCode:     code,
		stdoutGolden: fields[4],
		stderrGolden: fields[5],
		artifactMode: fields[6],
		artifactStem: fields[0],
	}
	if len(fields) == 8 {
		item.artifactStem = fields[7]
	}
	return item, true, nil
}

// countRunParityCaseFailures compares manifest cases to checked-in goldens.
func countRunParityCaseFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
	cases []runParityCase,
) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("selfhost run parity requires clang: %v", err)
		return 1
	}
	if err := prepareRunParityDir(); err != nil {
		t.Errorf("prepare run parity dir: %v", err)
		return 1
	}
	failures := 0
	for _, item := range cases {
		result, caseFailures := runRunParityCase(t, clang, runner, item)
		failures += caseFailures
		appendRunParityResult(report, item, result)
	}
	return failures
}

// runRunParityCase emits, links, and executes one hosted run artifact case.
func runRunParityCase(
	t *testing.T,
	clang string,
	runner string,
	item runParityCase,
) (runParityResult, int) {
	t.Helper()
	result := runParityResult{compiler: runBootstrapCommand(t, runner, item.command, item.fixture)}
	expectedOut, expectedErr, err := readRunParityGoldens(item)
	if err != nil {
		t.Errorf("read run parity goldens for %s: %v", item.name, err)
		return result, 1
	}
	if item.artifactMode != "hosted-artifact" {
		t.Errorf("run parity %s unsupported artifact mode %q", item.name, item.artifactMode)
		return result, 1
	}
	if item.exitCode != 0 {
		return result, compareRunCompilerResult(t, item, result.compiler, expectedOut, expectedErr) +
			countUnexpectedRunArtifacts(t, item)
	}
	linkFailures := linkAndRunRunParityArtifact(t, clang, item, &result)
	checkFailures := compareRunProgramResult(t, item, result, expectedOut, expectedErr)
	return result, linkFailures + checkFailures
}

// countUnexpectedRunArtifacts rejects artifact emission after frontend failure.
func countUnexpectedRunArtifacts(t *testing.T, item runParityCase) int {
	t.Helper()
	failures := 0
	for _, path := range []string{
		filepath.Join("target/selfhost/run", item.name+".ll"),
		filepath.Join("target/selfhost/run", item.name+".ll.meta"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("run parity %s emitted unexpected artifact %s", item.name, path)
			failures++
		}
	}
	return failures
}

// compareRunCompilerResult checks frontend-failure run command output.
func compareRunCompilerResult(
	t *testing.T,
	item runParityCase,
	result bootstrapCommandResult,
	expectedOut string,
	expectedErr string,
) int {
	t.Helper()
	if result.code != item.exitCode || result.stdout != expectedOut || result.stderr != expectedErr {
		t.Errorf("run parity %s compiler mismatch", item.name)
		return 1
	}
	return 0
}

// linkAndRunRunParityArtifact links the emitted LLVM and captures its execution.
func linkAndRunRunParityArtifact(
	t *testing.T,
	clang string,
	item runParityCase,
	result *runParityResult,
) int {
	t.Helper()
	if item.artifactStem == "" || item.artifactStem == "-" {
		t.Errorf("run parity %s missing artifact stem", item.name)
		return 1
	}
	result.llPath = filepath.Join("target/selfhost/run", item.artifactStem+".ll")
	result.metadataPath = result.llPath + ".meta"
	result.exePath = filepath.Join("target/selfhost/run", item.name)
	if result.compiler.code != 0 || result.compiler.stdout != "" || result.compiler.stderr != "" {
		t.Errorf("run parity %s compiler output mismatch", item.name)
		return 1
	}
	var err error
	result.llBytes, err = fileSize(result.llPath)
	if err != nil {
		t.Errorf("run parity %s artifact missing: %v", item.name, err)
		return 1
	}
	result.metadataBytes, err = fileSize(result.metadataPath)
	if err != nil {
		t.Errorf("run parity %s metadata missing: %v", item.name, err)
		return 1
	}
	if failures := countRunCodegenMetadataFailures(t, item, result.metadataPath); failures > 0 {
		return failures
	}
	if err := linkRunParityExecutable(clang, result.llPath, result.exePath); err != nil {
		t.Errorf("link run parity %s: %v", item.name, err)
		return 1
	}
	result.program = runRunParityExecutable(t, result.exePath)
	return 0
}

// countRunCodegenMetadataFailures proves selected run artifacts use codegen IR.
func countRunCodegenMetadataFailures(t *testing.T, item runParityCase, metadataPath string) int {
	t.Helper()
	if item.artifactStem == "run_return" {
		return 0
	}
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Errorf("run parity %s read metadata: %v", item.name, err)
		return 1
	}
	metadata := string(metadataBytes)
	required := []string{
		"codegen_ir selfhost::ir::codegen::Program main-print-v0\n",
		"run.codegen-ir enabled\n",
		"go.cmd-kizu-fallback none\n",
	}
	for _, fragment := range required {
		if !strings.Contains(metadata, fragment) {
			t.Errorf("run parity %s metadata missing %q", item.name, fragment)
			return 1
		}
	}
	return 0
}

// compareRunProgramResult checks executable output against checked-in goldens.
func compareRunProgramResult(
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
		t.Errorf("run parity %s program mismatch", item.name)
		return 1
	}
	return 0
}

// readRunParityGoldens loads the expected stdout and stderr bytes.
func readRunParityGoldens(item runParityCase) (string, string, error) {
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

// prepareRunParityDir removes bounded run artifacts without precreating the leaf.
func prepareRunParityDir() error {
	if err := os.RemoveAll("target/selfhost/run"); err != nil {
		return err
	}
	return os.MkdirAll("target/selfhost/reports", 0o755)
}

// linkRunParityExecutable links one emitted run artifact with the host runtime.
func linkRunParityExecutable(clang string, llPath string, exePath string) error {
	return linkRunParityExecutableWithHost(
		clang,
		llPath,
		"target/selfhost/stage2/selfhost.host.ll",
		exePath,
	)
}

// linkRunParityExecutableWithHost links one emitted run artifact with a host runtime.
func linkRunParityExecutableWithHost(
	clang string,
	llPath string,
	hostLLPath string,
	exePath string,
) error {
	harnessPath := filepath.Join(filepath.Dir(exePath), "hosted_run_main.c")
	if err := os.WriteFile(harnessPath, []byte(hostedRunHarnessSource), 0o644); err != nil {
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

// runRunParityExecutable captures stdout, stderr, and exit code for run output.
func runRunParityExecutable(t *testing.T, exePath string) bootstrapCommandResult {
	t.Helper()
	start := time.Now()
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Errorf("resolve %s: %v", exePath, err)
	}
	run := exec.Command(absExe)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	return bootstrapCommandResult{
		name:    filepath.Base(exePath),
		command: absExe,
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// fileSize returns the size of a regular file.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	return info.Size(), nil
}

// countRunParityGuardFailures checks usage and unsupported-command stability.
func countRunParityGuardFailures(
	t *testing.T,
	report *strings.Builder,
	runner string,
) int {
	t.Helper()
	failures := 0
	for _, item := range runParityGuardCases() {
		result := runBootstrapCommand(t, runner, item.args...)
		if result.code != item.exitCode ||
			result.stdout != item.stdout ||
			result.stderr != item.stderr {
			t.Errorf("run parity guard %s mismatch", item.name)
			failures++
		}
		appendRunParityGuardResult(report, item, result)
	}
	return failures
}

// runParityGuardCases returns the #569 unsupported behavior contract.
func runParityGuardCases() []runParityGuardCase {
	return []runParityGuardCase{
		{name: "wrong_arity_none", args: []string{}, exitCode: 64, stderr: selfhostUsageStderr()},
		{name: "wrong_arity_one", args: []string{"run"}, exitCode: 64, stderr: selfhostUsageStderr()},
		{
			name:     "flag_command",
			args:     []string{"--help", "selfhost/tests/cli/run_hello.kizu"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "flag_target",
			args:     []string{"run", "--help"},
			exitCode: 64,
			stderr:   selfhostUsageStderr(),
		},
		{
			name:     "unsupported_target",
			args:     []string{"run", "selfhost/tests/cli/test_expect_ok.kizu"},
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

// appendRunParityHeader writes durable #569 gate metadata.
func appendRunParityHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-run-parity-v0\n")
	fmt.Fprintf(out, "issue #569\n")
	fmt.Fprintf(out, "tracker #497\n")
	fmt.Fprintf(out, "manifest selfhost/tests/cli/run-parity.tsv\n")
	fmt.Fprintf(out, "runner target/selfhost/stage2/selfhost\n")
	fmt.Fprintf(out, "bootstrap.report target/selfhost/reports/bootstrap.txt\n")
	fmt.Fprintf(out, "validation.path hosted-stage2-artifact\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	fmt.Fprintf(out, "artifact.dir target/selfhost/run\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendRunParityResult writes one manifest result line group.
func appendRunParityResult(
	out *strings.Builder,
	item runParityCase,
	result runParityResult,
) {
	fmt.Fprintf(out, "case.%s.command %s %s\n", item.name, item.command, item.fixture)
	fmt.Fprintf(out, "case.%s.fixture %s\n", item.name, item.fixture)
	fmt.Fprintf(out, "case.%s.artifact_mode %s\n", item.name, item.artifactMode)
	fmt.Fprintf(out, "case.%s.artifact_stem %s\n", item.name, item.artifactStem)
	fmt.Fprintf(out, "case.%s.compiler.exit %d\n", item.name, result.compiler.code)
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

// appendRunParityArtifactResult writes artifact paths and sizes when emitted.
func appendRunParityArtifactResult(
	out *strings.Builder,
	item runParityCase,
	result runParityResult,
) {
	if result.llPath == "" {
		fmt.Fprintf(out, "case.%s.artifact.emitted false\n", item.name)
		return
	}
	fmt.Fprintf(out, "case.%s.artifact.emitted true\n", item.name)
	fmt.Fprintf(out, "case.%s.artifact.ll.path %s\n", item.name, result.llPath)
	fmt.Fprintf(out, "case.%s.artifact.ll.bytes %d\n", item.name, result.llBytes)
	fmt.Fprintf(out, "case.%s.artifact.metadata.path %s\n", item.name, result.metadataPath)
	fmt.Fprintf(out, "case.%s.artifact.metadata.bytes %d\n", item.name, result.metadataBytes)
	fmt.Fprintf(out, "case.%s.artifact.exe.path %s\n", item.name, result.exePath)
}

// appendRunParityGuardResult writes one guard result line group.
func appendRunParityGuardResult(
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

// appendRunParityFooter writes elapsed time and pass/fail status.
func appendRunParityFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}

// writeRunParityReport persists the #569 gate report.
func writeRunParityReport(report string) error {
	return os.WriteFile("target/selfhost/reports/run-parity.txt", []byte(report), 0o644)
}

const hostedRunHarnessSource = `
#include <stdint.h>

void kizu_host_init(int argc, char **argv);
int64_t kizu_run_main(void);

int main(int argc, char **argv) {
    kizu_host_init(argc, argv);
    return (int)kizu_run_main();
}
`
