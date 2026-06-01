package main

import (
	"strings"
	"testing"
)

// TestSelfhostAstCompiledClosureDerivedFromSharedBFS pins that the selfhost::ast
// node-count traversal is emitted through the shared compiled closure BFS instead of
// a handwritten append_compiled_function_auto per member. The closure seeds the BFS
// with declaration_count and node_count over the "selfhost::ast::" prefix, routes
// through the shared member builder and emitter, and derives each member's
// params_spec from signature facts. The forbidden fragments pin that the twelve
// handwritten cluster appends and their literal symbols / params_spec strings are
// gone, so nothing keeps a per-helper table.
func TestSelfhostAstCompiledClosureDerivedFromSharedBFS(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentReachableCompiledClosure(
		t,
		cli,
		"fn append_ast_reachable_compiled_functions(",
		"selfhost::ast::",
		false,
		[]string{
			"declaration_count",
			"node_count",
		},
	)

	assertSharedCompiledClosurePath(t, cli)
	assertNoPerComponentCompiledClosureHelpers(t, cli)

	// The append_functions entry point delegates to the shared walk rather than
	// re-listing the cluster members.
	if !strings.Contains(cli, "try append_ast_reachable_compiled_functions(out, ir_bytes);") {
		t.Fatalf("append_functions missing shared ast closure delegation")
	}

	// Every handwritten cluster append (qualified name, mangled symbol, and the
	// per-member params_spec literals) must be gone.
	forbidden := []string{
		"\"selfhost::ast::declaration_count\"",
		"\"selfhost::ast::node_count\"",
		"\"selfhost::ast::count_range\"",
		"\"selfhost::ast::count_one\"",
		"\"selfhost::ast::count_two\"",
		"\"selfhost::ast::count_three\"",
		"\"selfhost::ast::count_five\"",
		"\"selfhost::ast::count_with_range\"",
		"\"selfhost::ast::count_node_with_range\"",
		"\"selfhost::ast::count_named_range\"",
		"\"selfhost::ast::count_named_ranges\"",
		"\"selfhost::ast::count_fn_decl_parts\"",
		"\"kizu_selfhost__ast_declaration_count\"",
		"\"kizu_selfhost__ast_node_count\"",
		"\"kizu_selfhost__ast_count_fn_decl_parts\"",
		"\"%kizu.kizu.ast.ast tree;%kizu.kizu.ast.node_id root\"",
		"\"%kizu.kizu.ast.ast tree;%kizu.kizu.ast.child_range range\"",
	}
	for _, fragment := range forbidden {
		if strings.Contains(cli, fragment) {
			t.Fatalf("ast compiled cluster keeps hand-written fragment %q", fragment)
		}
	}
}

// TestSelfhostAstCompiledClosureExternalAccessorAllowPolicy pins the explicit, narrow
// external-callee allow policy the selfhost::ast closure relies on. The shared callee
// collector keeps its IR-fact gate and consults compiled_external_accessor_allowed,
// which admits only the two std::kizu::ast read accessors spelled as method calls
// (tree.get / tree.child_at). No other cross-component callee is admitted, so the
// BFS neither re-emits nor walks the separately-compiled accessors.
func TestSelfhostAstCompiledClosureExternalAccessorAllowPolicy(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	assertComponentCompiledCalleeFactGate(t, cli)

	callee := selfhostKizuFunctionBody(t, cli, "fn collect_component_compiled_callee(")
	if !strings.Contains(callee, "if compiled_external_accessor_allowed(prefix, callee) {") {
		t.Fatalf("collect_component_compiled_callee missing external accessor allow check")
	}

	allow := selfhostKizuFunctionBody(t, cli, "fn compiled_external_accessor_allowed(")
	required := []string{
		"if !std::mem::equal_bytes(prefix, \"selfhost::ast::\") {",
		"if std::mem::equal_bytes(callee, \"tree.get\") {",
		"if std::mem::equal_bytes(callee, \"tree.child_at\") {",
		"return true;",
		"return false;",
	}
	for _, fragment := range required {
		if !strings.Contains(allow, fragment) {
			t.Fatalf("compiled_external_accessor_allowed missing %q", fragment)
		}
	}
}

// TestSelfhostAstCompiledParamsSpecDerivedFromSignatures keeps the selfhost::ast
// closure tied to function-signature-param facts: the ABI mapper learns the
// std::kizu::ast Ast / NodeId / ChildRange value-type spellings so append_params_spec
// can derive each member's params_spec without a handwritten per-helper table.
func TestSelfhostAstCompiledParamsSpecDerivedFromSignatures(t *testing.T) {
	abi := readSelfhostFile(t, "../../selfhost/src/backend/compiled_abi_params.kizu")

	required := []string{
		"std::mem::equal_bytes(kizu_type, \"std::kizu::ast::Ast\")",
		"\"%kizu.kizu.ast.ast\"",
		"std::mem::equal_bytes(kizu_type, \"std::kizu::ast::NodeId\")",
		"\"%kizu.kizu.ast.node_id\"",
		"std::mem::equal_bytes(kizu_type, \"std::kizu::ast::ChildRange\")",
		"\"%kizu.kizu.ast.child_range\"",
	}
	for _, fragment := range required {
		if !strings.Contains(abi, fragment) {
			t.Fatalf("compiled ABI params mapper missing %q", fragment)
		}
	}
}
