package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestSelfhostPackageDependencyIdentityFlow guards the numeric handoff boundaries.
func TestSelfhostPackageDependencyIdentityFlow(t *testing.T) {
	dependency := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency.kizu")
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	for _, fragment := range []string{
		"pub struct ComponentId",
		"pub struct FunctionId",
		"pub struct CallDependency",
		"pub struct DependencyRecord",
		"pub fn resolve_call(",
		"pub fn append_closure_targets(",
		"pub fn definition_node(",
	} {
		if !strings.Contains(dependency, fragment) {
			t.Errorf("package dependency identity missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"import selfhost::ir::package_dependency;",
		"pub fn dependency_definition_node(",
		"package_dependency::dependency_target(dependency)",
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
	cliBody := dependencyFunctionBody(t, cli, "dependency_definition_node")
	for _, forbidden := range []string{"equal_bytes", "starts_with", "callee", "symbol"} {
		if strings.Contains(strings.ToLower(cliBody), forbidden) {
			t.Errorf("cli dependency consumption must not inspect %q", forbidden)
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

	resolver := dependencyFunctionBody(t, dependency, "resolve_field_callee_index")
	for _, fragment := range []string{
		"find_param_type", "find_type_method", "namespace",
	} {
		if !strings.Contains(resolver, fragment) {
			t.Fatalf("parameter value-method resolver missing %q", fragment)
		}
	}
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
