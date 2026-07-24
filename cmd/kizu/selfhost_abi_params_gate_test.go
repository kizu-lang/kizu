package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostAbiParamsGateOutput = "abi-params-spec\n" +
	"i8 byte;i64 count;%kizu.slice.u8 source;" +
	"%kizu.kizu.lexer.position pos;%kizu.kizu.lexer.token tok;" +
	"%kizu.selfhost.source.source_file file;" +
	"%kizu.selfhost.source.loader.local_record local;" +
	"%kizu.owned values;%kizu.owned table\n" +
	"abi-params-count\n" +
	"9\n"

// TestSelfhostAbiParamsGate executes the fact-driven ABI resolver through the
// production params-spec entry. It covers local/global shadowing, an import
// alias, a fully-qualified identity, arbitrary nominal structs, and multiple
// owned generic constructors without type-specific parameter branches.
func TestSelfhostAbiParamsGate(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(t, "selfhost::backend::compiled_abi_params::gate")
	if err != nil {
		t.Fatalf("abi params gate failed: %v\n%s", err, out)
	}
	if out != selfhostAbiParamsGateOutput {
		t.Fatalf("abi params gate output mismatch\nwant:\n%sgot:\n%s", selfhostAbiParamsGateOutput, out)
	}
}

func TestSelfhostAbiParamsResolvesBareLocalUnionABI(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_abi_params::gate_local_union_abi",
	)
	if err != nil {
		t.Fatalf("bare local union ABI gate failed: %v\n%s", err, out)
	}
	if out != "%kizu.kizu.ast.ast_data data\n" {
		t.Fatalf("bare local union ABI output mismatch: %q", out)
	}
}

func TestSelfhostExternalStructFieldUsesDeclaringModuleContext(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_types::gate_external_struct_field_type_context",
	)
	if err != nil {
		t.Fatalf("external struct field context gate failed: %v\n%s", err, out)
	}
	want := "alpha::Child\n%alpha.child\n" +
		"std::array::Array<Child>\nalpha::\nalpha::Child\n%alpha.child\n" +
		"alpha::\nalpha::Child\n%alpha.child\nalpha::Child\n%alpha.child\n"
	if out != want {
		t.Fatalf("external struct field context mismatch\nwant:\n%sgot:\n%s", want, out)
	}
}

func TestSelfhostExternalFunctionReturnUsesOwnerModuleContext(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_types::gate_external_function_return_type_context",
	)
	if err != nil {
		t.Fatalf("external function return context gate failed: %v\n%s", err, out)
	}
	want := "alpha::make\nalpha::\nItem\nalpha::Item\n%alpha.item\n" +
		"%alpha.item\nalpha::\nmake\n" +
		"alpha::Item\n%alpha.item\nalpha::\n"
	if out != want {
		t.Fatalf("external function return context mismatch\nwant:\n%sgot:\n%s", want, out)
	}
}

func TestSelfhostFunctionResolutionRejectsMissingOwnerAndDuplicateSignature(t *testing.T) {
	cases := []struct {
		entry string
		want  string
	}{
		{"gate_function_return_missing_owner", "function owner module not found"},
		{"gate_function_return_invalid_owner", "invalid function owner module"},
		{"gate_duplicate_function_signature", "duplicate exact fact"},
		{"gate_duplicate_function_owner", "duplicate exact fact"},
		{"gate_duplicate_body_call_target", "duplicate body call target"},
		{"gate_missing_body_call_target", "body call target not found"},
		{"gate_duplicate_body_call_builtin", "duplicate body call builtin"},
		{"gate_overlapping_body_call_classification", "overlapping source and builtin call"},
		{"gate_unknown_body_call_builtin_operation", "invalid body call builtin operation"},
		{"gate_mismatched_body_call_builtin_kind", "builtin kind and operation mismatch"},
	}
	for _, tc := range cases {
		out, err := runSelfhostAbiParamsGate(
			t, "selfhost::backend::compiled_mir_types::"+tc.entry,
		)
		if err == nil {
			t.Fatalf("function resolver accepted invalid facts for %s\n%s", tc.entry, out)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("function resolver error mismatch for %s: %v", tc.entry, err)
		}
	}
}

