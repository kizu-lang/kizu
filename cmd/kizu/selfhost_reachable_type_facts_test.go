package main

import (
	"strings"
	"testing"
)

// TestSelfhostReachableComponentTypeFacts pins the type facts emitted for one
// reachable component: struct/enum/union identity, field spellings that keep
// their generic arguments intact, and declared type parameters reproduced
// verbatim (including a leading-underscore name). The tail of the test also
// pins the enum ABI header as a single line emitted exactly once and
// immediately followed by its first variant, because a mis-terminated header
// silently fuses two facts into one and still looks plausible in the dump.
func TestSelfhostReachableComponentTypeFacts(t *testing.T) {
	out, err := runSelfhostPackageGate(
		t, "selfhost::ir::package_type_facts_gates::component_type_facts_gate",
	)
	if err != nil {
		t.Fatalf("component type facts gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "generic component type facts", out, []string{
		"type-llvm app::model::Child %kizu.app.model.child",
		"struct-field app::model::Child 0 value i64",
		"type-llvm app::model::Model %kizu.app.model.model",
		"struct-field app::model::Model 0 child Child",
		"struct-field app::model::Model 1 items std::array::Array<Model>",
		"struct-field app::model::Model 2 bytes std::channel::Channel<[]u8>",
		"struct-field app::model::Model 3 models std::channel::Channel<std::array::Array<Model>>",
		"struct-field app::model::Model 4 by_name std::map::Map<[]u8,Model>",
		"declared-type-parameter app::model::Tokens 0 V",
		"declared-type-parameter app::model::Tokens 1 _V",
		"struct-field app::model::Tokens 0 plain std::map::Map<[]u8,V>",
		"struct-field app::model::Tokens 1 underscored std::map::Map<[]u8,_V>",
		"enum-type app::model::State i64",
		"enum-variant app::model::State Ready 0",
		"enum-variant app::model::State Done 1",
		"union-type app::model::Event %kizu.app.model.event",
		"union-variant app::model::Event Empty 0 void",
		"union-variant app::model::Event Model 1 Model",
		"union-variant app::model::Event Bytes 2 []u8",
		"declared-type-parameter app::model::GenericEvent 0 K",
		"declared-type-parameter app::model::GenericEvent 1 V",
		"union-type app::model::GenericEvent %kizu.app.model.generic_event",
		"union-variant app::model::GenericEvent Empty 0 void",
		"union-variant app::model::GenericEvent Entry 1 std::map::Map<K,V>",
	})
	lines := strings.Split(out, "\n")
	enumType := "enum-type app::model::State i64"
	firstVariant := "enum-variant app::model::State Ready 0"
	headerIndex := -1
	for index, line := range lines {
		if line == enumType {
			if headerIndex >= 0 {
				t.Fatalf("enum ABI header emitted more than once: %q", enumType)
			}
			headerIndex = index
		}
	}
	if headerIndex < 0 {
		t.Fatalf("exact enum i64 ABI line missing:\n%s", out)
	}
	if headerIndex+1 >= len(lines) {
		t.Fatalf("enum ABI header has no following variant fact:\n%s", out)
	}
	if lines[headerIndex+1] != firstVariant {
		t.Fatalf(
			"enum header and first variant are not separate adjacent facts\nheader=%q\nnext=%q",
			enumType, lines[headerIndex+1],
		)
	}
}

// TestSelfhostEnumTypeFactUsesExplicitNewlineByte guards the source-level cause
// of the fused enum header above: the selfhost string literal does not decode
// "\n", so the emitter must terminate the line with byte 10 rather than an
// escape that would survive into the fact stream as two literal characters.
func TestSelfhostEnumTypeFactUsesExplicitNewlineByte(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/package_type_facts.kizu")
	body := selfhostKizuFunctionBody(t, source, "fn append_enum_name(")
	requireSourceFragments(t, "enum fact line termination", body, []string{
		`out.append_bytes(" i64")`,
		"out.append_byte(cast<u8>(10))",
	})
	if strings.Contains(body, `append_bytes(" i64\n")`) {
		t.Fatal("enum fact emitter relies on an undecoded backslash-newline escape")
	}
}

