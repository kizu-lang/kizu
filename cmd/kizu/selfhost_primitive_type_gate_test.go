package main

import "testing"

// TestSelfhostPrimitiveTypeGate exercises generic decimal width decoding and
// rejects malformed, three-digit, and unsupported primitive widths.
func TestSelfhostPrimitiveTypeGate(t *testing.T) {
	const entry = "selfhost::types::primitive_type_gate::gate"
	out, err := runSelfhostPackageGate(t, entry)
	if err != nil {
		t.Fatalf("primitive type gate failed: %v\n%s", err, out)
	}
	if out != "primitive-type-contract\n" {
		t.Fatalf("primitive type gate output = %q", out)
	}
}
