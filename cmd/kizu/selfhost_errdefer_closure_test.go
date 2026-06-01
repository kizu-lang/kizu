package main

import (
	"strings"
	"testing"
)

// TestSelfhostCatalogClosureCollectsErrDeferCallees keeps the catalog closure
// callee walker descending into errdefer expressions so a callee like
// `errdefer cleanup();` is reached by the BFS closure exactly like a deferred
// one, without adding any fallback branch.
func TestSelfhostCatalogClosureCollectsErrDeferCallees(t *testing.T) {
	executableFunctions := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")

	walkerBody := selfhostKizuFunctionBody(
		t,
		executableFunctions,
		"fn collect_catalog_closure_direct_callees(",
	)
	for _, fragment := range []string{
		"Defer(defer_node) => try collect_catalog_closure_direct_callees(",
		"ErrDefer(err_defer_node) => try collect_catalog_closure_direct_callees(",
		"ExprStmt(expr_stmt) => try collect_catalog_closure_direct_callees(",
		"text, ast, err_defer_node.expr, catalog, pending, qualified_prefix,",
	} {
		if !strings.Contains(walkerBody, fragment) {
			t.Fatalf("catalog closure walker missing ErrDefer parity %q", fragment)
		}
	}
	// The ErrDefer arm must reuse the shared unsupported call-form / qualified
	// callee error flow rather than introducing a new fallback path.
	sharedErrorFlow := "unsupported_call_form_error, unsupported_qualified_callee_error"
	if !strings.Contains(walkerBody, sharedErrorFlow) {
		t.Fatal("catalog closure walker lost the shared unsupported callee error flow")
	}
}
