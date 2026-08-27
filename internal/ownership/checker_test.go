package ownership

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/project"
)

// TestCheckAcceptsCopyReuse checks that copy values are reusable after move contexts.
func TestCheckAcceptsCopyReuse(t *testing.T) {
	source := `fn take(a: i64) { print(a); }
fn main() {
    let a = 1;
    let b = a;
    take(a);
    print(a);
    print(b);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsMoveErrorsInTestDecl checks test blocks share move checking.
func TestCheckRejectsMoveErrorsInTestDecl(t *testing.T) {
	source := `struct Name { value: []u8 }
test "move error" {
    let a = Name { value: "hello" };
    let b = move a;
    print(a.value);
    print(b.value);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "moved value `a` was used") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsMoveErrors checks basic non-copy move failures.
func TestCheckRejectsMoveErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "assignment move",
			source: `struct Name { value: []u8 }
fn main() {
    let a = Name { value: "hello" };
    let b = move a;
    print(a.value);
    print(b.value);
}`,
			want: "moved value `a` was used",
		},
		{
			name: "function argument move",
			source: `struct Name { value: []u8 }
fn take(name: Name) { print(name.value); }
fn main() {
    let name = Name { value: "alice" };
    take(move name);
    print(name.value);
}`,
			want: "moved value `name` was used",
		},
		{
			name: "double move",
			source: `struct Name { value: []u8 }
fn take(name: Name) { print(name.value); }
fn main() {
    let name = Name { value: "alice" };
    take(move name);
    take(name);
}`,
			want: "moved value `name` was used",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckBorrowArgumentDoesNotMove checks borrow parameters preserve ownership.
func TestCheckBorrowArgumentDoesNotMove(t *testing.T) {
	source := `fn show(s: &[]u8) { print(s); }
fn main() {
    let name = "alice";
    show(name);
    print(name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAllowsBorrowProvenanceReturns keeps returned borrows tied to local owners.
func TestCheckAllowsBorrowProvenanceReturns(t *testing.T) {
	source := `fn shared(value: &i64) -> &i64 {
    return value;
}
fn mutable(value: &var i64) -> &var i64 {
    return value;
}
fn main() {
    var value = 1;
    let read = shared(value);
    print(read.*);
    let write = mutable(value);
    write.* = 2;
    print(value);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsFixedBufferAllocator keeps the tied-allocator chain usable:
// buffer -> view -> allocator -> owner, including a helper that takes the
// Allocator parameter, with everything consumed in the frame (ADR-0099).
func TestCheckAcceptsFixedBufferAllocator(t *testing.T) {
	source := `fn fill(allocator: Allocator) -> !std::array::Array<i64> {
    var values = std::array::new<i64>(allocator);
    errdefer values.deinit(allocator);
    try values.append(allocator, 7);
    return move values;
}
fn main() -> !void {
    var buf = [512]u8{};
    let scratch = buf.as_mut_bytes();
    let alloc = std::mem::fixed_buffer(scratch);
    var values = try fill(alloc);
    defer values.deinit(alloc);
    try values.append(alloc, 1);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsFixedBufferEscapes pins the tied-allocator escape rules
// (ADR-0099): owners and allocators tied to a local buffer stay in the frame.
func TestCheckRejectsFixedBufferEscapes(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "owner return",
			source: `fn leak() -> std::array::Array<i64> {
    var buf = [512]u8{};
    let scratch = buf.as_mut_bytes();
    let alloc = std::mem::fixed_buffer(scratch);
    let values = std::array::new<i64>(alloc);
    return values;
}
fn main(allocator: Allocator) -> !void {
    var values = leak();
    values.deinit(allocator);
    return;
}`,
			want: "allocated from a tied allocator and cannot escape its frame",
		},
		{
			name: "allocator local escape",
			source: `fn leak() -> Allocator {
    var buf = [512]u8{};
    let scratch = buf.as_mut_bytes();
    return std::mem::fixed_buffer(scratch);
}
fn main(allocator: Allocator) -> !void {
    let alloc = leak();
    var values = std::array::new<i64>(alloc);
    defer values.deinit(allocator);
    try values.append(allocator, 1);
    return;
}`,
			want: "returns an allocator tied to local state and cannot escape",
		},
		{
			name: "buffer reborrow while allocator lives",
			source: `fn main() -> !void {
    var buf = [512]u8{};
    let scratch = buf.as_mut_bytes();
    let alloc = std::mem::fixed_buffer(scratch);
    var values = std::array::new<i64>(alloc);
    defer values.deinit(alloc);
    let sneak = buf.as_mut_bytes();
    sneak[0] = cast<u8>(1);
    try values.append(alloc, 1);
    return;
}`,
			want: "value `buf` cannot be borrowed while mutably borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsViewCapture pins the view-capture chain (ADR-0100):
// buffer -> view -> struct, through both the literal and a factory call, plus
// the precision case of a view-free struct return.
func TestCheckAcceptsViewCapture(t *testing.T) {
	source := `struct BytesIter {
    pub bytes: []u8,
    pub index: i64,
}
struct Stats {
    pub len: i64,
}
fn iter(bytes: []u8) -> BytesIter {
    return BytesIter { bytes: bytes, index: 0 };
}
fn stats(bytes: []u8) -> Stats {
    return Stats { len: std::mem::len(bytes) };
}
fn main() -> !void {
    var buf = [16]u8{};
    let view = buf.as_bytes();
    let direct = BytesIter { bytes: view, index: 0 };
    print(direct.index);
    var made = iter(view);
    print(made.index);
    let s = stats(view);
    print(s.len);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsOwnerViewCapture keeps a view tie and an explicit deinit
// obligation on the same binding. The source becomes writable again after
// the owner is consumed.
func TestCheckAcceptsOwnerViewCapture(t *testing.T) {
	source := `struct Reader {
    pub input: []u8,
    pub pending: std::array::Array<i64>,
}
fn reader(allocator: Allocator, input: []u8) -> Reader {
    return Reader {
        input: input,
        pending: std::array::new<i64>(allocator),
    };
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var buf = [16]u8{};
    let view = buf.as_bytes();
    var value = reader(allocator, view);
    print(value.pending.len());
    value.deinit(allocator);
    let writable = buf.as_mut_bytes();
    writable[0] = cast<u8>(1);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsOwnerViewCaptureMisuse checks that supporting a mixed
// aggregate neither drops its cleanup nor releases its source tie early.
func TestCheckRejectsOwnerViewCaptureMisuse(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "missing cleanup",
			source: `struct Reader {
    pub input: []u8,
    pub pending: std::array::Array<i64>,
}
fn reader(allocator: Allocator, input: []u8) -> Reader {
    return Reader { input: input, pending: std::array::new<i64>(allocator) };
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var buf = [16]u8{};
    let view = buf.as_bytes();
    let value = reader(allocator, view);
    print(value.pending.len());
    return;
}`,
			want: "owned value `value` is never deinitialized",
		},
		{
			name: "source mutation while owner lives",
			source: `struct Reader {
    pub input: []u8,
    pub pending: std::array::Array<i64>,
}
fn reader(allocator: Allocator, input: []u8) -> Reader {
    return Reader { input: input, pending: std::array::new<i64>(allocator) };
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var buf = [16]u8{};
    let view = buf.as_bytes();
    let value = reader(allocator, view);
    defer value.deinit(allocator);
    let writable = buf.as_mut_bytes();
    writable[0] = cast<u8>(1);
    print(value.pending.len());
    return;
}`,
			want: "value `buf` cannot be mutably borrowed while borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsViewCaptureEscapes pins the view-capture escape rules
// (ADR-0100): a struct tied to a local view stays in the frame, and views
// cannot smuggle out through `&var` parameters.
func TestCheckRejectsViewCaptureEscapes(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "capture return",
			source: `struct BytesIter {
    pub bytes: []u8,
    pub index: i64,
}
fn make() -> BytesIter {
    var buf = [16]u8{};
    let view = buf.as_bytes();
    let it = BytesIter { bytes: view, index: 0 };
    return it;
}
fn main() -> !void {
    let it = make();
    print(it.index);
    return;
}`,
			want: "borrowed value `it` cannot escape",
		},
		{
			name: "view field escape",
			source: `struct BytesIter {
    pub bytes: []u8,
    pub index: i64,
}
fn leak() -> []u8 {
    var buf = [16]u8{};
    let view = buf.as_bytes();
    let it = BytesIter { bytes: view, index: 0 };
    return it.bytes;
}
fn main() -> !void {
    print(leak());
    return;
}`,
			want: "view field `it.bytes` cannot escape its borrowed owner",
		},
		{
			name: "out-parameter smuggle",
			source: `struct BytesIter {
    pub bytes: []u8,
    pub index: i64,
}
fn smuggle(bytes: []u8, out: &var BytesIter) -> void {
    out.bytes = bytes;
    return;
}
fn main() -> !void {
    var buf = [16]u8{};
    let view = buf.as_bytes();
    var it = BytesIter { bytes: "", index: 0 };
    smuggle(view, it);
    print(it.index);
    return;
}`,
			want: "borrowed value `view` cannot escape",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsTransitiveViewCaptureMutation keeps every link in a
// String-view-helper-capture chain borrowed for the capture body.
func TestCheckRejectsTransitiveViewCaptureMutation(t *testing.T) {
	source := `struct SplitView {
    pub left: []u8,
    pub right: []u8,
}
fn tail(bytes: []u8) -> []u8 {
    return bytes[1..std::mem::len(bytes)];
}
fn split(bytes: []u8) -> !?SplitView {
    return SplitView { left: bytes[0..1], right: bytes[1..std::mem::len(bytes)] };
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var text = try std::string::from_bytes(allocator, "abc");
    defer text.deinit(allocator);
    let bytes = text.as_bytes();
    let suffix = tail(bytes);
    if try split(suffix) |parts| {
        try text.append_bytes(allocator, "d");
        print(parts.left);
    }
    return;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(),
		"String.append_bytes` cannot run while string is borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsViewCaptureMutationThroughBorrowArgument derives the
// capture tie from an explicit borrow parameter rather than argument syntax.
func TestCheckRejectsViewCaptureMutationThroughBorrowArgument(t *testing.T) {
	source := `struct SplitView {
    pub left: []u8,
    pub right: []u8,
}
fn split(text: &std::string::String) -> ?SplitView {
    let bytes = text.as_bytes();
    return SplitView { left: bytes[0..1], right: bytes[1..std::mem::len(bytes)] };
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var text = try std::string::from_bytes(allocator, "abc");
    defer text.deinit();
    if split(&text) |parts| {
        try text.append_bytes("d");
        print(parts.left);
    }
    return;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(),
		"String.append_bytes` cannot run while string is borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsBorrowProvenanceReturnConflicts checks parent restrictions stay local.
func TestCheckRejectsBorrowProvenanceReturnConflicts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "assign while shared return live",
			source: `fn shared(value: &i64) -> &i64 {
    return value;
}
fn main() {
    var value = 1;
    let read = shared(value);
    value = 2;
    print(read.*);
}`,
			want: "cannot be assigned while borrowed",
		},
		{
			name: "read while mutable return live",
			source: `fn mutable(value: &var i64) -> &var i64 {
    return value;
}
fn main() {
    var value = 1;
    let write = mutable(value);
    print(value);
    write.* = 2;
}`,
			want: "cannot be read while mutably borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckMutableBorrowArgumentDoesNotMove checks &var preserves ownership.
func TestCheckMutableBorrowArgumentDoesNotMove(t *testing.T) {
	source := `struct User { name: []u8 }
fn show(user: &var User) { print(user.name); }
fn main() {
    let user = User { name: "alice" };
    show(user);
    print(user.name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsMutableBorrowForwarding checks &var params can be reborrowed for calls.
func TestCheckAcceptsMutableBorrowForwarding(t *testing.T) {
	source := `struct User { name: []u8 }
fn rename(user: &var User) { user.name = "bob"; }
fn outer(user: &var User) {
    rename(user);
    user.name = "carol";
}
fn main() {
    var user = User { name: "alice" };
    outer(user);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsFieldBorrowProjection checks call-scoped field borrow arguments.
func TestCheckAcceptsFieldBorrowProjection(t *testing.T) {
	source := `struct Pair { left: i64, right: i64 }
fn touch(left: &var i64, right: &var i64) {
    print(left);
    print(right);
}
fn main() {
    var pair = Pair { left: 1, right: 2 };
    touch(&var pair.left, &var pair.right);
    print(pair.left);
    print(pair.right);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsFieldBorrowProjectionConflicts keeps projected borrows call-scoped.
func TestCheckRejectsFieldBorrowProjectionConflicts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "same field twice",
			source: `struct Pair { left: i64, right: i64 }
fn touch(left: &var i64, right: &var i64) {
    print(left);
    print(right);
}
fn main() {
    var pair = Pair { left: 1, right: 2 };
    touch(&var pair.left, &var pair.left);
}`,
			want: "field `pair.left` cannot be borrowed while mutably borrowed",
		},
		{
			name: "moved base",
			source: `struct Pair { left: i64, right: i64 }
fn (self: Pair) deinit(allocator: Allocator) -> void { }
fn touch(left: &var i64) { print(left); }
fn main() {
    var pair = Pair { left: 1, right: 2 };
    let moved = move pair;
    touch(&var pair.left);
    print(moved.left);
}`,
			want: "moved value `pair` was borrowed",
		},
		{
			name: "deinitialized field",
			source: `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn touch(users: &var std::arena::Arena<User, UserArena>) {
    print(0);
}
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
    touch(&var self.users);
    return;
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    registry.deinit(allocator);
}`,
			want: "field `self.users` was deinitialized",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsMutableBorrowForwardingAlias checks reborrows stay exclusive.
func TestCheckRejectsMutableBorrowForwardingAlias(t *testing.T) {
	source := `struct User { name: []u8 }
fn use(left: &User, right: &var User) {
    print(left.name);
    print(right.name);
}
fn outer(user: &var User) {
    use(user, user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "cannot be mutably borrowed while borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsErrorReturnAfterLocalView checks a failure return can
// follow a local borrowed view; the error value carries nothing from it.
func TestCheckAcceptsErrorReturnAfterLocalView(t *testing.T) {
	source := `error ViewError { Bad }
fn fail(allocator: Allocator, text: std::string::String) -> !void {
    let bytes = text.as_bytes();
    print(bytes);
    text.deinit(allocator);
    return ViewError::Bad;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsBorrowEscape checks borrowed parameters cannot become owned values.
func TestCheckRejectsBorrowEscape(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "return borrowed parameter",
			source: `fn bad(s: &[]u8) -> []u8 {
    return s;
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "store borrowed parameter in local",
			source: `fn bad(s: &[]u8) {
    let alias = s;
    print(alias);
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			// A view-carrying return could launder the borrow away, so the
			// lend that a scalar/void return permits (ADR-0096) stays rejected.
			name: "pass borrowed parameter to view-returning function",
			source: `fn take(s: []u8) -> []u8 {
    return s;
}
fn bad(s: &[]u8) {
    let escaped = take(s);
    print(escaped);
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow field",
			source: `struct Bad {
    value: &[]u8,
}
fn main() {}`,
			want: "struct field `Bad.value` cannot store borrow",
		},
		{
			name: "move non-copy deref",
			source: `struct User { name: []u8 }
fn bad(user: &User) -> User {
    return user.*;
}`,
			want: "cannot be moved out of borrow",
		},
		{
			name: "move non-copy mutable deref",
			source: `struct User { name: []u8 }
fn bad(user: &var User) -> User {
    return user.*;
}`,
			want: "cannot be moved out of borrow",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAllowsCopyDeref checks copying a primitive through a borrow is allowed.
func TestCheckAllowsCopyDeref(t *testing.T) {
	source := `fn copy_value(value: &i64) -> i64 {
    return value.*;
}
fn main() {
    let x = 1;
    print(copy_value(x));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsRawPointerDerefSyntax keeps unsafe pointer access out of borrow rules.
func TestCheckAcceptsRawPointerDerefSyntax(t *testing.T) {
	source := `struct Node { tag: i64, name: []u8 }
fn read_tag(node: ptr<const Node>) -> i64 {
    return unsafe node.*.tag;}
fn write_tag(node: ptr<Node>, tag: i64) -> void {
    unsafe node.*.tag = tag;
    return;}
fn replace(node: ptr<Node>, value: Node) -> void {
    unsafe node.* = move value;
    return;}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsMoveWhileBorrowed checks overlapping borrow and move in a call.
func TestCheckRejectsMoveWhileBorrowed(t *testing.T) {
	source := `struct Name { value: []u8 }
fn use(a: &Name, b: Name) {
    print(a.value);
    print(b.value);
}
fn main() {
    let name = Name { value: "alice" };
    use(name, name);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "value `name` cannot be moved while borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsMutableBorrowConflicts checks & and &var cannot overlap.
func TestCheckRejectsMutableBorrowConflicts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "shared then mutable",
			source: `struct User { name: []u8 }
fn use(left: &User, right: &var User) {
    print(left.name);
    print(right.name);
}
fn main() {
    let user = User { name: "alice" };
    use(user, user);
}`,
			want: "cannot be mutably borrowed while borrowed",
		},
		{
			name: "mutable then shared",
			source: `struct User { name: []u8 }
fn use(left: &var User, right: &User) {
    print(left.name);
    print(right.name);
}
fn main() {
    let user = User { name: "alice" };
    use(user, user);
}`,
			want: "cannot be borrowed while mutably borrowed",
		},
		{
			name: "double mutable",
			source: `struct User { name: []u8 }
fn use(left: &var User, right: &var User) {
    print(left.name);
    print(right.name);
}
fn main() {
    let user = User { name: "alice" };
    use(user, user);
}`,
			want: "cannot be borrowed while mutably borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsTwoPhaseReceiverBorrow checks a `&var self` call can read
// its own receiver in argument position: the exclusive borrow activates after
// the arguments settle.
func TestCheckAcceptsTwoPhaseReceiverBorrow(t *testing.T) {
	source := `struct Registry { cursor: i64 }
fn (self: &Registry) peek() -> i64 {
    return self.cursor + 1;
}
fn (self: &var Registry) select(index: i64) -> void {
    self.cursor = index;
}
fn (self: &var Registry) advance() -> void {
    self.select(self.cursor);
    self.select(self.peek());
}
fn main() {
    var reg = Registry { cursor: 1 };
    reg.advance();
    reg.select(reg.cursor + 1);
    print(reg.cursor);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsTwoPhaseReceiverBorrowConflicts keeps arguments that borrow
// the receiver themselves out of a `&var self` call.
func TestCheckRejectsTwoPhaseReceiverBorrowConflicts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "mutating method in argument position",
			source: `struct Registry { cursor: i64 }
fn (self: &var Registry) next() -> i64 {
    self.cursor = self.cursor + 1;
    return self.cursor;
}
fn (self: &var Registry) select(index: i64) -> void {
    self.cursor = index;
}
fn (self: &var Registry) advance() -> void {
    self.select(self.next());
}`,
			want: "method `Registry.next` mutates its receiver while it is borrowed",
		},
		{
			name: "receiver field borrow argument",
			source: `struct Registry { cursor: i64 }
fn (self: &var Registry) inspect(peek: &i64) -> void {
    self.cursor = peek.*;
}
fn main() {
    var reg = Registry { cursor: 1 };
    reg.inspect(&reg.cursor);
}`,
			want: "value `reg` cannot be mutably borrowed while field is borrowed",
		},
		{
			name: "receiver mutable field borrow argument",
			source: `struct Registry { cursor: i64 }
fn (self: &var Registry) inspect(peek: &var i64) -> void {
    self.cursor = peek.*;
}
fn main() {
    var reg = Registry { cursor: 1 };
    reg.inspect(&var reg.cursor);
}`,
			want: "field `reg.cursor` cannot be mutably borrowed while value is borrowed",
		},
		{
			name: "field receiver aliased by mutable borrow of the same field",
			source: `struct Counter { count: i64 }
fn (self: &var Counter) steal(other: &var Counter) -> void {
    self.count = other.count;
}
struct Holder { counter: Counter }
fn (self: &var Holder) go() -> void {
    self.counter.steal(&var self.counter);
}`,
			want: "field `self.counter` cannot be mutably borrowed while borrowed",
		},
		{
			name: "field receiver aliased by shared borrow of the owner",
			source: `struct Counter { count: i64 }
struct Holder { counter: Counter }
fn (self: &var Counter) load(owner: &Holder) -> void {
    self.count = owner.counter.count;
}
fn (self: &var Holder) go() -> void {
    self.counter.load(&self);
}`,
			want: "field `self.counter` cannot be mutably borrowed while value is borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsBorrowedFieldMove checks field access cannot bypass borrow rules.
func TestCheckRejectsBorrowedFieldMove(t *testing.T) {
	source := `struct User { name: []u8 }
struct Box { user: User }
fn take(user: User) { print(user.name); }
fn bad(box: &Box) {
    take(box.user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "cannot be moved out of borrowed value `box`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsArenaHandle checks arena handles with matching provenance.
func TestCheckAcceptsArenaHandle(t *testing.T) {
	source := `struct UserArena {} struct User {
    name: []u8,
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    print(users.at(alice).name);
    users.deinit(allocator);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsDeferredArenaCleanup checks cleanup runs at block exit.
func TestCheckAcceptsDeferredArenaCleanup(t *testing.T) {
	source := `struct UserArena {} struct User {
    name: []u8,
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    defer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    print(users.at(alice).name);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArenaAllocatorReadOnly checks arena construction reads allocator capabilities.
func TestCheckArenaAllocatorReadOnly(t *testing.T) {
	source := `struct UserArena {} struct User {
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    print(left);
    print(right);
    left.deinit(allocator);
    right.deinit(allocator);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsArenaHandleProvenanceErrors checks Phase 6 provenance errors.
func TestCheckRejectsArenaHandleProvenanceErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "wrong arena",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    print(right.at(alice).name);
    return;
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
		{
			name: "inline wrong arena",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    print(right.at(try left.add(allocator, User { name: "alice" })).name);
    return;
}`,
			want: "handle from `left` does not belong to arena `right`",
		},
		{
			name: "returned handle",
			source: `struct UserArena {} struct User { name: []u8 }
fn make() -> !std::arena::Handle<User, UserArena> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    defer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    return alice;
}`,
			want: "handle `alice` cannot outlive its arena",
		},
		{
			name: "returned handle from a local struct's field arena",
			source: `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn make() -> !std::arena::Handle<User, UserArena> {
    let allocator = std::mem::page_allocator();
    var registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    defer registry.deinit(allocator);
    return try registry.users.add(allocator, User { name: "alice" });
}`,
			want: "handle from `registry.users` cannot outlive its arena",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsFieldArenaHandleReturn lets a factory method hand back the
// handle it added: the arena is a field of the borrowed receiver, so it is the
// caller's storage and the handle does not outlive it (§10). A local owner's
// field arena still pins its handles to the frame (the reject cases above).
func TestCheckAcceptsFieldArenaHandleReturn(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: &var Registry) add_direct(
    allocator: Allocator, name: []u8
) -> !std::arena::Handle<User, UserArena> {
    return try self.users.add(allocator, User { name: name });
}
fn (self: &var Registry) add_bound(
    allocator: Allocator, name: []u8
) -> !std::arena::Handle<User, UserArena> {
    let handle = try self.users.add(allocator, User { name: name });
    return handle;
}
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn main() {
    let allocator = std::mem::page_allocator();
    var registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    errdefer registry.deinit(allocator);
    let alice = registry.add_direct(allocator, "alice");
    let bob = registry.add_bound(allocator, "bob");
    print(registry.users.at(alice).name);
    print(registry.users.at(bob).name);
    registry.deinit(allocator);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckAcceptsArenaParamHandle keeps the provenance rule lenient where
// nothing is known: an arena that arrived as a parameter cannot name where it
// was made, so a handle parameter is accepted and the signature carries the
// contract (ADR-0098). Known confusions between local arenas still stop.
func TestCheckAcceptsArenaParamHandle(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    show(&users, alice);
    users.deinit(allocator);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckRejectsArenaParamHandleMismatch checks the caller re-derives the
// arena/handle pairing from a helper's signature (ADR-0098): when a borrowed
// `Arena<T>` and a by-value `Handle<T>` pair up exactly once and both call
// arguments have known origins, a confusion is rejected at the call site.
func TestCheckRejectsArenaParamHandleMismatch(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "shared borrow helper",
			source: `struct UserArena {} struct User { name: []u8 }
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    show(&right, alice);
    return;
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
		{
			name: "mutable borrow helper",
			source: `struct UserArena {} struct User { name: []u8 }
fn touch(
    users: &var std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    if users.at_mut(user) |u| {
        u.name = "bob";
    }
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    var right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    touch(right, alice);
    return;
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsArenaParamHandleMismatchInlineAdd checks the derived pairing
// also fires when the handle is the add expression itself, with no binding for
// the origin to be read off.
func TestCheckRejectsArenaParamHandleMismatchInlineAdd(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "inline add argument",
			source: `struct UserArena {} struct User { name: []u8 }
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    show(&right, try left.add(allocator, User { name: "alice" }));
    return;
}`,
			want: "handle from `left` does not belong to arena `right`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsArenaParamHandleMismatchOtherForms checks the derived
// pairing also fires through generic instantiation and impl method calls.
func TestCheckRejectsArenaParamHandleMismatchOtherForms(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "generic helper",
			source: `struct TArena {} struct UserArena {} struct User { name: []u8 }
fn show<T>(items: &std::arena::Arena<T, TArena>, item: std::arena::Handle<T, TArena>) -> void {
    print(items.at(item).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    show<User>(&right, alice);
    return;
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
		{
			name: "method helper",
			source: `struct UserArena {} struct User { name: []u8 }
struct Viewer { id: i64 }
fn (self: &Viewer) show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    defer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    defer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    let viewer = Viewer { id: 1 };
    viewer.show(&right, alice);
    return;
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsArenaParamHandleUnknowns keeps the derived pairing lenient
// where a side is unknown or the signature is ambiguous: the contract chains
// through forwarding helpers, and a signature with two arenas of the same T
// derives nothing.
const arenaForwardedContractSource = `struct UserArena {} struct User { name: []u8 }
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn outer(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    show(users, user);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    outer(&users, alice);
    users.deinit(allocator);
    return;
}`

const arenaTwoArenasSource = `struct UserArena {} struct User { name: []u8 }
fn pick(
    a: &std::arena::Arena<User, UserArena>,
    b: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>,
) -> void {
    print(a.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User, UserArena>(allocator);
    errdefer left.deinit(allocator);
    let right = std::arena::new<User, UserArena>(allocator);
    errdefer right.deinit(allocator);
    let alice = try left.add(allocator, User { name: "alice" });
    pick(&left, &right, alice);
    left.deinit(allocator);
    right.deinit(allocator);
    return;
}`

const arenaFieldArenaSource = `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    errdefer registry.deinit(allocator);
    let alice = try registry.users.add(allocator, User { name: "alice" });
    show(&registry.users, alice);
    registry.deinit(allocator);
    return;
}`

// TestCheckAcceptsArenaParamHandleUnknowns checks the derived arena/handle
// pairing against the shapes whose provenance is not known at the call.
func TestCheckAcceptsArenaParamHandleUnknowns(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "forwarded arena chains the contract", source: arenaForwardedContractSource},
		{name: "two arenas of the same T derive nothing", source: arenaTwoArenasSource},
		{name: "matching field arena passes", source: arenaFieldArenaSource},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkSource(tt.source); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCheckRejectsArenaFieldParamHandleMismatch checks the derived pairing
// also sees an arena lent from one direct owner field.
func TestCheckRejectsArenaFieldParamHandleMismatch(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn show(
    users: &std::arena::Arena<User, UserArena>,
    user: std::arena::Handle<User, UserArena>
) -> void {
    print(users.at(user).name);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    defer registry.deinit(allocator);
    let stray = std::arena::new<User, UserArena>(allocator);
    defer stray.deinit(allocator);
    let alice = try stray.add(allocator, User { name: "alice" });
    show(&registry.users, alice);
    return;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "handle `alice` does not belong to arena `registry.users`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsArenaHandleMoveErrors checks arena move diagnostics.
func TestCheckRejectsArenaHandleMoveErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "move after arena add",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    defer users.deinit(allocator);
    let user = User { name: "alice" };
    let alice = try users.add(allocator, move user);
    print(user.name);
    print(users.at(alice).name);
    return;
}`,
			want: "moved value `user` was used",
		},
		{
			name: "move field from arena borrow",
			source: `struct BoxArena {} struct User { name: []u8 }
struct Box { user: User }
fn take(user: User) { print(user.name); }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let boxes = std::arena::new<Box, BoxArena>(allocator);
    defer boxes.deinit(allocator);
    let h = try boxes.add(allocator, Box { user: User { name: "alice" } });
    take(boxes.at(h).user);
    return;
}`,
			want: "Arena.at returns &T, so its fields cannot be moved",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsArenaUseAfterDeinit checks arenas cannot be reused after cleanup.
func TestCheckRejectsArenaUseAfterDeinit(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "double deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    users.deinit(allocator);
    users.deinit(allocator);
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "add after deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    users.deinit(allocator);
    try users.add(allocator, User { name: "alice" });
    return;
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "get after deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    users.deinit(allocator);
    print(users.at(alice).name);
    return;
			}`,
			want: "arena `users` was deinitialized",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsArenaDeinitWithLiveReferences checks cleanup reference rules.
func TestCheckRejectsArenaDeinitWithLiveReferences(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "deinit while borrowed",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    let borrowed = &users;
    users.deinit(allocator);
    print(borrowed);
}`,
			want: "`Arena.deinit` cannot run while arena is borrowed",
		},
		{
			name: "handle after deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let alice = try users.add(allocator, User { name: "alice" });
    users.deinit(allocator);
    print(alice);
    return;
}`,
			want: "handle `alice` cannot be used after arena `users` deinit",
		},
		{
			name: "borrow after deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    users.deinit(allocator);
    let borrowed = &users;
    print(borrowed);
}`,
			want: "arena `users` was deinitialized",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsInvalidDeferredCleanup checks the supported defer shape.
func TestCheckRejectsInvalidDeferredCleanup(t *testing.T) {
	source := `fn main() {
    defer print("not cleanup");
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "defer expects cleanup method call") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsDeferredCleanupExitErrors checks cleanup ownership at exit.
func TestCheckRejectsDeferredCleanupExitErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "after explicit deinit",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    users.deinit(allocator);
    defer users.deinit(allocator);
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "moved before cleanup",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    defer users.deinit(allocator);
    let moved = move users;
    print(moved);
}`,
			want: "moved value `users` was used",
		},
		{
			name: "borrowed at cleanup",
			source: `struct UserArena {} struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    let borrowed = &users;
    defer users.deinit(allocator);
    while false { print(borrowed); }
}`,
			want: "`Arena.deinit` cannot run while arena is borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsErrDeferReturnedOwner checks errdefer does not block the
// success-path move of the owner it guards.
func TestCheckAcceptsErrDeferReturnedOwner(t *testing.T) {
	source := `struct UserArena {} error BuildError { Boom }
struct User { name: []u8 }
fn build() -> !std::arena::Arena<User, UserArena> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    return move users;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckRejectsInvalidErrDeferCleanup keeps errdefer to cleanup method calls.
func TestCheckRejectsInvalidErrDeferCleanup(t *testing.T) {
	source := `fn main() {
    errdefer print("not cleanup");
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "errdefer expects cleanup method call") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsErrDeferReceiverInvalidOnErrorPath checks the errdefer
// receiver must stay valid at every error path that can run the cleanup.
func TestCheckRejectsErrDeferReceiverInvalidOnErrorPath(t *testing.T) {
	runErrorCases(t, []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "borrowed on error path",
			source: `struct UserArena {} struct User { name: []u8 }
fn step() -> !void { return; }
fn build() -> !std::arena::Arena<User, UserArena> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    let borrowed = &users;
    try step();
    print(borrowed);
    return users;
}`,
			want: "errdefer cleanup receiver `users` is borrowed on an error path",
		},
	})
}

// TestCheckErrDeferRetiresAtExplicitDeinit checks releasing the receiver early
// and then returning an error is accepted (ADR-0114). The cleanup already ran
// in source; running the errdefer as well would release the same value twice.
func TestCheckErrDeferRetiresAtExplicitDeinit(t *testing.T) {
	source := `struct UserArena {} error BuildError { Boom }
struct User { name: []u8 }
fn build() -> !std::arena::Arena<User, UserArena> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User, UserArena>(allocator);
    errdefer users.deinit(allocator);
    users.deinit(allocator);
    return BuildError::Boom;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckErrDeferRetiresAtMove checks a moved receiver retires its cleanup
// instead of failing the error paths that follow (ADR-0114). The move hands the
// obligation to a new owner, and that owner carries its own cleanup.
func TestCheckErrDeferRetiresAtMove(t *testing.T) {
	source := `fn build(allocator: Allocator) -> !std::array::Array<std::string::String> {
    let parent = std::array::new<std::string::String>(allocator);
    errdefer parent.deinit(allocator);
    let child = std::string::new(allocator);
    errdefer child.deinit(allocator);
    try child.append_byte(allocator, cast<u8>(97));
    try parent.append(allocator, move child);
    try parent.reserve(allocator, 1);
    return move parent;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckErrDeferRetirementIsRecorded checks the retired receivers reach the
// error exits from the move onward, since lowering reads them to drop the
// cleanups it would otherwise emit there. The try that performs the move
// retires it too: by the time that call can fail, the callee holds the value.
func TestCheckErrDeferRetirementIsRecorded(t *testing.T) {
	source := `fn build(allocator: Allocator) -> !std::array::Array<std::string::String> {
    let parent = std::array::new<std::string::String>(allocator);
    errdefer parent.deinit(allocator);
    let child = std::string::new(allocator);
    errdefer child.deinit(allocator);
    try child.append_byte(allocator, cast<u8>(97));
    try parent.append(allocator, move child);
    try parent.reserve(allocator, 1);
    return move parent;
}`
	program, err := project.LoadSource("", withStdImport(source))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	checker := New()
	if err := checker.Check(program); err != nil {
		t.Fatalf("check: %v", err)
	}
	retired := retiredErrDefersOf(t, program, checker.Result(), "build")
	want := [][]string{nil, {"child"}, {"child"}}
	if len(retired) != len(want) {
		t.Fatalf("got %d try exits, want %d", len(retired), len(want))
	}
	for index, names := range want {
		if strings.Join(retired[index], ",") != strings.Join(names, ",") {
			t.Fatalf("try %d retired %v, want %v", index, retired[index], names)
		}
	}
}

// TestCheckStringViewThroughFieldPath pins the view a `String` field lends.
// ADR-0111 made every borrow position take a field path; the view initializer
// tracks its borrow on the root under that path, so a disjoint field stays
// writable while an overlapping one does not.
func TestCheckStringViewThroughFieldPath(t *testing.T) {
	source := `struct Pair { pub left: std::string::String, pub right: std::string::String }
fn (self: Pair) deinit(allocator: Allocator) -> void {
    self.left.deinit(allocator);
    self.right.deinit(allocator);
    return;
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var pair = Pair { left: std::string::new(allocator), right: std::string::new(allocator) };
    defer pair.deinit(allocator);
    let seen = pair.left.as_bytes();
    try pair.right.append_bytes(allocator, "other");
    print(seen);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckRejectsStringViewFieldPathConflict keeps the view exclusive over the
// path it names: growing the string can move its buffer, which would leave the
// view pointing at the old one.
func TestCheckRejectsStringViewFieldPathConflict(t *testing.T) {
	source := `struct Holder { pub name: std::string::String }
fn (self: Holder) deinit(allocator: Allocator) -> void {
    self.name.deinit(allocator);
    return;
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var holder = Holder { name: std::string::new(allocator) };
    defer holder.deinit(allocator);
    let seen = holder.name.as_bytes();
    try holder.name.append_bytes(allocator, "grow");
    print(seen);
    return;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "cannot run while string is borrowed"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q, want substring %q", err.Error(), want)
	}
}

// TestCheckRejectsCleanupReceiverOverwrite pins one name to one value to one
// cleanup. A registered cleanup releases the value that was live when it was
// written, so assigning over the name would leave it holding something the name
// no longer means.
func TestCheckRejectsCleanupReceiverOverwrite(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "errdefer receiver after its value moved out",
			source: `fn build(allocator: Allocator) -> !std::array::Array<std::string::String> {
    var parent = std::array::new<std::string::String>(allocator);
    errdefer parent.deinit(allocator);
    var name = std::string::new(allocator);
    errdefer name.deinit(allocator);
    try name.append_byte(allocator, cast<u8>(97));
    try parent.append(allocator, move name);
    name = std::string::new(allocator);
    try parent.append(allocator, name);
    return parent;
}`,
			want: "`errdefer` cleanup receiver `name` cannot be assigned over",
		},
		{
			name: "defer receiver",
			source: `fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var name = std::string::new(allocator);
    defer name.deinit(allocator);
    try name.append_byte(allocator, cast<u8>(97));
    name = std::string::new(allocator);
    print(name.len());
    return;
}`,
			want: "`defer` cleanup receiver `name` cannot be assigned over",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAllowsSecondOwnerUnderItsOwnName keeps the builder writable: a new
// owner takes a new name and registers its own cleanup.
func TestCheckAllowsSecondOwnerUnderItsOwnName(t *testing.T) {
	source := `fn build(allocator: Allocator) -> !std::array::Array<std::string::String> {
    var parent = std::array::new<std::string::String>(allocator);
    errdefer parent.deinit(allocator);
    var first = std::string::new(allocator);
    errdefer first.deinit(allocator);
    try first.append_byte(allocator, cast<u8>(97));
    try parent.append(allocator, move first);
    var second = std::string::new(allocator);
    errdefer second.deinit(allocator);
    try second.append_byte(allocator, cast<u8>(98));
    try parent.reserve(allocator, 1);
    try parent.append(allocator, move second);
    return move parent;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// retiredErrDefersOf lists, in source order, what each try in the named
// function retires.
func retiredErrDefersOf(
	t *testing.T,
	program *ast.Program,
	result Result,
	name string,
) [][]string {
	t.Helper()
	var retired [][]string
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || fn.Name != name {
			continue
		}
		for _, stmt := range fn.Body.Statements {
			expr, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			if try, ok := expr.Expr.(*ast.TryExpr); ok {
				retired = append(retired, result.RetiredErrDefersForTry(try))
			}
		}
	}
	return retired
}

// TestCheckBranchMoveMarksOuterValueMoved checks possible moves escape branches.
func TestCheckBranchMoveMarksOuterValueMoved(t *testing.T) {
	source := `struct Name { value: []u8 }
fn take(name: Name) { print(name.value); }
fn main() {
    let name = Name { value: "alice" };
    if true { take(move name); }
    print(name.value);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "moved value `name` was used") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckControlExpressionMoveMarksOuterValueMoved checks expression branch moves.
func TestCheckControlExpressionMoveMarksOuterValueMoved(t *testing.T) {
	source := `struct Name { value: []u8 }
fn pick(left: Name, right: Name) -> Name {
    let chosen = if true { move left } else { move right };
    return move chosen;
}
fn main() {
    let left = Name { value: "left" };
    let right = Name { value: "right" };
    let chosen = pick(move left, move right);
    print(left.value);
    print(chosen.value);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "moved value `left` was used") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckUnsafeDoesNotDisableMoveAndBorrowRules checks unsafe keeps safe rules.
func TestCheckUnsafeDoesNotDisableMoveAndBorrowRules(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "moved value in unsafe block",
			source: `struct Name { value: []u8 }
fn take(name: Name) { print(name.value); }
fn main() {
    let name = Name { value: "alice" };
    take(move name);
    print(name.value);}`,
			want: "moved value `name` was used",
		},
		{
			name: "borrow escape in requires-unsafe function",
			source: `unsafe fn bad(s: &[]u8) -> []u8 {
    return s;
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow escape in unsafe block",
			source: `fn bad(s: &[]u8) {
    let alias = s;
    print(alias);}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow field in unsafe-adjacent code",
			source: `struct Bad {
    value: &[]u8,
}
fn main() { print(1); }`,
			want: "struct field `Bad.value` cannot store borrow",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckComptimeDoesNotMoveRuntimeValues checks compile-time arguments are read-only.
func TestCheckComptimeDoesNotMoveRuntimeValues(t *testing.T) {
	source := `fn sized<n: i64>() -> i64 { return n ;}
fn main() {
    let name = "alice";
    print(sized<8>());
    print(name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsComptimeRuntimeBorrow checks runtime borrows cannot cross comptime.
func TestCheckRejectsComptimeRuntimeBorrow(t *testing.T) {
	source := `fn bad(s: &[]u8) -> []u8 {
    let alias = comptime s;
    return alias;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "runtime value cannot") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckComptimeRejectsRuntimeBoundary checks runtime locals cannot cross comptime.
func TestCheckComptimeRejectsRuntimeBoundary(t *testing.T) {
	source := `fn main() {
    let name = "alice";
    let alias = comptime name;
    print(alias);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "runtime value cannot cross comptime boundary") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckMinimalGenericInstantiation checks ownership after explicit type application.
func TestCheckMinimalGenericInstantiation(t *testing.T) {
	source := `struct Name { value: []u8 }
fn pass<T>(value: T) -> T {
    return move value;
}
fn main() {
    let name = Name { value: "alice" };
    let other = pass<Name>(name);
    print(other.value);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayLenDoesNotMoveNonCopyArray keeps read-only Array methods non-consuming.
func TestCheckArrayLenDoesNotMoveNonCopyArray(t *testing.T) {
	source := `struct Name { value: []u8 }
fn main(allocator: Allocator, values: std::array::Array<Name>) {
    let count = values.len();
    print(count);
    values.deinit(allocator);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayCloneDoesNotMoveCopyArray keeps clone as a read of the source
// while tracking the returned Array as a separate owner.
func TestCheckArrayCloneDoesNotMoveCopyArray(t *testing.T) {
	source := `fn copy(values: std::array::Array<i64>, allocator: Allocator) -> !void {
    defer values.deinit(allocator);
    let copied = try values.clone(allocator);
    defer copied.deinit(allocator);
    print(values.len());
    return;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayCloneRejectsOwnerElements keeps element-specific deep copy
// outside the generic Array API.
func TestCheckArrayCloneRejectsOwnerElements(t *testing.T) {
	source := `fn copy(
    values: std::array::Array<std::string::String>,
    allocator: Allocator,
) -> !void {
    defer values.deinit(allocator);
    let copied = try values.clone(allocator);
    defer copied.deinit(allocator);
    return;
}
fn main() {}`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected copy-element error")
	}
	if !strings.Contains(err.Error(), "`Array.clone` requires copy element") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckArrayPopMovesNonCopyElement keeps resource arrays usable without copy reads.
func TestCheckArrayPopMovesNonCopyElement(t *testing.T) {
	source := `struct Name { value: []u8 }
fn check(allocator: Allocator, values: std::array::Array<Name>) -> !void {
    defer values.deinit(allocator);
    if values.pop() |value| {
        print(value.value);
    }
    return;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayPopOrPanicMovesNonCopyElement combines pop moves with explicit trapping.
func TestCheckArrayPopOrPanicMovesNonCopyElement(t *testing.T) {
	source := `struct Parsed { values: std::array::Array<i64> }
fn (self: Parsed) deinit(allocator: Allocator) -> void {
    self.values.deinit(allocator);
}
fn check(allocator: Allocator, values: std::array::Array<Parsed>) -> void {
    let value = values.pop_or_panic();
    value.deinit(allocator);
    print(values.len());
    values.deinit(allocator);
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayPopOrPanicRejectsActiveElementBorrow keeps mutation alias-safe.
func TestCheckArrayPopOrPanicRejectsActiveElementBorrow(t *testing.T) {
	source := `struct Parsed { values: std::array::Array<i64> }
fn (self: Parsed) deinit(allocator: Allocator) -> void {
    self.values.deinit(allocator);
}
fn observe(value: &Parsed) -> void {}
fn check(allocator: Allocator, values: std::array::Array<Parsed>) -> !void {
    if values.at(0) |first| {
        let value = values.pop_or_panic();
        observe(first);
        value.deinit(allocator);
    }
    values.deinit(allocator);
    return;
}
fn main() {}`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected active borrow error")
	}
	if !strings.Contains(err.Error(), "`Array.pop_or_panic` cannot run while array is borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckArrayAcceptsNestedResourceElement checks Array owns element cleanup.
func TestCheckArrayAcceptsNestedResourceElement(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
struct Parsed {
    users: std::arena::Arena<User, UserArena>,
    ids: std::array::Array<i64>,
}
fn (self: Parsed) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
    self.ids.deinit(allocator);
}
fn check(allocator: Allocator, values: std::array::Array<Parsed>) -> !void {
    defer values.deinit(allocator);
    if values.pop() |item| {
        item.deinit(allocator);
    }
    return;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckImplMethodReturnTypeFeedsGenericCall keeps method result types precise.
func TestCheckImplMethodReturnTypeFeedsGenericCall(t *testing.T) {
	source := `struct Counter { value: i64 }
fn (self: Counter) len() -> i64 {
    return self.value;
}
fn expect_equal<T>(expected: T, actual: T) -> void {
    return;
}
fn main() {
    let counter = Counter { value: 1 };
    expect_equal<i64>(1, counter.len());
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsImplMethodReturnTypeMismatch checks generic calls see method returns.
func TestCheckRejectsImplMethodReturnTypeMismatch(t *testing.T) {
	source := `struct Counter { value: i64 }
fn (self: Counter) label() -> []u8 {
    return "one";
}
fn expect_equal<T>(expected: T, actual: T) -> void {
    return;
}
fn main() {
    let counter = Counter { value: 1 };
    expect_equal<i64>(1, counter.label());
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "arg 2 of `expect_equal` expects i64, got []u8") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsImplMethodArgCount checks method signatures replace unknown fallback.
func TestCheckRejectsImplMethodArgCount(t *testing.T) {
	source := `struct Counter { value: i64 }
fn (self: Counter) add(value: i64) -> i64 {
    return value;
}
fn main() {
    let counter = Counter { value: 1 };
    print(counter.add());
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "`Counter.add` expects 1 args, got 0") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsDirectFieldReceiverMethods checks owner fields can forward storage APIs.
func TestCheckAcceptsDirectFieldReceiverMethods(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: &var Registry) add(allocator: Allocator, user: User) -> !void {
    let handle = try self.users.add(allocator, move user);
    print(self.users.at(handle).name);
    return;
}
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
    return;
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    errdefer registry.deinit(allocator);
    try registry.add(allocator, User { name: "alice" });
    registry.deinit(allocator);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsNestedFieldReceiverMethods checks method receivers reach
// through a nested field path while cleanup still descends one level at a time.
func TestCheckAcceptsNestedFieldReceiverMethods(t *testing.T) {
	source := `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
struct Wrapper { registry: Registry }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn (self: Wrapper) deinit(allocator: Allocator) -> void {
    self.registry.deinit(allocator);
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    let wrapper = Wrapper { registry: move registry };
    defer wrapper.deinit(allocator);
    try wrapper.registry.users.add(allocator, User { name: "alice" });
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsDirectFieldReceiverPolicy keeps field cleanup and paths bounded.
func TestCheckRejectsDirectFieldReceiverPolicy(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "field cleanup outside owner deinit",
			source: `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    registry.users.deinit(allocator);
}`,
			want: "field cleanup `registry.users.deinit` is only allowed inside owner deinit",
		},
		{
			name: "use after field cleanup",
			source: `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
    self.users.deinit(allocator);
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    registry.deinit(allocator);
}`,
			want: "field `self.users` was deinitialized",
		},
		{
			name: "nested field cleanup",
			source: `struct UserArena {} struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User, UserArena> }
struct Wrapper { registry: Registry }
fn (self: Registry) deinit(allocator: Allocator) -> void {
    self.users.deinit(allocator);
}
fn (self: Wrapper) deinit(allocator: Allocator) -> void {
    self.registry.users.deinit(allocator);
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User, UserArena>(allocator) };
    let wrapper = Wrapper { registry: registry };
    wrapper.deinit(allocator);
}`,
			want: "field cleanup `self.registry.users.deinit` is only allowed on one direct field",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsDiscardedOwnerExpression covers the values that reach a
// caller without a binding. Cleanup obligations are tracked per binding, so an
// unbound owner is never tracked at all; `?T` and `E!T` wrappers do not change
// whose obligation it is.
func TestCheckRejectsDiscardedOwnerExpression(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "constructor result",
			source: `fn main() {
    let allocator = std::mem::page_allocator();
    std::string::new(allocator);
}`,
			want: "produces owned `std::string::String` and discards it",
		},
		{
			name: "optional element moved out of its container",
			source: `fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let parent = std::array::new<std::string::String>(allocator);
    defer parent.deinit(allocator);
    let name = std::string::new(allocator);
    errdefer name.deinit(allocator);
    try name.append_byte(allocator, cast<u8>(97));
    try parent.append(allocator, move name);
    parent.pop();
    return;
}`,
			want: "produces owned `?std::string::String` and discards it",
		},
		{
			name: "error union unwrapped by try",
			source: `fn make(allocator: Allocator) -> !std::string::String {
    let name = std::string::new(allocator);
    errdefer name.deinit(allocator);
    try name.append_byte(allocator, cast<u8>(97));
    return move name;
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    try make(allocator);
    return;
}`,
			want: "produces owned `std::string::String` and discards it",
		},
		{
			name: "error union left wrapped",
			source: `fn main() -> !void {
    let allocator = std::mem::page_allocator();
    std::mem::box<i64>(allocator, 1);
    return;
}`,
			want: "produces owned `std::mem::Error!std::mem::Box<i64>` and discards it",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckDiscardedOwnerPointsAtTheCall pins the callee span through name
// qualification: qualifying rewrites the callee node, and a rewrite that drops
// the span leaves this diagnostic with nowhere to point.
func TestCheckDiscardedOwnerPointsAtTheCall(t *testing.T) {
	source := `import std;
fn main() {
    let allocator = std::mem::page_allocator();
    std::string::new(allocator);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	diagnostic, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("got %T, want *diagnostic.Diagnostic", err)
	}
	if got := diagnostic.SourceSpan().Start.Line; got != 4 {
		t.Fatalf("got line %d, want 4", got)
	}
}

// TestCheckAllowsDiscardedNonOwnerExpression keeps the rule type-directed: a
// value with no cleanup contract may be produced and dropped as before.
func TestCheckAllowsDiscardedNonOwnerExpression(t *testing.T) {
	source := `fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let values = std::array::new<i64>(allocator);
    defer values.deinit(allocator);
    try values.append(allocator, 1);
    values.pop();
    values.len();
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckRejectsGenericMoveCallWithoutTypeArgs keeps generic calls explicit.
func TestCheckRejectsGenericMoveCallWithoutTypeArgs(t *testing.T) {
	source := `fn pass<T>(value: T) -> T {
    return value;
}
fn main() {
    print(pass(1));
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "requires explicit static arguments") {
		t.Fatalf("got %q", err.Error())
	}
}

// runErrorCases checks that each source fails with the expected message.
func runErrorCases(t *testing.T, cases []struct {
	name   string
	source string
	want   string
}) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSource(tt.source)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

// checkSource parses and move-checks a source snippet.
func checkSource(source string) error {
	// std wrappers are what a program calls now that the `std::internal::builtin::`
	// namespace is closed to source outside std, so a checkable program is one
	// with std loaded -- which is what every real invocation does.
	program, err := project.LoadSource("", withStdImport(source))
	if err != nil {
		return err
	}
	return New().Check(program)
}

// withStdImport gives a snippet the root import a file needs to spell full std
// paths. The snippets are about what the checker does with std types, not about
// import lines, and a file that writes `std::mem::page_allocator` has to bring
// the root into scope the same way any other file does.
func withStdImport(source string) string {
	if strings.Contains(source, "import std;") || !writesFullStdPath(source) {
		return source
	}
	return "import std;\n" + source
}

// writesFullStdPath reports whether a snippet spells a std path in code rather
// than only naming one in an import declaration.
func writesFullStdPath(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "import ") &&
			strings.Contains(line, "std::") {
			return true
		}
	}
	return false
}
