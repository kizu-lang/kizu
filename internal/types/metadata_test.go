package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestCheckerMetadataStartsEmpty pins the one-check registry boundary.
func TestCheckerMetadataStartsEmpty(t *testing.T) {
	metadata := newCheckerMetadata()
	if len(metadata.functions) != 0 || len(metadata.structs) != 0 ||
		len(metadata.enums) != 0 || len(metadata.errorSets) != 0 ||
		len(metadata.unions) != 0 || len(metadata.contracts) != 0 ||
		len(metadata.impls) != 0 || len(metadata.declaredTypes) != 0 {
		t.Fatal("new checker metadata is not empty")
	}
}

// TestCheckerMetadataOwnsTypeNameAndVisibilityQueries keeps registry policy at
// the metadata boundary rather than spreading concrete Map checks through the
// checker.
func TestCheckerMetadataOwnsTypeNameAndVisibilityQueries(t *testing.T) {
	metadata := newCheckerMetadata()
	metadata.declaredTypes["Forward"] = true
	metadata.structs["Public"] = &ast.StructDecl{Public: true}
	metadata.enums["Private"] = &enumType{}
	metadata.contracts["Contract"] = &contractType{public: true}

	for _, name := range []string{"i64", "std::array::Array", "Forward", "Public"} {
		if !metadata.isTypeName(name) {
			t.Fatalf("%q was not recognized as a type", name)
		}
	}
	if metadata.isTypeName("Missing") {
		t.Fatal("unknown name was recognized as a type")
	}
	if !metadata.isUserDeclaredType("Public") ||
		!metadata.isUserDeclaredType("Private") ||
		!metadata.isUserDeclaredType("Contract") {
		t.Fatal("retained declaration was not recognized")
	}
	if !metadata.isPublicType("Public") || !metadata.isPublicType("Contract") ||
		metadata.isPublicType("Private") || metadata.isPublicType("Missing") {
		t.Fatal("type visibility did not follow retained metadata")
	}
}
