package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostSourceOracleOutput = `source-files
100
source-selfhost
84
source-std
15
source-diagnostics
4
source-related
2
`

// TestSelfhostSourceGate executes the Kizu-owned source manager oracle entry.
func TestSelfhostSourceGate(t *testing.T) {
	if failures := countSelfhostSourceGateFailures(t); failures > 0 {
		t.Fatalf("selfhost source gate failures=%d", failures)
	}
}

// countSelfhostSourceGateFailures returns failures for oracle summary logging.
func countSelfhostSourceGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostSourceGate(t)
	if err != nil {
		t.Errorf("source gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostSourceOracleOutput {
		t.Errorf("source gate output mismatch\nwant:\n%sgot:\n%s", selfhostSourceOracleOutput, out)
		return 1
	}
	return 0
}

// runSelfhostSourceGate loads the selfhost package and runs its source oracle.
func runSelfhostSourceGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::source_oracle::gate")
	return out.String(), err
}

// chdirRepoRoot runs filesystem-backed source gates from the repository root.
func chdirRepoRoot() (func(), error) {
	oldWd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir("../.."); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Chdir(oldWd)
	}, nil
}
