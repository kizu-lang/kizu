package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestSelfhostPackageSemanticSessionUsesReachableCanonicalInstances follows one
// generic function from the semantic session to the backend. The session must
// emit instance facts only for reachable instantiations and deduplicate them,
// the backend must read those facts as a view rather than copying the package
// tables, and neither side may keep the older skip-and-respecialize path around.
func TestSelfhostPackageSemanticSessionUsesReachableCanonicalInstances(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t,
		"selfhost::ir::package_semantic_session_gate::"+
			"ordered_reachable_instance_gate",
	)
	if err != nil {
		t.Fatalf("package semantic instance gate failed: %v\n%s", err, out)
	}
	assertOrderedReachableInstanceFacts(t, out)

	executable := readSelfhostFile(
		t, "../../selfhost/src/ir/executable_functions.kizu",
	)
	for _, fragment := range []string{
		"package_semantic_session::append_reachable_instances(",
		"package_semantic_session::append_function_instance_facts(",
		"&resolved_package_calls.calls",
		"&resolved_package_calls.canonical_types",
		"&emitted_targets",
	} {
		if !strings.Contains(executable, fragment) {
			t.Errorf("production executable fact path missing %q", fragment)
		}
	}

	compiledProgram := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	canonicalFacts := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_canonical_facts.kizu",
	)
	instanceContext := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_instance_context.kizu",
	)
	assertCanonicalInstanceConsumers(t, instanceContext, canonicalFacts)
	assertCompiledProgramInstancePath(t, compiledProgram)
}

// assertOrderedReachableInstanceFacts decodes the instance facts the gate
// printed. The fixture instantiates the same generic twice with the two type
// arguments swapped, so the two facts must agree on target identity, arity and
// body-lowering mode while their ordered type columns mirror each other -- the
// cheapest way to catch an instance key that has stopped distinguishing argument
// order, or a table that collapsed the two instantiations into one.
func assertOrderedReachableInstanceFacts(t *testing.T, out string) {
	t.Helper()
	instanceFacts := functionInstanceFactLines(out)
	if len(instanceFacts) != 2 {
		t.Fatalf(
			"function instance fact count = %d, want deduplicated 2\n%s",
			len(instanceFacts), out,
		)
	}
	first := strings.Fields(instanceFacts[0])
	second := strings.Fields(instanceFacts[1])
	if len(first) != 16 || len(second) != 16 {
		t.Fatalf("malformed function instance facts: %q", instanceFacts)
	}
	if first[1] != second[1] || first[2] != second[2] {
		t.Fatalf("instance target identity drifted: %q", instanceFacts)
	}
	if first[4] != "body" || second[4] != "body" {
		t.Fatalf("source generic instances did not require body lowering: %q", instanceFacts)
	}
	if first[5] != "2" || second[5] != "2" {
		t.Fatalf("instance ordered type arity drifted: %q", instanceFacts)
	}
	if !orderedCanonicalTypeValuesPreserved(first, second) {
		t.Fatalf("ordered canonical type values were not preserved: %q", instanceFacts)
	}
}

// functionInstanceFactLines returns the function-instance rows of a fact tape.
func functionInstanceFactLines(out string) []string {
	var facts []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "function-instance ") {
			facts = append(facts, line)
		}
	}
	return facts
}

// orderedCanonicalTypeValuesPreserved reports whether two instances of the same
// generic target kept their type arguments bound in order rather than merged: each
// carries one ordered value, the two rows carry them in opposite positions, and the
// pair differs -- which is what distinguishes ordering from deduplication.
func orderedCanonicalTypeValuesPreserved(first, second []string) bool {
	return first[6] == "1" && first[11] == "1" &&
		first[7] == second[12] && first[12] == second[7] &&
		first[7] != first[12]
}

