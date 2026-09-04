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

// TestFormatErrDeferCleanup checks errdefer lays out like a statement keyword.
func TestFormatErrDeferCleanup(t *testing.T) {
	src := `fn build(allocator: Allocator) -> !void {errdefer values.deinit(allocator);return;}`
	want := "fn build(allocator: Allocator) -> !void {\n" +
		"    errdefer values.deinit(allocator);\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(errdefer):\n--- got ---\n%s\n--- want ---\n%s", got, want)
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

// TestFormatUnsafeFnDeclaration keeps the unsafe marker on the declaration.
func TestFormatUnsafeFnDeclaration(t *testing.T) {
	src := `unsafe fn raw(){return;}`
	want := "unsafe fn raw() {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(unsafe fn):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatUnsafeExpression keeps the unsafe marker on the expression it covers.
func TestFormatUnsafeExpression(t *testing.T) {
	src := `fn read(p:ptr<u8>)->u8{return unsafe ptr_read(p);}`
	want := "fn read(p: ptr<u8>) -> u8 {\n" +
		"    return unsafe ptr_read(p);\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(unsafe expr):\n--- got ---\n%s\n--- want ---\n%s", got, want)
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

// TestFormatPreservesMethodDocComment keeps docs on a method declaration, and
// keeps the space that tells a receiver slot from a call.
func TestFormatPreservesMethodDocComment(t *testing.T) {
	src := "/// Advances.\nfn(self: &var Parser)advance()->void{return;}\n"
	want := "/// Advances.\n" +
		"fn (self: &var Parser) advance() -> void {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(doc commented method):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPreservesTypeMemberDocComments keeps docs on fields and variants.
func TestFormatPreservesTypeMemberDocComments(t *testing.T) {
	src := "struct Trace{\n/// Label.\nlabel:[]u8,}\n" +
		"enum Color{\n/// Secondary.\nGreen,}\n"
	want := "struct Trace {\n" +
		"    /// Label.\n" +
		"    label: []u8\n" +
		"}\n" +
		"\n" +
		"enum Color {\n" +
		"    /// Secondary.\n" +
		"    Green,\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(type member docs):\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
	src := `import app::parser;
import app;
import app::lexer;
fn main(){return;}`
	want := "import app;\n" +
		"import app::lexer;\n" +
		"import app::parser;\n" +
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
	src := `fn main() { let a = std::array::new<i64>(x); }`
	want := "fn main() {\n" +
		"    let a = std::array::new<i64>(x);\n" +
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

// TestFormatMultilineStringPreservesQuotes keeps values that cannot be
// represented by Kizu's escape-free single-line literal syntax parseable.
func TestFormatMultilineStringPreservesQuotes(t *testing.T) {
	src := "fn main() {\n" +
		"    let input =\n" +
		"        \\\\ \"quoted\" value\n" +
		"    ;\n" +
		"    print(input);\n" +
		"}\n"
	want := src
	got := Format(src)
	if got != want {
		t.Fatalf("quoted multiline string changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got2 := Format(got); got2 != got {
		t.Fatalf("non-idempotent quoted multiline string:\n--- got1 ---\n%s\n--- got2 ---\n%s", got, got2)
	}
}

// TestFormatMultilineStringPreservesCommentText keeps `//` in multiline
// payloads out of the formatter's independent line-comment stream.
func TestFormatMultilineStringPreservesCommentText(t *testing.T) {
	src := "fn main() {\n" +
		"    let input =\n" +
		"        \\\\// SAFETY: payload, not a source comment\n" +
		"        \\\\/// docs are payload here too\n" +
		"    ;\n" +
		"    print(input);\n" +
		"}\n"
	got := Format(src)
	if got != src {
		t.Fatalf("comment-like multiline text changed:\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
	if got2 := Format(got); got2 != got {
		t.Fatalf("non-idempotent comment-like multiline text:"+
			"\n--- got1 ---\n%s\n--- got2 ---\n%s", got, got2)
	}
}

// TestFormatUnaryMinusHugsValue checks a sign hugs its value while
// subtraction keeps its spaces.
func TestFormatUnaryMinusHugsValue(t *testing.T) {
	src := "fn f(x: i64) -> i64 {\n" +
		"    let a = x - 1;\n" +
		"    let b = x - - 1;\n" +
		"    if x < - 2 {\n" +
		"        return - 3;\n" +
		"    }\n" +
		"    return f(- 4) orelse - 5;\n" +
		"}\n"
	want := "fn f(x: i64) -> i64 {\n" +
		"    let a = x - 1;\n" +
		"    let b = x - -1;\n" +
		"    if x < -2 {\n" +
		"        return -3;\n" +
		"    }\n" +
		"    return f(-4) orelse -5;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(unary minus):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got := Format(want); got != want {
		t.Fatalf("Format(unary minus idempotent):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatGroupingParenTakesSpace checks `(` hugs a callee but not a group.
func TestFormatGroupingParenTakesSpace(t *testing.T) {
	src := "fn f(x: i64) -> i64 {\n" +
		"    let c =(x + 1) * 2;\n" +
		"    return f(c)(x);\n" +
		"}\n"
	want := "fn f(x: i64) -> i64 {\n" +
		"    let c = (x + 1) * 2;\n" +
		"    return f(c)(x);\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(grouping paren):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got := Format(want); got != want {
		t.Fatalf("Format(grouping paren idempotent):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatPostfixIndexAndComparisonGroup keeps postfix brackets attached
// while a grouping parenthesis after `>` remains visibly separate.
func TestFormatPostfixIndexAndComparisonGroup(t *testing.T) {
	src := "fn read(bytes: []u8, index: i64) -> u8 {\n" +
		"    if bytes [index] >(0) {\n" +
		"        return bytes [index];\n" +
		"    }\n" +
		"}\n"
	want := "fn read(bytes: []u8, index: i64) -> u8 {\n" +
		"    if bytes[index] > (0) {\n" +
		"        return bytes[index];\n" +
		"    }\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(postfix and group):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatNamedErrorUnionHugsBothTypes keeps `!` attached when an explicit
// error set appears on its left, while the return arrow remains separated.
func TestFormatNamedErrorUnionHugsBothTypes(t *testing.T) {
	src := "fn read() -> ReadError ! []u8 { return Error::Closed; }"
	want := "fn read() -> ReadError![]u8 {\n" +
		"    return Error::Closed;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(named error union):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatContinuationIndent tracks grouping depth instead of flattening
// every source-preserved line break to the surrounding brace depth.
func TestFormatContinuationIndent(t *testing.T) {
	src := "fn calculate(\n" +
		"left: i64,\n" +
		"right: i64\n" +
		") -> i64 {\n" +
		"    return outer(\n" +
		"    inner(\n" +
		"    left\n" +
		"    ),\n" +
		"    right\n" +
		"    );\n" +
		"}\n"
	want := "fn calculate(\n" +
		"    left: i64,\n" +
		"    right: i64\n" +
		") -> i64 {\n" +
		"    return outer(\n" +
		"        inner(\n" +
		"            left\n" +
		"        ),\n" +
		"        right\n" +
		"    );\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(continuation indent):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got := Format(want); got != want {
		t.Fatalf("Format(continuation idempotent):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatSameLineNestedCallClosesAtStatementDepth avoids accumulating
// indentation for delimiters that opened together on one source line.
func TestFormatSameLineNestedCallClosesAtStatementDepth(t *testing.T) {
	src := "fn calculate(value: i64) -> i64 {\n" +
		"    return outer(inner(\n" +
		"    value\n" +
		"    ));\n" +
		"}\n"
	want := "fn calculate(value: i64) -> i64 {\n" +
		"    return outer(inner(\n" +
		"        value\n" +
		"    ));\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(same-line nested call):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatContinuationCommentUsesGroupIndent keeps comments and the token
// after them at the surrounding call's continuation depth.
func TestFormatContinuationCommentUsesGroupIndent(t *testing.T) {
	src := "fn send(value: i64) -> void {\n" +
		"    use(\n" +
		"    // payload\n" +
		"    value\n" +
		"    );\n" +
		"}\n"
	want := "fn send(value: i64) -> void {\n" +
		"    use(\n" +
		"        // payload\n" +
		"        value\n" +
		"    );\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(continuation comment):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatKeepsSameLineAggregateLiteralCompact distinguishes data literals
// from declaration and control-flow blocks.
func TestFormatKeepsSameLineAggregateLiteralCompact(t *testing.T) {
	src := "fn span() -> Span {\n" +
		"    return Span { start: 0, end: 1 };\n" +
		"}\n"
	if got := Format(src); got != src {
		t.Fatalf("Format(compact aggregate):\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestFormatTaggedDeclarationsKeepTrailingComma applies the same list style
// to error names and union variants that enums already use.
func TestFormatTaggedDeclarationsKeepTrailingComma(t *testing.T) {
	src := "error ReadError { Closed, Invalid }\n" +
		"union Value { Int(i64), None }\n"
	want := "error ReadError {\n" +
		"    Closed, Invalid,\n" +
		"}\n" +
		"\n" +
		"union Value {\n" +
		"    Int(i64), None,\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(tagged declarations):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatMethodNamedErrorKeepsNormalBody distinguishes a method name from
// the `error Name { ... }` declaration introducer.
func TestFormatMethodNamedErrorKeepsNormalBody(t *testing.T) {
	src := "struct Diagnostic {}\n" +
		"fn (self: &Diagnostic) error() -> void { return; }\n"
	want := "struct Diagnostic {}\n" +
		"\n" +
		"fn (self: &Diagnostic) error() -> void {\n" +
		"    return;\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(error method):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got := Format(want); got != want {
		t.Fatalf("Format(error method idempotent):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFormatComptimeMatchDoesNotAddArmComma distinguishes the compile-time
// structural body from a runtime match arm list.
func TestFormatComptimeMatchDoesNotAddArmComma(t *testing.T) {
	src := "fn encode<T>(value: &T) -> void {\n" +
		"    comptime match value |v| {\n" +
		"        comptime if std::meta::has_payload<T, v>() {\n" +
		"            print(1);\n" +
		"        } else {\n" +
		"            print(0);\n" +
		"        }\n" +
		"    }\n" +
		"}\n"
	if got := Format(src); got != src {
		t.Fatalf("Format(comptime match):\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestFormatBitwiseAndShiftOperators checks the operators of SPEC §6.9.2 lay
// out as binary operators: a shift's two tokens stay together, the bitwise
// `&` and `|` take a space on both sides, and `~` hugs its operand -- while a
// borrow `&`, a payload capture `|name|`, and a generic `>>` keep their forms.
func TestFormatBitwiseAndShiftOperators(t *testing.T) {
	src := "import std::array;\n\n" +
		"fn main() {\n" +
		"    let x = 1<<3;\n" +
		"    let y = x >>1;\n" +
		"    let z = x&0xF|y^2;\n" +
		"    let n = ~x;\n" +
		"    let r = &x;\n" +
		"    let f = 1.5-2.5e-3;\n" +
		"    let nested = array::new<array::Array<i64>>(allocator);\n" +
		"    if lookup(x) |value,extra| { print(value); }\n" +
		"}\n"
	want := "import std::array;\n\n" +
		"fn main() {\n" +
		"    let x = 1 << 3;\n" +
		"    let y = x >> 1;\n" +
		"    let z = x & 0xF | y ^ 2;\n" +
		"    let n = ~x;\n" +
		"    let r = &x;\n" +
		"    let f = 1.5 - 2.5e-3;\n" +
		"    let nested = array::new<array::Array<i64>>(allocator);\n" +
		"    if lookup(x) |value, extra| {\n" +
		"        print(value);\n" +
		"    }\n" +
		"}\n"
	if got := Format(src); got != want {
		t.Fatalf("Format(bitwise):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if again := Format(want); again != want {
		t.Fatalf("Format(bitwise) is not idempotent:\n--- got ---\n%s", again)
	}
}
