package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

func TestSelfhostProductionFunctionTargetsUseOwnedProjections(t *testing.T) {
	root := "../../selfhost/src"
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".kizu") {
			return nil
		}
		name := entry.Name()
		if name == "package_function_identity.kizu" || strings.Contains(name, "_gate") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, direct := range []string{".component.value", ".function.value"} {
			if strings.Contains(string(source), direct) {
				t.Errorf("production FunctionTarget consumer %s directly uses %s", path, direct)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func countEveryEmittedBodyCallClassificationFailures(t *testing.T, facts string) int {
	t.Helper()
	bodyCalls := map[string]int{}
	classifications := map[string]int{}
	malformedClassifications := 0
	unknownClassifications := 0
	for _, line := range strings.Split(facts, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 10 && fields[0] == "body-node" &&
			fields[5] == "kind" && fields[6] == "Call" && fields[7] == "span" {
			bodyCalls[fields[1]+"\x00"+fields[8]+"\x00"+fields[9]]++
		}
		if len(fields) > 0 && fields[0] == "body-call-target" {
			if len(fields) != 5 {
				malformedClassifications++
				continue
			}
			classifications[fields[1]+"\x00"+fields[2]+"\x00"+fields[3]]++
		}
		if len(fields) > 0 && fields[0] == "body-call-builtin" {
			if len(fields) != 7 {
				malformedClassifications++
				continue
			}
			classifications[fields[1]+"\x00"+fields[2]+"\x00"+fields[3]]++
			kind, operation, identity := fields[4], fields[5], fields[6]
			if (kind != "language" && kind != "runtime" && kind != "external-shape") ||
				operation == "none" || operation == "unknown" {
				unknownClassifications++
			}
			if (operation == "free-runtime" && identity == "-") ||
				(operation != "free-runtime" && identity != "-") {
				malformedClassifications++
			}
		}
	}
	invalidBodyCalls := 0
	firstInvalidBodyCall := ""
	for key, count := range bodyCalls {
		if count != 1 || classifications[key] != 1 {
			invalidBodyCalls++
			if firstInvalidBodyCall == "" {
				firstInvalidBodyCall = key
			}
		}
	}
	orphanClassifications := 0
	for key, count := range classifications {
		if count != 1 || bodyCalls[key] != 1 {
			orphanClassifications++
		}
	}
	if len(bodyCalls) == 0 || malformedClassifications != 0 ||
		unknownClassifications != 0 || invalidBodyCalls != 0 ||
		orphanClassifications != 0 {
		t.Errorf(
			"emitted Call classification gate: calls=%d classifications=%d "+
				"invalid-calls=%d first-invalid=%q orphan-classifications=%d "+
				"malformed=%d unknown=%d",
			len(bodyCalls), len(classifications), invalidBodyCalls,
			firstInvalidBodyCall, orphanClassifications,
			malformedClassifications, unknownClassifications,
		)
		return 1
	}
	return 0
}

// TestSelfhostNumericPackageCollectorBehavior resolves only the production
// constructor-facts component against the complete numeric package catalog,
// then emits the real collect_checked closure without source-text selection.
func TestSelfhostNumericPackageCollectorBehavior(t *testing.T) {
	requirePackageIdentityGate(t)
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	const entry = "selfhost::ir::executable_functions::numeric_package_collector_gate"
	err = interp.New(&out).RunEntry(program, entry)
	if err != nil {
		t.Fatalf("numeric collector gate failed: %v\n%s", err, out.String())
	}
	facts := out.String()
	if countEveryEmittedBodyCallClassificationFailures(t, facts) != 0 {
		return
	}
	for _, fact := range []string{
		"function-type-parameter std::array::Array 0 T",
		"function-type-parameter std::map::Map 0 K",
		"function-type-parameter std::map::Map 1 V",
	} {
		if count := strings.Count(facts, fact+"\n"); count != 1 {
			t.Fatalf("numeric closure emitted %q %d times, want exactly once", fact, count)
		}
	}
	signatureCounts := map[string]int{}
	ownerCounts := map[string]int{}
	callTargetFound := false
	for _, line := range strings.Split(facts, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "function-signature-return" {
			signatureCounts[fields[1]]++
		}
		if len(fields) == 3 && fields[0] == "function-owner-module" {
			ownerCounts[fields[1]]++
		}
		if len(fields) == 5 && fields[0] == "body-call-target" &&
			fields[1] == "selfhost::types::constructor_facts::collect_checked" &&
			fields[4] == "selfhost::types::constructor_facts::collect_node" {
			callTargetFound = true
		}
	}
	for name, count := range signatureCounts {
		if count != 1 {
			t.Fatalf("function signature %s emitted %d times, want exactly once", name, count)
		}
		if count := ownerCounts[name]; count != 1 {
			t.Fatalf("function signature %s has %d owner facts, want exactly one", name, count)
		}
	}
	if !callTargetFound {
		t.Fatal("resolved collect_checked -> collect_node body-call-target fact missing")
	}
	requireSourceFragments(t, "ConstructorFacts source ABI facts", facts, []string{
		"type-llvm selfhost::types::constructor_facts::ConstructorFacts " +
			"%kizu.selfhost.types.constructor_facts.constructor_facts",
		"struct-field selfhost::types::constructor_facts::ConstructorFacts 0 " +
			"node_starts std::array::Array<i64>",
		"struct-field selfhost::types::constructor_facts::ConstructorFacts 1 " +
			"node_ends std::array::Array<i64>",
		"struct-field selfhost::types::constructor_facts::ConstructorFacts 2 " +
			"constructor_ids std::array::Array<i64>",
		"struct-field selfhost::types::constructor_facts::ConstructorFacts 3 " +
			"type_arg0_ids std::array::Array<i64>",
		"struct-field selfhost::types::constructor_facts::ConstructorFacts 4 " +
			"type_arg0_storage_abis std::array::Array<i64>",
	})
	requireSourceFragments(t, "TypeRecord source ABI facts", facts, []string{
		"type-llvm selfhost::types::primitive_type::TypeRecord " +
			"%kizu.selfhost.types.primitive_type.type_record",
		"struct-field selfhost::types::primitive_type::TypeRecord 0 identity i64",
		"struct-field selfhost::types::primitive_type::TypeRecord 1 kind i64",
		"struct-field selfhost::types::primitive_type::TypeRecord 2 bit_width i64",
		"struct-field selfhost::types::primitive_type::TypeRecord 3 signed bool",
	})
	for _, name := range []string{
		"selfhost::cli::check::fast_diagnostics_parsed_file",
		"selfhost::cli::execute::render_checked_run_artifact",
		"selfhost::ir::code_render::render_run_artifact",
		"selfhost::ir::codegen::lower_code_module",
		"selfhost::parser::format::format_source",
		"selfhost::backend::hosted::artifact_path",
		"selfhost::ir::codegen::metadata_line",
		"std::kizu::parser::parse_program",
		"selfhost::types::constructor_facts::collect_checked",
		"selfhost::types::constructor_facts::append_identity",
		"std::kizu::ast::ast_get",
		"std::kizu::ast::ast_child_at",
	} {
		marker := "function-signature-return " + name + " "
		if count := strings.Count(facts, marker); count != 1 {
			t.Fatalf(
				"numeric closure emitted %s definition %d times, want exactly once\n%s",
				name, count, facts,
			)
		}
	}
	for _, name := range []string{
		"selfhost::cli::check::fast_diagnostics_parsed_file",
		"selfhost::cli::execute::render_checked_run_artifact",
		"std::kizu::parser::parse_program",
		"selfhost::parser::format::format_source",
		"selfhost::backend::hosted::artifact_path",
		"selfhost::ir::codegen::metadata_line",
	} {
		marker := "external-abi-entrypoint " + name
		if count := strings.Count(facts, marker); count != 1 {
			t.Fatalf("external ABI entrypoint %s emitted %d times, want exactly once", name, count)
		}
	}
	if count := strings.Count(facts, "package-dependency "); count == 0 {
		t.Fatal("numeric collector closure emitted no dependency records")
	}
	if count := strings.Count(facts, "package-definition "); count == 0 {
		t.Fatal("numeric collector closure emitted no target definitions")
	}
	if count := strings.Count(facts, "package-reference "); count == 0 {
		t.Fatal("numeric collector closure emitted no target references")
	}
}

// requirePackageIdentityGate keeps the three interpreted numeric-package-collector gates out
// of the daily tier. Each loads and checks the selfhost package and then runs a gate entry
// through the Go interpreter with interp.New(...).RunEntry(...) --  the shape
// docs/selfhost-test-tiers.md excludes from `go test ./...` by policy and CLAUDE.md calls
// debug-only. Measured:
//
//	...BackendConsumerRejectsWrongIDs   1533s
//	...BackendConsumer                   510s
//	...Behavior                          244s
//	                                    -----
//	                                    2287s of the cmd/kizu package's 2383s
//
// At that cost `go test ./...` cannot finish inside the pre-commit hook's 10-minute timeout
// no matter what the code does, so the hook could never pass and --no-verify became routine.
// Skipping them by default drops the Behavior gate's positive fact assertions from the daily
// run; that coverage is still reachable through the command below, and the alternative was a
// daily tier nobody could run.
//
// There is deliberately no just recipe, for the same reason the run tape and render gates have
// none. Run it directly and read the log rather than piping it through tail:
//
//	KIZU_RUN_SELFHOST_PACKAGE_IDENTITY=1 go test -timeout=60m ./cmd/kizu \
//	  -run 'TestSelfhostNumericPackageCollector' -count=1 -v > gate.log 2>&1
func requirePackageIdentityGate(t *testing.T) {
	t.Helper()
	if os.Getenv("KIZU_RUN_SELFHOST_PACKAGE_IDENTITY") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_PACKAGE_IDENTITY=1 to run the interpreted package identity gates")
	}
}

// TestSelfhostNumericPackageCollectorBackendConsumer crosses the owned-file backend boundary
// and pins the next interpreted-consumer blocker after ConstructorFacts ABI mapping: lowering
// the checked producer reaches a mutable struct-field assignment that the interpreter does not
// yet represent as a struct value. The native stage path is checked separately from this gate.
func TestSelfhostNumericPackageCollectorBackendConsumer(t *testing.T) {
	requirePackageIdentityGate(t)
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	const entry = "selfhost::backend::package_dependency_edge_gate::consume_constructor_facts_gate"
	err = interp.New(&out).RunEntry(program, entry)
	if err == nil || !strings.Contains(err.Error(), "field assignment expects struct") {
		t.Fatalf(
			"production dependency consumer error = %v, want mutable struct-field blocker\n%s",
			err,
			out.String(),
		)
	}
}

// TestSelfhostNumericPackageCollectorBackendConsumerRejectsWrongIDs guards both numeric identities.
func TestSelfhostNumericPackageCollectorBackendConsumerRejectsWrongIDs(t *testing.T) {
	requirePackageIdentityGate(t)
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ entry, want string }{
		{"reject_constructor_facts_bad_caller_gate", "package dependency numeric ID out of bounds"},
		{"reject_constructor_facts_bad_target_gate", "package dependency numeric ID out of bounds"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			var out bytes.Buffer
			entry := "selfhost::backend::package_dependency_edge_gate::" + tc.entry
			err := interp.New(&out).RunEntry(program, entry)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("consumer error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestSelfhostPackageResolverClassificationBehavior exercises the production
// resolver boundary for package dependencies and deliberate runtime omissions.
func TestSelfhostPackageResolverClassificationBehavior(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		module    string
		entry     string
		wantError string
	}{
		{name: "known runtime builtin is explicitly classified", entry: "package_resolver_builtin_gate"},
		{name: "semantic TypeIds are structurally interned", entry: "package_type_store_structural_interning_gate"},
		{name: "generic owner identity is a canonical IR token", entry: "package_generic_owner_identity_canonical_gate"},
		{name: "free runtime return schema preserves fallibility", entry: "package_resolver_free_runtime_return_type_gate"},
		{name: "dense component function ranges preserve exact lookup", entry: "package_catalog_dense_function_range_gate"},
		{name: "function name spans use ordered binary search", entry: "package_catalog_function_span_binary_search_gate"},
		{
			name:      "function name spans reject source order violations",
			entry:     "package_catalog_reject_unordered_function_span_gate",
			wantError: "package function name spans must register in source order",
		},
		{
			name:      "function name spans reject overlap",
			entry:     "package_catalog_reject_overlapping_function_span_gate",
			wantError: "package function name spans must not overlap",
		},
		{name: "exact function indexes preserve collisions and owner identity", entry: "package_catalog_exact_index_gate"},
		{
			name:      "exact function lookup fails closed before finalization",
			entry:     "package_catalog_exact_index_unfinalized_gate",
			wantError: "exact name index is not finalized",
		},
		{
			name:      "exact function lookup fails closed for a corrupt row",
			entry:     "package_catalog_exact_index_corrupt_row_gate",
			wantError: "exact name index contains invalid row",
		},
		{
			name:      "duplicate exact function identity is rejected",
			entry:     "package_catalog_exact_index_duplicate_gate",
			wantError: "duplicate package function",
		},
		{
			name:      "finalized exact function index rejects mutation",
			entry:     "package_catalog_exact_index_reject_mutation_gate",
			wantError: "package function catalog already finalized",
		},
		{
			name:      "exact function index rejects unsafe hash capacity",
			entry:     "package_catalog_exact_index_capacity_overflow_gate",
			wantError: "exact name index capacity overflow",
		},
		{
			name:      "late function registration cannot invalidate dense ranges",
			entry:     "package_catalog_reject_late_function_gate",
			wantError: "package functions must register contiguously by component",
		},
		{name: "function type and import rows stay component-owned", entry: "package_catalog_component_owned_ranges_gate"},
		{
			name:      "import collection rejects a skipped component",
			entry:     "package_catalog_reject_out_of_order_import_begin_gate",
			wantError: "package imports must register in component order",
		},
		{
			name:      "import collection rejects overlapping active components",
			entry:     "package_catalog_reject_overlapping_import_begin_gate",
			wantError: "package imports must register in component order",
		},
		{name: "bare declared type stays component-local", entry: "package_declared_type_bare_local_gate"},
		{name: "qualified declared type follows exact import alias", entry: "package_declared_type_import_alias_gate"},
		{name: "canonical declared type wins over import alias", entry: "package_declared_type_canonical_precedence_gate"},
		{name: "Channel generic recv classification", entry: "package_resolver_channel_recv_gate"},
		{name: "Array generic get classification", entry: "package_resolver_array_get_gate"},
		{name: "Array generic reserve classification", entry: "package_resolver_array_reserve_gate"},
		{
			name:  "generic source body builtin classification",
			entry: "package_resolver_generic_source_body_builtin_gate",
		},
		{
			name:  "owned constructor descriptor excludes ordinary factories",
			entry: "package_resolver_owned_constructor_descriptor_gate",
		},
		{name: "local Call return feeds receiver type", entry: "package_resolver_local_call_receiver_gate"},
		{name: "comptime Call return feeds receiver type", entry: "package_resolver_comptime_receiver_gate"},
		{
			name:  "cross-component Match payload keeps declaration owner",
			entry: "package_resolver_cross_component_match_payload_gate",
		},
		{
			name:  "qualified method return and field payload keep declaration owners",
			entry: "package_resolver_qualified_method_field_match_gate",
		},
		{
			name:  "direct qualified field and Match use catalog declaration identity",
			entry: "package_resolver_direct_qualified_field_match_gate",
		},
		{
			name:  "generic nominal struct substitutes arbitrary nested field types",
			entry: "package_resolver_imported_generic_constructor_result_gate",
		},
		{
			name:  "generic nominal union substitutes nested payload types",
			entry: "package_resolver_nested_generic_union_payload_gate",
		},
		{
			name:      "declared generic type application rejects arity mismatch",
			entry:     "package_resolver_generic_arity_mismatch_gate",
			wantError: "declared type argument arity mismatch",
		},
		{
			name:      "prepared declared type rejects unknown formal",
			entry:     "package_resolver_unknown_generic_formal_gate",
			wantError: "exact type declaration not found",
		},
		{
			name:      "declared generic type rejects duplicate formal",
			entry:     "package_resolver_duplicate_declared_formal_gate",
			wantError: "duplicate declared type parameter",
		},
		{
			name:  "function parameter layout owns receiver and runtime ordinals",
			entry: "package_catalog_function_param_layout_gate",
		},
		{
			name:      "function receiver must be ordinal zero",
			entry:     "package_catalog_late_receiver_gate",
			wantError: "function receiver parameter must be first",
		},
		{name: "nested imported component type resolves exactly", entry: "package_resolver_nested_import_type_gate"},
		{name: "binary initializer preserves exact operand and result types", entry: "package_resolver_binary_initializer_gate"},
		{
			name:      "binary initializer rejects mismatched exact operand types",
			entry:     "package_resolver_binary_operand_mismatch_gate",
			wantError: "binary operator operands must have same type",
		},
		{name: "union match expression binds payload and accepts trailing wildcard", entry: "package_resolver_match_expression_gate"},
		{name: "enum match expression validates exact exhaustive tags", entry: "package_resolver_enum_match_expression_gate"},
		{
			name:      "match expression rejects differing exact arm types",
			entry:     "package_resolver_match_expression_type_mismatch_gate",
			wantError: "match expression arm types differ",
		},
		{name: "if expression derives exact type from scoped block tails", entry: "package_resolver_if_expression_block_gate"},
		{name: "namespace enum variant supplies exact binary operand type", entry: "package_resolver_namespace_variant_binary_gate"},
		{name: "payloadless union namespace variant supplies exact value type", entry: "package_resolver_payloadless_union_variant_binary_gate"},
		{
			name:      "catalog rejects duplicate enum tags before wildcard match",
			entry:     "package_resolver_duplicate_enum_tag_gate",
			wantError: "duplicate package enum tag",
		},
		{
			name:      "plain error union call does not unwrap receiver",
			entry:     "package_resolver_plain_error_union_receiver_gate",
			wantError: "unresolved receiver package method target",
		},
		{name: "try unwraps exact error union receiver", entry: "package_resolver_try_error_union_receiver_gate"},
		{name: "impl Self and borrowed Self resolve exactly", entry: "package_resolver_impl_self_gate"},
		{
			name:      "top level first self parameter is not a method",
			entry:     "package_resolver_top_level_self_param_not_method_gate",
			wantError: "unresolved receiver package method target",
		},
		{name: "declared method wins builtin spelling collision", entry: "package_resolver_declared_builtin_collision_gate"},
		{name: "task spawn result follows worker return", entry: "package_resolver_task_spawn_result_gate"},
		{
			name:      "function body shares parameter lexical scope",
			entry:     "package_resolver_function_param_shadow_gate",
			wantError: "resolver duplicate local in lexical scope",
		},
		{name: "ordinary nested block may shadow outer binding", entry: "package_resolver_nested_block_shadow_gate"},
		{
			name:      "local binding shadows top level callee",
			entry:     "package_resolver_local_callee_shadow_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name:      "for body shares induction binding scope",
			entry:     "package_resolver_for_index_shadow_gate",
			wantError: "resolver duplicate local in lexical scope",
		},
		{
			name:      "match arm shares payload binding scope",
			entry:     "package_resolver_match_payload_shadow_gate",
			wantError: "resolver duplicate local in lexical scope",
		},
		{name: "generic task spawn result substitutes worker return", entry: "package_resolver_generic_task_spawn_result_gate"},
		{name: "raw pointer dereference keeps nested generic identity", entry: "package_resolver_raw_pointer_nested_generic_gate"},
		{
			name:      "type application receiver has bare type value type",
			entry:     "package_resolver_type_value_gate",
			wantError: "unresolved receiver package method target",
		},
		{name: "pointer cast result uses exact static type argument", entry: "package_resolver_pointer_cast_result_gate"},
		{name: "comparison does not cross generic cast syntax", entry: "package_resolver_comparison_generic_cast_gate"},
		{name: "bare generic call accepts nested static type", entry: "package_resolver_bare_nested_generic_call_gate"},
		{
			name:      "unknown local receiver method",
			entry:     "package_resolver_unknown_local_method_gate",
			wantError: "unresolved receiver package method target",
		},
		{
			name: "unknown builtin is not hidden", entry: "package_resolver_unknown_builtin_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name: "unknown local call", entry: "package_resolver_unknown_local_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name: "unknown qualified component", entry: "package_resolver_unknown_component_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name: "unimported alias does not resolve by component spelling", entry: "package_resolver_unimported_alias_gate",
			wantError: "unresolved qualified package call target",
		},
		{name: "std source function resolves", entry: "package_resolver_std_function_gate"},
		{
			name:      "missing function in catalogued std component",
			entry:     "package_resolver_missing_std_function_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name:      "missing method on catalogued std owner",
			entry:     "package_resolver_missing_std_method_gate",
			wantError: "unresolved receiver package method target",
		},
		{
			name: "unresolved qualified call", entry: "package_resolver_unresolved_qualified_gate",
			wantError: "unresolved qualified package call target",
		},
		{
			name: "unknown typed receiver method", entry: "package_resolver_unknown_typed_method_gate",
			wantError: "unresolved receiver package method target",
		},
		{
			name: "same spelling in wrong component", entry: "package_resolver_wrong_component_gate",
			wantError: "unresolved qualified package call target",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			module := tc.module
			if module == "" {
				module = "package_resolver_gates"
			}
			err := interp.New(&out).RunEntry(program, "selfhost::ir::"+module+"::"+tc.entry)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("resolver gate failed: %v\n%s", err, out.String())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("resolver error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

// TestSelfhostPackageDependencyIdentityFlow guards the numeric handoff boundaries.
func TestSelfhostPackageDependencyIdentityFlow(t *testing.T) {
	identity := readSelfhostFile(t, "../../selfhost/src/ir/package_function_identity.kizu")
	catalog := readSelfhostFile(t, "../../selfhost/src/ir/package_catalog.kizu")
	definitionLookup := readSelfhostFile(t, "../../selfhost/src/ir/package_definition_lookup.kizu")
	lookup := readSelfhostFile(t, "../../selfhost/src/ir/package_exact_lookup.kizu")
	callFacts := readSelfhostFile(t, "../../selfhost/src/ir/package_call_facts.kizu")
	graph := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency_graph.kizu")
	programLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	typeDeclarations := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_declarations.kizu")

	requireSourceFragments(t, "package function identity", identity, []string{
		"pub struct ComponentId",
		"pub struct FunctionId",
		"pub struct DependencyRecord",
		"pub fn target_component_value(target: &FunctionTarget) -> i64",
		"pub fn target_function_value(target: &FunctionTarget) -> i64",
	})
	componentProjection := dependencyFunctionBody(t, identity, "target_component_value")
	functionProjection := dependencyFunctionBody(t, identity, "target_function_value")
	if !strings.Contains(componentProjection, "return target.component.value") ||
		!strings.Contains(functionProjection, "return target.function.value") {
		t.Fatal("FunctionTarget projections are not typed in their owning module")
	}
	targetSlot := dependencyFunctionBody(t, catalog, "target_slot")
	for _, projection := range []string{
		"package_function_identity::target_component_value(&target)",
		"package_function_identity::target_function_value(&target)",
	} {
		if count := strings.Count(targetSlot, projection); count != 1 {
			t.Fatalf("target_slot projection %q count = %d, want 1", projection, count)
		}
	}
	if strings.Contains(targetSlot, "target.component.value") ||
		strings.Contains(targetSlot, "target.function.value") {
		t.Fatal("target_slot directly projects cross-module FunctionTarget fields")
	}
	for label, source := range map[string]string{
		"package function identity": identity,
		"package exact lookup":      lookup,
		"package call facts":        callFacts,
		"package dependency graph":  graph,
	} {
		for _, legacyAPI := range []string{
			"pub fn dependency_target(",
			"pub fn source_function_target(",
			"pub fn declared_type_component(",
			"pub struct CallDependency",
			"pub fn resolved_call_target(",
			"pub fn resolved_call_node(",
			"pub fn builtin_call_node(",
			"pub fn builtin_call_operation(",
			"pub fn append_resolved_call_targets(",
			"pub fn dependency_record(",
			"pub fn target_local_name(",
		} {
			if strings.Contains(source, legacyAPI) {
				t.Errorf("%s retained dead package API %q", label, legacyAPI)
			}
		}
	}
	requireSourceFragments(t, "package definition lookup", definitionLookup, []string{
		"pub fn definition_node(",
	})
	requireSourceFragments(t, "package exact lookup", lookup, []string{
		"pub fn resolve_call(",
	})
	qualifiedRoute := dependencyFunctionBody(t, lookup, "find_qualified_function")
	requireSourceFragments(t, "shape-directed qualified call routing", qualifiedRoute, []string{
		"var second_separator = separator + 2",
		"if second_separator + 1 >= std::mem::len(qualified)",
		"package_catalog::component_import_start_at(",
		"var component = 0",
	})
	if aliasRoute, canonicalRoute := strings.Index(
		qualifiedRoute, "if second_separator + 1 >= std::mem::len(qualified)",
	), strings.Index(qualifiedRoute, "var component = 0"); aliasRoute < 0 ||
		canonicalRoute < 0 || aliasRoute > canonicalRoute {
		t.Fatal("alias-qualified calls do not route to imports before canonical component scan")
	}
	requireSourceFragments(t, "package dependency graph", graph, []string{
		"pub fn append_closure_targets(",
	})
	typeRoute := dependencyFunctionBody(t, lookup, "declared_type_identity")
	requireSourceFragments(t, "shape-directed declared type routing", typeRoute, []string{
		"if !contains_double_colon(constructor)",
		"return exact_local_type_identity(",
		"let canonical = try exact_canonical_type_identity",
		"return exact_imported_type_identity(",
	})
	if local, canonical := strings.Index(typeRoute, "return exact_local_type_identity("),
		strings.Index(typeRoute, "let canonical = try exact_canonical_type_identity"); local < 0 || canonical < 0 || local > canonical {
		t.Fatal("bare declared type route does not return before canonical lookup")
	}
	for _, forbidden := range []string{
		"%kizu.selfhost.types.constructor_facts.constructor_facts = type",
		"%kizu.selfhost.types.primitive_type.type_record = type",
	} {
		if strings.Contains(llvm, forbidden) {
			t.Fatalf("LLVM preamble retained hardcoded reachable declaration %q", forbidden)
		}
	}
	requireSourceFragments(t, "reachable declaration registry", typeDeclarations, []string{
		"pub fn append_reachable_declarations(",
		"lower_and_declare(",
	})
	for _, fragment := range []string{
		"import selfhost::ir::package_function_identity;",
		"pub fn append_reachable_functions(",
		"dependency_record_from_line(line)",
		"package identity table size overflow",
		"package identity table exceeds fact input",
	} {
		if !strings.Contains(programLLVM, fragment) {
			t.Errorf("compiled_program_llvm dependency consumption missing %q", fragment)
		}
	}
	body := dependencyFunctionBody(t, graph, "append_closure_targets")
	for _, forbidden := range []string{
		"equal_bytes", "starts_with", "callee", "symbol", "llvm",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("numeric closure BFS must not inspect %q", forbidden)
		}
	}
	assertSparsePackageDependencyConsumer(t, programLLVM)
}

// assertSparsePackageDependencyConsumer verifies numeric claims use sparse identity pairs.
func assertSparsePackageDependencyConsumer(t *testing.T, programLLVM string) {
	t.Helper()
	consumer := dependencyFunctionBody(t, programLLVM, "append_reachable_functions")
	dependencyLoopStart := strings.Index(consumer, "var dependency_index = 0")
	emissionLoopStart := strings.Index(consumer, "var emitted_count = 0")
	if dependencyLoopStart < 0 || emissionLoopStart <= dependencyLoopStart {
		t.Fatal("compiled program numeric dependency validation loop missing")
	}
	dependencyLoop := consumer[dependencyLoopStart:emissionLoopStart]
	for _, forbidden := range []string{"equal_bytes", "callee", "symbol", "local_name"} {
		if strings.Contains(strings.ToLower(dependencyLoop), forbidden) {
			t.Errorf("compiled program dependency consumption must not inspect %q", forbidden)
		}
	}
	if !strings.Contains(consumer, `return error("package dependency caller definition missing")`) {
		t.Fatal("compiled program dependency consumer does not reject a missing or wrong numeric caller")
	}
	for _, fragment := range []string{
		"function_stride",
		"identity_slot_count = definition_components.len() + reference_components.len()",
		"numeric_target_index(",
		"&definition_components, &definition_functions",
		"&reference_components, &reference_functions",
	} {
		if !strings.Contains(consumer, fragment) {
			t.Fatalf("compiled program dependency consumer sparse numeric claimed set missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"component_count * function_stride",
		"var definitions = std::array::Array",
		"var references = std::array::Array",
		"claimed_components",
		"claimed_functions",
	} {
		if strings.Contains(consumer, forbidden) {
			t.Fatalf("compiled program dependency consumer reintroduced dense numeric allocation %q", forbidden)
		}
	}
}

// TestSelfhostCheckedConstructorHandoffPinsAtomicABI keeps the checked producer,
// the checked identity/storage-ABI handoff, and run lowering joined by numeric identities.
// Atomic selection must not regress to spelling checks in codegen.
func TestSelfhostCheckedConstructorHandoffPinsAtomicABI(t *testing.T) {
	constructorFacts := readSelfhostFile(t, "../../selfhost/src/types/constructor_facts.kizu")
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")

	requireSourceFragments(t, "checked constructor producer", constructorFacts, []string{
		"pub fn collect_checked(",
		"facts: &var ConstructorFacts",
		"pub fn append_identity(",
		"try facts.node_starts.append(start)",
		"try facts.node_ends.append(end)",
		"try facts.constructor_ids.append(constructor_id)",
		"try facts.type_arg0_ids.append(type_arg0_id)",
		"try facts.type_arg0_storage_abis.append(type_arg0_storage_abi)",
	})
	for _, name := range []string{"scratch_constructor_kind", "lower_code_runtime_constructor"} {
		body := dependencyFunctionBody(t, codegen, name)
		for _, forbidden := range []string{"equal_bytes", "starts_with", `"Atomic"`, `"std::atomic"`} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s reintroduced constructor spelling selection %q", name, forbidden)
			}
		}
	}
	constructorLookup := dependencyFunctionBody(t, codegen, "scratch_constructor_kind")
	requireSourceFragments(t, "numeric constructor lookup", constructorLookup, []string{
		"fact_start == node.span.start",
		"fact_end == node.span.end",
		"return try args_scratch.get(fact_pos + 2)",
	})
	atomicRender := dependencyFunctionBody(t, render, "render_atomic_bool_new")
	requireSourceFragments(t, "Atomic<bool> LLVM semantics", atomicRender, []string{
		"getelementptr i8, ptr %atomic",
		"zext i1 %v",
		"store atomic i8 %ab",
		"seq_cst, align 1",
	})
}

// TestSelfhostPackageDependencyIdentityUsesBothNumericIDs rejects spelling-only identity.
func TestSelfhostPackageDependencyIdentityUsesBothNumericIDs(t *testing.T) {
	identity := readSelfhostFile(t, "../../selfhost/src/ir/package_function_identity.kizu")
	graph := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency_graph.kizu")
	catalog := readSelfhostFile(t, "../../selfhost/src/ir/package_catalog.kizu")
	for _, tc := range []struct{ source, name string }{
		{identity, "same_target"},
		{graph, "queue_append"},
		{catalog, "target_slot"},
	} {
		body := dependencyFunctionBody(t, tc.source, tc.name)
		if !strings.Contains(body, "component") || !strings.Contains(body, "function") {
			t.Fatalf("%s does not distinguish same-spelling functions by both numeric IDs", tc.name)
		}
	}
	lookup := readSelfhostFile(t, "../../selfhost/src/ir/package_exact_lookup.kizu")
	resolveBody := dependencyFunctionBody(t, lookup, "resolve_call")
	if !strings.Contains(resolveBody, "unresolved call target component") ||
		!strings.Contains(resolveBody, "unresolved call target function") {
		t.Fatal("resolver does not reject unresolved component/function targets")
	}
}

// TestSelfhostPackageCallResolverOwnsAstChildAtEdge pins the first real
// cross-component numeric dependency: a parameter value method on Ast resolves
// to the child_at ImplDecl method in std::kizu::ast, never to a same-name method
// in another component.
func TestSelfhostPackageCallResolverOwnsAstChildAtEdge(t *testing.T) {
	lookup := readSelfhostFile(t, "../../selfhost/src/ir/package_exact_lookup.kizu")
	expression := readSelfhostFile(t, "../../selfhost/src/ir/package_expression_type_resolution.kizu")
	executable := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")

	resolver := dependencyFunctionBody(t, expression, "resolve_field_callee")
	requireSourceFragments(t, "parameter value-method resolver", resolver, []string{
		"infer_exact_expression_type", "without_borrow", "declared_impl_method_entry", "namespace",
	})
	methodLookup := dependencyFunctionBody(t, lookup, "declared_impl_method_entry")
	if !strings.Contains(methodLookup, "package_catalog::exact_declared_impl_method_entry(") {
		t.Fatal("numeric method lookup does not use the declaration-identity exact index")
	}
	if strings.Contains(methodLookup, "function_owner_type_at") {
		t.Fatal("method resolver fell back to owner spelling instead of declaration identity")
	}
	for _, fragment := range []string{
		"package_call_resolution::resolve_package_calls_with_types(",
		"package_call_resolution::append_resolved_dependencies(",
		"append_numeric_package_closure(",
		"package_dependency_graph::queue_append_dependencies(",
		"package_definition_lookup::definition_node(",
		"package_dependency_graph::DependencyGraph",
	} {
		if !strings.Contains(executable, fragment) {
			t.Fatalf("append_facts does not consume resolver numeric targets: missing %q", fragment)
		}
	}
	closureEntry := dependencyFunctionBody(t, executable, "append_numeric_package_closure")
	if !strings.Contains(closureEntry, "append_external_abi_roots(") {
		t.Fatal("numeric package closure no longer seeds its queue from the external ABI manifest")
	}
	// The manifest walk lives in append_external_abi_roots; the closure and the
	// helper it delegates to are audited as one unit so extracting code cannot
	// move a name policy out of range of the forbidden-fragment scan below.
	numericClosure := closureEntry + "\n" +
		dependencyFunctionBody(t, executable, "append_external_abi_roots")
	for _, fragment := range []string{
		"external_abi_entrypoints::collect(allocator)",
		"manifest_entries.at(entrypoint_index)",
		"entrypoint.package_name",
		"entrypoint.module_path",
		"entrypoint.function_name",
		"package_exact_lookup::resolve_call(",
		"package_dependency_graph::queue_append(pending, catalog, target)",
	} {
		if !strings.Contains(numericClosure, fragment) {
			t.Fatalf("numeric package closure missing external ABI manifest flow %q", fragment)
		}
	}
	for _, forbidden := range []string{"allowed", "starts_with", "equal_bytes", "callee_text"} {
		if strings.Contains(numericClosure, forbidden) {
			t.Fatalf("numeric package closure reintroduced name policy %q", forbidden)
		}
	}
	emitter := dependencyFunctionBody(t, executable, "append_numeric_package_definition")
	for _, fragment := range []string{
		"package_definition_lookup::definition_node(",
		"package_dependency_graph::append_target_qualified_name(",
		"append_numeric_package_definition_body(",
	} {
		if !strings.Contains(emitter, fragment) {
			t.Fatalf("numeric dependency emitter missing %q", fragment)
		}
	}
	assertNumericDependencyClaimHasNoSpelling(t)
}

// assertNumericDependencyClaimHasNoSpelling rejects textual selection in dependency claims.
func assertNumericDependencyClaimHasNoSpelling(t *testing.T) {
	t.Helper()
	programLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	claim := dependencyFunctionBody(t, programLLVM, "append_reachable_functions")
	dependencyLoopStart := strings.Index(claim, "var dependency_index = 0")
	emissionLoopStart := strings.Index(claim, "var emitted_count = 0")
	if dependencyLoopStart < 0 || emissionLoopStart <= dependencyLoopStart {
		t.Fatal("LLVM numeric dependency validation loop missing")
	}
	claim = claim[dependencyLoopStart:emissionLoopStart]
	for _, forbidden := range []string{
		"equal_bytes", "callee", "symbol", "local_name",
	} {
		if strings.Contains(claim, forbidden) {
			t.Fatalf("LLVM dependency target claim reintroduced spelling selection %q", forbidden)
		}
	}
}

// requireSourceFragments checks structural source/fact guards with a shared diagnostic.
func requireSourceFragments(t *testing.T, label, source string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(source, fragment) {
			t.Errorf("%s missing %q", label, fragment)
		}
	}
}

// TestSelfhostPackageMethodIdentityIncludesOwnerType pins the catalog key that
// lets Ast.deinit and ParseResult.deinit coexist and resolve to distinct numeric targets.
func TestSelfhostPackageMethodIdentityIncludesOwnerType(t *testing.T) {
	catalog := readSelfhostFile(t, "../../selfhost/src/ir/package_catalog.kizu")
	collector := readSelfhostFile(t, "../../selfhost/src/ir/package_catalog_collect.kizu")
	exactIndex := readSelfhostFile(t, "../../selfhost/src/ir/exact_name_index.kizu")
	lookupSource := readSelfhostFile(t, "../../selfhost/src/ir/package_exact_lookup.kizu")
	graph := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency_graph.kizu")
	dependency := catalog + "\n" + collector + "\n" + lookupSource + "\n" + graph
	requireSourceFragments(t, "raw dense package catalog registration", catalog, []string{
		"component_name: []u8\n) -> !i64",
		"component_value: i64,\n    local_name: []u8",
		"component: package_function_identity::ComponentId { value: component_value }",
	})
	registration := dependencyFunctionBody(t, catalog, "register_function")
	if strings.Contains(registration, "component.value") {
		t.Fatal("dense package catalog registration unwraps a typed ComponentId")
	}
	collection := dependencyFunctionBody(t, collector, "collect_from_parsed_files")
	if strings.Contains(collection, "component.value") {
		t.Fatal("package catalog collection re-wraps and unwraps its dense component id")
	}
	for _, fragment := range []string{
		"var component_ids = std::array::Array<i64>(allocator)",
		"try component_ids.append(component)",
		"let owner_component = try component_ids.get(parsed_index)",
		"validate_component_source_identity(",
	} {
		if !strings.Contains(collection, fragment) {
			t.Fatalf("package catalog collection does not preserve first-pass component identity %q", fragment)
		}
	}
	if strings.Contains(collection, "package_catalog::component_by_name(") {
		t.Fatal("package catalog collection re-resolves second-pass component identity by name")
	}
	if strings.Contains(catalog, "next_import_component") {
		t.Fatal("package catalog retains duplicate scalar import lifecycle state")
	}
	beginImports := dependencyFunctionBody(t, catalog, "begin_component_imports")
	requireSourceFragments(t, "array-length import begin lifecycle", beginImports, []string{
		"owner_component != catalog.component_import_starts.len()",
		"owner_component != catalog.component_import_lens.len()",
		"component_import_starts.append(catalog.import_alias_names.len())",
	})
	finishImports := dependencyFunctionBody(t, catalog, "finish_component_imports")
	requireSourceFragments(t, "array-length import finish lifecycle", finishImports, []string{
		"catalog.component_import_starts.len() != owner_component + 1",
		"catalog.component_import_lens.len() != owner_component",
		"catalog.component_import_lens.append(",
	})
	appendImport := dependencyFunctionBody(t, catalog, "append_component_import")
	requireSourceFragments(t, "active component import append lifecycle", appendImport, []string{
		"catalog.component_import_starts.len() != owner_component + 1",
		"catalog.component_import_lens.len() != owner_component",
	})
	for _, fragment := range []string{
		"function_owner_type_names: std::array::Array<[]u8>",
		"node_text(text, ast, impl_decl.type_name)",
		"find_owned_function(catalog, component_value, owner_type_name, local_name)",
		"let owner = package_catalog::function_owner_type_at(catalog, index)",
		"component_name[component_index] == cast<u8>(47)",
	} {
		if !strings.Contains(dependency, fragment) {
			t.Fatalf("method catalog owner identity missing %q", fragment)
		}
	}
	lookup := dependencyFunctionBody(t, lookupSource, "declared_impl_method_entry")
	if !strings.Contains(lookup, "package_catalog::exact_declared_impl_method_entry(") {
		t.Fatal("receiver method lookup does not select the owner-specific exact index")
	}
	topLevel := dependencyFunctionBody(t, lookupSource, "local_function_entry")
	if !strings.Contains(topLevel, "package_catalog::exact_top_level_function_entry(") {
		t.Fatal("top-level lookup does not use the component-and-name exact index")
	}
	indexBuild := dependencyFunctionBody(t, catalog, "finalize_exact_function_indexes")
	for _, fragment := range []string{
		"function_owner_type_names.get_or_panic(function_index)",
		"function_owner_declaration_indices.get_or_panic(function_index)",
		"insert_top_level_function_index(catalog, function_index)",
		"insert_declared_impl_method_index(catalog, function_index)",
	} {
		if !strings.Contains(indexBuild, fragment) {
			t.Fatalf("exact function index build misses identity guard %q", fragment)
		}
	}
	hashStep := dependencyFunctionBody(t, exactIndex, "hash_step")
	requireSourceFragments(t, "constant-time exact function hash step", hashStep, []string{
		"let normalized = hash_normalize(value, modulus)",
		"return (state * 33 + normalized) % modulus",
	})
	if strings.Contains(hashStep, "while shift < 5") {
		t.Fatal("exact function hash retained the per-byte shift loop")
	}
	if strings.Contains(exactIndex, "fn exact_hash_add_mod(") {
		t.Fatal("exact function hash retained the repeated modular-add helper")
	}
	capacity := dependencyFunctionBody(t, exactIndex, "capacity")
	limit := dependencyFunctionBody(t, exactIndex, "maximum_capacity")
	requireSourceFragments(t, "safe exact function hash capacity", capacity+limit, []string{
		"result >= maximum_capacity()",
		"144115188075855872",
	})
}

// TestSelfhostPackageMethodCallerIdentityUsesNameSpan pins caller resolution
// when two impl owner types declare methods with the same spelling.
func TestSelfhostPackageMethodCallerIdentityUsesNameSpan(t *testing.T) {
	catalog := readSelfhostFile(t, "../../selfhost/src/ir/package_catalog.kizu")
	lookup := readSelfhostFile(t, "../../selfhost/src/ir/package_exact_lookup.kizu")
	resolverSource := readSelfhostFile(t, "../../selfhost/src/ir/package_call_resolution.kizu")
	dependency := catalog + "\n" + lookup + "\n" + resolverSource
	for _, fragment := range []string{
		"function_name_starts: std::array::Array<i64>",
		"function_name_ends: std::array::Array<i64>",
		"package_exact_lookup::function_entry_by_name_span(catalog, component_id, name_span.start, name_span.end)",
	} {
		if !strings.Contains(dependency, fragment) {
			t.Fatalf("same-name impl caller identity missing %q", fragment)
		}
	}
	lookupBody := dependencyFunctionBody(t, lookup, "find_function_by_span")
	callerIdentityFields := []string{
		"component_function_start_at",
		"var upper = function_end",
		"while lower < upper",
		"let middle = lower + (upper - lower) / 2",
		"function_name_start_at",
		"function_name_end_at",
	}
	for _, fragment := range callerIdentityFields {
		if !strings.Contains(lookupBody, fragment) {
			t.Fatalf("span caller lookup cannot distinguish owner method declarations: missing %q", fragment)
		}
	}
	if strings.Contains(lookupBody, "index = index + 1") {
		t.Fatal("span caller lookup retained a linear fallback")
	}
	registration := dependencyFunctionBody(t, catalog, "register_function")
	for _, fragment := range []string{
		"name_end <= name_start",
		"name_start <= previous_start",
		"name_start < previous_end",
	} {
		if !strings.Contains(registration, fragment) {
			t.Fatalf("catalog does not enforce binary-search span ordering: missing %q", fragment)
		}
	}
	resolver := dependencyFunctionBody(t, resolverSource, "resolve_function_calls")
	if strings.Contains(resolver, "find_function(catalog") {
		t.Fatal("caller resolution still collapses same-name impl methods through top-level name lookup")
	}
}

// dependencyFunctionBody extracts one Kizu function for focused hardcoding audits.
func dependencyFunctionBody(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, fmt.Sprintf("fn %s(", name))
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	bodyStart := strings.Index(source[start:], "{")
	if bodyStart < 0 {
		t.Fatalf("function %s body not found", name)
	}
	start += bodyStart
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : index+1]
			}
		}
	}
	t.Fatalf("function %s body is unclosed", name)
	return ""
}
