package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestServerInitializesAndPublishesDiagnostics checks the first editor loop.
func TestServerInitializesAndPublishesDiagnostics(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"languageId":"kizu","version":1,"text":"let x = 1;\n"}}}`),
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
	if messages[0]["id"].(float64) != 1 {
		t.Fatalf("initialize response id = %#v, want 1", messages[0]["id"])
	}
	result := messages[0]["result"].(map[string]any)
	capabilities := result["capabilities"].(map[string]any)
	requireInitializeCapabilities(t, capabilities)
	if messages[1]["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("got method %#v, want publish diagnostics", messages[1]["method"])
	}
	params := messages[1]["params"].(map[string]any)
	diagnostics := params["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
	}
}

// requireInitializeCapabilities checks advertised LSP feature providers.
func requireInitializeCapabilities(t *testing.T, capabilities map[string]any) {
	t.Helper()
	requireCapability(t, capabilities, "documentFormattingProvider")
	requireCapability(t, capabilities, "inlayHintProvider")
	requireCapability(t, capabilities, "definitionProvider")
	requireCapability(t, capabilities, "typeDefinitionProvider")
	requireCapability(t, capabilities, "implementationProvider")
	requireCapability(t, capabilities, "callHierarchyProvider")
	requireCapability(t, capabilities, "hoverProvider")
	requireCapability(t, capabilities, "documentSymbolProvider")
	requireCapability(t, capabilities, "referencesProvider")
	requireCapability(t, capabilities, "workspaceSymbolProvider")
	requireCapability(t, capabilities, "documentHighlightProvider")
	requireCapability(t, capabilities, "foldingRangeProvider")
	requireCapability(t, capabilities, "selectionRangeProvider")
	requireCapabilityObject(t, capabilities, "renameProvider")
	requireCapabilityObject(t, capabilities, "completionProvider")
	requireCompletionTrigger(t, capabilities, "@")
	requireCapabilityObject(t, capabilities, "signatureHelpProvider")
	requireCapabilityObject(t, capabilities, "semanticTokensProvider")
}

// requireCapability checks a boolean server capability.
func requireCapability(t *testing.T, capabilities map[string]any, key string) {
	t.Helper()
	if capabilities[key] != true {
		t.Fatalf("%s = %#v, want true", key, capabilities[key])
	}
}

// requireCapabilityObject checks an object server capability.
func requireCapabilityObject(t *testing.T, capabilities map[string]any, key string) {
	t.Helper()
	if _, ok := capabilities[key].(map[string]any); !ok {
		t.Fatalf("%s missing from capabilities: %#v", key, capabilities)
	}
}

// requireCompletionTrigger checks one advertised completion trigger character.
func requireCompletionTrigger(t *testing.T, capabilities map[string]any, want string) {
	t.Helper()
	provider := capabilities["completionProvider"].(map[string]any)
	values, ok := provider["triggerCharacters"].([]any)
	if !ok {
		t.Fatalf("triggerCharacters missing: %#v", provider)
	}
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("triggerCharacters = %#v, want %q", values, want)
}

// TestServerInlayHintReturnsLocalTypes checks the LSP request response shape.
func TestServerInlayHintReturnsLocalTypes(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"languageId":"kizu","version":1,"text":"fn main() {\n    let n = 1;\n}"}}}`),
		frame(`{"jsonrpc":"2.0","id":9,"method":"textDocument/inlayHint",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},` +
			`"range":{"start":{"line":0,"character":0},"end":{"line":2,"character":1}}}}`),
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
	if messages[1]["id"].(float64) != 9 {
		t.Fatalf("inlay hint response id = %#v, want 9", messages[1]["id"])
	}
	hints := messages[1]["result"].([]any)
	if len(hints) != 1 {
		t.Fatalf("got hints %#v, want one hint", hints)
	}
	got := hints[0].(map[string]any)
	if got["label"] != ": i64" {
		t.Fatalf("hint label = %#v, want : i64", got["label"])
	}
	position := got["position"].(map[string]any)
	if position["line"].(float64) != 1 ||
		position["character"].(float64) != float64(len("    let n")) {
		t.Fatalf("position = %#v, want after local name", position)
	}
}

