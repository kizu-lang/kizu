package lsp

import "testing"

// TestInlayHintsReturnLocalTypes checks conservative local type display.
func TestInlayHintsReturnLocalTypes(t *testing.T) {
	source := `enum Color { Red, Green }
struct Trace { label: []u8, count: i64, }

fn main(value: i64) {
    let n = 1;
    let neg = -1;
    let text = "kizu";
    let ok = true;
    let color = Color::Green;
    let trace = Trace { label: "x", count: n };
    let again = value;
    let same = n == 1;
    let narrow = cast<i32>(n);
    let ptr = cast<ptr<u8>>(raw());
}
`
	hints := InlayHints(source, Range{End: Position{Line: 99, Character: 99}})
	want := []struct {
		label string
		line  int
		char  int
	}{
		{": i64", 4, len("    let n")},
		{": i64", 5, len("    let neg")},
		{": []u8", 6, len("    let text")},
		{": bool", 7, len("    let ok")},
		{": Color", 8, len("    let color")},
		{": Trace", 9, len("    let trace")},
		{": i64", 10, len("    let again")},
		{": bool", 11, len("    let same")},
		{": i32", 12, len("    let narrow")},
		{": ptr<u8>", 13, len("    let ptr")},
	}
	if len(hints) != len(want) {
		t.Fatalf("got %d hints %#v, want %d", len(hints), hints, len(want))
	}
	for idx, wantHint := range want {
		got := hints[idx]
		if got.Label != wantHint.label || got.Kind != inlayHintKindType ||
			got.Position.Line != wantHint.line || got.Position.Character != wantHint.char {
			t.Fatalf("hint %d = %#v, want label %q at %d:%d",
				idx, got, wantHint.label, wantHint.line, wantHint.char)
		}
	}
}

// TestInlayHintsDoNotCrossBindingBoundaries avoids misleading invalid-source hints.
func TestInlayHintsDoNotCrossBindingBoundaries(t *testing.T) {
	source := `fn main() {
    let missing;
    let shown = 1;
}
`
	hints := InlayHints(source, Range{End: Position{Line: 99, Character: 99}})
	if len(hints) != 1 {
		t.Fatalf("got hints %#v, want one initialized binding hint", hints)
	}
	if hints[0].Label != ": i64" || hints[0].Position.Line != 2 {
		t.Fatalf("hint = %#v, want i64 for shown", hints[0])
	}
}

// TestInlayHintsRespectRange checks editors can request visible-range hints only.
func TestInlayHintsRespectRange(t *testing.T) {
	source := `fn main() {
    let hidden = 1;
    let shown = true;
}
`
	hints := InlayHints(source, Range{
		Start: Position{Line: 2, Character: 0},
		End:   Position{Line: 2, Character: 99},
	})
	if len(hints) != 1 {
		t.Fatalf("got hints %#v, want one visible hint", hints)
	}
	if hints[0].Label != ": bool" {
		t.Fatalf("label = %q, want bool", hints[0].Label)
	}
}
