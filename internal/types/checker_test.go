package types

import (
	"errors"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestCheckValidPhase2Programs checks programs that the interpreter can run.
func TestCheckValidPhase2Programs(t *testing.T) {
	cases := []string{
		`fn main() { print("hello, kizu"); }`,
		`fn add(a: i64, b: i64) -> i64 { return a + b ;}
fn main() { print(add(1, 2)); }`,
		`fn main() {
    let name = "alice";
    var age = 30;
    age = age + 1;
    print(name);
    print(age);
}`,
		`fn main() {
    let age = 20;
    if age >= 20 and age < 130 or false { print("adult"); } else { print("minor"); }
    var i = 0;
    while i < 3 { i = i + 1 ;}
}`,
	}
	for _, source := range cases {
		if err := checkSource(source); err != nil {
			t.Fatalf("check failed: %v\nsource:\n%s", err, source)
		}
	}
}

// TestCheckRejectsNameAndCallErrors checks scope and call errors.
func TestCheckRejectsNameAndCallErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "undefined variable",
			source: `fn main() {
    print(name);
}`,
			want: "undefined variable `name`",
		},
		{
			name: "arg count",
			source: `fn add(a: i64, b: i64) -> i64 { return a + b ;}
fn main() { print(add(1)); }`,
			want: "`add` expects 2 args, got 1",
		},
		{
			name: "arg type",
			source: `fn take(a: i64) -> i64 { return a ;}
fn main() { print(take("no")); }`,
			want: "arg 1 of `take` expects i64, got []const u8",
		},
		{
			name: "return type",
			source: `fn bad() -> i64 {
    return "no";
}`,
			want: "return expects i64, got []const u8",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsReturnAndOperatorErrors checks return flow and operators.
func TestCheckRejectsReturnAndOperatorErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "missing return",
			source: `fn bad() -> i64 {
    1;
}`,
			want: "function `bad` must return i64",
		},
		{
			name: "if return path",
			source: `fn bad(ok: bool) -> i64 {
    if ok { return 1 ;}
}`,
			want: "function `bad` must return i64",
		},
		{
			name: "return void literal",
			source: `fn bad() -> !void {
    return void;
}`,
			want: "void is not a value; use `return;`",
		},
		{
			name: "missing error union void return",
			source: `fn bad() -> !void {
    print("done");
}`,
			want: "function `bad` must return !void",
		},
		{
			name: "binary operands",
			source: `fn main() {
    print(1 + "no");
}`,
			want: "operator `+` operands must have same type",
		},
		{
			name: "binary numeric operands",
			source: `fn bad(a: bool, b: bool) -> bool {
    return a + b;
}`,
			want: "operator `+` expects numeric operands",
		},
		{
			name: "logical bool operands",
			source: `fn bad(a: bool) -> bool {
    return a and 1;
}`,
			want: "operator `and` expects bool operands",
		},
		{
			name: "no numeric promotion",
			source: `fn bad(a: i32, b: u32) -> i32 {
    return a + b;
}`,
			want: "operator `+` operands must have same type",
		},
	}
	runErrorCases(t, cases)
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

