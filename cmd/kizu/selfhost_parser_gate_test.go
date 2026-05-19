package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostParserOracleOutput = `parser-tokens
23
parser-declarations
2
parser-diagnostics
0
`

// TestSelfhostParserGate executes the Kizu-owned parser oracle entry.
func TestSelfhostParserGate(t *testing.T) {
	if failures := countSelfhostParserGateFailures(t); failures > 0 {
		t.Fatalf("selfhost parser gate failures=%d", failures)
	}
}

// countSelfhostParserGateFailures returns failures for oracle summary logging.
func countSelfhostParserGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostParserGate(t)
	if err != nil {
		t.Errorf("parser gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostParserOracleOutput {
		t.Errorf("parser gate output mismatch\nwant:\n%sgot:\n%s", selfhostParserOracleOutput, out)
		return 1
	}
	return 0
}

// runSelfhostParserGate loads the selfhost package and runs its parser oracle.
func runSelfhostParserGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::parser_oracle::gate")
	return out.String(), err
}
