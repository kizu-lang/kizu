package main

import (
	"strings"
	"testing"
)

// codegenClosureSeeds lists the public codegen entrypoints plus the one documented
// stage2 ABI foundation root that replace the old handwritten body list.
func codegenClosureSeeds() []string {
	return []string{
		"lower_run_program",
		"lower_run_parse_result",
		"lower_run_ast",
		"main_print_program",
		"unsupported_program",
		"program_supported",
		"lowered_program_supported",
		"require_main_print",
		"require_lowered_main_print",
		"stdout_payload",
		"fs_read_program_supported",
		"i64_call_program_supported",
		"return_void_program_supported",
		"try_void_program_supported",
		"loop_i64_program_supported",
		"metadata_for_program",
		"metadata_line",
		"empty_int_env",
		"lower_code_expr",
	}
}

// TestSelfhostCodegenClosureUsesComponentCatalog keeps selfhost::ir::codegen
// IR body-fact selection on component-catalog closure roots instead of the old
// handwritten append_selected_function_with_body / append_selected_helper_body list.
func TestSelfhostCodegenClosureUsesComponentCatalog(t *testing.T) {
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	factsBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn append_codegen_function_facts(",
	)
	closureBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn append_codegen_reachable_helper_bodies(",
	)
	helperBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn append_codegen_closure_helper_body(",
	)
	preambleBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"pub fn append_runtime_stdlib_symbol_preamble(",
	)
	roleBody := selfhostKizuFunctionBody(t, executableFunctions, "fn codegen_closure_role(")
	policyBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn collect_catalog_closure_external_callee_allowed(",
	)
	codegenPolicyBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn codegen_closure_external_callee_allowed(",
	)

	assertCodegenClosureFactsPath(t, factsBody)
	assertRuntimeStdlibSymbolPreamble(t, preambleBody)
	assertCodegenClosureSeeds(t, closureBody)
	assertCodegenClosureSharedBody(t, helperBody)
	assertCodegenClosureRoles(t, roleBody)
	assertCodegenClosureExternalPolicy(t, policyBody, codegenPolicyBody)
	assertCodegenPrivateHelpersReachedByBFS(t, closureBody, codegen)
}

