package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostTypeOracleOutput = `type-modules
31
type-production-symbols
98
type-production-functions
556
type-production-typed-nodes
40369
type-symbols
9
type-typed-nodes
9
type-diagnostics
19
`

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
	if out != selfhostTypeOracleOutput {
		t.Errorf("type gate output mismatch\nwant:\n%sgot:\n%s", selfhostTypeOracleOutput, out)
		return 1
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
