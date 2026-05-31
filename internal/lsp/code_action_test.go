package lsp

import "testing"

// TestCodeActionOrganizeImportsSorts checks an unsorted import run is reordered.
func TestCodeActionOrganizeImportsSorts(t *testing.T) {
	uri := "file:///main.kizu"
	source := "import std::mem;\n" +
		"import std::io;\n" +
		"import std::fs;\n" +
		"\n" +
		"fn main() {}\n"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	actions := server.codeActions(uri)
	if len(actions) != 1 {
		t.Fatalf("codeActions = %#v, want one organize-imports action", actions)
	}
	if actions[0].Kind != codeActionOrganizeImports {
		t.Fatalf("action kind = %q, want %q", actions[0].Kind, codeActionOrganizeImports)
	}
	edits := actions[0].Edit.Changes[uri]
	if len(edits) != 1 {
		t.Fatalf("edits = %#v, want one run replacement", edits)
	}
	want := "import std::fs;\nimport std::io;\nimport std::mem;\n"
	if edits[0].NewText != want {
		t.Fatalf("sorted text = %q, want %q", edits[0].NewText, want)
	}
	// The replaced range must cover exactly the three import lines.
	if edits[0].Range.Start.Line != 0 || edits[0].Range.End.Line != 3 {
		t.Fatalf("range = %#v, want lines 0..3", edits[0].Range)
	}
}

// TestCodeActionSkipsSortedImports checks already-sorted imports yield no action.
func TestCodeActionSkipsSortedImports(t *testing.T) {
	uri := "file:///main.kizu"
	source := "import std::fs;\n" +
		"import std::io;\n" +
		"\n" +
		"fn main() {}\n"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	if actions := server.codeActions(uri); len(actions) != 0 {
		t.Fatalf("codeActions on sorted imports = %#v, want empty", actions)
	}
}

// TestCodeActionKeepsSeparateRuns checks comment-separated runs sort independently.
func TestCodeActionKeepsSeparateRuns(t *testing.T) {
	uri := "file:///main.kizu"
	source := "import std::mem;\n" +
		"import std::io;\n" +
		"\n" +
		"import zoo::b;\n" +
		"import zoo::a;\n" +
		"\n" +
		"fn main() {}\n"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	actions := server.codeActions(uri)
	if len(actions) != 1 {
		t.Fatalf("codeActions = %#v, want one action", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("edits = %#v, want one per run", edits)
	}
}

// TestCodeActionUnknownDocument returns an empty slice.
func TestCodeActionUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if actions := server.codeActions("file:///missing.kizu"); len(actions) != 0 {
		t.Fatalf("codeActions on missing doc = %#v, want empty", actions)
	}
}
