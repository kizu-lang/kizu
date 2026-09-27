package staticexpr

import "testing"

// TestEval checks the canonical spelling against names bound by the caller.
func TestEval(t *testing.T) {
	names := map[string]int64{"n1": 3, "k2": 2}
	lookup := func(name string) (int64, bool) {
		value, ok := names[name]
		return value, ok
	}
	cases := map[string]int64{
		"(n1 * k2)":             6,
		"((n1 * k2) % 4)":       2,
		"((n1 + 21) - -k2)":     26,
		"(((n1 * 8) + k2) / 5)": 5,
		"(-n1 * k2)":            -6,
	}
	for text, want := range cases {
		got, err := Eval(text, lookup)
		if err != nil || got != want {
			t.Fatalf("Eval(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"(n1 * missing)", "(n1 / 0)", "(n1 * k2"} {
		if _, err := Eval(text, lookup); err == nil {
			t.Fatalf("Eval(%q) succeeded; want an error", text)
		}
	}
}

// TestNames lists the names an expression reads and skips its literals.
func TestNames(t *testing.T) {
	got := Names("((n1 * 24) + (0 - k2))")
	if len(got) != 2 || got[0] != "n1" || got[1] != "k2" {
		t.Fatalf("Names = %v; want [n1 k2]", got)
	}
}
