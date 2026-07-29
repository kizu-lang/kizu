package main

import (
	"strings"
	"testing"
)

// TestSelfhostCountRangeLoweringResolvesItsTypesAndCallees rejects the former
// function-name/AST-ABI allowlist and pins the exact fact-driven boundary.
func TestSelfhostCountRangeLoweringResolvesItsTypesAndCallees(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	classifier := selfhostKizuFunctionBody(t, source, "pub fn is_count_range_function(")
	for _, forbidden := range []string{
		"selfhost::ast::count_range", "%kizu.kizu.ast.ast", "%kizu.kizu.ast.child_range",
	} {
		if strings.Contains(classifier, forbidden) {
			t.Fatalf("count_range classifier retained type-specific gate %q", forbidden)
		}
	}
	lower := selfhostKizuFunctionBody(t, source, "pub fn lower_count_range_function(")
	for _, required := range []string{
		"compiled_mir_types::resolve_value_kizu_type(",
		"compiled_fact_lookup::lookup_struct_field_exact_indexed(",
		"compiled_mir_types::lower_call_info_for_instance_indexed(",
		"compiled_mir_types::call_info_error_success_llvm(",
		"compiled_canonical_facts::parsed_call_runtime_type_id(",
		"child_info.return_llvm_type",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("count_range lowerer missing fact-derived boundary %q", required)
		}
	}
}
