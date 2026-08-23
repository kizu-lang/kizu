package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestErrorSetCollectionSharesItsEnumMemberSet pins the one-owner shape that
// the Kizu port represents with an EnumHandle.
func TestErrorSetCollectionSharesItsEnumMemberSet(t *testing.T) {
	checker := &Checker{checkerMetadata: newCheckerMetadata()}
	err := checker.collectErrorSet(&ast.ErrorSetDecl{
		Name:    "Problem",
		Members: []string{"Failed", "Missing"},
		Public:  true,
	})
	if err != nil {
		t.Fatalf("collect error set: %v", err)
	}
	set := checker.errorSets["Problem"]
	if set == nil || set.tagged == nil || !set.public || !set.tagged.public {
		t.Fatalf("retained error set = %#v", set)
	}
	set.members["Later"] = true
	if !set.tagged.tags["Later"] {
		t.Fatal("error member set and tagged enum do not share storage")
	}
}
