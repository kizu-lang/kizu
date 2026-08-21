package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/typ"
)

// TestTypeTableReusesParsedStructure keeps one parsed tree behind each
// canonical spelling in a checker phase.
func TestTypeTableReusesParsedStructure(t *testing.T) {
	table := newTypeTable()
	first, ok := table.lookup("ParseError!Array<i64>")
	if !ok {
		t.Fatal("type table rejected a canonical type")
	}
	second, ok := table.lookup("ParseError!Array<i64>")
	if !ok || first != second {
		t.Fatal("type table parsed the same spelling more than once")
	}

	_, success, ok := table.errorUnionParts("ParseError!Array<i64>")
	if !ok {
		t.Fatal("type table did not expose error union structure")
	}
	_, wantSuccess, ok := typ.ErrorUnionParts(first)
	if !ok {
		t.Fatal("parsed type is not an error union")
	}
	cachedSuccess, ok := table.lookup(success)
	if !ok || cachedSuccess != wantSuccess {
		t.Fatal("type table did not retain the parsed success type")
	}
}