// TestSelfhostExternalABIRepresentationHasSingleOwner keeps compiler-owned
// types to exactly one ABI owner. String is described by an abi-repr fact, and
// a competing type-llvm fact for the same identity would give lowering two
// answers to choose between. The intrinsic reprs asserted alongside it are the
// generic (arity-driven) ones, so admitting a new pointer-like type must not
// add a new fact family.
func TestSelfhostExternalABIRepresentationHasSingleOwner(t *testing.T) {
	out, err := runSelfhostPackageGate(
		t, "selfhost::ir::package_type_facts_gates::external_abi_owner_gate",
	)
	if err != nil {
		t.Fatalf("external ABI owner gate failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "abi-repr std::string::String %kizu.owned") {
		t.Fatalf("external ABI fact missing:\n%s", out)
	}
	for _, fact := range []string{
		"abi-repr ptr ptr",
		"nullable-abi-repr ptr ptr",
		"abi-repr []u8 %kizu.slice.u8",
	} {
		if !strings.Contains(out, fact) {
			t.Fatalf("generic intrinsic ABI fact %q missing:\n%s", fact, out)
		}
	}
	if strings.Contains(out, "type-llvm std::string::String") {
		t.Fatalf("external ABI identity retained a competing nominal owner:\n%s", out)
	}
}

// TestSelfhostExternalABIContractIsUnifiedAndFailClosed pins the external ABI
// table as a single record read through one accessor. The four per-field
// lookups it replaced could drift apart row by row, so their names are asserted
// gone rather than merely unused. The out-of-range half matters just as much:
// an unknown index must raise, since a zero-value row would hand lowering a
// well-formed but wrong ABI.
func TestSelfhostExternalABIContractIsUnifiedAndFailClosed(t *testing.T) {
	contractSource := readSelfhostFile(
		t, "../../selfhost/src/ir/intrinsic_type_contract.kizu",
	)
	requireSourceFragments(t, "unified external ABI contract", contractSource, []string{
		"pub struct ExternalAbiContract",
		"pub identity: []u8",
		"pub fixed_abi: fixed_abi_contract::FixedAbi",
		"pub generic_arity: i64",
		"pub dependence: ExternalAbiDependence",
		"pub nullable: bool",
		"pub owned_family: []u8",
		"pub fn external_abi_contract_at(index: i64)",
	})
	for _, removed := range []string{
		"external_abi_identity_at",
		"external_abi_representation_at",
		"external_abi_is_nullable_at",
		"external_abi_owned_family_at",
	} {
		if strings.Contains(contractSource, removed) {
			t.Fatalf("parallel external ABI lookup remains: %q", removed)
		}
	}
	factsSource := readSelfhostFile(t, "../../selfhost/src/ir/package_type_facts.kizu")
	emitter := selfhostKizuFunctionBody(
		t, factsSource, "pub fn append_core_abi_representation_facts(",
	)
	requireSourceFragments(t, "external ABI record consumer", emitter, []string{
		"external_abi_contract_at(index)",
		"entry.identity",
		"entry.fixed_abi.llvm_name",
		"entry.owned_family",
		"entry.nullable",
		"entry.dependence",
	})

	out, err := runSelfhostPackageGate(
		t, "selfhost::ir::package_type_facts_gates::external_abi_contract_gate",
	)
	if err != nil {
		t.Fatalf("external ABI contract gate failed: %v\n%s", err, out)
	}
	if out != "ok\n" {
		t.Fatalf("external ABI contract gate output = %q, want ok", out)
	}

	out, err = runSelfhostPackageGate(
		t,
		"selfhost::ir::package_type_facts_gates::"+
			"external_abi_contract_out_of_range_gate",
	)
	if err == nil {
		t.Fatalf("out-of-range external ABI contract was accepted\n%s", out)
	}
	if !strings.Contains(err.Error(), "external ABI index out of range") {
		t.Fatalf("out-of-range external ABI contract error mismatch: %v", err)
	}
}

// TestSelfhostPackageTypeCatalogIncludesTypeOnlyComponents covers the
// components a function-driven walk would miss: RunArtifact and OutputStream
// are only ever named as types, never called, so they appear in the catalog
// only if type discovery runs over the whole package instead of following the
// reachable-function closure.
func TestSelfhostPackageTypeCatalogIncludesTypeOnlyComponents(t *testing.T) {
	out, err := runSelfhostPackageGate(
		t, "selfhost::ir::package_type_facts_gates::package_component_type_facts_gate",
	)
	if err != nil {
		t.Fatalf("package component type facts gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "package-wide type facts", out, []string{
		"type-llvm selfhost::backend::data::RunArtifact %kizu.selfhost.backend.data.run_artifact",
		"struct-field selfhost::backend::data::RunArtifact 0 bytes i64",
		"struct-field selfhost::backend::data::RunArtifact 1 metadata_bytes i64",
		"enum-type selfhost::backend::data::OutputStream i64",
		"enum-variant selfhost::backend::data::OutputStream None 0",
		"enum-variant selfhost::backend::data::OutputStream Stdout 1",
	})
}

// TestSelfhostNumericCollectorHasNoNamedTypeSeeds keeps the numeric closure
// purely a reachability walk. Seeding it with specific type names, or appending
// struct facts by qualified name from inside it, would make the closure's
// result depend on which types happen to be spelled in the collector rather
// than on what the package actually reaches.
func TestSelfhostNumericCollectorHasNoNamedTypeSeeds(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	body := selfhostKizuFunctionBody(t, source, "fn append_numeric_package_closure(")
	for _, forbidden := range []string{
		`"ConstructorFacts"`, `"TypeRecord"`,
		`append_qualified_struct_type_facts(`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("numeric closure retained named type seed %s", forbidden)
		}
	}
}

