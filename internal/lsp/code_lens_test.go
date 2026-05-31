package lsp

import "testing"

// lensFor returns the lens whose range covers the named declaration.
func lensFor(t *testing.T, lenses []codeLens, source string, name string) codeLens {
	t.Helper()
	for _, lens := range lenses {
		if textIn(source, lens.Range) == name {
			return lens
		}
	}
	t.Fatalf("no code lens for %q in %#v", name, lenses)
	return codeLens{}
}

// TestCodeLensCountsReferences checks each function lens reports its use count.
func TestCodeLensCountsReferences(t *testing.T) {
	uri := "file:///main.kizu"
	source := callHierarchyFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	lenses := server.codeLenses(uri)
	if len(lenses) != 3 {
		t.Fatalf("code lenses = %#v, want one per function", lenses)
	}

	if title := lensFor(t, lenses, source, "helper").Command.Title; title != "2 references" {
		t.Fatalf("helper lens = %q, want \"2 references\"", title)
	}
	if title := lensFor(t, lenses, source, "leaf").Command.Title; title != "1 reference" {
		t.Fatalf("leaf lens = %q, want \"1 reference\"", title)
	}
	if title := lensFor(t, lenses, source, "main").Command.Title; title != "0 references" {
		t.Fatalf("main lens = %q, want \"0 references\"", title)
	}
}

// TestCodeLensCoversTypes checks struct and contract declarations get lenses.
func TestCodeLensCoversTypes(t *testing.T) {
	uri := "file:///main.kizu"
	source := implementationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	lenses := server.codeLenses(uri)
	// File (struct), Writer (contract), and write (function) all qualify.
	lensFor(t, lenses, source, "File")
	lensFor(t, lenses, source, "Writer")
}

// TestCodeLensUnknownDocument returns an empty slice.
func TestCodeLensUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if lenses := server.codeLenses("file:///missing.kizu"); len(lenses) != 0 {
		t.Fatalf("codeLenses on missing doc = %#v, want empty", lenses)
	}
}
