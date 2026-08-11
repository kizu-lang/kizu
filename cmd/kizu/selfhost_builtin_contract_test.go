package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostBuiltinContractValidAndInvalidPairs runs the contract's own
// accept and reject oracles. The positive gates are grouped because a builtin
// call is only well formed when the frontend contract, the compiled resolver,
// and the semantic signature all agree on the same pair; the negative cases
// assert on the diagnostic text so a rejection that stops naming its reason
// still fails.
func TestSelfhostBuiltinContractValidAndInvalidPairs(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{
		"selfhost::abi::semantic_signature::valid_gate",
		"selfhost::ir::builtin_contract::valid_pairs_gate",
		"selfhost::ir::builtin_contract::invalid_pairs_gate",
		"selfhost::backend::compiled_type_resolver::builtin_contract_valid_pair_gate",
		"selfhost::backend::compiled_type_resolver::owned_constructor_descriptor_gate",
		"selfhost::backend::compiled_type_resolver::generic_intrinsic_abi_gate",
	} {
		var out bytes.Buffer
		if err := interp.New(&out).RunEntry(program, entry); err != nil {
			t.Fatalf("builtin contract gate %s failed: %v\n%s", entry, err, out.String())
		}
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(
		program,
		"selfhost::backend::compiled_type_resolver::owned_constructor_family_mismatch_gate",
	)
	if err == nil || !strings.Contains(err.Error(), "canonical family mismatch") {
		t.Fatalf("owned constructor family mismatch error = %v, want canonical mismatch", err)
	}
	for _, tc := range []struct {
		entry string
		want  string
	}{
		{"out_of_range_gate", "semantic signature: parameter index out of range"},
		{"empty_slot_gate", "semantic signature: empty parameter type"},
		{"trailing_separator_gate", "semantic signature: empty parameter type"},
	} {
		var signatureOut bytes.Buffer
		err := interp.New(&signatureOut).RunEntry(
			program, "selfhost::abi::semantic_signature::"+tc.entry,
		)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("semantic signature gate %s error = %v, want %q", tc.entry, err, tc.want)
		}
	}
}

// TestSelfhostNullableABIRequiresContractCapability checks that nullable ABI is
// a capability a type has to declare, not a fallback: asking for it on a type
// without the contract is an error rather than a silently chosen representation.
func TestSelfhostNullableABIRequiresContractCapability(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_resolver::invalid_nullable_abi_gate",
	)
	if err == nil || !strings.Contains(err.Error(), "type has no nullable ABI contract") {
		t.Fatalf("nullable ABI error = %v, want missing capability", err)
	}
}

