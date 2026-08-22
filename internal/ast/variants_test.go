package ast

import "testing"

// TestComptimeMatchExpansionUsesPayloadPresence verifies that expansion needs
// only the presence bit and retains the source body's pointer identity.
func TestComptimeMatchExpansionUsesPayloadPresence(t *testing.T) {
	body := &BlockStmt{}
	stmt := &ComptimeMatchStmt{
		Value:   &IdentExpr{Name: "value"},
		Name:    "variant",
		Binding: "payload",
		Body:    body,
	}
	expanded := ComptimeMatchExpansion(stmt, "Slot", []MetaVariant{
		{Name: "Vacant"},
		{Name: "Held", HasPayload: true},
	})

	if len(expanded.Arms) != 2 {
		t.Fatalf("got %d arms, want 2", len(expanded.Arms))
	}
	if expanded.Arms[0].Binding != "" {
		t.Fatalf("tag-only binding = %q, want empty", expanded.Arms[0].Binding)
	}
	if expanded.Arms[1].Binding != "payload" {
		t.Fatalf("payload binding = %q, want payload", expanded.Arms[1].Binding)
	}
	for index, arm := range expanded.Arms {
		if arm.Body != body {
			t.Fatalf("arm %d did not retain source body identity", index)
		}
	}
}
