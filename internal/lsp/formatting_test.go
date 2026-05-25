package lsp

import "testing"

// TestFormatEditsReturnsFullDocumentEdit checks LSP formatting uses kizu fmt.
func TestFormatEditsReturnsFullDocumentEdit(t *testing.T) {
	source := "fn main(){return;}"
	edits := FormatEdits(source)
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(edits))
	}
	want := "fn main() {\n" +
		"    return;\n" +
		"}\n"
	if edits[0].NewText != want {
		t.Fatalf("formatted text:\n--- got ---\n%s\n--- want ---\n%s", edits[0].NewText, want)
	}
	if edits[0].Range.Start.Line != 0 || edits[0].Range.Start.Character != 0 {
		t.Fatalf("edit start = %#v, want document start", edits[0].Range.Start)
	}
	if edits[0].Range.End.Line != 0 || edits[0].Range.End.Character != len(source) {
		t.Fatalf("edit end = %#v, want end of source", edits[0].Range.End)
	}
}

// TestFormatEditsSkipsParseInvalidSource avoids rewriting broken editor buffers.
func TestFormatEditsSkipsParseInvalidSource(t *testing.T) {
	edits := FormatEdits("let x = 1;\n")
	if len(edits) != 0 {
		t.Fatalf("got edits %#v, want none", edits)
	}
}

// TestDocumentEndUsesUTF16Characters checks LSP positions for non-ASCII text.
func TestDocumentEndUsesUTF16Characters(t *testing.T) {
	got := documentEnd("a\n🙂")
	if got.Line != 1 || got.Character != 2 {
		t.Fatalf("document end = %#v, want line 1 character 2", got)
	}
}
