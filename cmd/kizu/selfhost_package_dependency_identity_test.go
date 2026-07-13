package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostNumericPackageCollectorBehavior parses the actual package and
// drives the production resolver, dependency graph, closure, and emitter from
// constructor_facts::collect_into. The emitted facts prove Ast parameter
// receiver calls resolved to the numeric std::kizu::ast method definitions.
func TestSelfhostNumericPackageCollectorBehavior(t *testing.T) {
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
	})
	for _, name := range []string{"std::kizu::ast::ast_get", "std::kizu::ast::ast_child_at"} {
		marker := "function-signature-return " + name + " "
		if count := strings.Count(facts, marker); count != 1 {
			t.Fatalf(
				"numeric closure emitted %s definition %d times, want exactly once\n%s",
				name, count, facts,
			)
		}
	}
	if count := strings.Count(facts, "package-dependency "); count == 0 {
		t.Fatal("numeric collector closure emitted no numeric dependency records")
	}
	if count := strings.Count(facts, "package-definition "); count == 0 {
		t.Fatal("numeric collector closure emitted no numeric target definitions")
	}
	if count := strings.Count(facts, "package-reference "); count == 0 {
		t.Fatal("numeric collector closure emitted no numeric target references")
	}
}

// TestSelfhostNumericPackageCollectorBackendConsumer crosses the owned-file backend boundary
// and pins the next interpreted-consumer blocker after ConstructorFacts ABI mapping: lowering
// the collector body reaches a mutable struct-field assignment that the interpreter does not
// yet represent as a struct value. The native stage path is checked separately from this gate.
func TestSelfhostNumericPackageCollectorBackendConsumer(t *testing.T) {
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
		entry     string
		wantError string
	}{
		{name: "known runtime builtin is deliberately omitted", entry: "package_resolver_builtin_gate"},
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
			err := interp.New(&out).RunEntry(program, "selfhost::ir::executable_functions::"+tc.entry)
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
	dependency := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")

	requireSourceFragments(t, "package dependency identity", dependency, []string{
		"pub struct ComponentId",
		"pub struct FunctionId",
		"pub struct CallDependency",
		"pub struct DependencyRecord",
		"pub fn resolve_call(",
		"pub fn append_closure_targets(",
		"pub fn definition_node(",
	})
	requireSourceFragments(t, "ConstructorFacts LLVM ABI", llvm, []string{
		"%kizu.selfhost.types.constructor_facts.constructor_facts = type { " +
			"%kizu.owned, %kizu.owned, %kizu.owned, %kizu.owned }",
	})
	for _, fragment := range []string{
		"import selfhost::ir::package_dependency;",
		"pub fn consume_package_dependencies(",
		"dependency_record_from_line(line)",
		"package identity table size overflow",
		"package identity table exceeds fact input",
	} {
		if !strings.Contains(cli, fragment) {
			t.Errorf("cli_llvm dependency consumption missing %q", fragment)
		}
	}
	body := dependencyFunctionBody(t, dependency, "append_closure_targets")
	for _, forbidden := range []string{
		"equal_bytes", "starts_with", "callee", "symbol", "llvm",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("numeric closure BFS must not inspect %q", forbidden)
		}
	}
	cliBody := dependencyFunctionBody(t, cli, "consume_package_dependencies")
	dependencyLoopStart := strings.Index(cliBody, "var dependency_index = 0")
	emissionLoopStart := strings.Index(cliBody, "var emitted_count = 0")
	if dependencyLoopStart < 0 || emissionLoopStart <= dependencyLoopStart {
		t.Fatal("cli numeric dependency validation loop missing")
	}
	dependencyLoop := cliBody[dependencyLoopStart:emissionLoopStart]
	for _, forbidden := range []string{"equal_bytes", "callee", "symbol", "local_name"} {
		if strings.Contains(strings.ToLower(dependencyLoop), forbidden) {
			t.Errorf("cli dependency consumption must not inspect %q", forbidden)
		}
	}
	if !strings.Contains(cliBody, `return error("package dependency caller definition missing")`) {
		t.Fatal("cli dependency consumer does not reject a missing or wrong numeric caller")
	}
	linearClaimArrays := strings.Contains(cliBody, "claimed_components") ||
		strings.Contains(cliBody, "claimed_functions")
	if linearClaimArrays {
		t.Fatal("cli dependency consumer reintroduced a linear claimed-target pair scan")
	}
	for _, fragment := range []string{
		"function_stride", "identity_slot_count",
		"definitions.get(caller_slot)", "references.get(claim_slot)",
	} {
		if !strings.Contains(cliBody, fragment) {
			t.Fatalf("cli dependency consumer dense numeric claimed set missing %q", fragment)
		}
	}
}

