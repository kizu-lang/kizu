package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseFormatAllocMaxLines pins the hand-written parse_format_alloc LLVM emitter at its
// current size. The formatter migration (issue 1165 / issue 1162) moves real
// selfhost::parser::format helpers onto the compiled path; it must not grow the legacy
// hand-written indentation / import-sort / comment logic in append_parse_format_alloc_function.
const parseFormatAllocMaxLines = 197

// formatCompiledHelperSeeds is the read-only formatter closure compiled into stage2: the
// four TokenKind predicates, token_text, next_token_text_equals (the first token-array
// read helper, reading tokens.len()/tokens.get(...) on a value-receiver Array<Token> param
// and calling lexer::is_eof across the module boundary), and index_after_import (the first
// token-array scan-while helper, a bounded counter loop with a parameter-seeded induction
// variable and i64 early returns). They are the first selfhost::parser::format members on
// the compiled path and must keep being emitted from both the IR fact catalog and the
// backend BFS.
var formatCompiledHelperSeeds = []string{
	"is_import_token",
	"is_ident_token",
	"is_double_colon_token",
	"is_semicolon_token",
	"token_text",
	"next_token_text_equals",
	"index_after_import",
}

// formatHandPathOnlyHelpers stay out of the compiled format closure: they are the
// indentation / import-sort / comment / whole-formatter helpers whose logic lives in the
// hand-written parse_format_alloc path. Seeding them would smuggle the legacy formatter
// logic into the compiled closure instead of lowering it as a real component.
var formatHandPathOnlyHelpers = []string{
	"format_source",
	"leading_import_indices",
	"sort_import_indices",
	"append_sorted_imports",
	"append_indent",
	"append_preserved_line_comments",
}

// TestSelfhostFormatHelperStructuralGate pins that the first selfhost::parser::format
// read-only helpers stay on the compiled path through the shared component catalog, and
// that the migration does not extend the hand-written parse_format_alloc emitter. It is a
// source-structural gate, so it runs without the bootstrap or clang.
func TestSelfhostFormatHelperStructuralGate(t *testing.T) {
	irEmission := readSelfhostSrc(t, filepath.Join("ir", "executable_functions.kizu"))
	backendEmission := readSelfhostSrc(t, filepath.Join("backend", "cli_llvm.kizu"))
	parseLLVM := readSelfhostSrc(t, filepath.Join("backend", "cli_parse_llvm.kizu"))

	assertFormatClosureCatalogDriven(t, irEmission, backendEmission)
	assertFormatClosureSeeds(t, irEmission, backendEmission)
	assertFormatClosureExcludesHandPath(t, irEmission, backendEmission)
	assertParseFormatAllocNotExtended(t, parseLLVM)
}

// readSelfhostSrc reads a selfhost source file (relative to selfhost/src) for the gate.
func readSelfhostSrc(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", "selfhost", "src", rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// assertFormatClosureCatalogDriven pins that the formatter facts flow through the shared
// component catalog + body-call collector rather than a hand-written per-helper metadata
// table, and that the backend resolves each member's symbol/params from signature facts.
func assertFormatClosureCatalogDriven(t *testing.T, irEmission, backendEmission string) {
	t.Helper()
	irRequired := []string{
		`component_function_catalog::collect_from_ast`,
		`"selfhost::parser::format"`,
		`executable_body::append_catalog_helper_body_ir`,
		`collect_catalog_closure_direct_callees`,
		`"selfhost::parser::format::"`,
	}
	for _, fragment := range irRequired {
		if !strings.Contains(irEmission, fragment) {
			t.Errorf("executable_functions.kizu missing catalog-driven format emission: %q", fragment)
		}
	}
	backendRequired := []string{
		`fn append_format_reachable_compiled_functions`,
		`"selfhost::parser::format::"`,
		`append_component_reachable_compiled_functions`,
	}
	for _, fragment := range backendRequired {
		if !strings.Contains(backendEmission, fragment) {
			t.Errorf("cli_llvm.kizu missing catalog-driven format emission: %q", fragment)
		}
	}
}

// assertFormatClosureSeeds pins the read-only seed set on both emission sites.
func assertFormatClosureSeeds(t *testing.T, irEmission, backendEmission string) {
	t.Helper()
	for _, seed := range formatCompiledHelperSeeds {
		quoted := `"` + seed + `"`
		if !strings.Contains(irEmission, quoted) {
			t.Errorf("executable_functions.kizu format closure missing seed %q", seed)
		}
		if !strings.Contains(backendEmission, quoted) {
			t.Errorf("cli_llvm.kizu format closure missing seed %q", seed)
		}
	}
}

// assertFormatClosureExcludesHandPath keeps the indentation / import-sort / whole-formatter
// helpers out of the compiled closure so the migration cannot smuggle parse_format_alloc's
// logic in as a seed instead of lowering a real read-only component.
func assertFormatClosureExcludesHandPath(t *testing.T, irEmission, backendEmission string) {
	t.Helper()
	for _, helper := range formatHandPathOnlyHelpers {
		quoted := `"` + helper + `"`
		if strings.Contains(irEmission, quoted) {
			t.Errorf("executable_functions.kizu format closure seeds hand-path helper %q", helper)
		}
		if strings.Contains(backendEmission, quoted) {
			t.Errorf("cli_llvm.kizu format closure seeds hand-path helper %q", helper)
		}
	}
}

// assertParseFormatAllocNotExtended pins that the hand-written parse_format_alloc emitter is
// not grown and does not start calling the compiled formatter helpers.
func assertParseFormatAllocNotExtended(t *testing.T, parseLLVM string) {
	t.Helper()
	lines := parseFormatAllocFunctionLineCount(t, parseLLVM)
	if lines > parseFormatAllocMaxLines {
		t.Errorf("append_parse_format_alloc_function grew to %d lines (max %d) -- the formatter "+
			"migration must not extend the hand-written indentation / import-sort / comment logic",
			lines, parseFormatAllocMaxLines)
	}
	if strings.Contains(parseLLVM, "parser_format") {
		t.Errorf("cli_parse_llvm.kizu references a compiled parser_format symbol -- the compiled " +
			"formatter helpers must not be wired into the hand-written parse_format_alloc path")
	}
}

// parseFormatAllocFunctionLineCount returns the line span of the hand-written
// append_parse_format_alloc_function definition.
func parseFormatAllocFunctionLineCount(t *testing.T, parseLLVM string) int {
	t.Helper()
	lines := strings.Split(parseLLVM, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "fn append_parse_format_alloc_function") ||
			strings.HasPrefix(line, "pub fn append_parse_format_alloc_function") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("append_parse_format_alloc_function not found in cli_parse_llvm.kizu")
	}
	for i := start; i < len(lines); i++ {
		if lines[i] == "}" {
			return i - start + 1
		}
	}
	t.Fatalf("append_parse_format_alloc_function has no closing brace")
	return 0
}
