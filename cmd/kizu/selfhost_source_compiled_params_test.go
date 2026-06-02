package main

import (
	"strings"
	"testing"
)

// assertComponentReachableCompiledClosure pins that one component's compiled
// closure entry point seeds its BFS queue and delegates to the shared
// append_component_reachable_compiled_functions walk with the component prefix
// and the allow_empty_params flag. Keeping the seeds inline guarantees the
// closure is not widened by a hand-seeded supported list, and routing through the
// shared walk guarantees the per-component BFS duplicates stay collapsed.
func assertComponentReachableCompiledClosure(
	t *testing.T,
	cli string,
	fn string,
	prefix string,
	allowEmpty bool,
	seeds []string,
) {
	t.Helper()
	body := selfhostKizuFunctionBody(t, cli, fn)
	for _, seed := range seeds {
		fragment := "try pending.append(\"" + seed + "\");"
		if !strings.Contains(body, fragment) {
			t.Fatalf("%s seed changed, missing %q", fn, fragment)
		}
	}
	flag := "false"
	if allowEmpty {
		flag = "true"
	}
	for _, fragment := range []string{
		"try append_component_reachable_compiled_functions(",
		"\"" + prefix + "\",",
		"&var pending,",
		flag,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("%s missing shared compiled closure delegation %q", fn, fragment)
		}
	}
}

// assertSharedCompiledClosurePath pins the deduplicated compiled closure path:
// the shared BFS emits each pending member once and queues its body callees
// through the shared collector, the member builder constructs the qualified name
// from the component prefix plus the local name, and the emitter derives the
// params_spec from function-signature-param facts while guarding the empty case
// behind allow_empty_params.
func assertSharedCompiledClosurePath(t *testing.T, cli string) {
	t.Helper()
	bfs := selfhostKizuFunctionBody(t, cli, "fn append_component_reachable_compiled_functions(")
	for _, fragment := range []string{
		"if !(try compiled_closure_name_seen(&emitted, local_name)) {",
		"try append_component_compiled_closure_member(",
		"allow_empty_params",
		"try collect_component_compiled_callees(ir_bytes, prefix, local_name, pending);",
	} {
		if !strings.Contains(bfs, fragment) {
			t.Fatalf("shared compiled closure BFS missing %q", fragment)
		}
	}

	collector := selfhostKizuFunctionBody(t, cli, "fn collect_component_compiled_body_callees(")
	for _, fragment := range []string{
		"let body_start = ir_contract::body_facts_start(ir_bytes, name);",
		"ir_contract::body_node_count_from(ir_bytes, name, body_start)",
		"ir_contract::body_node_kind_from(ir_bytes, name, sequence, body_start)",
		"ir_contract::body_call_callee_or_empty_from(",
	} {
		if !strings.Contains(collector, fragment) {
			t.Fatalf("shared compiled closure collector missing scoped body lookup %q", fragment)
		}
	}
	for _, fragment := range []string{
		"ir_contract::body_node_count(ir_bytes, name)",
		"ir_contract::body_node_kind(ir_bytes, name, sequence)",
		"ir_contract::body_call_callee_or_empty(",
	} {
		if strings.Contains(collector, fragment) {
			t.Fatalf("shared compiled closure collector keeps full-artifact scan %q", fragment)
		}
	}

	member := selfhostKizuFunctionBody(t, cli, "fn append_component_compiled_closure_member(")
	for _, fragment := range []string{
		"try append_component_qualified_name(&var function_name, prefix, local_name);",
		"let function_name_bytes = function_name.as_bytes();",
		"try emit_compiled_closure_member(out, ir_bytes, function_name_bytes, allow_empty_params);",
	} {
		if !strings.Contains(member, fragment) {
			t.Fatalf("shared compiled closure member builder missing %q", fragment)
		}
	}

	assertCompiledClosureParamsDerivation(t, cli)
}