// TestSelfhostSelectedComponentsUseCanonicalTypeCatalog pins the one place type
// discovery is allowed to happen. The fact producer builds the package-wide
// catalog; the numeric function closure must no longer own component discovery
// or track which components it already emitted. The forbidden names are the
// per-role emitters that predated the catalog, each of which decided for itself
// which types a loader, a diagnostic, or the codegen path needed.
func TestSelfhostSelectedComponentsUseCanonicalTypeCatalog(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	appendFacts := selfhostKizuFunctionBody(t, source, "fn append_facts_from_parsed(")
	if !strings.Contains(appendFacts, "append_package_component_type_facts(") {
		t.Fatal("append_facts does not build the package-wide type catalog")
	}
	numeric := selfhostKizuFunctionBody(t, source, "fn append_numeric_package_closure(")
	if strings.Contains(numeric, "append_component_type_facts(") ||
		strings.Contains(numeric, "emitted_type_components") {
		t.Fatal("numeric function closure still owns component type discovery")
	}
	for _, forbidden := range []string{
		"append_loader_function_facts(", "append_diagnostic_function_facts(",
		"append_codegen_function_facts", "append_selected_helper_body(",
		"stdlib-return-generic ",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy selected/type-specific producer remains %s", forbidden)
		}
	}
}

// TestSelfhostLegacyAstTypeFactHelpersAreDeleted keeps the AST-shaped fact
// emitters from coming back. Each named helper recognised the compiler's own
// node structs by name and emitted layout for them directly; leaving any of
// them reachable would let AST types bypass the catalog the tests above pin.
func TestSelfhostLegacyAstTypeFactHelpersAreDeleted(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	for _, deadHelper := range []string{
		"append_ast_leaf_payload_struct_type_facts",
		"append_ast_struct_type_fact",
		"emit_ast_node_type_llvm_fact",
		"emit_ast_node_struct_field_facts",
		"emit_ast_node_struct_fields",
		"emit_ast_node_struct_field",
		"find_ast_node_struct",
		"find_ast_node_struct_in_program",
		"ast_node_struct_name_matches",
		"struct_name_is_variant_node",
	} {
		if strings.Contains(source, deadHelper) {
			t.Fatalf("legacy AST type-fact helper remains: %s", deadHelper)
		}
	}
}
