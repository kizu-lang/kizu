package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostTypeGate executes the Kizu-owned type checker oracle entry.
func TestSelfhostTypeGate(t *testing.T) {
	if failures := countSelfhostTypeGateFailures(t); failures > 0 {
		t.Fatalf("selfhost type gate failures=%d", failures)
	}
}

// countSelfhostTypeGateFailures returns failures for oracle summary logging.
func countSelfhostTypeGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostTypeGate(t)
	if err != nil {
		t.Errorf("type gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"type-modules\n",
		"type-production-symbols\n",
		"type-production-functions\n",
		"type-production-typed-nodes\n",
		"type-production-diagnostics\n0\n",
		"type-symbols\n9\n",
		"type-typed-nodes\n9\n",
		"type-diagnostics\n19\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("type gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return 0
}

// runSelfhostTypeGate loads the selfhost package and runs its type checker oracle.
func runSelfhostTypeGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::types_oracle::gate")
	return out.String(), err
}
