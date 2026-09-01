package stdtarget

import "testing"

// TestPredicatesRoundTrip keeps the compiler phases on one closed registry.
func TestPredicatesRoundTrip(t *testing.T) {
	targets := []Target{Native, WasmWASI, WasmBrowser}
	for index, predicate := range Predicates() {
		name := Spelling(predicate)
		identified, ok := Identify(name)
		if !ok || identified != predicate {
			t.Fatalf("Identify(%q) = (%v, %t), want (%v, true)",
				name, identified, ok, predicate)
		}
		for targetIndex, target := range targets {
			if got, want := Evaluate(target, predicate), targetIndex == index; got != want {
				t.Fatalf("Evaluate(%v, %v) = %t, want %t", target, predicate, got, want)
			}
		}
	}
	if _, ok := Identify("std::target::unknown"); ok {
		t.Fatal("unknown target predicate was identified")
	}
}
