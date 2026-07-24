package main

import (
	"strings"
	"testing"
)

// TestSelfhostCodegenRunAstSupportEmissionRemoved keeps the superseded RunAst
// support registrations out of the hosted backend.
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
	bridge := readSelfhostFile(t, "../../selfhost/src/backend/cli_run_i64_codegen_llvm.kizu")

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

	if !strings.Contains(cli, "try cli_run_i64_codegen_llvm::append_functions(out, ir_bytes);") {
		t.Fatal("cli backend should delegate the live parse-int bridge to its owning module")
	}
	for _, fragment := range []string{
		"try append_parse_int_literal_function(out);",
		"fn append_parse_int_literal_function(",
		"@kizu_selfhost__ir_codegen_parse_int_literal(",
	} {
		if !strings.Contains(bridge, fragment) {
			t.Fatalf("live parse-int bridge owner missing %q", fragment)
		}
	}
}
