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

// TestSelfhostPackageDefinitionConsumerUsesIrIndex keeps the backend on the
// generic package-definition contract. Reachability belongs to the frontend
// package graph; the backend consumes indexed facts and lowers each definition.
func TestSelfhostPackageDefinitionConsumerUsesIrIndex(t *testing.T) {
	programLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	consumer := selfhostKizuFunctionBody(t, programLLVM, "pub fn append_reachable_functions(")
	for _, fragment := range []string{
		`let prefix = "package-dependency ";`,
		"ir_index::first_entry_with_fact_prefix(lookup_index, ir_bytes, prefix)",
		"ir_index::entry_key_starts_with(lookup_index, ir_bytes, entry, prefix)",
		`let name_prefix = "package-definition-name ";`,
		"out, lookup_index, canonical_facts, ir_bytes, name",
	} {
		if !strings.Contains(consumer, fragment) {
			t.Fatalf("generic package dependency consumer missing %q", fragment)
		}
	}
	emit := selfhostKizuFunctionBody(t, programLLVM, "fn emit_numeric_package_definition(")
	if !strings.Contains(emit, "compiled_llvm::append_compiled_function_auto_indexed(") {
		t.Fatal("generic package definition should lower through indexed compiled lowering")
	}
	if strings.Contains(programLLVM, "collect_component_compiled_body_callees") {
		t.Fatal("backend-local component reachability collector should remain removed")
	}
}
