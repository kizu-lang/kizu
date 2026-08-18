package ownership

import (
	"strings"
	"testing"

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
    let b = a;
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
    let b = a;
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
    take(name);
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
    take(name);
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
    errdefer values.deinit();
    try values.append(7);
    return values;
}
fn main() -> !void {
    var buf = [512]u8{};
    let scratch = buf.as_mut_bytes();
    let alloc = std::mem::fixed_buffer(scratch);
    var values = try fill(alloc);
    defer values.deinit();
    try values.append(1);
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
fn main() -> !void {
    var values = leak();
    values.deinit();
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
fn main() -> !void {
    let alloc = leak();
    var values = std::array::new<i64>(alloc);
    defer values.deinit();
    try values.append(1);
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
    defer values.deinit();
    let sneak = buf.as_mut_bytes();
    sneak[0] = cast<u8>(1);
    try values.append(1);
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
fn touch(left: &var i64) { print(left); }
fn main() {
    var pair = Pair { left: 1, right: 2 };
    let moved = pair;
    touch(&var pair.left);
    print(moved.left);
}`,
			want: "moved value `pair` was borrowed",
		},
		{
			name: "deinitialized field",
			source: `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn touch(users: &var std::arena::Arena<User>) {
    print(0);
}
fn (self: Registry) deinit() -> void {
    self.users.deinit();
    touch(&var self.users);
    return;
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User>(allocator) };
    registry.deinit();
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
fn fail(text: std::string::String) -> !void {
    let bytes = text.as_bytes();
    print(bytes);
    text.deinit();
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
    unsafe node.* = value;
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
	source := `struct User {
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.at(alice).name);
    users.deinit();
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsDeferredArenaCleanup checks cleanup runs at block exit.
func TestCheckAcceptsDeferredArenaCleanup(t *testing.T) {
	source := `struct User {
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    defer users.deinit();
    let alice = users.add(User { name: "alice" });
    print(users.at(alice).name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArenaAllocatorReadOnly checks arena construction reads allocator capabilities.
func TestCheckArenaAllocatorReadOnly(t *testing.T) {
	source := `struct User {
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User>(allocator);
    let right = std::arena::new<User>(allocator);
    print(left);
    print(right);
    left.deinit();
    right.deinit();
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User>(allocator);
    let right = std::arena::new<User>(allocator);
    let alice = left.add(User { name: "alice" });
    print(right.at(alice).name);
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
		{
			name: "inline wrong arena",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let left = std::arena::new<User>(allocator);
    let right = std::arena::new<User>(allocator);
    print(right.at(left.add(User { name: "alice" })).name);
}`,
			want: "handle from `left` does not belong to arena `right`",
		},
		{
			name: "unknown handle parameter",
			source: `struct User { name: []u8 }
fn show(users: std::arena::Arena<User>, user: std::arena::Handle<User>) {
    print(users.at(user).name);
}`,
			want: "arena `users` has unknown provenance",
		},
		{
			name: "returned handle",
			source: `struct User { name: []u8 }
fn make() -> std::arena::Handle<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let alice = users.add(User { name: "alice" });
    return alice;
}`,
			want: "handle `alice` cannot outlive its arena",
		},
	}
	runErrorCases(t, cases)
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let user = User { name: "alice" };
    let alice = users.add(user);
    print(user.name);
    print(users.at(alice).name);
}`,
			want: "moved value `user` was used",
		},
		{
			name: "move field from arena borrow",
			source: `struct User { name: []u8 }
struct Box { user: User }
fn take(user: User) { print(user.name); }
fn main() {
    let allocator = std::mem::page_allocator();
    let boxes = std::arena::new<Box>(allocator);
    let h = boxes.add(Box { user: User { name: "alice" } });
    take(boxes.at(h).user);
}`,
			want: "arena.at returns a local borrow and its fields cannot be moved",
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    users.deinit();
    users.deinit();
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "add after deinit",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    users.deinit();
    users.add(User { name: "alice" });
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "get after deinit",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let alice = users.add(User { name: "alice" });
    users.deinit();
    print(users.at(alice).name);
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let borrowed = &users;
    users.deinit();
    print(borrowed);
}`,
			want: "`Arena.deinit` cannot run while arena is borrowed",
		},
		{
			name: "handle after deinit",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let alice = users.add(User { name: "alice" });
    users.deinit();
    print(alice);
}`,
			want: "handle `alice` cannot be used after arena `users` deinit",
		},
		{
			name: "borrow after deinit",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    users.deinit();
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    users.deinit();
    defer users.deinit();
}`,
			want: "arena `users` was deinitialized",
		},
		{
			name: "moved before cleanup",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    defer users.deinit();
    let moved = users;
    print(moved);
}`,
			want: "moved value `users` was used",
		},
		{
			name: "borrowed at cleanup",
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    let borrowed = &users;
    defer users.deinit();
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
	source := `error BuildError { Boom }
struct User { name: []u8 }
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    return users;
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
			name: "moved before error path",
			source: `struct User { name: []u8 }
fn step() -> !void { return; }
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    let moved = users;
    try step();
    return moved;
}`,
			want: "errdefer cleanup receiver `users` was moved before an error path",
		},
		{
			name: "deinitialized before error path",
			source: `struct User { name: []u8 }
fn step() -> !void { return; }
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    users.deinit();
    try step();
    return users;
}`,
			want: "errdefer cleanup receiver `users` was deinitialized before an error path",
		},
		{
			name: "borrowed on error path",
			source: `struct User { name: []u8 }
fn step() -> !void { return; }
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    let borrowed = &users;
    try step();
    print(borrowed);
    return users;
}`,
			want: "errdefer cleanup receiver `users` is borrowed on an error path",
		},
		{
			name: "deinitialized before explicit error return",
			source: `error BuildError { Boom }
struct User { name: []u8 }
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    users.deinit();
    return BuildError::Boom;
}`,
			want: "errdefer cleanup receiver `users` was deinitialized before an error path",
		},
	})
}

