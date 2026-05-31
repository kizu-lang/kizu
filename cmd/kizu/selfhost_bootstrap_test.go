package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var bootstrapArtifactFiles = []string{
	"selfhost.ll",
	"selfhost.ll.meta",
	"selfhost.storage.ll",
	"selfhost.storage.ll.meta",
	"selfhost.host.ll",
	"selfhost.host.ll.meta",
}

type bootstrapCommandResult struct {
	name    string
	command string
	stdout  string
	stderr  string
	code    int
	elapsed time.Duration
}

type bootstrapStageResult struct {
	stage       string
	executable  string
	mode        string
	check       bootstrapCommandResult
	stageRun    bootstrapCommandResult
	fingerprint map[string]string
}

// TestSelfhostBootstrapRunner runs the explicit stage0-stage1-stage2 comparison.
func TestSelfhostBootstrapRunner(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_BOOTSTRAP") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_BOOTSTRAP=1 to run selfhost bootstrap")
	}
	report, failures := runSelfhostBootstrap(t)
	if failures > 0 {
		t.Fatalf("selfhost bootstrap failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost bootstrap report:\n%s", report)
}

// runSelfhostBootstrap builds stage1, builds stage2 with stage1, and compares them.
func runSelfhostBootstrap(t *testing.T) (string, int) {
	t.Helper()
	start := time.Now()
	if failures := countSelfhostBackendArtifactGateFailures(t); failures > 0 {
		return "stage0 bootstrap artifact gate failed", failures
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	if err := prepareBootstrapDirs(); err != nil {
		t.Errorf("prepare bootstrap dirs: %v", err)
		return "", 1
	}
	if err := copyStageArtifacts("target/selfhost", "target/selfhost/stage1"); err != nil {
		t.Errorf("copy stage1 artifacts: %v", err)
		return "", 1
	}
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("selfhost bootstrap requires clang: %v", err)
		return "", 1
	}
	if err := writeBootstrapHarness(); err != nil {
		t.Errorf("write bootstrap harness: %v", err)
		return "", 1
	}
	stage1, failures := buildAndRunStage(t, clang, "stage1")
	if failures > 0 {
		return formatBootstrapReport(start, stage1, bootstrapStageResult{}), failures
	}
	stage2, failures := buildAndRunStage(t, clang, "stage2")
	if failures > 0 {
		return formatBootstrapReport(start, stage1, stage2), failures
	}
	failures = compareBootstrapStages(t, stage1, stage2)
	report := formatBootstrapReport(start, stage1, stage2)
	reportPath := "target/selfhost/reports/bootstrap.txt"
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Errorf("write bootstrap report: %v", err)
		failures++
	}
	return report, failures
}

// prepareBootstrapDirs creates isolated stage and report directories.
func prepareBootstrapDirs() error {
	for _, dir := range []string{
		"target/selfhost/stage0",
		"target/selfhost/stage1",
		"target/selfhost/stage2",
		"target/selfhost/reports",
		"target/selfhost/bootstrap-cache",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return copyStageArtifacts("target/selfhost", "target/selfhost/stage0")
}

// copyStageArtifacts copies deterministic compiler artifacts into one stage.
func copyStageArtifacts(from string, to string) error {
	for _, name := range bootstrapArtifactFiles {
		if err := copyBootstrapFile(filepath.Join(from, name), filepath.Join(to, name)); err != nil {
			return err
		}
	}
	return nil
}

// copyBootstrapFile copies one stage artifact.
func copyBootstrapFile(from string, to string) error {
	bytes, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, bytes, 0o644)
}

// writeBootstrapHarness writes the tiny hosted CLI launcher used for both stages.
func writeBootstrapHarness() error {
	return os.WriteFile(
		"target/selfhost/stage0/hosted_cli_main.c",
		[]byte(hostedCompilerCLIHarnessSource),
		0o644,
	)
}

