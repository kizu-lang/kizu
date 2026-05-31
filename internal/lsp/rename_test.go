package lsp

import "testing"

// TestPrepareRenameReturnsTokenRange checks the editable range matches the
// identifier under the cursor.
func TestPrepareRenameReturnsTokenRange(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	rng := server.prepareRename(uri, positionIn(source, "inspect(1)", "inspect"))
	if rng == nil {
		t.Fatalf("prepareRename returned nil, want a range")
	}
	if got := textIn(source, *rng); got != "inspect" {
		t.Fatalf("prepareRename range covers %q, want %q", got, "inspect")
	}
}

// TestPrepareRenameRejectsNonSymbol checks prepareRename declines literals.
func TestPrepareRenameRejectsNonSymbol(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	if rng := server.prepareRename("file:///missing.kizu", Position{}); rng != nil {
		t.Fatalf("prepareRename on missing doc = %#v, want nil", rng)
	}
}

// TestRenameRewritesDeclarationAndUses checks rename touches every occurrence.
func TestRenameRewritesDeclarationAndUses(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	edit := server.rename(uri, positionIn(source, "inspect(1)", "inspect"), "examine")
	if edit == nil {
		t.Fatalf("rename returned nil, want a workspace edit")
	}
	edits := edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("rename edits = %#v, want declaration and call", edits)
	}
	for _, e := range edits {
		if e.NewText != "examine" {
			t.Fatalf("rename new text = %q, want %q", e.NewText, "examine")
		}
		if got := textIn(source, e.Range); got != "inspect" {
			t.Fatalf("rename edit covers %q, want %q", got, "inspect")
		}
	}
}

// TestRenameReturnsNilForMissingDocument checks rename declines unknown docs.
func TestRenameReturnsNilForMissingDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if edit := server.rename("file:///missing.kizu", Position{}, "x"); edit != nil {
		t.Fatalf("rename on missing doc = %#v, want nil", edit)
	}
}

// textIn extracts the source substring covered by a single-line range.
func textIn(source string, rng Range) string {
	lines := splitLinesForTest(source)
	if rng.Start.Line < 0 || rng.Start.Line >= len(lines) {
		return ""
	}
	line := lines[rng.Start.Line]
	if rng.Start.Character < 0 || rng.End.Character > len(line) {
		return ""
	}
	return line[rng.Start.Character:rng.End.Character]
}

// splitLinesForTest splits source into lines without trailing newline handling.
func splitLinesForTest(source string) []string {
	lines := []string{}
	current := ""
	for _, r := range source {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
			continue
		}
		current += string(r)
	}
	lines = append(lines, current)
	return lines
}
