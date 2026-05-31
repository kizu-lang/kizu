package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostFormatOracleOutput = `format-cases
12
`

// TestSelfhostFormatGate executes the Kizu-owned formatter oracle entry.
func TestSelfhostFormatGate(t *testing.T) {
	if failures := countSelfhostFormatGateFailures(t); failures > 0 {
		t.Fatalf("selfhost format gate failures=%d", failures)
	}
}

// countSelfhostFormatGateFailures returns failures for oracle summary logging.
func countSelfhostFormatGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostFormatGate(t)
	if err != nil {
		t.Errorf("format gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostFormatOracleOutput {
		t.Errorf("format gate output mismatch\nwant:\n%sgot:\n%s", selfhostFormatOracleOutput, out)
		return 1
	}
	return 0
}

// runSelfhostFormatGate loads the selfhost package and runs its formatter oracle.
func runSelfhostFormatGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::format_oracle::gate")
	return out.String(), err
}
