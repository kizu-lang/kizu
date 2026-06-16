package main

import (
	"strings"
	"testing"
)

var parserClosureSeeds = []string{
	"is_double_colon",
	"is_eof_token",
	"is_left_brace_token",
	"is_right_brace_token",
	"is_block_close_token",
	"is_ident_kind",
	"is_postfix_start",
	"is_call_close_token",
	"is_pub_token",
	"is_comptime_token",
	"is_decl_item_separator",
	"is_lt_token",
	"is_gt_token",
	"is_comma_token",
	"is_arrow_token",
	"is_left_paren_token",
	"is_right_paren_token",
	"is_single_token_byte",
	"is_double_token_byte",
	"is_type_apply_start",
	"is_struct_literal_start",
	"parse_bool",
	"parse_int",
	"parse_string",
	// issue 1157 C2 real parser consumer: exercises ast.add_var_with_doc without
	// pulling the full declaration parser.
	"name_node_with_doc",
	// issue 1157 PR-PE synthetic fixture: a '-> !ParseNode' helper seeded to exercise the
	// ParseNode error-union return + ParseNode value type on the parser closure path.
	"synth_parse_node_sig",
	// issue 1157 worker-3 grammar-loop fixture: synth_postfix_loop carries the parse_postfix_expr
	// 'while true' value-loop CFG (reassigning through the lowered synth_parse_node_sig leaf), so it
	// exercises the GrammarLoop lowering ahead of the full parse_primary chain. expect_ident
	// exercises !Token ok-wrap and error arms.
	"synth_postfix_loop",
	"expect_ident",
}

var parserClosurePrivateHelpers = []string{
	"is_double_colon_at",
	"is_name_byte",
	"is_upper_byte",
	"is_namespace_path_span",
	"is_struct_literal_type_span",
}

// TestSelfhostParserClosureUsesComponentCatalog keeps std::kizu::parser helper
// body selection on the component catalog and shared body-call closure.
func TestSelfhostParserClosureUsesComponentCatalog(t *testing.T) {
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	stdParser := readSelfhostFile(t, "../../std/src/kizu/parser.kizu")
	factsBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn append_kizu_parser_function_facts(",
	)
	helperBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn append_kizu_parser_closure_helper_body(",
	)
	roleBody := selfhostKizuFunctionBody(t, executableFunctions, "fn kizu_parser_closure_role(")
	policyBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn collect_catalog_closure_external_callee_allowed(",
	)

	assertParserClosureSeeds(t, factsBody)
	assertParserClosureSharedBody(t, helperBody)
	assertParserClosureRoles(t, roleBody)
	assertParserExternalAccessorPolicy(t, policyBody)
	assertParserPrivateHelpersReachedByBFS(t, factsBody, stdParser)
	assertParserStdlibSymbolFactsRemain(t, factsBody)
}

// assertParserClosureSeeds pins the parser closure seeds and old body list removal.
func assertParserClosureSeeds(t *testing.T, factsBody string) {
	t.Helper()
	for _, fragment := range []string{
		"component_function_catalog::collect_from_ast(",
		"\"std::kizu::parser\"",
		"while index < pending.len()",
		"closure_index_seen(&emitted, function_index)",
		"append_kizu_parser_closure_helper_body(",
	} {
		if !strings.Contains(factsBody, fragment) {
			t.Fatalf("std parser closure seed path missing %q", fragment)
		}
	}
	if strings.Count(factsBody, "append_kizu_parser_closure_seed(") != len(parserClosureSeeds) {
		t.Fatal("std parser closure seed count changed")
	}
	for _, seed := range parserClosureSeeds {
		fragment := "append_kizu_parser_closure_seed(&var pending, &catalog, \"" + seed + "\")"
		if !strings.Contains(factsBody, fragment) {
			t.Fatalf("std parser closure seed missing %q", fragment)
		}
	}
	for _, helper := range parserClosurePrivateHelpers {
		fragment := "append_kizu_parser_closure_seed(&var pending, &catalog, \"" + helper + "\")"
		if strings.Contains(factsBody, fragment) {
			t.Fatalf("std parser private helper is still hand-seeded: %s", helper)
		}
	}
	if strings.Contains(factsBody, "append_selected_helper_body(") {
		t.Fatal("std parser function facts keep hand-written helper body selection")
	}
}

