package parser

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
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

// TestParseTopLevelErrorNamesAllowedDeclarations keeps top-level diagnostics useful.
func TestParseTopLevelErrorNamesAllowedDeclarations(t *testing.T) {
	p := New(lexer.New(`foo`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected parser error")
	}
	got := p.Errors()[0]
	for _, want := range []string{
		"expected declaration",
		"fn, test, import",
		"@requires_unsafe()",
		`identifier "foo"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("error = %q, want substring %q", got, want)
		}
	}
}

// TestParseExpectedGotDescribesTokenText avoids exposing lexer token names to users.
func TestParseExpectedGotDescribesTokenText(t *testing.T) {
	p := New(lexer.New(`enum Color { 1 }`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected parser error")
	}
	got := p.Errors()[0]
	if !strings.Contains(got, `expected enum tag, got integer "1"`) {
		t.Fatalf("error = %q, want integer token description", got)
	}
}

// TestParseDiagnosticsCarryStructuredSpan keeps parser failures structured for LSP and CLI.
func TestParseDiagnosticsCarryStructuredSpan(t *testing.T) {
	p := New(lexer.New("foo\n"))
	p.ParseProgram()
	if len(p.Diagnostics()) == 0 {
		t.Fatal("expected parser diagnostics")
	}
	got := p.Diagnostics()[0]
	if strings.HasPrefix(got.Message, "error:") {
		t.Fatalf("message = %q, want summary without CLI severity", got.Message)
	}
	if got.Span.Start.Line != 1 || got.Span.Start.Column != 1 {
		t.Fatalf("got start %d:%d, want 1:1", got.Span.Start.Line, got.Span.Start.Column)
	}
	if got.Span.End.Line != 1 || got.Span.End.Column != 4 {
		t.Fatalf("got end %d:%d, want 1:4", got.Span.End.Line, got.Span.End.Column)
	}
	if cli := got.CLIError(); !strings.HasPrefix(cli, "error: expected declaration") {
		t.Fatalf("cli = %q, want CLI severity prefix", cli)
	}
}

// TestParseTestDecl checks top-level test block parsing.
func TestParseTestDecl(t *testing.T) {
	input := `test "basic assertion" {
    std::testing::expect(true);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `test "basic assertion" { std::testing::expect(true); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFunctionWithParamsAndReturn checks typed parameters and return parsing.
func TestParseFunctionWithParamsAndReturn(t *testing.T) {
	input := `fn add(a: &i64, b: &var i64) -> i64 {
    return a + b;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn add(a: &i64, b: &var i64) -> i64 { return (a + b); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseFunctionDocComments checks attached docs survive parsing through pub.
func TestParseFunctionDocComments(t *testing.T) {
	input := `/// Parses a source file.
/// Returns an AST.
pub fn parse() -> i64 {
    return 1;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("got %d declarations, want 1", len(program.Decls))
	}
	fn, ok := program.Decls[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatalf("decl = %#v, want function", program.Decls[0])
	}
	if !fn.Public {
		t.Fatalf("function is not public")
	}
	want := "Parses a source file.\nReturns an AST."
	if fn.Doc != want {
		t.Fatalf("doc = %q, want %q", fn.Doc, want)
	}
}

// TestParseFunctionDocCommentsThroughExtern checks docs pass through modifiers.
func TestParseFunctionDocCommentsThroughExtern(t *testing.T) {
	input := `/// Calls the host runtime.
pub extern "c" fn host(value: i64)`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	fn, ok := program.Decls[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatalf("decl = %#v, want function", program.Decls[0])
	}
	if !fn.Public || fn.ExternABI != "c" {
		t.Fatalf("function modifiers = public:%v abi:%q", fn.Public, fn.ExternABI)
	}
	if fn.Doc != "Calls the host runtime." {
		t.Fatalf("doc = %q", fn.Doc)
	}
}

// TestParseImplMethodDocComments checks docs attach to methods inside impl blocks.
func TestParseImplMethodDocComments(t *testing.T) {
	input := `struct Parser {}

impl Parser {
    /// Advances to the next token.
    fn advance(self: Parser) -> void {
        return;
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	implDecl, ok := program.Decls[1].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("decl = %#v, want impl", program.Decls[1])
	}
	if len(implDecl.Methods) != 1 {
		t.Fatalf("got %d methods, want 1", len(implDecl.Methods))
	}
	if implDecl.Methods[0].Doc != "Advances to the next token." {
		t.Fatalf("method doc = %q", implDecl.Methods[0].Doc)
	}
}

// TestParseDeferCleanup checks block-exit cleanup statement parsing.
func TestParseDeferCleanup(t *testing.T) {
	input := `fn main() {
    defer values.deinit();
    return;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() { defer values.deinit(); return; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRejectsDeferredStatements checks the first defer form stays narrow.
func TestParseRejectsDeferredStatements(t *testing.T) {
	cases := []string{
		`fn main() { defer let value = 1; }`,
		`fn main() { defer return; }`,
		`fn main() { defer { print("cleanup"); } }`,
		`fn main() { defer defer values.deinit(); }`,
	}
	for _, input := range cases {
		p := New(lexer.New(input))
		p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", input)
		}
		if !strings.Contains(strings.Join(p.Errors(), "\n"), "defer expects an expression statement") {
			t.Fatalf("got parser errors %v", p.Errors())
		}
	}
}

// TestParseErrDeferCleanup checks errdefer parses as a distinct statement.
func TestParseErrDeferCleanup(t *testing.T) {
	input := `fn main() -> !void {
    errdefer values.deinit();
    return;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() -> !void { errdefer values.deinit(); return; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRejectsErrDeferredStatements keeps errdefer to expression statements.
func TestParseRejectsErrDeferredStatements(t *testing.T) {
	cases := []string{
		`fn main() { errdefer let value = 1; }`,
		`fn main() { errdefer return; }`,
		`fn main() { errdefer { print("cleanup"); } }`,
		`fn main() { errdefer errdefer values.deinit(); }`,
	}
	for _, input := range cases {
		p := New(lexer.New(input))
		p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", input)
		}
		if !strings.Contains(strings.Join(p.Errors(), "\n"), "errdefer expects an expression statement") {
			t.Fatalf("got parser errors %v", p.Errors())
		}
	}
}

// TestParseFieldAndDerefAssignment checks assignment targets beyond identifiers.
func TestParseFieldAndDerefAssignment(t *testing.T) {
	input := `fn rename(user: &var User) -> void {
    user.*.name = "bob";
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `fn rename(user: &var User) -> void { user.*.name = "bob"; }`
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
    name: []u8,
    age: i64,
}
fn main() {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	got := program.String()
	want := `struct User { name: []u8, age: i64 }
fn main() {  }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseTypeAndMemberDocComments checks docs attach to types and members.
func TestParseTypeAndMemberDocComments(t *testing.T) {
	input := `/// User record.
pub struct User {
    /// Display name.
    pub name: []u8,
}

/// User state.
enum State {
    /// Active user.
    Active,
}

/// User event.
union Event {
    /// Rename event.
    Rename([]u8),
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	user, ok := program.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf("decl = %#v, want struct", program.Decls[0])
	}
	if user.Doc != "User record." || user.Fields[0].Doc != "Display name." {
		t.Fatalf("struct docs = %q field docs = %q", user.Doc, user.Fields[0].Doc)
	}
	state, ok := program.Decls[1].(*ast.EnumDecl)
	if !ok {
		t.Fatalf("decl = %#v, want enum", program.Decls[1])
	}
	if state.Doc != "User state." || state.TagDocs["Active"] != "Active user." {
		t.Fatalf("enum docs = %q tag docs = %#v", state.Doc, state.TagDocs)
	}
	event, ok := program.Decls[2].(*ast.UnionDecl)
	if !ok {
		t.Fatalf("decl = %#v, want union", program.Decls[2])
	}
	if event.Doc != "User event." || event.Variants[0].Doc != "Rename event." {
		t.Fatalf("union docs = %q variant docs = %q", event.Doc, event.Variants[0].Doc)
	}
}

// TestParseContractImplDecl checks explicit contract implementation syntax.
func TestParseContractImplDecl(t *testing.T) {
	input := `contract Writer {
    fn write(self: &Self) -> !i64;
}
impl Writer for File {
    fn write(self: &Self) -> !i64 {
        return 1;
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `contract Writer { fn write(self: &Self) -> !i64; }
impl Writer for File { fn write(self: &Self) -> !i64 { return 1; } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseDynContractParam checks dyn contract object type syntax.
func TestParseDynContractParam(t *testing.T) {
	input := `fn save(writer: &dyn Writer) -> !void {
    return;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn save(writer: &dyn Writer) -> !void { return; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseImportsAndPublicDeclarations checks module imports and visibility syntax.
func TestParseImportsAndPublicDeclarations(t *testing.T) {
	input := `import app::lexer;
pub struct Token {
    pub kind: TokenKind,
    start: i64,
}
pub enum TokenKind {
    Ident,
}
pub fn lex(source: []u8) -> void {}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `import app::lexer
pub struct Token { pub kind: TokenKind, start: i64 }
pub enum TokenKind { Ident }
pub fn lex(source: []u8) -> void {  }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseEnumDecl checks Zig/C-style tag enum parsing.
func TestParseEnumDecl(t *testing.T) {
	input := `enum Color {
    Red,
    Green,
    Blue,
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
	want := `enum Color { Red, Green, Blue }
fn main() { print(Color::Red); }`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnionDecl checks tagged union declaration parsing.
func TestParseUnionDecl(t *testing.T) {
	input := `union Shape {
    Point,
    Circle(i64),
    Label([]u8),
}
fn main() {
    let shape = Shape::Circle(10);
    match shape {
        Point => print("point"),
        Circle(radius) => print(radius),
        Label(text) => print(text),
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `union Shape { Point, Circle(i64), Label([]u8) }
fn main() { let shape = Shape::Circle(10); match shape { Point => print("point"), ` +
		`Circle(radius) => print(radius), Label(text) => print(text) } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMatchStmt checks simple enum tag match parsing.
func TestParseMatchStmt(t *testing.T) {
	input := `enum Color {
    Red,
    Green,
    Blue,
}
fn main() {
    let color = Color::Red;
    match color {
        Red => print("red"),
        Green => print("green"),
        Blue => print("blue"),
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red, Green, Blue }
fn main() { let color = Color::Red; match color { Red => print("red"), ` +
		`Green => print("green"), Blue => print("blue") } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMatchWildcard checks fallback arm parsing.
func TestParseMatchWildcard(t *testing.T) {
	input := `enum Color { Red, Green, Blue }
fn main() {
    let color = Color::Blue;
    match color {
        Red => print("red"),
        _ => print("other"),
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red, Green, Blue }
fn main() { let color = Color::Blue; match color { Red => print("red"), _ => print("other") } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseControlExpressions checks if/match expression values without `;`
// inside semicolon-terminated simple statements (ADR-0074).
func TestParseControlExpressions(t *testing.T) {
	input := `enum Color { Red, Green }
fn main() {
    let color = Color::Green;
    let value = if true { 1 } else { 2 };
    let name = match color { Red => "red", Green => "green", };
    print(value);
    print(name);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `enum Color { Red, Green }
fn main() { let color = Color::Green; let value = if true { 1; } else { 2; }; ` +
		`let name = match color { Red => "red", Green => "green" }; print(value); print(name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseRequiresSemicolonAfterSimpleStatements rejects semicolonless simple
// statements while keeping branch and arm values free of `;` (ADR-0036/ADR-0074).
func TestParseRequiresSemicolonAfterSimpleStatements(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: `fn main() { let value = 1 }`,
			want:  "expected `;` after let statement",
		},
		{
			input: `fn main() { var value = 1 }`,
			want:  "expected `;` after let statement",
		},
		{
			input: `fn main() { let value = 0; value = 1 }`,
			want:  "expected `;` after assignment",
		},
		{
			input: `fn main() { print(1) print(2); }`,
			want:  "expected `;` after expression statement",
		},
		{
			input: "fn main() {\n    let value = 1\n    print(value);\n}",
			want:  "expected `;` after let statement",
		},
		{
			input: `fn loop_body() { while true { break } }`,
			want:  "expected `;` after break statement",
		},
	}
	for _, tc := range cases {
		p := New(lexer.New(tc.input))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", tc.input)
		}
		if !strings.Contains(strings.Join(p.Errors(), "\n"), tc.want) {
			t.Fatalf("got parser errors %v, want %q", p.Errors(), tc.want)
		}
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

// TestParseRequiresCommaListDelimiters keeps lists separate from statement syntax.
func TestParseRequiresCommaListDelimiters(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: `struct User { name: []u8; age: i64 }`,
			want:  "expected `,` after struct field",
		},
		{
			input: `enum Color { Red Green }`,
			want:  "expected `,` after enum tag",
		},
		{
			input: `union Shape { Point Circle(i64) }`,
			want:  "expected `,` after union variant",
		},
		{
			input: `fn main() { let color = Color::Red; ` +
				`match color { Red => print("red"); Green => print("green"); } }`,
			want: "expected `,` after match arm",
		},
		{
			input: `fn main() { let user = User { name: "alice"; age: 1 }; }`,
			want:  "expected `,` after struct literal field",
		},
	}
	for _, tc := range cases {
		p := New(lexer.New(tc.input))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", tc.input)
		}
		if !strings.Contains(strings.Join(p.Errors(), "\n"), tc.want) {
			t.Fatalf("got parser errors %v, want %q", p.Errors(), tc.want)
		}
	}
}

// TestParseAllowsTrailingCommas checks trailing commas in common list syntax.
func TestParseAllowsTrailingCommas(t *testing.T) {
	input := `fn id<T,>(value: T,) -> T {
    return value;
}
struct User {
    name: []u8,
}
fn main() {
    let user = User { name: "alice", };
    let values = std::array::Array<i64,>(allocator,);
    print(id(1,));
    print(values.len());
    print(user.name);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn id<T>(value: T) -> T { return value; }
struct User { name: []u8 }
fn main() { let user = User { name: "alice" }; let values = std::array::Array<i64>(allocator); ` +
		`print(id(1)); print(values.len()); print(user.name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseArenaAndStructLiteral checks Phase 6 arena and struct literal syntax.
func TestParseArenaAndStructLiteral(t *testing.T) {
	input := `struct User {
    name: []u8,
}
fn main() {
    let users = std::arena::Arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `struct User { name: []u8 }
fn main() { let users = std::arena::Arena<User>(allocator); ` +
		`let alice = users.add(User { name: "alice" }); ` +
		`print(users.get(alice).name); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseNamespacedStructLiteral checks imported type construction syntax.
func TestParseNamespacedStructLiteral(t *testing.T) {
	input := `import app::token;
fn main() {
    let token = token::Token { kind: 1 };
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `import app::token
fn main() { let token = token::Token { kind: 1 }; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMultiArgGenericTypes checks static type argument lists.
func TestParseMultiArgGenericTypes(t *testing.T) {
	input := `fn lookup(table: std::map::Map<[]u8, i64>) -> i64 {
    return table.get("main");
}
fn main() {
    let table = std::map::Map<[]u8, i64>(allocator);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn lookup(table: std::map::Map<[]u8, i64>) -> i64 { ` +
		`return table.get("main"); }
fn main() { let table = std::map::Map<[]u8, i64>(allocator); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseBorrowErrorUnionReturnType checks !&T and !&var T return spellings.
func TestParseBorrowErrorUnionReturnType(t *testing.T) {
	input := `fn at<T>(values: std::array::Array<T>, index: i64) -> !&T {
    return std::builtin::array_at<T>(values, index);
}
fn at_mut<T>(values: std::array::Array<T>, index: i64) -> !&var T {
    return std::builtin::array_at_mut<T>(values, index);
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn at<T>(values: std::array::Array<T>, index: i64) -> !&T { ` +
		`return std::builtin::array_at<T>(values, index); }
fn at_mut<T>(values: std::array::Array<T>, index: i64) -> !&var T { ` +
		`return std::builtin::array_at_mut<T>(values, index); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnsafeAndExtern checks Phase 12 @unsafe and C ABI declarations.
func TestParseUnsafeAndExtern(t *testing.T) {
	input := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
fn main() {
    @unsafe(extern_call, ptr_read) {
        print(get_byte(ptr_read_ptr()));
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `extern "c" fn get_byte(p: ptr<const u8>) -> u8
fn main() { @unsafe(extern_call, ptr_read) { print(get_byte(ptr_read_ptr())); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseUnsafeFnIsRejected keeps the removed caller-obligation syntax out.
func TestParseUnsafeFnIsRejected(t *testing.T) {
	p := New(lexer.New(`unsafe fn poke() { return; }`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected parser error")
	}
	if got := p.Errors()[0]; !strings.Contains(got, "unsafe fn is not supported") {
		t.Fatalf("first error = %q", got)
	}
}

// TestParseRequiresUnsafeFunction checks caller-obligation function syntax.
func TestParseRequiresUnsafeFunction(t *testing.T) {
	input := `pub @requires_unsafe() fn poke() -> i64 {
    return 1;
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `pub @requires_unsafe() fn poke() -> i64 { return 1; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	fn, ok := program.Decls[0].(*ast.FunctionDecl)
	if !ok || !fn.RequiresUnsafe || !fn.Public {
		t.Fatalf("decl = %#v, want public requires-unsafe function", program.Decls[0])
	}
}

// TestParseUnsafeCapabilityListRejectsEmpty checks capability lists are explicit.
func TestParseUnsafeCapabilityListRejectsEmpty(t *testing.T) {
	p := New(lexer.New(`fn main() { @unsafe() { return; } }`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected parser error")
	}
	if got := p.Errors()[0]; !strings.Contains(got, "expected unsafe capability") {
		t.Fatalf("first error = %q", got)
	}
}

// TestParseUnsafeCapabilityListRejectsUnknown checks capability names are reserved.
func TestParseUnsafeCapabilityListRejectsUnknown(t *testing.T) {
	p := New(lexer.New(`fn main() { @unsafe(anything) { return; } }`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected parser error")
	}
	if got := p.Errors()[0]; !strings.Contains(got, "unknown unsafe capability") {
		t.Fatalf("first error = %q", got)
	}
}

// TestParseRequiresUnsafeImplMethod checks caller-obligation method syntax.
func TestParseRequiresUnsafeImplMethod(t *testing.T) {
	input := `impl Register {
    @requires_unsafe() fn write(self: Register) -> void {
        return;
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	impl, ok := program.Decls[0].(*ast.ImplDecl)
	if !ok || len(impl.Methods) != 1 || !impl.Methods[0].RequiresUnsafe {
		t.Fatalf("decl = %#v, want impl method requiring unsafe", program.Decls[0])
	}
}

// TestParseComptime checks Phase 13 compile-time expression and parameter syntax.
func TestParseComptime(t *testing.T) {
	input := `fn sized<n: i64>() -> i64 {
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
	want := `fn sized<n: i64>() -> i64 { return n; }
fn main() { let size = comptime (4 * 1024); ` +
		`comptime if ((1 + 1) == 2) { print(sized(comptime size)); } else { print(0); } }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseMinimalGenerics checks explicit static type args and type literals.
func TestParseMinimalGenerics(t *testing.T) {
	input := `fn is_i64<T>(value: T) -> bool {
    comptime if T == type<i64> {
        return true;
    } else {
        return false;
    }
}
fn main() {
    print(is_i64<i64>(1));
    print(is_i64<bool>(false));
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn is_i64<T>(value: T) -> bool { ` +
		`comptime if (T == type<i64>) { return true; } else { return false; } }
fn main() { print(is_i64<i64>(1)); print(is_i64<bool>(false)); }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestParseAcceptsValueStaticArg keeps a compile-time value in the static
// argument list parseable. Whether the parameter accepts it is a type-check
// question, not a syntax one.
func TestParseAcceptsValueStaticArg(t *testing.T) {
	input := `fn sized<n: i64>() -> i64 {
    return n;
}
fn main() {
    print(sized<4096>());
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("got parser errors %v", p.Errors())
	}
	fn := program.Decls[0].(*ast.FunctionDecl)
	if len(fn.StaticParams) != 1 || fn.StaticParams[0].Type != "i64" {
		t.Fatalf("got static params %#v", fn.StaticParams)
	}
}

// TestParseCast checks explicit low-level cast syntax.
func TestParseCast(t *testing.T) {
	input := `fn main() {
    let x = cast<i32>(1);
    @unsafe(ptr_cast) {
        let p = cast<ptr<u8>>(raw());
    }
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn main() { let x = cast<i32>(1); @unsafe(ptr_cast) { let p = cast<ptr<u8>>(raw()); } }`
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

// TestParseBorrowReturnProvenance checks borrowed-return provenance syntax.
func TestParseBorrowReturnProvenance(t *testing.T) {
	input := `fn show(s: []u8) -> []u8 borrows s {
    return s[0..1];
}`
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	want := `fn show(s: []u8) -> []u8 borrows s { return s[0..1]; }`
	if got := program.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
