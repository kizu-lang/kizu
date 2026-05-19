package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostLexerOracleOutput = `lexer-tokens
11
lexer-bytes
29
lexer-diagnostics
0
`

// TestSelfhostLexerGate executes the Kizu-owned lexer oracle entry.
func TestSelfhostLexerGate(t *testing.T) {
	if failures := countSelfhostLexerGateFailures(t); failures > 0 {
		t.Fatalf("selfhost lexer gate failures=%d", failures)
	}
}

// countSelfhostLexerGateFailures returns failures for oracle summary logging.
func countSelfhostLexerGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostLexerGate(t)
	if err != nil {
		t.Errorf("lexer gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostLexerOracleOutput {
		t.Errorf("lexer gate output mismatch\nwant:\n%sgot:\n%s", selfhostLexerOracleOutput, out)
		return 1
	}
	return 0
}

// runSelfhostLexerGate loads the selfhost package and runs its lexer oracle.
func runSelfhostLexerGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::lexer_oracle::gate")
	return out.String(), err
}
