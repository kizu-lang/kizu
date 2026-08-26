package types

import (
	"strings"
	"testing"
)

// ownerUnionDeinit is a canonical owner-payload union with active-variant cleanup.
const ownerUnionDeinit = `union Node {
    Left(std::string::String),
    Right(std::array::Array<i64>),
}
fn (self: Node) deinit(allocator: Allocator) -> void {
    match self {
        Left(s) => s.deinit(allocator),
        Right(a) => a.deinit(allocator),
    }
}`

// TestCheckAcceptsCopyUnionWithoutDeinit keeps copy/scalar unions non-owner.
func TestCheckAcceptsCopyUnionWithoutDeinit(t *testing.T) {
	cases := []string{
		`union Tag { A, B, C }
fn main() { let _ = Tag::A; }`,
		`union Scalar { Count(i64), Byte(u8), Bytes([]u8), Empty }
fn main() { let _ = Scalar::Empty; }`,
	}
	for _, source := range cases {
		if err := checkSource(source); err != nil {
			t.Fatalf("copy union should not require deinit: %v\nsource:\n%s", err, source)
		}
	}
}

// TestCheckAcceptsOwnerUnionWithActiveVariantDeinit accepts the supported shape.
func TestCheckAcceptsOwnerUnionWithActiveVariantDeinit(t *testing.T) {
	if err := checkSource(ownerUnionDeinit); err != nil {
		t.Fatalf("owner union with active-variant deinit should check: %v", err)
	}
}

// TestCheckAcceptsOwnerUnionWithCopyVariants accepts mixed owner/copy variants
// where only the owner payload is cleaned and copy variants do nothing.
func TestCheckAcceptsOwnerUnionWithCopyVariants(t *testing.T) {
	source := `union Value {
    Count(i64),
    Empty,
    Text(std::string::String),
}
fn (self: Value) deinit(allocator: Allocator) -> void {
    match self {
        Count(n) => print(n),
        Empty => print(0),
        Text(s) => s.deinit(allocator),
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("mixed owner/copy union should check: %v", err)
	}
}

// TestCheckAcceptsOwnerUnionThroughNestedAggregate cleans a nested owner payload
// by delegating to the payload aggregate's own deinit.
func TestCheckAcceptsOwnerUnionThroughNestedAggregate(t *testing.T) {
	source := `struct Inner { buf: std::array::Array<i64> }
fn (self: Inner) deinit(allocator: Allocator) -> void { self.buf.deinit(allocator); }
union Outer {
    Wrapped(Inner),
    None,
}
fn (self: Outer) deinit(allocator: Allocator) -> void {
    match self {
        Wrapped(inner) => inner.deinit(allocator),
        None => print(0),
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("owner union with nested aggregate payload should check: %v", err)
	}
}

// TestCheckRejectsBoxOwnerUnionMissingCleanup keeps Box payload ownership on
// the same DeinitOwners definition used by the rest of the checker.
func TestCheckRejectsBoxOwnerUnionMissingCleanup(t *testing.T) {
	source := `struct Node { child: std::mem::Box<i64> }
union Slot { Held(Node), Empty }
fn (self: Slot) deinit(allocator: Allocator) -> void {
    match self {
        Held(node) => print(0),
        Empty => print(0),
    }
}`
	assertCheckError(t, source,
		"owner-payload union variant `Slot::Held` must clean its payload via `node.deinit(allocator)`")
}

// TestCheckRejectsDeclaredOwnerUnionMissingCleanup treats a source-visible
// deinit declaration as the payload type's own cleanup contract.
func TestCheckRejectsDeclaredOwnerUnionMissingCleanup(t *testing.T) {
	source := `struct Resource {}
fn (self: Resource) deinit(allocator: Allocator) -> void {}
union Slot { Held(Resource), Empty }
fn (self: Slot) deinit(allocator: Allocator) -> void {
    match self {
        Held(resource) => print(0),
        Empty => print(0),
    }
}`
	assertCheckError(t, source,
		"owner-payload union variant `Slot::Held` must clean its payload"+
			" via `resource.deinit(allocator)`")
}

// TestCheckAcceptsOwnerUnionWithoutDeclaredDeinit checks a union that declares
// no cleanup is accepted: its body is the derived one, and there is no author's
// body to validate.
func TestCheckAcceptsOwnerUnionWithoutDeclaredDeinit(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("owner union without a declared deinit should check: %v", err)
	}
}

// TestCheckRejectsOwnerUnionMissingVariantCleanup names the leaking variant.
func TestCheckRejectsOwnerUnionMissingVariantCleanup(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
fn (self: Node) deinit(allocator: Allocator) -> void {
    match self {
        Left(s) => print(0),
        Right(n) => print(n),
    }
}`
	assertCheckError(t, source,
		"owner-payload union variant `Node::Left` must clean its payload via `s.deinit(allocator)`")
}

// TestCheckRejectsOwnerUnionBorrowedDeinitReceiver keeps deinit consuming.
func TestCheckRejectsOwnerUnionBorrowedDeinitReceiver(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
fn (self: &Node) deinit(allocator: Allocator) -> void {
    match self {
        Left(s) => print(0),
        Right(n) => print(n),
    }
}`
	assertCheckError(t, source,
		"owner-payload union `Node` deinit must take `self` by value")
}

// TestCheckRejectsOwnerUnionDeinitWithoutMatch requires active-variant dispatch.
func TestCheckRejectsOwnerUnionDeinitWithoutMatch(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
fn (self: Node) deinit(allocator: Allocator) -> void {
    return;
}`
	assertCheckError(t, source,
		"owner-payload union `Node` deinit must dispatch on `self` with an exhaustive `match`")
}

// TestCheckRejectsOwnerUnionDeinitMatchNotFirst rejects a deinit that can skip
// the active-variant cleanup by running a statement before `match self`.
func TestCheckRejectsOwnerUnionDeinitMatchNotFirst(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
fn (self: Node) deinit(allocator: Allocator) -> void {
    if true { return; }
    match self {
        Left(s) => s.deinit(allocator),
        Right(n) => n,
    }
}`
	assertCheckError(t, source,
		"owner-payload union `Node` deinit must dispatch on `self` with an exhaustive `match`")
}

// TestCheckRejectsOwnerUnionWildcardDeinit rejects `_` hiding owner variants.
func TestCheckRejectsOwnerUnionWildcardDeinit(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
fn (self: Node) deinit(allocator: Allocator) -> void {
    match self {
        Left(s) => s.deinit(allocator),
        _ => print(0),
    }
}`
	assertCheckError(t, source,
		"owner-payload union `Node` deinit `match` cannot use `_`")
}

// TestCheckRejectsGenericOwnerUnion rejects the unsupported general shape visibly.
func TestCheckRejectsGenericOwnerUnion(t *testing.T) {
	source := `union Holder<T> {
    Items(std::array::Array<T>),
    None,
}`
	assertCheckError(t, source,
		"generic owner-payload union `Holder` is unsupported")
}

// assertCheckError fails unless checking source reports an error containing want.
func assertCheckError(t *testing.T, source string, want string) {
	t.Helper()
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q, want substring %q", err.Error(), want)
	}
}
