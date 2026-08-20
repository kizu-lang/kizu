package llvm

import (
	"strings"
	"testing"
)

// TestVerifyEmittedTextReportsUndefinedRegister catches the shape #1622 shipped:
// a phi reading a register no instruction ever defines.
func TestVerifyEmittedTextReportsUndefinedRegister(t *testing.T) {
	text := strings.Join([]string{
		"define i64 @mode_of(i64 %kizu.k) {",
		"entry:",
		"  br label %match.end.1",
		"match.end.1:",
		"  %kizu.5 = phi i64 [ %kizu.2, %entry ]",
		"  ret i64 %kizu.5",
		"}",
	}, "\n")
	err := verifyEmittedText(text)
	if err == nil {
		t.Fatal("expected an undefined register to be reported")
	}
	if !strings.Contains(err.Error(), "`mode_of`") || !strings.Contains(err.Error(), "`%kizu.2`") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestVerifyEmittedTextAcceptsForwardReads accepts reads that textually precede
// their definition: a merge block is written before the arms that feed it.
func TestVerifyEmittedTextAcceptsForwardReads(t *testing.T) {
	text := strings.Join([]string{
		"%kizu.slice.u8 = type { ptr, i64 }",
		"",
		"define i64 @f(i64 %kizu.k, ptr byval(%kizu.slice.u8) %kizu.s.addr) {",
		"entry:",
		"  br label %join",
		"join:",
		"  %kizu.2 = phi i64 [ %kizu.1, %step ]",
		"  ret i64 %kizu.2",
		"step:",
		"  %kizu.1 = add i64 %kizu.k, 1",
		"  br label %join",
		"}",
	}, "\n")
	if err := verifyEmittedText(text); err != nil {
		t.Fatalf("valid text rejected: %v", err)
	}
}

// TestVerifyEmittedTextIgnoresModuleLevelText leaves globals, declares, and
// string constants (which may contain `%` bytes) out of the function check.
func TestVerifyEmittedTextIgnoresModuleLevelText(t *testing.T) {
	text := strings.Join([]string{
		"@.str.1 = private unnamed_addr constant [3 x i8] c\"%d\\00\"",
		"declare void @kizu_print(ptr, i64)",
		"",
		"define void @f() {",
		"entry:",
		"  ret void",
		"}",
	}, "\n")
	if err := verifyEmittedText(text); err != nil {
		t.Fatalf("valid text rejected: %v", err)
	}
}

// TestVerifyEmittedTextReportsAllocaOutsideEntry catches the shape that grew the
// stack a slot per loop turn: an alloca in a block the loop runs again.
func TestVerifyEmittedTextReportsAllocaOutsideEntry(t *testing.T) {
	text := strings.Join([]string{
		"define void @drain(ptr %kizu.p) {",
		"entry:",
		"  br label %loop",
		"loop:",
		"  %kizu.1 = alloca i64",
		"  store i64 0, ptr %kizu.1",
		"  br label %loop",
		"}",
	}, "\n")
	err := verifyEmittedText(text)
	if err == nil {
		t.Fatal("expected an alloca outside the entry block to be reported")
	}
	if !strings.Contains(err.Error(), "`drain`") || !strings.Contains(err.Error(), "alloca") {
		t.Fatalf("got %q", err.Error())
	}
}

// TestVerifyEmittedTextAcceptsEntryAllocas accepts what hoistAllocasToEntry
// produces: every alloca in the entry block, used from the blocks after it.
func TestVerifyEmittedTextAcceptsEntryAllocas(t *testing.T) {
	text := strings.Join([]string{
		"define void @drain(ptr %kizu.p) {",
		"entry:",
		"  %kizu.1 = alloca i64",
		"  br label %loop",
		"loop:",
		"  store i64 0, ptr %kizu.1",
		"  br label %loop",
		"}",
	}, "\n")
	if err := verifyEmittedText(text); err != nil {
		t.Fatalf("expected entry-block allocas to pass, got %v", err)
	}
}

// TestHoistAllocasToEntryMovesLoopSlots is the transform the invariant relies
// on: the alloca moves, the store that fills it stays where it was written.
func TestHoistAllocasToEntryMovesLoopSlots(t *testing.T) {
	body := strings.Join([]string{
		"entry:",
		"  %kizu.1 = call ptr @f()",
		"  br label %loop",
		"loop:",
		"  %kizu.2 = alloca i64",
		"  store i64 0, ptr %kizu.2",
		"  br label %loop",
		"",
	}, "\n")
	hoisted, err := hoistAllocasToEntry(body)
	if err != nil {
		t.Fatalf("expected a fixed-size alloca to hoist, got %v", err)
	}
	lines := strings.Split(hoisted, "\n")
	if lines[0] != "entry:" || lines[1] != "  %kizu.2 = alloca i64" {
		t.Fatalf("alloca did not move to the entry block:\n%s", hoisted)
	}
	if strings.Count(hoisted, "= alloca") != 1 {
		t.Fatalf("alloca was copied rather than moved:\n%s", hoisted)
	}
	if !strings.Contains(hoisted, "loop:\n  store i64 0, ptr %kizu.2") {
		t.Fatalf("the store left the block it was written in:\n%s", hoisted)
	}
	second, err := hoistAllocasToEntry(hoisted)
	if err != nil {
		t.Fatalf("expected hoisting to stay stable, got %v", err)
	}
	if second != hoisted {
		t.Fatalf("hoisting is not idempotent:\n%s", second)
	}
}

// TestHoistAllocasToEntryRefusesElementCount keeps the transform honest about
// what it can move: a slot whose size is a register cannot go where that
// register is not defined yet, and a fresh slot per turn is why one is written.
func TestHoistAllocasToEntryRefusesElementCount(t *testing.T) {
	body := strings.Join([]string{
		"entry:",
		"  br label %loop",
		"loop:",
		"  %kizu.1 = call i64 @count()",
		"  %kizu.2 = alloca i8, i64 %kizu.1",
		"  br label %loop",
		"",
	}, "\n")
	if _, err := hoistAllocasToEntry(body); err == nil {
		t.Fatal("expected a counted alloca to be refused")
	}
}

// TestHoistAllocasToEntryKeepsAggregateTypes accepts a type spelled with commas
// of its own: those separate the fields, not the operands.
func TestHoistAllocasToEntryKeepsAggregateTypes(t *testing.T) {
	body := strings.Join([]string{
		"entry:",
		"  br label %loop",
		"loop:",
		"  %kizu.1 = alloca { ptr, i64 }, align 8",
		"  br label %loop",
		"",
	}, "\n")
	hoisted, err := hoistAllocasToEntry(body)
	if err != nil {
		t.Fatalf("expected an aggregate-typed alloca to hoist, got %v", err)
	}
	if !strings.HasPrefix(hoisted, "entry:\n  %kizu.1 = alloca { ptr, i64 }, align 8") {
		t.Fatalf("aggregate-typed alloca did not move:\n%s", hoisted)
	}
}
