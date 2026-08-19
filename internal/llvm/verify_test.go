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