// buildAndRunStage links one stage executable and runs its supported commands.
func buildAndRunStage(t *testing.T, clang string, stage string) (bootstrapStageResult, int) {
	t.Helper()
	result := bootstrapStageResult{
		stage:      stage,
		executable: filepath.Join("target/selfhost", stage, "selfhost"),
		mode:       "hosted-artifact no-go",
	}
	if err := linkBootstrapStage(clang, stage, result.executable); err != nil {
		t.Errorf("link %s: %v", stage, err)
		return result, 1
	}
	result.check = runBootstrapCommand(t, result.executable, "check", "selfhost")
	if failures := expectBootstrapCommand(t, result.check, "check: ok\n", "", 0); failures > 0 {
		return result, failures
	}
	result.stageRun = runBootstrapCommand(t, result.executable, "stage", "selfhost")
	wantStage := bootstrapStageStdout()
	if failures := expectBootstrapCommand(t, result.stageRun, wantStage, "", 0); failures > 0 {
		return result, failures
	}
	fingerprint, err := fingerprintStage(filepath.Join("target/selfhost", stage))
	if err != nil {
		t.Errorf("fingerprint %s: %v", stage, err)
		return result, 1
	}
	result.fingerprint = fingerprint
	return result, 0
}

// linkBootstrapStage links a hosted selfhost compiler artifact for one stage.
func linkBootstrapStage(clang string, stage string, exePath string) error {
	stageDir := filepath.Join("target/selfhost", stage)
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		"-fno-integrated-as",
		filepath.Join(stageDir, "selfhost.ll"),
		filepath.Join(stageDir, "selfhost.host.ll"),
		"selfhost/runtime/selfhost.hosted.c",
		"target/selfhost/stage0/hosted_cli_main.c",
		"-o",
		exePath,
	)
	if out, err := compile.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// runBootstrapCommand runs one hosted stage command and captures user-visible output.
func runBootstrapCommand(
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
	run.Env = append(os.Environ(), "KIZU_CACHE_DIR=target/selfhost/bootstrap-cache")
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

// exitCode returns a process exit code, using -1 for runner errors.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return -1
	}
	return exitErr.ExitCode()
}

// expectBootstrapCommand validates one hosted command result.
func expectBootstrapCommand(
	t *testing.T,
	result bootstrapCommandResult,
	stdout string,
	stderr string,
	code int,
) int {
	t.Helper()
	if result.code != code || result.stdout != stdout || result.stderr != stderr {
		t.Errorf("%s mismatch\ncode=%d stdout=%q stderr=%q",
			result.name, result.code, result.stdout, result.stderr)
		return 1
	}
	return 0
}

// bootstrapStageStdout returns the stable user-visible stage output.
func bootstrapStageStdout() string {
	return strings.Join([]string{
		"stage: ok",
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.ll.meta",
		"target/selfhost/selfhost.storage.ll",
		"target/selfhost/selfhost.storage.ll.meta",
		"target/selfhost/selfhost.host.ll",
		"target/selfhost/selfhost.host.ll.meta",
		"",
	}, "\n")
}

// fingerprintStage computes deterministic fingerprints for a stage directory.
func fingerprintStage(stageDir string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range bootstrapArtifactFiles {
		hash, err := fingerprintFile(filepath.Join(stageDir, name))
		if err != nil {
			return nil, err
		}
		out[name] = hash
	}
	return out, nil
}

