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
impl Node {
    fn deinit(self: Node) -> void {
        match self {
            Left(s) => s.deinit(),
            Right(a) => a.deinit(),
        }
    }
}`

// TestCheckAcceptsCopyUnionWithoutDeinit keeps copy/scalar unions non-owner.
func TestCheckAcceptsCopyUnionWithoutDeinit(t *testing.T) {
	cases := []string{
		`union Tag { A, B, C }
fn main() { let t = Tag::A; }`,
		`union Scalar { Count(i64), Byte(u8), Bytes([]u8), Empty }
fn main() { let s = Scalar::Empty; }`,
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
impl Value {
    fn deinit(self: Value) -> void {
        match self {
            Count(n) => print(n),
            Empty => print(0),
            Text(s) => s.deinit(),
        }
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
impl Inner { fn deinit(self: Inner) -> void { self.buf.deinit(); } }
union Outer {
    Wrapped(Inner),
    None,
}
impl Outer {
    fn deinit(self: Outer) -> void {
        match self {
            Wrapped(inner) => inner.deinit(),
            None => print(0),
        }
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("owner union with nested aggregate payload should check: %v", err)
	}
}

// TestCheckRejectsOwnerUnionMissingDeinit names the union missing cleanup.
func TestCheckRejectsOwnerUnionMissingDeinit(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}`
	assertCheckError(t, source,
		"owner-payload union `Node` requires explicit `deinit(self: Node) -> void`")
}

// TestCheckRejectsOwnerUnionMissingVariantCleanup names the leaking variant.
func TestCheckRejectsOwnerUnionMissingVariantCleanup(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
impl Node {
    fn deinit(self: Node) -> void {
        match self {
            Left(s) => print(0),
            Right(n) => print(n),
        }
    }
}`
	assertCheckError(t, source,
		"owner-payload union variant `Node::Left` must clean its payload via `s.deinit()`")
}

// TestCheckRejectsOwnerUnionBorrowedDeinitReceiver keeps deinit consuming.
func TestCheckRejectsOwnerUnionBorrowedDeinitReceiver(t *testing.T) {
	source := `union Node {
    Left(std::string::String),
    Right(i64),
}
impl Node {
    fn deinit(self: &Node) -> void {
        match self {
            Left(s) => print(0),
            Right(n) => print(n),
        }
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
impl Node {
    fn deinit(self: Node) -> void {
        return;
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
impl Node {
    fn deinit(self: Node) -> void {
        match self {
            Left(s) => s.deinit(),
            _ => print(0),
        }
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
		"generic owner-payload union `Holder` is unsupported in v0.2")
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