// TestServerNavigationRequests checks definition, hover, and outline response shapes.
func TestServerNavigationRequests(t *testing.T) {
	source := "fn helper() -> void { return; }\n\nfn main() {\n    helper();\n}\n"
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"languageId":"kizu","version":1,"text":` + strconv.Quote(source) + `}}}`),
		frame(`{"jsonrpc":"2.0","id":2,"method":"textDocument/definition",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},` +
			`"position":{"line":3,"character":5}}}`),
		frame(`{"jsonrpc":"2.0","id":3,"method":"textDocument/hover",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},` +
			`"position":{"line":3,"character":5}}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/documentSymbol",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"}}}`),
		frame(`{"jsonrpc":"2.0","id":5,"method":"textDocument/references",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},` +
			`"position":{"line":3,"character":5},"context":{"includeDeclaration":true}}}`),
		frame(`{"jsonrpc":"2.0","id":6,"method":"textDocument/signatureHelp",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},` +
			`"position":{"line":3,"character":10}}}`),
		frame(`{"jsonrpc":"2.0","id":7,"method":"textDocument/semanticTokens/full",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"}}}`),
		frame(`{"jsonrpc":"2.0","id":8,"method":"workspace/symbol",` +
			`"params":{"query":"helper"}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 8 {
		t.Fatalf("got %d messages, want diagnostics and 7 responses", len(messages))
	}
	locations := messages[1]["result"].([]any)
	if len(locations) != 1 {
		t.Fatalf("definition result = %#v, want one location", locations)
	}
	hoverResult := messages[2]["result"].(map[string]any)
	contents := hoverResult["contents"].(map[string]any)
	if !strings.Contains(contents["value"].(string), "fn helper") {
		t.Fatalf("hover contents = %#v, want helper signature", contents)
	}
	symbols := messages[3]["result"].([]any)
	if len(symbols) != 2 {
		t.Fatalf("symbols = %#v, want helper and main", symbols)
	}
	references := messages[4]["result"].([]any)
	if len(references) != 2 {
		t.Fatalf("references = %#v, want declaration and call", references)
	}
	signature := messages[5]["result"].(map[string]any)
	if len(signature["signatures"].([]any)) != 1 {
		t.Fatalf("signature = %#v, want one signature", signature)
	}
	semantic := messages[6]["result"].(map[string]any)
	if len(semantic["data"].([]any)) == 0 {
		t.Fatalf("semantic tokens = %#v, want data", semantic)
	}
	workspaceSymbols := messages[7]["result"].([]any)
	if len(workspaceSymbols) == 0 {
		t.Fatalf("workspace symbols = %#v, want helper", workspaceSymbols)
	}
}

// TestServerUsesNestedPackageGraphForDiagnostics checks subdirectory package roots work.
func TestServerUsesNestedPackageGraphForDiagnostics(t *testing.T) {
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
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	diagnostics := publishedDiagnostics(t, messages[0])
	if len(diagnostics) != 0 {
		t.Fatalf("got diagnostics %#v, want none", diagnostics)
	}
}

// TestServerPackageDiagnosticsUseStdDecls checks package diagnostics share CLI std wrappers.
func TestServerPackageDiagnosticsUseStdDecls(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace", "sub-app")
	mainSource := `test "package test command" {
    std::testing::expect(true);
}
`
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu": mainSource,
	})
	input := strings.Join([]string{
		didOpenFrame(t, fileURI(filepath.Join(root, "src", "main.kizu")), mainSource),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	diagnostics := publishedDiagnostics(t, messages[0])
	if len(diagnostics) != 0 {
		t.Fatalf("got diagnostics %#v, want none", diagnostics)
	}
}

// TestServerPackageDiagnosticsUseOpenBuffer checks unsaved edits override disk.
func TestServerPackageDiagnosticsUseOpenBuffer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace", "sub-app")
	openMain := `import app::token;

fn main(value: token::Token) -> void {
    return;
}
`
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu": "let x = 1;\n",
		"src/token.kizu": `pub struct Token {
    pub kind: i64,
}
`,
	})
	input := strings.Join([]string{
		didOpenFrame(t, fileURI(filepath.Join(root, "src", "main.kizu")), openMain),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	diagnostics := publishedDiagnostics(t, messages[0])
	if len(diagnostics) != 0 {
		t.Fatalf("got diagnostics %#v, want none", diagnostics)
	}
}

// TestServerClearsDiagnosticsOnClose checks editors can remove stale errors.
func TestServerClearsDiagnosticsOnClose(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"text":"let x = 1;\n"}}}`),
		frame(`{"jsonrpc":"2.0","method":"textDocument/didClose",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"}}}`),
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
	params := messages[1]["params"].(map[string]any)
	diagnostics := params["diagnostics"].([]any)
	if len(diagnostics) != 0 {
		t.Fatalf("got %d diagnostics, want cleared diagnostics", len(diagnostics))
	}
}

