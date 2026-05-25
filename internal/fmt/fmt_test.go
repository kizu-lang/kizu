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

// TestFormatPreservesFunctionLineComment keeps doc-style comments before functions.
func TestFormatPreservesFunctionLineComment(t *testing.T) {
	src := "// explain main\nfn main(){return;}\n"
	want := "// explain main\n" +
		"fn main() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(commented fn):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesFunctionDocComment keeps `///` documentation attached.
func TestFormatPreservesFunctionDocComment(t *testing.T) {
	src := "/// explain main\nfn main(){return;}\n"
	want := "/// explain main\n" +
		"fn main() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(doc commented fn):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesImplMethodDocComment keeps method docs inside impl bodies.
func TestFormatPreservesImplMethodDocComment(t *testing.T) {
	src := "impl Parser{\n/// Advances.\nfn advance(self: Parser)->void{return;}}\n"
	want := "impl Parser {\n" +
		"    /// Advances.\n" +
		"    fn advance(self: Parser) -> void {\n" +
		"        return;\n" +
		"    }\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(doc commented method):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatKeepsFunctionLineCommentAttached keeps doc comments with following functions.
func TestFormatKeepsFunctionLineCommentAttached(t *testing.T) {
	src := "fn helper(){return;}\n// explain main\nfn main(){return;}\n"
	want := "fn helper() {\n" +
		"    return;\n" +
		"}\n" +
		"// explain main\n" +
		"fn main() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(comment after fn):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesBlankLineBeforeTopLevelComment keeps section comments separated.
func TestFormatPreservesBlankLineBeforeTopLevelComment(t *testing.T) {
	src := "fn helper(){return;}\n\n// color choices\nenum Color{Red}\n"
	want := "fn helper() {\n" +
		"    return;\n" +
		"}\n" +
		"\n" +
		"// color choices\n" +
		"enum Color {\n" +
		"    Red,\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(blank before comment):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesBlankLineAfterTopLevelComment keeps section comments detached.
func TestFormatPreservesBlankLineAfterTopLevelComment(t *testing.T) {
	src := "fn helper(){return;}\n// color choices\n\nenum Color{Red}\n"
	want := "fn helper() {\n" +
		"    return;\n" +
		"}\n" +
		"// color choices\n" +
		"\n" +
		"enum Color {\n" +
		"    Red,\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(blank after comment):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatSortsLeadingImports keeps the import block canonical without blank lines inside it.
func TestFormatSortsLeadingImports(t *testing.T) {
	src := `import selfhost::parser;
import selfhost;
import selfhost::lexer;
fn main(){return;}`
	want := "import selfhost;\n" +
		"import selfhost::lexer;\n" +
		"import selfhost::parser;\n" +
		"\n" +
		"fn main() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(imports):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatEnumKeepsTrailingComma checks enum declarations prefer trailing commas.
func TestFormatEnumKeepsTrailingComma(t *testing.T) {
	src := "enum Color { Red, Green }\n"
	want := "enum Color {\n" +
		"    Red, Green,\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("enum trailing comma:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatEnumAddsTrailingCommaBeforeComment keeps enum comments after the comma.
func TestFormatEnumAddsTrailingCommaBeforeComment(t *testing.T) {
	src := "enum Color {\n    Red\n    // primary\n}\n"
	want := "enum Color {\n" +
		"    Red,\n" +
		"    // primary\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("enum comment trailing comma:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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

// TestFormatNullableTypeTight checks nullable `?T` keeps no internal space.
func TestFormatNullableTypeTight(t *testing.T) {
	src := `extern "c" fn source() -> ?ptr<const u8>`
	want := "extern \"c\" fn source() -> ?ptr<const u8>\n"
	if got := Format(src); got != want {
		t.Fatalf("nullable type:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
