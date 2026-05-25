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
}

// NewServer creates a Kizu language server over one JSON-RPC transport.
func NewServer(reader io.Reader, writer io.Writer) *Server {
	return &Server{
		reader:    bufio.NewReader(reader),
		writer:    writer,
		documents: map[string]string{},
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
	switch msg.Method {
	case "initialize":
		return false, s.respond(msg.ID, initializeResult{
			Capabilities: serverCapabilities{
				TextDocumentSync:           textDocumentSyncKindFull,
				DocumentFormattingProvider: true,
			},
			ServerInfo: serverInfo{Name: "kizu-lsp"},
		})
	case "shutdown":
		return false, s.respond(msg.ID, nil)
	case "textDocument/formatting":
		return false, s.handleFormattingRequest(msg)
	default:
		return false, s.respondError(msg.ID, -32601, fmt.Sprintf("method not found: %s", msg.Method))
	}
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
		return false, s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didClose":
		var params didCloseTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false, err
		}
		delete(s.documents, params.TextDocument.URI)
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
		Diagnostics: s.analyzeDocument(uri),
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
