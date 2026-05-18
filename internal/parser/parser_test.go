package parser

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
)

// TestParseHello checks that a minimal program parses cleanly.
func TestParseHello(t *testing.T) {
	input := `fn main() {
    print("hello, kizu");
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("got %d declarations, want 1", len(program.Decls))
	}
	got := program.String()
	want := `fn main() { print("hello, kizu"); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFunctionWithParamsAndReturn checks typed parameters and return parsing.
func TestParseFunctionWithParamsAndReturn(t *testing.T) {
	input := `fn add(a: &i64, b: &mut i64) -> i64 {
    return a + b;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn add(a: &i64, b: &mut i64) -> i64 { return (a + b); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFieldAndDerefAssignment checks assignment targets beyond identifiers.
func TestParseFieldAndDerefAssignment(t *testing.T) {
	input := `fn rename(user: &mut User) -> void {
    user.*.name = "bob";
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn rename(user: &mut User) -> void { user.*.name = "bob"; }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseIfAndWhile checks Phase 2 control-flow statement parsing.
func TestParseIfAndWhile(t *testing.T) {
	input := `fn main() {
    var i = 0;
    if i < 3 {
        print(i);
    } else {
        print("done");
    }
    while i < 3 {
        i = i + 1;
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn main() { var i = 0; if (i < 3) { print(i); } else { print("done"); } ` +
		`while (i < 3) { i = (i + 1); } }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseLogicalExpressions checks boolean operator precedence.
func TestParseLogicalExpressions(t *testing.T) {
	input := `fn main() {
    let ok = age >= 20 and age < 130 or false;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() { let ok = (((age >= 20) and (age < 130)) or false); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseIndexAndSliceExpressions checks checked byte access syntax.
func TestParseIndexAndSliceExpressions(t *testing.T) {
	input := `fn main() -> !void {
    let byte = bytes[0];
    let part = bytes[0..5];
    let tail = bytes[2..];
    let head = bytes[..3];
    return;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() -> !void { let byte = bytes[0]; let part = bytes[0..5]; ` +
		`let tail = bytes[2..]; let head = bytes[..3]; return; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseLoopControl checks for loops and labeled branches.
func TestParseLoopControl(t *testing.T) {
	input := `fn main() {
    outer: while true {
        for 0..3 |i| {
            continue :outer;
        }
        break;
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn main() { outer: while true { for 0..3 |i| { continue :outer; } break; } }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseStructDecl checks top-level struct field parsing.
func TestParseStructDecl(t *testing.T) {
	input := `struct User {
    name: []const u8
    age: i64
}
fn main() {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `struct User { name: []const u8; age: i64 }
fn main() {  }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseImportsAndPublicDeclarations checks module imports and visibility syntax.
func TestParseImportsAndPublicDeclarations(t *testing.T) {
	input := `import app::lexer;
pub struct Token {
    pub kind: TokenKind;
    start: i64;
}
pub enum TokenKind {
    Ident
}
pub fn lex(source: []const u8) -> void {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `import app::lexer
pub struct Token { pub kind: TokenKind; start: i64 }
pub enum TokenKind { Ident }
pub fn lex(source: []const u8) -> void {  }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseEnumDecl checks Zig/C-style tag enum parsing.
func TestParseEnumDecl(t *testing.T) {
	input := `enum Color {
    Red
    Green
    Blue
}
fn main() {
    print(Color::Red);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `enum Color { Red; Green; Blue }
fn main() { print(Color::Red); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnionDecl checks tagged union declaration parsing.
func TestParseUnionDecl(t *testing.T) {
	input := `union Shape {
    Point
    Circle(i64);
    Label([]const u8);
}
fn main() {
    let shape = Shape::Circle(10);
    match shape {
        Point => print("point");
        Circle(radius) => print(radius);
        Label(text) => print(text);
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `union Shape { Point; Circle(i64); Label([]const u8) }
fn main() { let shape = Shape::Circle(10); match shape { Point => print("point"); ` +
		`Circle(radius) => print(radius); Label(text) => print(text); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMatchStmt checks simple enum tag match parsing.
func TestParseMatchStmt(t *testing.T) {
	input := `enum Color {
    Red
    Green
    Blue
}
fn main() {
    let color = Color::Red;
    match color {
        Red => print("red");
        Green => print("green");
        Blue => print("blue");
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red; Green; Blue }
fn main() { let color = Color::Red; match color { Red => print("red"); ` +
		`Green => print("green"); Blue => print("blue"); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseControlExpressions checks if/match expressions and optional semicolons.
func TestParseControlExpressions(t *testing.T) {
	input := `enum Color { Red Green }
fn main() {
    let color = Color::Green
    let value = if true { 1 } else { 2 }
    let name = match color { Red => "red", Green => "green" }
    print(value)
    print(name)
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red; Green }
fn main() { let color = Color::Green; let value = if true { 1; } else { 2; }; ` +
		`let name = match color { Red => "red"; Green => "green"; }; print(value); print(name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRequiresSemicolonAfterReturn keeps return statement syntax explicit.
func TestParseRequiresSemicolonAfterReturn(t *testing.T) {
	input := `fn main() -> i64 {
    return 1
}`
	p := New(lexer.New(input))
	_ = p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("expected parser error")
	}
	if !strings.Contains(p.Errors()[0], "expected `;` after return statement") {
		t.Fatalf("got %v", p.Errors())
	}
}

// TestParseAllowsTrailingCommas checks trailing commas in common list syntax.
func TestParseAllowsTrailingCommas(t *testing.T) {
	input := `fn id<T,>(value: T,) -> T {
    return value;
}
struct User {
    name: []const u8,
}
fn main() {
    let user = User { name: "alice", }
    let values = std::array::Array<i64,>(allocator,)
    print(id(1,))
    print(values.len())
    print(user.name)
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn id<T>(value: T) -> T { return value; }
struct User { name: []const u8 }
fn main() { let user = User { name: "alice" }; let values = std::array::Array<i64>(allocator); ` +
		`print(id(1)); print(values.len()); print(user.name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseArenaAndStructLiteral checks Phase 6 arena and struct literal syntax.
func TestParseArenaAndStructLiteral(t *testing.T) {
	input := `struct User {
    name: []const u8
}
fn main() {
    let users = arena<User>();
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `struct User { name: []const u8 }
fn main() { let users = arena<User>(); let alice = users.add(User { name: "alice" }); ` +
		`print(users.get(alice).name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMultiArgGenericTypes checks generic type argument lists.
func TestParseMultiArgGenericTypes(t *testing.T) {
	input := `fn lookup(table: std::map::Map<[]const u8, i64>) -> i64 {
    return table.get("main");
}
fn main() {
    let table = std::map::Map<[]const u8, i64>(allocator);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn lookup(table: std::map::Map<[]const u8, i64>) -> i64 { ` +
		`return table.get("main"); }
fn main() { let table = std::map::Map<[]const u8, i64>(allocator); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseBorrowErrorUnionReturnType checks !&T and !&mut T return spellings.
func TestParseBorrowErrorUnionReturnType(t *testing.T) {
	input := `fn at<T>(values: std::array::Array<T>, index: i64) -> !&T {
    return std::builtin::array_at<T>(values, index);
}
fn at_mut<T>(values: std::array::Array<T>, index: i64) -> !&mut T {
    return std::builtin::array_at_mut<T>(values, index);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn at<T>(values: std::array::Array<T>, index: i64) -> !&T { ` +
		`return std::builtin::array_at<T>(values, index); }
fn at_mut<T>(values: std::array::Array<T>, index: i64) -> !&mut T { ` +
		`return std::builtin::array_at_mut<T>(values, index); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnsafeAndExtern checks Phase 12 unsafe and C ABI declarations.
func TestParseUnsafeAndExtern(t *testing.T) {
	input := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
unsafe fn write_byte(p: ptr<u8>, value: u8) {
    ptr_write(p, value);
}
fn main() {
    unsafe {
        print(get_byte(ptr_read_ptr()));
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
unsafe fn write_byte(p: ptr<u8>, value: u8) { ptr_write(p, value); }
fn main() { unsafe { print(get_byte(ptr_read_ptr())); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseComptime checks Phase 13 compile-time expression and parameter syntax.
func TestParseComptime(t *testing.T) {
	input := `fn sized(comptime n: i64) -> i64 {
    return n;
}
fn main() {
    let size = comptime 4 * 1024;
    comptime if 1 + 1 == 2 {
        print(sized(comptime size));
    } else {
        print(0);
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn sized(comptime n: i64) -> i64 { return n; }
fn main() { let size = comptime (4 * 1024); ` +
		`comptime if ((1 + 1) == 2) { print(sized(comptime size)); } else { print(0); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseCast checks explicit low-level cast syntax.
func TestParseCast(t *testing.T) {
	input := `fn main() {
    let x = cast<i32>(1);
    unsafe {
        let p = cast<ptr<u8>>(raw());
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() { let x = cast<i32>(1); unsafe { let p = cast<ptr<u8>>(raw()); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseTry checks error-union propagation syntax.
func TestParseTry(t *testing.T) {
	input := `fn main() -> !i64 {
    let x = try parse();
    return x;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() -> !i64 { let x = try parse(); return x; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRejectsExplicitLifetime checks that lifetime syntax is not accepted.
func TestParseRejectsExplicitLifetime(t *testing.T) {
	input := `fn show(s: &'a []const u8) {}`
	p := New(lexer.New(input))
	_ = p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("expected parser error")
	}
}
