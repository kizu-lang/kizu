package types

import (
	"errors"
	"strings"
	"testing"

	"tiny-safe/internal/lexer"
	"tiny-safe/internal/parser"
)

// TestCheckValidPhase2Programs checks programs that the interpreter can run.
func TestCheckValidPhase2Programs(t *testing.T) {
	cases := []string{
		`fn main() { print("hello, kizu") }`,
		`fn add(a: int, b: int) -> int { return a + b }
fn main() { print(add(1, 2)) }`,
		`fn main() {
    let name = "alice"
    var age = 30
    age = age + 1
    print(name)
    print(age)
}`,
		`fn main() {
    let age = 20
    if age >= 20 { print("adult") } else { print("minor") }
    var i = 0
    while i < 3 { i = i + 1 }
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
    print(name)
}`,
			want: "undefined variable `name`",
		},
		{
			name: "arg count",
			source: `fn add(a: int, b: int) -> int { return a + b }
fn main() { print(add(1)) }`,
			want: "`add` expects 2 args, got 1",
		},
		{
			name: "arg type",
			source: `fn take(a: int) -> int { return a }
fn main() { print(take("no")) }`,
			want: "arg 1 of `take` expects int, got string",
		},
		{
			name: "return type",
			source: `fn bad() -> int {
    return "no"
}`,
			want: "return expects int, got string",
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
			source: `fn bad() -> int {
    1
}`,
			want: "function `bad` must return int",
		},
		{
			name: "if return path",
			source: `fn bad(ok: bool) -> int {
    if ok { return 1 }
}`,
			want: "function `bad` must return int",
		},
		{
			name: "binary operands",
			source: `fn main() {
    print(1 + "no")
}`,
			want: "operator `+` operands must have same type",
		},
		{
			name: "binary numeric operands",
			source: `fn bad(a: bool, b: bool) -> bool {
    return a + b
}`,
			want: "operator `+` expects numeric operands",
		},
		{
			name: "no numeric promotion",
			source: `fn bad(a: i32, b: u32) -> i32 {
    return a + b
}`,
			want: "operator `+` operands must have same type",
		},
		{
			name: "no integer promotion",
			source: `fn take(a: i32) -> i32 { return a }
fn main() { print(take(1)) }`,
			want: "arg 1 of `take` expects i32, got int",
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
	source := `fn a(x: i8) -> i8 { return x }
fn b(x: i16) -> i16 { return x }
fn c(x: i32) -> i32 { return x }
fn d(x: i64) -> i64 { return x }
fn e(x: u8) -> u8 { return x }
fn f(x: u16) -> u16 { return x }
fn g(x: u32) -> u32 { return x }
fn h(x: u64) -> u64 { return x }
fn i(x: usize) -> usize { return x }
fn j(x: isize) -> isize { return x }
fn k(x: f32) -> f32 { return x }
fn l(x: f64) -> f64 { return x }
fn m(x: i32, y: i32) -> i32 { return x + y }
fn n(x: f64, y: f64) -> bool { return x <= y }
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsStructDeclarations checks Phase 5 struct declarations.
func TestCheckAcceptsStructDeclarations(t *testing.T) {
	source := `struct User {
    name: string
    age: int
}
fn take(user: User) {}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsEnumDeclarations checks Zig/C-style tag enum values.
func TestCheckAcceptsEnumDeclarations(t *testing.T) {
	source := `enum Color {
    Red
    Green
    Blue
}
fn take(color: Color) -> Color { return color }
fn main() {
    let red = Color.Red
    print(take(red))
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
			source: `enum Color { Red Red }
fn main() {}`,
			want: "duplicate enum tag `Color.Red`",
		},
		{
			name: "unknown tag",
			source: `enum Color { Red }
fn main() { print(Color.Blue) }`,
			want: "unknown enum tag `Color.Blue`",
		},
		{
			name:   "unknown enum namespace",
			source: `fn main() { print(Color.Red) }`,
			want:   "undefined variable `Color`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsEnumMatch checks exhaustive simple enum match statements.
func TestCheckAcceptsEnumMatch(t *testing.T) {
	source := `enum Color {
    Red
    Green
    Blue
}
fn main() {
    let color = Color.Green
    match color {
        Red => print("red")
        Green => print("green")
        Blue => print("blue")
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
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
    match 1 { Red => print("red") }
}`,
			want: "match expects enum, got int",
		},
		{
			name: "unknown tag",
			source: `enum Color { Red Green }
fn main() {
    let color = Color.Red
    match color { Red => print("red") Blue => print("blue") }
}`,
			want: "unknown enum tag `Color.Blue`",
		},
		{
			name: "duplicate tag",
			source: `enum Color { Red Green }
fn main() {
    let color = Color.Red
    match color { Red => print("red") Red => print("again") Green => print("green") }
}`,
			want: "duplicate match tag `Color.Red`",
		},
		{
			name: "not exhaustive",
			source: `enum Color { Red Green }
fn main() {
    let color = Color.Red
    match color { Red => print("red") }
}`,
			want: "match on `Color` is not exhaustive",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsArenaHandle checks Phase 6 arena and handle types.
func TestCheckAcceptsArenaHandle(t *testing.T) {
	source := `struct User {
    name: string
}
fn main() {
    let users = arena<User>()
    let alice = users.add(User { name: "alice" })
    print(users.get(alice).name)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsUnsafePointerOperations checks raw pointer ops inside unsafe.
func TestCheckAcceptsUnsafePointerOperations(t *testing.T) {
	source := `extern "c" fn source() -> ptr<int>
fn main() {
    unsafe {
        let p = source()
        ptr_write(p, 1)
        print(ptr_read(p))
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
	source := `fn sized(comptime n: int) -> int {
    return n
}
fn main() {
    let size = comptime 4 * 1024
    comptime if 1 + 1 == 2 {
        print(sized(comptime 8))
    } else {
        print("not checked")
    }
    print(size)
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
    let x = 1
    let y = comptime x + 1
    print(y)
}`,
			want: "comptime error: runtime value cannot be used",
		},
		{
			name: "division by zero",
			source: `fn main() {
    let x = comptime 1 / 0
    print(x)
}`,
			want: "comptime error: division by zero",
		},
		{
			name: "comptime parameter",
			source: `fn sized(comptime n: int) -> int { return n }
fn main() {
    let x = 8
    print(sized(x))
}`,
			want: "comptime error: runtime value cannot be used",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsExplicitNumericCast checks safe explicit numeric conversions.
func TestCheckAcceptsExplicitNumericCast(t *testing.T) {
	source := `fn take(x: i32) -> i32 { return x }
fn main() {
    let x = cast<i32>(1)
    print(take(x))
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsUnsafePointerCast checks pointer casts remain inside unsafe.
func TestCheckAcceptsUnsafePointerCast(t *testing.T) {
	source := `extern "c" fn raw() -> ptr<const u8>
fn main() {
    unsafe {
        let p = raw()
        let writable = cast<ptr<u8>>(p)
        ptr_write(writable, cast<u8>(1))
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
    return cast<ptr<u8>>(p)
}`,
			want: "pointer cast requires unsafe block",
		},
		{
			name: "invalid value cast",
			source: `fn main() {
    let x = cast<i32>("no")
    print(x)
}`,
			want: "cannot cast string to i32",
		},
		{
			name: "bool is not numeric",
			source: `fn main() {
    let x = cast<i32>(true)
    print(x)
}`,
			want: "cannot cast bool to i32",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsErrorUnionTry checks minimal !T propagation.
func TestCheckAcceptsErrorUnionTry(t *testing.T) {
	source := `fn parse() -> !int {
    return 1
}
fn main() -> !int {
    let value = try parse()
    return value + 1
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsErrorUnionError checks explicit error value construction.
func TestCheckAcceptsErrorUnionError(t *testing.T) {
	source := `fn parse() -> !int {
    return error("bad")
}
fn main() -> !int {
    let value = try parse()
    return value
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
			source: `fn parse() -> !int { return 1 }
fn main() {
    let x = try parse()
    print(x)
}`,
			want: "try requires function to return !T",
		},
		{
			name: "non error-union expression",
			source: `fn main() -> !int {
    let x = try 1
    return x
}`,
			want: "try expects !T, got int",
		},
		{
			name: "error message type",
			source: `fn main() -> !int {
    return error(1)
}`,
			want: "`error` expects string",
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
			source: `extern "c" fn source() -> ptr<int>
fn main() {
    let p = source()
    print(ptr_read(p))
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "ptr read outside unsafe with pointer param",
			source: `fn read(p: ptr<u8>) -> u8 {
    return ptr_read(p)
}`,
			want: "ptr_read requires unsafe block",
		},
		{
			name: "extern call outside unsafe",
			source: `extern "c" fn source() -> u8
fn main() {
    print(source())
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "unsafe function call outside unsafe",
			source: `unsafe fn source() -> int { return 1 }
fn main() {
    print(source())
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name: "write through const pointer",
			source: `extern "c" fn source() -> ptr<const int>
fn main() {
    unsafe {
        let p = source()
        ptr_write(p, 1)
    }
}`,
			want: "ptr_write` expects mutable non-null raw pointer",
		},
	}
	runErrorCases(t, cases)
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
