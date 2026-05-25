package lsp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/project"
)

// TestCompletionItemsIncludeBasics checks keywords, types, and declarations.
func TestCompletionItemsIncludeBasics(t *testing.T) {
	source := `struct Token {
    pub kind: i64,
}

fn helper() -> void {
    return;
}
`
	items := CompletionItems(source, emptyGraph(), false)
	assertCompletionLabels(t, items, "fn", "i64", "Token", "helper")
}

// TestServerCompletesPackageModules checks package graph module candidates.
func TestServerCompletesPackageModules(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace", "sub-app")
	mainSource := `import app::token;

fn main(value: token::Token) -> void {
    return;
}
`
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu": mainSource,
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
}
`,
	})
	input := strings.Join([]string{
		didOpenFrame(t, fileURI(filepath.Join(root, "src", "main.kizu")), mainSource),
		completionFrame(t, 2, fileURI(filepath.Join(root, "src", "main.kizu"))),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	items := completionItemsFromMessage(t, messages[1])
	assertCompletionLabels(t, items, "app", "app::token")
}

// completionFrame returns a textDocument/completion request frame.
func completionFrame(t *testing.T, id int, uri string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 7},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return frame(string(body))
}

// completionItemsFromMessage decodes completion labels from a response message.
func completionItemsFromMessage(t *testing.T, message map[string]any) []completionItem {
	t.Helper()
	if message["id"].(float64) != 2 {
		t.Fatalf("completion response id = %#v, want 2", message["id"])
	}
	result := message["result"].([]any)
	items := make([]completionItem, 0, len(result))
	for _, raw := range result {
		item := raw.(map[string]any)
		items = append(items, completionItem{Label: item["label"].(string)})
	}
	return items
}

// assertCompletionLabels checks that every expected label is present.
func assertCompletionLabels(t *testing.T, items []completionItem, labels ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Label] = true
	}
	for _, label := range labels {
		if !seen[label] {
			t.Fatalf("missing completion %q in %#v", label, items)
		}
	}
}

// emptyGraph returns a zero-value package graph for standalone completions.
func emptyGraph() project.Graph {
	return project.Graph{}
}
