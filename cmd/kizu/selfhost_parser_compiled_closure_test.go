package main

import (
	"strings"
	"testing"
)

// parserCompiledClosureSeeds lists the roots the parser compiled closure seeds its BFS with: the
// standalone TokenKind predicates, postfix roots, and the real parser consumers currently pinned
// on the native compiled path.
func parserCompiledClosureSeeds() []string {
	return []string{
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
		"is_gt_token",
		"is_comma_token",
		"is_arrow_token",
		"is_left_paren_token",
		"is_right_paren_token",
		"is_double_token_byte",
		"is_type_apply_start",
		"is_struct_literal_start",
		"parse_bool",
		"parse_int",
		"parse_string",
		// issue 1157 C2 real parser consumer: exercises ast.add_var_with_doc from the
		// compiled parser path without pulling the full declaration parser.
		"name_node_with_doc",
		// issue 1157 PJ synthetic fixture: a '-> !ParseNode' helper seeded (in-degree
		// zero) to exercise the site-955 cursor-constructor call, the ParseNode
		// error-union return, and the ParseNode value type on the compiled parser path.
		"synth_parse_node_sig",
		// issue 1157 worker-3 grammar-loop fixture: synth_postfix_loop carries the
		// parse_postfix_expr 'while true' value-loop CFG (reassigning through the lowered
		// synth_parse_node_sig leaf), exercising the GrammarLoop lowering ahead of the real
		// parse_primary consumer.
		"synth_postfix_loop",
		// issue 1157 worker-2 real parser consumer: parse_primary passes parse_int /
		// parse_string call results into parse_postfix_expr, pinning let-try nested Call
		// argument hoisting.
		"parse_primary",
		// expect_ident exercises !Token ok-wrap/error arms.
		"expect_ident",
	}
}

// parserCompiledClosureForbiddenFragments lists every handwritten cluster append
// (qualified name, mangled symbol, per-member params_spec literal) that must be
// gone now the cluster is derived through the shared BFS. The seven helpers the BFS
// reaches through body callees (is_lt_token, is_single_token_byte,
// is_namespace_path_span, is_struct_literal_type_span, is_name_byte, is_upper_byte,
// is_double_colon_at) are pinned too: they are emitted by the closure, never by a
// handwritten append.
func parserCompiledClosureForbiddenFragments() []string {
	return []string{
		"\"std::kizu::parser::is_double_colon\"",
		"\"std::kizu::parser::is_eof_token\"",
		"\"std::kizu::parser::is_left_brace_token\"",
		"\"std::kizu::parser::is_right_brace_token\"",
		"\"std::kizu::parser::is_block_close_token\"",
		"\"std::kizu::parser::is_ident_kind\"",
		"\"std::kizu::parser::is_postfix_start\"",
		"\"std::kizu::parser::is_call_close_token\"",
		"\"std::kizu::parser::is_pub_token\"",
		"\"std::kizu::parser::is_comptime_token\"",
		"\"std::kizu::parser::is_decl_item_separator\"",
		"\"std::kizu::parser::is_lt_token\"",
		"\"std::kizu::parser::is_gt_token\"",
		"\"std::kizu::parser::is_comma_token\"",
		"\"std::kizu::parser::is_arrow_token\"",
		"\"std::kizu::parser::is_left_paren_token\"",
		"\"std::kizu::parser::is_right_paren_token\"",
		"\"std::kizu::parser::is_single_token_byte\"",
		"\"std::kizu::parser::is_double_token_byte\"",
		"\"std::kizu::parser::is_double_colon_at\"",
		"\"std::kizu::parser::is_name_byte\"",
		"\"std::kizu::parser::is_upper_byte\"",
		"\"std::kizu::parser::is_namespace_path_span\"",
		"\"std::kizu::parser::is_struct_literal_type_span\"",
		"\"std::kizu::parser::is_type_apply_start\"",
		"\"std::kizu::parser::is_struct_literal_start\"",
		"\"kizu_kizu__parser_is_double_colon\"",
		"\"kizu_kizu__parser_is_type_apply_start\"",
		"\"kizu_kizu__parser_is_struct_literal_start\"",
		"\"%kizu.slice.u8 text;%kizu.kizu.lexer.token token;i8 byte\"",
		"\"%kizu.slice.u8 source;%kizu.kizu.ast.span span\"",
		"\"%kizu.kizu.ast.ast ast;%kizu.slice.u8 text;%kizu.kizu.parser.parse_node left\"",
	}
}

