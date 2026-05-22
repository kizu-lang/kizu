package fmt

import (
	"testing"
)

// TestFormatHelloIsMultiLine lays out a one-line function across multiple lines.
func TestFormatHelloIsMultiLine(t *testing.T) {
	src := `fn main() { print("hello, kizu"); }`
	want := "fn main() {\n" +
		"    print(\"hello, kizu\");\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(hello):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesAlreadyFormatted checks idempotency on canonical input.
func TestFormatPreservesAlreadyFormatted(t *testing.T) {
	src := "fn main() {\n    print(\"hello, kizu\");\n}\n"
	if got := Format(src); got != src {
		t.Fatalf("Format(idempotent):\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestFormatIsIdempotent checks Format(Format(x)) == Format(x) on compact input.
func TestFormatIsIdempotent(t *testing.T) {
	src := `fn main(){print("a");print("b");}fn other(){return;}`
	once := Format(src)
	twice := Format(once)
	if once != twice {
		t.Fatalf("non-idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

// TestFormatTopLevelBlankLineSeparator checks a blank line separates top-level declarations.
func TestFormatTopLevelBlankLineSeparator(t *testing.T) {
	src := `fn a() { return; } fn b() { return; }`
	want := "fn a() {\n" +
		"    return;\n" +
		"}\n" +
		"\n" +
		"fn b() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(two fns):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatTrailingCommaDroppedBeforeClose checks that `,}` becomes `}`.
func TestFormatTrailingCommaDroppedBeforeClose(t *testing.T) {
	src := "struct Point {\n    x: i64,\n    y: i64,\n}\n"
	want := "struct Point {\n    x: i64,\n    y: i64\n}\n"
	if got := Format(src); got != want {
		t.Fatalf("trailing comma:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatGenericBracketsTight checks generic `<T>` keeps no surrounding spaces.
func TestFormatGenericBracketsTight(t *testing.T) {
	src := `fn main() { let a = std::array::Array<i64>(x); }`
	want := "fn main() {\n" +
		"    let a = std::array::Array<i64>(x);\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("generic brackets:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatComparisonKeepsSpaces checks comparison `>=` keeps surrounding spaces.
func TestFormatComparisonKeepsSpaces(t *testing.T) {
	src := "fn main() {\n    if age >= 18 {\n        x = 1;\n    }\n}\n"
	if got := Format(src); got != src {
		t.Fatalf("comparison spacing changed:\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestFormatEmptyBlockStaysInline checks `{}` is not expanded onto multiple lines.
func TestFormatEmptyBlockStaysInline(t *testing.T) {
	src := `fn main() {}`
	want := "fn main() {}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(empty block):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatMultilineStringPreservesNewlines checks `\\` multi-line strings round-trip.
func TestFormatMultilineStringPreservesNewlines(t *testing.T) {
	src := "fn main() {\n" +
		"    let h =\n" +
		"        \\\\Hello\n" +
		"        \\\\World\n" +
		"    ;\n" +
		"    print(h);\n" +
		"}\n"
	got := Format(src)
	// Round-trip: Format(Format(src)) should equal Format(src).
	if got2 := Format(got); got2 != got {
		t.Fatalf("non-idempotent multiline string:\n--- got1 ---\n%s\n--- got2 ---\n%s", got, got2)
	}
}
