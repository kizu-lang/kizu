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

// TestRunFieldAndDerefAssignment checks mutable fields and &mut writes.
func TestRunFieldAndDerefAssignment(t *testing.T) {
	got := runSource(t, `struct User {
    name: []const u8
    age: i64
}
fn rename(user: &mut User) -> void {
    user.*.name = "bob";
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
    name: []const u8
}
fn main() {
    let users = arena<User>();
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
}`)
	want := "alice\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunEnumValueAccess checks Zig/C-style tag enum runtime values.
func TestRunEnumValueAccess(t *testing.T) {
	got := runSource(t, `enum Color {
    Red
    Green
    Blue
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
    Red
    Green
    Blue
}
fn main() {
    let color = Color::Blue;
    match color {
        Red => print("red");
        Green => print("green");
        Blue => print("blue");
    }
}`)
	want := "blue\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunTaggedUnionMatch checks payload binding in tagged union matches.
func TestRunTaggedUnionMatch(t *testing.T) {
	got := runSource(t, `union Shape {
    Point
    Circle(i64);
    Label([]const u8);
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
        Point => print("point");
        Circle(radius) => print(radius);
        Label(text) => print(text);
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
    let io = std::io::blocking();
    try std::fs::write_file(io, "__PATH__", "hello fs");
    let text = try std::fs::read_file(io, "__PATH__");
    print(text);
    return void;
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
