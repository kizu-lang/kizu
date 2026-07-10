package main

import (
	"strings"
	"testing"
)

// codegenClosureSeeds lists the surviving codegen helper-body closure roots after
// the #1255 slice4 PR-1 shape contract severance: only the live metadata leaf plus
// the documented stage2 ABI/backend/tape roots remain. The hosted per-shape Program
// builder / consumer / lowering seeds were removed once shape emit became
// closure-excluded.
func codegenClosureSeeds() []string {
	return []string{
		"metadata_line",
		"empty_int_env",
		"lower_code_module",
		"code_op_array_read_i64",
		"code_op_array_read",
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

// TestSelfhostCodegenTypeClassificationUsesRecords keeps run codegen type
// classification on the central TypeRecord path instead of re-growing local
// string-spelling branches for each container shape.
func TestSelfhostCodegenTypeClassificationUsesRecords(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	typeID := readSelfhostFile(t, "../../selfhost/src/types/type_id.kizu")
	signature := readSelfhostFile(t, "../../selfhost/src/ir/function_signature.kizu")

	assertCodegenTypeRecordClassifier(t, codegen)
	assertTypesTypeIDSource(t, typeID)
	assertSignatureTypeIDFacts(t, signature)
	assertManualSignatureTypeIDFacts(t, executableFunctions)
	assertTypeIDFunctionFacts(t, executableFunctions)
}

// assertCodegenTypeRecordClassifier pins run codegen param/return classification
// to TypeId-derived records instead of local source-spelling parsing.
func assertCodegenTypeRecordClassifier(t *testing.T, codegen string) {
	t.Helper()
	for _, fragment := range []string{
		"import selfhost::types::type_id;",
		"return type_id::type_record_from_node(text, ast, type_node);",
		"type_id::function_return_type_id_from_node(text, ast, return_type)",
		"type_id::function_param_type_id_from_node(text, ast, type_node)",
		"type_id::type_record_from_id(return_type_id)",
		"type_id::type_record_from_id(param_type_id)",
		"fn code_return_kind_from_record(",
		"fn code_param_kind_from_record(",
		"type_record_param_builtin_kind(shape)",
	} {
		if !strings.Contains(codegen, fragment) {
			t.Fatalf("codegen TypeRecord classifier missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"struct TypeShape",
		"struct TypeRecord",
		"fn type_record_from_text(",
		"fn type_generic_parts(",
		"fn read_borrow_referent_kind(",
		"\"std::map::Map<[]u8, i64>\"",
		"\"std::map::Map<[]u8,i64>\"",
		"\"std::map::Map <[]u8, i64>\"",
	} {
		if strings.Contains(codegen, fragment) {
			t.Fatalf("codegen type classifier keeps string-spelling branch %q", fragment)
		}
	}
}

// assertTypesTypeIDSource pins the canonical TypeId source and its only
// temporary spelling adapter.
func assertTypesTypeIDSource(t *testing.T, typeID string) {
	t.Helper()
	for _, fragment := range []string{
		"pub struct TypeId",
		"pub struct TypeRecord",
		"pub struct TypeTable",
		"pub fn function_return_type_id_from_node(",
		"pub fn function_param_type_id_from_node(",
		"pub fn type_record_from_id(",
		"pub fn type_record_from_text(type_text: []u8) -> TypeRecord",
		"fn type_generic_parts(",
		"fn type_record_generic_from_text(",
		"single compatibility adapter",
		"Delete this adapter once every backend caller receives checker-emitted TypeId",
	} {
		if !strings.Contains(typeID, fragment) {
			t.Fatalf("types TypeId source missing %q", fragment)
		}
	}
}

// assertSignatureTypeIDFacts pins AST-derived function signatures to emit
// TypeId facts beside the legacy text facts.
func assertSignatureTypeIDFacts(t *testing.T, signature string) {
	t.Helper()
	for _, fragment := range []string{
		"import selfhost::types::type_id;",
		"function-signature-return-type-id ",
		"function-signature-param-type-id ",
		"type_id::type_record_from_node(text, ast, return_type)",
		"type_id::type_record_from_node(text, ast, type_node)",
	} {
		if !strings.Contains(signature, fragment) {
			t.Fatalf("function signature TypeId fact path missing %q", fragment)
		}
	}
}

// assertManualSignatureTypeIDFacts keeps handwritten signature facts from
// bypassing the TypeId fact stream.
func assertManualSignatureTypeIDFacts(t *testing.T, executableFunctions string) {
	t.Helper()
	for _, fragment := range []string{
		"import selfhost::types::type_id;",
		"fn append_function_signature_return_type_id_fact(",
		"fn append_function_signature_param_type_id_fact(",
		"type_id::type_record_from_text(return_type)",
		"type_id::type_record_from_text(param_type)",
		"function-signature-return-type-id ast.",
	} {
		if !strings.Contains(executableFunctions, fragment) {
			t.Fatalf("manual signature TypeId fact path missing %q", fragment)
		}
	}
}

// assertTypeIDFunctionFacts keeps the canonical TypeId component in the IR
// artifact instead of leaving codegen's TypeRecord calls as unresolved external
// references.
func assertTypeIDFunctionFacts(t *testing.T, executableFunctions string) {
	t.Helper()
	for _, fragment := range []string{
		"var type_id_found = false;",
		"std::mem::equal_bytes(module, \"types/type_id\")",
		"try append_type_id_function_facts(allocator, out, file);",
		"return error(\"missing selfhost type id source\");",
		"fn append_type_id_function_facts(",
		"\"selfhost::types::type_id\"",
		"\"selfhost::types::type_id::TypeRecord\"",
		"\"checked-type-id-classifier\"",
		"fn type_id_closure_external_callee_allowed(",
	} {
		if !strings.Contains(executableFunctions, fragment) {
			t.Fatalf("TypeId fact source missing %q", fragment)
		}
	}
}

// assertCodegenClosureFactsPath pins the top-level codegen fact emitter to the
// component catalog while keeping the non-body facts in place.
func assertCodegenClosureFactsPath(t *testing.T, factsBody string) {
	t.Helper()
	for _, fragment := range []string{
		"component_function_catalog::collect_from_ast(",
		"\"selfhost::ir::codegen\"",
		"append_codegen_reachable_helper_bodies(",
		"append_selected_struct_field_facts(",
		"append_type_llvm_fact(",
		"append_runtime_stdlib_symbol_preamble(out)",
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
	if !strings.Contains(closureBody, "append_codegen_opcode_closure_seeds(&var pending, catalog)") {
		t.Fatal("codegen closure does not seed opcode accessors from the catalog")
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
		"codegen_closure_type_id_callee_allowed(callee_text)",
		"std::mem::starts_with(callee_text, \"std::kizu::ast::binary_\")",
		"codegen_closure_binary_op_accessor_allowed(callee_text)",
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
		"ast_node_text",
		"string_literal_span",
		"is_payload_supported",
	} {
		fragment := "append_codegen_closure_seed(&var pending, catalog, \"" + helper + "\")"
		if strings.Contains(closureBody, fragment) {
			t.Fatalf("codegen BFS helper is still directly seeded: %s", helper)
		}
	}
	for _, check := range [][]string{
		{"fn string_literal_span(", "is_payload_supported("},
		{"fn string_literal_span(", "empty_payload_span("},
	} {
		body := selfhostKizuFunctionBody(t, codegen, check[0])
		if !strings.Contains(body, check[1]) {
			t.Fatalf("%s does not reach %q", check[0], check[1])
		}
	}
}
