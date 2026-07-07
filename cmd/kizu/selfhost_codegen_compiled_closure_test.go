package main

import (
	"strings"
	"testing"
)

// codegenCompiledClosureSeeds lists the codegen compiled closure roots after the
// #1255 slice4 PR-1 shape contract severance: only the run tape lowering root
// lower_code_module and the code_op_mem_page_allocator opcode accessor remain. The
// hosted per-shape Program builder / metadata roots (lowered_main_print_program /
// metadata_for_program / main_print_payload / unsupported_program) were severed once
// shape emit became closure-excluded. metadata_line stays a direct root: the live
// metadata constant leaf was previously reached via metadata_for_program.
func codegenCompiledClosureSeeds() []string {
	return []string{
		"lower_code_module",
		"code_op_mem_page_allocator",
		"metadata_line",
	}
}

// codegenCompiledClosureForbiddenFragments lists every handwritten cluster append
// (quoted qualified name, quoted mangled symbol, and the per-member params_spec
// literals that were unique to a registration) that must be gone now the cluster is
// derived through the shared BFS. The seven members the BFS reaches through body
// callees (build_main_print_program, const_string_value, const_string_instruction,
// call_instruction, return_void_instruction, metadata_line, none_value) are pinned
// too: they are emitted by the closure, never by a handwritten append. The bare
// mangled symbols for lowered_main_print_program / unsupported_program /
// main_print_payload deliberately stay out of this list because the hand-written
// LLVM renderer still calls them at their stage2 call sites; only the quoted
// registration spelling ("kizu_selfhost__ir_codegen_<name>") is forbidden.
func codegenCompiledClosureForbiddenFragments() []string {
	names := []string{
		"metadata_line",
		"return_void_instruction",
		"const_string_value",
		"const_string_instruction",
		"call_instruction",
		"build_main_print_program",
		"lowered_main_print_program",
		"unsupported_program",
		"none_value",
		"main_print_payload",
		"metadata_for_program",
	}
	fragments := make([]string, 0, len(names)*2+3)
	for _, name := range names {
		fragments = append(fragments, "\"selfhost::ir::codegen::"+name+"\"")
		fragments = append(fragments, "\"kizu_selfhost__ir_codegen_"+name+"\"")
	}
	// The params_spec literals that were unique to a removed registration. The shared
	// "%kizu.selfhost.codegen.program program" spelling is not listed here (it is
	// covered by the program-support severance gate); after #1255 slice4 PR-1 no
	// codegen compiled helper carries it any longer.
	fragments = append(fragments,
		"\"%kizu.slice.u8 payload;i1 from_codegen_lowering\"",
		"\"%kizu.selfhost.codegen.value value\"",
		"\"%kizu.slice.u8 callee;%kizu.selfhost.codegen.value argument\"",
	)
	return fragments
}

// TestSelfhostCodegenCompiledClosureDerivedFromSharedBFS pins that the
// selfhost::ir::codegen Program builder / metadata / Value helper cluster is emitted
// through the shared compiled closure BFS instead of a handwritten
// append_compiled_function_auto per member. The closure seeds the BFS over the
// "selfhost::ir::codegen::" prefix with allow_empty_params set (the parameterless
// metadata_line / return_void_instruction members derive an empty params_spec),
// routes through the shared member builder and emitter, and derives each member's
// params_spec from signature facts. The forbidden fragments pin that the handwritten
// cluster appends and their literal symbols / params_spec strings are gone, so
// nothing keeps a per-helper table.
func TestSelfhostCodegenCompiledClosureDerivedFromSharedBFS(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentReachableCompiledClosure(
		t,
		cli,
		"fn append_codegen_reachable_compiled_functions(",
		"selfhost::ir::codegen::",
		true,
		codegenCompiledClosureSeeds(),
	)

	assertSharedCompiledClosurePath(t, cli)
	assertNoPerComponentCompiledClosureHelpers(t, cli)

	// The append_functions entry point delegates to the profiled shared walk rather than
	// re-listing the cluster members.
	delegation := "try append_codegen_reachable_compiled_functions_profiled("
	if !strings.Contains(cli, delegation) {
		t.Fatalf("append_functions missing shared codegen closure delegation")
	}

	for _, fragment := range codegenCompiledClosureForbiddenFragments() {
		if strings.Contains(cli, fragment) {
			t.Fatalf("codegen compiled cluster keeps hand-written fragment %q", fragment)
		}
	}
}

