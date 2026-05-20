package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostResolverOracleOutput = `resolver-modules
31
resolver-production-symbols
668
resolver-symbols
4
resolver-diagnostics
4
resolver-related
3
`

// TestSelfhostResolverGate executes the Kizu-owned resolver oracle entry.
func TestSelfhostResolverGate(t *testing.T) {
	if failures := countSelfhostResolverGateFailures(t); failures > 0 {
		t.Fatalf("selfhost resolver gate failures=%d", failures)
	}
}

// countSelfhostResolverGateFailures returns failures for oracle summary logging.
func countSelfhostResolverGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostResolverGate(t)
	if err != nil {
		t.Errorf("resolver gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostResolverOracleOutput {
		t.Errorf("resolver gate output mismatch\nwant:\n%sgot:\n%s", selfhostResolverOracleOutput, out)
		return 1
	}
	return 0
}

// runSelfhostResolverGate loads the selfhost package and runs its resolver oracle.
func runSelfhostResolverGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::resolver_oracle::gate")
	return out.String(), err
}