// TestCheckBranchMoveMarksOuterValueMoved checks possible moves escape branches.
func TestCheckBranchMoveMarksOuterValueMoved(t *testing.T) {
	source := `struct Name { value: []u8 }
fn take(name: Name) { print(name.value); }
fn main() {
    let name = Name { value: "alice" };
    if true { take(name); }
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
    let chosen = if true { left } else { right };
    return chosen;
}
fn main() {
    let left = Name { value: "left" };
    let right = Name { value: "right" };
    let chosen = pick(left, right);
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
    take(name);
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
    return value;
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
fn main(values: std::array::Array<Name>) {
    let count = values.len();
    print(count);
    values.deinit();
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayPopMovesNonCopyElement keeps resource arrays usable without copy reads.
func TestCheckArrayPopMovesNonCopyElement(t *testing.T) {
	source := `struct Name { value: []u8 }
fn check(values: std::array::Array<Name>) -> !void {
    defer values.deinit();
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
fn (self: Parsed) deinit() -> void {
    self.values.deinit();
}
fn check(values: std::array::Array<Parsed>) -> void {
    let value = values.pop_or_panic();
    value.deinit();
    print(values.len());
    values.deinit();
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckArrayPopOrPanicRejectsActiveElementBorrow keeps mutation alias-safe.
func TestCheckArrayPopOrPanicRejectsActiveElementBorrow(t *testing.T) {
	source := `struct Parsed { values: std::array::Array<i64> }
fn (self: Parsed) deinit() -> void {
    self.values.deinit();
}
fn observe(value: &Parsed) -> void {}
fn check(values: std::array::Array<Parsed>) -> !void {
    if values.at(0) |first| {
        let value = values.pop_or_panic();
        observe(first);
        value.deinit();
    }
    values.deinit();
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
	source := `struct User { name: []u8 }
struct Parsed {
    users: std::arena::Arena<User>,
    ids: std::array::Array<i64>,
}
fn (self: Parsed) deinit() -> void {
    self.users.deinit();
    self.ids.deinit();
}
fn check(values: std::array::Array<Parsed>) -> !void {
    defer values.deinit();
    if values.pop() |item| {
        item.deinit();
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
	source := `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn (self: Registry) add(user: User) -> void {
    let handle = self.users.add(user);
    print(self.users.at(handle).name);
    return;
}
fn (self: Registry) deinit() -> void {
    self.users.deinit();
    return;
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User>(allocator) };
    registry.add(User { name: "alice" });
    registry.deinit();
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
			source: `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn (self: Registry) deinit() -> void {
    self.users.deinit();
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User>(allocator) };
    registry.users.deinit();
}`,
			want: "field cleanup `registry.users.deinit` is only allowed inside owner deinit",
		},
		{
			name: "use after field cleanup",
			source: `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn (self: Registry) deinit() -> void {
    self.users.deinit();
    self.users.add(User { name: "alice" });
    return;
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User>(allocator) };
    registry.deinit();
}`,
			want: "field `self.users` was deinitialized",
		},
		{
			name: "nested field receiver",
			source: `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
struct Wrapper { registry: Registry }
fn (self: Registry) deinit() -> void {
    self.users.deinit();
}
fn (self: Wrapper) deinit() -> void {
    self.registry.deinit();
}
fn main() {
    let allocator = std::mem::page_allocator();
    let registry = Registry { users: std::arena::new<User>(allocator) };
    let wrapper = Wrapper { registry: registry };
    wrapper.registry.users.add(User { name: "alice" });
}`,
			want: "field method receiver only supports one direct field",
		},
	}
	runErrorCases(t, cases)
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