// TestSelfhostParserCompiledClosureDerivedFromSharedBFS pins that the
// std::kizu::parser predicate / byte / span helper cluster is emitted through the
// shared compiled closure BFS instead of a handwritten append_compiled_function_auto
// per member. The closure seeds the BFS over the "std::kizu::parser::" prefix,
// routes through the shared member builder and emitter, and derives each member's
// params_spec from signature facts. The forbidden fragments pin that the handwritten
// cluster appends and their literal symbols / params_spec strings are gone, so
// nothing keeps a per-helper table.
func TestSelfhostParserCompiledClosureDerivedFromSharedBFS(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentReachableCompiledClosure(
		t,
		cli,
		"fn append_kizu_parser_reachable_compiled_functions(",
		"std::kizu::parser::",
		false,
		parserCompiledClosureSeeds(),
	)

	assertSharedCompiledClosurePath(t, cli)
	assertNoPerComponentCompiledClosureHelpers(t, cli)

	// The append_functions entry point delegates to the shared walk rather than
	// re-listing the cluster members.
	delegation := "try append_kizu_parser_reachable_compiled_functions(out, lookup_index, ir_bytes);"
	if !strings.Contains(cli, delegation) {
		t.Fatalf("append_functions missing shared parser closure delegation")
	}

	for _, fragment := range parserCompiledClosureForbiddenFragments() {
		if strings.Contains(cli, fragment) {
			t.Fatalf("parser compiled cluster keeps hand-written fragment %q", fragment)
		}
	}
}

// TestSelfhostParserCompiledClosureExternalAccessorAllowPolicy pins the explicit,
// narrow external-callee allow policy the std::kizu::parser closure relies on. The
// shared callee collector keeps its IR-fact gate and consults
// compiled_external_accessor_allowed, which under the std::kizu::parser:: prefix
// admits only the single std::kizu::ast read accessor spelled as a method call
// (ast.get). No other cross-component callee is admitted under that prefix, so the
// BFS neither re-emits nor walks the separately-compiled accessor.
func TestSelfhostParserCompiledClosureExternalAccessorAllowPolicy(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentCompiledCalleeFactGate(t, cli)

	callee := selfhostKizuFunctionBody(t, cli, "fn collect_component_compiled_callee(")
	if !strings.Contains(callee, "if compiled_external_accessor_allowed(prefix, callee) {") {
		t.Fatalf("collect_component_compiled_callee missing external accessor allow check")
	}

	allow := selfhostKizuFunctionBody(t, cli, "fn compiled_external_accessor_allowed(")
	parserPrefix := "if std::mem::equal_bytes(prefix, \"std::kizu::parser::\") {"
	required := []string{
		parserPrefix,
		"if std::mem::equal_bytes(callee, \"ast.get\") {",
		"return true;",
		"return false;",
	}
	for _, fragment := range required {
		if !strings.Contains(allow, fragment) {
			t.Fatalf("compiled_external_accessor_allowed missing %q", fragment)
		}
	}

	// The parser prefix admits exactly one accessor: the selfhost::ast traversal
	// spellings tree.get / tree.child_at must not leak into the parser branch, and
	// no std::kizu::* / selfhost::* callee is admitted by fallback.
	astGuard := "if !std::mem::equal_bytes(prefix, \"selfhost::ast::\") {"
	parserBranch := allow[strings.Index(allow, parserPrefix):]
	if idx := strings.Index(parserBranch, astGuard); idx >= 0 {
		parserBranch = parserBranch[:idx]
	}
	for _, leaked := range []string{"tree.get", "tree.child_at"} {
		if strings.Contains(parserBranch, leaked) {
			t.Fatalf("parser external accessor branch leaks %q", leaked)
		}
	}
}

// TestSelfhostParserCompiledParamsSpecDerivedFromSignatures keeps the
// std::kizu::parser closure tied to function-signature-param facts: the ABI mapper
// learns the std::kizu::ast Span value-type spelling (borrowed as
// &std::kizu::ast::Span, stripped to the fully qualified form) and the bare
// ParseNode cursor spelling the parser module references unqualified, so
// append_params_spec can derive each member's params_spec without a handwritten
// per-helper table.
func TestSelfhostParserCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	required := []string{
		"std::mem::equal_bytes(kizu_type, \"std::kizu::ast::Span\")",
		"\"%kizu.kizu.ast.span\"",
		"std::mem::equal_bytes(kizu_type, \"ParseNode\")",
		"\"%kizu.kizu.parser.parse_node\"",
	}
	for _, fragment := range required {
		if !strings.Contains(abi, fragment) {
			t.Fatalf("compiled ABI params mapper missing %q", fragment)
		}
	}
}
