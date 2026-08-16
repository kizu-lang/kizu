package types

import (
	"errors"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/unsafecap"
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

// TestCheckAcceptsTestDecl checks test blocks are first-class checked bodies.
func TestCheckAcceptsTestDecl(t *testing.T) {
	source := `test "basic assertion" {
    print("ok");
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsTestReturnValue keeps test blocks void-returning.
func TestCheckRejectsTestNonVoidReturnValue(t *testing.T) {
	source := `test "bad return" {
    return 1;
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "return expects !void, got i64") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsContractImpl checks explicit contract implementation syntax.
func TestCheckAcceptsContractImpl(t *testing.T) {
	source := `struct Bytes { text: []u8 }
struct File { name: []u8 }
contract Writer {
    fn write(bytes: &Bytes) -> !i64;
}
fn (self: &File) write(bytes: &Bytes) -> !i64 {
    print(self.name);
    print(bytes.text);
    return 1;
}
impl Writer for File;
fn save(writer: &dyn Writer, bytes: &Bytes) -> !void {
    let n = try writer.write(bytes);
    print(n);
    return;
}
fn main() -> !void {
    let file = File { name: "out" };
    let bytes = Bytes { text: "hello" };
    try save(file, bytes);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsOwnedDynParam keeps dynamic dispatch behind a borrow.
func TestCheckRejectsOwnedDynParam(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "owned",
			source: `struct File { name: []u8 }
contract Writer {
    fn write() -> !i64;
}
fn save(writer: dyn Writer) -> !void {
    return;
}
fn main() {}`,
			want: "dyn parameter `writer` must be borrowed",
		},
		{
			name: "mutable borrow",
			source: `contract Writer {
    fn write() -> !i64;
}
fn save(writer: &var dyn Writer) -> !void {
    return;
}
fn main() {}`,
			want: "dyn parameter `writer` must use immutable borrow in v0",
		},
		{
			name: "nullable",
			source: `contract Writer {
    fn write() -> !i64;
}
fn save(writer: ?dyn Writer) -> !void {
    return;
}
fn main() {}`,
			want: "nullable type `?dyn Writer` must wrap ptr<T>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSource(tc.source)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestCheckRejectsLegacyDynWrapper keeps the dyn keyword as the only spelling.
func TestCheckRejectsLegacyDynWrapper(t *testing.T) {
	source := `struct File { name: []u8 }
contract Writer {
    fn write() -> !i64;
}
fn save(writer: &Dyn<Writer>) -> !void {
    return;
}
fn main() {}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "unknown generic type `Dyn`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsStoredTypeValue keeps type<T> comptime-only.
func TestCheckRejectsStoredTypeValue(t *testing.T) {
	for _, tc := range storedTypeValueCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSource(tc.source)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

type errorCase struct {
	name   string
	source string
	want   string
}

// storedTypeValueCases returns negative cases for comptime-only type values.
func storedTypeValueCases() []errorCase {
	return []errorCase{
		{
			name: "local",
			source: `fn main() -> !void {
    let t = type<i64>;
    return;
}`,
			want: "type value cannot be stored in local `t`",
		},
		{
			name: "return",
			source: `fn type_name() -> type {
    return type<i64>;
}`,
			want: "function `type_name` cannot return type",
		},
		{
			name: "wrapped return",
			source: `fn type_name() -> !type {
    return type<i64>;
}`,
			want: "function `type_name` cannot return type",
		},
		{
			name: "parameter",
			source: `fn type_name(value: !type) -> !void {
    return;
}
fn main() {}`,
			want: "parameter `value` cannot have type",
		},
		{
			name: "struct field",
			source: `struct Holder {
    value: type,
}
fn main() {}`,
			want: "struct field `Holder.value` cannot store type value",
		},
		{
			name: "wrapped struct field",
			source: `struct Holder {
    value: !type,
}
fn main() {}`,
			want: "struct field `Holder.value` cannot store type value",
		},
		{
			name: "union payload",
			source: `union Holder {
    Value(type),
}
fn main() {}`,
			want: "union variant `Holder::Value` cannot store type value",
		},
		{
			name: "wrapped union payload",
			source: `union Holder {
    Value(!type),
}
fn main() {}`,
			want: "union variant `Holder::Value` cannot store type value",
		},
	}
}

// TestCheckRejectsIncompleteContractImpl checks contract method validation.
func TestCheckRejectsIncompleteContractImpl(t *testing.T) {
	source := `struct Bytes { text: []u8 }
struct File { name: []u8 }
contract Writer {
    fn write(bytes: &Bytes) -> !i64;
}
impl Writer for File;
fn main() {}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing method `write`") {
		t.Fatalf("got %q", err.Error())
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
			want: "arg 1 of `take` expects i64, got []u8",
		},
		{
			name: "return type",
			source: `fn bad() -> i64 {
    return "no";
}`,
			want: "return expects i64, got []u8",
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

// TestCheckOperatorKindErrorReportsOperandTypes keeps operator notes actionable.
func TestCheckOperatorKindErrorReportsOperandTypes(t *testing.T) {
	err := checkSource(`fn bad(a: bool, b: bool) -> bool {
    return a + b;
}`)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"operator `+` expects numeric operands",
		"at 2:14",
		"left operand has type bool",
		"right operand has type bool",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestCheckAssignmentMismatchUsesExpectedGot keeps assignment diagnostics concrete.
func TestCheckAssignmentMismatchUsesExpectedGot(t *testing.T) {
	err := checkSource(`struct User { name: []u8 }
fn main() {
    var user = User { name: "alice" };
    user.name = 1;
}`)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"assignment to `user.name` expects []u8, got i64",
		"at 4:5",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
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

// TestUnsafeCapabilityMetadataMatchesChecker keeps every operation kind the
// checker can name backed by help text. The name never appears in source, so a
// missing entry would silently drop the only line that tells the reader which
// kind of operation the marker would cover.
func TestUnsafeCapabilityMetadataMatchesChecker(t *testing.T) {
	checkerCapabilities := []unsafeCapability{
		unsafePtrRead, unsafePtrWrite, unsafePtrDeref, unsafePtrCast,
		unsafePtrIntCast, unsafeExternCall, unsafeUnsafeCall, unsafeField,
		unsafeVolatile,
	}
	for _, capability := range checkerCapabilities {
		if _, ok := unsafecap.Lookup(string(capability)); !ok {
			t.Fatalf("checker capability %q is missing metadata", capability)
		}
	}
	if got, want := len(unsafecap.All()), len(checkerCapabilities); got != want {
		t.Fatalf("metadata has %d capabilities, checker names %d", got, want)
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

// TestCheckAcceptsMapTypeDecl checks two-argument Map type spelling.
func TestCheckAcceptsMapTypeDecl(t *testing.T) {
	source := `fn use_table(table: std::map::Map<[]u8, i64>) -> void {
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
			want: "std::map::Map expects 2 static arguments",
		},
		{
			name: "wrong key",
			source: `fn use_table(table: std::map::Map<i64, i64>) -> void {
    return;
}
fn main() {}`,
			want: "std::map::Map key type must be []u8",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsStructDeclarations checks Phase 5 struct declarations.
func TestCheckAcceptsStructDeclarations(t *testing.T) {
	source := `struct User {
    name: []u8,
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
	source := `fn first(bytes: []u8) -> []u8 borrows bytes {
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
fn mutable(value: &var i64) -> &var i64 borrows value {
    return value;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsBorrowProvenanceThroughFieldAliases keeps the root owner
// through explicit field borrows and borrowed-view locals.
func TestCheckAcceptsBorrowProvenanceThroughFieldAliases(t *testing.T) {
	source := `import std::string;
struct Owner {
    text: string::String,
}
fn (self: &Owner) bytes() -> []u8 borrows self {
    let storage = &self.text;
    let view = storage.as_bytes();
    return view;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsMethodBorrowProvenance maps method return sources across
// the implicit receiver offset as well as explicit method parameters.
func TestCheckAcceptsMethodBorrowProvenance(t *testing.T) {
	source := `struct Picker {
    bytes: []u8,
}
fn (self: &Picker) from_self() -> []u8 borrows self {
    return self.bytes;
}
fn (self: &Picker) from_arg(value: []u8) -> []u8 borrows value {
    return value;
}
fn forward_self(picker: &Picker) -> []u8 borrows picker {
    let view = picker.from_self();
    return view;
}
fn forward_arg(picker: &Picker, value: []u8) -> []u8 borrows value {
    let view = picker.from_arg(value);
    return view;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckPreservesArrayElementNominalTypeThroughAt pins the exact receiver
// identity used by field lookup after a generic method and try unwrap.
func TestCheckPreservesArrayElementNominalTypeThroughAt(t *testing.T) {
	source := `struct MirCallArg {
    string_index: i64,
}
fn read(args: std::array::Array<MirCallArg>) -> !i64 {
    let arg = try args.at(0);
    return arg.string_index;
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
			source: `struct View { bytes: &[]u8 } fn main() {}`,
			want:   "borrow field `View.bytes` cannot store borrow",
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
			source: `fn bad(left: []u8, right: []u8) -> []u8 borrows left {
    return right;
}`,
			want: "return borrows `left` but returned value is not tied to that source",
		},
		{
			name: "field alias source mismatch",
			source: `import std::string;
struct Owner {
    text: string::String,
}
fn bad(left: &Owner, right: &Owner) -> []u8 borrows left {
    let storage = &right.text;
    let view = storage.as_bytes();
    return view;
}`,
			want: "return borrows `left` but returned value is not tied to that source",
		},
		{
			name: "temporary field owner",
			source: `import std::string;
struct Owner {
    text: string::String,
}
fn make(text: string::String) -> Owner {
    return Owner { text: text };
}
fn bad(owner: &Owner, text: string::String) -> []u8 borrows owner {
    let view = make(text).text.as_bytes();
    return view;
}`,
			want: "return borrows `owner` but returned value is not tied to that source",
		},
		{
			name: "method explicit source mismatch",
			source: `struct Picker {}
fn (self: &Picker) from_arg(value: []u8) -> []u8 borrows value {
    return value;
}
fn bad(picker: &Picker, left: []u8, right: []u8) -> []u8 borrows left {
    let view = picker.from_arg(right);
    return view;
}`,
			want: "return borrows `left` but returned value is not tied to that source",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsArrayElementViewTiedToArrayOwner keeps `Array.at` provenance.
// The element borrow is declared `-> !&T borrows self`, so a view read off the
// element is backed by whatever backs the array; binding the element without
// that source made `self.parts.at(index)` read as a fresh owner and refused a
// return that is in fact tied to `self`.
func TestCheckAcceptsArrayElementViewTiedToArrayOwner(t *testing.T) {
	source := `import std::array;
import std::string;
struct Store {
    parts: array::Array<string::String>,
}
fn view(store: &Store, index: i64) -> ![]u8 borrows store {
    let parts = &store.parts;
    let part = try parts.at(index);
    let length = part.len();
    let bytes = part.as_bytes();
    return bytes[0..length];
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("array element view rejected: %v", err)
	}
}

// TestCheckRejectsArrayElementViewFromAnotherOwner keeps the provenance answer
// exact: an element view off one array must not satisfy another's borrow.
func TestCheckRejectsArrayElementViewFromAnotherOwner(t *testing.T) {
	source := `import std::array;
import std::string;
struct Store {
    parts: array::Array<string::String>,
}
fn view(left: &Store, right: &Store, index: i64) -> ![]u8 borrows left {
    let parts = &right.parts;
    let part = try parts.at(index);
    let length = part.len();
    let bytes = part.as_bytes();
    return bytes[0..length];
}
fn main() {}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "return borrows `left` but returned value is not tied to that source"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q, want substring %q", err.Error(), want)
	}
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
			want: "duplicate type `Color`",
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
		{
			name: "mismatched enum equality",
			source: `enum Color { Red, Green }
enum Animal { Cat, Dog }
fn main() {
    let color = Color::Green;
    if color == Animal::Cat { return; }
}`,
			want: "operator `==` operands must have same type",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckBinaryMismatchReportsOperatorSpan keeps checker diagnostics location-aware.
func TestCheckBinaryMismatchReportsOperatorSpan(t *testing.T) {
	source := `enum Color { Red, Green }
enum Animal { Cat, Dog }

fn main() {
    let color = Color::Green;
    if color == Animal::Cat { return; }
}`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected error")
	}
	var diagnostic *DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("got %T, want DiagnosticError", err)
	}
	if diagnostic.Code != "type.operator_type_mismatch" {
		t.Fatalf("code = %q, want type.operator_type_mismatch",
			diagnostic.Code)
	}
	if diagnostic.Span.Start.Line != 6 || diagnostic.Span.Start.Column != 14 {
		t.Fatalf("start = %d:%d, want 6:14",
			diagnostic.Span.Start.Line, diagnostic.Span.Start.Column)
	}
	if diagnostic.Span.End.Line != 6 || diagnostic.Span.End.Column != 16 {
		t.Fatalf("end = %d:%d, want 6:16",
			diagnostic.Span.End.Line, diagnostic.Span.End.Column)
	}
	for _, want := range []string{
		"operator `==` operands must have same type",
		"at 6:14",
		"left operand has type Color",
		"right operand has type Animal",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestCheckUnknownNamespaceReportsHelp keeps namespace diagnostics actionable.
func TestCheckUnknownNamespaceReportsHelp(t *testing.T) {
	source := `enum Animal { Cat }
fn main() { print(Color::Red); }`
	err := checkSource(source)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"unknown namespace `Color`",
		"at 2:19",
		"enum/union name or import a module",
		"known namespaces: Animal",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
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
union Shape { Point, Circle(i64), Label([]u8), }
fn describe(shape: &Shape) -> void {
    match shape {
        Circle(radius) => print(radius);,
        _ => print("other");,
    }
}
fn main() -> void {
    let color = Color::Blue;
    let name = match color { Red => "red", _ => "other", };
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
    let color = Color::Green;
    let value = if true { 1 } else { 2 };
    let name = match color { Red => "red", Green => "green", };
    print(value);
    print(name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsSemicolonTailControlExpressions keeps block values explicit.
func TestCheckRejectsSemicolonTailControlExpressions(t *testing.T) {
	source := `fn main() -> void {
    let value = if true { 1; } else { 2; };
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
    Label([]u8),
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

// TestCheckRejectsMutableBorrowTypeErrors checks &var requires mutable locals.
func TestCheckRejectsMutableBorrowTypeErrors(t *testing.T) {
	source := `struct User { name: []u8 }
fn update(user: &var User) -> void { print(user.name); }
fn main() {
    let user = User { name: "alice" };
    update(user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "&var argument `user` must be mutable") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsMutableBorrowForwarding checks call-scoped &var reborrows.
func TestCheckAcceptsMutableBorrowForwarding(t *testing.T) {
	source := `struct User { name: []u8 }
fn rename(user: &var User) -> void { user.name = "bob" ;}
fn outer(user: &var User) -> void {
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

// TestCheckAcceptsExplicitFieldBorrowProjection checks call-scoped direct field borrows.
func TestCheckAcceptsExplicitFieldBorrowProjection(t *testing.T) {
	source := `struct Facts {
    type_kinds: std::map::Map<[]u8, i64>,
    type_arities: std::map::Map<[]u8, i64>,
    values: std::array::Array<i64>,
}
fn collect(
    type_kinds: &var std::map::Map<[]u8, i64>,
    type_arities: &var std::map::Map<[]u8, i64>,
    values: &var std::array::Array<i64>
) -> void {
    print(0);
}
fn run(facts: &var Facts) -> void {
    collect(&var facts.type_kinds, &var facts.type_arities, &var facts.values);
}
fn main() -> void {
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsInvalidFieldBorrowProjection keeps field projection call-scoped.
func TestCheckRejectsInvalidFieldBorrowProjection(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "immutable base",
			source: `struct Facts { value: i64 }
fn bump(value: &var i64) -> void { print(value); }
fn main() -> void {
    let facts = Facts { value: 1 };
    bump(&var facts.value);
}`,
			want: "&var argument `facts` must be mutable",
		},
		{
			name: "return projected field borrow",
			source: `struct Facts { value: i64 }
fn borrow_value(facts: &var Facts) -> &var i64 borrows facts {
    return &var facts.value;
}`,
			want: "return expects &var i64, got i64",
		},
		{
			name: "store projected field borrow",
			source: `struct Holder { value: &var i64 }
fn main() -> void {}`,
			want: "borrow field `Holder.value` cannot store borrow",
		},
		{
			name: "projected borrow to owner",
			source: `struct Facts { value: i64 }
fn take(value: i64) -> void { print(value); }
fn main() -> void {
    var facts = Facts { value: 1 };
    take(&facts.value);
}`,
			want: "borrow argument cannot be passed to owning parameter",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsSharedBorrowForwarding rejects shared-to-mutable reborrows.
func TestCheckRejectsSharedBorrowForwarding(t *testing.T) {
	source := `struct User { name: []u8 }
fn rename(user: &var User) -> void { user.name = "bob" ;}
fn outer(user: &User) -> void {
    rename(user);
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "&var argument `user` must be mutable") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckFieldAndDerefAssignment validates mutable assignment targets.
func TestCheckFieldAndDerefAssignment(t *testing.T) {
	source := `struct User { name: []u8 }
fn rename(user: &var User) -> void { user.name = "bob" ;}
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
			source: `struct User { name: []u8 }
fn main() -> void {
    let user = User { name: "alice" };
    user.name = "bob";
}`,
			want: "cannot assign field of immutable binding `user`",
		},
		{
			name: "shared borrow field",
			source: `struct User { name: []u8 }
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
			want: "union variant `Shape::Circle` expects i64, got []u8",
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
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
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
    name: []u8,
}
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
    defer users.deinit();
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsArrayResourceElements checks resource-owning Array element types.
func TestCheckAcceptsArrayResourceElements(t *testing.T) {
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
    let item = try values.pop();
    item.deinit();
    values.deinit();
    return;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsArrayPopOrPanicResourceElements keeps the trap variant move-capable.
func TestCheckAcceptsArrayPopOrPanicResourceElements(t *testing.T) {
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

// TestCheckRejectsArrayPopOrPanicArguments fixes the zero-argument signature.
func TestCheckRejectsArrayPopOrPanicArguments(t *testing.T) {
	err := checkSource(`fn check(values: std::array::Array<i64>) -> i64 {
    return values.pop_or_panic(0);
}
fn main() {}`)
	if err == nil {
		t.Fatal("expected pop_or_panic arity error")
	}
	if !strings.Contains(err.Error(), "`Array.pop_or_panic` expects 0 args") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAcceptsErrDeferredCleanup checks errdefer type-checks like defer.
func TestCheckAcceptsErrDeferredCleanup(t *testing.T) {
	source := `struct User {
    name: []u8,
}
fn build() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
    errdefer users.deinit();
    return users;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsInvalidErrDeferredCleanup checks errdefer rejects non-cleanup.
func TestCheckRejectsInvalidErrDeferredCleanup(t *testing.T) {
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
    users.deinit(1);
}`,
			want: "`arena.deinit` expects 0 args",
		},
		{
			name: "field receiver",
			source: `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
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
	source := `struct User { name: []u8 }
struct Registry { users: std::arena::Arena<User> }
fn (self: Registry) deinit() -> void {
    self.users.deinit();
    return;
}
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
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
    let p = unsafe source();
    unsafe ptr_write(p, 1);
    unsafe print(ptr_read(p));}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsRawPointerDerefSyntax checks explicit unsafe pointer dereference.
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
	source := `fn sized<n: i64>() -> i64 {
    return n;
}
fn main() {
    let size = comptime 4 * 1024;
    comptime if 1 + 1 == 2 {
        print(sized<8>());
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
			name: "runtime value as static argument",
			source: `fn sized<n: i64>() -> i64 { return n ;}
fn main() {
    let x = 8;
    print(sized<x>());
}`,
			want: "static argument `n` expects i64, got `x`",
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
fn (self: Counter) push(value: u8) -> u8 {
    return value;
}
fn take_u8(x: u8) -> u8 { return x ;}
fn take_i32(x: i32) -> i32 { return x ;}
fn returns_u8() -> u8 { return 7 ;}
fn returns_error() -> !u8 { return 8 ;}
fn main() -> !void {
    var assigned = take_u8(1);
    assigned = 2;
    let _ = Byte { value: 65 };
    let _ = ByteValue::Item(66);
    let counter = Counter {};
    let p = unsafe raw_byte();
    unsafe ptr_write(p, 67);    print(counter.push(68));
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
    let p = unsafe raw();
    let writable = unsafe cast<ptr<u8>>(p);
    unsafe ptr_write(writable, 1);}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsPointerIntCastAndVolatile checks low-level unsafe primitives.
func TestCheckAcceptsPointerIntCastAndVolatile(t *testing.T) {
	source := `fn main() {
    let p = unsafe ptr_from_int<ptr<u32>>(0);
    unsafe volatile_write(p, 1);
    let value = unsafe volatile_read(p);
    print(value);}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsRequiresUnsafeCall checks caller-obligation functions.
func TestCheckAcceptsRequiresUnsafeCall(t *testing.T) {
	source := `unsafe fn source() -> i64 { return 1; }
fn main() {
    unsafe print(source());}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsRequiresUnsafeMethodCall checks caller-obligation methods.
func TestCheckAcceptsRequiresUnsafeMethodCall(t *testing.T) {
	source := `struct Register { value: i64 }
unsafe fn (self: Register) read() -> i64 {
    return self.value;
}
fn main() {
    let register = Register { value: 1 };
    print(unsafe register.read());}`
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
			want: "pointer cast requires `unsafe`",
		},
		{
			name: "invalid value cast",
			source: `fn main() {
    let x = cast<i32>("no");
    print(x);
}`,
			want: "cannot cast []u8 to i32",
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
			source: `struct User { name: []u8 }
fn main() {
    let allocator = std::mem::page_allocator();
    let users = std::arena::Arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    let p = cast<ptr<User>>(alice);
    print(p);
}`,
			want: "cannot cast std::arena::Handle<User> to ptr<User>",
		},
		{
			name: "arena non allocator",
			source: `struct User { name: []u8 }
fn main() {
    let users = std::arena::Arena<User>(1);
    print(users);
}`,
			want: "`std::arena::Arena<User>` expects Allocator, got i64",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsErrorUnionTry checks minimal !T propagation.
func TestCheckAcceptsErrorUnionTry(t *testing.T) {
	source := `fn parse() -> !i64 {
    return 1;
}
fn main() -> !void {
    let value = try parse();
    print(value + 1);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsErrorUnionError checks explicit error value construction.
func TestCheckAcceptsErrorUnionError(t *testing.T) {
	source := `error ParseError {
    Bad,
}
fn parse() -> !i64 {
    return ParseError::Bad;
}
fn main() -> !void {
    let value = try parse();
    print(value);
    return;
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

// TestCheckAcceptsUserFunctionNamedError keeps `error` an ordinary name: the
// removed error(message) form reserved nothing.
func TestCheckAcceptsUserFunctionNamedError(t *testing.T) {
	source := `fn error(code: i64) -> void {
    print(code);
    return;
}
fn main() -> void {
    error(3);
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsUnionTypedErrorUnion keeps E!T limited to error sets.
func TestCheckRejectsUnionTypedErrorUnion(t *testing.T) {
	source := `union CompileError {
    Message([]u8),
}
fn parse() -> CompileError!i64 {
    return 1;
}`
	err := checkSource(source)
	if err == nil || !strings.Contains(err.Error(), "must be an `error` set") {
		t.Fatalf("got %v, want error-set requirement", err)
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
			source: `fn main() -> !void {
    let x = try 1;
    print(x);
    return;
}`,
			want: "try expects !T, got i64",
		},
		{
			name: "error message construction removed",
			source: `fn main() -> !void {
    return error(1);
}`,
			want: "undefined function `error`",
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
			want: "call to `source` requires `unsafe`",
		},
		{
			name: "ptr read outside unsafe with pointer param",
			source: `fn read(p: ptr<u8>) -> u8 {
    return ptr_read(p);
}`,
			want: "`ptr_read` requires `unsafe`",
		},
		{
			name: "extern call outside unsafe",
			source: `extern "c" fn source() -> u8
fn main() {
    print(source());
}`,
			want: "call to `source` requires `unsafe`",
		},
		{
			name: "requires unsafe call outside unsafe",
			source: `unsafe fn source() -> i64 { return 1 ;}
fn main() {
    print(source());
}`,
			want: "call to `source` requires `unsafe`",
		},
		{
			name: "requires unsafe method call outside unsafe",
			source: `struct Register { value: i64 }
unsafe fn (self: Register) read() -> i64 {
    return self.value;
}
fn main() {
    let register = Register { value: 1 };
    print(register.read());
}`,
			want: "call to `Register.read` requires `unsafe`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsUnsafeStructErrors checks the rules a raw pointer field puts
// on the struct that holds it.
func TestCheckRejectsUnsafeStructErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "raw pointer field without unsafe struct",
			source: `struct Buf { data: ptr<u8>, len: usize }
fn main() {}`,
			want: "must be declared `unsafe struct`",
		},
		{
			name: "raw pointer behind a slice",
			source: `struct Buf { data: []ptr<u8> }
fn main() {}`,
			want: "must be declared `unsafe struct`",
		},
		{
			name: "nullable raw pointer field",
			source: `struct Buf { data: ?ptr<u8> }
fn main() {}`,
			want: "must be declared `unsafe struct`",
		},
		{
			name: "public field on unsafe struct",
			source: `unsafe struct Buf { pub data: ptr<u8> }
fn main() {}`,
			want: "cannot have `pub` field `data`",
		},
		{
			name: "field write without a marker",
			source: `unsafe struct Buf { data: ptr<u8>, len: usize }
fn shrink(b: &var Buf, n: usize) -> void {
    b.len = n;
}`,
			want: "write to field `Buf.len` requires `unsafe`",
		},
		{
			name: "construction without a marker",
			source: `unsafe struct Buf { data: ptr<u8>, len: usize }
fn wrap(p: ptr<u8>) -> Buf {
    return Buf { data: p, len: 0 };
}`,
			want: "construction of `unsafe struct Buf` requires `unsafe`",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsUnsafeStruct checks that reading a field of an `unsafe struct`
// stays unmarked while writing and constructing one carry the marker.
func TestCheckAcceptsUnsafeStruct(t *testing.T) {
	source := `unsafe struct Buf { data: ptr<u8>, len: usize }
fn steal(b: &Buf) -> ptr<u8> {
    return b.data;
}
fn size(b: &Buf) -> usize {
    return b.len;
}
fn shrink(b: &var Buf, n: usize) -> void {
    unsafe b.len = n;
}
fn wrap(p: ptr<u8>) -> Buf {
    return unsafe Buf { data: p, len: 0 };
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckAcceptsUnsafeStructWithoutRawPointer keeps `unsafe struct` available
// to a struct whose invariant does not run through a raw pointer. The rule that
// a raw pointer field forces the declaration does not mean it is the only thing
// that may carry one.
func TestCheckAcceptsUnsafeStructWithoutRawPointer(t *testing.T) {
	source := `unsafe struct Slot { index: usize }
fn take(s: &Slot) -> usize {
    return s.index;
}
fn main() {}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckUnsafeBoundaryErrorNamesTheOperationKind keeps unsafe diagnostics
// actionable. Source no longer spells capability names, so the diagnostic is
// the only place a reader learns which kind of operation needed the marker.
func TestCheckUnsafeBoundaryErrorNamesTheOperationKind(t *testing.T) {
	err := checkSource(`fn read(p: ptr<u8>) -> u8 {
    return ptr_read(p);
}`)
	if err == nil {
		t.Fatal("expected error")
	}
	var diagnostic *DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("got %T, want DiagnosticError", err)
	}
	if diagnostic.Code != "unsafe.missing_marker" {
		t.Fatalf("code = %q, want unsafe.missing_marker",
			diagnostic.Code)
	}
	for _, want := range []string{
		"`ptr_read` requires `unsafe`",
		"at 2:12",
		"help: `unsafe` here covers raw pointer reads with `ptr_read(p)`",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestCheckRejectsUnmarkedUnsafeOperations checks that each kind of unproven
// operation needs a marker. A marker covers the expression it wraps, so a
// second statement needs its own.
func TestCheckRejectsUnmarkedUnsafeOperations(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "ptr_from_int without a marker",
			source: `fn main() {
    let p = ptr_from_int<ptr<u32>>(0);
    volatile_write(p, 1);
}`,
			want: "`ptr_from_int` requires `unsafe`",
		},
		{
			name: "marker on one statement does not reach the next",
			source: `fn main() {
    let p = unsafe ptr_from_int<ptr<u32>>(0);
    volatile_write(p, 1);
}`,
			want: "`volatile_write` requires `unsafe`",
		},
		{
			name: "write through const pointer",
			source: `extern "c" fn source() -> ptr<const i64>
fn main() {
    let p = unsafe source();
    unsafe ptr_write(p, 1);}`,
			want: "ptr_write` expects mutable non-null raw pointer",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsUnusedUnsafeMarker checks that an `unsafe` covering no
// unproven operation is rejected, so the marker means "this happens here"
// rather than "this would be allowed".
func TestCheckRejectsUnusedUnsafeMarker(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "marker covers an ordinary call",
			source: `fn main() {
    unsafe print(1);
}`,
			want: "`unsafe` covers no operation that needs it",
		},
		{
			name: "inner marker already covers the only operation",
			source: `fn write(p: ptr<u8>, value: u8) -> void {
    unsafe (unsafe ptr_write(p, value));
}`,
			want: "`unsafe` covers no operation that needs it",
		},
		{
			name: "marker on an assignment target that needs none",
			source: `fn main() {
    var count = 0;
    unsafe count = 1;
    print(count);
}`,
			want: "`unsafe` covers no operation that needs it",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAcceptsOneMarkerForSeveralOperations checks that a marker covers
// every unproven operation in the expression it wraps, not just the outermost.
func TestCheckAcceptsOneMarkerForSeveralOperations(t *testing.T) {
	source := `fn copy(dst: ptr<u8>, src: ptr<const u8>) -> void {
    unsafe ptr_write(dst, ptr_read(src));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
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
			want: "raw pointer dereference requires `unsafe`",
		},
		{
			name: "raw pointer field outside unsafe",
			source: `struct Node { tag: i64 }
fn read(p: ptr<Node>) -> i64 {
    return p.*.tag;
}`,
			want: "raw pointer dereference requires `unsafe`",
		},
		{
			name: "nullable raw pointer deref",
			source: `fn read(p: ?ptr<u8>) -> u8 {
    return unsafe p.*;}`,
			want: "nullable raw pointer `?ptr<u8>` cannot be dereferenced",
		},
		{
			name: "assign through const raw pointer",
			source: `fn write(p: ptr<const u8>) -> void {
    unsafe p.* = 1;
    return;}`,
			want: "cannot assign through const raw pointer `ptr<const u8>`",
		},
		{
			name: "assign field through const raw pointer",
			source: `struct Node { tag: i64 }
fn write(p: ptr<const Node>) -> void {
    unsafe p.*.tag = 1;
    return;}`,
			want: "cannot assign through const raw pointer `ptr<const Node>`",
		},
		{
			name: "direct raw pointer field access",
			source: `struct Node { tag: i64 }
fn read(p: ptr<Node>) -> i64 {
    return p.tag;}`,
			want: "`ptr<Node>` has no fields",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckMinimalExplicitGenerics checks explicit instantiation and type branches.
func TestCheckMinimalExplicitGenerics(t *testing.T) {
	source := `fn identity<T>(value: T) -> T {
    return value;
}
fn is_i64<T>(value: T) -> bool {
    comptime if T == type<i64> {
        return true;
    } else {
        return false;
    }
}
fn main() {
    print(identity<i64>(7));
    print(is_i64<i64>(1));
    print(is_i64<bool>(false));
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsBareTypeNameAsComptimeValue keeps type<T> canonical.
func TestCheckRejectsBareTypeNameAsComptimeValue(t *testing.T) {
	source := `fn is_i64<T>(value: T) -> bool {
    comptime if T == i64 {
        return true;
    } else {
        return false;
    }
}
fn main() {
    print(is_i64<i64>(1));
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "runtime value cannot be used") &&
		!strings.Contains(err.Error(), "undefined variable `i64`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsGenericCallWithoutTypeArgs keeps instantiation explicit.
func TestCheckRejectsGenericCallWithoutTypeArgs(t *testing.T) {
	source := `fn identity<T>(value: T) -> T {
    return value;
}
fn main() {
    print(identity(7));
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "requires explicit static arguments") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckAllCollectsPerFunctionErrors checks CheckAll reports each function's
// error instead of stopping at the first, while Check still returns just one.
func TestCheckAllCollectsPerFunctionErrors(t *testing.T) {
	source := "fn first() -> i64 { return missing_one; }\n" +
		"fn second() -> i64 { return missing_two; }\n"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}

	if errs := New().CheckAll(program); len(errs) != 2 {
		t.Fatalf("CheckAll returned %d errors, want 2", len(errs))
	}
	if err := New().Check(program); err == nil {
		t.Fatal("Check returned nil, want the first error")
	}
}

// TestCheckAllReturnsEmptyForValidProgram checks a sound program yields no errors.
func TestCheckAllReturnsEmptyForValidProgram(t *testing.T) {
	source := "fn main() { print(\"ok\"); }\n"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	if errs := New().CheckAll(program); len(errs) != 0 {
		t.Fatalf("CheckAll returned %#v, want no errors", errs)
	}
}

// checkSource parses and type-checks a source snippet.
func checkSource(source string) error {
	// The checker reads std container method signatures from std's own
	// declarations, so a program is only checkable with std loaded, which is
	// what every real invocation does.
	program, err := project.LoadSource("", withStdImport(source))
	if err != nil {
		return err
	}
	return New().Check(program)
}

// TestReferencedTypeNamesSeesThroughEveryWrapper covers the public API boundary
// check. Reading the names off the spelling stopped at the first wrapper it
// recognized, so `&[]Secret` answered `[]Secret` and a private type reached a
// public signature unreported.
func TestReferencedTypeNamesSeesThroughEveryWrapper(t *testing.T) {
	for _, tc := range []struct {
		spelling string
		want     []string
	}{
		{"Secret", []string{"Secret"}},
		{"[]Secret", []string{"Secret"}},
		{"&Secret", []string{"Secret"}},
		{"&[]Secret", []string{"Secret"}},
		{"&var []Secret", []string{"Secret"}},
		{"[]&Secret", []string{"Secret"}},
		{"!&[]Secret", []string{"Secret"}},
		{"?ptr<const Secret>", []string{"ptr", "Secret"}},
		{"dyn Secret", []string{"Secret"}},
		{"Error!Secret", []string{"Error", "Secret"}},
		{"std::array::Array<&[]Secret>", []string{"std::array::Array", "Secret"}},
		{"std::map::Map<[]u8, &Secret>", []string{"std::map::Map", "u8", "Secret"}},
	} {
		got := referencedTypeNames(tc.spelling)
		if len(got) != len(tc.want) {
			t.Fatalf("referencedTypeNames(%q) = %q, want %q", tc.spelling, got, tc.want)
		}
		for idx := range tc.want {
			if got[idx] != tc.want[idx] {
				t.Fatalf("referencedTypeNames(%q) = %q, want %q", tc.spelling, got, tc.want)
			}
		}
	}
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
