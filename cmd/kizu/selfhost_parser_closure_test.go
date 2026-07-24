package main

import (
	"strings"
	"testing"
)

func TestSelfhostParserClosureUsesComponentCatalog(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	production := selfhostKizuFunctionBody(t, source, "fn append_facts_from_parsed(")
	queueWalk := selfhostKizuFunctionBody(
		t, source, "fn append_numeric_package_closure_from_queue(",
	)
	for _, fragment := range []string{
		"package_catalog_collect::collect_from_parsed_files(",
		"package_call_resolution::resolve_package_calls_with_types(",
		"package_dependency_graph::dependency_graph(",
		"package_call_resolution::append_resolved_dependencies(",
	} {
		if !strings.Contains(production, fragment) {
			t.Fatalf("parser production graph path missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"package_dependency_graph::queue_append_dependencies(",
		"append_numeric_package_definition(",
	} {
		if !strings.Contains(queueWalk, fragment) {
			t.Fatalf("parser numeric closure queue walk missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"append_kizu_parser_function_facts",
		"append_kizu_parser_closure_seed",
		"append_selected_helper_body",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy parser selector remains %q", forbidden)
		}
	}
}
