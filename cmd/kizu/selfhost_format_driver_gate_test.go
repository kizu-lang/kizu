package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostFormatDriverFactsGate drives the real component catalog + closure collector over
// the actual selfhost::parser::format::format_source body and asserts the driver call surface
// (the std::string::String constructor, the lexer::tokenize tokenizer entry, and the owned-handle
// '.deinit()') is classified by the production walk -- a behavior gate, not a source-text check
// (issue 1165 / 1162). The gate reads the on-disk format.kizu, so it runs from the repo root.
func TestSelfhostFormatDriverFactsGate(t *testing.T) {
	out, err := runSelfhostFormatDriverFactsGate(t)
	if err != nil {
		t.Fatalf("format driver facts gate failed: %v\n%s", err, out)
	}
	if out != "format-driver-walk-ok\n" {
		t.Fatalf("format driver facts gate output mismatch\nwant:\nformat-driver-walk-ok\ngot:\n%s", out)
	}
}

// runSelfhostFormatDriverFactsGate loads the selfhost package and runs the facts-layer gate.
func runSelfhostFormatDriverFactsGate(t *testing.T) (string, error) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		return "", err
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	const entry = "selfhost::ir::executable_functions::format_driver_facts_gate"
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}

// formatDriverCrossedLoweringBlockers are the lowering blockers this gate has already driven the
// real format_source lowering past. If any resurfaces, the lowering regressed and the gate must
// fail (issue 1165 / 1162):
//   - "compiled signature: call return type not found": the cross-module 'lexer::tokenize' callee
//     (resolved to its emitted '*::lexer::tokenize' signature via the generic cross-module
//     resolver) and the sibling component helpers (leading_import_indices, ...) now resolve their
//     return types from the emitted function-signature-return facts -- the gate emits every local
//     component signature, the same set the production IR carries.
//   - "compiled function: stdlib return not found": the std::string::String value constructor
//     ('var out = std::string::String(allocator)') now resolves through the shared stdlib-symbol
//     preamble (executable_functions::append_runtime_stdlib_symbol_preamble), the single source of
//     truth the production IR fact emission and this gate both supply.
//   - "compiled mir: unsupported call arg kind": the '&format_tokens' borrow argument at the
//     leading_import_indices call now lowers through the borrow-prefix call-arg path
//     (compiled_mir_lower_call::lower_single_call_arg) -- a borrow shares its pointee's ABI
//     representation, so the operand lowers with the resolved arg type.
var formatDriverCrossedLoweringBlockers = []string{
	"compiled signature: call return type not found",
	"compiled function: stdlib return not found",
	"compiled mir: unsupported call arg kind",
}

// formatDriverLoweringBlocker is the exact diagnostic the compiled MIR lowering now raises. With
// format_source's first statements lowered -- the cross-module 'lexer::tokenize' tokenizer, the
// 'var out = std::string::String(allocator)' constructor (stdlib-symbol preamble), and the
// 'leading_import_indices(allocator, source, &format_tokens)' sibling call with its
// '&format_tokens' borrow argument -- lowering advances to format_source's first top-level 'if'
// statement 'if leading_imports.len() > 1 { ... }' (selfhost/src/parser/format.kizu:28). Its
// then-block is a void multi-statement body (let bindings, a nested if, sibling/method call
// statements, and several assignments to outer vars), but the compiled if-lowering only supports a
// continue-latch then-block or a single-Return then-block
// (selfhost/src/backend/compiled_mir_lower.kizu:4900), so it raises
// "compiled mir: unsupported then block". Lowering a general void multi-statement if-then block is
// the next capability -- a multi-part blocker (statement-list branch bodies, nested if, call
// statements, multi-assign) -- so the gate stops here and pins the measured next blocker as a
// behavior assertion rather than a comment (issue 1165 / 1162).
const formatDriverLoweringBlocker = "compiled mir: unsupported then block"

// TestSelfhostFormatDriverLoweringGate emits the real format_source IR facts and drives the
// production compiled MIR lowering over them, asserting it reaches the measured next blocker. The
// shared stdlib-symbol preamble, the full component signature set, and the borrow-prefix call-arg
// lowering now carry format_source past the std::string::String constructor and the
// leading_import_indices sibling call (with its '&format_tokens' borrow argument), so lowering
// advances into the first top-level 'if' statement, where the void multi-statement then-block is
// the next pinned blocker (issue 1165 / 1162).
func TestSelfhostFormatDriverLoweringGate(t *testing.T) {
	out, err := runSelfhostFormatDriverLoweringGate(t)
	if err == nil {
		t.Fatalf("format driver lowering gate unexpectedly succeeded\n%s", out)
	}
	for _, blocker := range formatDriverCrossedLoweringBlockers {
		if strings.Contains(err.Error(), blocker) {
			t.Fatalf(
				"format driver lowering gate regressed to a crossed blocker %q: %v\n%s",
				blocker, err, out,
			)
		}
	}
	if !strings.Contains(err.Error(), formatDriverLoweringBlocker) {
		t.Fatalf("format driver lowering gate stopped at an unexpected blocker: %v\n%s", err, out)
	}
}

// runSelfhostFormatDriverLoweringGate loads the selfhost package and drives the lowering gate.
func runSelfhostFormatDriverLoweringGate(t *testing.T) (string, error) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		return "", err
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	const entry = "selfhost::backend::format_driver_gate::format_driver_lowering_gate"
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}