// assertComponentCompiledCalleeFactGate pins that the single shared callee
// collector classifies each body call against the component prefix from IR facts:
// std::mem intrinsics are ignored, prefix-local helpers and bare local names are
// admitted only when component_compiled_local_present confirms a signature, and
// nested sub-namespace or cross-component qualified callees are explicit errors
// detected through compiled_callee_has_namespace instead of a hand-written table.
func assertComponentCompiledCalleeFactGate(t *testing.T, cli string) {
	t.Helper()
	callee := selfhostKizuFunctionBody(t, cli, "fn collect_component_compiled_callee(")
	for _, fragment := range []string{
		"if std::mem::starts_with(callee, \"std::mem::\") {",
		"if std::mem::starts_with(callee, prefix) {",
		"if compiled_callee_has_namespace(local) {",
		"if !component_compiled_local_present(ir_bytes, prefix, local) {",
		"if compiled_callee_has_namespace(callee) {",
		"if component_compiled_local_present(ir_bytes, prefix, callee) {",
	} {
		if !strings.Contains(callee, fragment) {
			t.Fatalf("collect_component_compiled_callee missing fact-based gate %q", fragment)
		}
	}
}

// assertNoPerComponentCompiledClosureHelpers pins that the per-component compiled
// closure BFS, append, and collect duplicates stay collapsed into the shared
// path and never reappear as hand-written component-specific helpers.
func assertNoPerComponentCompiledClosureHelpers(t *testing.T, cli string) {
	t.Helper()
	removed := []string{
		"fn append_source_compiled_function(",
		"fn append_kizu_lexer_compiled_function(",
		"fn append_lexer_compiled_function(",
		"fn append_loader_compiled_function(",
		"fn append_compiled_closure_function(",
		"fn append_lexer_compiled_closure_member(",
		"fn collect_source_compiled_callees(",
		"fn collect_source_compiled_callee(",
		"fn collect_kizu_lexer_compiled_callee(",
		"fn collect_lexer_compiled_callee(",
		"fn collect_loader_compiled_callee(",
	}
	for _, fragment := range removed {
		if strings.Contains(cli, fragment) {
			t.Fatalf("compiled closure keeps per-component duplicate %q", fragment)
		}
	}
}

// TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures keeps the
// selfhost::source compiled closure tied to function-signature-param facts
// instead of a handwritten params_spec table. The source closure seeds the shared
// BFS with the "selfhost::source::" prefix, which builds each member's fully
// qualified name and derives the params_spec through
// compiled_abi_params::append_params_spec, guarding the empty case (every source
// helper takes at least one parameter, so allow_empty_params stays false). The
// forbidden fragments pin that the handwritten source_compiled_params_spec table
// and its literal entries are gone, and the ABI mapper learns the bare
// SourceKind/SourceFile spellings the source-module closures use without any new
// params_spec literal.
func TestSelfhostSourceCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	assertComponentReachableCompiledClosure(
		t,
		cli,
		"fn append_source_reachable_compiled_functions(",
		"selfhost::source::",
		false,
		[]string{
			"is_source_code",
			"is_frontend_source",
			"module_path",
			"is_absolute_name_for_file",
		},
	)

	assertSharedCompiledClosurePath(t, cli)
	assertNoPerComponentCompiledClosureHelpers(t, cli)

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
}

// TestSelfhostSourceLoaderCompiledClosureResolvedFromFacts pins that the
// selfhost::source and selfhost::source::loader compiled closures no longer keep
// a handwritten supported/qualified lookup table. The qualified name is built
// from the component prefix plus the local name, and a local helper is only
// treated as supported when its function-signature-return fact actually exists in
// the IR, resolved through compiled_fact_lookup::lookup_fact_value_by_prefix_or_empty.
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

	assertComponentCompiledCalleeFactGate(t, cli)

	assertComponentReachableCompiledClosure(
		t,
		cli,
		"fn append_loader_reachable_compiled_functions(",
		"selfhost::source::loader::",
		false,
		[]string{
			"package_module_end",
			"is_manifest_root_source",
			"package_module_start",
			"source_file",
		},
	)
}