// TestSelfhostCodegenRunAstSupportEmissionRemoved keeps the old RunAst support
// predicates out of the stage2 emission list now that run dispatch uses the tape
// renderer.
func TestSelfhostCodegenRunAstSupportEmissionRemoved(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	forbiddenRegistrations := []string{
		`"selfhost::ir::codegen::try_void_run_ast_supported"`,
		`"selfhost::ir::codegen::loop_i64_run_ast_supported"`,
		`"kizu_selfhost__ir_codegen_loop_i64_run_ast_supported"`,
		`"%kizu.selfhost.codegen.run_ast run_ast"`,
		`"selfhost::ir::codegen::fs_read_run_ast_supported"`,
		"try append_run_ast_supported_function(out, ir_bytes);",
	}
	for _, fragment := range forbiddenRegistrations {
		if strings.Contains(cli, fragment) {
			t.Fatalf("legacy RunAst support emission remains: %q", fragment)
		}
	}

	// #1255 slice4 PR-1: the loop_i64_program_supported compiled-auto registration
	// was severed once shape emit became closure-excluded; its quoted qualified /
	// mangled spellings and the shared program params_spec literal must now be gone.
	forbidden := []string{
		"fn append_loop_i64_run_ast_supported_function(",
		"try append_loop_i64_run_ast_supported_function(out, ir_bytes);",
		"fn append_loop_i64_program_supported_function(",
		"try append_loop_i64_program_supported_function(out, ir_bytes);",
		`"selfhost::ir::codegen::loop_i64_program_supported"`,
		`"kizu_selfhost__ir_codegen_loop_i64_program_supported"`,
		`"%kizu.selfhost.codegen.program program"`,
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("loop-i64 support keeps hand-written renderer fragment %q", fragment)
		}
	}
}

// TestSelfhostCodegenProgramSupportPredicatesSevered pins that the Program support
// predicates are severed from the stage2 compiled closure (#1255 slice4 PR-1): once
// shape emit became closure-excluded their compiled-auto registrations were removed,
// and their hand-written LLVM renderers must stay gone too (they were never restored).
func TestSelfhostCodegenProgramSupportPredicatesSevered(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	forbidden := []string{
		`"selfhost::ir::codegen::program_supported"`,
		`"kizu_selfhost__ir_codegen_program_supported"`,
		`"selfhost::ir::codegen::i64_call_program_supported"`,
		`"kizu_selfhost__ir_codegen_i64_call_program_supported"`,
		`"selfhost::ir::codegen::return_void_program_supported"`,
		`"kizu_selfhost__ir_codegen_return_void_program_supported"`,
		`"selfhost::ir::codegen::try_void_program_supported"`,
		`"kizu_selfhost__ir_codegen_try_void_program_supported"`,
		"fn append_program_supported_function(",
		"try append_program_supported_function(out, ir_bytes);",
		"fn append_i64_call_program_supported_function(",
		"try append_i64_call_program_supported_function(out, ir_bytes);",
		"fn append_return_void_program_supported_function(",
		"try append_return_void_program_supported_function(out, ir_bytes);",
		"fn append_try_void_program_supported_function(",
		"try append_try_void_program_supported_function(out, ir_bytes);",
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("program support predicate registration/renderer should be severed: %q", fragment)
		}
	}
}

