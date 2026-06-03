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
// and calling lexer::is_eof across the module boundary), index_after_import (the first
// token-array scan-while helper, a bounded counter loop with a parameter-seeded induction
// variable and i64 early returns), and index_after_leading_imports (the first scan-while
// whose loop latch is a loop-carried try-call, 'index = try index_after_import(tokens,
// index);', feeding the loop-head phi from the try-call success value), and compare_bytes
// (the first import-sort helper, an i64 byte comparison whose loop header is a short-circuit
// `and` of two comparisons over pure length locals with []u8 index loads in the body guards),
// and import_path_less (the first multi-counter helper: two lockstep token cursors with
// base-plus-offset inits advanced by a trailing run of constant-step increments under a
// short-circuit `and` header, with a nested-call-argument compare_bytes let and a prefix-not
// call return), and sort_import_indices (the first structured-control-flow helper: the
// scan-shift insertion sort with an outer counter loop, a nested scan loop carrying a cursor
// and a boolean flag through loop-head phis, an if/else that re-merges into the loop, a
// try-call import_path_less condition, and Array<i64>.get/set element reads/writes through the
// element-size-generic @kizu_rt_array_at / @kizu_rt_array_set ABI on its '&var
// std::array::Array<i64>' parameter, lowered through compiled_struct_cf), and
// leading_import_indices (the first structured-control-flow collector: it builds a
// runtime-owned Array<i64> through the element-size-generic @kizu_rt_array_new /
// @kizu_rt_array_append ABI, scans the token array through an if/else branch-merge loop -- a
// loop-terminating 'index = tokens.len()' on one arm, an append-then-try-advance on the other
// re-merging at a loop-latch phi -- then sorts the collected indices in place and returns the
// filled array as the '!std::array::Array<i64>' success (a %kizu.error.owned wrap), lowered
// through compiled_struct_cf). They are the first selfhost::parser::format members on the
// compiled path and must keep being emitted from both the IR fact catalog and the backend BFS.
var formatCompiledHelperSeeds = []string{
	"is_import_token",
	"is_ident_token",
	"is_double_colon_token",
	"is_semicolon_token",
	"token_text",
	"next_token_text_equals",
	"index_after_import",
	"index_after_leading_imports",
	"compare_bytes",
	"import_path_less",
	"sort_import_indices",
	"leading_import_indices",
	"append_import_decl",
}

// formatHandPathOnlyHelpers stay out of the compiled format closure: they are the
// indentation / import-sort / comment / whole-formatter helpers whose logic lives in the
// hand-written parse_format_alloc path. Seeding them would smuggle the legacy formatter
// logic into the compiled closure instead of lowering it as a real component.
var formatHandPathOnlyHelpers = []string{
	"format_source",
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
	assertImportSortShapeValidated(t)
	assertLeadingImportShapeValidated(t)
	assertImportDeclShapeValidated(t)
}

// importDeclShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the single-import
// emitter skeleton. compiled_struct_cf reads only some identifiers off the AST and then emits a
// fixed String-append / scan-loop / early-return / boolean-or shape, so it must reject any helper
// whose accumulator, prefix append, counter seed, scan condition, token read, token_text call,
// semicolon branch, text guard, text append or increment differ -- never silently mis-lower a
// near-miss. Pinning the strings keeps the per-operand validation from being quietly weakened back
// to name-only checks.
var importDeclShapeValidationErrors = []string{
	"compiled struct-cf: import emitter out accumulator must be a String handle",
	"compiled struct-cf: import emitter must open with the 'import ' literal append",
	"compiled struct-cf: import emitter prefix append must be a string literal",
	"compiled struct-cf: scan counter must be seeded as '<import_index> + 1'",
	"compiled struct-cf: scan counter seed must add to an i64 import index parameter",
	"compiled struct-cf: scan loop condition must be '<index> < <tokens>.len()'",
	"compiled struct-cf: token read must index the scan counter",
	"compiled struct-cf: token_text must pass the source first",
	"compiled struct-cf: token_text must pass the current token second",
	"compiled struct-cf: semicolon guard must test the current token",
	"compiled struct-cf: semicolon branch must append the ';' literal",
	"compiled struct-cf: semicolon branch return must be a bare 'return;'",
	"compiled struct-cf: text guard must be 'ident or double-colon'",
	"compiled struct-cf: text branch must append the token text local",
	"compiled struct-cf: counter increment must be '<index> + 1'",
}

