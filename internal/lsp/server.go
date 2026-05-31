package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Run serves LSP messages over reader and writer until EOF or exit.
func Run(reader io.Reader, writer io.Writer) error {
	return NewServer(reader, writer).Run()
}

// Server owns the in-memory LSP document state.
type Server struct {
	reader    *bufio.Reader
	writer    io.Writer
	documents map[string]string
	analysis  map[string]checkedDocument
}

// NewServer creates a Kizu language server over one JSON-RPC transport.
func NewServer(reader io.Reader, writer io.Writer) *Server {
	return &Server{
		reader:    bufio.NewReader(reader),
		writer:    writer,
		documents: map[string]string{},
		analysis:  map[string]checkedDocument{},
	}
}

// Run reads and handles JSON-RPC messages.
func (s *Server) Run() error {
	for {
		body, err := readMessage(s.reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		stop, err := s.handleMessage(body)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

// handleMessage dispatches one decoded JSON-RPC request or notification.
func (s *Server) handleMessage(body []byte) (bool, error) {
	var msg incomingMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return false, err
	}
	if msg.Method == "" {
		return false, nil
	}
	if msg.ID != nil {
		return s.handleRequest(msg)
	}
	return s.handleNotification(msg)
}

// handleRequest responds to LSP requests that expect a JSON-RPC response.
func (s *Server) handleRequest(msg incomingMessage) (bool, error) {
	if handler, ok := s.requestHandlers()[msg.Method]; ok {
		return false, handler(msg)
	}
	return false, s.respondError(msg.ID, -32601, fmt.Sprintf("method not found: %s", msg.Method))
}

// requestHandlers maps each supported request method to its handler. Splitting
// the dispatch into a table keeps handleRequest flat instead of a large switch.
func (s *Server) requestHandlers() map[string]func(incomingMessage) error {
	return map[string]func(incomingMessage) error{
		"initialize":                       s.handleInitializeRequest,
		"shutdown":                         s.handleShutdownRequest,
		"textDocument/formatting":          s.handleFormattingRequest,
		"textDocument/completion":          s.handleCompletionRequest,
		"textDocument/inlayHint":           s.handleInlayHintRequest,
		"textDocument/definition":          s.handleDefinitionRequest,
		"textDocument/hover":               s.handleHoverRequest,
		"textDocument/documentSymbol":      s.handleDocumentSymbolRequest,
		"textDocument/references":          s.handleReferencesRequest,
		"textDocument/signatureHelp":       s.handleSignatureHelpRequest,
		"textDocument/semanticTokens/full": s.handleSemanticTokensRequest,
		"textDocument/documentHighlight":   s.handleDocumentHighlightRequest,
		"textDocument/prepareRename":       s.handlePrepareRenameRequest,
		"textDocument/rename":              s.handleRenameRequest,
		"workspace/symbol":                 s.handleWorkspaceSymbolRequest,
	}
}

// handleShutdownRequest acknowledges a shutdown request with a null result.
func (s *Server) handleShutdownRequest(msg incomingMessage) error {
	return s.respond(msg.ID, nil)
}

// handleInitializeRequest advertises the server's capabilities to the client.
func (s *Server) handleInitializeRequest(msg incomingMessage) error {
	return s.respond(msg.ID, initializeResult{
		Capabilities: serverCapabilities{
			TextDocumentSync:           textDocumentSyncKindFull,
			DocumentFormattingProvider: true,
			CompletionProvider: &completionOptions{
				TriggerCharacters: []string{":", ".", "@"},
			},
			InlayHintProvider:      true,
			DefinitionProvider:     true,
			HoverProvider:          true,
			DocumentSymbolProvider: true,
			ReferencesProvider:     true,
			SignatureHelpProvider: &signatureOptions{
				TriggerCharacters: []string{"(", ","},
			},
			SemanticTokensProvider: &semanticTokensOptions{
				Legend: semanticTokenLegend(),
				Full:   true,
			},
			WorkspaceSymbolProvider:   true,
			DocumentHighlightProvider: true,
			RenameProvider:            &renameOptions{PrepareProvider: true},
		},
		ServerInfo: serverInfo{Name: "kizu-lsp"},
	})
}

// handleFormattingRequest returns whole-document edits for a tracked document.
func (s *Server) handleFormattingRequest(msg incomingMessage) error {
	var params documentFormattingParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	source, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return s.respond(msg.ID, []textEdit{})
	}
	return s.respond(msg.ID, FormatEdits(source))
}

// handleCompletionRequest returns completions for a tracked document.
func (s *Server) handleCompletionRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.completions(params.TextDocument.URI, params.Position))
}

