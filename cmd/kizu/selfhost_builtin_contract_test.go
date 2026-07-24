package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

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