// TestServerFormattingReturnsEdit checks formatting request response shape.
func TestServerFormattingReturnsEdit(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"text":"fn main(){return;}"}}}`),
		frame(`{"jsonrpc":"2.0","id":7,"method":"textDocument/formatting",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},"options":{}}}`),
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
	if messages[1]["id"].(float64) != 7 {
		t.Fatalf("formatting response id = %#v, want 7", messages[1]["id"])
	}
	edits := messages[1]["result"].([]any)
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(edits))
	}
	edit := edits[0].(map[string]any)
	want := "fn main() {\n    return;\n}\n"
	if edit["newText"] != want {
		t.Fatalf("newText = %#v, want %#v", edit["newText"], want)
	}
}

// TestServerFormattingSkipsParseInvalidSource checks broken buffers are untouched.
func TestServerFormattingSkipsParseInvalidSource(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","method":"textDocument/didOpen",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu",` +
			`"text":"let x = 1;\n"}}}`),
		frame(`{"jsonrpc":"2.0","id":8,"method":"textDocument/formatting",` +
			`"params":{"textDocument":{"uri":"file:///main.kizu"},"options":{}}}`),
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
	edits := messages[1]["result"].([]any)
	if len(edits) != 0 {
		t.Fatalf("got %d edits, want none", len(edits))
	}
}

