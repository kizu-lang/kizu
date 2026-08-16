package lsp

import (
	"strings"
	"testing"
)

// TestDefinitionReturnsLocalAndDeclarationLocations checks editor navigation basics.
func TestDefinitionReturnsLocalAndDeclarationLocations(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	requireDefinition(t, server.definition(uri, positionIn(source, "inspect(1)", "inspect")),
		uri, source, "fn inspect")
	requireDefinition(t, server.definition(uri, positionIn(source, "Color::Green;", "Green")),
		uri, source, "Green,")
	requireDefinition(t, server.definition(uri, positionIn(source, "trace.label", "label")),
		uri, source, "label:")
	requireDefinition(t, server.definition(uri, positionIn(source, "trace.rename", "rename")),
		uri, source, ") rename(")
	requireDefinition(t, server.definition(uri, positionIn(source, "trace.label", "trace")),
		uri, source, "let trace")
}

// TestHoverReturnsSymbolDetails checks hover text for locals and declarations.
func TestHoverReturnsSymbolDetails(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	local := server.hover(uri, positionIn(source, "trace.label", "trace"))
	requireHoverContains(t, local, "trace: Trace")
	traceType := server.hover(uri, positionIn(source, "Trace { label", "Trace"))
	requireHoverContains(t, traceType, "Trace record.")
	field := server.hover(uri, positionIn(source, "trace.label", "label"))
	requireHoverContains(t, field, "Human-readable label.")
	method := server.hover(uri, positionIn(source, "trace.rename", "rename"))
	requireHoverContains(t, method, "fn rename")
	requireHoverContains(t, method, "Renames the trace.")
	variant := server.hover(uri, positionIn(source, "Color::Green;", "Green"))
	requireHoverContains(t, variant, "enum Color::Green")
	requireHoverContains(t, variant, "Secondary color.")
	function := server.hover(uri, positionIn(source, "inspect(1)", "inspect"))
	requireHoverContains(t, function, "Inspects a trace value.")
}

// TestDocumentSymbolsReturnOutline checks VSCode Outline gets useful structure.
func TestDocumentSymbolsReturnOutline(t *testing.T) {
	symbols := DocumentSymbols(navigationFixture())
	requireSymbol(t, symbols, "Trace", symbolKindStruct)
	requireSymbol(t, symbols, "Color", symbolKindEnum)
	requireSymbol(t, symbols, "Trace.rename", symbolKindMethod)
	requireSymbol(t, symbols, "inspect", symbolKindFunction)
	requireSymbol(t, symbols, "main", symbolKindFunction)

	trace := requireSymbol(t, symbols, "Trace", symbolKindStruct)
	requireSymbol(t, trace.Children, "label", symbolKindField)
	color := requireSymbol(t, symbols, "Color", symbolKindEnum)
	requireSymbol(t, color.Children, "Green", symbolKindEnumMember)
}

// TestDefinitionUsesPackageModuleGraph checks navigation crosses package files.
func TestDefinitionUsesPackageModuleGraph(t *testing.T) {
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
		"src/main.kizu":  mainSource,
		"src/token.kizu": tokenSource,
	})
	mainURI := fileURIFromPath(root + "/src/main.kizu")
	tokenURI := fileURIFromPath(root + "/src/token.kizu")
	server := NewServer(nil, nil)
	server.documents[mainURI] = mainSource

	requireDefinition(t, server.definition(mainURI, positionIn(mainSource, "token::Token", "Token")),
		tokenURI, tokenSource, "Token")
	importPosition := positionIn(mainSource, "import app::token", "token")
	importDefinition := server.definition(mainURI, importPosition)
	requireDefinition(t, importDefinition,
		tokenURI, tokenSource, "pub struct")
}

// navigationFixture returns source that exercises navigation features together.
func navigationFixture() string {
	return `/// Available colors.
enum Color {
    Red,
    /// Secondary color.
    Green,
}

/// Trace record.
struct Trace {
    /// Human-readable label.
    label: []u8,
    count: i64,
}

/// Renames the trace.
fn (self: &var Trace) rename(name: []u8) -> void {
    return;
}

/// Inspects a trace value.
pub fn inspect(value: i64) -> void {
    let trace = Trace { label: "x", count: value };
    let color = Color::Green;
    trace.label;
    trace.rename("next");
    return;
}

fn main() {
    inspect(1);
    return;
}
`
}

// requireDefinition checks that one definition points to the expected line.
func requireDefinition(
	t *testing.T,
	locations []location,
	wantURI string,
	source string,
	declarationMarker string,
) {
	t.Helper()
	if len(locations) != 1 {
		t.Fatalf("got locations %#v, want one", locations)
	}
	if locations[0].URI != wantURI {
		t.Fatalf("uri = %q, want %q", locations[0].URI, wantURI)
	}
	want := positionIn(source, declarationMarker, strings.TrimSpace(declarationMarker))
	if locations[0].Range.Start.Line != want.Line {
		t.Fatalf("range = %#v, want line %d for %q",
			locations[0].Range, want.Line, declarationMarker)
	}
}

// requireHoverContains checks hover markdown text.
func requireHoverContains(t *testing.T, got *hover, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("hover missing, want %q", want)
	}
	if !strings.Contains(got.Contents.Value, want) {
		t.Fatalf("hover = %#v, want it to contain %q", got, want)
	}
}

// requireSymbol returns one document symbol by name and kind.
func requireSymbol(
	t *testing.T,
	symbols []documentSymbol,
	name string,
	kind int,
) documentSymbol {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind {
			return symbol
		}
	}
	t.Fatalf("symbol %q kind %d missing from %#v", name, kind, symbols)
	return documentSymbol{}
}

// positionIn returns the position of needle inside a larger source marker.
func positionIn(source string, marker string, needle string) Position {
	offset := strings.Index(source, marker)
	if offset < 0 {
		panic("marker not found: " + marker)
	}
	inner := strings.Index(marker, needle)
	if inner < 0 {
		panic("needle not found in marker: " + needle)
	}
	return positionFromOffset(source, offset+inner)
}
