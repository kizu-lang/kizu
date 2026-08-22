package ast

import "testing"

// TestStringExpressionUsesDeterministicByteQuote keeps AST diagnostics byte-stable.
func TestStringExpressionUsesDeterministicByteQuote(t *testing.T) {
	value := &StringExpr{Value: "café\n"}
	want := `"caf\xC3\xA9\n"`
	if got := value.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
