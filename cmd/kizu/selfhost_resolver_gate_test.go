package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostResolverGate executes the Kizu-owned resolver oracle entry.
func TestSelfhostResolverGate(t *testing.T) {
	requireSelfhostGate(t)
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
	required := []string{
		"resolver-modules\n",
		"resolver-production-symbols\n",
		"resolver-production-diagnostics\n0\n",
		"resolver-symbols\n4\n",
		"resolver-diagnostics\n4\n",
		"resolver-related\n3\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("resolver gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
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
