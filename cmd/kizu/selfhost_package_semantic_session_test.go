package main

import (
	"testing"
)

// TestSelfhostBodyCallInstanceFactsRejectMismatchedOwnership checks that the
// resolver refuses instance facts whose symbol, target or multiplicity does not
// line up. These tapes are well formed, so nothing but the ownership rules
// stands between them and a wrongly bound call.
func TestSelfhostBodyCallInstanceFactsRejectMismatchedOwnership(t *testing.T) {
	for _, entry := range []string{
		"gate_body_call_instance_symbol_mismatch",
		"gate_body_call_instance_duplicate",
		"gate_body_call_instance_target_mismatch",
		"gate_function_instance_symbol_owner_collision",
	} {
		out, err := runSelfhostPackageGate(
			t, "selfhost::backend::compiled_type_resolver::"+entry,
		)
		if err == nil {
			t.Fatalf("%s unexpectedly accepted invalid instance facts\n%s", entry, out)
		}
	}
}
