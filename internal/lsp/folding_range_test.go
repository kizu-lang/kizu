package lsp

import "testing"

// foldingFixture is a small document with a multi-line function body, a
// multi-line struct, an import block, and a single-line block that must not
// fold. Line numbers (0-based) are noted for the assertions below.
//
//	0  import std::io;
//	1  import std::mem;
//	2
//	3  struct Point {
//	4      x: i64,
//	5      y: i64,
//	6  }
//	7
//	8  fn main() {
//	9      let p = Point { x: 1, y: 2 };
//	10     return;
//	11 }
//	12
//	13 fn noop() { return; }
func foldingFixture() string {
	return "import std::io;\n" +
		"import std::mem;\n" +
		"\n" +
		"struct Point {\n" +
		"    x: i64,\n" +
		"    y: i64,\n" +
		"}\n" +
		"\n" +
		"fn main() {\n" +
		"    let p = Point { x: 1, y: 2 };\n" +
		"    return;\n" +
		"}\n" +
		"\n" +
		"fn noop() { return; }\n"
}

// hasFold reports whether ranges contains a region with the given bounds.
func hasFold(ranges []foldingRange, startLine int, endLine int) bool {
	for _, r := range ranges {
		if r.StartLine == startLine && r.EndLine == endLine {
			return true
		}
	}
	return false
}

// TestFoldingRangesCoversMultiLineBlocks checks struct and function bodies fold.
func TestFoldingRangesCoversMultiLineBlocks(t *testing.T) {
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = foldingFixture()

	ranges := server.foldingRanges(uri)

	if !hasFold(ranges, 3, 5) {
		t.Errorf("struct body fold (3..5) missing in %#v", ranges)
	}
	if !hasFold(ranges, 8, 10) {
		t.Errorf("function body fold (8..10) missing in %#v", ranges)
	}
}

// TestFoldingRangesGroupsImports checks consecutive imports fold together.
func TestFoldingRangesGroupsImports(t *testing.T) {
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = foldingFixture()

	ranges := server.foldingRanges(uri)

	found := false
	for _, r := range ranges {
		if r.Kind == "imports" && r.StartLine == 0 && r.EndLine == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("import block fold (0..1, kind imports) missing in %#v", ranges)
	}
}

// TestFoldingRangesSkipsSingleLineBlocks checks one-line braces do not fold.
func TestFoldingRangesSkipsSingleLineBlocks(t *testing.T) {
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = foldingFixture()

	ranges := server.foldingRanges(uri)

	for _, r := range ranges {
		if r.StartLine == 13 {
			t.Errorf("single-line block on line 13 should not fold: %#v", r)
		}
	}
}

// TestFoldingRangesUnknownDocument checks unknown docs yield an empty slice.
func TestFoldingRangesUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if ranges := server.foldingRanges("file:///missing.kizu"); len(ranges) != 0 {
		t.Fatalf("foldingRanges on missing doc = %#v, want empty", ranges)
	}
}
