package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type conformanceRunCase struct {
	name string
	path string
	out  string
}

type conformanceErrorCase struct {
	name    string
	command string
	source  string
	path    string
	want    string
}

// TestV01PositiveExamples checks every executable v0.1 example stays runnable.
func TestV01PositiveExamples(t *testing.T) {
	cases := []conformanceRunCase{
		{name: "hello", path: "../../examples/hello.kizu", out: "hello, kizu\n"},
		{name: "variables", path: "../../examples/variables.kizu", out: "alice\n31\n"},
		{name: "arithmetic", path: "../../examples/arithmetic.kizu", out: "7\n"},
		{name: "functions", path: "../../examples/functions.kizu", out: "3\n"},
		{name: "if", path: "../../examples/if.kizu", out: "adult\n"},
		{name: "while", path: "../../examples/while.kizu", out: "0\n1\n2\n"},
		{name: "struct", path: "../../examples/struct.kizu", out: "alice\n30\n"},
		{name: "borrow", path: "../../examples/borrow.kizu", out: "alice\nalice\n"},
		{name: "arena", path: "../../examples/arena.kizu", out: "alice\n"},
		{name: "result_try", path: "../../examples/result_try.kizu", out: "1\n"},
		{name: "comptime", path: "../../examples/comptime.kizu", out: "8\n4096\n"},
		{name: "enum", path: "../../examples/enum.kizu", out: "Color.Green\ntrue\n"},
		{name: "match", path: "../../examples/match.kizu", out: "blue\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			runKizuOK(t, "check", tt.path)
			out := runKizuOK(t, "run", tt.path)
			if out != tt.out {
				t.Fatalf("got %q, want %q", out, tt.out)
			}
		})
	}
}

// TestV01CheckOnlyExamples checks examples that describe static boundaries.
func TestV01CheckOnlyExamples(t *testing.T) {
	runKizuOK(t, "check", "../../examples/unsafe_wrapper.kizu")
}

// TestV01NegativeExamples checks representative readable diagnostics.
func TestV01NegativeExamples(t *testing.T) {
	cases := append(ownershipNegativeCases(), staticNegativeCases()...)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if path == "" {
				path = writeKizuSource(t, tt.source)
			}
			out, err := runKizu(tt.command, path)
			if err == nil {
				t.Fatalf("expected command to fail\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("got %q, want substring %q", out, tt.want)
			}
		})
	}
}

// ownershipNegativeCases returns move, borrow, and mutability error examples.
func ownershipNegativeCases() []conformanceErrorCase {
	return []conformanceErrorCase{
		{
			name:    "moved value",
			command: "check",
			path:    "../../examples/move_error.kizu",
			want:    "moved value `name` was used",
		},
		{
			name:    "borrow escape",
			command: "check",
			source:  "fn bad(s: borrow string) -> string { return s }",
			want:    "borrowed value `s` cannot escape",
		},
		{
			name:    "borrow field",
			command: "check",
			source: `struct Bad {
    value: borrow string
}
fn main() {}`,
			want: "struct field `Bad.value` cannot store borrow",
		},
		{
			name:    "immutable assignment",
			command: "run",
			source:  "fn main() { let x = 1 x = 2 }",
			want:    "cannot assign to immutable binding `x`",
		},
	}
}

// staticNegativeCases returns type, unsafe, and exhaustiveness error examples.
func staticNegativeCases() []conformanceErrorCase {
	return []conformanceErrorCase{
		{
			name:    "invalid field",
			command: "check",
			source: `struct User {
    name: string
}
fn main() {
    let user = User { name: "alice" }
    print(user.age)
}`,
			want: "unknown field `User.age`",
		},
		{
			name:    "invalid try",
			command: "check",
			source: `fn parse() -> result<int> { return ok(1) }
fn main() {
    let x = try parse()
    print(x)
}`,
			want: "try requires function to return result<T>",
		},
		{
			name:    "invalid cast",
			command: "check",
			source: `fn main() {
    let x = cast<i32>("no")
    print(x)
}`,
			want: "cannot cast string to i32",
		},
		{
			name:    "unsafe operation",
			command: "check",
			source: `extern "c" fn source() -> u8
fn main() {
    print(source())
}`,
			want: "call to `source` requires unsafe block",
		},
		{
			name:    "non exhaustive match",
			command: "check",
			source: `enum Color {
    Red
    Green
}
fn main() {
    let color = Color.Red
    match color {
        Red => print("red")
    }
}`,
			want: "match on `Color` is not exhaustive",
		},
	}
}

// writeKizuSource writes a temporary Kizu source file.
func writeKizuSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runKizuOK runs the Kizu CLI and fails the test on errors.
func runKizuOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runKizu(args...)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}
	return out
}

// runKizu runs the Kizu CLI from the cmd/kizu package directory.
func runKizu(args ...string) (string, error) {
	cmdArgs := append([]string{"run", "."}, args...)
	cmd := exec.Command("go", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
