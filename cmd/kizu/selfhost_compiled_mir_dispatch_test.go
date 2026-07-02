package main

import (
	"strings"
	"testing"
)

// TestSelfhostCompiledMIRGuardsParserOnlyLoopProbes keeps parser-only loop shape
// detectors out of non-parser compiled components.
func TestSelfhostCompiledMIRGuardsParserOnlyLoopProbes(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	body := selfhostKizuFunctionBody(t, lower, "pub fn lower_multi_statement_function(")
	guard := "if is_parser_component {"
	guardIndex := strings.Index(body, guard)
	if guardIndex < 0 {
		t.Fatalf("compiled MIR lowerer missing parser component guard %q", guard)
	}

	for _, probe := range []string{
		"is_grammar_postfix_loop_shape(",
		"is_type_apply_loop_shape(",
		"is_while_match_loop_shape(",
		"is_precedence_loop_shape(",
		"is_dual_cursor_loop_shape(",
		"is_trailing_token_loop_shape(",
		"is_value_cursor_append_loop_shape(",
		"is_guarded_cursor_return_loop_shape(",
	} {
		probeIndex := strings.Index(body, probe)
		if probeIndex < 0 {
			t.Fatalf("compiled MIR lowerer missing parser-only probe %q", probe)
		}
		if probeIndex < guardIndex {
			t.Fatalf("parser-only probe %q is not guarded by the parser component prefix", probe)
		}
	}

	embeddedGuard := "if is_parser_component and try is_embedded_value_cursor_append_loop_shape("
	if !strings.Contains(body, embeddedGuard) {
		t.Fatalf("embedded value-cursor probe missing parser component guard %q", embeddedGuard)
	}

	helper := selfhostKizuFunctionBody(t, lower, "fn compiled_mir_lower_is_parser_component(")
	parserPrefix := `std::mem::starts_with(function_name, "std::kizu::parser::")`
	if !strings.Contains(helper, parserPrefix) {
		t.Fatal("parser component guard must be prefix-limited to std::kizu::parser")
	}
}

// TestSelfhostCompiledMIRUsesPerFunctionStructuralCache keeps hot top-level
// statement and call-argument lookups on the per-function lowering cache.
func TestSelfhostCompiledMIRUsesPerFunctionStructuralCache(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	multi := selfhostKizuFunctionBody(t, lower, "pub fn lower_multi_statement_function(")
	for _, fragment := range []string{
		"try compiled_mir_types::build_fn_node_index(",
		"compiled_mir_types::fn_body_child_sequence_from(",
		"compiled_mir_types::fn_node_kind(",
	} {
		if !strings.Contains(multi, fragment) {
			t.Fatalf("lower_multi_statement_function missing cached lookup %q", fragment)
		}
	}
	if strings.Contains(multi, "let stmt_kind = try ir_contract::body_node_kind_from(") {
		t.Fatal("top-level statement dispatch should use the cached node kind table")
	}

	topLevelHasKind := selfhostKizuFunctionBody(t, lower, "fn top_level_has_statement_kind(")
	for _, fragment := range []string{
		"compiled_mir_types::fn_body_child_sequence_from(",
		"compiled_mir_types::fn_node_kind(",
	} {
		if !strings.Contains(topLevelHasKind, fragment) {
			t.Fatalf("top_level_has_statement_kind missing cached lookup %q", fragment)
		}
	}

	types := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	countArgs := selfhostKizuFunctionBody(t, types, "pub fn count_call_args_cached(")
	cachedArgCount := `return try fn_body_edge_count_from(` +
		`cache, ir_bytes, function_name, call_node, "arg", bs` +
		`);`
	if !strings.Contains(countArgs, cachedArgCount) {
		t.Fatal("cached call-argument count should use one edge-count lookup")
	}
	if strings.Contains(countArgs, "while true") {
		t.Fatal("cached call-argument count should not rescan once per ordinal")
	}
}

// TestSelfhostReachabilityCollectUsesNodeLineIndex keeps the component BFS collector
// from rescanning a function's body-node facts from the body start for every sequence.
func TestSelfhostReachabilityCollectUsesNodeLineIndex(t *testing.T) {
	cliLLVM := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	collect := selfhostKizuFunctionBody(t, cliLLVM, "fn collect_component_compiled_body_callees(")
	for _, fragment := range []string{
		"std::array::Array<i64>",
		"ir_contract::collect_body_node_line_starts(",
		"let line_start = try node_line_starts.get(sequence);",
		"ir_contract::body_node_kind_from(ir_bytes, name, sequence, line_start)",
		"ir_contract::body_call_callee_or_empty_from(",
	} {
		if !strings.Contains(collect, fragment) {
			t.Fatalf("collect_component_compiled_body_callees missing %q", fragment)
		}
	}
	if strings.Contains(collect, "ir_contract::body_node_count_from(") {
		t.Fatal("reachability collect should use the node line index, not a count plus repeated scans")
	}
	if strings.Contains(collect, "sequence, body_start") {
		t.Fatal("reachability collect should not read each node from the function body start")
	}
}
