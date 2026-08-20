package ast

import "testing"

// TestConstructExpansionMarksOwnerHandoff checks the expansion writes `move` on
// the fields that own something and leaves the rest bare. The ownership checker
// reads the literal as a move, so a field whose hand-off went unmarked would be
// a marker error inside generated code, and one marked without owning anything
// would claim a hand-off that does not happen.
func TestConstructExpansionMarksOwnerHandoff(t *testing.T) {
	fields := []ConstructField{
		{Name: "name", Type: "std::string::String"},
		{Name: "count", Type: "i64"},
	}
	owners := map[string]bool{"std::string::String": true}
	_, expr := ConstructExpansion("User", "make", fields, nil, owners)
	literal, ok := expr.(*StructLiteralExpr)
	if !ok {
		t.Fatalf("got %T, want a struct literal", expr)
	}
	if len(literal.Fields) != len(fields) {
		t.Fatalf("got %d fields, want %d", len(literal.Fields), len(fields))
	}
	if _, ok := literal.Fields[0].Value.(*MoveExpr); !ok {
		t.Fatalf("owner field: got %T, want the hand-off marked", literal.Fields[0].Value)
	}
	if _, ok := literal.Fields[1].Value.(*IdentExpr); !ok {
		t.Fatalf("copy field: got %T, want the binding unmarked", literal.Fields[1].Value)
	}
}
