package main

import (
	"strings"
	"testing"
)

// TestSelfhostBackendProfileFlushesIncrementallyOnlyWhenRequested keeps detailed
// backend profile I/O from dominating phase timing runs unless explicitly requested.
func TestSelfhostBackendProfileFlushesIncrementallyOnlyWhenRequested(t *testing.T) {
	profile := readSelfhostFile(t, "../../selfhost/src/backend/profile.kizu")
	start := selfhostKizuFunctionBody(t, profile, "pub fn start(")
	if strings.Contains(start, "KIZU_SELFHOST_STAGE_PROFILE") {
		t.Fatal("backend profile must not be enabled implicitly by stage profiling")
	}
	for _, fragment := range []string{
		`std::process::env_or_empty("KIZU_SELFHOST_BACKEND_PROFILE")`,
		`std::process::env_or_empty("KIZU_SELFHOST_BACKEND_PROFILE_FLUSH_EACH")`,
		`let enabled = std::mem::equal_bytes(backend_enabled, "1");`,
		`let flush_each = std::mem::equal_bytes(flush_each_value, "1");`,
		`flush_each: flush_each,`,
	} {
		if !strings.Contains(start, fragment) {
			t.Fatalf("backend profile start missing %q", fragment)
		}
	}

	begin := selfhostKizuFunctionBody(t, profile, "pub fn begin(")
	end := selfhostKizuFunctionBody(t, profile, "pub fn end(")
	beginComponent := selfhostKizuFunctionBody(t, profile, "pub fn begin_component_member(")
	endComponent := selfhostKizuFunctionBody(t, profile, "pub fn end_component_member(")
	for name, body := range map[string]string{
		"begin":                  begin,
		"end":                    end,
		"begin_component_member": beginComponent,
		"end_component_member":   endComponent,
	} {
		if !strings.Contains(body, "try flush_incremental(profile, out, io);") {
			t.Fatalf("%s does not use incremental profile flushing", name)
		}
		if strings.Contains(body, "try flush(profile, out, io);") {
			t.Fatalf("%s still flushes every profile event unconditionally", name)
		}
	}

	flushIncremental := selfhostKizuFunctionBody(t, profile, "fn flush_incremental(")
	for _, fragment := range []string{
		"if !profile.flush_each {",
		"try flush(profile, out, io);",
	} {
		if !strings.Contains(flushIncremental, fragment) {
			t.Fatalf("flush_incremental missing %q", fragment)
		}
	}

	finish := selfhostKizuFunctionBody(t, profile, "pub fn finish(")
	if !strings.Contains(finish, "try flush(profile, out, io);") {
		t.Fatal("backend profile finish must write the final profile")
	}
}

// TestSelfhostBackendProfileRecordsIRFactMetricsOnlyWhenEnabled keeps fact-size
// diagnostics available without adding work to normal backend profile-off runs.
func TestSelfhostBackendProfileRecordsIRFactMetricsOnlyWhenEnabled(t *testing.T) {
	profile := readSelfhostFile(t, "../../selfhost/src/backend/profile.kizu")
	isEnabled := selfhostKizuFunctionBody(t, profile, "pub fn is_enabled(")
	if !strings.Contains(isEnabled, "return profile.enabled;") {
		t.Fatal("backend profile should expose a cheap enabled predicate")
	}

	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	emit := selfhostKizuFunctionBody(t, llvm, "pub fn emit_llvm_artifact(")
	for _, fragment := range []string{
		"if profile::is_enabled(&backend_profile) {",
		`"record_ir_fact_metrics"`,
		"try record_ir_fact_profile_metrics(",
	} {
		if !strings.Contains(emit, fragment) {
			t.Fatalf("emit_llvm_artifact missing profile guard %q", fragment)
		}
	}

	recordFacts := selfhostKizuFunctionBody(t, llvm, "fn record_ir_fact_profile_metrics(")
	for _, fragment := range []string{
		`"ir.fact.body_node.entries"`,
		`"ir.fact.body_node.line_bytes"`,
		`"ir.fact.body_edge.entries"`,
		`"ir.fact.body_edge.line_bytes"`,
		`"ir.fact.function_signature_param.entries"`,
		`"ir.fact.struct_field.entries"`,
		`"ir.fact.stdlib_return.entries"`,
	} {
		if !strings.Contains(recordFacts, fragment) {
			t.Fatalf("record_ir_fact_profile_metrics missing %q", fragment)
		}
	}

	recordPrefix := selfhostKizuFunctionBody(t, llvm, "fn record_fact_prefix_metrics(")
	for _, fragment := range []string{
		"ir_index::fact_prefix_entry_count(lookup_index, ir_bytes, fact_prefix)",
		"ir_index::fact_prefix_line_bytes(lookup_index, ir_bytes, fact_prefix)",
	} {
		if !strings.Contains(recordPrefix, fragment) {
			t.Fatalf("record_fact_prefix_metrics missing %q", fragment)
		}
	}

	irIndex := readSelfhostFile(t, "../../selfhost/src/backend/ir_index.kizu")
	for _, signature := range []string{
		"pub fn fact_prefix_entry_count(",
		"pub fn fact_prefix_line_bytes(",
	} {
		body := selfhostKizuFunctionBody(t, irIndex, signature)
		for _, fragment := range []string{
			"first_entry_with_fact_prefix(index, ir_bytes, fact_prefix)",
			"entry_key_starts_with(index, ir_bytes, entry, fact_prefix)",
		} {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s missing %q", signature, fragment)
			}
		}
	}
}

