package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostIRArtifactGate executes the selfhost IR artifact emission smoke.
func TestSelfhostIRArtifactGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countWithIsolatedSelfhostTarget(
		t,
		func() int { return countSelfhostIRArtifactGateFailures(t) },
	); failures > 0 {
		t.Fatalf("selfhost IR artifact gate failures=%d", failures)
	}
}

// countSelfhostIRArtifactGateFailures returns failures for artifact summary logging.
func countSelfhostIRArtifactGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostIRArtifactGate(t)
	if err != nil {
		t.Errorf("IR artifact gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"ir-artifact-path\ntarget/selfhost/selfhost.ir\n",
		"ir-manifest-path\ntarget/selfhost/selfhost.ir.manifest\n",
		"backend-artifact-bytes\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("IR artifact gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return countSelfhostIRArtifactFileFailures(t)
}

// countSelfhostIRArtifactFileFailures checks emitted files contain deterministic headers.
func countSelfhostIRArtifactFileFailures(t *testing.T) int {
	t.Helper()
	irBytes, err := os.ReadFile("../../target/selfhost/selfhost.ir")
	if err != nil {
		t.Errorf("read IR artifact: %v", err)
		return 1
	}
	manifestBytes, err := os.ReadFile("../../target/selfhost/selfhost.ir.manifest")
	if err != nil {
		t.Errorf("read IR manifest: %v", err)
		return 1
	}
	irContent := string(irBytes)
	if !strings.Contains(irContent, "kizu-ir-v0\npackage selfhost\n") {
		t.Errorf("IR artifact missing deterministic header:\n%s", irBytes)
		return 1
	}
	for _, fragment := range requiredSelfhostIRContractFragments() {
		if !strings.Contains(irContent, fragment) {
			t.Errorf("IR artifact missing contract fragment %q:\n%s", fragment, irBytes)
			return 1
		}
	}
	if !strings.Contains(string(manifestBytes), "kizu-ir-shape-v0\n") {
		t.Errorf("IR manifest missing deterministic header:\n%s", manifestBytes)
		return 1
	}
	if !strings.Contains(string(manifestBytes), "external std::fs::write_file\n") {
		t.Errorf("IR manifest missing fs write capability:\n%s", manifestBytes)
		return 1
	}
	return 0
}

// requiredSelfhostIRContractFragments returns facts the hosted backend requires.
func requiredSelfhostIRContractFragments() []string {
	return []string{
		"ir-contract selfhost-checked-package-v1\n",
		"module selfhost\n",
		"entry selfhost::cli_main\n",
		"checked-entry selfhost::cli_main\n",
		"hosted-entry @kizu_selfhost__cli_main\n",
		"hosted-smoke @kizu_selfhost__smoke\n",
		"frontend-executable-lowering checked-ast-bounded\n",
		"hosted-executable-lowering executable-result-bounded\n",
		"hosted-executable-main-scan leading-functions\n",
		"checked-nodes ",
		"checked-resources ",
		"checked-borrows ",
		"checked-diagnostics 0\n",
	}
}

// runSelfhostIRArtifactGate loads the selfhost package and runs its artifact smoke.
func runSelfhostIRArtifactGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::artifact_gate")
	return out.String(), err
}
