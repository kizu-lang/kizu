package parser

import (
	"testing"

	"tiny-safe/internal/lexer"
)

// TestParseHello checks that a minimal program parses cleanly.
func TestParseHello(t *testing.T) {
	input := `fn main() {
    print("hello, kizu")
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
	want := `fn main() { print("hello, kizu") }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFunctionWithParamsAndReturn checks typed parameters and return parsing.
func TestParseFunctionWithParamsAndReturn(t *testing.T) {
	input := `fn add(a: i64, b: i64) -> i64 {
    return a + b
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn add(a: i64, b: i64) -> i64 { return (a + b) }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseIfAndWhile checks Phase 2 control-flow statement parsing.
func TestParseIfAndWhile(t *testing.T) {
	input := `fn main() {
    var i = 0
    if i < 3 {
        print(i)
    } else {
        print("done")
    }
    while i < 3 {
        i = i + 1
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn main() { var i = 0; if (i < 3) { print(i) } else { print("done") }; ` +
		`while (i < 3) { i = (i + 1) } }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseStructDecl checks top-level struct field parsing.
func TestParseStructDecl(t *testing.T) {
	input := `struct User {
    name: string
    age: i64
}
fn main() {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `struct User { name: string; age: i64 }
fn main() {  }`
	if got != want {
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
    print(Color.Red)
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `enum Color { Red; Green; Blue }
fn main() { print(Color.Red) }`
	if got != want {
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
    let color = Color.Red
    match color {
        Red => print("red")
        Green => print("green")
        Blue => print("blue")
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red; Green; Blue }
fn main() { let color = Color.Red; match color { Red => print("red"); ` +
		`Green => print("green"); Blue => print("blue") } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseArenaAndStructLiteral checks Phase 6 arena and struct literal syntax.
func TestParseArenaAndStructLiteral(t *testing.T) {
	input := `struct User {
    name: string
}
fn main() {
    let users = arena<User>()
    let alice = users.add(User { name: "alice" })
    print(users.get(alice).name)
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `struct User { name: string }
fn main() { let users = arena<User>(); let alice = users.add(User { name: "alice" }); ` +
		`print(users.get(alice).name) }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnsafeAndExtern checks Phase 12 unsafe and C ABI declarations.
func TestParseUnsafeAndExtern(t *testing.T) {
	input := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
unsafe fn write_byte(p: ptr<u8>, value: u8) {
    ptr_write(p, value)
}
fn main() {
    unsafe {
        print(get_byte(ptr_read_ptr()))
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
unsafe fn write_byte(p: ptr<u8>, value: u8) { ptr_write(p, value) }
fn main() { unsafe { print(get_byte(ptr_read_ptr())) } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseComptime checks Phase 13 compile-time expression and parameter syntax.
func TestParseComptime(t *testing.T) {
	input := `fn sized(comptime n: i64) -> i64 {
    return n
}
fn main() {
    let size = comptime 4 * 1024
    comptime if 1 + 1 == 2 {
        print(sized(comptime size))
    } else {
        print(0)
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn sized(comptime n: i64) -> i64 { return n }
fn main() { let size = comptime (4 * 1024); ` +
		`comptime if ((1 + 1) == 2) { print(sized(comptime size)) } else { print(0) } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseCast checks explicit low-level cast syntax.
func TestParseCast(t *testing.T) {
	input := `fn main() {
    let x = cast<i32>(1)
    unsafe {
        let p = cast<ptr<u8>>(raw())
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() { let x = cast<i32>(1); unsafe { let p = cast<ptr<u8>>(raw()) } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseTry checks error-union propagation syntax.
func TestParseTry(t *testing.T) {
	input := `fn main() -> !i64 {
    let x = try parse()
    return x
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() -> !i64 { let x = try parse(); return x }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRejectsExplicitLifetime checks that lifetime syntax is not accepted.
func TestParseRejectsExplicitLifetime(t *testing.T) {
	input := `fn show(s: borrow 'a string) {}`
	p := New(lexer.New(input))
	_ = p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("expected parser error")
	}
}
