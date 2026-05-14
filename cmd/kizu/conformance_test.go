package main

import (
	"os/exec"
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
		{name: "return", path: "../../examples/return.kizu", out: "done\n"},
		{name: "if", path: "../../examples/if.kizu", out: "adult\n"},
		{name: "while", path: "../../examples/while.kizu", out: "0\n1\n2\n"},
		{name: "struct", path: "../../examples/struct.kizu", out: "alice\n30\n"},
		{name: "borrow", path: "../../examples/borrow.kizu", out: "alice\nalice\n"},
		{name: "arena", path: "../../examples/arena.kizu", out: "alice\n"},
		{name: "error_union_try", path: "../../examples/error_union_try.kizu", out: "1\n"},
		{name: "error_union_void", path: "../../examples/error_union_void.kizu", out: "ok\n"},
		{name: "comptime", path: "../../examples/comptime.kizu", out: "8\n4096\n"},
		{name: "enum", path: "../../examples/enum.kizu", out: "Color.Green\ntrue\n"},
		{name: "match", path: "../../examples/match.kizu", out: "blue\n"},
		{
			name: "user_registry",
			path: "../../examples/user_registry.kizu",
			out:  "alice\nadmin\n8\nbob\nguest\n3\n0\n1\nready\n",
		},
		{
			name: "contract_writer",
			path: "../../examples/contract_writer.kizu",
			out:  "out\nhello\n2\n",
		},
		{
			name: "task_group",
			path: "../../examples/task_group.kizu",
			out:  "config\n42\n",
		},
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
	runKizuOK(t, "check", "../../examples/pointer_policy.kizu")
}

// TestV01NegativeExamples checks representative readable diagnostics.
func TestV01NegativeExamples(t *testing.T) {
	cases := append(ownershipNegativeCases(), typeNegativeCases()...)
	cases = append(cases, lowLevelNegativeCases()...)
	cases = append(cases, abstractionNegativeCases()...)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runKizu(tt.command, tt.path)
			if err == nil {
				t.Fatalf("expected command to fail\n%s", out)
			}
			if !strings.Contains(out, "error:") {
				t.Fatalf("got %q, want readable error prefix", out)
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
			path:    "../../examples/negative/moved_value.kizu",
			want:    "moved value `name` was used",
		},
		{
			name:    "borrow escape",
			command: "check",
			path:    "../../examples/negative/borrow_escape.kizu",
			want:    "borrowed value `s` cannot escape",
		},
		{
			name:    "borrow field",
			command: "check",
			path:    "../../examples/negative/borrow_field.kizu",
			want:    "struct field `Bad.value` cannot store borrow",
		},
		{
			name:    "arena get move",
			command: "check",
			path:    "../../examples/negative/arena_get_move.kizu",
			want:    "arena.get returns a local borrow and cannot be moved",
		},
		{
			name:    "immutable assignment",
			command: "check",
			path:    "../../examples/negative/immutable_assignment.kizu",
			want:    "cannot assign to immutable binding `x`",
		},
	}
}

// typeNegativeCases returns core type-system error examples.
func typeNegativeCases() []conformanceErrorCase {
	return []conformanceErrorCase{
		{
			name:    "invalid field",
			command: "check",
			path:    "../../examples/negative/invalid_field.kizu",
			want:    "unknown field `User.age`",
		},
		{
			name:    "empty return value",
			command: "check",
			path:    "../../examples/negative/empty_return_value.kizu",
			want:    "return expects i64, got void",
		},
		{
			name:    "missing return",
			command: "check",
			path:    "../../examples/negative/missing_return.kizu",
			want:    "function `bad` must return i64",
		},
		{
			name:    "invalid try",
			command: "check",
			path:    "../../examples/negative/invalid_try.kizu",
			want:    "try requires function to return !T",
		},
		{
			name:    "invalid cast",
			command: "check",
			path:    "../../examples/negative/invalid_cast.kizu",
			want:    "cannot cast string to i32",
		},
		{
			name:    "non exhaustive match",
			command: "check",
			path:    "../../examples/negative/match_non_exhaustive.kizu",
			want:    "match on `Color` is not exhaustive",
		},
	}
}

// lowLevelNegativeCases returns unsafe and pointer policy error examples.
func lowLevelNegativeCases() []conformanceErrorCase {
	return []conformanceErrorCase{
		{
			name:    "unsafe operation",
			command: "check",
			path:    "../../examples/negative/unsafe_call.kizu",
			want:    "call to `source` requires unsafe block",
		},
		{
			name:    "nullable pointer read",
			command: "check",
			path:    "../../examples/negative/nullable_ptr_read.kizu",
			want:    "ptr_read` expects non-null raw pointer",
		},
	}
}

// abstractionNegativeCases returns contract, Dyn, and task error examples.
func abstractionNegativeCases() []conformanceErrorCase {
	return []conformanceErrorCase{
		{
			name:    "missing contract method",
			command: "check",
			path:    "../../examples/negative/missing_contract_method.kizu",
			want:    "missing method `write`",
		},
		{
			name:    "unsatisfied dyn",
			command: "check",
			path:    "../../examples/negative/unsatisfied_dyn.kizu",
			want:    "File does not satisfy `Writer`",
		},
		{
			name:    "owned dyn",
			command: "check",
			path:    "../../examples/negative/owned_dyn.kizu",
			want:    "Dyn parameter `writer` must be borrowed",
		},
		{
			name:    "unawaited task",
			command: "check",
			path:    "../../examples/negative/unawaited_task.kizu",
			want:    "task `task` must be awaited or canceled",
		},
		{
			name:    "task move",
			command: "check",
			path:    "../../examples/negative/task_move.kizu",
			want:    "moved value `name` was used",
		},
		{
			name:    "task borrow capture",
			command: "check",
			path:    "../../examples/negative/task_borrow_capture.kizu",
			want:    "task cannot capture borrow parameter",
		},
	}
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
