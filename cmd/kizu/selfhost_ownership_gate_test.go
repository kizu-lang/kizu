package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostOwnershipGate executes the Kizu-owned ownership oracle entry.
func TestSelfhostOwnershipGate(t *testing.T) {
	if failures := countSelfhostOwnershipGateFailures(t); failures > 0 {
		t.Fatalf("selfhost ownership gate failures=%d", failures)
	}
}

// countSelfhostOwnershipGateFailures returns failures for oracle summary logging.
func countSelfhostOwnershipGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostOwnershipGate(t)
	if err != nil {
		t.Errorf("ownership gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"ownership-production-resources\n",
		"ownership-production-checked-nodes\n",
		"ownership-production-borrows\n",
		"ownership-production-errors\n0\n",
		"ownership-resources\n7\n",
		"ownership-borrows\n2\n",
		"ownership-errors\n4\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("ownership gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return 0
}

// runSelfhostOwnershipGate loads the selfhost package and runs its ownership oracle.
func runSelfhostOwnershipGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::ownership_oracle::gate")
	return out.String(), err
}
