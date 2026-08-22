package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// TestDeclarationPolicyDecisionsStayIndependentOfDiagnostics keeps the policy
// layer copy-only so the checker boundary remains the one diagnostic owner.
func TestDeclarationPolicyDecisionsStayIndependentOfDiagnostics(t *testing.T) {
	function := &typ.Name{Path: []string{"Function"}}
	field := &typ.Name{Path: []string{"Field"}}
	signature := ast.FunctionSignature{StaticParams: []ast.StaticParam{
		{Name: "field", Type: field},
		{Name: "worker", Type: function},
	}}
	index, reserved := reservedFunctionStaticParamIndex(signature)
	if !reserved || index != 1 {
		t.Fatalf("reserved parameter = (%d, %t), want (1, true)", index, reserved)
	}
	signature.Std = true
	if _, reserved := reservedFunctionStaticParamIndex(signature); reserved {
		t.Fatal("std Function parameter was rejected")
	}

	table := newTypeTable()
	if !compileTimeOnlyType(&table, "?Function") {
		t.Fatal("wrapped Function was treated as a runtime type")
	}
	if compileTimeOnlyType(&table, "i64") {
		t.Fatal("i64 was treated as a compile-time-only type")
	}
	if !rawPointerFieldRequiresUnsafe(&table, false, "[]ptr<u8>") {
		t.Fatal("unmarked raw-pointer field was accepted")
	}
	if rawPointerFieldRequiresUnsafe(&table, true, "[]ptr<u8>") {
		t.Fatal("unsafe struct raw-pointer field was rejected")
	}

	if !isBorrowPayload(&typ.Borrow{Elem: &typ.Name{Path: []string{"Item"}}}) {
		t.Fatal("borrow payload was accepted")
	}
	if isBorrowPayload(&typ.Name{Path: []string{"Item"}}) {
		t.Fatal("owned payload was rejected")
	}
}
