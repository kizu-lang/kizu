package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostCacheGateOutput = `cache-key
755768063
cache-entry
755768063
cache-lookup
755768063
`

// TestSelfhostCacheGate executes the Kizu-owned cache key gate.
func TestSelfhostCacheGate(t *testing.T) {
	if failures := countSelfhostCacheGateFailures(t); failures > 0 {
		t.Fatalf("selfhost cache gate failures=%d", failures)
	}
}

// countSelfhostCacheGateFailures returns failures for oracle summary logging.
func countSelfhostCacheGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostCacheGate(t)
	if err != nil {
		t.Errorf("cache gate failed: %v\n%s", err, out)
		return 1
	}
	if out != selfhostCacheGateOutput {
		t.Errorf("cache gate output mismatch\nwant:\n%sgot:\n%s", selfhostCacheGateOutput, out)
		return 1
	}
	return 0
}

// runSelfhostCacheGate loads the selfhost package and runs its cache gate.
func runSelfhostCacheGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::cache::gate")
	return out.String(), err
}