// TestSelfhostLLVMRenderReservesModuleOutput keeps the large selfhost LLVM
// module render from growing its byte buffer one small append at a time.
func TestSelfhostLLVMRenderReservesModuleOutput(t *testing.T) {
	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	render := selfhostKizuFunctionBody(t, llvm, "fn render_llvm_module(")
	reserve := "try out.reserve(std::mem::len(ir_bytes) / 3);"
	preamble := `profile::begin(backend_profile, profile_out, io, "render_module_preamble")`
	reserveIndex := strings.Index(render, reserve)
	if reserveIndex < 0 {
		t.Fatalf("render_llvm_module missing output reserve %q", reserve)
	}
	preambleIndex := strings.Index(render, preamble)
	if preambleIndex < 0 {
		t.Fatalf("render_llvm_module missing preamble profile marker %q", preamble)
	}
	if reserveIndex > preambleIndex {
		t.Fatal("render_llvm_module should reserve before the first large append phase")
	}
}

// TestSelfhostBackendProfileSplitsCompiledLowerRender keeps the profiled
// component path from hiding return-type lookup inside the lower/render bucket.
func TestSelfhostBackendProfileSplitsCompiledLowerRender(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	emit := selfhostKizuFunctionBody(t, cli, "fn emit_compiled_closure_member_profiled(")
	for _, fragment := range []string{
		`"derive_return"`,
		`compiled_signature::derive_return_type_indexed(`,
		`"lower_render_body"`,
		`compiled_llvm::append_compiled_function_auto_return_indexed(`,
	} {
		if !strings.Contains(emit, fragment) {
			t.Fatalf("profiled compiled closure emission missing %q", fragment)
		}
	}
	if strings.Contains(emit, `"lower_render"`) {
		t.Fatal("profiled compiled closure emission should split lower_render into narrower buckets")
	}

	compiledLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_llvm.kizu")
	wrapper := selfhostKizuFunctionBody(
		t,
		compiledLLVM,
		"pub fn append_compiled_function_auto_return_indexed(",
	)
	for _, fragment := range []string{
		"let llvm_symbol_value = llvm_symbol.*;",
		"let params_spec_value = params_spec.*;",
		"try append_compiled_function_params_indexed(",
	} {
		if !strings.Contains(wrapper, fragment) {
			t.Fatalf("append_compiled_function_auto_return_indexed missing %q", fragment)
		}
	}
}

