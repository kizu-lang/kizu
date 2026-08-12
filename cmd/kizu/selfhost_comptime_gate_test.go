package main

import "testing"

// TestSelfhostComptimeGate exercises the shared Kizu-owned evaluator directly.
func TestSelfhostComptimeGate(t *testing.T) {
	const entry = "selfhost::comptime_gate::gate"
	out, err := runSelfhostPackageGate(t, entry)
	if err != nil {
		t.Fatalf("comptime gate failed: %v\n%s", err, out)
	}
	if out != "comptime-contract\n" {
		t.Fatalf("comptime gate output = %q", out)
	}
}
