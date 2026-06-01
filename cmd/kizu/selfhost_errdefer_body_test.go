package main

import (
	"strings"
	"testing"
)

// TestSelfhostExecutableBodyHandlesErrDefer keeps the executable body IR emitter
// and body_node_kind treating ErrDefer as a single-child statement alongside
// Defer and ExprStmt, so error-path cleanup nodes are not dropped from the
// checked body facts.
func TestSelfhostExecutableBodyHandlesErrDefer(t *testing.T) {
	executableBody := readSelfhostFile(t, "../../selfhost/src/ir/executable_body.kizu")

	emitterBody := selfhostKizuFunctionBody(t, executableBody, "fn append_body_node_ir(")
	for _, fragment := range []string{
		"Defer(defer_node) => return try append_body_single_child_ir(",
		"ErrDefer(err_defer_node) => return try append_body_single_child_ir(",
		"ExprStmt(expr_stmt) => return try append_body_single_child_ir(",
		"err_defer_node.expr,",
	} {
		if !strings.Contains(emitterBody, fragment) {
			t.Fatalf("executable body emitter missing ErrDefer parity %q", fragment)
		}
	}
	// ErrDefer must share the same "expr" child label as Defer/ExprStmt.
	if strings.Count(emitterBody, "\"expr\",") < 3 {
		t.Fatal("executable body emitter lost the shared expr child label for Defer/ErrDefer/ExprStmt")
	}

	kindBody := selfhostKizuFunctionBody(t, executableBody, "fn body_node_kind(")
	for _, fragment := range []string{
		"Defer(defer_node) => \"Defer\",",
		"ErrDefer(err_defer_node) => \"ErrDefer\",",
		"ExprStmt(expr_stmt) => \"ExprStmt\",",
	} {
		if !strings.Contains(kindBody, fragment) {
			t.Fatalf("body_node_kind missing ErrDefer parity %q", fragment)
		}
	}
}
