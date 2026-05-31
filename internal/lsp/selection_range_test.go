package lsp

import "testing"

// TestSelectionRangeExpandsOutward checks the chain grows from the token under
// the cursor through the enclosing braces up to the whole document.
func TestSelectionRangeExpandsOutward(t *testing.T) {
	uri := "file:///main.kizu"
	source := navigationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	ranges := server.selectionRanges(uri, []Position{
		positionIn(source, "inspect(1)", "inspect"),
	})
	if len(ranges) != 1 || ranges[0] == nil {
		t.Fatalf("selectionRanges = %#v, want one non-nil hierarchy", ranges)
	}

	// Innermost node covers the identifier itself.
	innermost := ranges[0]
	if got := textIn(source, innermost.Range); got != "inspect" {
		t.Fatalf("innermost range covers %q, want %q", got, "inspect")
	}

	// Each parent must strictly enclose its child, and the chain must reach the
	// document root (a node with no parent).
	depth := 0
	for node := innermost; node.Parent != nil; node = node.Parent {
		if !rangeContainsRange(node.Parent.Range, node.Range) {
			t.Fatalf("parent %#v does not enclose child %#v", node.Parent.Range, node.Range)
		}
		depth++
	}
	if depth < 2 {
		t.Fatalf("selection chain depth = %d, want at least 2 levels", depth)
	}
}

// TestSelectionRangeReachesDocumentRoot checks the outermost node spans all
// lines of the source.
func TestSelectionRangeReachesDocumentRoot(t *testing.T) {
	uri := "file:///main.kizu"
	source := navigationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	ranges := server.selectionRanges(uri, []Position{
		positionIn(source, "inspect(1)", "inspect"),
	})
	root := ranges[0]
	for root.Parent != nil {
		root = root.Parent
	}
	doc := documentRange(source)
	if root.Range != doc {
		t.Fatalf("root range = %#v, want document range %#v", root.Range, doc)
	}
}

// TestSelectionRangeUnknownDocument checks unknown docs yield an empty slice.
func TestSelectionRangeUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	ranges := server.selectionRanges("file:///missing.kizu", []Position{{}})
	if len(ranges) != 0 {
		t.Fatalf("selectionRanges on missing doc = %#v, want empty", ranges)
	}
}
