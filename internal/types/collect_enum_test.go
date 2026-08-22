package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestEnumCollectionDecisionsStayIndependentOfDiagnostics keeps declaration
// collection copy-only below the Checker diagnostic boundary.
func TestEnumCollectionDecisionsStayIndependentOfDiagnostics(t *testing.T) {
	t.Run("duplicate enum tag", func(t *testing.T) {
		checker := &Checker{checkerMetadata: newCheckerMetadata()}
		issue, found := checker.collectEnumDecision(&ast.EnumDecl{
			Name: "Choice",
			Tags: []string{"First", "First"},
		})
		if !found || issue.kind != enumCollectionDuplicateTag ||
			issue.owner != "Choice" || issue.member != "First" {
			t.Fatalf("issue = %#v, found = %t", issue, found)
		}
		if len(checker.enums) != 0 {
			t.Fatal("rejected enum was retained")
		}
	})

	t.Run("duplicate error member", func(t *testing.T) {
		checker := &Checker{checkerMetadata: newCheckerMetadata()}
		issue, found := checker.collectErrorSetDecision(&ast.ErrorSetDecl{
			Name:    "Problem",
			Members: []string{"Failed", "Failed"},
		})
		if !found || issue.kind != enumCollectionDuplicateError ||
			issue.owner != "Problem" || issue.member != "Failed" {
			t.Fatalf("issue = %#v, found = %t", issue, found)
		}
		if len(checker.errorSets) != 0 {
			t.Fatal("rejected error set was retained")
		}
	})

	t.Run("duplicate type", func(t *testing.T) {
		checker := &Checker{checkerMetadata: newCheckerMetadata()}
		checker.structs["Choice"] = &ast.StructDecl{Name: "Choice"}
		issue, found := checker.collectEnumDecision(&ast.EnumDecl{Name: "Choice"})
		if !found || issue.kind != enumCollectionDuplicateType ||
			issue.owner != "Choice" || issue.member != "" {
			t.Fatalf("issue = %#v, found = %t", issue, found)
		}
	})
}

// TestErrorSetCollectionSharesItsEnumMemberSet pins the one-owner shape that
// the Kizu port represents with an EnumHandle.
func TestErrorSetCollectionSharesItsEnumMemberSet(t *testing.T) {
	checker := &Checker{checkerMetadata: newCheckerMetadata()}
	issue, found := checker.collectErrorSetDecision(&ast.ErrorSetDecl{
		Name:    "Problem",
		Members: []string{"Failed", "Missing"},
		Public:  true,
	})
	if found {
		t.Fatalf("unexpected issue: %#v", issue)
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
