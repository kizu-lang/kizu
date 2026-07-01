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

const (
	stage0BackendArtifactRunnerPath = "target/selfhost/stage0-native/selfhost"
	stage0BackendArtifactCacheDir   = "target/selfhost/stage0-native-cache"
	stage0BackendArtifactReportPath = "target/selfhost/reports/backend-artifact-stage0-native.txt"
	stage0StageProfilePath          = "target/selfhost/reports/stage0-stage-profile.txt"
	stage0BackendProfilePath        = "target/selfhost/reports/stage0-backend-profile.txt"
)

var backendArtifactContractInventory = []string{
	"contract.report artifact-paths-and-byte-counts",
	"contract.textual-llvm required-runtime-cli-executable-fragments",
	"contract.textual-llvm forbids-fixed-cli-fixture-paths",
	"contract.textual-llvm forbids-source-shape-dispatch",
	"contract.metadata selfhost-checked-package-no-go-fallback",
	"contract.runtime-storage textual-metadata-link-smoke",
	"contract.host-capability textual-metadata-link-smoke",
	"contract.hosted-cli link-and-smoke",
}

// runSelfhostBackendArtifactGate builds stage0 natively and stages backend artifacts.
func runSelfhostBackendArtifactGate(t *testing.T) (string, error) {
	t.Helper()
	report, failures := runStage0BackendArtifactGate(t)
	if failures > 0 {
		return report, fmt.Errorf("stage0 native backend artifact failures=%d", failures)
	}
	return report, nil
}

// runStage0BackendArtifactGate uses Go only as the explicit stage0 bootstrap.
func runStage0BackendArtifactGate(t *testing.T) (string, int) {
	t.Helper()
	restoreCWD, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restoreCWD()
	if err := prepareStage0BackendArtifactDir(); err != nil {
		t.Errorf("prepare stage0 backend artifact dir: %v", err)
		return "", 1
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Errorf("stage0 native backend artifact gate requires clang: %v", err)
		return "", 1
	}

	start := time.Now()
	var report strings.Builder
	appendStage0BackendArtifactHeader(&report)
	failures := 0
	failures += runStage0BackendArtifactBuild(t, &report)
	if failures == 0 {
		failures += runStage0BackendArtifactCommand(
			t,
			&report,
			"stage0.check",
			[]string{"check", "selfhost"},
			"check: ok\n",
		)
	}
	if failures == 0 {
		failures += runStage0SeedStageStep(t, &report)
	}
	if failures == 0 {
		failures += appendSelfhostBackendArtifactReport(t, &report)
	}
	return finishStage0BackendArtifactReport(&report, start, failures)
}

// runStage0BackendArtifactBuild compiles the explicit stage0 native selfhost.
func runStage0BackendArtifactBuild(t *testing.T, report *strings.Builder) int {
	t.Helper()
	build := buildNativeSelfhost(t, nativeSelfhostBuildConfig{
		name:       "stage0 backend-artifact selfhost",
		outputPath: stage0BackendArtifactRunnerPath,
		cacheDir:   stage0BackendArtifactCacheDir,
	})
	appendNativeSourceCommandResult(report, "stage0.build", build)
	return expectNativeSourceCommand(
		t,
		"stage0 build selfhost",
		build,
		stage0BackendArtifactRunnerPath+"\n",
		"",
		0,
	)
}

// runStage0BackendArtifactCommand runs one command through the stage0 executable.
func runStage0BackendArtifactCommand(
	t *testing.T,
	report *strings.Builder,
	label string,
	args []string,
	wantStdout string,
) int {
	t.Helper()
	return runStage0BackendArtifactCommandWithEnv(t, report, label, nil, args, wantStdout)
}

// runStage0BackendArtifactCommandWithEnv runs one stage0 command with opt-in diagnostics.
func runStage0BackendArtifactCommandWithEnv(
	t *testing.T,
	report *strings.Builder,
	label string,
	extraEnv []string,
	args []string,
	wantStdout string,
) int {
	t.Helper()
	result := runNativeSelfhostCommandWithEnv(
		t,
		stage0BackendArtifactRunnerPath,
		stage0BackendArtifactCacheDir,
		extraEnv,
		args...,
	)
	appendNativeSourceCommandResult(report, label, result)
	return expectNativeSourceCommand(t, strings.Join(args, " "), result, wantStdout, "", 0)
}

// prepareStage0BackendArtifactDir creates a clean explicit stage0 bootstrap area.
func prepareStage0BackendArtifactDir() error {
	if err := os.RemoveAll("target/selfhost"); err != nil {
		return err
	}
	for _, dir := range []string{
		"target/selfhost/stage0-native",
		"target/selfhost/stage0-native-cache",
		"target/selfhost/reports",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// appendStage0BackendArtifactHeader records the explicit bootstrap boundary.
func appendStage0BackendArtifactHeader(out *strings.Builder) {
	fmt.Fprintf(out, "kizu-selfhost-backend-artifact-stage0-native-v0\n")
	fmt.Fprintf(out, "stage0.compiler go-native-source-bootstrap\n")
	fmt.Fprintf(out, "stage0.mode explicit-bootstrap\n")
	fmt.Fprintf(out, "stage0.runner %s\n", stage0BackendArtifactRunnerPath)
	fmt.Fprintf(out, "stage0.cache %s\n", stage0BackendArtifactCacheDir)
	fmt.Fprintf(out, "stage0.stage.profile.path %s\n", stage0StageProfilePath)
	fmt.Fprintf(out, "stage0.backend.profile.path %s\n", stage0BackendProfilePath)
	fmt.Fprintf(out, "validation.path stage0-native-stage-selfhost\n")
	fmt.Fprintf(out, "interpreter.backend-artifact-gate none\n")
	fmt.Fprintf(out, "go.production none\n")
	fmt.Fprintf(out, "go.cmd-kizu-fallback none\n")
	appendBackendArtifactContractInventory(out)
}

// appendBackendArtifactContractInventory lists the legacy gate responsibilities.
func appendBackendArtifactContractInventory(out *strings.Builder) {
	for _, line := range backendArtifactContractInventory {
		fmt.Fprintf(out, "%s\n", line)
	}
}

// finishStage0BackendArtifactReport writes the stage0 native artifact report.
func finishStage0BackendArtifactReport(
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
	if err := os.MkdirAll(filepath.Dir(stage0BackendArtifactReportPath), 0o755); err != nil {
		return text, failures + 1
	}
	if err := os.WriteFile(stage0BackendArtifactReportPath, []byte(text), 0o644); err != nil {
		return text, failures + 1
	}
	return text, failures
}
