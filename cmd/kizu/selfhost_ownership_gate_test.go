package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostOwnershipOracleOutput = `ownership-resources
7
ownership-borrows
2
ownership-errors
4
`

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
	if out != selfhostOwnershipOracleOutput {
		t.Errorf(
			"ownership gate output mismatch\nwant:\n%sgot:\n%s",
			selfhostOwnershipOracleOutput,
			out,
		)
		return 1
	}
	return 0
}

// runSelfhostOwnershipGate loads the selfhost package and runs its ownership oracle.
func runSelfhostOwnershipGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
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