// TestSelfhostPackageDependencyIdentityUsesBothNumericIDs rejects spelling-only identity.
func TestSelfhostPackageDependencyIdentityUsesBothNumericIDs(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	for _, fn := range []string{"same_target", "find_target", "contains_target"} {
		body := dependencyFunctionBody(t, source, fn)
		if !strings.Contains(body, "component") || !strings.Contains(body, "function") {
			t.Fatalf("%s does not distinguish same-spelling functions by both numeric IDs", fn)
		}
	}
	resolveBody := dependencyFunctionBody(t, source, "resolve_call")
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
	dependency := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	executable := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")

	resolver := dependencyFunctionBody(t, dependency, "resolve_field_callee")
	requireSourceFragments(t, "parameter value-method resolver", resolver, []string{
		"find_param_type", "find_type_method", "namespace",
	})
	methodLookup := dependencyFunctionBody(t, dependency, "find_type_method")
	for _, fragment := range []string{
		"function_component_ids", "component_package_names", "component_names",
		"type_mentions_component", "function_names",
	} {
		if !strings.Contains(methodLookup, fragment) {
			t.Fatalf("numeric method lookup missing component-safe check %q", fragment)
		}
	}
	if strings.Contains(methodLookup, "return find_function") {
		t.Fatal("method resolver can select a same-name function without receiver component identity")
	}
	for _, fragment := range []string{
		"package_dependency::resolve_package_calls(",
		"package_dependency::append_resolved_dependencies(",
		"append_numeric_package_closure(",
		"package_dependency::queue_append_dependencies(",
		"package_dependency::definition_node(",
		"package_dependency::DependencyGraph",
	} {
		if !strings.Contains(executable, fragment) {
			t.Fatalf("append_facts does not consume resolver numeric targets: missing %q", fragment)
		}
	}
	numericClosure := dependencyFunctionBody(t, executable, "append_numeric_package_closure")
	for _, forbidden := range []string{"allowed", "starts_with", "equal_bytes", "callee_text"} {
		if strings.Contains(numericClosure, forbidden) {
			t.Fatalf("numeric package closure reintroduced name policy %q", forbidden)
		}
	}
	emitter := dependencyFunctionBody(t, executable, "append_numeric_package_definition")
	for _, fragment := range []string{
		"package_dependency::definition_node(",
		"package_dependency::append_target_qualified_name(",
		"append_numeric_package_definition_body(",
	} {
		if !strings.Contains(emitter, fragment) {
			t.Fatalf("numeric dependency emitter missing %q", fragment)
		}
	}
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	claim := dependencyFunctionBody(t, cli, "consume_package_dependencies")
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
	dependency := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	for _, fragment := range []string{
		"function_owner_type_names: std::array::Array<[]u8>",
		"node_text(text, ast, impl_decl.type_name)",
		"find_owned_function(catalog, component_value, owner_type_name, local_name)",
		"let owner = try catalog.function_owner_type_names.get(index)",
		"component_name[component_index] == cast<u8>(47)",
	} {
		if !strings.Contains(dependency, fragment) {
			t.Fatalf("method catalog owner identity missing %q", fragment)
		}
	}
	lookup := dependencyFunctionBody(t, dependency, "find_type_method")
	for _, fragment := range []string{"function_owner_type_names", "type_mentions_owner"} {
		if !strings.Contains(lookup, fragment) {
			t.Fatalf(
				"receiver method lookup does not select the owner-specific numeric target: missing %q",
				fragment,
			)
		}
	}
	topLevel := dependencyFunctionBody(t, dependency, "find_function")
	ownerIsEmpty := "std::mem::len(" +
		"catalog.function_owner_type_names.get_or_panic(index)) == 0"
	if !strings.Contains(topLevel, ownerIsEmpty) {
		t.Fatal("top-level lookup can select an impl method with the same spelling")
	}
}

// TestSelfhostPackageMethodCallerIdentityUsesNameSpan pins caller resolution
// when two impl owner types declare methods with the same spelling.
func TestSelfhostPackageMethodCallerIdentityUsesNameSpan(t *testing.T) {
	dependency := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	for _, fragment := range []string{
		"function_name_starts: std::array::Array<i64>",
		"function_name_ends: std::array::Array<i64>",
		"find_function_by_span(catalog, component_id, name_span.start, name_span.end)",
	} {
		if !strings.Contains(dependency, fragment) {
			t.Fatalf("same-name impl caller identity missing %q", fragment)
		}
	}
	lookup := dependencyFunctionBody(t, dependency, "find_function_by_span")
	callerIdentityFields := []string{
		"function_component_ids",
		"function_name_starts",
		"function_name_ends",
	}
	for _, fragment := range callerIdentityFields {
		if !strings.Contains(lookup, fragment) {
			t.Fatalf("span caller lookup cannot distinguish owner method declarations: missing %q", fragment)
		}
	}
	resolver := dependencyFunctionBody(t, dependency, "resolve_function_calls")
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