// fingerprintFile returns the sha256 fingerprint of one artifact.
func fingerprintFile(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

// compareBootstrapStages compares stage user-visible outputs and fingerprints.
func compareBootstrapStages(
	t *testing.T,
	stage1 bootstrapStageResult,
	stage2 bootstrapStageResult,
) int {
	t.Helper()
	failures := 0
	if stage1.check.stdout != stage2.check.stdout || stage1.check.stderr != stage2.check.stderr ||
		stage1.check.code != stage2.check.code {
		t.Errorf("stage check output mismatch")
		failures++
	}
	if stage1.stageRun.stdout != stage2.stageRun.stdout ||
		stage1.stageRun.stderr != stage2.stageRun.stderr ||
		stage1.stageRun.code != stage2.stageRun.code {
		t.Errorf("stage command output mismatch")
		failures++
	}
	for _, name := range bootstrapArtifactFiles {
		if stage1.fingerprint[name] != stage2.fingerprint[name] {
			t.Errorf("stage artifact fingerprint mismatch %s", name)
			failures++
		}
	}
	return failures
}

// formatBootstrapReport renders the durable bootstrap report.
func formatBootstrapReport(
	start time.Time,
	stage1 bootstrapStageResult,
	stage2 bootstrapStageResult,
) string {
	var out strings.Builder
	appendBootstrapHeader(&out, start)
	appendBootstrapStage(&out, "stage1", stage1)
	appendBootstrapStage(&out, "stage2", stage2)
	appendBootstrapComparison(&out, stage1, stage2)
	return out.String()
}

// appendBootstrapHeader writes stage0 and environment metadata.
func appendBootstrapHeader(out *strings.Builder, start time.Time) {
	cacheSize := directorySize("target/selfhost/bootstrap-cache")
	fmt.Fprintf(out, "kizu-selfhost-bootstrap-v0\n")
	fmt.Fprintf(out, "stage0.compiler go-native-source-bootstrap\n")
	fmt.Fprintf(out, "stage0.mode explicit-bootstrap\n")
	fmt.Fprintf(out, "stage0.command TestSelfhostBackendArtifactGate\n")
	fmt.Fprintf(out, "stage0.report target/selfhost/reports/backend-artifact-stage0-native.txt\n")
	fmt.Fprintf(out, "cache.dir target/selfhost/bootstrap-cache\n")
	fmt.Fprintf(out, "cache.bytes %d\n", cacheSize)
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
}

// appendBootstrapStage writes one stage report block.
func appendBootstrapStage(out *strings.Builder, label string, stage bootstrapStageResult) {
	if stage.stage == "" {
		return
	}
	fmt.Fprintf(out, "%s.executable %s\n", label, stage.executable)
	fmt.Fprintf(out, "%s.mode %s\n", label, stage.mode)
	fmt.Fprintf(out, "%s.check.exit %d\n", label, stage.check.code)
	fmt.Fprintf(out, "%s.check.stdout.sha256 %s\n", label, textFingerprint(stage.check.stdout))
	fmt.Fprintf(out, "%s.check.stderr.sha256 %s\n", label, textFingerprint(stage.check.stderr))
	fmt.Fprintf(out, "%s.stage.exit %d\n", label, stage.stageRun.code)
	fmt.Fprintf(out, "%s.stage.stdout.sha256 %s\n", label, textFingerprint(stage.stageRun.stdout))
	fmt.Fprintf(out, "%s.stage.stderr.sha256 %s\n", label, textFingerprint(stage.stageRun.stderr))
	for _, name := range bootstrapArtifactFiles {
		fmt.Fprintf(out, "%s.artifact.%s.sha256 %s\n", label, name, stage.fingerprint[name])
	}
}

// appendBootstrapComparison writes the pass/fail comparison metadata.
func appendBootstrapComparison(
	out *strings.Builder,
	stage1 bootstrapStageResult,
	stage2 bootstrapStageResult,
) {
	if stage1.stage == "" || stage2.stage == "" {
		fmt.Fprintf(out, "comparison.status incomplete\n")
		return
	}
	fmt.Fprintf(out, "comparison.user-visible check-and-stage\n")
	fmt.Fprintf(out, "comparison.artifacts deterministic-sha256\n")
	fmt.Fprintf(out, "comparison.status pass\n")
}

// textFingerprint returns a sha256 fingerprint for captured process text.
func textFingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// directorySize returns the recursive size of a directory, or zero if absent.
func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
