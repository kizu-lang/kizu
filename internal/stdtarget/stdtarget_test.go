package stdtarget

import "testing"

// TestPredicatesRoundTrip keeps the compiler phases on one closed registry:
// every spelling identifies its predicate, and each target answers exactly
// the predicates that describe it.
func TestPredicatesRoundTrip(t *testing.T) {
	answers := map[Target][]Predicate{
		NativeDarwin: {IsNative, IsDarwin},
		NativeLinux:  {IsNative, IsLinux},
		WasmWASI:     {IsWASI},
		WasmBrowser:  {IsBrowser},
	}
	for _, predicate := range Predicates() {
		name := Spelling(predicate)
		identified, ok := Identify(name)
		if !ok || identified != predicate {
			t.Fatalf("Identify(%q) = (%v, %t), want (%v, true)",
				name, identified, ok, predicate)
		}
		for target, expected := range answers {
			want := false
			for _, answer := range expected {
				want = want || answer == predicate
			}
			if got := Evaluate(target, predicate); got != want {
				t.Fatalf("Evaluate(%v, %v) = %t, want %t", target, predicate, got, want)
			}
		}
	}
	if _, ok := Identify("std::target::unknown"); ok {
		t.Fatal("unknown target predicate was identified")
	}
}

// TestNativeOSRoundTrip keeps the OS names the predicates use and the names
// the manifest sections use as one vocabulary.
func TestNativeOSRoundTrip(t *testing.T) {
	for _, os := range NativeOS() {
		target, ok := NativeFor(os)
		if !ok || target.OS() != os || !target.IsNative() {
			t.Fatalf("NativeFor(%q) = (%v, %t)", os, target, ok)
		}
	}
	if _, ok := NativeFor("windows"); ok {
		t.Fatal("an unsupported OS named a native target")
	}
	if WasmWASI.OS() != "" {
		t.Fatal("a wasm target names an OS")
	}
}