// TestSelfhostBuiltinContractHasSingleOwner keeps builtin_contract.kizu the only
// place that knows which builtin kinds and operations exist. Both consumers must
// import it and derive their answers from it, and neither may keep the private
// name tables and equal_bytes chains it replaced -- a second copy of the table is
// exactly how the frontend and the backend drift apart on what a builtin means.
func TestSelfhostBuiltinContractHasSingleOwner(t *testing.T) {
	contract := readSelfhostFile(t, "../../selfhost/src/ir/builtin_contract.kizu")
	callFacts := readSelfhostFile(t, "../../selfhost/src/ir/package_call_facts.kizu")
	typeResolver := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_resolver.kizu")

	for _, fragment := range []string{
		"pub fn operation(id: i64) -> BuiltinOperation",
		"pub fn operation_from_name(name: []u8) -> i64",
		"pub fn valid_pair(kind_id: i64, operation_id: i64) -> bool",
		"pub fn operation_requires_identity(operation_id: i64) -> !bool",
		`known_operation(id, kind_runtime(), "channel-recv", false)`,
	} {
		if !strings.Contains(contract, fragment) {
			t.Fatalf("canonical builtin contract missing %q", fragment)
		}
	}

	for label, source := range map[string]string{
		"package call facts":     callFacts,
		"compiled type resolver": typeResolver,
	} {
		if !strings.Contains(source, "import selfhost::ir::builtin_contract;") {
			t.Fatalf("%s does not import the canonical builtin contract", label)
		}
	}
	for _, fragment := range []string{
		"return builtin_contract::operation_name(operation);",
		"builtin_contract::valid_pair(kind, operation)",
		"builtin_contract::operation(operation)",
	} {
		if !strings.Contains(callFacts, fragment) {
			t.Fatalf("package call facts do not derive from the canonical contract: missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"builtin_contract::kind_from_name(kind)",
		"builtin_contract::operation_from_name(operation)",
		"builtin_contract::valid_pair(kind_id, operation_id)",
		"builtin_contract::operation_requires_identity(operation_id)",
	} {
		if !strings.Contains(typeResolver, fragment) {
			t.Fatalf("compiled resolver does not derive from the canonical contract: missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"validate_builtin_call_contract(",
		"is_closed_receiver_builtin_operation(",
		`std::mem::equal_bytes(operation, "array-len")`,
		`std::mem::equal_bytes(operation, "channel-recv")`,
		`std::mem::equal_bytes(operation, "union-construct")`,
	} {
		if strings.Contains(typeResolver, forbidden) {
			t.Fatalf("compiled resolver retained a second builtin contract %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		`return "array-len"`,
		`return "channel-recv"`,
		`return "union-construct"`,
	} {
		if strings.Contains(callFacts, forbidden) {
			t.Fatalf("package call facts retained a second operation-name table %q", forbidden)
		}
	}
}

// TestSelfhostOwnedDeinitUsesBuiltinDescriptorAndResolvedABI pins owned-container
// deinit lowering to two resolved facts: the operation id from the builtin
// descriptor and the receiver's resolved ABI kind. The forbidden fragments are
// the spelling-derived route it replaced -- matching the receiver against
// std::array::Array / std::arena::Arena or the method name "deinit" -- which
// AGENTS.md rules out and which cannot see through an alias or a type parameter.
func TestSelfhostOwnedDeinitUsesBuiltinDescriptorAndResolvedABI(t *testing.T) {
	contract := readSelfhostFile(t, "../../selfhost/src/ir/builtin_contract.kizu")
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	deinit := selfhostKizuFunctionBody(
		t, lower, "fn lower_owned_deinit_expr_statement(",
	)

	for _, fragment := range []string{
		"pub struct OwnedContainerOperation",
		"pub fn owned_container_operation(operation_id: i64)",
		"operation_id == operation_array_deinit()",
		"operation_id == operation_arena_deinit()",
		"operation_id == operation_map_deinit()",
		"operation_id == operation_box_deinit()",
	} {
		if !strings.Contains(contract, fragment) {
			t.Fatalf("owned-container builtin descriptor missing %q", fragment)
		}
	}

	for _, fragment := range []string{
		"compiled_type_resolver::call_builtin_indexed(",
		"builtin_contract::operation_from_name(builtin.operation)",
		"builtin_contract::owned_container_operation(operation_id)",
		"compiled_mir_types::resolve_receiver_value_type_indexed_cached(",
		"fixed_abi_contract::from_llvm(receiver_type.abi)",
		"receiver_abi.kind != fixed_abi_contract::FixedAbiKind::Owned",
	} {
		if !strings.Contains(deinit, fragment) {
			t.Fatalf("owned deinit does not consume canonical facts: missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"owned_deinit_receiver_type_supported(",
		`std::array::Array<`,
		`std::arena::Arena<`,
		`std::mem::equal_bytes(method_name, "deinit")`,
		"resolve_value_kizu_type(",
		"resolve_value_llvm_type(",
	} {
		if strings.Contains(deinit, forbidden) {
			t.Fatalf("owned deinit retained spelling-derived classification %q", forbidden)
		}
	}
}

// TestSelfhostOwnedConstructorUsesOrthogonalTypeIDDescriptor follows one owned
// constructor from source to LLVM and requires every stage to speak the same
// descriptor: the frontend classifies from resolved type identity, the emitter
// writes the type arguments as facts, the compiled resolver hands back an
// indexed descriptor, and both lowering sites consume it. The descriptor is
// "orthogonal" in that container family, result ABI, and storage ABI are
// independent fields, so a new container needs no new branch anywhere here.
func TestSelfhostOwnedConstructorUsesOrthogonalTypeIDDescriptor(t *testing.T) {
	callResolution := readSelfhostFile(t, "../../selfhost/src/ir/package_call_resolution.kizu")
	typeResolution := readSelfhostFile(t, "../../selfhost/src/ir/package_type_resolution.kizu")
	callFacts := readSelfhostFile(t, "../../selfhost/src/ir/package_call_facts.kizu")
	emitter := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	resolver := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_resolver.kizu")
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	structLower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower_struct.kizu")

	for label, source := range map[string]string{
		"call resolution":   callResolution,
		"type resolution":   typeResolution,
		"call facts":        callFacts,
		"fact emitter":      emitter,
		"compiled resolver": resolver,
	} {
		if !strings.Contains(source, "owned_constructor") &&
			!strings.Contains(source, "owned-container") {
			t.Fatalf("%s does not participate in the owned constructor descriptor", label)
		}
	}
	assertOwnedConstructorFrontendClassification(t, callResolution)
	assertOwnedConstructorFactEmission(t, emitter, callFacts)
	assertOwnedConstructorResolverDescriptor(t, resolver)
	assertOwnedConstructorLoweringConsumesDescriptor(t, lower, structLower)
}

// assertOwnedConstructorFrontendClassification requires the frontend to decide
// the container family from the resolved nominal target and its type arguments.
// Recovering it from the constructor's spelling is the failure this rules out.
func assertOwnedConstructorFrontendClassification(t *testing.T, callResolution string) {
	t.Helper()
	for _, fragment := range []string{
		"package_type_resolution::intrinsic_owned_container_family(",
		"nominal_constructor != resolution.target_index",
		"package_type_resolution::same_type(",
		"package_type_resolution::type_argument(",
	} {
		if !strings.Contains(callResolution, fragment) {
			t.Fatalf("frontend constructor classification missing %q", fragment)
		}
	}
	if strings.Contains(callResolution, "runtime_intrinsic_method_identity(") {
		t.Fatal("constructor classification recovered family from constructor spelling")
	}
}

// assertOwnedConstructorFactEmission requires the emitted fact to carry the full
// type-argument list. The forbidden name is the earlier single-element schema,
// which could not describe a two-parameter container such as a map.
func assertOwnedConstructorFactEmission(t *testing.T, emitter, callFacts string) {
	t.Helper()
	for _, fragment := range []string{
		"body-call-owned-constructor ",
		"owned_constructor_action(calls, index)",
		"owned_constructor_type_argument_len(",
		"owned_constructor_type_argument(",
	} {
		if !strings.Contains(emitter, fragment) {
			t.Fatalf("owned constructor fact emission missing %q", fragment)
		}
	}
	if strings.Contains(callFacts, "owned_constructor_element_spelling") {
		t.Fatal("owned constructor facts retain the single-element schema")
	}
}

// assertOwnedConstructorResolverDescriptor requires the compiled resolver to
// expose one indexed descriptor lookup that fails closed: the action, the result
// identity, and the owned ABI are each checked with their own diagnostic.
func assertOwnedConstructorResolverDescriptor(t *testing.T, resolver string) {
	t.Helper()
	for _, fragment := range []string{
		"pub fn owned_constructor_indexed(",
		"owned constructor action mismatch",
		"owned constructor result identity mismatch",
		"owned constructor result ABI is not owned",
		"type_size_argument_slot",
		"storage_type_argument_spelling",
	} {
		if !strings.Contains(resolver, fragment) {
			t.Fatalf("compiled owned constructor resolver missing %q", fragment)
		}
	}
}

// assertOwnedConstructorLoweringConsumesDescriptor requires both lowering sites
// -- standalone/value-loop and struct-field -- to read the same descriptor for
// the result ABI, the storage ABI, and the runtime symbol. They differ only in
// what they bind the descriptor to, so they are checked against separate lists.
// The forbidden names are the per-shape constructor paths they replaced.
func assertOwnedConstructorLoweringConsumesDescriptor(t *testing.T, lower, structLower string) {
	t.Helper()
	for _, fragment := range []string{
		"compiled_type_resolver::owned_constructor_indexed(",
		"owned_constructor.result_type.abi",
		"owned_constructor.storage_type.abi",
		"compiled_mir::type_size_call_arg(",
		"constructor_runtime.symbol",
	} {
		if !strings.Contains(lower, fragment) {
			t.Fatalf("standalone/value-loop lowering missing descriptor use %q", fragment)
		}
	}
	for _, fragment := range []string{
		"compiled_type_resolver::owned_constructor_indexed(",
		"constructor.result_type.abi",
		"constructor.storage_type.abi",
		"compiled_mir::type_size_call_arg(",
		"constructor_runtime.symbol",
	} {
		if !strings.Contains(structLower, fragment) {
			t.Fatalf("struct-field lowering missing descriptor use %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"lower_stdlib_struct_constructor_function",
		"append_stdlib_struct_constructor_indexed",
		"ctor_callee",
		"field_callee",
		"value_loop_array_let_name",
		"requires_type_size",
	} {
		if strings.Contains(lower, forbidden) || strings.Contains(structLower, forbidden) {
			t.Fatalf("constructor spelling/dead path remains %q", forbidden)
		}
	}
}
