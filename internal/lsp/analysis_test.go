package lsp

import "testing"

// TestCheckedDocumentCachesTypeFacts checks hover and inlay share analysis facts.
func TestCheckedDocumentCachesTypeFacts(t *testing.T) {
	source := navigationFixture()
	uri := "file:///main.kizu"
	server := NewServer(nil, nil)
	server.documents[uri] = source

	hints := server.inlayHints(uri, Range{End: Position{Line: 99, Character: 99}})
	if len(hints) == 0 {
		t.Fatalf("got no inlay hints, want cached local type hints")
	}
	cached, ok := server.analysis[uri]
	if !ok {
		t.Fatalf("checked document was not cached")
	}
	if len(cached.TypeFacts) == 0 {
		t.Fatalf("cached type facts missing")
	}
	fact, ok := typeFactAt(source, positionIn(source, "trace.label", "trace"), cached.TypeFacts)
	if !ok {
		t.Fatalf("cached type fact for trace missing")
	}
	if fact.typ != "Trace" {
		t.Fatalf("trace type = %q, want Trace", fact.typ)
	}
	param, ok := typeFactAt(source, positionIn(source, "value: i64", "value"), cached.TypeFacts)
	if !ok {
		t.Fatalf("cached type fact for value parameter missing")
	}
	if param.typ != "i64" {
		t.Fatalf("value type = %q, want i64", param.typ)
	}

	local := server.hover(uri, positionIn(source, "trace.label", "trace"))
	requireHoverContains(t, local, "trace: Trace")
}

// TestCheckedTypeFactsStayInScope checks one function's locals do not leak.
func TestCheckedTypeFactsStayInScope(t *testing.T) {
	source := `struct Trace { label: []u8, }

fn first() {
    let trace = Trace { label: "x" };
}

fn second() {
    trace;
}
`
	facts := documentTypeFacts(source)
	if _, ok := typeFactAt(source, positionIn(source, "trace;", "trace"), facts); ok {
		t.Fatalf("trace type fact leaked from first into second")
	}
}