func TestSelfhostLegacyTypeAndStructFieldSuffixLookupsAreDeleted(t *testing.T) {
	lookup := readSelfhostFile(t, "../../selfhost/src/backend/compiled_fact_lookup.kizu")
	for _, forbidden := range []string{
		"lookup_type_llvm_by_prefix",
		"lookup_struct_field_by_prefix",
		"lookup_struct_field_index_by_prefix",
		"lookup_struct_field_type_by_prefix",
		"lookup_struct_field_name_by_prefix",
		"lookup_struct_field_by_suffix",
		"lookup_struct_field_type_by_suffix",
		"lookup_struct_field_name_by_suffix",
		"lookup_qualified_function_name_by_callee_suffix",
		"pub fn lookup_fact_value_by_prefix_or_empty(",
		"pub fn lookup_stdlib_return(",
		"pub fn lookup_stdlib_arg_type(",
		"pub fn enum_variant_tag_by_prefix(",
	} {
		if strings.Contains(lookup, forbidden) {
			t.Fatalf("legacy suffix-capable lookup remains: %s", forbidden)
		}
	}
	for _, required := range []string{
		"lookup_struct_field_exact_indexed",
		"lookup_struct_field_at_index_exact_indexed",
		"duplicate exact struct field",
	} {
		if !strings.Contains(lookup, required) {
			t.Fatalf("canonical exact field lookup missing: %s", required)
		}
	}
	mirTypes := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	if strings.Contains(mirTypes, "fn lower_stdlib_call_arg_types(") {
		t.Fatal("dead non-indexed stdlib arg type collector remains")
	}
	if strings.Contains(mirTypes, "cross_module_callee_qualified_name_or_empty") {
		t.Fatal("legacy cross-module call suffix resolver remains")
	}
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	for _, forbidden := range []string{
		"function-signature-return ast.",
		"function-signature-param ast.",
		"append_ast_receiver_method_return_facts",
		"append_ast_receiver_method_param_facts",
	} {
		if strings.Contains(executableFunctions, forbidden) {
			t.Fatalf("synthetic receiver signature producer remains: %s", forbidden)
		}
	}
}

func TestSelfhostAbiParamsGateUnsupportedType(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_abi_params::gate_unsupported_type",
	)
	if err == nil {
		t.Fatalf("abi params gate accepted unsupported type, want error\n%s", out)
	}
	if !strings.Contains(err.Error(), "compiled type resolver: type facts not found") {
		t.Fatalf("abi params gate error mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), "module=selfhost::backend::; type=Unregistered") {
		t.Fatalf("abi params gate error lacks exact lookup context: %v", err)
	}
}

func TestSelfhostAbiParamsRejectDuplicateAndConflictingFacts(t *testing.T) {
	for _, entry := range []string{
		"selfhost::backend::compiled_abi_params::gate_duplicate_type_fact",
		"selfhost::backend::compiled_abi_params::gate_conflicting_type_fact",
	} {
		out, err := runSelfhostAbiParamsGate(t, entry)
		if err == nil {
			t.Fatalf("ABI resolver accepted duplicate fact for %s\n%s", entry, out)
		}
		if !strings.Contains(err.Error(), "compiled type resolver: duplicate exact fact") {
			t.Fatalf("ABI resolver duplicate error mismatch for %s: %v", entry, err)
		}
	}
}

func TestSelfhostAbiParamsRejectInvalidEnumFacts(t *testing.T) {
	cases := []struct {
		entry string
		want  string
	}{
		{"gate_invalid_enum_representation", "invalid enum representation"},
		{"gate_variant_only_enum", "type facts not found"},
	}
	for _, tc := range cases {
		out, err := runSelfhostAbiParamsGate(
			t, "selfhost::backend::compiled_abi_params::"+tc.entry,
		)
		if err == nil {
			t.Fatalf("ABI resolver accepted invalid enum facts for %s\n%s", tc.entry, out)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("ABI resolver enum error mismatch for %s: %v", tc.entry, err)
		}
	}
}

