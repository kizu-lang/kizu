package ownership

import (
	"errors"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// TestCheckAcceptsCopyReuse checks that copy values are reusable after move contexts.
func TestCheckAcceptsCopyReuse(t *testing.T) {
	source := `fn take(a: i64) { print(a) }
fn main() {
    let a = 1
    let b = a
    take(a)
    print(a)
    print(b)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
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
			source: `struct Name { value: []const u8 }
fn main() {
    let a = Name { value: "hello" }
    let b = a
    print(a.value)
    print(b.value)
}`,
			want: "moved value `a` was used",
		},
		{
			name: "function argument move",
			source: `struct Name { value: []const u8 }
fn take(name: Name) { print(name.value) }
fn main() {
    let name = Name { value: "alice" }
    take(name)
    print(name.value)
}`,
			want: "moved value `name` was used",
		},
		{
			name: "double move",
			source: `struct Name { value: []const u8 }
fn take(name: Name) { print(name.value) }
fn main() {
    let name = Name { value: "alice" }
    take(name)
    take(name)
}`,
			want: "moved value `name` was used",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckBorrowArgumentDoesNotMove checks borrow parameters preserve ownership.
func TestCheckBorrowArgumentDoesNotMove(t *testing.T) {
	source := `fn show(s: &[]const u8) { print(s) }
fn main() {
    let name = "alice"
    show(name)
    print(name)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckMutableBorrowArgumentDoesNotMove checks &mut preserves ownership.
func TestCheckMutableBorrowArgumentDoesNotMove(t *testing.T) {
	source := `struct User { name: []const u8 }
fn show(user: &mut User) { print(user.name) }
fn main() {
    let user = User { name: "alice" }
    show(user)
    print(user.name)
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
			source: `fn bad(s: &[]const u8) -> []const u8 {
    return s
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "store borrowed parameter in local",
			source: `fn bad(s: &[]const u8) {
    let alias = s
    print(alias)
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "pass borrowed parameter to owner",
			source: `fn take(s: []const u8) { print(s) }
fn bad(s: &[]const u8) {
    take(s)
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow field",
			source: `struct Bad {
    value: &[]const u8
}
fn main() {}`,
			want: "struct field `Bad.value` cannot store borrow",
		},
		{
			name: "move non-copy deref",
			source: `struct User { name: []const u8 }
fn bad(user: &User) -> User {
    return user.*
}`,
			want: "cannot be moved out of borrow",
		},
		{
			name: "move non-copy mutable deref",
			source: `struct User { name: []const u8 }
fn bad(user: &mut User) -> User {
    return user.*
}`,
			want: "cannot be moved out of borrow",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckAllowsCopyDeref checks copying a primitive through a borrow is allowed.
func TestCheckAllowsCopyDeref(t *testing.T) {
	source := `fn copy_value(value: &i64) -> i64 {
    return value.*
}
fn main() {
    let x = 1
    print(copy_value(x))
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsMoveWhileBorrowed checks overlapping borrow and move in a call.
func TestCheckRejectsMoveWhileBorrowed(t *testing.T) {
	source := `struct Name { value: []const u8 }
fn use(a: &Name, b: Name) {
    print(a.value)
    print(b.value)
}
fn main() {
    let name = Name { value: "alice" }
    use(name, name)
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "value `name` cannot be moved while borrowed") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestCheckRejectsMutableBorrowConflicts checks & and &mut cannot overlap.
func TestCheckRejectsMutableBorrowConflicts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "shared then mutable",
			source: `struct User { name: []const u8 }
fn use(left: &User, right: &mut User) {
    print(left.name)
    print(right.name)
}
fn main() {
    let user = User { name: "alice" }
    use(user, user)
}`,
			want: "cannot be mutably borrowed while borrowed",
		},
		{
			name: "mutable then shared",
			source: `struct User { name: []const u8 }
fn use(left: &mut User, right: &User) {
    print(left.name)
    print(right.name)
}
fn main() {
    let user = User { name: "alice" }
    use(user, user)
}`,
			want: "cannot be borrowed while mutably borrowed",
		},
		{
			name: "double mutable",
			source: `struct User { name: []const u8 }
fn use(left: &mut User, right: &mut User) {
    print(left.name)
    print(right.name)
}
fn main() {
    let user = User { name: "alice" }
    use(user, user)
}`,
			want: "cannot be borrowed while mutably borrowed",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckRejectsBorrowedFieldMove checks field access cannot bypass borrow rules.
func TestCheckRejectsBorrowedFieldMove(t *testing.T) {
	source := `struct User { name: []const u8 }
struct Box { user: User }
fn take(user: User) { print(user.name) }
fn bad(box: &Box) {
    take(box.user)
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
    name: []const u8
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

// TestCheckRejectsArenaHandleErrors checks Phase 6 provenance errors.
func TestCheckRejectsArenaHandleErrors(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "wrong arena",
			source: `struct User { name: []const u8 }
fn main() {
    let left = arena<User>()
    let right = arena<User>()
    let alice = left.add(User { name: "alice" })
    print(right.get(alice).name)
}`,
			want: "handle `alice` does not belong to arena `right`",
		},
		{
			name: "inline wrong arena",
			source: `struct User { name: []const u8 }
fn main() {
    let left = arena<User>()
    let right = arena<User>()
    print(right.get(left.add(User { name: "alice" })).name)
}`,
			want: "handle from `left` does not belong to arena `right`",
		},
		{
			name: "unknown handle parameter",
			source: `struct User { name: []const u8 }
fn show(users: arena<User>, user: handle<User>) {
    print(users.get(user).name)
}`,
			want: "arena `users` has unknown provenance",
		},
		{
			name: "returned handle",
			source: `struct User { name: []const u8 }
fn make() -> handle<User> {
    let users = arena<User>()
    let alice = users.add(User { name: "alice" })
    return alice
}`,
			want: "handle `alice` cannot outlive its arena",
		},
		{
			name: "move after arena add",
			source: `struct User { name: []const u8 }
fn main() {
    let users = arena<User>()
    let user = User { name: "alice" }
    let alice = users.add(user)
    print(user.name)
    print(users.get(alice).name)
}`,
			want: "moved value `user` was used",
		},
		{
			name: "move field from arena borrow",
			source: `struct User { name: []const u8 }
struct Box { user: User }
fn take(user: User) { print(user.name) }
fn main() {
    let boxes = arena<Box>()
    let h = boxes.add(Box { user: User { name: "alice" } })
    take(boxes.get(h).user)
}`,
			want: "arena.get returns a local borrow and its fields cannot be moved",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckBranchMoveMarksOuterValueMoved checks possible moves escape branches.
func TestCheckBranchMoveMarksOuterValueMoved(t *testing.T) {
	source := `struct Name { value: []const u8 }
fn take(name: Name) { print(name.value) }
fn main() {
    let name = Name { value: "alice" }
    if true { take(name) }
    print(name.value)
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "moved value `name` was used") {
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
			source: `struct Name { value: []const u8 }
fn take(name: Name) { print(name.value) }
fn main() {
    let name = Name { value: "alice" }
    take(name)
    unsafe { print(name.value) }
}`,
			want: "moved value `name` was used",
		},
		{
			name: "borrow escape in unsafe function",
			source: `unsafe fn bad(s: &[]const u8) -> []const u8 {
    return s
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow escape in unsafe block",
			source: `fn bad(s: &[]const u8) {
    unsafe {
        let alias = s
        print(alias)
    }
}`,
			want: "borrowed value `s` cannot escape",
		},
		{
			name: "borrow field in unsafe-adjacent code",
			source: `struct Bad {
    value: &[]const u8
}
fn main() { unsafe { print(1) } }`,
			want: "struct field `Bad.value` cannot store borrow",
		},
	}
	runErrorCases(t, cases)
}

// TestCheckComptimeDoesNotMoveRuntimeValues checks compile-time arguments are read-only.
func TestCheckComptimeDoesNotMoveRuntimeValues(t *testing.T) {
	source := `fn sized(comptime n: i64) -> i64 { return n }
fn main() {
    let name = "alice"
    print(sized(comptime 8))
    print(name)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckRejectsComptimeRuntimeBorrow checks runtime borrows cannot cross comptime.
func TestCheckRejectsComptimeRuntimeBorrow(t *testing.T) {
	source := `fn bad(s: &[]const u8) -> []const u8 {
    let alias = comptime s
    return alias
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
    let name = "alice"
    let alias = comptime name
    print(alias)
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "runtime value cannot cross comptime boundary") {
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
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return errors.New(p.Errors()[0])
	}
	return New().Check(program)
}
