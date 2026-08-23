package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/source"
)

// TestUnsafePolicyDecisionsStayIndependentOfDiagnostics keeps source identity
// and documentation checks copy-only below the checker diagnostic boundary.
func TestUnsafePolicyDecisionsStayIndependentOfDiagnostics(t *testing.T) {
	if got := unsafeStructInvariantRequirement(nil, ast.Span{}); got != unsafeInvariantNone {
		t.Fatalf("nil declaration requirement = %d, want none", got)
	}

	sources := source.NewMap()
	declarationSource := sources.Add("declaration.kizu", "")
	useSource := sources.Add("use.kizu", "")
	decl := &ast.StructDecl{RequiresUnsafe: true, Span: ast.Span{Source: declarationSource}}
	if got := unsafeStructInvariantRequirement(
		decl,
		ast.Span{Source: declarationSource},
	); got != unsafeInvariantMarker {
		t.Fatalf("same-file requirement = %d, want marker", got)
	}
	if got := unsafeStructInvariantRequirement(
		decl,
		ast.Span{Source: useSource},
	); got != unsafeInvariantFileBoundary {
		t.Fatalf("cross-file requirement = %d, want file boundary", got)
	}

	otherSources := source.NewMap()
	sameIndexOtherMap := otherSources.Add("other.kizu", "")
	if got := unsafeStructInvariantRequirement(
		decl,
		ast.Span{Source: sameIndexOtherMap},
	); got != unsafeInvariantFileBoundary {
		t.Fatalf("cross-map requirement = %d, want file boundary", got)
	}

	if !obligationDocMissing(true, "") {
		t.Fatal("undocumented unsafe obligation was accepted")
	}
	if obligationDocMissing(true, "documented") || obligationDocMissing(false, "") {
		t.Fatal("documented or safe declaration was rejected")
	}
}