func TestSelfhostUnionFactsRequireExactIdentity(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_fact_lookup::gate_exact_union_identity",
	)
	if err != nil {
		t.Fatalf("exact union identity gate failed: %v\n%s", err, out)
	}
	if out != "union-exact\n" {
		t.Fatalf("exact union identity output mismatch: %q", out)
	}

	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_fact_lookup::gate_union_suffix_rejected",
	)
	if err == nil {
		t.Fatalf("union suffix lookup was accepted\n%s", out)
	}
	if !strings.Contains(err.Error(), "compiled type resolver: type facts not found") {
		t.Fatalf("union suffix rejection mismatch: %v", err)
	}

	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_fact_lookup::gate_invalid_union_variant_facts_rejected",
	)
	if err == nil {
		t.Fatalf("union lookup accepted a malformed variant table\n%s", out)
	}
	if !strings.Contains(err.Error(), "variant tags must be ordered from zero") {
		t.Fatalf("union variant validation error mismatch: %v", err)
	}

	factLookup := readSelfhostFile(t, "../../selfhost/src/backend/compiled_fact_lookup.kizu")
	for _, required := range []string{
		"compiled_type_resolver::resolve_type_indexed(",
		"compiled_type_resolver::resolve_union_variant_indexed(",
	} {
		if !strings.Contains(factLookup, required) {
			t.Fatalf("union lookup does not use canonical validated owner %q", required)
		}
	}
	for _, removed := range []string{
		"resolve_union_identity_indexed(",
		"match_union_variant_suffix(",
		"union_name_separator(",
	} {
		if strings.Contains(factLookup, removed) {
			t.Fatalf("union lookup retained duplicate resolver/parser %q", removed)
		}
	}
}

func TestSelfhostFreeRuntimeABIContractIsExactAndFailClosed(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::ir::builtin_contract::free_runtime_abi_gate",
	)
	if err != nil {
		t.Fatalf("free runtime ABI gate failed: %v\n%s", err, out)
	}
	if out != "kizu_selfhost__slice_len\n[]u8\nkizu_rt_mem_page_allocator\n" {
		t.Fatalf("free runtime ABI output mismatch: %q", out)
	}

	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::ir::builtin_contract::reachable_free_runtime_abi_gate",
	)
	if err != nil {
		t.Fatalf("reachable free runtime ABI gate failed: %v\n%s", err, out)
	}
	if out != "kizu_rt_fs_read_file\n2\nIo\n[]u8\n"+
		"kizu_rt_io_write_stderr\n2\nIo\n[]u8\n" {
		t.Fatalf("reachable free runtime ABI output mismatch: %q", out)
	}

	contract := readSelfhostFile(t, "../../selfhost/src/ir/builtin_contract.kizu")
	for _, required := range []string{
		"pub struct FreeRuntimeAbi",
		"pub fn free_runtime_abi(canonical_identity: []u8)",
		"pub fn free_runtime_arg_type(",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("free runtime descriptor missing %q", required)
		}
	}

	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_program_llvm::generic_callsite_lowering_gate",
	)
	if err != nil {
		t.Fatalf("generic call-site lowering gate failed: %v\n%s", err, out)
	}
	if out != "generic-callsite-lowering-ok\n" {
		t.Fatalf("generic call-site lowering output mismatch: %q", out)
	}
	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_program_llvm::missing_generic_callsite_lowering_gate",
	)
	if err == nil {
		t.Fatalf("generic definition without concrete call-site lowering was accepted\n%s", out)
	}
	if !strings.Contains(err.Error(), "has no concrete function instance") {
		t.Fatalf("missing generic call-site lowering error mismatch: %v", err)
	}
	out, err = runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_program_llvm::formal_generic_callsite_lowering_gate",
	)
	if err != nil {
		t.Fatalf("concrete generic body instance was rejected: %v\n%s", err, out)
	}
	if out != "generic-body-instance-ok\n" {
		t.Fatalf("generic body instance output mismatch: %q", out)
	}
	if strings.Contains(contract, "free_runtime_arg_llvm_type") {
		t.Fatal("free runtime contract still owns backend LLVM type spellings")
	}
	types := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	for _, required := range []string{
		"builtin_contract::free_runtime_arg_type(callee_name, arg_index)",
		"compiled_type_lower::kizu_type_to_llvm_indexed(",
	} {
		if !strings.Contains(types, required) {
			t.Fatalf("free runtime ABI lowering does not consume type facts: missing %q", required)
		}
	}

	cases := []struct {
		entry string
		want  string
	}{
		{"free_runtime_arg_out_of_range_gate", "argument index out of range"},
		{"unknown_free_runtime_identity_gate", "free runtime symbol not found"},
	}
	for _, tc := range cases {
		out, err := runSelfhostAbiParamsGate(
			t, "selfhost::ir::builtin_contract::"+tc.entry,
		)
		if err == nil {
			t.Fatalf("free runtime ABI gate %s accepted invalid input\n%s", tc.entry, out)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("free runtime ABI gate %s error mismatch: %v", tc.entry, err)
		}
	}
}

