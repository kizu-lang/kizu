package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseFormatAllocMaxCodeLines pins the hand-written parse_format_alloc LLVM emitter at its
// current size. The formatter migration (issue 1165 / issue 1162) moves real
// selfhost::parser::format helpers onto the compiled path; it must not grow the legacy
// hand-written indentation / import-sort / comment logic in append_parse_format_alloc_function.
//
// The ceiling counts code lines, skipping comments and blanks. Counting raw lines also fired on
// documentation: commit 5689d099 relocated one already-emitted line below its block's phis (LLVM
// groups a block's phis at the top) and explained why in two comment lines, which tripped a
// 197-raw-line ceiling while emitting exactly the same LLVM. Counting emission statements
// instead would fix that but stop being a size ceiling at all -- growth spelled with any other
// helper, including a wall of hardcoded static LLVM, would pass.
//
// 197 is the code-line count at 5a579a65, before the WIP checkpoint, and at every revision
// since, so the ceiling has zero headroom: one added line of code in any idiom fails the gate.
const parseFormatAllocMaxCodeLines = 197

// The per-helper seed list that used to live here (formatCompiledHelperSeeds, one entry per
// compiled selfhost::parser::format member, checked for by name against the two emission
// sources) is gone with the mechanism it described. The semantic package graph derives closure
// membership from the real format.kizu call graph, so no emitter names a helper any more --
// assertFormatClosurePackageGraphDriven below asserts the opposite of what that list needed,
// that no hand-written per-component closure table survives. Per-member coverage now sits at
// the artifact level in TestSelfhostBackendArtifactGate, which pins the emitted
// 'define ... @kizu_selfhost__parser_format_<member>' definitions; git history at ffb2306f keeps
// the per-helper shape notes.

// TestSelfhostFormatHelperStructuralGate pins that the first selfhost::parser::format
// read-only helpers stay on the compiled path through the shared component catalog, and
// that the migration does not extend the hand-written parse_format_alloc emitter. It is a
// source-structural gate, so it runs without the bootstrap or clang.
func TestSelfhostFormatHelperStructuralGate(t *testing.T) {
	irEmission := readSelfhostSrc(t, filepath.Join("ir", "executable_functions.kizu"))
	backendEmission := readSelfhostSrc(t, filepath.Join("backend", "compiled_program_llvm.kizu"))
	parseLLVM := readSelfhostSrc(t, filepath.Join("backend", "cli_parse_llvm.kizu"))

	assertFormatClosurePackageGraphDriven(t, irEmission, backendEmission)
	assertParseFormatAllocNotExtended(t, parseLLVM)
	assertImportSortShapeValidated(t)
	assertLeadingImportShapeValidated(t)
	assertImportDeclShapeValidated(t)
	assertSortedImportsShapeValidated(t)
	assertAppendIndentShapeValidated(t)
	assertCommentPreserveShapeValidated(t)
	assertIfNotTryGuardShapeValidated(t)
	assertVoidThenBlockShapeValidated(t)
}

// ifNotTryGuardShapeValidationErrors pins the explicit shape-validation diagnostic that the
// prefix-not-of-try-call lowering raises when an 'if !(try <call>) { return <bool>; }' guard drifts
// from its then-only skeleton. The if-not-try-call-return-bool statement renders a then-only
// conditional branch (the negation returns the bool or falls through to the join the following
// statements render into), so an else arm with a real body would be silently dropped -- it must be
// an explicit error rather than a silent mis-lower. Pinning the string keeps the else-absence check
// from being quietly removed.
var ifNotTryGuardShapeValidationErrors = []string{
	"compiled mir: if-not-try guard must not carry an else arm",
}

