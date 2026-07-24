package main

import (
	"strings"
	"testing"
)

func TestSelfhostCompiledRunModuleIsPureComposition(t *testing.T) {
	renderer := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_run_llvm.kizu",
	)
	runtimeDeclarations := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_runtime_declarations.kizu",
	)
	legacyRenderer := readSelfhostFile(
		t, "../../selfhost/src/backend/llvm.kizu",
	)
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	body := selfhostKizuFunctionBody(t, renderer, "pub fn render_module(")
	for _, fragment := range []string{
		"validate_fact_bytes(facts)",
		"ir_index::build(facts)",
		"compiled_canonical_facts::parse_and_validate(",
		"compiled_runtime_declarations::append_foundation_types(",
		"compiled_type_declarations::append_reachable_fact_declarations_indexed(",
		"compiled_runtime_declarations::append_linked_host_externals(",
		"compiled_runtime_declarations::append_linked_storage_externals(",
		"compiled_program_llvm::append_reachable_functions(",
		"compiled_run_entry_llvm::append_run_entry(",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("compiled run renderer missing %q", fragment)
		}
	}
	if count := strings.Count(body, "ir_index::build(facts)"); count != 1 {
		t.Errorf("compiled run renderer index build count = %d, want 1", count)
	}
	if count := strings.Count(
		body, "compiled_canonical_facts::parse_and_validate(",
	); count != 1 {
		t.Errorf("compiled run renderer canonical parse count = %d, want 1", count)
	}
	ordered := []string{
		"compiled_runtime_declarations::append_foundation_types(",
		"compiled_type_declarations::append_reachable_fact_declarations_indexed(",
		"compiled_runtime_declarations::append_linked_host_externals(",
		"compiled_runtime_declarations::append_linked_storage_externals(",
		"compiled_program_llvm::append_reachable_functions(",
		"compiled_run_entry_llvm::append_run_entry(",
	}
	previous := -1
	for _, fragment := range ordered {
		at := strings.Index(body, fragment)
		if at <= previous {
			t.Errorf("compiled run renderer stage %q is out of order", fragment)
		}
		previous = at
	}
	for _, forbidden := range []string{
		"selfhost::backend::llvm",
		"selfhost::backend::cli_llvm",
		"selfhost::ir::code_render",
		"std::fs::",
		"read_file(",
		"write_file(",
		"artifact_path",
	} {
		if strings.Contains(renderer, forbidden) {
			t.Errorf("compiled run renderer contains forbidden legacy or file policy %q", forbidden)
		}
	}
	if strings.Contains(cli, "compiled_run_llvm") {
		t.Fatal("CLI switched to compiled run renderer before the integration slice")
	}

	for _, fragment := range []string{
		"compiled_runtime_declarations::append_foundation_types(out)",
		"compiled_runtime_declarations::append_linked_host_externals(out)",
		"compiled_run_entry_llvm::append_host_declarations(out)",
	} {
		if !strings.Contains(legacyRenderer, fragment) {
			t.Errorf("legacy renderer does not consume shared declaration owner %q", fragment)
		}
	}
	if strings.Contains(
		legacyRenderer, "compiled_runtime_declarations::append_linked_storage_externals(out)",
	) {
		t.Fatal("legacy self-contained renderer declared storage functions it defines")
	}
	for _, forbidden := range []string{
		`"declare %kizu.owned @kizu_rt_mem_page_allocator()"`,
		`"declare %kizu.error.void @kizu_rt_array_append(`,
		`"declare %kizu.slice.u8 @kizu_rt_array_pop_or_panic(`,
		`"declare i64 @kizu_rt_process_exit_code(i64)"`,
		`"%kizu.error.bool = type { i1, i1, %kizu.slice.u8 }"`,
	} {
		if strings.Contains(legacyRenderer, forbidden) {
			t.Errorf("legacy renderer retains duplicate declaration ownership %q", forbidden)
		}
	}
	for _, fragment := range []string{
		`"declare %kizu.owned @kizu_rt_mem_page_allocator()"`,
		`"declare %kizu.error.void @kizu_rt_array_append(%kizu.owned, %kizu.slice.u8)"`,
		`"declare %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned)"`,
	} {
		if !strings.Contains(runtimeDeclarations, fragment) {
			t.Errorf("shared runtime declaration owner missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		`"define `,
		`"@.kizu.rt.`,
		"%kizu.rt.array = type",
		"%kizu.rt.string = type",
	} {
		if strings.Contains(runtimeDeclarations, forbidden) {
			t.Errorf("runtime declaration owner embeds linked implementation %q", forbidden)
		}
	}
}

func TestSelfhostCompiledRunMinimalModuleLLVM(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_run_llvm::minimal_module_gate",
	)
	if err != nil {
		t.Fatalf("compiled run module gate failed: %v\n%s", err, out)
	}
	for _, fragment := range []string{
		"; Kizu compiled run module",
		`source_filename = "entry.kizu"`,
		"%kizu.slice.u8 = type { ptr, i64 }",
		"declare %kizu.owned @kizu_rt_mem_page_allocator()",
		"declare %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned)",
		"define i64 @kizu_app__entry_start()",
		"ret i64 0",
		"define i64 @kizu_run_main()",
		"define i32 @main(i32 %argc, ptr %argv)",
	} {
		if !strings.Contains(out, fragment) {
			t.Errorf("compiled run module output missing %q\n%s", fragment, out)
		}
	}
	for _, forbidden := range []string{
		"define i64 @kizu_rt_array_len(",
		"define %kizu.error.slice.u8 @kizu_rt_array_at(",
		"@.kizu.rt.array_index_out_of_bounds =",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("compiled run module embeds linked runtime implementation %q", forbidden)
		}
	}
	verifyCompiledRunEntryLLVM(t, out)
}
