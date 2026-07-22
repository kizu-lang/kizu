package main

import "testing"

// TestSelfhostCountRangeClassifierRequiresTwoParams guards the specialized
// count_range lowerer from claiming one-parameter accumulator-shaped loops.
func TestSelfhostCountRangeClassifierRequiresTwoParams(t *testing.T) {
	const entry = "selfhost::backend::compiled_mir_lower::count_range_param_shape_gate"
	out, err := runSelfhostAbiParamsGate(t, entry)
	if err != nil {
		t.Fatalf("count_range param-shape gate failed: %v\n%s", err, out)
	}
	if out != "count-range-param-shape\n" {
		t.Fatalf("count_range param-shape gate output = %q", out)
	}
}