// assertCodegenClosureFactsPath pins the top-level codegen fact emitter to the
// component catalog while keeping the non-body facts in place.
func assertCodegenClosureFactsPath(t *testing.T, factsBody string) {
	t.Helper()
	for _, fragment := range []string{
		"component_function_catalog::collect_from_ast(",
		"\"selfhost::ir::codegen\"",
		"append_codegen_reachable_helper_bodies(",
		"append_selected_enum_variant_facts(",
		"append_selected_struct_field_facts(",
		"append_type_llvm_fact(",
		"append_runtime_stdlib_symbol_preamble(out)",
		"\"FsReadBinding\"",
	} {
		if !strings.Contains(factsBody, fragment) {
			t.Fatalf("codegen closure facts path missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"append_selected_function_with_body(",
		"append_selected_helper_body(",
	} {
		if strings.Contains(factsBody, fragment) {
			t.Fatalf("codegen function facts keep hand-written body selection %q", fragment)
		}
	}
}

// assertRuntimeStdlibSymbolPreamble pins the shared stdlib-symbol preamble helper as the single
// source of truth both the production codegen facts path and the format driver lowering gate use:
// it must still emit the std::mem slice helpers and the std::string::String value constructor that
// routes through @kizu_rt_string_new (issue 1165 / 1162).
func assertRuntimeStdlibSymbolPreamble(t *testing.T, preambleBody string) {
	t.Helper()
	for _, fragment := range []string{
		"append_stdlib_symbol_fact(out, \"std::mem::equal_bytes\"",
		"append_stdlib_symbol_fact(out, \"std::mem::len\"",
		"append_stdlib_symbol_fact_arg(",
		"\"std::string::String\"",
		"\"kizu_rt_string_new\"",
		"\"%kizu.owned\"",
	} {
		if !strings.Contains(preambleBody, fragment) {
			t.Fatalf("runtime stdlib-symbol preamble missing %q", fragment)
		}
	}
}

// assertCodegenClosureSeeds keeps the closure rooted in entrypoints and rejects
// directly seeding private helpers, except for the documented empty_int_env ABI root.
func assertCodegenClosureSeeds(t *testing.T, closureBody string) {
	t.Helper()
	if strings.Count(closureBody, "append_codegen_closure_seed(") != len(codegenClosureSeeds()) {
		t.Fatal("codegen closure seed count changed")
	}
	for _, seed := range codegenClosureSeeds() {
		fragment := "append_codegen_closure_seed(&var pending, catalog, \"" + seed + "\")"
		if !strings.Contains(closureBody, fragment) {
			t.Fatalf("codegen closure seed missing %q", fragment)
		}
	}
	for _, helper := range []string{
		"lower_print_call",
		"const_string_value",
		"lowered_main_print_program",
		"main_print_payload",
		"none_value",
		"ast_node_text",
		"lower_run_int_program",
	} {
		fragment := "append_codegen_closure_seed(&var pending, catalog, \"" + helper + "\")"
		if strings.Contains(closureBody, fragment) {
			t.Fatalf("codegen private helper is hand-seeded: %s", helper)
		}
	}
	for _, fragment := range []string{
		"while index < pending.len()",
		"closure_index_seen(&emitted, function_index)",
		"append_codegen_closure_helper_body(",
	} {
		if !strings.Contains(closureBody, fragment) {
			t.Fatalf("codegen closure BFS missing %q", fragment)
		}
	}
	emptyIntEnvSeed := "append_codegen_closure_seed(&var pending, catalog, " +
		"\"empty_int_env\")"
	if !strings.Contains(closureBody, emptyIntEnvSeed) {
		t.Fatal("codegen closure missing empty_int_env stage2 ABI foundation root")
	}
	if !strings.Contains(closureBody, "empty_int_env is a direct stage2 backend root") {
		t.Fatal("empty_int_env root lacks stage2 ABI rationale")
	}
}

// assertCodegenClosureSharedBody pins the shared body emitter and callee walker.
func assertCodegenClosureSharedBody(t *testing.T, helperBody string) {
	t.Helper()
	for _, fragment := range []string{
		"function_signature::append_catalog(",
		"executable_body::append_catalog_helper_body_ir(",
		"codegen_closure_role(local_name)",
		"collect_catalog_closure_direct_callees(",
		"\"selfhost::ir::codegen::\"",
		"\"codegen closure: unsupported call form\"",
		"\"codegen closure: unsupported qualified callee\"",
	} {
		if !strings.Contains(helperBody, fragment) {
			t.Fatalf("codegen closure body missing %q", fragment)
		}
	}
}

// assertCodegenClosureRoles keeps catalog-discovered members on their existing
// body-fact roles.
func assertCodegenClosureRoles(t *testing.T, roleBody string) {
	t.Helper()
	for _, fragment := range []string{
		"ast_node_text",
		"\"checked-text-accessor\"",
		"codegen_closure_builder_role(local_name)",
		"\"checked-run-codegen-builder\"",
		"codegen_closure_consumer_role(local_name)",
		"\"checked-run-codegen-consumer\"",
		"\"checked-run-codegen-lowering\"",
	} {
		if !strings.Contains(roleBody, fragment) {
			t.Fatalf("codegen closure role mapping missing %q", fragment)
		}
	}
}

// assertCodegenClosureExternalPolicy pins the narrow external/boundary callee
// policy the codegen closure needs.
func assertCodegenClosureExternalPolicy(t *testing.T, policyBody string, codegenPolicyBody string) {
	t.Helper()
	for _, fragment := range []string{
		"std::mem::equal_bytes(qualified_prefix, \"selfhost::ir::codegen::\")",
		"return codegen_closure_external_callee_allowed(callee_text);",
	} {
		if !strings.Contains(policyBody, fragment) {
			t.Fatalf("codegen external callee policy missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"std::mem::equal_bytes(callee_text, \"ast.get\")",
		"std::mem::equal_bytes(callee_text, \"ast.child_at\")",
		"std::mem::starts_with(callee_text, \"std::kizu::ast::binary_\")",
		"codegen_closure_binary_op_accessor_allowed(callee_text)",
		"std::mem::equal_bytes(callee_text, \"lower_run_int_program\")",
	} {
		if !strings.Contains(codegenPolicyBody, fragment) {
			t.Fatalf("codegen external callee policy missing %q", fragment)
		}
	}
}

// assertCodegenPrivateHelpersReachedByBFS proves representative private helpers
// are body-call reachable instead of directly seeded.
func assertCodegenPrivateHelpersReachedByBFS(t *testing.T, closureBody string, codegen string) {
	t.Helper()
	for _, helper := range []string{
		"lower_fs_read_block",
		"lower_loop_i64_block",
		"lower_print_call",
		"const_string_value",
		"none_value",
		"ast_node_text",
	} {
		fragment := "append_codegen_closure_seed(&var pending, catalog, \"" + helper + "\")"
		if strings.Contains(closureBody, fragment) {
			t.Fatalf("codegen BFS helper is still directly seeded: %s", helper)
		}
	}
	for _, check := range [][]string{
		{"fn lower_run_ast_block(", "lower_fs_read_block("},
		{"fn lower_run_ast_block(", "lower_loop_i64_block("},
		{"fn lower_print_call(", "print_payload("},
		{"fn build_main_print_program(", "const_string_value("},
		{"fn main_print_payload(", "none_value("},
		{"fn string_literal_span(", "is_payload_supported("},
	} {
		body := selfhostKizuFunctionBody(t, codegen, check[0])
		if !strings.Contains(body, check[1]) {
			t.Fatalf("%s does not reach %q", check[0], check[1])
		}
	}
}