// TestSelfhostCodegenRunAstBuilderEmissionRemoved keeps the old RunAst builder
// registrations out of stage2 while preserving the live parse_int_literal bridge
// used by the tape lowering.
func TestSelfhostCodegenRunAstBuilderEmissionRemoved(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	forbiddenRegistrations := []string{
		`"selfhost::ir::codegen::i64_call_run_ast"`,
		`"selfhost::ir::codegen::return_void_run_ast"`,
		`"selfhost::ir::codegen::try_void_run_ast"`,
		`"selfhost::ir::codegen::loop_i64_run_ast"`,
		`"kizu_selfhost__ir_codegen_loop_i64_run_ast"`,
		`"selfhost::ir::codegen::fs_read_run_ast"`,
		`"selfhost::ir::codegen::lowered_loop_i64_program"`,
		`"kizu_selfhost__ir_codegen_lowered_loop_i64_program"`,
		"try append_lower_run_ast_to_program_function(out, ir_bytes);",
	}
	for _, fragment := range forbiddenRegistrations {
		if strings.Contains(cli, fragment) {
			t.Fatalf("legacy RunAst builder emission remains: %q", fragment)
		}
	}

	required := []string{
		"try cli_run_i64_codegen_llvm::append_functions(out, ir_bytes);",
		`"parse_int_literal"`,
	}
	for _, fragment := range required {
		if !strings.Contains(cli, fragment) {
			t.Fatalf("live parse-int bridge emission missing %q", fragment)
		}
	}
}

// TestSelfhostCodegenCompiledClosureNoExternalAccessorWidening pins the exact
// external-accessor policy of the codegen cluster. The run codegen tape lowering
// references two kinds of separately-emitted definitions: the compiled
// std::kizu::ast operator constant accessors (kizu_kizu__ast_binary_* called by
// code_binary_kind and the short-circuit and/or routing, plus
// kizu_kizu__ast_prefix_not called by the logical-not lowering), and the
// transitional handwritten parse_int_literal define (deleted once the generic
// while lowering carries mid-body scalar assigns). Anything beyond that exact
// allowlist is a policy widening this test rejects.
func TestSelfhostCodegenCompiledClosureNoExternalAccessorWidening(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentCompiledCalleeFactGate(t, cli)

	allow := selfhostKizuFunctionBody(t, cli, "fn compiled_external_accessor_allowed(")
	allowed := []string{
		`"parse_int_literal"`,
		`"std::kizu::ast::binary_add"`,
		`"std::kizu::ast::binary_sub"`,
		`"std::kizu::ast::binary_mul"`,
		`"std::kizu::ast::binary_div"`,
		`"std::kizu::ast::binary_mod"`,
		// The comparison BinaryOp accessors back the run codegen tape's if-condition
		// lowering (code_binary_kind eq/not_eq/lt/lte/gt/gte).
		`"std::kizu::ast::binary_eq"`,
		`"std::kizu::ast::binary_not_eq"`,
		`"std::kizu::ast::binary_lt"`,
		`"std::kizu::ast::binary_lte"`,
		`"std::kizu::ast::binary_gt"`,
		`"std::kizu::ast::binary_gte"`,
		// The logical accessors back the tape's short-circuit and/or lowering
		// (SC_BEGIN/SC_END records) and prefix_not its logical-not lowering.
		`"std::kizu::ast::binary_and"`,
		`"std::kizu::ast::binary_or"`,
		`"std::kizu::ast::prefix_not"`,
	}
	for _, fragment := range allowed {
		if !strings.Contains(allow, fragment) {
			t.Fatalf("compiled_external_accessor_allowed lost the codegen allowlist entry %s", fragment)
		}
	}
}

// TestSelfhostCodegenCompiledParamsSpecDerivedFromSignatures keeps the codegen
// closure tied to function-signature-param facts: the strict ABI mapper learns the
// selfhost::ir::codegen Value / Program record-struct spellings (Program borrowed as
// &Program and stripped to the bare form) and the bool flag type (i1 ABI), so
// append_params_spec can derive each member's params_spec without a handwritten
// per-helper table. Instruction is intentionally absent: it is a return type only
// and never appears in a function-signature-param fact.
func TestSelfhostCodegenCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	required := []string{
		"std::mem::equal_bytes(kizu_type, \"bool\")",
		"return \"i1\";",
		"std::mem::equal_bytes(kizu_type, \"Value\")",
		"\"%kizu.selfhost.codegen.value\"",
		"std::mem::equal_bytes(kizu_type, \"Program\")",
		"\"%kizu.selfhost.codegen.program\"",
	}
	for _, fragment := range required {
		if !strings.Contains(abi, fragment) {
			t.Fatalf("compiled ABI params mapper missing %q", fragment)
		}
	}

	if strings.Contains(abi, "kizu_type, \"Instruction\"") {
		t.Fatalf("ABI mapper added an Instruction spelling that never appears in a param fact")
	}
}
