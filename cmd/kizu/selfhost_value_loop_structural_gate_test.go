package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfhostValueLoopStructuralGate pins that std::kizu::lexer::tokenize stays lowered
// through the generic value-carried array loop (issue 1165): the name-based MirTokenizeStmt
// path must not return, and lower_value_array_loop must keep requiring the exact source shape
// so a near-miss function is an explicit error rather than a silent mis-lowering. It is a
// source-structural gate, not a textual LLVM gate, so it runs without the bootstrap.
func TestSelfhostValueLoopStructuralGate(t *testing.T) {
	mir := readBackendKizu(t, "compiled_mir.kizu")
	lower := readBackendKizu(t, "compiled_mir_lower.kizu")
	llvm := readBackendKizu(t, "compiled_mir_llvm.kizu")
	cliLLVM := readBackendKizu(t, "compiled_llvm.kizu")

	assertValueLoopNoTokenizeDispatch(t, mir, lower, llvm, cliLLVM)
	assertValueLoopExactShapeChecks(t, lower)
	assertValueLoopExactArrayConstructorCallee(t, lower)
}

// readBackendKizu reads a selfhost backend source file for source-structural gates.
func readBackendKizu(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", "selfhost", "src", "backend", rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// assertValueLoopNoTokenizeDispatch keeps the removed tokenize-specific lowering from returning.
func assertValueLoopNoTokenizeDispatch(t *testing.T, mir, lower, llvm, cliLLVM string) {
	t.Helper()
	forbidden := []struct {
		file    string
		content string
		token   string
	}{
		{"compiled_mir.kizu", mir, "MirTokenizeStmt"},
		{"compiled_mir.kizu", mir, "tokenize_stmt"},
		{"compiled_mir_lower.kizu", lower, "lower_tokenize_function"},
		{"compiled_mir_llvm.kizu", llvm, "append_multi_tokenize"},
		{"compiled_mir_llvm.kizu", llvm, "append_tokenize_append"},
		{"compiled_mir_llvm.kizu", llvm, "append_tokenize_globals"},
	}
	for _, f := range forbidden {
		if strings.Contains(f.content, f.token) {
			t.Errorf("%s still references %q -- the name-based tokenize lowering must stay removed",
				f.file, f.token)
		}
	}

	// The tokenize name dispatch must stay out of compiled_llvm.kizu; tokenize flows through the
	// generic multi-statement path.
	if strings.Contains(cliLLVM, `"std::kizu::lexer::tokenize"`) {
		t.Errorf("compiled_llvm.kizu still name-dispatches std::kizu::lexer::tokenize")
	}
}

// assertValueLoopExactShapeChecks pins exact source-shape validation errors.
func assertValueLoopExactShapeChecks(t *testing.T, lower string) {
	t.Helper()
	requiredChecks := []string{
		"value-loop while must follow the seed append",
		"value-loop while must be the penultimate statement",
		"value-loop array constructor let not found",
		"value-loop array constructor takes exactly the allocator argument",
		"value-loop array constructor argument must be the allocator",
		"value-loop seed takes exactly the source argument",
		"value-loop seed argument must be the source",
		"value-loop reassignment target must be a local",
		"value-loop reassignment must target the carried value",
		"value-loop update takes the source and carried value",
		"value-loop update first argument must be the source",
		"value-loop update second argument must be the carried value",
		"value-loop append receiver must be the array",
		"value-loop append argument must be the carried value",
		"value-loop must end with the array return",
		"value-loop must return the filled array",
	}
	for _, check := range requiredChecks {
		if !strings.Contains(lower, check) {
			t.Errorf("lower_value_array_loop missing exact-shape validation: %q", check)
		}
	}
}

// assertValueLoopExactArrayConstructorCallee pins exact Array constructor callee matching.
func assertValueLoopExactArrayConstructorCallee(t *testing.T, lower string) {
	t.Helper()
	exactCalleeChecks := []string{
		`std::mem::equal_bytes(callee, "std::array::Array")`,
		`std::mem::starts_with(callee, "std::array::Array<")`,
	}
	for _, check := range exactCalleeChecks {
		if !strings.Contains(lower, check) {
			t.Errorf("value_loop_array_let_name missing exact callee match: %q", check)
		}
	}
	if strings.Contains(lower, `std::mem::starts_with(callee, "std::array::Array")`) {
		t.Errorf("value_loop_array_let_name uses a loose std::array::Array prefix test -- " +
			"a near-miss like std::array::ArrayFoo would be mis-accepted as the array constructor")
	}
}
