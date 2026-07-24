package main

import (
	"strings"
	"testing"
)

// TestSelfhostCodegenClosureUsesComponentCatalog keeps selfhost::ir::codegen
// IR body-fact selection on package-owned component-catalog closure roots instead of the old
// handwritten append_selected_function_with_body / append_selected_helper_body list.
func TestSelfhostCodegenClosureUsesComponentCatalog(t *testing.T) {
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	wrapper := selfhostKizuFunctionBody(t, executableFunctions, "pub fn append_facts(")
	production := selfhostKizuFunctionBody(t, executableFunctions, "fn append_facts_from_parsed(")
	closure := selfhostKizuFunctionBody(t, executableFunctions, "fn append_numeric_package_closure(")
	queueWalk := selfhostKizuFunctionBody(
		t, executableFunctions, "fn append_numeric_package_closure_from_queue(",
	)
	for _, fragment := range []string{
		"parser::parse_source_files(allocator, files)",
		"let result = append_facts_from_parsed(",
		"parser::deinit_parsed_source_files(parsed_package)",
	} {
		if !strings.Contains(wrapper, fragment) {
			t.Fatalf("package fact wrapper missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"package_catalog_collect::collect_from_parsed_files(",
		"package_dependency_graph::dependency_graph(",
		"package_call_resolution::append_resolved_dependencies(",
		"append_numeric_package_closure(",
	} {
		if !strings.Contains(production, fragment) {
			t.Fatalf("production package graph path missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"package_dependency_graph::function_target_queue(",
		"append_numeric_package_closure_from_queue(",
	} {
		if !strings.Contains(closure, fragment) {
			t.Fatalf("numeric closure seed missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"package_dependency_graph::queue_append_dependencies(",
		"append_numeric_package_definition(",
	} {
		if !strings.Contains(queueWalk, fragment) {
			t.Fatalf("numeric closure queue walk missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"append_codegen_function_facts", "append_selected_helper_body",
		"append_selected_function_with_body",
	} {
		if strings.Contains(executableFunctions, forbidden) {
			t.Fatalf("legacy codegen selector remains %q", forbidden)
		}
	}
}
