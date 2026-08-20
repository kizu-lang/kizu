package ast

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/typ"
)

// name spells a plain type for a test declaration.
func name(text string) typ.Type { return typeName(text) }

// optional spells `?T` for a test declaration.
func optional(text string) typ.Type { return &typ.Optional{Elem: typeName(text)} }

// TestDeriveDeinitConsumesWhatTheTypeHolds checks the derived body reaches each
// owner field and skips the rest: a copy field owes nothing, and an optional
// owner is opened before it is consumed.
func TestDeriveDeinitConsumesWhatTheTypeHolds(t *testing.T) {
	decl := &StructDecl{Name: "Visitor", Fields: []Field{
		{Name: "name", TypeName: name("std::string::String")},
		{Name: "age", TypeName: name("i64")},
		{Name: "nick", TypeName: optional("std::string::String")},
	}}
	owners := map[string]bool{"std::string::String": true}
	fn := DeriveDeinit(owners, decl)
	if fn == nil {
		t.Fatal("a struct holding a String should derive a deinit")
	}
	if len(fn.Body.Statements) != 3 {
		t.Fatalf("got %d statements, want the two owner fields and a return",
			len(fn.Body.Statements))
	}
	if got := fn.Body.Statements[0].String(); got != "self.name.deinit();" {
		t.Fatalf("owner field: got %q", got)
	}
	if _, ok := fn.Body.Statements[1].(*IfStmt); !ok {
		t.Fatalf("optional owner field: got %T, want it opened first",
			fn.Body.Statements[1])
	}
}

// TestDeriveDeinitSkipsTypesHoldingNothing checks a type with no obligation
// gets no deinit, so it stays a plain value rather than becoming an owner.
func TestDeriveDeinitSkipsTypesHoldingNothing(t *testing.T) {
	decl := &StructDecl{Name: "Point", Fields: []Field{
		{Name: "x", TypeName: name("i64")},
		{Name: "y", TypeName: name("i64")},
	}}
	if fn := DeriveDeinit(map[string]bool{}, decl); fn != nil {
		t.Fatalf("got a derived deinit for a copy struct: %s", fn.Body.String())
	}
}

// TestDeriveDeinitMatchesTheActiveVariant checks a union gets one arm per
// variant, since the match has to be exhaustive, and consumes only the ones
// carrying an owner.
func TestDeriveDeinitMatchesTheActiveVariant(t *testing.T) {
	decl := &UnionDecl{Name: "Slot", Variants: []UnionVariant{
		{Name: "Kept", Payload: name("std::string::String")},
		{Name: "Count", Payload: name("i64")},
		{Name: "Vacant"},
	}}
	owners := map[string]bool{"std::string::String": true}
	fn := DeriveDeinit(owners, decl)
	if fn == nil {
		t.Fatal("a union carrying a String should derive a deinit")
	}
	match, ok := fn.Body.Statements[0].(*MatchStmt)
	if !ok {
		t.Fatalf("got %T, want a match on the receiver", fn.Body.Statements[0])
	}
	if len(match.Arms) != len(decl.Variants) {
		t.Fatalf("got %d arms, want one per variant", len(match.Arms))
	}
	if match.Arms[0].Binding == "" {
		t.Fatal("the owner variant should bind its payload")
	}
	if match.Arms[1].Binding != "" || match.Arms[2].Binding != "" {
		t.Fatal("a variant carrying no owner should bind nothing")
	}
}

// TestDeinitOwnersTransitivelyIncludesHolders checks holding an owner makes a
// type one, through as many steps as the declarations take.
func TestDeinitOwnersTransitivelyIncludesHolders(t *testing.T) {
	program := &Program{Decls: []Decl{
		&StructDecl{Name: "Outer", Fields: []Field{
			{Name: "middle", TypeName: name("Middle")},
		}},
		&StructDecl{Name: "Middle", Fields: []Field{
			{Name: "inner", TypeName: name("Inner")},
		}},
		&StructDecl{Name: "Inner", Fields: []Field{
			{Name: "bytes", TypeName: name("std::array::Array<u8>")},
		}},
		&FunctionDecl{FunctionSignature: FunctionSignature{
			Receiver: true,
			Name:     "std::array::Array.deinit",
		}},
	}}
	owners := DeinitOwners(program)
	for _, want := range []string{"Inner", "Middle", "Outer"} {
		if !owners[want] {
			t.Fatalf("`%s` holds an owner and should be one", want)
		}
	}
}