func TestSelfhostStdlibReturnDropsLegacyFactWithoutLosingGenericConstructor(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t,
		"selfhost::backend::compiled_mir_types::"+
			"gate_generic_constructor_return_without_legacy_fact",
	)
	if err != nil {
		t.Fatalf("generic constructor return gate failed: %v\n%s", err, out)
	}
	if out != "std::array::Array<Item>\n" {
		t.Fatalf("generic constructor return output mismatch: %q", out)
	}
}

func TestSelfhostOwnedConstructorLocalUsesConcreteResult(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t,
		"selfhost::backend::compiled_mir_types::"+
			"gate_owned_constructor_local_uses_concrete_result",
	)
	if err != nil {
		t.Fatalf("owned constructor local gate failed: %v\n%s", err, out)
	}
}

func TestSelfhostLegacyStdlibFactChannelIsDeleted(t *testing.T) {
	lookup := readSelfhostFile(t, "../../selfhost/src/backend/compiled_fact_lookup.kizu")
	for _, forbidden := range []string{
		"lookup_stdlib_symbol_indexed",
		"lookup_stdlib_return_indexed",
		"lookup_stdlib_arg_type_indexed",
		"exact_stdlib_symbol_fact_indexed",
		`"stdlib-symbol "`,
		`"stdlib-return "`,
		"lookup_fact_value_by_prefix_or_empty(",
	} {
		if strings.Contains(lookup, forbidden) {
			t.Fatalf("legacy stdlib fact lookup remains %q", forbidden)
		}
	}

	formatGate := readSelfhostFile(t, "../../selfhost/src/backend/format_driver_gate.kizu")
	required := []string{
		"executable_functions::numeric_package_collector_facts()",
		"production external ABI entrypoint",
		"var lookup_index = try ir_index::build(ir_bytes);",
		"compiled_abi_params::append_params_spec_indexed(",
		"compiled_signature::derive_return_type_indexed(",
		"compiled_llvm::append_compiled_function_auto_return_indexed(",
	}
	for _, fragment := range required {
		if !strings.Contains(formatGate, fragment) {
			t.Fatalf("format gate does not consume canonical package facts: missing %q", fragment)
		}
	}
	if got := strings.Count(formatGate, "ir_index::build(ir_bytes)"); got != 1 {
		t.Fatalf("format gate must build exactly one caller-owned IR index, got %d", got)
	}
	for _, forbidden := range []string{
		"stdlib-symbol", "append_legacy_stdlib_symbol_facts",
		"append_external_callee_facts", "append_external_callee_signature",
		"compiled_abi_params::append_params_spec(&",
		"compiled_llvm::append_compiled_function_auto(&",
	} {
		if strings.Contains(formatGate, forbidden) {
			t.Fatalf("format gate retains parallel fact producer %q", forbidden)
		}
	}
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	for _, required := range []string{
		"compiled_type_resolver::call_target_indexed(",
		"compiled_type_resolver::resolve_call_indexed(",
		"name_function.canonical_identity",
		"guard_function.canonical_identity",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("parser helper lowering does not use canonical call targets: missing %q", required)
		}
	}
	index := readSelfhostFile(t, "../../selfhost/src/backend/ir_index.kizu")
	if strings.Contains(index, "pub fn lookup_fact_value_by_prefix_or_empty(") {
		t.Fatal("IR index retains the producer-free optional fact lookup")
	}
}

func TestSelfhostAbiParamsHasNoNominalTypeTable(t *testing.T) {
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")
	for _, forbidden := range []string{
		`"SourceFile"`, `"Token"`, `"ConstructorFacts"`, `"TypeRecord"`,
		`"std::array::Array<"`, `"std::string::String"`,
	} {
		if strings.Contains(selfhostKizuFunctionBody(t, abi, "pub fn append_params_spec_indexed("), forbidden) {
			t.Fatalf("ABI parameter lowering regained nominal type branch %s", forbidden)
		}
	}
	if !strings.Contains(abi, "compiled_type_resolver::resolve_indexed(") {
		t.Fatal("ABI parameter lowering does not use canonical type resolver")
	}
}

func runSelfhostAbiParamsGate(t *testing.T, entry string) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}
