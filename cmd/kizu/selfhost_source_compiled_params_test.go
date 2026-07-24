package main

import (
	"strings"
	"testing"
)

// TestSelfhostSourceCompiledClosureOwnedBySemanticFacade keeps source ABI
// lowering on the semantic package closure, without a second source-specific
// compiled closure or parameter table.
func TestSelfhostSourceCompiledClosureOwnedBySemanticFacade(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	forbidden := []string{
		"fn append_source_reachable_compiled_functions(",
		"fn source_compiled_params_spec(",
		"\"i64 kind\"",
		"\"%kizu.selfhost.source.source_file file\"",
		"\"%kizu.selfhost.source.source_file file;%kizu.slice.u8 name\"",
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("source compiled params keeps hand-written fragment %q", fragment)
		}
	}

	abiForbidden := []string{
		"std::mem::equal_bytes(kizu_type, \"SourceKind\")",
		"std::mem::equal_bytes(kizu_type, \"SourceFile\")",
		"std::mem::equal_bytes(kizu_type, \"source::SourceFile\")",
		"std::mem::equal_bytes(kizu_type, \"selfhost::source::SourceFile\")",
		"std::mem::equal_bytes(kizu_type, \"std::kizu::ast::SourceFile\")",
	}
	for _, fragment := range abiForbidden {
		if strings.Contains(abi, fragment) {
			t.Fatalf("compiled ABI params retained source spelling branch %q", fragment)
		}
	}
}

// TestSelfhostPackageDefinitionsCompileThroughGenericConsumer pins that source
// and loader functions are emitted from the semantic package registry, without
// a second backend-owned loader seed.
func TestSelfhostPackageDefinitionsCompileThroughGenericConsumer(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	program := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	for _, fragment := range []string{
		"fn source_compiled_supported_local(",
		"fn source_compiled_qualified_name(",
		"fn loader_compiled_supported_local(",
		"fn loader_compiled_qualified_name(",
		"fn append_loader_reachable_compiled_functions(",
		`"append_loader_reachable"`,
	} {
		if strings.Contains(cli, fragment) {
			t.Fatalf("source/loader compilation keeps backend-owned closure %q", fragment)
		}
	}

	consumer := selfhostKizuFunctionBody(t, program, "pub fn append_reachable_functions(")
	for _, fragment := range []string{
		`append_numeric_target_facts(lookup_index, ir_bytes, "package-definition "`,
		`let name_prefix = "package-definition-name ";`,
		"out, lookup_index, canonical_facts, ir_bytes,",
	} {
		if !strings.Contains(consumer, fragment) {
			t.Fatalf("generic package consumer missing compiled emission %q", fragment)
		}
	}
	emitter := selfhostKizuFunctionBody(t, program, "fn emit_numeric_package_definition(")
	if !strings.Contains(emitter, "compiled_llvm::append_compiled_function_auto_indexed(") {
		t.Fatal("generic package definition emitter does not compile the claimed function")
	}
}
