package main

import (
	"strings"
	"testing"
)

// codegenCompiledClosureSeeds lists the in-degree-zero cluster roots the
// selfhost::ir::codegen Program builder / metadata / Value helper closure seeds
// its BFS with: lowered_main_print_program (the const-string main-print builder
// alias), metadata_for_program (the program metadata selector), main_print_payload
// (the program-shape Value reader), and unsupported_program (the empty-program
// sentinel). None of the four is called by another cluster member, and together
// they reach the remaining seven members through their body callees.
func codegenCompiledClosureSeeds() []string {
	return []string{
		"lowered_main_print_program",
		"metadata_for_program",
		"main_print_payload",
		"unsupported_program",
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
	// The params_spec literals that were unique to a removed registration. The
	// shared "%kizu.selfhost.codegen.program program" spelling is not forbidden: it
	// is still carried by other compiled helpers (e.g. lowered_program_supported).
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

	// The append_functions entry point delegates to the shared walk rather than
	// re-listing the cluster members.
	delegation := "try append_codegen_reachable_compiled_functions(out, ir_bytes);"
	if !strings.Contains(cli, delegation) {
		t.Fatalf("append_functions missing shared codegen closure delegation")
	}

	for _, fragment := range codegenCompiledClosureForbiddenFragments() {
		if strings.Contains(cli, fragment) {
			t.Fatalf("codegen compiled cluster keeps hand-written fragment %q", fragment)
		}
	}
}

// TestSelfhostCodegenLoopI64SupportedUsesCompiledAuto keeps the loop-i64
// support predicate on real body lowering instead of restoring the old
// hand-written LLVM renderer.
func TestSelfhostCodegenLoopI64SupportedUsesCompiledAuto(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	required := []string{
		`"selfhost::ir::codegen::loop_i64_run_ast_supported"`,
		`"kizu_selfhost__ir_codegen_loop_i64_run_ast_supported"`,
		`"%kizu.selfhost.codegen.run_ast run_ast"`,
		`"selfhost::ir::codegen::loop_i64_program_supported"`,
		`"kizu_selfhost__ir_codegen_loop_i64_program_supported"`,
		`"%kizu.selfhost.codegen.program program"`,
	}
	for _, fragment := range required {
		if !strings.Contains(cli, fragment) {
			t.Fatalf("loop-i64 support compiled-auto registration missing %q", fragment)
		}
	}

	forbidden := []string{
		"fn append_loop_i64_run_ast_supported_function(",
		"try append_loop_i64_run_ast_supported_function(out, ir_bytes);",
		"fn append_loop_i64_program_supported_function(",
		"try append_loop_i64_program_supported_function(out, ir_bytes);",
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("loop-i64 support keeps hand-written renderer fragment %q", fragment)
		}
	}
}

// TestSelfhostCodegenLoopI64BuildersUseCompiledAuto keeps the loop-i64 RunAst
// and Program builders on compiled lowering instead of the old hand-written LLVM
// emitters.
func TestSelfhostCodegenLoopI64BuildersUseCompiledAuto(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	required := []string{
		`"selfhost::ir::codegen::loop_i64_run_ast"`,
		`"kizu_selfhost__ir_codegen_loop_i64_run_ast"`,
		"\"%kizu.slice.u8 function_name;i64 statement_count;" +
			"%kizu.slice.u8 print_callee;i64 start_value;i64 bound;" +
			"i64 continue_value;i64 break_value\"",
		`"selfhost::ir::codegen::lowered_loop_i64_program"`,
		`"kizu_selfhost__ir_codegen_lowered_loop_i64_program"`,
	}
	for _, fragment := range required {
		if !strings.Contains(cli, fragment) {
			t.Fatalf("loop-i64 compiled-auto builder registration missing %q", fragment)
		}
	}

	forbidden := []string{
		"fn append_loop_i64_run_ast_function(",
		"try append_loop_i64_run_ast_function(out, ir_bytes);",
		"fn append_lowered_loop_i64_program_function(",
		"try append_lowered_loop_i64_program_function(out, ir_bytes);",
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("loop-i64 builder keeps hand-written renderer fragment %q", fragment)
		}
	}
}

// TestSelfhostCodegenCompiledClosureNoExternalAccessorWidening pins that the
// codegen cluster admits no cross-component external accessor. Its only non-local
// callee is the std::mem equal_bytes intrinsic (handled by the shared collector's
// std::mem:: branch), so compiled_external_accessor_allowed must keep its
// prefix-limited parser / selfhost::ast branches and must not grow a
// selfhost::ir::codegen:: branch that would broaden the allow policy.
func TestSelfhostCodegenCompiledClosureNoExternalAccessorWidening(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentCompiledCalleeFactGate(t, cli)

	allow := selfhostKizuFunctionBody(t, cli, "fn compiled_external_accessor_allowed(")
	if strings.Contains(allow, "selfhost::ir::codegen::") {
		t.Fatalf("compiled_external_accessor_allowed grew a codegen branch; " +
			"the cluster needs no external accessor")
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
