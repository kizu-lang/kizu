package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfhostValueLoopStructuralGate pins that std::kizu::lexer::tokenize is lowered by a
// path that reads its statements rather than its name (issue 1165). It is a source-structural
// gate, not a textual LLVM gate, so it runs without the bootstrap.
//
// It used to also assert that lower_value_array_loop validated the exact source shape, so a
// near-miss function was an explicit error rather than a silent mis-lowering. That shape is
// gone: the generic while path took the loop over, and the assertions had no subject left.
// They are not replaced by nothing. The old shape emitted the whole loop as one opaque
// ValueWhile and never lowered its body, so what it did inside was unpinnable; the artifact
// gate now pins tokenize's generic form directly -- the head reload, BOTH array_append calls
// including the body one that could not be named before, and the owned success return. A
// mis-lowering that the deleted assertions would have caught by refusing to recognise the
// shape is now caught by the emitted code not matching.
func TestSelfhostValueLoopStructuralGate(t *testing.T) {
	mir := readBackendKizu(t, "compiled_mir.kizu")
	lower := readBackendKizu(t, "compiled_mir_lower.kizu")
	llvm := readBackendKizu(t, "compiled_mir_llvm.kizu")
	cliLLVM := readBackendKizu(t, "compiled_llvm.kizu")

	assertValueLoopNoTokenizeDispatch(t, mir, lower, llvm, cliLLVM)
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
