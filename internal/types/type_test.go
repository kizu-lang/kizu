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

// TestTypeTableWrappedPredicatesWalkRetainedGraphs keeps semantic type queries
// on the parsed graph instead of reparsing canonical spellings into text parts.
func TestTypeTableWrappedPredicatesWalkRetainedGraphs(t *testing.T) {
	table := newTypeTable()
	for _, tc := range []struct {
		name  string
		value Type
		match func(Type) bool
	}{
		{
			name:  "raw pointer in nested generic",
			value: "Pair<?[]ptr<i64>, bool>",
			match: table.containsRawPointer,
		},
		{
			name:  "type value in const argument",
			value: "std::array::Array<const type>",
			match: table.containsTypeValue,
		},
		{
			name:  "compile-time token in error union buffer",
			value: "ParseError![4]Function",
			match: table.containsCompileTimeOnly,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.match(tc.value) {
				t.Fatalf("predicate rejected %q", tc.value)
			}
		})
	}
	if table.containsCompileTimeOnly("Pair<i64, bool>") {
		t.Fatal("runtime-only type matched a compile-time token")
	}
}
