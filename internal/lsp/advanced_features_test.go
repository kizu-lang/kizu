package lsp

import (
	"strings"
	"testing"
)

// TestReferencesReturnFunctionAndLocalUses checks find-references behavior.
func TestReferencesReturnFunctionAndLocalUses(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	functionRefs := server.references(uri, positionIn(source, "inspect(1)", "inspect"), true)
	if len(functionRefs) != 2 {
		t.Fatalf("function refs = %#v, want declaration and call", functionRefs)
	}
	localRefs := server.references(uri, positionIn(source, "trace.label", "trace"), false)
	if len(localRefs) != 2 {
		t.Fatalf("local refs = %#v, want field and method receiver uses", localRefs)
	}
}

// TestSignatureHelpReturnsFunctionAndMethodParameters checks call help.
func TestSignatureHelpReturnsFunctionAndMethodParameters(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	functionHelp := server.signatureHelp(uri, positionIn(source, "inspect(1)", "1"))
	requireSignature(t, functionHelp, "fn inspect", "value: i64", 0)
	methodHelp := server.signatureHelp(uri, positionIn(source, `rename("next")`, `"next"`))
	requireSignature(t, methodHelp, "fn rename", "name: []u8", 0)
}

// TestSemanticTokensReturnClassifiedData checks semantic token encoding exists.
func TestSemanticTokensReturnClassifiedData(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	tokens := server.semanticTokens(uri)
	if len(tokens.Data) == 0 || len(tokens.Data)%5 != 0 {
		t.Fatalf("semantic tokens = %#v, want encoded 5-tuples", tokens.Data)
	}
	if !semanticDataContains(tokens.Data, semanticFunction) {
		t.Fatalf("semantic tokens = %#v, want function token", tokens.Data)
	}
	if !semanticDataContains(tokens.Data, semanticEnumMember) {
		t.Fatalf("semantic tokens = %#v, want enum member token", tokens.Data)
	}
}

// TestWorkspaceSymbolsSearchOpenPackage checks workspace symbol lookup.
func TestWorkspaceSymbolsSearchOpenPackage(t *testing.T) {
	root := t.TempDir()
	mainSource := `import app::token;

fn main(value: token::Token) -> void {
    return;
}
`
	tokenSource := `pub struct Token {
    pub kind: i64,
}
`
	writeLSPPackage(t, root, map[string]string{
		"src/main.kizu":        mainSource,
		"src/token/token.kizu": tokenSource,
	})
	mainURI := fileURIFromPath(root + "/src/main.kizu")
	server := NewServer(nil, nil)
	server.documents[mainURI] = mainSource

	symbols := server.workspaceSymbols("Tok")
	if !hasSymbolInformation(symbols, "Token", symbolKindStruct) {
		t.Fatalf("workspace symbols missing Token")
	}
}

// requireSignature checks one signature help result.
func requireSignature(
	t *testing.T,
	got *signatureHelp,
	label string,
	param string,
	active int,
) {
	t.Helper()
	if got == nil || len(got.Signatures) != 1 {
		t.Fatalf("signature help = %#v, want one signature", got)
	}
	sig := got.Signatures[0]
	if !strings.Contains(sig.Label, label) {
		t.Fatalf("signature label = %q, want %q", sig.Label, label)
	}
	if len(sig.Parameters) == 0 || sig.Parameters[0].Label != param {
		t.Fatalf("parameters = %#v, want %q", sig.Parameters, param)
	}
	if got.ActiveParameter != active {
		t.Fatalf("active parameter = %d, want %d", got.ActiveParameter, active)
	}
}

// semanticDataContains checks whether encoded semantic data includes a token kind.
func semanticDataContains(data []int, kind int) bool {
	for i := 3; i < len(data); i += 5 {
		if data[i] == kind {
			return true
		}
	}
	return false
}

// hasSymbolInformation checks whether a workspace symbol exists.
func hasSymbolInformation(symbols []symbolInformation, name string, kind int) bool {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind {
			return true
		}
	}
	return false
}
