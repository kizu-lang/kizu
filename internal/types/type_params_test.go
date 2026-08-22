package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// TestTypeParamStoreRestoresReplacedScopes pins the non-capturing scope stack
// used while declarations and generic instances are checked.
func TestTypeParamStoreRestoresReplacedScopes(t *testing.T) {
	var store typeParamStore
	outer := store.enter([]string{"T"})
	if outer != nil || !store.contains("T") {
		t.Fatal("outer type parameter was not selected")
	}
	previous := store.enter([]string{"U"})
	if !store.contains("U") || store.contains("T") {
		t.Fatal("inner type parameters captured the outer scope")
	}
	store.restore(previous)
	if !store.contains("T") || store.contains("U") {
		t.Fatal("outer type parameter scope was not restored")
	}
	store.restore(outer)
	if store.contains("T") {
		t.Fatal("empty type parameter scope was not restored")
	}
}

// TestTypeParamStoreSelectsOnlySignatureTypes separates static values from
// names that are valid in a type position.
func TestTypeParamStoreSelectsOnlySignatureTypes(t *testing.T) {
	var store typeParamStore
	store.enterSignature(ast.FunctionSignature{StaticParams: []ast.StaticParam{
		{Name: "T"},
		{Name: "count", Type: &typ.Name{Path: []string{"i64"}}},
	}})
	if !store.contains("T") || store.contains("count") {
		t.Fatal("signature static values were treated as type parameters")
	}
}