// handleInlayHintRequest returns inferred local type hints for a tracked document.
func (s *Server) handleInlayHintRequest(msg incomingMessage) error {
	var params inlayHintParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.inlayHints(params.TextDocument.URI, params.Range))
}

// handleDefinitionRequest returns a location for the symbol under the cursor.
func (s *Server) handleDefinitionRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.definition(params.TextDocument.URI, params.Position))
}

// handleHoverRequest returns concise information for the symbol under the cursor.
func (s *Server) handleHoverRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.hover(params.TextDocument.URI, params.Position))
}

// handleDocumentSymbolRequest returns outline symbols for a tracked document.
func (s *Server) handleDocumentSymbolRequest(msg incomingMessage) error {
	var params documentSymbolParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	source, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return s.respond(msg.ID, []documentSymbol{})
	}
	return s.respond(msg.ID, DocumentSymbols(source))
}

// handleReferencesRequest returns locations that resolve to the cursor target.
func (s *Server) handleReferencesRequest(msg incomingMessage) error {
	var params referenceParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.references(
		params.TextDocument.URI,
		params.Position,
		params.Context.IncludeDeclaration,
	))
}

// handleSignatureHelpRequest returns call signature help for the cursor.
func (s *Server) handleSignatureHelpRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.signatureHelp(params.TextDocument.URI, params.Position))
}

// handleSemanticTokensRequest returns whole-document semantic token data.
func (s *Server) handleSemanticTokensRequest(msg incomingMessage) error {
	var params semanticTokensParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.semanticTokens(params.TextDocument.URI))
}

// handleDocumentHighlightRequest returns highlight ranges for the cursor symbol.
func (s *Server) handleDocumentHighlightRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.documentHighlights(params.TextDocument.URI, params.Position))
}

// handlePrepareRenameRequest returns the editable range for the cursor symbol.
func (s *Server) handlePrepareRenameRequest(msg incomingMessage) error {
	var params textDocumentPositionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.prepareRename(params.TextDocument.URI, params.Position))
}

// handleRenameRequest returns a workspace edit renaming the cursor symbol.
func (s *Server) handleRenameRequest(msg incomingMessage) error {
	var params renameParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.rename(params.TextDocument.URI, params.Position, params.NewName))
}

// handleWorkspaceSymbolRequest returns package symbols matching a query.
func (s *Server) handleWorkspaceSymbolRequest(msg incomingMessage) error {
	var params workspaceSymbolParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	return s.respond(msg.ID, s.workspaceSymbols(params.Query))
}

// handleNotification applies LSP notifications without sending request responses.
func (s *Server) handleNotification(msg incomingMessage) (bool, error) {
	switch msg.Method {
	case "exit":
		return true, nil
	case "initialized":
		return false, nil
	case "textDocument/didOpen":
		var params didOpenTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false, err
		}
		s.documents[params.TextDocument.URI] = params.TextDocument.Text
		delete(s.analysis, params.TextDocument.URI)
		return false, s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didChange":
		var params didChangeTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false, err
		}
		if len(params.ContentChanges) == 0 {
			return false, nil
		}
		s.documents[params.TextDocument.URI] = params.ContentChanges[len(params.ContentChanges)-1].Text
		delete(s.analysis, params.TextDocument.URI)
		return false, s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didClose":
		var params didCloseTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false, err
		}
		delete(s.documents, params.TextDocument.URI)
		delete(s.analysis, params.TextDocument.URI)
		return false, s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
			URI:         params.TextDocument.URI,
			Diagnostics: []Diagnostic{},
		})
	default:
		return false, nil
	}
}

// publishDiagnostics analyzes the current document text and notifies the client.
func (s *Server) publishDiagnostics(uri string) error {
	if _, ok := s.documents[uri]; !ok {
		return nil
	}
	return s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
		URI:         uri,
		Diagnostics: s.checkedDocument(uri).Diagnostics,
	})
}

// respond writes a successful JSON-RPC response.
func (s *Server) respond(id *json.RawMessage, result any) error {
	resultBody, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return writeMessage(s.writer, outgoingResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultBody,
	})
}

// respondError writes a JSON-RPC error response.
func (s *Server) respondError(id *json.RawMessage, code int, message string) error {
	return writeMessage(s.writer, outgoingResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: message,
		},
	})
}

// notify writes an LSP notification to the client.
func (s *Server) notify(method string, params any) error {
	return writeMessage(s.writer, outgoingNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}
