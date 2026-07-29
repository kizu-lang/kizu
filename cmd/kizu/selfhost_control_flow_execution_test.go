package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildAndRunSelfhostModule links a selfhost-emitted LLVM module together with a driver that
// exercises it and runs the result. A non-zero exit means the emitted code computed something the
// driver did not expect, so these tests check behaviour rather than LLVM text.
func buildAndRunSelfhostModule(t *testing.T, name string, module string) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("clang is required to execute selfhost-emitted LLVM: %v", err)
	}
	exe := filepath.Join(t.TempDir(), name)
	clang := exec.Command("clang", "-x", "ir", "-o", exe, "-")
	clang.Stdin = strings.NewReader(module)
	if out, err := clang.CombinedOutput(); err != nil {
		t.Fatalf("clang rejected the %s module: %v\n%s\n%s", name, err, out, module)
	}
	if out, err := exec.Command(exe).CombinedOutput(); err != nil {
		t.Fatalf("%s executable reported a mismatch: %v\n%s\n%s", name, err, out, module)
	}
}

// TestSelfhostShortCircuitConditionExecutes pins the nested short-circuit condition lowering by
// execution. The gate renders 'if a or b or (c and d)' as a four-leaf chain whose per-leaf branch
// targets encode the mixed or/and tree; the driver walks the full 16-row truth table and compares
// the emitted function against the same expression built from plain LLVM and/or.
func TestSelfhostShortCircuitConditionExecutes(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::short_circuit_condition_renderer_gate",
	)
	if err != nil {
		t.Fatalf("short circuit condition gate failed: %v\n%s", err, out)
	}
	// Each leaf must land in its own block so a later leaf never runs once an earlier one decided
	// the condition. Leaf 0 stays in entry; leaves 1..3 get if0_rhs, if0_rhs2, if0_rhs3.
	for _, label := range []string{"if0_rhs:", "if0_rhs2:", "if0_rhs3:", "if0_then:", "if0_cont:"} {
		if strings.Count(out, label) != 1 {
			t.Fatalf("expected exactly one %q block\n%s", label, out)
		}
	}
	module := out + `
define i32 @main() {
entry:
  br label %loop

loop:
  %i = phi i64 [ 0, %entry ], [ %next, %cont ]
  %abit = and i64 %i, 1
  %a = icmp ne i64 %abit, 0
  %bshift = lshr i64 %i, 1
  %bbit = and i64 %bshift, 1
  %b = icmp ne i64 %bbit, 0
  %cshift = lshr i64 %i, 2
  %cbit = and i64 %cshift, 1
  %c = icmp ne i64 %cbit, 0
  %dshift = lshr i64 %i, 3
  %dbit = and i64 %dshift, 1
  %d = icmp ne i64 %dbit, 0
  %got = call i1 @short_circuit_condition_gate(i1 %a, i1 %b, i1 %c, i1 %d)
  %cd = and i1 %c, %d
  %ab = or i1 %a, %b
  %want = or i1 %ab, %cd
  %same = icmp eq i1 %got, %want
  br i1 %same, label %cont, label %fail

cont:
  %next = add i64 %i, 1
  %done = icmp eq i64 %next, 16
  br i1 %done, label %ok, label %loop

ok:
  ret i32 0

fail:
  ret i32 1
}
`
	buildAndRunSelfhostModule(t, "short-circuit-condition", module)
}

// TestSelfhostDispatchValueBindingExecutes pins 'let x = match ..' by execution. The gate renders a
// dispatch that merges its arms through a stack slot; the driver checks that the matching tag
// yields its own arm's value and every other tag falls through to the default arm's value.
func TestSelfhostDispatchValueBindingExecutes(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::dispatch_value_binding_renderer_gate",
	)
	if err != nil {
		t.Fatalf("dispatch value binding gate failed: %v\n%s", err, out)
	}
	// The merge must go through a slot the join loads, not a redefinition per arm.
	for _, fragment := range []string{
		"= alloca i64",
		"store i64 %dispatch.result.0.0, ptr",
		"store i64 %dispatch.result.0.1, ptr",
		"%picked = load i64, ptr",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("dispatch value binding missing %q\n%s", fragment, out)
		}
	}
	module := out + `
define i32 @main() {
entry:
  %matched = call i64 @dispatch_value_binding_gate(i64 7)
  %matched_ok = icmp eq i64 %matched, 10
  br i1 %matched_ok, label %check_default, label %fail

check_default:
  %other = call i64 @dispatch_value_binding_gate(i64 3)
  %other_ok = icmp eq i64 %other, 20
  br i1 %other_ok, label %check_zero, label %fail

check_zero:
  %zero = call i64 @dispatch_value_binding_gate(i64 0)
  %zero_ok = icmp eq i64 %zero, 20
  br i1 %zero_ok, label %ok, label %fail

ok:
  ret i32 0

fail:
  ret i32 1
}
`
	buildAndRunSelfhostModule(t, "dispatch-value-binding", module)
}

// TestSelfhostStructFieldAccessPathExecutes pins the arbitrary-depth struct-literal field read by
// execution. The gate renders a field whose value walks 'outer.1.0.1'; the driver builds a nested
// aggregate with a distinct value in every slot so reading the wrong hop cannot pass by accident.
func TestSelfhostStructFieldAccessPathExecutes(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::struct_field_access_path_renderer_gate",
	)
	if err != nil {
		t.Fatalf("struct field access path gate failed: %v\n%s", err, out)
	}
	// Three hops means three extractvalue instructions, each reading the previous result.
	if got := strings.Count(out, "extractvalue"); got != 3 {
		t.Fatalf("expected three extractvalue hops, got %d\n%s", got, out)
	}
	module := `%test.inner = type { i64, i64 }
%test.mid = type { %test.inner }
%test.outer = type { i64, %test.mid }
%test.result = type { i64 }

` + out + `
define i32 @main() {
entry:
  %inner = insertvalue %test.inner zeroinitializer, i64 11, 0
  %inner2 = insertvalue %test.inner %inner, i64 22, 1
  %mid = insertvalue %test.mid zeroinitializer, %test.inner %inner2, 0
  %outer = insertvalue %test.outer zeroinitializer, i64 33, 0
  %outer2 = insertvalue %test.outer %outer, %test.mid %mid, 1
  %res = call %test.result @struct_field_access_path_gate(%test.outer %outer2)
  %got = extractvalue %test.result %res, 0
  %ok = icmp eq i64 %got, 22
  br i1 %ok, label %pass, label %fail

pass:
  ret i32 0

fail:
  ret i32 1
}
`
	buildAndRunSelfhostModule(t, "struct-field-access-path", module)
}
