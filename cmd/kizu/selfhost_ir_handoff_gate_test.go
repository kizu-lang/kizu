package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostIRHandoffGate executes the selfhost ownership-to-IR handoff smoke.
func TestSelfhostIRHandoffGate(t *testing.T) {
	if failures := countSelfhostIRHandoffGateFailures(t); failures > 0 {
		t.Fatalf("selfhost IR handoff gate failures=%d", failures)
	}
}

// countSelfhostIRHandoffGateFailures returns failures for handoff summary logging.
func countSelfhostIRHandoffGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostIRHandoffGate(t)
	if err != nil {
		t.Errorf("IR handoff gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"ir-handoff-blocks\n",
		"ir-handoff-entry-points\n1\n",
		"ir-handoff-diagnostics\n0\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("IR handoff gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return 0
}

// runSelfhostIRHandoffGate loads the selfhost package and runs its IR handoff smoke.
func runSelfhostIRHandoffGate(t *testing.T) (string, error) {
	t.Helper()
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
	err = interp.New(&out).RunEntry(program, "selfhost::ir::handoff_gate")
	return out.String(), err
}
