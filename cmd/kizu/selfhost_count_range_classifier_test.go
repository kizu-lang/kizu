package main

import (
	"strings"
	"testing"
)

// TestSelfhostCountRangeClassifierRequiresExactTarget guards the specialized
// count_range lowerer with its exact function identity and two parameter ABI
// types, without coupling selection to the parameter names.
func TestSelfhostCountRangeClassifierRequiresExactTarget(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	classifier := selfhostKizuFunctionBody(t, source, "pub fn is_count_range_function(")
	targetCheck := strings.Index(
		classifier, "count_range_target_supported(function_name, params_spec)",
	)
	bodyInspection := strings.Index(classifier, "body_child_sequence_from(")
	if targetCheck < 0 || bodyInspection < 0 || targetCheck > bodyInspection {
		t.Fatal("count_range target identity/ABI check must precede body inspection")
	}

	const entry = "selfhost::backend::compiled_mir_lower::count_range_param_shape_gate"
	out, err := runSelfhostAbiParamsGate(t, entry)
	if err != nil {
		t.Fatalf("count_range param-shape gate failed: %v\n%s", err, out)
	}
	if out != "count-range-param-shape\n" {
		t.Fatalf("count_range param-shape gate output = %q", out)
	}
}
