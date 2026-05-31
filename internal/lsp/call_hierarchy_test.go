package lsp

import "testing"

// callHierarchyFixture wires three functions: main calls helper, helper calls
// leaf, leaf calls nothing.
//
//	0  fn leaf() -> i64 {
//	1      return 1;
//	2  }
//	3
//	4  fn helper() -> i64 {
//	5      return leaf();
//	6  }
//	7
//	8  fn main() -> void {
//	9      let a = helper();
//	10     let b = helper();
//	11     return;
//	12 }
func callHierarchyFixture() string {
	return "fn leaf() -> i64 {\n" +
		"    return 1;\n" +
		"}\n" +
		"\n" +
		"fn helper() -> i64 {\n" +
		"    return leaf();\n" +
		"}\n" +
		"\n" +
		"fn main() -> void {\n" +
		"    let a = helper();\n" +
		"    let b = helper();\n" +
		"    return;\n" +
		"}\n"
}

// prepareSingle resolves one call hierarchy item by name at the given position.
func prepareSingle(
	t *testing.T,
	server *Server,
	uri string,
	source string,
	name string,
) callHierarchyItem {
	t.Helper()
	items := server.prepareCallHierarchy(uri, positionIn(source, "fn "+name, name))
	if len(items) != 1 {
		t.Fatalf("prepareCallHierarchy(%q) = %#v, want one item", name, items)
	}
	return items[0]
}

// TestPrepareCallHierarchyResolvesFunction checks the seed item names the function.
func TestPrepareCallHierarchyResolvesFunction(t *testing.T) {
	uri := "file:///main.kizu"
	source := callHierarchyFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	item := prepareSingle(t, server, uri, source, "helper")
	if item.Name != "helper" || item.Kind != symbolKindFunction {
		t.Fatalf("item = %#v, want helper function", item)
	}
	if got := textIn(source, item.SelectionRange); got != "helper" {
		t.Fatalf("selection covers %q, want %q", got, "helper")
	}
}

// TestIncomingCallsFindsCallers checks helper's caller is main, called twice.
func TestIncomingCallsFindsCallers(t *testing.T) {
	uri := "file:///main.kizu"
	source := callHierarchyFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	item := prepareSingle(t, server, uri, source, "helper")
	calls := server.incomingCalls(item)
	if len(calls) != 1 {
		t.Fatalf("incomingCalls = %#v, want one caller", calls)
	}
	if calls[0].From.Name != "main" {
		t.Fatalf("caller = %q, want main", calls[0].From.Name)
	}
	if len(calls[0].FromRanges) != 2 {
		t.Fatalf("call ranges = %#v, want two call sites", calls[0].FromRanges)
	}
}

// TestOutgoingCallsFindsCallees checks helper calls leaf once.
func TestOutgoingCallsFindsCallees(t *testing.T) {
	uri := "file:///main.kizu"
	source := callHierarchyFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	item := prepareSingle(t, server, uri, source, "helper")
	calls := server.outgoingCalls(item)
	if len(calls) != 1 {
		t.Fatalf("outgoingCalls = %#v, want one callee", calls)
	}
	if calls[0].To.Name != "leaf" {
		t.Fatalf("callee = %q, want leaf", calls[0].To.Name)
	}
	if len(calls[0].FromRanges) != 1 {
		t.Fatalf("call ranges = %#v, want one call site", calls[0].FromRanges)
	}
}

// TestOutgoingCallsLeafHasNone checks a function calling nothing reports none.
func TestOutgoingCallsLeafHasNone(t *testing.T) {
	uri := "file:///main.kizu"
	source := callHierarchyFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	item := prepareSingle(t, server, uri, source, "leaf")
	if calls := server.outgoingCalls(item); len(calls) != 0 {
		t.Fatalf("outgoingCalls(leaf) = %#v, want empty", calls)
	}
}

// TestPrepareCallHierarchyUnknownDocument returns an empty slice.
func TestPrepareCallHierarchyUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if items := server.prepareCallHierarchy("file:///missing.kizu", Position{}); len(items) != 0 {
		t.Fatalf("prepareCallHierarchy on missing doc = %#v, want empty", items)
	}
}
