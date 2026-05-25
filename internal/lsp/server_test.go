package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
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
	if capabilities["documentFormattingProvider"] != true {
		t.Fatalf("formatting capability = %#v, want true", capabilities["documentFormattingProvider"])
	}
	if messages[1]["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("got method %#v, want publish diagnostics", messages[1]["method"])
	}
	params := messages[1]["params"].(map[string]any)
	diagnostics := params["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diagnostics))
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

// frame adds the LSP Content-Length envelope around a JSON body.
func frame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
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
