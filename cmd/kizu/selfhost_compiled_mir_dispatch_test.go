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