// TestServerShutdownReturnsNullResult checks the JSON-RPC shutdown shape.
func TestServerShutdownReturnsNullResult(t *testing.T) {
	input := strings.Join([]string{
		frame(`{"jsonrpc":"2.0","id":"shutdown-1","method":"shutdown"}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if _, ok := messages[0]["result"]; !ok {
		t.Fatalf("shutdown response missing result: %#v", messages[0])
	}
	if messages[0]["result"] != nil {
		t.Fatalf("shutdown result = %#v, want nil", messages[0]["result"])
	}
}

// TestServerStructuralRequests exercises the folding, selection, type
// definition, implementation, and rename methods over the full JSON-RPC loop.
func TestServerStructuralRequests(t *testing.T) {
	source := "import std::io;\n" +
		"import std::mem;\n" +
		"\n" +
		"struct File {\n" +
		"    name: []u8,\n" +
		"}\n" +
		"\n" +
		"contract Writer {\n" +
		"    fn write(self: &Self) -> !i64;\n" +
		"}\n" +
		"\n" +
		"impl Writer for File {\n" +
		"    fn write(self: &Self) -> !i64 {\n" +
		"        return 2;\n" +
		"    }\n" +
		"}\n" +
		"\n" +
		"fn main() -> !void {\n" +
		"    let file = File { name: \"out\" };\n" +
		"    print(file.name);\n" +
		"    return;\n" +
		"}\n"
	uri := "file:///main.kizu"
	input := strings.Join([]string{
		didOpenFrame(t, uri, source),
		frame(`{"jsonrpc":"2.0","id":2,"method":"textDocument/foldingRange",` +
			`"params":{"textDocument":{"uri":"` + uri + `"}}}`),
		frame(`{"jsonrpc":"2.0","id":3,"method":"textDocument/selectionRange",` +
			`"params":{"textDocument":{"uri":"` + uri + `"},` +
			`"positions":[{"line":18,"character":8}]}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"textDocument/typeDefinition",` +
			`"params":{"textDocument":{"uri":"` + uri + `"},` +
			`"position":{"line":18,"character":8}}}`),
		frame(`{"jsonrpc":"2.0","id":5,"method":"textDocument/implementation",` +
			`"params":{"textDocument":{"uri":"` + uri + `"},` +
			`"position":{"line":7,"character":9}}}`),
		frame(`{"jsonrpc":"2.0","id":6,"method":"textDocument/rename",` +
			`"params":{"textDocument":{"uri":"` + uri + `"},` +
			`"position":{"line":18,"character":8},"newName":"handle"}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 6 {
		t.Fatalf("got %d messages, want diagnostics and 5 responses", len(messages))
	}
	assertStructuralResponses(t, messages, uri)
}

// assertStructuralResponses checks the folding, selection, type definition,
// implementation, and rename responses produced by TestServerStructuralRequests.
func assertStructuralResponses(t *testing.T, messages []map[string]any, uri string) {
	t.Helper()
	folds := messages[1]["result"].([]any)
	if len(folds) < 2 {
		t.Fatalf("foldingRange result = %#v, want at least 2 regions", folds)
	}

	selections := messages[2]["result"].([]any)
	if len(selections) != 1 {
		t.Fatalf("selectionRange result = %#v, want one hierarchy", selections)
	}
	if _, ok := selections[0].(map[string]any)["parent"]; !ok {
		t.Fatalf("selectionRange[0] = %#v, want a parent node", selections[0])
	}

	typeDefs := messages[3]["result"].([]any)
	if len(typeDefs) != 1 {
		t.Fatalf("typeDefinition result = %#v, want one location", typeDefs)
	}
	if line := typeDefRangeLine(t, typeDefs[0]); line != 3 {
		t.Fatalf("typeDefinition line = %d, want struct File on line 3", line)
	}

	impls := messages[4]["result"].([]any)
	if len(impls) != 1 {
		t.Fatalf("implementation result = %#v, want one location", impls)
	}
	if line := typeDefRangeLine(t, impls[0]); line != 11 {
		t.Fatalf("implementation line = %d, want impl on line 11", line)
	}

	renameResult := messages[5]["result"].(map[string]any)
	changes := renameResult["changes"].(map[string]any)
	edits := changes[uri].([]any)
	if len(edits) != 2 {
		t.Fatalf("rename edits = %#v, want declaration and use", edits)
	}
}

// typeDefRangeLine extracts the start line of a location's range.
func typeDefRangeLine(t *testing.T, loc any) int {
	t.Helper()
	rng := loc.(map[string]any)["range"].(map[string]any)
	start := rng["start"].(map[string]any)
	return int(start["line"].(float64))
}

// TestServerCallHierarchyRequests exercises prepare, incoming, and outgoing
// call hierarchy methods over the full JSON-RPC loop.
func TestServerCallHierarchyRequests(t *testing.T) {
	source := callHierarchyFixture()
	uri := "file:///main.kizu"
	helperItem := `{"name":"helper","kind":12,"uri":"` + uri + `",` +
		`"range":{"start":{"line":4,"character":0},"end":{"line":6,"character":1}},` +
		`"selectionRange":{"start":{"line":4,"character":3},"end":{"line":4,"character":9}}}`
	input := strings.Join([]string{
		didOpenFrame(t, uri, source),
		frame(`{"jsonrpc":"2.0","id":2,"method":"textDocument/prepareCallHierarchy",` +
			`"params":{"textDocument":{"uri":"` + uri + `"},` +
			`"position":{"line":4,"character":3}}}`),
		frame(`{"jsonrpc":"2.0","id":3,"method":"callHierarchy/incomingCalls",` +
			`"params":{"item":` + helperItem + `}}`),
		frame(`{"jsonrpc":"2.0","id":4,"method":"callHierarchy/outgoingCalls",` +
			`"params":{"item":` + helperItem + `}}`),
		frame(`{"jsonrpc":"2.0","method":"exit"}`),
	}, "")
	var output bytes.Buffer

	if err := Run(strings.NewReader(input), &output); err != nil {
		t.Fatalf("run server: %v", err)
	}
	messages := readFrames(t, output.String())
	if len(messages) != 4 {
		t.Fatalf("got %d messages, want diagnostics and 3 responses", len(messages))
	}

	prepared := messages[1]["result"].([]any)
	if len(prepared) != 1 {
		t.Fatalf("prepareCallHierarchy result = %#v, want one item", prepared)
	}
	if name := prepared[0].(map[string]any)["name"]; name != "helper" {
		t.Fatalf("prepared item name = %v, want helper", name)
	}

	incoming := messages[2]["result"].([]any)
	if len(incoming) != 1 {
		t.Fatalf("incomingCalls result = %#v, want one caller", incoming)
	}
	from := incoming[0].(map[string]any)["from"].(map[string]any)
	if from["name"] != "main" {
		t.Fatalf("incoming caller = %v, want main", from["name"])
	}

	outgoing := messages[3]["result"].([]any)
	if len(outgoing) != 1 {
		t.Fatalf("outgoingCalls result = %#v, want one callee", outgoing)
	}
	to := outgoing[0].(map[string]any)["to"].(map[string]any)
	if to["name"] != "leaf" {
		t.Fatalf("outgoing callee = %v, want leaf", to["name"])
	}
}

// frame adds the LSP Content-Length envelope around a JSON body.
func frame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

// didOpenFrame returns one didOpen notification for a file buffer.
func didOpenFrame(t *testing.T, uri string, text string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "kizu",
				"version":    1,
				"text":       text,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return frame(string(body))
}

// fileURI converts a test file path into a local LSP URI.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// writeLSPPackage writes a minimal Kizu package for server diagnostics tests.
func writeLSPPackage(t *testing.T, root string, sources map[string]string) {
	t.Helper()
	manifest := `[package]
name = "app"
version = "0.2.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`
	writeLSPFile(t, filepath.Join(root, "kizu.toml"), manifest)
	for rel, source := range sources {
		writeLSPFile(t, filepath.Join(root, rel), source)
	}
}

// writeLSPFile writes one test file, creating parent directories first.
func writeLSPFile(t *testing.T, path string, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// publishedDiagnostics returns the diagnostics array from one publish notification.
func publishedDiagnostics(t *testing.T, message map[string]any) []any {
	t.Helper()
	if message["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("got method %#v, want publish diagnostics", message["method"])
	}
	params := message["params"].(map[string]any)
	return params["diagnostics"].([]any)
}

// readFrames decodes all framed JSON-RPC messages from server output.
func readFrames(t *testing.T, output string) []map[string]any {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(output))
	var messages []map[string]any
	for {
		body, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return messages
			}
			t.Fatalf("read frame: %v", err)
		}
		var message map[string]any
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		messages = append(messages, message)
	}
}