// assertParserClosureSharedBody pins the shared catalog body emitter and callee walker.
func assertParserClosureSharedBody(t *testing.T, helperBody string) {
	t.Helper()
	for _, fragment := range []string{
		"function_signature::append_catalog(",
		"executable_body::append_catalog_helper_body_ir(",
		"kizu_parser_closure_role(local_name)",
		"collect_catalog_closure_direct_callees(",
		"\"std::kizu::parser::\"",
		"\"std parser closure: unsupported call form\"",
		"\"std parser closure: unsupported qualified callee\"",
	} {
		if !strings.Contains(helperBody, fragment) {
			t.Fatalf("std parser closure body missing %q", fragment)
		}
	}
}

// assertParserClosureRoles keeps BFS-discovered helpers on their old body roles.
func assertParserClosureRoles(t *testing.T, roleBody string) {
	t.Helper()
	for _, fragment := range []string{
		"is_double_colon_at",
		"is_name_byte",
		"is_upper_byte",
		"\"checked-parser-byte\"",
		"is_namespace_path_span",
		"is_struct_literal_type_span",
		"\"checked-parser-span-predicate\"",
		"is_type_apply_start",
		"is_struct_literal_start",
		"\"checked-parser-postfix-start\"",
		"\"checked-parser-predicate\"",
	} {
		if !strings.Contains(roleBody, fragment) {
			t.Fatalf("std parser closure role mapping missing %q", fragment)
		}
	}
}

// assertParserExternalAccessorPolicy pins the exact parser-only external callee.
func assertParserExternalAccessorPolicy(t *testing.T, policyBody string) {
	t.Helper()
	for _, fragment := range []string{
		"std::mem::equal_bytes(qualified_prefix, \"std::kizu::parser::\")",
		"std::mem::equal_bytes(callee_text, \"ast.get\")",
	} {
		if !strings.Contains(policyBody, fragment) {
			t.Fatalf("std parser external accessor policy missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"std::mem::starts_with(callee_text, \"std::kizu::\")",
		"std::mem::starts_with(callee_text, \"selfhost::\")",
	} {
		if strings.Contains(policyBody, fragment) {
			t.Fatalf("std parser external accessor policy is too broad: %q", fragment)
		}
	}
}

// assertParserPrivateHelpersReachedByBFS proves private helpers are reached from seeds.
func assertParserPrivateHelpersReachedByBFS(t *testing.T, factsBody string, stdParser string) {
	t.Helper()
	checks := [][]string{
		{"fn is_type_apply_start(", "std::kizu::parser::is_namespace_path_span("},
		{"fn is_struct_literal_start(", "std::kizu::parser::is_struct_literal_type_span("},
		{"fn is_namespace_path_span(", "std::kizu::parser::is_name_byte("},
		{"fn is_namespace_path_span(", "std::kizu::parser::is_double_colon_at("},
		{"fn is_struct_literal_type_span(", "std::kizu::parser::is_name_byte("},
		{"fn is_struct_literal_type_span(", "std::kizu::parser::is_double_colon_at("},
		{"fn is_struct_literal_type_span(", "std::kizu::parser::is_upper_byte("},
		{"fn is_name_byte(", "std::kizu::parser::is_upper_byte("},
	}
	for _, check := range checks {
		body := selfhostKizuFunctionBody(t, stdParser, check[0])
		if !strings.Contains(body, check[1]) {
			t.Fatalf("%s does not reach private helper %q", check[0], check[1])
		}
	}
	for _, helper := range parserClosurePrivateHelpers {
		if !strings.Contains(factsBody, "std::kizu::parser::"+helper) {
			t.Fatalf("std parser symbol facts no longer mention private helper %s", helper)
		}
	}
	for _, fragment := range []string{
		"lexer::tokenize",
		"std::kizu::parser::parse_program",
		"parse_program(",
	} {
		if strings.Contains(factsBody, fragment) {
			t.Fatalf("std parser helper closure expands too far via %q", fragment)
		}
	}
}

// assertParserStdlibSymbolFactsRemain rejects deleting parser symbol facts here.
func assertParserStdlibSymbolFactsRemain(t *testing.T, factsBody string) {
	t.Helper()
	for _, fragment := range []string{
		"append_stdlib_symbol_fact_arg(",
		"append_stdlib_symbol_fact_args2(",
		"append_stdlib_symbol_fact_args3(",
		"\"std::kizu::parser::is_double_colon_at\"",
		"\"std::kizu::parser::is_namespace_path_span\"",
		"\"std::kizu::parser::is_struct_literal_type_span\"",
		"\"std::kizu::parser::is_type_apply_start\"",
		"\"std::kizu::parser::is_struct_literal_start\"",
	} {
		if !strings.Contains(factsBody, fragment) {
			t.Fatalf("std parser symbol fact path missing %q", fragment)
		}
	}
}