// TestSelfhostMIRCachedLetTypeResolutionUsesIndexedHelpers keeps the hot
// per-function let-type cache from falling back to whole-IR fact scans.
func TestSelfhostMIRCachedLetTypeResolutionUsesIndexedHelpers(t *testing.T) {
	mirTypes := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	cached := selfhostKizuFunctionBody(t, mirTypes, "fn resolve_let_value_kizu_type_for_value_cached(")
	for _, fragment := range []string{
		"resolve_value_kizu_type_indexed_cached(",
		"resolve_value_array_get_element_or_empty_indexed_cached(",
		"resolve_try_call_success_kizu_type_indexed(",
	} {
		if !strings.Contains(cached, fragment) {
			t.Fatalf("cached let type resolution missing %q", fragment)
		}
	}

	callCached := selfhostKizuFunctionBody(
		t,
		mirTypes,
		"fn resolve_let_call_value_kizu_type_indexed_cached(",
	)
	for _, fragment := range []string{
		"resolve_receiver_value_kizu_type_indexed_cached(",
		"value_receiver_method_return_kizu_or_empty_indexed_cached(",
	} {
		if !strings.Contains(callCached, fragment) {
			t.Fatalf("cached call type resolution missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"try resolve_receiver_value_kizu_type(",
		"try value_receiver_method_return_kizu_or_empty(",
	} {
		if strings.Contains(callCached, forbidden) {
			t.Fatalf("cached call type resolution still uses non-indexed helper %q", forbidden)
		}
	}

	tryIndexed := selfhostKizuFunctionBody(
		t,
		mirTypes,
		"fn resolve_try_call_success_kizu_type_indexed(",
	)
	for _, fragment := range []string{
		"cross_module_callee_qualified_name_or_empty_indexed(",
		"lookup_fact_value_by_prefix_or_empty_indexed(",
		"lookup_stdlib_return_indexed(",
	} {
		if !strings.Contains(tryIndexed, fragment) {
			t.Fatalf("indexed try-call type resolution missing %q", fragment)
		}
	}
}

// TestSelfhostMIRLetCallLoweringKeepsCalleeResolutionIndexed covers the hot
// let-call lowering path sampled under lower_multi_let_statement_named.
func TestSelfhostMIRLetCallLoweringKeepsCalleeResolutionIndexed(t *testing.T) {
	mirLower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	for _, signature := range []string{
		"fn lower_multi_let_try_call(",
		"fn lower_multi_let_call(",
	} {
		body := selfhostKizuFunctionBody(t, mirLower, signature)
		for _, fragment := range []string{
			"lower_call_callee_name_indexed(",
			"lower_call_module_prefix_indexed(",
		} {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s missing %q", signature, fragment)
			}
		}
		for _, forbidden := range []string{
			"try compiled_mir_types::lower_call_callee_name(",
			"try compiled_mir_types::lower_call_module_prefix(",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still uses non-indexed helper %q", signature, forbidden)
			}
		}
	}
}

// TestSelfhostIndexedTypeLoweringAvoidsKnownFallbackScans keeps the indexed
// type lowerer from bouncing known ABI shapes through the whole-IR fallback.
func TestSelfhostIndexedTypeLoweringAvoidsKnownFallbackScans(t *testing.T) {
	typeLower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_lower.kizu")
	indexed := selfhostKizuFunctionBody(t, typeLower, "pub fn kizu_type_to_llvm_indexed(")
	for _, fragment := range []string{
		"error_union_type_llvm_direct_or_empty(inner)",
		"error_union_enum_abi_or_empty_indexed(",
		"non_error_type_llvm_direct_or_empty(kizu_type)",
	} {
		if !strings.Contains(indexed, fragment) {
			t.Fatalf("indexed type lowering missing %q", fragment)
		}
	}

	enumIndexed := selfhostKizuFunctionBody(t, typeLower, "fn error_union_enum_abi_or_empty_indexed(")
	if !strings.Contains(enumIndexed, "compiled_fact_lookup::is_enum_type_indexed(") {
		t.Fatal("indexed error-union enum lowering should use indexed enum lookup")
	}

	errorDirect := selfhostKizuFunctionBody(t, typeLower, "fn error_union_type_llvm_direct_or_empty(")
	for _, fragment := range []string{
		`"ParseNode"`,
		`"%kizu.error.parse_node"`,
		`"Token"`,
		`"%kizu.error.token"`,
	} {
		if !strings.Contains(errorDirect, fragment) {
			t.Fatalf("direct error-union lowering missing %q", fragment)
		}
	}
}
