package lsp

import "encoding/json"

const (
	textDocumentSyncKindFull = 1

	diagnosticSeverityError = 1

	completionItemKindMethod        = 2
	completionItemKindFunction      = 3
	completionItemKindField         = 5
	completionItemKindVariable      = 6
	completionItemKindModule        = 9
	completionItemKindValue         = 12
	completionItemKindEnum          = 13
	completionItemKindKeyword       = 14
	completionItemKindSnippet       = 15
	completionItemKindEnumMember    = 20
	completionItemKindStruct        = 22
	completionItemKindTypeParameter = 25

	insertTextFormatSnippet = 2

	inlayHintKindType = 1
)

type incomingMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type outgoingResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *responseError   `json:"error,omitempty"`
}

type outgoingNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	TextDocumentSync           int                `json:"textDocumentSync"`
	DocumentFormattingProvider bool               `json:"documentFormattingProvider,omitempty"`
	CompletionProvider         *completionOptions `json:"completionProvider,omitempty"`
	InlayHintProvider          bool               `json:"inlayHintProvider,omitempty"`
}

type completionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type serverInfo struct {
	Name string `json:"name"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId,omitempty"`
	Version    int    `json:"version,omitempty"`
	Text       string `json:"text"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version,omitempty"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type didOpenTextDocumentParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeTextDocumentParams struct {
	TextDocument   versionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []textDocumentContentChangeEvent `json:"contentChanges"`
}

type didCloseTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type documentFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type inlayHintParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type textDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type textEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type completionTextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type completionItem struct {
	Label            string              `json:"label"`
	Kind             int                 `json:"kind,omitempty"`
	Detail           string              `json:"detail,omitempty"`
	InsertText       string              `json:"insertText,omitempty"`
	InsertTextFormat int                 `json:"insertTextFormat,omitempty"`
	TextEdit         *completionTextEdit `json:"textEdit,omitempty"`
}

type inlayHint struct {
	Position Position `json:"position"`
	Label    string   `json:"label"`
	Kind     int      `json:"kind,omitempty"`
}

// Diagnostic is the LSP diagnostic shape emitted by the server.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Range identifies a half-open text region in zero-based LSP positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position identifies a zero-based line and UTF-16 character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
