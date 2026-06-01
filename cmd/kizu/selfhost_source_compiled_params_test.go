package main

import (
	"strings"
	"testing"
)

// TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures keeps the
// selfhost::source compiled closure tied to function-signature-param facts
// instead of a handwritten params_spec table. append_source_compiled_function
// must derive its params_spec through compiled_abi_params::append_params_spec
// and guard the empty case (every source helper takes at least one parameter),
// matching the std lexer / lexer / loader closures. The forbidden fragments pin
// that the handwritten source_compiled_params_spec table and its literal entries
// are gone, and the ABI mapper learns the bare SourceKind/SourceFile spellings
// the source-module closures use without any new params_spec literal.
func TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	body := selfhostKizuFunctionBody(t, cli, "fn append_source_compiled_function(")
	required := []string{
		"let function_name = try source_compiled_qualified_name(local_name);",
		"var params_spec = std::string::String(std::mem::page_allocator());",
		"defer params_spec.deinit();",
		"compiled_abi_params::append_params_spec(&var params_spec, ir_bytes, function_name)",
		"if params_spec.len() == 0 {",
		"source compiled closure: missing signature params",
		"let params_spec_bytes = params_spec.as_bytes();",
		"params_spec_bytes",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("source compiled params derivation missing %q", fragment)
		}
	}

	forbidden := []string{
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

	abiRequired := []string{
		"std::mem::equal_bytes(kizu_type, \"SourceKind\")",
		"std::mem::equal_bytes(kizu_type, \"SourceFile\")",
	}
	for _, fragment := range abiRequired {
		if !strings.Contains(abi, fragment) {
			t.Fatalf("compiled ABI params mapper missing %q", fragment)
		}
	}

	// The compiled closure shape (seed, supported helpers, qualified names) must
	// stay unchanged by the params derivation; pin the supported helper count.
	supported := selfhostKizuFunctionBody(t, cli, "fn source_compiled_supported_local(")
	if got := strings.Count(supported, "std::mem::equal_bytes(name, "); got != 5 {
		t.Fatalf("source compiled supported helper count changed: got %d, want 5", got)
	}
	reachable := selfhostKizuFunctionBody(t, cli, "fn append_source_reachable_compiled_functions(")
	for _, seed := range []string{
		"try pending.append(\"is_source_code\");",
		"try pending.append(\"is_frontend_source\");",
		"try pending.append(\"module_path\");",
		"try pending.append(\"is_absolute_name_for_file\");",
	} {
		if !strings.Contains(reachable, seed) {
			t.Fatalf("source compiled closure seed changed, missing %q", seed)
		}
	}
}