// assertImportDeclShapeValidated pins that the structured-control-flow lowering keeps its explicit
// per-operand shape diagnostics for the single-import emitter, so a near-miss helper is rejected
// with an error rather than silently lowered to the hard-coded String-append / scan-loop skeleton.
func assertImportDeclShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range importDeclShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// leadingImportShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the
// leading-import collector skeleton. compiled_struct_cf reads only some identifiers off the
// AST and then emits a fixed array-constructor / branch-merge / try-call / owned-wrap shape,
// so it must reject any helper whose constructor, scan condition, predicate negation, branch
// assignments, append target, advance arguments, sort call or return shape differ -- never
// silently mis-lower a near-miss. Pinning the strings keeps the per-operand validation from
// being quietly weakened back to name-only checks.
var leadingImportShapeValidationErrors = []string{
	"compiled struct-cf: leading-import collector must open with the array constructor",
	"compiled struct-cf: array constructor takes exactly the allocator argument",
	"compiled struct-cf: array constructor allocator must be a parameter",
	"compiled struct-cf: scan counter must be initialized to an integer literal",
	"compiled struct-cf: scan loop condition must be '<index> < <tokens>.len()'",
	"compiled struct-cf: token read must index the scan counter",
	"compiled struct-cf: predicate must be negated with '!'",
	"compiled struct-cf: predicate must test the current token",
	"compiled struct-cf: scan terminator must be '<index> = <tokens>.len()'",
	"compiled struct-cf: append must target the indices array",
	"compiled struct-cf: append must store the scan counter",
	"compiled struct-cf: scan advance must pass the token array first",
	"compiled struct-cf: scan advance must pass the scan counter second",
	"compiled struct-cf: sort call takes the source, the token array, and the indices handle",
	"compiled struct-cf: sort call must pass the source first",
	"compiled struct-cf: sort call must pass the token array second",
	"compiled struct-cf: sort call must pass the indices array by mutable borrow",
	"compiled struct-cf: sort call indices argument must be a borrow, not another prefix",
	"compiled struct-cf: sort call must borrow the constructed indices array",
	"compiled struct-cf: collector must return the filled indices array",
}

// assertLeadingImportShapeValidated pins that the structured-control-flow lowering keeps its
// explicit per-operand shape diagnostics for the leading-import collector, so a near-miss
// helper is rejected with an error rather than silently lowered to the hard-coded
// array-constructor / branch-merge skeleton.
func assertLeadingImportShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range leadingImportShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// importSortShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the
// scan-shift insertion sort skeleton. compiled_struct_cf reads only some identifiers off
// the AST and then emits a fixed basic-block / phi shape, so it must reject any helper
// whose operators, operands, swap indices, branch assignments, increment or return shape
// differ -- never silently mis-lower a near-miss. Pinning the strings keeps the
// per-operand validation from being quietly weakened back to name-only checks.
var importSortShapeValidationErrors = []string{
	"compiled struct-cf: outer counter must be initialized to 1",
	"compiled struct-cf: outer loop condition must be '<counter> < <array>.len()'",
	"compiled struct-cf: outer loop must compare the counter on the left",
	"compiled struct-cf: cursor must be seeded from the outer counter",
	"compiled struct-cf: comparator must take the high slot ('<right>') first",
	"compiled struct-cf: comparator must take the low slot ('<left>') second",
	"compiled struct-cf: scanning flag must be seeded to true",
	"compiled struct-cf: scan loop header must be '<scanning> and <cursor> > 0'",
	"compiled struct-cf: scan loop header must guard '<cursor> > 0'",
	"compiled struct-cf: left read must index '<cursor> - 1'",
	"compiled struct-cf: right read must index the cursor",
	"compiled struct-cf: low swap must write index '<cursor> - 1'",
	"compiled struct-cf: low swap must store the right element",
	"compiled struct-cf: high swap must write the cursor index",
	"compiled struct-cf: high swap must store the left element",
	"compiled struct-cf: cursor decrement must be '<cursor> - 1'",
	"compiled struct-cf: else-block must set scanning to false",
	"compiled struct-cf: outer increment must be '<counter> + 1'",
	"compiled struct-cf: helper must end with a bare 'return;'",
}

// assertImportSortShapeValidated pins that the structured-control-flow lowering keeps its
// explicit per-operand shape diagnostics, so a near-miss helper is rejected with an error
// rather than silently lowered to the hard-coded insertion-sort skeleton.
func assertImportSortShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range importSortShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
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
