package types

import (
	"slices"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestScopeKeepsOneBindingRecordPerName pins the copy-only scope decisions
// below the checker diagnostic boundary.
func TestScopeKeepsOneBindingRecordPerName(t *testing.T) {
	root := newScope(nil)
	alphaSpan := ast.Span{Start: ast.Position{Line: 2, Column: 3}}
	zetaSpan := ast.Span{Start: ast.Position{Line: 4, Column: 5}}
	if !root.define("zeta", "bool", false) || !root.define("alpha", "i64", true) {
		t.Fatal("fresh bindings were rejected")
	}
	root.declareLocal("zeta", zetaSpan)
	root.declareLocal("alpha", alphaSpan)
	if !root.isMutable("alpha") {
		t.Fatal("binding metadata was lost")
	}
	if binding := root.bindings["alpha"]; !binding.unread || binding.declaration != alphaSpan {
		t.Fatalf("alpha unread binding = %#v", binding)
	}
	if root.define("alpha", "bool", false) {
		t.Fatal("duplicate binding was accepted")
	}
	if !root.define(discardName, "i64", false) || !root.define(discardName, "bool", true) {
		t.Fatal("repeated discard binding was rejected")
	}

	child := root.child()
	got, found := child.lookup("alpha")
	if !found || got != "i64" {
		t.Fatalf("child lookup = (%q, %t), want i64", got, found)
	}
	if root.bindings["alpha"].unread || !root.bindings["zeta"].unread {
		t.Fatal("lookup cleared the wrong unread binding")
	}
}

// TestScopeBorrowFactsResolveAtTheNearestBinding pins provenance and the
// distinction between signature and local mutable borrows.
func TestScopeBorrowFactsResolveAtTheNearestBinding(t *testing.T) {
	table := newTypeTable()
	root := newScope(nil)
	if !defineScopeParam(&table, root, "view", "[]u8", false, false) {
		t.Fatal("view parameter was rejected")
	}
	view, ok := root.binding("view")
	if !ok || !slices.Equal(view.borrowSources, []string{"view"}) {
		t.Fatalf("view sources = (%v, %t), want [view]", view.borrowSources, ok)
	}
	if !defineSignatureParam(&table, root, "out", "i64", true, true) {
		t.Fatal("signature borrow was rejected")
	}
	if !root.isBorrowed("out") || !root.isMutBorrowed("out") || !root.isMutBorrowedParam("out") {
		t.Fatal("signature borrow facts were lost")
	}

	child := root.child()
	if !child.defineParamWithSource("out", "bool", true, true, []string{"owner"}, false) {
		t.Fatal("shadowing local borrow was rejected")
	}
	if child.isMutBorrowedParam("out") {
		t.Fatal("local mutable borrow became caller storage")
	}
	shadow, ok := child.binding("out")
	if !ok || !slices.Equal(shadow.borrowSources, []string{"owner"}) {
		t.Fatalf("shadow sources = (%v, %t), want [owner]", shadow.borrowSources, ok)
	}
	if _, ok := child.binding("missing"); ok {
		t.Fatal("missing binding reported provenance")
	}
}
