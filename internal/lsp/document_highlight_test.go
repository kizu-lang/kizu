package lsp

import "testing"

// TestDocumentHighlightsCoverFunctionUses checks the caret symbol is highlighted
// at its declaration and every use within the same document.
func TestDocumentHighlightsCoverFunctionUses(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	highlights := server.documentHighlights(uri, positionIn(source, "inspect(1)", "inspect"))
	if len(highlights) != 2 {
		t.Fatalf("function highlights = %#v, want declaration and call", highlights)
	}
	for _, h := range highlights {
		if h.Kind != documentHighlightKindText {
			t.Fatalf("highlight kind = %d, want text", h.Kind)
		}
	}
}

// TestDocumentHighlightsCoverLocalUses checks a local binding highlights its uses.
func TestDocumentHighlightsCoverLocalUses(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	highlights := server.documentHighlights(uri, positionIn(source, "trace.label", "trace"))
	if len(highlights) < 2 {
		t.Fatalf("local highlights = %#v, want declaration and uses", highlights)
	}
}

// TestDocumentHighlightsEmptyForNonIdentifier checks no highlights off a symbol.
func TestDocumentHighlightsEmptyForNonIdentifier(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	highlights := server.documentHighlights("file:///missing.kizu", Position{Line: 0, Character: 0})
	if len(highlights) != 0 {
		t.Fatalf("highlights for missing document = %#v, want empty", highlights)
	}
}