// assertIfNotTryGuardShapeValidated pins that the prefix-not-of-try-call lowering keeps its
// explicit else-absence diagnostic for the 'if !(try <call>) { return <bool>; }' guard, so a
// near-miss helper whose guard carries an else arm is rejected with an error rather than silently
// lowered to the then-only skeleton.
func assertIfNotTryGuardShapeValidated(t *testing.T) {
	t.Helper()
	mirLower := readSelfhostSrc(t, filepath.Join("backend", "compiled_mir_lower.kizu"))
	for _, diagnostic := range ifNotTryGuardShapeValidationErrors {
		if !strings.Contains(mirLower, diagnostic) {
			t.Errorf("compiled_mir_lower.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// voidThenBlockShapeValidationErrors pins the explicit shape-validation diagnostic the no-else void
// multi-statement then-block lowering raises when the then-block carries an else arm. The void body
// is rendered (once the renderer lands) as a then-only conditional branch into a fall-through join,
// so an else arm with a real body would be silently dropped -- it must be an explicit error rather
// than a silent mis-lower. An absent or 'Empty' else child is accepted. Pinning the string keeps
// the else-absence check from being quietly removed before the renderer exists to honor it.
var voidThenBlockShapeValidationErrors = []string{
	"compiled mir: void then block must not carry an else arm",
}

// assertVoidThenBlockShapeValidated pins that the no-else void multi-statement then-block lowering
// keeps its explicit else-absence diagnostic, so a then-block that carries a real else arm is
// rejected with an error rather than descended into with the else arm silently dropped.
func assertVoidThenBlockShapeValidated(t *testing.T) {
	t.Helper()
	mirLower := readSelfhostSrc(t, filepath.Join("backend", "compiled_mir_lower.kizu"))
	for _, diagnostic := range voidThenBlockShapeValidationErrors {
		if !strings.Contains(mirLower, diagnostic) {
			t.Errorf("compiled_mir_lower.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// commentPreserveShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the
// comment-preservation driver skeleton. append_comment_preserve_function reads only some
// identifiers off the AST and then emits a fixed forward-scan-loop / if-else comment-arm /
// CommentFormatState success-wrap shape, so it must reject any helper whose accumulator, source,
// scalar-state parameters, cursor seed, loop condition, if/else arms, comment-arm statements,
// blank-before / blank-after guards, cursor advance or state return differ -- never silently
// mis-lower a near-miss. Pinning the strings keeps the per-operand validation from being quietly
// weakened back to a name-only check.
var commentPreserveShapeValidationErrors = []string{
	"compiled struct-cf: comment driver out accumulator must be a String handle",
	"compiled struct-cf: comment driver source must be a '[]u8' slice",
	"compiled struct-cf: comment driver last must be a 'u8'",
	"compiled struct-cf: comment driver at_line_start must be a 'bool'",
	"compiled struct-cf: comment driver index must be seeded from the start parameter",
	"compiled struct-cf: comment loop condition must be '<index> < <end>'",
	"compiled struct-cf: comment loop must compare the cursor on the left",
	"compiled struct-cf: comment loop must compare against the end parameter",
	"compiled struct-cf: comment loop body must be a single if/else",
	"compiled struct-cf: comment arm must be the 12-statement comment emitter",
	"compiled struct-cf: comment loop else arm must be the bare cursor advance",
	"compiled struct-cf: else arm must be '<index> + 1'",
	"compiled struct-cf: else arm must add to the cursor",
	"compiled struct-cf: comment arm must open with the full-line predicate",
	"compiled struct-cf: blank-before guard must be 'full and has_blank_before'",
	"compiled struct-cf: comment arm must close by advancing the cursor",
	"compiled struct-cf: comment driver must end with the state return",
	// Top-level state seeds.
	"compiled struct-cf: comment driver current_last must be seeded from the last parameter",
	"compiled struct-cf: comment driver current_after_comment must be seeded from false",
	// The short-circuit '//' detection guard.
	"compiled struct-cf: comment guard must be the three-operand '//' detection 'and'",
	"compiled struct-cf: comment guard bounds check must be 'index + 1 < end'",
	"compiled struct-cf: comment guard must test 'source[index] == cast<u8>(47)'",
	"compiled struct-cf: comment guard must test 'source[index + 1] == cast<u8>(47)'",
	// The blank-before insertion operands.
	"compiled struct-cf: blank-before insertion must test 'out.len() > 0'",
	"compiled struct-cf: blank-before insertion must set current_last = 10",
	"compiled struct-cf: blank-before insertion must set current_at_line_start = true",
	// The main comment emit operands.
	"compiled struct-cf: append_indent must take (out, depth)",
	"compiled struct-cf: line_end_excluding_break must scan from 'index + 2'",
	"compiled struct-cf: comment slice must be 'source[index..comment_end]'",
	"compiled struct-cf: comment arm must set current_after_comment = true",
	// The blank-after insertion operands.
	"compiled struct-cf: has_blank_after must take (source, comment_end, end)",
	"compiled struct-cf: line_end_including_break must scan from 'index + 2'",
	// The CommentFormatState success-wrap fields.
	"compiled struct-cf: CommentFormatState literal must have exactly three fields",
	"compiled struct-cf: CommentFormatState 'last' field must be current_last",
	"compiled struct-cf: CommentFormatState 'at_line_start' field must be current_at_line_start",
	"compiled struct-cf: CommentFormatState 'after_comment' field must be current_after_comment",
	// Exact signature / top-level body shape.
	"compiled struct-cf: comment driver must take exactly seven parameters",
	"compiled struct-cf: comment driver body must be exactly six statements",
	// The then-only comment-arm guards must carry no else arm.
	"compiled struct-cf: blank-before insertion must not carry an else arm",
	"compiled struct-cf: blank-before guard must not carry an else arm",
	"compiled struct-cf: blank-after guard must not carry an else arm",
}

// assertCommentPreserveShapeValidated pins that the structured-control-flow lowering keeps its
// explicit per-operand shape diagnostics for the comment-preservation driver, so a near-miss
// helper is rejected with an error rather than silently lowered to the hard-coded forward-scan /
// comment-arm / CommentFormatState success-wrap skeleton.
func assertCommentPreserveShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range commentPreserveShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// appendIndentShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the indentation
// emitter skeleton. compiled_struct_cf reads only some identifiers off the AST and then emits a
// fixed counter-loop / String-append shape, so it must reject any helper whose accumulator, depth
// parameter, counter seed, loop condition, indent literal append or increment differ -- never
// silently mis-lower a near-miss. Pinning the strings keeps the per-operand validation from being
// quietly weakened back to name-only checks.
var appendIndentShapeValidationErrors = []string{
	"compiled struct-cf: indent emitter out accumulator must be a String handle",
	"compiled struct-cf: indent emitter depth must be an i64 parameter",
	"compiled struct-cf: indent counter must be initialized to 0",
	"compiled struct-cf: indent loop condition must be '<index> < <depth>'",
	"compiled struct-cf: indent loop must compare the counter on the left",
	"compiled struct-cf: indent loop must compare against the depth parameter",
	"compiled struct-cf: indent body must append, then increment",
	"compiled struct-cf: indent body must append the indent literal",
	"compiled struct-cf: indent append must be a string literal",
	"compiled struct-cf: indent counter increment must be '<index> + 1'",
}

// assertAppendIndentShapeValidated pins that the structured-control-flow lowering keeps its
// explicit per-operand shape diagnostics for the indentation emitter, so a near-miss helper is
// rejected with an error rather than silently lowered to the hard-coded counter-loop /
// String-append skeleton.
func assertAppendIndentShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range appendIndentShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
}

// sortedImportsShapeValidationErrors pins the explicit shape-validation diagnostics that the
// structured-control-flow lowering raises when a near-miss helper drifts from the import-cluster
// emitter skeleton. compiled_struct_cf reads only some identifiers off the AST and then emits a
// fixed scan-loop / get / try-call / newline-append shape, so it must reject any helper whose
// accumulator, scan condition, index read, single-import call operands, newline append or
// increment differ -- never silently mis-lower a near-miss.
var sortedImportsShapeValidationErrors = []string{
	"compiled struct-cf: import cluster out accumulator must be a String handle",
	"compiled struct-cf: scan loop condition must be '<index> < <indices>.len()'",
	"compiled struct-cf: index read must target the indices array",
	"compiled struct-cf: scan body must call the single-import emitter",
	"compiled struct-cf: single-import call must pass the out accumulator first",
	"compiled struct-cf: single-import call must pass the source second",
	"compiled struct-cf: single-import call must pass the tokens third",
	"compiled struct-cf: single-import call must pass the import index last",
	"compiled struct-cf: scan body must append the newline byte",
	"compiled struct-cf: newline append must be a 'cast<u8>(<int>)'",
	"compiled struct-cf: newline append must cast to u8",
	"compiled struct-cf: counter increment must be '<index> + 1'",
}

// assertSortedImportsShapeValidated pins that the structured-control-flow lowering keeps its
// explicit per-operand shape diagnostics for the import-cluster emitter, so a near-miss helper is
// rejected with an error rather than silently lowered to the hard-coded scan-loop skeleton.
func assertSortedImportsShapeValidated(t *testing.T) {
	t.Helper()
	structCF := readSelfhostSrc(t, filepath.Join("backend", "compiled_struct_cf.kizu"))
	for _, diagnostic := range sortedImportsShapeValidationErrors {
		if !strings.Contains(structCF, diagnostic) {
			t.Errorf("compiled_struct_cf.kizu missing shape-validation diagnostic: %q", diagnostic)
		}
	}
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

// assertFormatClosurePackageGraphDriven pins that formatter facts flow through
// the semantic package graph while the backend resolves each compiled member
// from signature facts. The fact entry (append_facts_from_parsed) and the closure
// walk (append_numeric_package_closure) are named explicitly because they are the
// generic replacements for the deleted per-component format fact collector; the
// forbidden list is what keeps that collector from growing back.
func assertFormatClosurePackageGraphDriven(t *testing.T, irEmission, backendEmission string) {
	t.Helper()
	irRequired := []string{
		`package_catalog_collect::collect_from_parsed_files`,
		`package_dependency_graph::dependency_graph`,
		`package_call_resolution::append_resolved_dependencies`,
		`append_numeric_package_definition(`,
		`fn append_facts_from_parsed(`,
		`fn append_numeric_package_closure(`,
	}
	for _, fragment := range irRequired {
		if !strings.Contains(irEmission, fragment) {
			t.Errorf("executable_functions.kizu missing package-graph emission: %q", fragment)
		}
	}
	for _, fragment := range []string{
		`component_function_catalog::`,
		`collect_catalog_closure_`,
		`append_format_function_facts`,
	} {
		if strings.Contains(irEmission, fragment) {
			t.Errorf("executable_functions.kizu retains legacy closure path: %q", fragment)
		}
	}
	backendRequired := []string{
		`pub fn append_reachable_functions`,
		`fn emit_numeric_package_definition`,
		`compiled_llvm::append_compiled_function_auto_indexed`,
	}
	for _, fragment := range backendRequired {
		if !strings.Contains(backendEmission, fragment) {
			t.Errorf("compiled_program_llvm.kizu missing generic package emission: %q", fragment)
		}
	}
	for _, fragment := range []string{
		`append_format_reachable_compiled_functions`,
		`append_component_reachable_compiled_functions`,
	} {
		if strings.Contains(backendEmission, fragment) {
			t.Errorf("compiled_program_llvm.kizu retains manual format closure: %q", fragment)
		}
	}
}

// format_source's own membership in the compiled closure is no longer checked here by looking
// for its name in the backend emitter: under the package graph the backend names no member. It
// is rooted in the external ABI manifest, where
// TestSelfhostExternalABIEntrypointsOwnCompiledPackageRoots pins the entry and its exact semantic
// signature against a closed root count, and assertParseFormatAllocNotExtended below pins that
// hosted fmt calls the compiled symbol.

// The format_source driver call surface (the std::string::String(allocator) constructor, the
// lexer::tokenize tokenizer entry, and the owned-handle '.deinit()') is no longer pinned here as a
// source-text presence check (issue 1165 / 1162). TestSelfhostFormatDriverFactsGate exercises the
// production component catalog + closure collector over the real
// selfhost::parser::format::format_source body, TestSelfhostFormatDriverLoweringGate asserts that
// body reaches full lowering/rendering success, and TestSelfhostBackendArtifactGate pins that
// hosted fmt calls the compiled driver wrappers. Those behavior gates supersede the former
// string-presence assertion.

// assertParseFormatAllocNotExtended pins that the old hand-written parse_format_alloc emitter is
// not grown for the formatter path, and that hosted fmt now calls the compiled formatter driver.
func assertParseFormatAllocNotExtended(t *testing.T, parseLLVM string) {
	t.Helper()
	code := parseFormatAllocCodeLineCount(t, parseLLVM)
	if code > parseFormatAllocMaxCodeLines {
		t.Errorf("append_parse_format_alloc_function grew to %d code lines (max %d) -- the "+
			"formatter migration must not extend the hand-written indentation / import-sort / "+
			"comment logic", code, parseFormatAllocMaxCodeLines)
	}
	if !strings.Contains(parseLLVM, "@kizu_selfhost__parser_format_format_source") {
		t.Errorf("cli_parse_llvm.kizu does not call the compiled format_source driver")
	}
	if !strings.Contains(parseLLVM, "@kizu_selfhost__format_source_write") ||
		!strings.Contains(parseLLVM, "@kizu_selfhost__format_source_file_write") {
		t.Errorf("cli_parse_llvm.kizu does not route fmt through compiled format_source wrappers")
	}
	if strings.Contains(parseLLVM, "%fmt_format_ok = call i1 @kizu_selfhost__parse_format_write") ||
		strings.Contains(
			parseLLVM,
			"%fmt_write_format_ok = call i1 @kizu_selfhost__parse_format_file_write",
		) {
		t.Errorf("cli_parse_llvm.kizu still routes fmt through parse_format_alloc wrappers")
	}
	if !strings.Contains(parseLLVM, "call void @kizu_rt_owned_deinit(%kizu.owned %formatted_owned)") {
		t.Errorf("cli_parse_llvm.kizu does not clean up the compiled formatter String handle")
	}
	if !strings.Contains(parseLLVM, "%format_write_newline_slice") ||
		!strings.Contains(parseLLVM, "%format_newline = call %kizu.error.void @kizu_rt_io_write_stdout") {
		t.Errorf("cli_parse_llvm.kizu does not mirror write_parse_success stdout newline")
	}
}

// parseFormatAllocCodeLineCount returns the number of code lines in the hand-written
// append_parse_format_alloc_function definition: the span from its header line through the
// first column-0 closing brace, skipping blanks and comment-only lines so documenting the
// body does not register as growth.
func parseFormatAllocCodeLineCount(t *testing.T, parseLLVM string) int {
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
			code := 0
			for _, body := range lines[start : i+1] {
				trimmed := strings.TrimSpace(body)
				if trimmed == "" || strings.HasPrefix(trimmed, "//") {
					continue
				}
				code++
			}
			return code
		}
	}
	t.Fatalf("append_parse_format_alloc_function has no closing brace")
	return 0
}
