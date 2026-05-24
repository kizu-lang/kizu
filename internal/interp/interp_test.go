package interp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestRunHello checks the print builtin on a minimal program.
func TestRunHello(t *testing.T) {
	got := runSource(t, `fn main() { print("hello, kizu"); }`)
	want := "hello, kizu\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunFunctionsAndReturn checks user function calls and explicit return.
func TestRunFunctionsAndReturn(t *testing.T) {
	got := runSource(t, `fn add(a: i64, b: i64) -> i64 { return a + b ;}
fn main() { print(add(1, 2)); }`)
	want := "3\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunVariablesAndAssignment checks let, var, and mutable assignment.
func TestRunVariablesAndAssignment(t *testing.T) {
	got := runSource(t, `fn main() {
    let name = "alice";
    var age = 30;
    age = age + 1;
    print(name);
    print(age);
}`)
	want := "alice\n31\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunFieldAndDerefAssignment checks mutable fields and &var writes.
func TestRunFieldAndDerefAssignment(t *testing.T) {
	got := runSource(t, `struct User {
    name: []u8,
    age: i64,
}
fn rename(user: &var User) -> void {
    user.name = "bob";
}
fn main() -> void {
    var user = User { name: "alice", age: 30 };
    user.age = user.age + 1;
    rename(user);
    print(user.name);
    print(user.age);
}`)
	want := "bob\n31\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunExplicitFieldBorrowProjection checks call-scoped field borrows at runtime.
func TestRunExplicitFieldBorrowProjection(t *testing.T) {
	got := runSource(t, `struct Pair {
    left: i64,
    right: i64,
}
fn set(value: &var i64) -> void {
    value.* = 5;
}
fn forward(pair: &var Pair) -> void {
    set(&var pair.left);
}
fn main() -> void {
    var pair = Pair { left: 1, right: 2 };
    forward(&var pair);
    print(pair.left);
    print(pair.right);
}`)
	want := "5\n2\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunBorrowedSliceIntoValueParam checks implicit reads through local borrows.
func TestRunBorrowedSliceIntoValueParam(t *testing.T) {
	got := runSource(t, `fn bytes_len(bytes: []u8) -> i64 {
    return std::mem::len(bytes);
}
fn forward(bytes: &[]u8) -> i64 {
    return bytes_len(bytes);
}
fn main() -> void {
    let bytes = "hello";
    print(forward(bytes));
}`)
	want := "5\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunControlFlow checks if/else and while execution.
func TestRunControlFlow(t *testing.T) {
	got := runSource(t, `fn main() {
    let age = 20;
    if age >= 20 { print("adult"); } else { print("minor"); }
    var i = 0;
    while i < 3 {
        print(i);
        i = i + 1;
    }
}`)
	want := "adult\n0\n1\n2\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunLogicalOperators checks short-circuit boolean evaluation.
func TestRunLogicalOperators(t *testing.T) {
	got := runSource(t, `fn fail() -> bool {
    print("bad");
    return true;
}
fn main() -> void {
    if false and fail() { print("and"); } else { print("skip-and"); }
    if true or fail() { print("skip-or"); }
}`)
	want := "skip-and\nskip-or\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunStdMemHelpers checks accelerated std::mem helpers preserve source semantics.
func TestRunStdMemHelpers(t *testing.T) {
	got := runSource(t, `fn main() -> !void {
    print(std::mem::len("  hello  "));
    print(std::mem::equal_bytes("hello", "hello"));
    print(std::mem::equal_bytes("hello", "world"));
    print(std::mem::starts_with("hello", "he"));
    print(try std::mem::byte_at("hello", 1));
    print(try std::mem::slice("hello", 1, 4));
    print(std::mem::trim_ascii("  hello  "));
    print(std::mem::is_ascii_space(cast<u8>(32)));
    return;
}`)
	want := "9\ntrue\nfalse\ntrue\n101\nell\nhello\ntrue\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunLoopControl checks break, continue, labels, and bounded for loops.
func TestRunLoopControl(t *testing.T) {
	got := runSource(t, `fn main() -> void {
    var i = 0;
    outer: while true {
        for 0..4 |j| {
            if j == 1 { continue; }
            if i == 1 { break :outer; }
            print(i * 10 + j);
        }
        i = i + 1;
    }
}`)
	want := "0\n2\n3\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunArenaHandle checks Phase 6 arena add/get and field access.
func TestRunArenaHandle(t *testing.T) {
	got := runSource(t, `struct User {
    name: []u8,
}
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = std::arena::Arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
    users.deinit();
}`)
	want := "alice\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunRejectsArenaNonAllocator checks runtime allocator capability validation.
func TestRunRejectsArenaNonAllocator(t *testing.T) {
	_, err := parseAndRun(`struct User { name: []u8 }
fn main() {
    let users = std::arena::Arena<User>(1);
    print(users);
}`)
	if err == nil {
		t.Fatal("expected runtime error")
	}
	if !strings.Contains(err.Error(), "expects Allocator") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestRunRejectsArenaUseAfterDeinit checks runtime arena cleanup state.
func TestRunRejectsArenaUseAfterDeinit(t *testing.T) {
	_, err := parseAndRun(`struct User { name: []u8 }
fn main() {
    let allocator = std::builtin::mem_page_allocator();
    let users = std::arena::Arena<User>(allocator);
    users.deinit();
    users.add(User { name: "alice" });
}`)
	if err == nil {
		t.Fatal("expected runtime error")
	}
	if !strings.Contains(err.Error(), "Arena was deinitialized") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestRunDeferCleanupOrder checks nested blocks and reverse cleanup order.
func TestRunDeferCleanupOrder(t *testing.T) {
	got := runSource(t, `struct Trace {
    label: []u8,
}
impl Trace {
    fn deinit(self: Trace) -> void {
        print(self.label);
    }
}
fn done() -> i64 {
    let first = Trace { label: "first" };
    defer first.deinit();
    if true {
        let inner = Trace { label: "inner" };
        defer inner.deinit();
    }
    let second = Trace { label: "second" };
    defer second.deinit();
    return 7;
}
fn main() {
    print(done());
}`)
	want := "inner\nsecond\nfirst\n7\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunArrayPopMovesAndDeinitDropsRemaining checks resource Array cleanup.
func TestRunArrayPopMovesAndDeinitDropsRemaining(t *testing.T) {
	got := runSource(t, `struct Trace {
    label: []u8,
}
impl Trace {
    fn deinit(self: Trace) -> void {
        print(self.label);
    }
}
fn main() -> !void {
    let allocator = std::builtin::mem_page_allocator();
    let traces = std::builtin::array<Trace>(allocator);
    let first = Trace { label: "first" };
    let second = Trace { label: "second" };
    let first_result = std::builtin::array_append<Trace>(traces, first);
    try first_result;
    let second_result = std::builtin::array_append<Trace>(traces, second);
    try second_result;
    let pop_result = std::builtin::array_pop<Trace>(traces);
    let last = try pop_result;
    print(last.label);
    last.deinit();
    std::builtin::array_deinit<Trace>(traces);
    return;
}`)
	want := "second\nsecond\nfirst\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunEnumValueAccess checks Zig/C-style tag enum runtime values.
func TestRunEnumValueAccess(t *testing.T) {
	got := runSource(t, `enum Color {
    Red,
    Green,
    Blue,
}
fn main() {
    let color = Color::Green;
    print(color);
    print(color == Color::Green);
}`)
	want := "Color::Green\ntrue\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunEnumMatch checks simple enum tag match execution.
func TestRunEnumMatch(t *testing.T) {
	got := runSource(t, `enum Color {
    Red,
    Green,
    Blue,
}
fn main() {
    let color = Color::Blue;
    match color {
        Red => print("red");,
        Green => print("green");,
        Blue => print("blue");,
    }
}`)
	want := "blue\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunMatchWildcard checks fallback arms for enum and union matches.
func TestRunMatchWildcard(t *testing.T) {
	got := runSource(t, `enum Color { Red, Green, Blue }
union Shape { Point, Circle(i64), Label([]u8), }
fn describe(shape: &Shape) -> void {
    match shape {
        Circle(radius) => print(radius);,
        _ => print("not circle");,
    }
}
fn main() {
    let color = Color::Blue;
    let name = match color { Red => "red", _ => "other", };
    print(name);
    describe(Shape::Label("name"));
}`)
	want := "other\nnot circle\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunControlExpressions checks if/match expressions and semicolonless statements.
func TestRunControlExpressions(t *testing.T) {
	got := runSource(t, `enum Color { Red, Green }
fn main() {
    let color = Color::Green
    let value = if false { 1 } else { 2 }
    let name = match color { Red => "red", Green => "green", }
    print(value)
    print(name)
}`)
	want := "2\ngreen\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunTaggedUnionMatch checks payload binding in tagged union matches.
func TestRunTaggedUnionMatch(t *testing.T) {
	got := runSource(t, `union Shape {
    Point,
    Circle(i64),
    Label([]u8),
}
fn main() {
    let first = Shape::Circle(10);
    let second = Shape::Label("name");
    describe(first);
    describe(second);
    describe(Shape::Point);
}
fn describe(shape: &Shape) -> void {
    match shape {
        Point => print("point");,
        Circle(radius) => print(radius);,
        Label(text) => print(text);,
    }
}`)
	want := "10\nname\npoint\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunComptime checks Phase 13 expressions and selected branches execute normally.
func TestRunComptime(t *testing.T) {
	got := runSource(t, `fn sized(comptime n: i64) -> i64 { return n ;}
fn main() {
    let size = comptime 4 * 1024;
    comptime if 1 + 1 == 2 {
        print(sized(comptime 8));
    } else {
        print(0);
    }
    print(size);
}`)
	want := "8\n4096\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunMinimalGenerics checks explicit instantiation and type-value branches.
func TestRunMinimalGenerics(t *testing.T) {
	got := runSource(t, `fn identity<T>(value: T) -> T { return value; }
fn is_i64<T>(value: T) -> bool {
    comptime if T == type<i64> {
        return true;
    } else {
        return false;
    }
}
fn main() {
    print(identity<i64>(7));
    print(identity<[]u8>("kizu"));
    print(is_i64<i64>(1));
    print(is_i64<bool>(false));
}`)
	want := "7\nkizu\ntrue\nfalse\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunErrorUnionTry checks minimal !T propagation at runtime.
func TestRunErrorUnionTry(t *testing.T) {
	got := runSource(t, `fn parse() -> !i64 {
    return 1;
}
fn main() -> !i64 {
    let value = try parse();
    print(value);
    return value + 1;
}`)
	want := "1\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunErrorUnionTryPropagatesError checks try returns error-union errors without printing.
func TestRunErrorUnionTryPropagatesError(t *testing.T) {
	got, err := parseAndRun(`fn parse() -> !i64 {
    return error("bad");
}
fn main() -> !i64 {
    let value = try parse();
    print(value);
    return value;
}`)
	if err == nil || err.Error() != "runtime error: bad" {
		t.Fatalf("got err %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty output", got)
	}
}

// TestRunTypedErrorTryPropagatesFunctionExpr checks typed errors from calls.
func TestRunTypedErrorTryPropagatesFunctionExpr(t *testing.T) {
	got, err := parseAndRun(`union CompileError {
    Message([]u8),
}
fn parse(ok: bool) -> CompileError!i64 {
    if ok {
        return 1;
    }
    return CompileError::Message("bad");
}
fn main() -> CompileError!void {
    let value = try parse(false);
    print("lowered");
    print(value);
    return;
}`)
	if err == nil || err.Error() != "runtime error: CompileError::Message" {
		t.Fatalf("got err %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty output", got)
	}
}

// TestRunTypedErrorCastMapsUntypedError checks explicit message adaptation at runtime.
func TestRunTypedErrorCastMapsUntypedError(t *testing.T) {
	got, err := parseAndRun(`union CompileError {
    Message([]u8),
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
}
fn main() -> CompileError!void {
    let value = try parse(false);
    print(value);
    return;
}`)
	if err == nil || err.Error() != "runtime error: CompileError::Message" {
		t.Fatalf("got err %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty output", got)
	}
}

// TestRuntimeErrorChecksMutableAssignment checks a short readable runtime error.
func TestRuntimeErrorChecksMutableAssignment(t *testing.T) {
	_, err := parseAndRun(`fn main() {
    let x = 1;
    x = 2;
}`)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "runtime error: cannot assign to immutable binding `x`"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// TestRuntimeRejectsInvalidArenaHandle fixes the arena handle runtime invariant.
func TestRuntimeRejectsInvalidArenaHandle(t *testing.T) {
	arena := &Arena{values: []Value{intValue(1)}}
	env := NewEnv()
	err := env.Define("bad", handleValue(arena, 1), false)
	if err != nil {
		t.Fatalf("define failed: %v", err)
	}

	var out bytes.Buffer
	_, err = New(&out).evalArenaGet(arena, []ast.Expression{&ast.IdentExpr{Name: "bad"}}, env)
	if err == nil {
		t.Fatalf("expected invalid handle error")
	}
	want := "runtime error: invalid arena handle"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// TestRunFsWriteAndRead checks the minimal std::fs API against a temp file.
func TestRunFsWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	source := strings.ReplaceAll(`fn main() -> !void {
    let io = std::builtin::io_blocking();
    try std::builtin::fs_write_file(io, "__PATH__", "hello fs");
    let text = try std::builtin::fs_read_file(io, "__PATH__");
    print(text);
    return;
}`, "__PATH__", path)
	got := runSource(t, source)
	if got != "hello fs\n" {
		t.Fatalf("got %q", got)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "hello fs" {
		t.Fatalf("written %q", string(written))
	}
}

// runSource executes source and fails the test on parse or runtime errors.
func runSource(t *testing.T, source string) string {
	t.Helper()
	out, err := parseAndRun(source)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return out
}

// parseAndRun parses and executes source, returning captured stdout.
func parseAndRun(source string) (string, error) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", errors.New(p.Errors()[0])
	}
	var out bytes.Buffer
	err := New(&out).Run(program)
	return out.String(), err
}
