package ownership

import (
	"strings"
	"testing"
)

// ownerUnionPrelude declares an owner-payload union and its active-variant deinit.
const ownerUnionPrelude = `union Node {
    Left(std::string::String),
    Right(std::array::Array<i64>),
}
impl Node {
    fn deinit(self: Node) -> void {
        match self {
            Left(s) => s.deinit(),
            Right(a) => a.deinit(),
        }
    }
}
`

// TestCheckAcceptsOwnerUnionActiveVariantCleanup accepts cleaning the active
// payload bound by a match inside the union's own deinit.
func TestCheckAcceptsOwnerUnionActiveVariantCleanup(t *testing.T) {
	if err := checkSource(ownerUnionPrelude); err != nil {
		t.Fatalf("active-variant cleanup should check: %v", err)
	}
}

// TestCheckAcceptsOwnerUnionMoveAndDeinit moves a payload into a union value and
// consumes it through the union deinit.
func TestCheckAcceptsOwnerUnionMoveAndDeinit(t *testing.T) {
	source := ownerUnionPrelude + `fn consume(s: std::string::String) -> void {
    let n = Node::Left(s);
    n.deinit();
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("construct-and-deinit should check: %v", err)
	}
}

// TestCheckRejectsOwnerUnionUseAfterMove keeps owner unions move-only.
func TestCheckRejectsOwnerUnionUseAfterMove(t *testing.T) {
	source := ownerUnionPrelude + `fn show(n: &Node) -> void { return; }
fn consume(s: std::string::String) -> void {
    let n = Node::Left(s);
    show(n);
    let m = n;
    show(m);
    let other = n;
}`
	assertMoveError(t, source, "moved value `n` was used")
}

// TestCheckRejectsOwnerUnionDoubleCleanup rejects calling deinit twice.
func TestCheckRejectsOwnerUnionDoubleCleanup(t *testing.T) {
	source := ownerUnionPrelude + `fn consume(s: std::string::String) -> void {
    let n = Node::Left(s);
    n.deinit();
    n.deinit();
}`
	assertMoveError(t, source, "moved value `n` was used")
}

// TestCheckRejectsOwnerUnionUseAfterDeinit rejects reading a deinitialized union.
func TestCheckRejectsOwnerUnionUseAfterDeinit(t *testing.T) {
	source := ownerUnionPrelude + `fn show(n: &Node) -> void { return; }
fn consume(s: std::string::String) -> void {
    let n = Node::Left(s);
    n.deinit();
    show(n);
}`
	assertMoveError(t, source, "moved value `n` was borrowed")
}

// TestCheckRejectsActivePayloadCleanupOutsideDeinit proves the active payload is
// borrowed in an ordinary match, so inactive payload storage is never owned or
// cleaned outside the union's own deinit.
func TestCheckRejectsActivePayloadCleanupOutsideDeinit(t *testing.T) {
	source := ownerUnionPrelude + `fn drop_node(n: Node) -> void {
    match n {
        Left(s) => s.deinit(),
        Right(a) => a.deinit(),
    }
}`
	assertMoveError(t, source, "`String.deinit` requires owned String receiver")
}

// TestCheckRejectsOwnerUnionReuseAfterDeinitDispatch proves the deinit dispatch
// consumes the union: re-matching `self` after the active payload was cleaned is
// a use-after-move, so deinitialized payload storage can never be read.
func TestCheckRejectsOwnerUnionReuseAfterDeinitDispatch(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(std::array::Array<i64>),
}
impl Node {
    fn deinit(self: Node) -> void {
        match self {
            Left(s) => s.deinit(),
            Right(a) => a.deinit(),
        }
        match self {
            Left(s) => print(0),
            Right(a) => print(1),
        }
    }
}`
	assertMoveError(t, source, "moved value `self` was used")
}

// assertMoveError fails unless move-checking source reports an error with want.
func assertMoveError(t *testing.T, source string, want string) {
	t.Helper()
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q, want substring %q", err.Error(), want)
	}
}
