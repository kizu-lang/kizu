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
//   - "compiled mir: unsupported then block": the first top-level 'if leading_imports.len() > 1
//     { ... }' carries a no-else void multi-statement then-block, not a single Return. The
//     if-lowering now recognizes that shape and descends through lower_void_then_body, dispatching
//     each then-body statement by kind through the same per-kind lowering the top-level body loop
//     uses. The leading 'let next_index = try index_after_leading_imports(&format_tokens);' binding
//     lowers through that descent, so lowering advances into the then-body rather than stopping at
//     the Return-only then-kind path.
//   - "compiled mir: local type not found": the '.get()' receiver 'format_tokens' is bound by the
//     top-level cross-module try-call 'var format_tokens = try lexer::tokenize(allocator, source)'.
//     resolve_let_value_kizu_type now reuses the generic cross-module callee suffix resolver,
//     reads the real std::kizu::lexer::tokenize function-signature-return fact, and unwraps
//     '!std::array::Array<std::kizu::lexer::Token>' to the local's Kizu type.
var formatDriverCrossedLoweringBlockers = []string{
	"compiled signature: call return type not found",
	"compiled function: stdlib return not found",
	"compiled mir: unsupported call arg kind",
	"compiled mir: unsupported then block",
	"compiled mir: local type not found",
}

// formatDriverLoweringBlocker is the exact diagnostic the compiled MIR lowering now raises. With
// the no-else void multi-statement then-block descending through lower_void_then_body, and with
// cross-module try-call locals resolving their Kizu success type, lowering now reaches the
// then-block's second statement
// 'let last_import = try format_tokens.get(next_index - 1);'
// (selfhost/src/parser/format.kizu:30). lower_multi_let_array_get resolves the receiver local
// 'format_tokens' to 'std::array::Array<std::kizu::lexer::Token>', extracts the element type
// 'std::kizu::lexer::Token', and calls lookup_type_llvm_by_prefix at
// selfhost/src/backend/compiled_mir_lower.kizu:4013. That lookup raises
// "compiled function: type-llvm mapping not found" from
// selfhost/src/backend/compiled_fact_lookup.kizu:40 because the format driver lowering facts do
// not yet carry the cross-module Token type-llvm/struct facts reached through lexer::tokenize's
// return payload. Making those real cross-module payload type facts available is the next
// capability, so the gate pins this measured blocker as a behavior assertion (issue 1197).
const formatDriverLoweringBlocker = "compiled function: type-llvm mapping not found"

// TestSelfhostFormatDriverLoweringGate emits the real format_source IR facts and drives the
// production compiled MIR lowering over them, asserting it reaches the measured next blocker. The
// no-else void multi-statement then-block of the first top-level 'if' now descends through
// lower_void_then_body, lowering its leading 'let next_index = try
// index_after_leading_imports(&format_tokens);' binding, so lowering advances into the then-body to
// its second statement 'let last_import = try format_tokens.get(next_index - 1);'. The next pinned
// blocker is resolving the LLVM type for the cross-module Token element read by that Array.get
// (issue 1197).
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
