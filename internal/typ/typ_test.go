package typ

import "testing"

// roundTrip lists the type spellings the compiler writes, so parsing and
// printing a type has to give back the text it came from.
var roundTrip = []string{
	"i64", "u8", "bool", "void", "f64", "usize",
	"Io", "Allocator", "Function", "Self", "T",
	"Point", "std::map::Map", "std::arena::Handle",
	"[]u8", "[][]u8", "[]Point",
	"[4]u8", "[32]Array<Point>",
	"&i64", "&var i64", "&[]u8", "&var Point",
	"?ptr<u8>", "?i64",
	"!void", "!i64", "![]u8", "!&i64", "!&var i64",
	"Error!i64", "std::fs::Error![]u8",
	"ptr<u8>", "ptr<const u8>",
	"Array<i64>", "Box<Point>", "Channel<[]u8>",
	"std::map::Map<[]u8, i64>",
	"std::map::Map<[]u8, Array<Point>>",
	"Pair<i64, bool>", "Pair<[]u8, &var Point>",
	"Result<!void, Error!i64>",
}

// TestParsePrintsBackTheSpelling keeps the parsed form faithful to its text.
func TestParsePrintsBackTheSpelling(t *testing.T) {
	for _, text := range roundTrip {
		parsed, err := Parse(text)
		if err != nil {
			t.Fatalf("Parse(%q): %v", text, err)
		}
		if got := parsed.String(); got != text {
			t.Fatalf("Parse(%q).String() = %q", text, got)
		}
	}
}

// TestParseRejectsTextThatIsNotAType keeps malformed spellings out.
func TestParseRejectsTextThatIsNotAType(t *testing.T) {
	for _, text := range []string{
		"", "<i64>", "Map<i64", "Map<>", "Map<i64>>", "i64 i64", "[]",
	} {
		if parsed, err := Parse(text); err == nil {
			t.Fatalf("Parse(%q) = %v, want error", text, parsed)
		}
	}
}

// TestErrorUnionPartsReadsParsedStructure keeps structural queries free of
// spelling scans: a nested error union is not the root error union.
func TestErrorUnionPartsReadsParsedStructure(t *testing.T) {
	bare, err := Parse("!i64")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	set, success, ok := ErrorUnionParts(bare)
	if !ok || set != nil || success.String() != "i64" {
		t.Fatalf("ErrorUnionParts(!i64) = (%v, %v, %t)", set, success, ok)
	}

	declared, err := Parse("ParseError!i64")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	set, success, ok = ErrorUnionParts(declared)
	if !ok || set.String() != "ParseError" || success.String() != "i64" {
		t.Fatalf("ErrorUnionParts(ParseError!i64) = (%v, %v, %t)", set, success, ok)
	}

	nested, err := Parse("Array<!i64>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if set, success, ok = ErrorUnionParts(nested); ok {
		t.Fatalf("ErrorUnionParts(Array<!i64>) = (%v, %v, true)", set, success)
	}
}

// TestAbsorbsErrorSetComparesParsedStructure checks the one allowed error-set
// absorption without returning to spelling comparisons.
func TestAbsorbsErrorSetComparesParsedStructure(t *testing.T) {
	want, err := Parse("!Array<i64>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := Parse("ParseError!Array<i64>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !AbsorbsErrorSet(want, got) {
		t.Fatal("!Array<i64> did not absorb ParseError!Array<i64>")
	}

	other, err := Parse("ParseError!Array<u8>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if AbsorbsErrorSet(want, other) {
		t.Fatal("!Array<i64> absorbed ParseError!Array<u8>")
	}
}

// TestTableReusesParsedStructure reuses a root and makes its nested handles
// available by canonical spelling.
func TestTableReusesParsedStructure(t *testing.T) {
	table := NewTable()
	first, err := table.Parse("ParseError!Array<i64>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	second, err := table.Parse("ParseError!Array<i64>")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if first != second {
		t.Fatal("table parsed the same spelling more than once")
	}

	_, success, ok := ErrorUnionParts(first)
	if !ok {
		t.Fatal("parsed type is not an error union")
	}
	cachedSuccess, err := table.Parse(success.String())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cachedSuccess != success {
		t.Fatal("table did not retain the nested success type")
	}
}

// TestSubstituteReplacesWholeNamesOnly is the bug the spelling-level
// substitution had: replacing `T` inside `[]u8` produced `[]i648`.
func TestSubstituteReplacesWholeNamesOnly(t *testing.T) {
	subst := map[string]string{"u": "i64", "T": "i64"}
	for _, tc := range []struct{ in, want string }{
		{"[]u8", "[]u8"},
		{"[]Timer", "[]Timer"},
		{"&Tag", "&Tag"},
		{"[]T", "[]i64"},
		{"[4]T", "[4]i64"},
		{"&var T", "&var i64"},
		{"!T", "!i64"},
		{"Pair<T, []T>", "Pair<i64, []i64>"},
		{"std::map::Map<[]u8, T>", "std::map::Map<[]u8, i64>"},
	} {
		got, err := SubstituteText(tc.in, subst)
		if err != nil {
			t.Fatalf("SubstituteText(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("SubstituteText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWalkVisitsBufferElement keeps fixed buffers on the same structural walk as slices.
func TestWalkVisitsBufferElement(t *testing.T) {
	parsed, err := Parse("[4]Secret")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var visited []string
	Walk(parsed, func(node Type) {
		visited = append(visited, node.String())
	})
	want := []string{"[4]Secret", "Secret"}
	if len(visited) != len(want) {
		t.Fatalf("Walk visited %q, want %q", visited, want)
	}
	for index := range want {
		if visited[index] != want[index] {
			t.Fatalf("Walk visited %q, want %q", visited, want)
		}
	}
}

// TestSplitArgsKeepsNestedSpellingsWhole checks the one depth-aware split.
func TestSplitArgsKeepsNestedSpellingsWhole(t *testing.T) {
	got, err := SplitArgs("[]u8, std::map::Map<[]u8, i64>, bool")
	if err != nil {
		t.Fatalf("SplitArgs: %v", err)
	}
	want := []string{"[]u8", "std::map::Map<[]u8, i64>", "bool"}
	if len(got) != len(want) {
		t.Fatalf("SplitArgs = %q, want %q", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("SplitArgs = %q, want %q", got, want)
		}
	}
	for _, text := range []string{"i64,", ",i64", "Map<i64", "i64>"} {
		if parts, err := SplitArgs(text); err == nil {
			t.Fatalf("SplitArgs(%q) = %q, want error", text, parts)
		}
	}
}
