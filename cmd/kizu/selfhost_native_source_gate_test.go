package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	nativeSourceRunnerPath                 = "target/selfhost/native-source/selfhost"
	nativeSourceHostRuntimePath            = "target/selfhost/selfhost.host.ll"
	nativeSourceExecutableLoweringMetadata = "executable_lowering " +
		"selfhost::backend::executable checked-ast\n"
)

// TestSelfhostNativeSourceExecutableGate builds selfhost from source and runs
// executable artifacts through the Kizu-owned checked-AST lowering path.
func TestSelfhostNativeSourceExecutableGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_NATIVE_SOURCE") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_NATIVE_SOURCE=1 to run native source gate")
	}
	report, failures := runSelfhostNativeSourceExecutable(t)
	if failures > 0 {
		t.Fatalf("selfhost native source executable failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost native source executable report:\n%s", report)
}

// TestSelfhostNativeSourceExecutableRecipes keeps the native source gate explicit.
func TestSelfhostNativeSourceExecutableRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-native-source-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_NATIVE_SOURCE=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostNativeSourceExecutableGate$")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_ORACLE=1")

	switchGate := justRecipe(content, "selfhost-switch-gate")
	requireRecipeFragment(t, switchGate, "just selfhost-native-source-gate")
}

// runSelfhostNativeSourceExecutable runs the full native-source executable gate.
func runSelfhostNativeSourceExecutable(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	if err := prepareNativeSourceExecutableDir(); err != nil {
		t.Errorf("prepare native source dir: %v", err)
		return "", 1
	}
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("selfhost native source gate requires clang: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendNativeSourceExecutableHeader(&report)
	failures := 0
	build := buildNativeSourceSelfhost(t)
	appendNativeSourceCommandResult(&report, "build", build)
	if build.code != 0 || build.stdout != nativeSourceRunnerPath+"\n" || build.stderr != "" {
		t.Errorf("native source build mismatch")
		failures++
	}
	if failures > 0 {
		return finishNativeSourceExecutableReport(&report, start, failures)
	}
	check := runNativeSourceCommand(t, nativeSourceRunnerPath, "check", "selfhost")
	appendNativeSourceCommandResult(&report, "check", check)
	failures += expectNativeSourceCommand(t, "check selfhost", check, "check: ok\n", "", 0)
	stage := runNativeSourceCommand(t, nativeSourceRunnerPath, "stage", "selfhost")
	appendNativeSourceCommandResult(&report, "stage", stage)
	failures += expectNativeSourceCommand(t, "stage selfhost", stage, bootstrapStageStdout(), "", 0)
	failures += countNativeSourceStageArtifactFailures(t, &report)
	if failures == 0 {
		failures += countNativeSourceRunCaseFailures(t, &report, clang, nativeSourceRunnerPath)
		failures += countNativeSourceTestCaseFailures(t, &report, clang, nativeSourceRunnerPath)
	}
	return finishNativeSourceExecutableReport(&report, start, failures)
}

// prepareNativeSourceExecutableDir creates isolated native-source output paths.
func prepareNativeSourceExecutableDir() error {
	if err := os.RemoveAll("target/selfhost/native-source"); err != nil {
		return err
	}
	for _, dir := range []string{
		"target/selfhost/native-source",
		"target/selfhost/reports",
		"target/selfhost/native-source-cache",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// buildNativeSourceSelfhost compiles the selfhost package into a native executable.
func buildNativeSourceSelfhost(t *testing.T) bootstrapCommandResult {
	t.Helper()
	start := time.Now()
	build := exec.Command(
		"go",
		"run",
		"./cmd/kizu",
		"build",
		"--target",
		"native",
		"--libc",
		"on",
		"--runtime",
		"hosted",
		"--emit",
		"exe",
		"--linker",
		"clang",
		"-o",
		nativeSourceRunnerPath,
		"selfhost",
	)
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR=target/selfhost/native-source-cache")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	build.Stdout = &stdout
	build.Stderr = &stderr
	err := build.Run()
	return bootstrapCommandResult{
		name:    "build native-source selfhost",
		command: strings.Join(build.Args, " "),
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// runNativeSourceCommand runs one command through the native-source selfhost binary.
func runNativeSourceCommand(
	t *testing.T,
	exePath string,
	args ...string,
) bootstrapCommandResult {
	t.Helper()
	start := time.Now()
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Errorf("resolve %s: %v", exePath, err)
	}
	run := exec.Command(absExe, args...)
	run.Env = append(os.Environ(), "KIZU_CACHE_DIR=target/selfhost/native-source-cache")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	return bootstrapCommandResult{
		name:    strings.Join(args, " "),
		command: absExe + " " + strings.Join(args, " "),
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// expectNativeSourceCommand checks a native-source command result.
func expectNativeSourceCommand(
	t *testing.T,
	name string,
	result bootstrapCommandResult,
	stdout string,
	stderr string,
	code int,
) int {
	t.Helper()
	if result.code != code || result.stdout != stdout || result.stderr != stderr {
		t.Errorf("native source %s mismatch", name)
		return 1
	}
	return 0
}

// countNativeSourceStageArtifactFailures checks source-built stage artifacts exist.
func countNativeSourceStageArtifactFailures(t *testing.T, report *strings.Builder) int {
	t.Helper()
	failures := 0
	for _, path := range bootstrapArtifactFiles {
		fullPath := filepath.Join("target/selfhost", path)
		bytes, err := fileSize(fullPath)
		if err != nil {
			t.Errorf("native source stage artifact missing %s: %v", fullPath, err)
			failures++
			continue
		}
		fmt.Fprintf(report, "stage.artifact.%s.bytes %d\n", path, bytes)
	}
	return failures
}

// countNativeSourceRunCaseFailures emits, links, and executes run artifacts.
func countNativeSourceRunCaseFailures(
	t *testing.T,
	report *strings.Builder,
	clang string,
	runner string,
) int {
	t.Helper()
	if err := prepareRunParityDir(); err != nil {
		t.Errorf("prepare native source run dir: %v", err)
		return 1
	}
	failures := 0
	for _, item := range nativeSourceRunCases() {
		result, caseFailures := runNativeSourceRunCase(t, clang, runner, item)
		failures += caseFailures
		appendRunParityResult(report, item, result)
	}
	return failures
}

// nativeSourceRunCases returns representative checked-AST run lowering cases.
func nativeSourceRunCases() []runParityCase {
	return []runParityCase{
		{
			name:         "run_hello",
			command:      "run",
			fixture:      "selfhost/tests/cli/run_hello.kizu",
			exitCode:     0,
			stdoutGolden: "selfhost/tests/cli/golden/run_hello.stdout",
			stderrGolden: "selfhost/tests/cli/golden/run_hello.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "run_hello",
		},
		{
			name:         "run_return",
			command:      "run",
			fixture:      "selfhost/tests/cli/run_return.kizu",
			exitCode:     64,
			stdoutGolden: "selfhost/tests/cli/golden/run_hello.stderr",
			stderrGolden: "selfhost/tests/cli/golden/usage.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "-",
		},
		{
			name:         "run_local_string",
			command:      "run",
			fixture:      "selfhost/tests/cli/run_local_string.kizu",
			exitCode:     0,
			stdoutGolden: "selfhost/tests/cli/golden/run_print_custom.stdout",
			stderrGolden: "selfhost/tests/cli/golden/run_hello.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "run_local_string",
		},
		{
			name:         "run_two_local_strings",
			command:      "run",
			fixture:      "selfhost/tests/cli/run_two_local_strings.kizu",
			exitCode:     0,
			stdoutGolden: "selfhost/tests/cli/golden/run_two_local_strings.stdout",
			stderrGolden: "selfhost/tests/cli/golden/run_hello.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "run_two_local_strings",
		},
	}
}

// runNativeSourceRunCase checks one source-built run artifact.
func runNativeSourceRunCase(
	t *testing.T,
	clang string,
	runner string,
	item runParityCase,
) (runParityResult, int) {
	t.Helper()
	result := runParityResult{compiler: runNativeSourceCommand(t, runner, item.command, item.fixture)}
	expectedOut, expectedErr, err := readRunParityGoldens(item)
	if err != nil {
		t.Errorf("read native source run goldens for %s: %v", item.name, err)
		return result, 1
	}
	if result.compiler.code != 0 {
		return result, compareRunCompilerResult(t, item, result.compiler, expectedOut, expectedErr) +
			countUnexpectedRunArtifacts(t, item)
	}
	if result.compiler.stdout != "" || result.compiler.stderr != "" {
		t.Errorf("native source run %s compiler output mismatch", item.name)
		return result, 1
	}
	failures := fillNativeSourceArtifactResult(t, "run", item, &result)
	if failures > 0 {
		return result, failures
	}
	if err := linkRunParityExecutableWithHost(
		clang,
		result.llPath,
		nativeSourceHostRuntimePath,
		result.exePath,
	); err != nil {
		t.Errorf("link native source run %s: %v", item.name, err)
		return result, failures + 1
	}
	result.program = runRunParityExecutable(t, result.exePath)
	failures += compareRunProgramResult(t, item, result, expectedOut, expectedErr)
	return result, failures
}

// countNativeSourceTestCaseFailures emits, links, and executes test artifacts.
func countNativeSourceTestCaseFailures(
	t *testing.T,
	report *strings.Builder,
	clang string,
	runner string,
) int {
	t.Helper()
	if err := prepareTestParityDir(); err != nil {
		t.Errorf("prepare native source test dir: %v", err)
		return 1
	}
	failures := 0
	for _, item := range nativeSourceTestCases() {
		result, caseFailures := runNativeSourceTestCase(t, clang, runner, item)
		failures += caseFailures
		appendTestParityResult(report, item, result)
	}
	return failures
}

// nativeSourceTestCases returns representative checked-AST test lowering cases.
func nativeSourceTestCases() []runParityCase {
	return []runParityCase{
		{
			name:         "test_expect_ok",
			command:      "test",
			fixture:      "selfhost/tests/cli/test_expect_ok.kizu",
			exitCode:     0,
			stdoutGolden: "selfhost/tests/cli/golden/test_expect_ok.stdout",
			stderrGolden: "selfhost/tests/cli/golden/test_expect_ok.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "test_expect_ok",
		},
		{
			name:         "test_expect_failure",
			command:      "test",
			fixture:      "selfhost/tests/cli/test_expect_failure.kizu",
			exitCode:     1,
			stdoutGolden: "selfhost/tests/cli/golden/test_expect_failure.stdout",
			stderrGolden: "selfhost/tests/cli/golden/test_expect_failure.stderr",
			artifactMode: "hosted-artifact",
			artifactStem: "test_expect_failure",
		},
	}
}

// runNativeSourceTestCase checks one source-built test artifact.
func runNativeSourceTestCase(
	t *testing.T,
	clang string,
	runner string,
	item runParityCase,
) (runParityResult, int) {
	t.Helper()
	result := runParityResult{compiler: runNativeSourceCommand(t, runner, item.command, item.fixture)}
	expectedOut, expectedErr, err := readRunParityGoldens(item)
	if err != nil {
		t.Errorf("read native source test goldens for %s: %v", item.name, err)
		return result, 1
	}
	if result.compiler.code != 0 || result.compiler.stdout != "" || result.compiler.stderr != "" {
		t.Errorf("native source test %s compiler output mismatch", item.name)
		return result, 1
	}
	failures := fillNativeSourceArtifactResult(t, "test", item, &result)
	if failures > 0 {
		return result, failures
	}
	if err := linkTestParityExecutableWithHost(
		clang,
		result.llPath,
		nativeSourceHostRuntimePath,
		result.exePath,
	); err != nil {
		t.Errorf("link native source test %s: %v", item.name, err)
		return result, failures + 1
	}
	result.program = runRunParityExecutable(t, result.exePath)
	failures += compareTestProgramResult(t, item, result, expectedOut, expectedErr)
	return result, failures
}

// fillNativeSourceArtifactResult records artifact paths, sizes, and metadata status.
func fillNativeSourceArtifactResult(
	t *testing.T,
	command string,
	item runParityCase,
	result *runParityResult,
) int {
	t.Helper()
	result.llPath = filepath.Join("target/selfhost", command, item.artifactStem+".ll")
	result.metadataPath = result.llPath + ".meta"
	result.exePath = filepath.Join("target/selfhost", command, item.name)
	var err error
	result.llBytes, err = fileSize(result.llPath)
	if err != nil {
		t.Errorf("native source %s %s artifact missing: %v", command, item.name, err)
		return 1
	}
	result.metadataBytes, err = fileSize(result.metadataPath)
	if err != nil {
		t.Errorf("native source %s %s metadata missing: %v", command, item.name, err)
		return 1
	}
	return countNativeSourceExecutableMetadataFailures(t, command, result.metadataPath)
}

// countNativeSourceExecutableMetadataFailures validates source-path metadata.
func countNativeSourceExecutableMetadataFailures(
	t *testing.T,
	command string,
	metadataPath string,
) int {
	t.Helper()
	bytes, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Errorf("read native source metadata %s: %v", metadataPath, err)
		return 1
	}
	content := string(bytes)
	failures := 0
	for _, fragment := range []string{
		"runtime " + nativeSourceHostRuntimePath + "\n",
		nativeSourceExecutableLoweringMetadata,
		"go.cmd-kizu-fallback none\n",
		"artifact_mode hosted-artifact\n",
	} {
		if !strings.Contains(content, fragment) {
			t.Errorf("native source %s metadata missing %q:\n%s", command, fragment, content)
			failures++
		}
	}
	if strings.Contains(content, "runtime target/selfhost/stage2/selfhost.host.ll\n") {
		t.Errorf("native source %s metadata still points at stage2 runtime:\n%s", command, content)
		failures++
	}
	return failures
}

// appendNativeSourceExecutableHeader writes durable gate metadata.
func appendNativeSourceExecutableHeader(out *strings.Builder) {
	fmt.Fprintf(out, "kizu-selfhost-native-source-executable-v0\n")
	fmt.Fprintf(out, "issue #752\n")
	fmt.Fprintf(out, "runner %s\n", nativeSourceRunnerPath)
	fmt.Fprintf(out, "host-runtime %s\n", nativeSourceHostRuntimePath)
	fmt.Fprintf(out, "validation.path native-source-checked-ast-executable\n")
	fmt.Fprintf(out, "source.compiler go-native-lowering-explicit\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
}

// appendNativeSourceCommandResult records one command result in the gate report.
func appendNativeSourceCommandResult(
	out *strings.Builder,
	label string,
	result bootstrapCommandResult,
) {
	fmt.Fprintf(out, "%s.command %s\n", label, result.command)
	fmt.Fprintf(out, "%s.exit %d\n", label, result.code)
	fmt.Fprintf(out, "%s.stdout.sha256 %s\n", label, textFingerprint(result.stdout))
	fmt.Fprintf(out, "%s.stderr.sha256 %s\n", label, textFingerprint(result.stderr))
}

// finishNativeSourceExecutableReport writes the native-source report.
func finishNativeSourceExecutableReport(
	report *strings.Builder,
	start time.Time,
	failures int,
) (string, int) {
	fmt.Fprintf(report, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(report, "comparison.status pass\n")
	} else {
		fmt.Fprintf(report, "comparison.status fail\n")
	}
	text := report.String()
	if err := os.WriteFile(
		"target/selfhost/reports/native-source-executable.txt",
		[]byte(text),
		0o644,
	); err != nil {
		return text, failures + 1
	}
	return text, failures
}
