package main

import (
	"strings"
	"testing"
)

// TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures keeps the
// selfhost::source compiled closure tied to function-signature-param facts
// instead of a handwritten params_spec table. append_source_compiled_function
// builds its fully qualified name from the component prefix plus the local name
// and delegates to append_compiled_closure_function, which derives the
// params_spec through compiled_abi_params::append_params_spec and guards the
// empty case (every source helper takes at least one parameter). The forbidden
// fragments pin that the handwritten source_compiled_params_spec table and its
// literal entries are gone, and the ABI mapper learns the bare
// SourceKind/SourceFile spellings the source-module closures use without any new
// params_spec literal.
func TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	body := selfhostKizuFunctionBody(t, cli, "fn append_source_compiled_function(")
	bodyRequired := []string{
		"try append_component_qualified_name(&var function_name, \"selfhost::source::\", local_name);",
		"let function_name_bytes = function_name.as_bytes();",
		"try append_compiled_closure_function(out, ir_bytes, function_name_bytes);",
	}
	for _, fragment := range bodyRequired {
		if !strings.Contains(body, fragment) {
			t.Fatalf("source compiled function delegation missing %q", fragment)
		}
	}

	assertCompiledClosureParamsDerivation(t, cli)

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

// TestSelfhostSourceLoaderCompiledClosureResolvedFromFacts pins that the
// selfhost::source and selfhost::source::loader compiled closures no longer keep
// a handwritten supported/qualified lookup table. The qualified name is built
// from the component prefix plus the local name, and a local helper is only
// treated as supported when its function-signature-return fact actually exists
// in the IR, resolved through compiled_fact_lookup::lookup_fact_value_by_prefix_or_empty.
func TestSelfhostSourceLoaderCompiledClosureResolvedFromFacts(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	removed := []string{
		"fn source_compiled_supported_local(",
		"fn source_compiled_qualified_name(",
		"fn loader_compiled_supported_local(",
		"fn loader_compiled_qualified_name(",
	}
	for _, fragment := range removed {
		if strings.Contains(cli, fragment) {
			t.Fatalf("source/loader compiled closure keeps hand-written lookup %q", fragment)
		}
	}

	present := selfhostKizuFunctionBody(t, cli, "fn component_compiled_local_present(")
	presentRequired := []string{
		"compiled_fact_lookup::lookup_fact_value_by_prefix_or_empty(",
		"\"function-signature-return \",",
		"prefix,",
		"local_name",
		"return std::mem::len(value) != 0;",
	}
	for _, fragment := range presentRequired {
		if !strings.Contains(present, fragment) {
			t.Fatalf("component_compiled_local_present missing IR-fact check %q", fragment)
		}
	}

	sourceCallee := selfhostKizuFunctionBody(t, cli, "fn collect_source_compiled_callee(")
	for _, fragment := range []string{
		"if !component_compiled_local_present(ir_bytes, \"selfhost::source::\", local) {",
		"if component_compiled_local_present(ir_bytes, \"selfhost::source::\", callee) {",
	} {
		if !strings.Contains(sourceCallee, fragment) {
			t.Fatalf("collect_source_compiled_callee missing fact-based gate %q", fragment)
		}
	}

	loaderCallee := selfhostKizuFunctionBody(t, cli, "fn collect_loader_compiled_callee(")
	for _, fragment := range []string{
		"if !component_compiled_local_present(ir_bytes, \"selfhost::source::loader::\", local) {",
		"if component_compiled_local_present(ir_bytes, \"selfhost::source::loader::\", callee) {",
	} {
		if !strings.Contains(loaderCallee, fragment) {
			t.Fatalf("collect_loader_compiled_callee missing fact-based gate %q", fragment)
		}
	}
}