// assertCanonicalInstanceConsumers pins the reading half of the fact protocol:
// the context parses the tape by prefix and honours both lowering modes, and the
// canonical view resolves a call type by span and instance id. The forbidden
// names are the copy-based predecessor -- if any of them is back, the backend is
// specializing off its own duplicate of the package tables again.
func assertCanonicalInstanceConsumers(t *testing.T, instanceContext, canonicalFacts string) {
	t.Helper()
	for _, fragment := range []string{
		"pub fn load(",
		`let param_prefix = "function-instance-param ";`,
		`let result_prefix = "function-instance-result ";`,
		`let prefix = "function-instance ";`,
		`std::mem::equal_bytes(mode, "body")`,
		`std::mem::equal_bytes(mode, "callsite")`,
	} {
		if !strings.Contains(instanceContext, fragment) {
			t.Errorf("compiled instance consumer missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"pub fn validate_instance_context(",
		"pub fn parsed_call_type_for_span_instance_id(",
		`return error("concrete instance call type missing");`,
	} {
		if !strings.Contains(canonicalFacts, fragment) {
			t.Errorf("copy-free canonical instance view missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"copy_for_specialization",
		"specialize_function_call_type",
		"function-instance-type ",
		"lookup_type_map",
	} {
		if strings.Contains(canonicalFacts, forbidden) {
			t.Errorf("canonical instance view retained package-table copy path %q", forbidden)
		}
	}
}

// assertCompiledProgramInstancePath keeps the program emitter on the instance
// context for symbols and parameter specs. The removed names were the old
// escape hatch, where an instance the emitter could not describe was skipped and
// re-derived at the callsite; both paths existing at once is how the two
// disagreed.
func assertCompiledProgramInstancePath(t *testing.T, compiledProgram string) {
	t.Helper()
	for _, fragment := range []string{
		"compiled_instance_context::load(",
		"compiled_abi_params::append_params_spec_instance_indexed(",
		"compiled_instance_context::symbol(",
	} {
		if !strings.Contains(compiledProgram, fragment) {
			t.Errorf("compiled program instance path missing %q", fragment)
		}
	}
	for _, removed := range []string{
		"generic_definition_is_callsite_lowered",
		"callsite_has_owned_constructor_lowering",
		"owned_constructor_for_span_indexed",
	} {
		if strings.Contains(compiledProgram, removed) {
			t.Errorf("compiled program retained legacy generic skip %q", removed)
		}
	}
}

// TestSelfhostBodyCallInstanceFactsRejectMismatchedOwnership checks that the
// resolver refuses instance facts whose symbol, target or multiplicity does not
// line up. These tapes are well formed, so nothing but the ownership rules
// stands between them and a wrongly bound call.
func TestSelfhostBodyCallInstanceFactsRejectMismatchedOwnership(t *testing.T) {
	for _, entry := range []string{
		"gate_body_call_instance_symbol_mismatch",
		"gate_body_call_instance_duplicate",
		"gate_body_call_instance_target_mismatch",
		"gate_function_instance_symbol_owner_collision",
	} {
		out, err := runSelfhostAbiParamsGate(
			t, "selfhost::backend::compiled_type_resolver::"+entry,
		)
		if err == nil {
			t.Fatalf("%s unexpectedly accepted invalid instance facts\n%s", entry, out)
		}
	}
}

// TestSelfhostCanonicalCallLoweringElidesComptimeArguments runs the gate that
// lowers a call with comptime arguments: those arguments are already part of the
// canonical instance identity, so they must not also survive as runtime
// operands. The gate asserts the resulting shape itself; here we only require it
// to pass.
func TestSelfhostCanonicalCallLoweringElidesComptimeArguments(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t,
		"selfhost::backend::compiled_mir_lower_call::"+
			"gate_canonical_comptime_argument_elision",
	)
	if err != nil {
		t.Fatalf("canonical comptime call lowering gate failed: %v\n%s", err, out)
	}
}

// TestSelfhostGeneratedMIRNamesUseOneLifetimeStore pins the lifetime rule for
// generated SSA names: the call lowering cache owns one MirNameStore, every name
// the lowering invents is handed to it, and nothing frees or returns a name on
// its own. The names outlive the function that builds them -- they end up in the
// emitted MIR -- so a locally released name is a use-after-free, not a leak.
func TestSelfhostGeneratedMIRNamesUseOneLifetimeStore(t *testing.T) {
	mir := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir.kizu")
	mirTypes := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	lowering := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")

	assertMirNameStoreLifecycle(t, mir, mirTypes)

	// The generated-name helpers grow every time the lowering learns to invent
	// another SSA name, so a fixed consumer census cannot hold -- it was already
	// wrong when written (7 asserted, 8 in the tree at 1abebb90, 13 today).
	// Assert the property that census stood in for: every name the lowering
	// allocates is handed to the one cache-owned store, none is freed or
	// returned locally, and no site builds a store of its own.
	assertGeneratedMirNamesAreStoreOwned(t, lowering)

	if escaped := regexp.MustCompile(
		`return\s+[A-Za-z_][A-Za-z0-9_]*\.as_bytes\(\)`,
	).FindAllString(lowering, -1); len(escaped) != 0 {
		t.Errorf(
			"MIR lowering returns locally owned name bytes %v; "+
				"route them through name_store.own",
			escaped,
		)
	}
	if got := strings.Count(mirTypes, "compiled_mir::mir_name_store()"); got != 1 {
		t.Errorf(
			"MIR name store constructed %d times in the call lowering cache, want exactly 1",
			got,
		)
	}
	if strings.Contains(lowering, "mir_name_store()") {
		t.Error(
			"MIR lowering builds its own name store instead of the one the cache owns",
		)
	}
	if strings.Contains(lowering, "var copied = std::map::Map<[]u8, []u8>") {
		t.Error("generated MIR name helper retained a per-call Map lifetime workaround")
	}
}

// assertGeneratedMirNamesAreStoreOwned requires each String the MIR lowering
// allocates to reach exactly one of the two dispositions this file has: a
// generated SSA name handed to the cache-owned store, or an error payload
// returned to the caller. A String that reaches neither outlives its builder
// inside the emitted MIR, which is a use-after-free rather than a leak.
//
// The disposition is read per allocation site and the local is captured rather
// than spelled. An earlier form counted `var out = ...` against `own(out)`: it
// could not see the one builder that binds `spelling`, and a future builder
// binding any other local would drop out of both sides at once, leaving the
// equality satisfied while the new name escaped the store entirely.
func assertGeneratedMirNamesAreStoreOwned(t *testing.T, lowering string) {
	t.Helper()
	allocation := regexp.MustCompile(
		`var ([A-Za-z_][A-Za-z0-9_]*) = std::string::String\(std::mem::page_allocator\(\)\);`,
	)
	sites := allocation.FindAllStringSubmatchIndex(lowering, -1)
	if len(sites) == 0 {
		t.Error("MIR lowering allocates no strings at all")
		return
	}
	stored := 0
	for index, site := range sites {
		local := lowering[site[2]:site[3]]
		end := len(lowering)
		if index+1 < len(sites) {
			end = sites[index+1][0]
		}
		if mirNameDisposition(t, lowering[site[1]:end], local, mirLoweringLine(lowering, site[0])) {
			stored++
		}
	}
	if stored == 0 {
		t.Error("MIR lowering hands no generated name to MirNameStore")
	}
}

// mirNameDisposition classifies one allocation's scope and reports whether the
// String became a store-owned generated name. Both dispositions at once, or
// neither, are defects: the first double-books the bytes, the second strands them.
func mirNameDisposition(t *testing.T, scope string, local string, line int) bool {
	t.Helper()
	owned := strings.Contains(scope, "call_arg_type_cache.name_store.own("+local+")")
	reported := false
	for _, view := range mirLocalViewBindings(scope, local) {
		if strings.Contains(scope, "return error("+view+");") {
			reported = true
			continue
		}
		if strings.Contains(scope, "return "+view+";") {
			t.Errorf(
				"string %q at compiled_mir_lower.kizu:%d returns its own bytes through %q; "+
					"route the name through name_store.own",
				local,
				line,
				view,
			)
		}
	}
	if released := mirUnconditionalReleases(scope, local); owned && len(released) != 0 {
		t.Errorf(
			"generated MIR name %q at compiled_mir_lower.kizu:%d is released on the success "+
				"path %d times; MirNameStore owns the lifetime",
			local,
			line,
			len(released),
		)
	}
	switch {
	case owned && reported:
		t.Errorf(
			"string %q at compiled_mir_lower.kizu:%d is both handed to MirNameStore and "+
				"returned as an error payload",
			local,
			line,
		)
	case owned:
		return true
	case reported:
	default:
		t.Errorf(
			"string %q at compiled_mir_lower.kizu:%d is neither handed to MirNameStore nor "+
				"returned as an error payload; the emitted MIR would point at freed bytes",
			local,
			line,
		)
	}
	return false
}

// mirUnconditionalReleases returns the releases of this string that run on the
// success path. `errdefer <local>.deinit();` is excluded on purpose: it fires
// only when a builder fails before handing the name over, which is the fix for
// the allocation-failure leak these builders still have. A plain `deinit` or a
// `defer` runs after the store took ownership, and that is a use-after-free.
func mirUnconditionalReleases(scope string, local string) []string {
	release := regexp.MustCompile(
		`(errdefer\s+)?\b` + regexp.QuoteMeta(local) + `\.deinit\(\)`,
	)
	unconditional := []string{}
	for _, match := range release.FindAllStringSubmatch(scope, -1) {
		if match[1] == "" {
			unconditional = append(unconditional, match[0])
		}
	}
	return unconditional
}

// mirLocalViewBindings returns the locals bound to a borrowed view of this
// string. Tying the disposition to a view of THIS local keeps an unrelated
// `return error(...)` further down the scope from standing in as one.
func mirLocalViewBindings(scope string, local string) []string {
	binding := regexp.MustCompile(
		`let ([A-Za-z_][A-Za-z0-9_]*) = ` + regexp.QuoteMeta(local) + `\.as_bytes\(\);`,
	)
	views := []string{}
	for _, match := range binding.FindAllStringSubmatch(scope, -1) {
		views = append(views, match[1])
	}
	return views
}

// mirLoweringLine converts a byte offset into the 1-based source line the
// failure messages above quote.
func mirLoweringLine(lowering string, offset int) int {
	return 1 + strings.Count(lowering[:offset], "\n")
}

// assertMirNameStoreLifecycle pins the store's two halves: the structure that
// keeps the owned strings alongside the borrowed views handed back to callers,
// and the cache field that constructs it and, crucially, tears it down. Without
// the deinit the store is simply a leak with a name.
func assertMirNameStoreLifecycle(t *testing.T, mir, mirTypes string) {
	t.Helper()
	for _, fragment := range []string{
		"pub struct MirNameStore {",
		"names: std::array::Array<std::string::String>",
		"views: std::map::Map<[]u8, []u8>",
		"pub fn mir_name_store() -> MirNameStore",
		"try self.names.append(name);",
		"try self.views.insert(generated, generated);",
		"let name = self.names.pop_or_panic();",
	} {
		if !strings.Contains(mir, fragment) {
			t.Errorf("MIR name store missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"pub name_store: compiled_mir::MirNameStore",
		"name_store: compiled_mir::mir_name_store()",
		"self.name_store.deinit();",
	} {
		if !strings.Contains(mirTypes, fragment) {
			t.Errorf("call lowering cache missing MIR name-store lifecycle %q", fragment)
		}
	}
}

// TestSelfhostGenericAbiUsesStableStaticBindingRecords pins the generic ABI on
// numeric instance identity. Bindings and call edges are recorded as ids that
// stay stable across the session, the backend resolves a call through those ids,
// and the MIR lowering consumes one central call view instead of re-deriving an
// ABI from the callee's signature. The forbidden lists are the string-keyed and
// signature-derived predecessors, which are exactly what stable ids replaced.
func TestSelfhostGenericAbiUsesStableStaticBindingRecords(t *testing.T) {
	session := readSelfhostFile(
		t, "../../selfhost/src/ir/package_semantic_session.kizu",
	)
	callFacts := readSelfhostFile(
		t, "../../selfhost/src/ir/package_call_facts.kizu",
	)
	context := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_instance_context.kizu",
	)
	canonical := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_canonical_facts.kizu",
	)
	mirTypes := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_mir_types.kizu",
	)
	mirLowering := strings.Join([]string{
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu"),
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower_call.kizu"),
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower_struct.kizu"),
	}, "\n")
	mirConsumers := strings.Join([]string{
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu"),
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower_struct.kizu"),
	}, "\n")

	assertStableInstanceIdentityModel(t, session, callFacts, context, canonical)
	assertInstanceCallViewConsumers(t, mirTypes, mirLowering, mirConsumers)
}

// assertStableInstanceIdentityModel covers the producing side: the session
// records call edges between instance ids, the call facts describe a binding
// with numeric kinds instead of spelled type names, and the backend can mint and
// look up an instance id. The superseded names are the string-keyed maps and
// positional ordinals this model replaced -- an id is only stable if nothing
// else is still deriving identity a second way.
func assertStableInstanceIdentityModel(
	t *testing.T, session, callFacts, context, canonical string,
) {
	t.Helper()
	for _, fragment := range []string{
		"struct StaticBinding {",
		"struct CallEdge {",
		"caller_instance_id: i64",
		"callee_instance_id: i64",
		`out.append_bytes("instance-call-type-ids ")`,
		`out.append_bytes("instance-call-instance ")`,
		"append_instance_body_calls(",
	} {
		if !strings.Contains(session, fragment) {
			t.Errorf("semantic session missing stable instance model %q", fragment)
		}
	}
	for _, fragment := range []string{
		"pub struct StaticBindingInput {",
		"pub fn static_binding_type_kind()",
		"pub fn static_binding_scalar_kind()",
		"pub fn static_binding_function_kind()",
	} {
		if !strings.Contains(callFacts, fragment) {
			t.Errorf("call producer missing numeric static binding %q", fragment)
		}
	}
	for _, fragment := range []string{
		"pub fn next_instance_id(",
		"instance_id: i64",
		"pub fn parsed_call_type_for_span_instance_id(",
	} {
		if !strings.Contains(context+"\n"+canonical, fragment) {
			t.Errorf("backend missing exact instance identity %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"function-instance-type ",
		"instance_type_map",
		"call_instance_indexes",
		"call_caller_components",
		"instance_target_ordinal",
		"target_sequence",
		"pub fn load_into(",
	} {
		if strings.Contains(session+"\n"+context+"\n"+canonical, forbidden) {
			t.Errorf("generic ABI retained superseded path %q", forbidden)
		}
	}
}

// assertInstanceCallViewConsumers covers the consuming side: CallLoweringInfo is
// the single description of a call, and the lowering asks for it by instance id
// rather than reassembling a callee name, module prefix or return type. The
// forbidden helpers all reconstruct the ABI from the signature, which is what
// makes a generic call disagree with the definition it resolves to.
func assertInstanceCallViewConsumers(t *testing.T, mirTypes, mirLowering, mirConsumers string) {
	t.Helper()
	for _, fragment := range []string{
		"pub struct CallLoweringInfo {",
		"pub fn lower_call_info_for_instance_indexed(",
		"pub fn call_info_error_success_llvm(",
	} {
		if !strings.Contains(mirTypes, fragment) {
			t.Errorf("backend missing central instance call view %q", fragment)
		}
	}
	if !strings.Contains(mirLowering, "lower_call_info_for_instance_indexed(") {
		t.Error("MIR lowering does not consume the central instance call view")
	}
	for _, fragment := range []string{
		"canonical_runtime_arg_nodes(",
		"lower_call_args_canonical(",
		"lower_call_args_canonical_with_aliases(",
	} {
		if !strings.Contains(mirLowering, fragment) {
			t.Errorf("MIR lowering missing canonical runtime call path %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"lower_call_args_cached(",
		"lower_call_args_with_aliases_cached(",
		"lower_call_arg_types_cached(",
	} {
		if strings.Contains(mirConsumers, forbidden) {
			t.Errorf("production MIR consumer retained signature-based call ABI %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"lower_call_return_type_indexed(",
		"lower_call_callee_name(",
		"lower_call_module_prefix(",
	} {
		if strings.Contains(mirLowering, forbidden) {
			t.Errorf("MIR lowering bypasses the instance call view with %q", forbidden)
		}
	}
}