// TestCheckAcceptsDeclaredLowLevelTypes checks reserved scalar types in signatures.
func TestCheckAcceptsDeclaredLowLevelTypes(t *testing.T) {
	source := `fn a(x: i8) -> i8 { return x ;}
fn b(x: i16) -> i16 { return x ;}
fn c(x: i32) -> i32 { return x ;}
fn d(x: i64) -> i64 { return x ;}
fn e(x: u8) -> u8 { return x ;}
fn f(x: u16) -> u16 { return x ;}
fn g(x: u32) -> u32 { return x ;}
fn h(x: u64) -> u64 { return x ;}
fn i(x: usize) -> usize { return x ;}
fn j(x: isize) -> isize { return x ;}
fn k(x: f32) -> f32 { return x ;}
fn l(x: f64) -> f64 { return x ;}
fn m(x: i32, y: i32) -> i32 { return x + y ;}
fn n(x: f64, y: f64) -> bool { return x <= y ;}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsMapTypeDecl checks v0.2 two-argument Map type spelling.
func TestCheckAcceptsMapTypeDecl(t *testing.T) {
	source := `fn use_table(table: std::map::Map<[]const u8, i64>) -> void {
    return;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsMapTypeErrors checks explicit Map arity and key constraints.
func TestCheckRejectsMapTypeErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "wrong arity",
			source: `fn use_table(table: std::map::Map<i64>) -> void {
    return;
}
fn main() {}`,
			want: "std::map::Map expects 2 type arguments",
		},
		{
			name: "wrong key",
			source: `fn use_table(table: std::map::Map<i64, i64>) -> void {
    return;
}
fn main() {}`,
			want: "std::map::Map key type must be []const u8 in v0.2",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsStructDeclarations checks Phase 5 struct declarations.
func TestCheckAcceptsStructDeclarations(t *testing.T) {
	source := `struct User {
    name: []const u8,
    age: i64,
}
fn take(user: User) {}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsBorrowReturnProvenance checks borrowed slice return syntax.
func TestCheckAcceptsBorrowReturnProvenance(t *testing.T) {
	source := `fn first(bytes: []const u8) -> []const u8 borrows bytes {
    return bytes[0..1];
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsBorrowProvenanceReturns checks shared and mutable borrow returns.
func TestCheckAcceptsBorrowProvenanceReturns(t *testing.T) {
	source := `fn shared(value: &i64) -> &i64 borrows value {
    return value;
}
fn mutable(value: &mut i64) -> &mut i64 borrows value {
    return value;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsBorrowProvenanceDeclarationErrors keeps borrowed returns explicit.
func TestCheckRejectsBorrowProvenanceDeclarationErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "borrow return needs source",
			source: `fn bad(value: &i64) -> &i64 { return value; }`,
			want:   "borrow return requires `borrows <source>`",
		},
		{
			name:   "borrow field rejected",
			source: `struct View { bytes: &[]const u8 } fn main() {}`,
			want:   "borrow field `View.bytes` cannot store borrow",
		},
		{
			name:   "explicit lifetime parameter rejected",
			source: `fn unused<'a>() -> void { return; }`,
			want:   "explicit lifetime parameters are not supported",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsBorrowProvenanceEscapeErrors rejects untied return provenance.
func TestCheckRejectsBorrowProvenanceEscapeErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "return source mismatch",
			source: `fn bad(left: []const u8, right: []const u8) -> []const u8 borrows left {
    return right;
}`,
			want: "return borrows `left` but returned value is not tied to that source",
		},
		{
			name:   "explicit lifetime type rejected",
			source: `fn bad(bytes: []'a const u8) -> void { return; }`,
			want:   "explicit lifetime syntax is not supported",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsPublicAPIWithPublicTypes checks public boundary declarations.
func TestCheckAcceptsPublicAPIWithPublicTypes(t *testing.T) {
	source := `pub enum TokenKind {
    Ident,
}
pub struct Token {
    pub kind: TokenKind,
    start: i64,
}
pub fn take(token: Token) -> Token {
    return token;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsPrivateTypesInPublicAPI checks visibility leak errors.
func TestCheckRejectsPrivateTypesInPublicAPI(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "public function parameter",
			source: `struct Secret {}
pub fn leak(secret: Secret) -> void {
    return;
}
fn main() {}`,
			want: "public function `leak` parameter exposes private type `Secret`",
		},
		{
			name: "public field",
			source: `struct Secret {}
pub struct Token {
    pub secret: Secret,
}
fn main() {}`,
			want: "public field `Token.secret` exposes private type `Secret`",
		},
		{
			name: "public union payload",
			source: `struct Secret {}
pub union Result {
    Ok(Secret),
}
fn main() {}`,
			want: "public union variant `Result::Ok` exposes private type `Secret`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsEnumDeclarations checks Zig/C-style tag enum values.
func TestCheckAcceptsEnumDeclarations(t *testing.T) {
	source := `enum Color {
    Red,
    Green,
    Blue,
}
fn take(color: Color) -> Color { return color ;}
fn main() {
    let red = Color::Red;
    print(take(red));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsEnumErrors checks duplicate and unknown enum tags.
func TestCheckRejectsEnumErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "duplicate enum",
			source: `enum Color { Red }
enum Color { Green }
fn main() {}`,
			want: "duplicate enum `Color`",
		},
		{
			name: "duplicate tag",
			source: `enum Color { Red, Red }
fn main() {}`,
			want: "duplicate enum tag `Color::Red`",
		},
		{
			name: "unknown tag",
			source: `enum Color { Red }
fn main() { print(Color::Blue); }`,
			want: "unknown enum tag `Color::Blue`",
		},
		{
			name:   "unknown enum namespace",
			source: `fn main() { print(Color::Red); }`,
			want:   "unknown namespace `Color`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsEnumMatch checks exhaustive simple enum match statements.
func TestCheckAcceptsEnumMatch(t *testing.T) {
	source := `enum Color {
    Red,
    Green,
    Blue,
}
fn main() {
    let color = Color::Green;
    match color {
        Red => print("red");,
        Green => print("green");,
        Blue => print("blue");,
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsMatchWildcard checks fallback arms preserve exhaustiveness.
func TestCheckAcceptsMatchWildcard(t *testing.T) {
	source := `enum Color { Red, Green, Blue }
union Shape { Point, Circle(i64), Label([]const u8), }
fn describe(shape: &Shape) -> void {
    match shape {
        Circle(radius) => print(radius);,
        _ => print("other");,
    }
}
fn main() -> void {
    let color = Color::Blue;
    let name = match color { Red => "red", _ => "other" };
    print(name);
    describe(Shape::Label("tag"));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsControlExpressions checks value-producing if and match forms.
func TestCheckAcceptsControlExpressions(t *testing.T) {
	source := `enum Color { Red, Green }
fn main() -> void {
    let color = Color::Green
    let value = if true { 1 } else { 2 }
    let name = match color { Red => "red", Green => "green" }
    print(value)
    print(name)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsSemicolonTailControlExpressions keeps block values explicit.
func TestCheckRejectsSemicolonTailControlExpressions(t *testing.T) {
	source := `fn main() -> void {
    let value = if true { 1; } else { 2; }
    print(value);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "expression block must end with a value") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsTaggedUnionMatch checks tagged union constructors and payload matches.
func TestCheckAcceptsTaggedUnionMatch(t *testing.T) {
	source := `union Shape {
    Point,
    Circle(i64),
    Label([]const u8),
}
fn describe(shape: &Shape) -> void {
    match shape {
        Point => print("point");,
        Circle(radius) => print(radius);,
        Label(text) => print(text);,
    }
}
fn main() {
    describe(Shape::Circle(10));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsMutableBorrowTypeErrors checks &mut requires mutable locals.
func TestCheckRejectsMutableBorrowTypeErrors(t *testing.T) {
	source := `struct User { name: []const u8 }
fn update(user: &mut User) -> void { print(user.name); }
fn main() {
    let user = User { name: "alice" };
    update(user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "&mut argument `user` must be mutable") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsMutableBorrowForwarding checks call-scoped &mut reborrows.
func TestCheckAcceptsMutableBorrowForwarding(t *testing.T) {
	source := `struct User { name: []const u8 }
fn rename(user: &mut User) -> void { user.name = "bob" ;}
fn outer(user: &mut User) -> void {
    rename(user);
    user.name = "carol";
}
fn main() -> void {
    var user = User { name: "alice" };
    outer(user);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsSharedBorrowForwarding rejects shared-to-mutable reborrows.
func TestCheckRejectsSharedBorrowForwarding(t *testing.T) {
	source := `struct User { name: []const u8 }
fn rename(user: &mut User) -> void { user.name = "bob" ;}
fn outer(user: &User) -> void {
    rename(user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "&mut argument `user` must be mutable") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckFieldAndDerefAssignment validates mutable assignment targets.
func TestCheckFieldAndDerefAssignment(t *testing.T) {
	source := `struct User { name: []const u8 }
fn rename(user: &mut User) -> void { user.name = "bob" ;}
fn main() -> void {
    var user = User { name: "alice" };
    user.name = "carol";
    rename(user);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsInvalidFieldAssignment checks immutable and shared-borrow writes.
func TestCheckRejectsInvalidFieldAssignment(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "immutable field",
			source: `struct User { name: []const u8 }
fn main() -> void {
    let user = User { name: "alice" };
    user.name = "bob";
}`,
			want: "cannot assign field of immutable binding `user`",
		},
		{
			name: "shared borrow field",
			source: `struct User { name: []const u8 }
fn rename(user: &User) -> void { user.name = "bob" ;}`,
			want: "cannot assign field through shared borrow `user`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsTaggedUnionErrors checks tagged union diagnostics.
func TestCheckRejectsTaggedUnionErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "constructor type",
			source: `union Shape { Circle(i64), }
fn main() {
    let shape = Shape::Circle("large");
    print(shape);
}`,
			want: "union variant `Shape::Circle` expects i64, got []const u8",
		},
		{
			name: "exhaustiveness",
			source: `union Shape { Point, Circle(i64), }
fn main() {
    let shape = Shape::Point;
    match shape { Point => print("point");, }
}`,
			want: "match on `Shape` is not exhaustive",
		},
		{
			name: "payload on empty variant",
			source: `union Shape { Point }
fn main() {
    let shape = Shape::Point;
    match shape { Point(x) => print(x);, }
}`,
			want: "union variant `Shape::Point` has no payload",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsLoopControlErrors checks invalid loop control flow.
func TestCheckRejectsLoopControlErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "break outside loop",
			source: `fn main() -> void {
    break;
}`,
			want: "`break` used outside loop",
		},
		{
			name: "continue outside loop",
			source: `fn main() -> void {
    continue;
}`,
			want: "`continue` used outside loop",
		},
		{
			name: "unknown label",
			source: `fn main() -> void {
    while true { break :missing; }
}`,
			want: "unknown loop label `missing`",
		},
		{
			name: "for bounds",
			source: `fn main() -> void {
    for true..3 |i| { print(i); }
}`,
			want: "for range expects i64 bounds",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsEnumMatchErrors checks simple enum match diagnostics.
func TestCheckRejectsEnumMatchErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "non enum",
			source: `fn main() {
    match 1 { Red => print("red");, }
}`,
			want: "match expects enum or union, got i64",
		},
		{
			name: "unknown tag",
			source: `enum Color { Red, Green }
fn main() {
    let color = Color::Red;
    match color { Red => print("red");, Blue => print("blue");, }
}`,
			want: "unknown match tag `Color::Blue`",
		},
		{
			name: "duplicate tag",
			source: `enum Color { Red, Green }
fn main() {
    let color = Color::Red;
    match color { Red => print("red");, Red => print("again");, Green => print("green");, }
}`,
			want: "duplicate match tag `Color::Red`",
		},
		{
			name: "not exhaustive",
			source: `enum Color { Red, Green }
fn main() {
    let color = Color::Red;
    match color { Red => print("red");, }
}`,
			want: "match on `Color` is not exhaustive",
		},
		{
			name: "wildcard before tag",
			source: `enum Color { Red, Green }
fn main() {
    let color = Color::Red;
    match color { _ => print("other");, Red => print("red");, }
}`,
			want: "wildcard match arm must be last",
		},
		{
			name: "wildcard payload binding",
			source: `union Shape { Point, Circle(i64), }
fn main() {
    let shape = Shape::Circle(1);
    match shape { _(value) => print(value);, }
}`,
			want: "wildcard match arm cannot bind payload",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsArenaHandle checks Phase 6 arena and handle types.
func TestCheckAcceptsArenaHandle(t *testing.T) {
	source := `struct User {
    name: []const u8,
}
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
    users.deinit();
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsDeferredArenaCleanup checks block-exit cleanup registration.
func TestCheckAcceptsDeferredArenaCleanup(t *testing.T) {
	source := `struct User {
    name: []const u8,
}
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    defer users.deinit();
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsInvalidDeferredCleanup checks the first supported defer form.
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

// TestCheckRejectsArenaDeinitErrors checks explicit arena cleanup syntax.
func TestCheckRejectsArenaDeinitErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "arg",
			source: `struct User { name: []const u8 }
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    users.deinit(1);
}`,
			want: "`arena.deinit` expects 0 args",
		},
		{
			name: "field receiver",
			source: `struct User { name: []const u8 }
struct Registry { users: arena<User> }
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    let registry = Registry { users: users };
    registry.users.deinit();
}`,
			want: "field cleanup `registry.users.deinit` is only allowed inside owner deinit",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsOwnerFieldCleanup allows direct field cleanup inside owner deinit.
func TestCheckAcceptsOwnerFieldCleanup(t *testing.T) {
	source := `struct User { name: []const u8 }
struct Registry { users: arena<User> }
impl Registry {
    fn deinit(self: Registry) -> void {
        self.users.deinit();
        return;
    }
}
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    let registry = Registry { users: users };
    registry.deinit();
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsUnsafePointerOperations checks raw pointer ops inside unsafe.
func TestCheckAcceptsUnsafePointerOperations(t *testing.T) {
	source := `extern "c" fn source() -> ptr<i64>
fn main() {
    unsafe {
        let p = source();
        ptr_write(p, 1);
        print(ptr_read(p));
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsRawPointerDerefSyntax checks explicit unsafe pointer dereference.
func TestCheckAcceptsRawPointerDerefSyntax(t *testing.T) {
	source := `struct Node { tag: i64, name: []const u8 }
fn read_tag(node: ptr<const Node>) -> i64 {
    unsafe {
        return node.*.tag;
    }
}
fn write_tag(node: ptr<Node>, tag: i64) -> void {
    unsafe {
        node.*.tag = tag;
        return;
    }
}
fn replace(node: ptr<Node>, value: Node) -> void {
    unsafe {
        node.* = value;
        return;
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsPointerTypes checks non-null and nullable raw pointer types.
func TestCheckAcceptsPointerTypes(t *testing.T) {
	source := `extern "c" fn read_const(p: ptr<const u8>) -> u8
extern "c" fn maybe_data() -> ?ptr<const u8>
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsComptime checks Phase 13 compile-time values and branch selection.
func TestCheckAcceptsComptime(t *testing.T) {
	source := `fn sized(comptime n: i64) -> i64 {
    return n;
}
fn main() {
    let size = comptime 4 * 1024;
    comptime if 1 + 1 == 2 {
        print(sized(comptime 8));
    } else {
        print("not checked");
    }
    print(size);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsComptimeErrors checks readable Phase 13 diagnostics.
func TestCheckRejectsComptimeErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "runtime value",
			source: `fn main() {
    let x = 1;
    let y = comptime x + 1;
    print(y);
}`,
			want: "comptime error: runtime value cannot be used",
		},
		{
			name: "division by zero",
			source: `fn main() {
    let x = comptime 1 / 0;
    print(x);
}`,
			want: "comptime error: division by zero",
		},
		{
			name: "comptime parameter",
			source: `fn sized(comptime n: i64) -> i64 { return n ;}
fn main() {
    let x = 8;
    print(sized(x));
}`,
			want: "comptime error: runtime value cannot be used",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsExplicitNumericCast checks safe explicit numeric conversions.
func TestCheckAcceptsExplicitNumericCast(t *testing.T) {
	source := `fn take(x: i32) -> i32 { return x ;}
fn main() {
    let x = cast<i32>(1);
    print(take(x));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsContextualIntegerLiterals checks literal-only integer narrowing.
func TestCheckAcceptsContextualIntegerLiterals(t *testing.T) {
	source := `struct Byte {
    value: u8,
}
struct Counter {}
union ByteValue {
    Item(u8),
}
extern "c" fn raw_byte() -> ptr<u8>
impl Counter {
    fn push(self: Counter, value: u8) -> u8 {
        return value;
    }
}
fn take_u8(x: u8) -> u8 { return x ;}
fn take_i32(x: i32) -> i32 { return x ;}
fn returns_u8() -> u8 { return 7 ;}
fn returns_error() -> !u8 { return 8 ;}
fn main() -> !void {
    var assigned = take_u8(1);
    assigned = 2;
    let field = Byte { value: 65 };
    let variant = ByteValue::Item(66);
    let counter = Counter {};
    unsafe {
        let p = raw_byte();
        ptr_write(p, 67);
    }
    print(counter.push(68));
    print(take_i32(-1));
    print(returns_u8());
    print(try returns_error());
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsContextualIntegerLiteralOverflow checks narrowing bounds.
func TestCheckRejectsContextualIntegerLiteralOverflow(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "call literal overflow",
			source: `fn take(x: u8) -> u8 { return x ;}
fn main() {
    print(take(256));
}`,
			want: "integer literal `256` does not fit u8",
		},
		{
			name: "return literal overflow",
			source: `fn bad() -> u8 {
    return 300;
}`,
			want: "integer literal `300` does not fit u8",
		},
		{
			name: "variable is not narrowed",
			source: `fn take(x: u8) -> u8 { return x ;}
fn main() {
    let x = 1;
    print(take(x));
}`,
			want: "arg 1 of `take` expects u8, got i64",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsUnsafePointerCast checks pointer casts remain inside unsafe.
func TestCheckAcceptsUnsafePointerCast(t *testing.T) {
	source := `extern "c" fn raw() -> ptr<const u8>
fn main() {
    unsafe {
        let p = raw();
        let writable = cast<ptr<u8>>(p);
        ptr_write(writable, 1);
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsCastErrors checks explicit cast boundaries.
func TestCheckRejectsCastErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "pointer outside unsafe",
			source: `fn bad(p: ptr<const u8>) -> ptr<u8> {
    return cast<ptr<u8>>(p);
}`,
			want: "pointer cast requires unsafe block",
		},
		{
			name: "invalid value cast",
			source: `fn main() {
    let x = cast<i32>("no");
    print(x);
}`,
			want: "cannot cast []const u8 to i32",
		},
		{
			name: "bool is not numeric",
			source: `fn main() {
    let x = cast<i32>(true);
    print(x);
}`,
			want: "cannot cast bool to i32",
		},
		{
			name: "handle is not pointer",
			source: `struct User { name: []const u8 }
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    let p = cast<ptr<User>>(alice);
    print(p);
}`,
			want: "cannot cast handle<User> to ptr<User>",
		},
		{
			name: "arena non allocator",
			source: `struct User { name: []const u8 }
fn main() {
    let users = arena<User>(1);
    print(users);
}`,
			want: "`arena<User>` expects Allocator, got i64",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsErrorUnionTry checks minimal !T propagation.
func TestCheckAcceptsErrorUnionTry(t *testing.T) {
	source := `fn parse() -> !i64 {
    return 1;
}
fn main() -> !i64 {
    let value = try parse();
    return value + 1;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsErrorUnionError checks explicit error value construction.
func TestCheckAcceptsErrorUnionError(t *testing.T) {
	source := `fn parse() -> !i64 {
    return error("bad");
}
fn main() -> !i64 {
    let value = try parse();
    return value;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsBareReturnForErrorUnionVoid keeps !void success value-free.
func TestCheckAcceptsBareReturnForErrorUnionVoid(t *testing.T) {
	source := `fn step() -> !void {
    return;
}
fn main() -> !void {
    try step();
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsErrorFromStringView fixes error payload copy semantics.
func TestCheckAcceptsErrorFromStringView(t *testing.T) {
	source := `fn fail(text: std::string::String) -> !void {
    let bytes = text.as_bytes();
    return error(bytes);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsTypedErrorCast checks explicit untyped-to-typed error mapping.
func TestCheckAcceptsTypedErrorCast(t *testing.T) {
	source := `union CompileError {
    Message([]const u8),
}
fn lower(ok: bool) -> !i64 {
    if ok {
        return 1;
    }
    return error("bad");
}
fn parse(ok: bool) -> CompileError!i64 {
    let value = try cast<CompileError!i64>(lower(ok));
    return value;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsTryErrors checks readable error propagation errors.
func TestCheckRejectsTryErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "non error-union function",
			source: `fn parse() -> !i64 { return 1 ;}
fn main() {
    let x = try parse();
    print(x);
}`,
			want: "try requires function to return !T",
		},
		{
			name: "non error-union expression",
			source: `fn main() -> !i64 {
    let x = try 1;
    return x;
}`,
			want: "try expects !T, got i64",
		},
		{
			name: "error message type",
			source: `fn main() -> !i64 {
    return error(1);
}`,
			want: "`error` expects []const u8",
		},
		{
			name: "typed cast requires message variant",
			source: `union CompileError {
    Diagnostic(i64),
}
fn lower() -> !i64 {
    return error("bad");
}
fn parse() -> CompileError!i64 {
    return try cast<CompileError!i64>(lower());
}`,
			want: "typed error cast requires CompileError::Message([]const u8)",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsUnsafeBoundaryErrors checks unsafe-only operations.
func TestCheckRejectsUnsafeBoundaryErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "ptr read outside unsafe",
			source: `extern "c" fn source() -> ptr<i64>
fn main() {
    let p = source();
    print(ptr_read(p));
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "ptr read outside unsafe with pointer param",
			source: `fn read(p: ptr<u8>) -> u8 {
    return ptr_read(p);
}`,
			want: "ptr_read requires unsafe block",
		},
		{
			name: "extern call outside unsafe",
			source: `extern "c" fn source() -> u8
fn main() {
    print(source());
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "unsafe function call outside unsafe",
			source: `unsafe fn source() -> i64 { return 1 ;}
fn main() {
    print(source());
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "write through const pointer",
			source: `extern "c" fn source() -> ptr<const i64>
fn main() {
    unsafe {
        let p = source();
        ptr_write(p, 1);
    }
}`,
			want: "ptr_write` expects mutable non-null raw pointer",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsRawPointerDerefSyntaxErrors checks explicit pointer deref limits.
func TestCheckRejectsRawPointerDerefSyntaxErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "raw pointer deref outside unsafe",
			source: `fn read(p: ptr<u8>) -> u8 {
    return p.*;
}`,
			want: "raw pointer dereference requires unsafe block",
		},
		{
			name: "raw pointer field outside unsafe",
			source: `struct Node { tag: i64 }
fn read(p: ptr<Node>) -> i64 {
    return p.*.tag;
}`,
			want: "raw pointer dereference requires unsafe block",
		},
		{
			name: "nullable raw pointer deref",
			source: `fn read(p: ?ptr<u8>) -> u8 {
    unsafe {
        return p.*;
    }
}`,
			want: "nullable raw pointer `?ptr<u8>` cannot be dereferenced",
		},
		{
			name: "assign through const raw pointer",
			source: `fn write(p: ptr<const u8>) -> void {
    unsafe {
        p.* = 1;
        return;
    }
}`,
			want: "cannot assign through const raw pointer `ptr<const u8>`",
		},
		{
			name: "assign field through const raw pointer",
			source: `struct Node { tag: i64 }
fn write(p: ptr<const Node>) -> void {
    unsafe {
        p.*.tag = 1;
        return;
    }
}`,
			want: "cannot assign through const raw pointer `ptr<const Node>`",
		},
		{
			name: "direct raw pointer field access",
			source: `struct Node { tag: i64 }
fn read(p: ptr<Node>) -> i64 {
    unsafe {
        return p.tag;
    }
}`,
			want: "`ptr<Node>` has no fields",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckMinimalExplicitGenerics checks explicit instantiation and type branches.
func TestCheckMinimalExplicitGenerics(t *testing.T) {
	source := `fn Identity<T>(value: T) -> T {
    return value;
}
fn IsI64<T>(value: T) -> bool {
    comptime if T == type<i64> {
        return true;
    } else {
        return false;
    }
}
fn main() {
    print(Identity<i64>(7));
    print(IsI64<i64>(1));
    print(IsI64<bool>(false));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsGenericCallWithoutTypeArgs keeps instantiation explicit.
func TestCheckRejectsGenericCallWithoutTypeArgs(t *testing.T) {
	source := `fn Identity<T>(value: T) -> T {
    return value;
}
fn main() {
    print(Identity(7));
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "requires explicit type arguments") {
		t.Fatalf("got %q", err.Error())
	}
}

// checkSource parses and type-checks a source snippet.
func checkSource(source string) error {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return errors.New(p.Errors()[0])
	}
	return New().Check(program)
}
