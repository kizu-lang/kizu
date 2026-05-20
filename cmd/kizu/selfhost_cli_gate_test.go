package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostCLIGate executes the minimum Kizu-owned selfhost CLI contract.
func TestSelfhostCLIGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countSelfhostCLIGateFailures(t); failures > 0 {
		t.Fatalf("selfhost CLI gate failures=%d", failures)
	}
}

// countSelfhostCLIGateFailures checks supported CLI commands and stage artifacts.
func countSelfhostCLIGateFailures(t *testing.T) int {
	t.Helper()
	failures := 0
	stageOut, err := runSelfhostCLIContractGate(t)
	if err != nil {
		t.Errorf("selfhost CLI contract failed: %v\n%s", err, stageOut)
		failures++
	} else {
		failures += countSelfhostCLICheckOutputFailures(t, stageOut)
		failures += countSelfhostCLIStageOutputFailures(t, stageOut)
		failures += countSelfhostCLIArtifactPresenceFailures(t)
	}
	return failures
}

// countSelfhostCLICheckOutputFailures validates user-visible check output.
func countSelfhostCLICheckOutputFailures(t *testing.T, out string) int {
	t.Helper()
	required := "check: ok\nexit-code\n0\n"
	if !strings.Contains(out, required) {
		t.Errorf("selfhost check CLI output missing %q:\n%s", required, out)
		return 1
	}
	return 0
}

// countSelfhostCLIStageOutputFailures validates user-visible stage output.
func countSelfhostCLIStageOutputFailures(t *testing.T, out string) int {
	t.Helper()
	required := strings.Join([]string{
		"stage: ok",
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.ll.meta",
		"target/selfhost/selfhost.storage.ll",
		"target/selfhost/selfhost.storage.ll.meta",
		"target/selfhost/selfhost.host.ll",
		"target/selfhost/selfhost.host.ll.meta",
		"exit-code",
		"0",
		"",
	}, "\n")
	if !strings.Contains(out, required) {
		t.Errorf("selfhost stage CLI output missing %q:\n%s", required, out)
		return 1
	}
	return 0
}

// countSelfhostCLIArtifactPresenceFailures checks the stage command wrote outputs.
func countSelfhostCLIArtifactPresenceFailures(t *testing.T) int {
	t.Helper()
	paths := []string{
		"../../target/selfhost/selfhost.ll",
		"../../target/selfhost/selfhost.ll.meta",
		"../../target/selfhost/selfhost.storage.ll",
		"../../target/selfhost/selfhost.storage.ll.meta",
		"../../target/selfhost/selfhost.host.ll",
		"../../target/selfhost/selfhost.host.ll.meta",
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("selfhost CLI artifact missing %s: %v", path, err)
			return 1
		}
		if info.Size() == 0 {
			t.Errorf("selfhost CLI artifact is empty: %s", path)
			return 1
		}
	}
	return 0
}

// runSelfhostCLIContractGate runs the one-pass selfhost CLI contract entry.
func runSelfhostCLIContractGate(t *testing.T) (string, error) {
	t.Helper()
	if err := os.RemoveAll("../../target/selfhost"); err != nil {
		return "", err
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		return "", err
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::cli_contract_gate")
	return out.String(), err
}
