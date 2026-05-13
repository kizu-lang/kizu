package ownership

import (
	"errors"
	"strings"
	"testing"

	"tiny-safe/internal/lexer"
	"tiny-safe/internal/parser"
)

// TestCheckAcceptsCopyReuse checks that copy values are reusable after move contexts.
func TestCheckAcceptsCopyReuse(t *testing.T) {
	source := `fn take(a: int) { print(a) }
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
			source: `fn main() {
    let a = "hello"
    let b = a
    print(a)
    print(b)
}`,
			want: "moved value `a` was used",
		},
		{
			name: "function argument move",
			source: `fn take(s: string) { print(s) }
fn main() {
    let name = "alice"
    take(name)
    print(name)
}`,
			want: "moved value `name` was used",
		},
		{
			name: "double move",
			source: `fn take(s: string) { print(s) }
fn main() {
    let name = "alice"
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
	source := `fn show(s: borrow string) { print(s) }
fn main() {
    let name = "alice"
    show(name)
    print(name)
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}

// TestCheckBranchMoveMarksOuterValueMoved checks possible moves escape branches.
func TestCheckBranchMoveMarksOuterValueMoved(t *testing.T) {
	source := `fn take(s: string) { print(s) }
fn main() {
    let name = "alice"
    if true { take(name) }
    print(name)
}`
	err := checkSource(source)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "moved value `name` was used") {
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
