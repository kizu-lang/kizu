package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompleteReturnsStaticSnippets checks the baseline editor templates.
func TestCompleteReturnsStaticSnippets(t *testing.T) {
	items := Complete("", Position{})
	item := requireCompletionKind(t, items, "fn", completionItemKindSnippet)
	if item.InsertTextFormat != insertTextFormatSnippet {
		t.Fatalf("insertTextFormat = %d, want snippet", item.InsertTextFormat)
	}
	if item.InsertText == "" {
		t.Fatalf("fn completion missing insert text")
	}
}

// TestCompleteReturnsTestSnippet keeps the test-block LSP template available.
func TestCompleteReturnsTestSnippet(t *testing.T) {
	items := Complete("", Position{})
	item := requireCompletionKind(t, items, "test", completionItemKindSnippet)
	if item.Detail != "test block" {
		t.Fatalf("detail = %q, want test block", item.Detail)
	}
	if item.InsertTextFormat != insertTextFormatSnippet {
		t.Fatalf("insertTextFormat = %d, want snippet", item.InsertTextFormat)
	}
	want := "test \"${1:name}\" {\n    $0\n}"
	if item.InsertText != want {
		t.Fatalf("insertText = %q, want %q", item.InsertText, want)
	}
}

// TestVSCodeSnippetsExposeTestBlock checks the packaged editor snippet file.
func TestVSCodeSnippetsExposeTestBlock(t *testing.T) {
	path := filepath.Join("..", "..", "editors", "vscode", "snippets", "kizu.code-snippets")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snippets map[string]vscodeSnippet
	if err := json.Unmarshal(data, &snippets); err != nil {
		t.Fatal(err)
	}
	snippet, ok := snippets["Test Block"]
	if !ok {
		t.Fatalf("Test Block snippet missing from %s", path)
	}
	if snippet.Prefix != "test" {
		t.Fatalf("prefix = %q, want test", snippet.Prefix)
	}
	want := "test \"${1:name}\" {\n    $0\n}"
	if strings.Join(snippet.Body, "\n") != want {
		t.Fatalf("body = %#v, want %q", snippet.Body, want)
	}
	if snippet.Description != "Test block" {
		t.Fatalf("description = %q, want Test block", snippet.Description)
	}
}

// TestCompleteReturnsEnumMembersAfterNamespace checks incomplete namespace expressions work.
func TestCompleteReturnsEnumMembersAfterNamespace(t *testing.T) {
	source := `enum Color {
    Red,
    Green,
}

fn main() {
    let color = Color::
}
`
	items := Complete(source, Position{Line: 6, Character: len("    let color = Color::")})
	item := requireCompletion(t, items, "Green")
	if item.Kind != completionItemKindEnumMember {
		t.Fatalf("kind = %d, want enum member", item.Kind)
	}
	if item.TextEdit == nil || item.TextEdit.NewText != "Green" {
		t.Fatalf("textEdit = %#v, want Green replacement", item.TextEdit)
	}
}

// TestCompleteReturnsStructFieldsAndImplMethods checks member completions for simple locals.
func TestCompleteReturnsStructFieldsAndImplMethods(t *testing.T) {
	source := `struct Trace {
    label: []u8,
    count: i64,
}

impl Trace {
    fn deinit(self: Trace) -> void {
        return;
    }
    fn rename(self: &var Trace, name: []u8) -> void {
        return;
    }
}

fn main() {
    let first = Trace { label: "first", count: 1 };
    first.
}
`
	items := Complete(source, Position{Line: 16, Character: len("    first.")})
	label := requireCompletion(t, items, "label")
	if label.Kind != completionItemKindField || label.Detail != "[]u8" {
		t.Fatalf("label item = %#v, want []u8 field", label)
	}
	deinit := requireCompletion(t, items, "deinit")
	if deinit.Kind != completionItemKindMethod || deinit.TextEdit == nil ||
		deinit.TextEdit.NewText != "deinit()" {
		t.Fatalf("deinit item = %#v, want method call snippet", deinit)
	}
	rename := requireCompletion(t, items, "rename")
	if rename.TextEdit == nil || rename.TextEdit.NewText != "rename(${1:name})" {
		t.Fatalf("rename item = %#v, want parameter snippet", rename)
	}
}

// TestServerCompletionReturnsPackageModuleImportPaths checks package-aware import completions.
func TestServerCompletionReturnsPackageModuleImportPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	mainSource := "import app::\n\nfn main() {}\n"
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu":  mainSource,
		"src/token.kizu": "pub struct Token { pub kind: i64, }",
	})
	uri := fileURI(filepath.Join(root, "src", "main.kizu"))
	server := NewServer(nil, nil)
	server.documents[uri] = mainSource

	items := server.completions(uri, Position{Line: 0, Character: len("import app::")})
	item := requireCompletion(t, items, "app::token")
	if item.TextEdit == nil || item.TextEdit.NewText != "app::token" {
		t.Fatalf("module item = %#v, want full import text edit", item)
	}
}

// requireCompletion returns one item by label.
func requireCompletion(t *testing.T, items []completionItem, label string) completionItem {
	t.Helper()
	for _, item := range items {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("completion %q not found in %#v", label, items)
	return completionItem{}
}

// requireCompletionKind returns one item by label and kind.
func requireCompletionKind(
	t *testing.T,
	items []completionItem,
	label string,
	kind int,
) completionItem {
	t.Helper()
	for _, item := range items {
		if item.Label == label && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("completion %q kind %d not found in %#v", label, kind, items)
	return completionItem{}
}

type vscodeSnippet struct {
	Prefix      string   `json:"prefix"`
	Body        []string `json:"body"`
	Description string   `json:"description"`
}
