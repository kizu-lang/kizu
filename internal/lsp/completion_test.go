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

// TestVSCodePackageExposesRunAndTestCommands checks the editor command manifest.
func TestVSCodePackageExposesRunAndTestCommands(t *testing.T) {
	path := filepath.Join("..", "..", "editors", "vscode", "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest vscodePackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	requireVSCodeActivationEvent(t, manifest, "onCommand:kizu.runFile")
	requireVSCodeActivationEvent(t, manifest, "onCommand:kizu.testFile")
	requireVSCodeCommand(t, manifest, "kizu.runFile", "Kizu: Run File")
	requireVSCodeCommand(t, manifest, "kizu.testFile", "Kizu: Test File")
	prop, ok := manifest.Contributes.Configuration.Properties["kizu.cli.path"]
	if !ok {
		t.Fatalf("kizu.cli.path setting missing")
	}
	if prop.Type != "string" || prop.Default != "kizu" {
		t.Fatalf("kizu.cli.path = %#v, want string default kizu", prop)
	}
}

// TestCompleteReturnsEnumMembersAfterNamespace checks incomplete namespace expressions work.
func TestCompleteReturnsEnumMembersAfterNamespace(t *testing.T) {
	source := `/// Visible colors.
enum Color {
    Red,
    /// Secondary color.
    Green,
}

fn main() {
    let color = Color::
}
`
	items := Complete(source, Position{Line: 8, Character: len("    let color = Color::")})
	item := requireCompletion(t, items, "Green")
	if item.Kind != completionItemKindEnumMember {
		t.Fatalf("kind = %d, want enum member", item.Kind)
	}
	if item.TextEdit == nil || item.TextEdit.NewText != "Green" {
		t.Fatalf("textEdit = %#v, want Green replacement", item.TextEdit)
	}
	if item.Documentation == nil || item.Documentation.Value != "Secondary color." {
		t.Fatalf("documentation = %#v", item.Documentation)
	}
}

// TestCompleteReturnsTypeDocumentation checks general completions include type docs.
func TestCompleteReturnsTypeDocumentation(t *testing.T) {
	source := `/// Trace data.
struct Trace {
    value: i64,
}

fn main() {
    Tr
}
`
	items := Complete(source, Position{Line: 6, Character: len("    Tr")})
	trace := requireCompletion(t, items, "Trace")
	if trace.Kind != completionItemKindStruct {
		t.Fatalf("kind = %d, want struct", trace.Kind)
	}
	if trace.Documentation == nil || trace.Documentation.Value != "Trace data." {
		t.Fatalf("documentation = %#v", trace.Documentation)
	}
}

// TestCompleteReturnsUnionVariantDocumentation checks union namespace docs.
func TestCompleteReturnsUnionVariantDocumentation(t *testing.T) {
	source := `union Event {
    /// Rename event.
    Rename([]u8),
}

fn main() {
    let event = Event::
}
`
	items := Complete(source, Position{Line: 6, Character: len("    let event = Event::")})
	rename := requireCompletion(t, items, "Rename")
	if rename.Kind != completionItemKindEnumMember {
		t.Fatalf("kind = %d, want enum member", rename.Kind)
	}
	if rename.TextEdit == nil || rename.TextEdit.NewText != "Rename($1)" {
		t.Fatalf("textEdit = %#v, want Rename payload snippet", rename.TextEdit)
	}
	if rename.Documentation == nil || rename.Documentation.Value != "Rename event." {
		t.Fatalf("documentation = %#v", rename.Documentation)
	}
}

// TestCompleteReturnsStructFieldsAndImplMethods checks member completions for simple locals.
func TestCompleteReturnsStructFieldsAndImplMethods(t *testing.T) {
	source := `struct Trace {
    /// Human-readable label.
    label: []u8,
    count: i64,
}

fn (self: Trace) deinit(allocator: Allocator) -> void {
    return;
}
/// Renames the trace.
fn (self: &var Trace) rename(name: []u8) -> void {
    return;
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
	if label.Documentation == nil || label.Documentation.Value != "Human-readable label." {
		t.Fatalf("label documentation = %#v", label.Documentation)
	}
	deinit := requireCompletion(t, items, "deinit")
	if deinit.Kind != completionItemKindMethod || deinit.TextEdit == nil ||
		deinit.TextEdit.NewText != "deinit(${1:allocator})" {
		t.Fatalf("deinit item = %#v, want method call snippet", deinit)
	}
	if deinit.Detail != "fn deinit(self: Trace, allocator: Allocator) -> void" {
		t.Fatalf("deinit detail = %q, want method signature", deinit.Detail)
	}
	rename := requireCompletion(t, items, "rename")
	if rename.TextEdit == nil || rename.TextEdit.NewText != "rename(${1:name})" {
		t.Fatalf("rename item = %#v, want parameter snippet", rename)
	}
	if rename.Detail != "fn rename(self: &var Trace, name: []u8) -> void" {
		t.Fatalf("rename detail = %q, want method signature", rename.Detail)
	}
	if rename.Documentation == nil || rename.Documentation.Value != "Renames the trace." {
		t.Fatalf("rename documentation = %#v", rename.Documentation)
	}
}

// TestCompleteReturnsFunctionSignatureDetails checks callable completions are descriptive.
func TestCompleteReturnsFunctionSignatureDetails(t *testing.T) {
	source := `/// Inspects a value.
pub fn inspect(value: i64, name: []u8) -> bool {
    return true;
}

fn main() {
    insp
}
`
	items := Complete(source, Position{Line: 6, Character: len("    insp")})
	inspect := requireCompletion(t, items, "inspect")
	if inspect.Kind != completionItemKindFunction {
		t.Fatalf("kind = %d, want function", inspect.Kind)
	}
	if inspect.Detail != "fn inspect(value: i64, name: []u8) -> bool" {
		t.Fatalf("detail = %q, want full function signature", inspect.Detail)
	}
	if inspect.InsertText != "inspect(${1:value}, ${2:name})" {
		t.Fatalf("insertText = %q, want name-only snippet", inspect.InsertText)
	}
	if inspect.Documentation == nil || inspect.Documentation.Value != "Inspects a value." {
		t.Fatalf("documentation = %#v", inspect.Documentation)
	}
}

// TestServerCompletionReturnsPackageModuleImportPaths checks package-aware import completions.
func TestServerCompletionReturnsPackageModuleImportPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	mainSource := "import app::\n\nfn main() {}\n"
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu":        mainSource,
		"src/token/token.kizu": "pub struct Token { pub kind: i64, }",
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

type vscodePackageManifest struct {
	ActivationEvents []string `json:"activationEvents"`
	Contributes      struct {
		Commands []struct {
			Command string `json:"command"`
			Title   string `json:"title"`
		} `json:"commands"`
		Configuration struct {
			Properties map[string]vscodePackageProperty `json:"properties"`
		} `json:"configuration"`
	} `json:"contributes"`
}

type vscodePackageProperty struct {
	Type    string `json:"type"`
	Default string `json:"default"`
}

// requireVSCodeActivationEvent checks that package.json activates a command.
func requireVSCodeActivationEvent(
	t *testing.T,
	manifest vscodePackageManifest,
	want string,
) {
	t.Helper()
	for _, event := range manifest.ActivationEvents {
		if event == want {
			return
		}
	}
	t.Fatalf("activation event %q missing from %#v", want, manifest.ActivationEvents)
}

// requireVSCodeCommand checks that package.json contributes a named command.
func requireVSCodeCommand(
	t *testing.T,
	manifest vscodePackageManifest,
	command string,
	title string,
) {
	t.Helper()
	for _, item := range manifest.Contributes.Commands {
		if item.Command == command && item.Title == title {
			return
		}
	}
	t.Fatalf("command %q title %q missing from %#v", command, title, manifest.Contributes.Commands)
}
