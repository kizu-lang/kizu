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

// formatDriverPriorLoweringBlocker is the blocker the cross-module callee resolution now crosses.
// format_source's first statement 'var format_tokens = try lexer::tokenize(allocator, source)'
// reaches the tokenizer through the 'import selfhost::lexer' alias; the compiled signature lowering
// used to look up 'lexer::tokenize' under the caller's module prefix
// (selfhost::parser::format::lexer::tokenize), found no function-signature-return fact, and raised
// "compiled signature: call return type not found". The generic cross-module resolver
// (cross_module_callee_qualified_name_or_empty ->
// compiled_fact_lookup::lookup_qualified_function_name_by_callee_suffix) now resolves the
// alias-qualified callee to the emitted '*::lexer::tokenize' signature (std::kizu::lexer::tokenize,
// the real tokenizer entry the selfhost::lexer wrapper forwards to) over real facts, and the
// try-call success-type + Allocator arg-type lowering carry the rest of the statement. If this old
// blocker resurfaces the resolution regressed, so the gate must fail (issue 1165 / 1162).
const formatDriverPriorLoweringBlocker = "compiled signature: call return type not found"

// formatDriverLoweringBlocker is the exact diagnostic the compiled MIR lowering now raises. With
// format_source's first statement fully lowered (the cross-module 'lexer::tokenize' tokenizer
// crossing), lowering advances to the second top-level statement
// 'var out = std::string::String(allocator)' (selfhost/src/parser/format.kizu:14). The String value
// constructor is a stdlib callee lowered through the stdlib-symbol path
// (lookup_stdlib_return, compiled_fact_lookup.kizu:335), but the gate emits format_source's own
// signature/body plus the tokenizer signature only -- not the global stdlib-symbol preamble the
// production IR carries -- so the constructor's 'stdlib-symbol std::string::String' fact is absent
// and lowering raises "compiled function: stdlib return not found". The lowering gate drives the
// real lowering over format_source's emitted IR facts and must stop here; this pins the measured
// next blocker as a behavior assertion rather than a comment (issue 1165 / 1162).
const formatDriverLoweringBlocker = "compiled function: stdlib return not found"

// TestSelfhostFormatDriverLoweringGate emits the real format_source IR facts and drives the
// production compiled MIR lowering over them, asserting it reaches the measured next blocker. The
// generic cross-module callee resolution now carries format_source's first statement past the
// former "call return type not found" stop -- the alias-qualified 'lexer::tokenize' resolves to its
// real emitted signature, its try-call success type unwraps the !Array<Token> error union, and the
// Allocator argument type lowers -- so lowering advances into the second statement, where the
// std::string::String value constructor's missing stdlib-symbol fact is the next pinned blocker
// (issue 1165 / 1162).
func TestSelfhostFormatDriverLoweringGate(t *testing.T) {
	out, err := runSelfhostFormatDriverLoweringGate(t)
	if err == nil {
		t.Fatalf("format driver lowering gate unexpectedly succeeded\n%s", out)
	}
	if strings.Contains(err.Error(), formatDriverPriorLoweringBlocker) {
		t.Fatalf("format driver lowering gate regressed to the prior blocker: %v\n%s", err, out)
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
