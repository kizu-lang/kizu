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

// formatDriverLoweringBlocker is the exact diagnostic the compiled MIR lowering now raises once the
// branch-merge induction latch lands. The induction-init probe (statement_is_induction_init ->
// while_carries_induction_var) used to error at while_trailing_increment_count because
// format_source's scan loop advances 'index' inside a terminal if/else branch-merge (a
// loop-terminating 'index = format_tokens.len()' on the eof arm and an 'index = index + 1' on the
// else arm) instead of ending in a trailing-increment Assign. while_carries_induction_var now
// recognizes that branch-merge latch (while_is_branch_merge_latch, compiled_mir_lower.kizu), so the
// probe classifies 'index' without erroring and lowering proceeds past the old blocker into the
// real statement lowering. The first top-level statement it lowers is
// 'var format_tokens = try lexer::tokenize(allocator, source)', whose call-return-type resolution
// (resolve_call_return_type, compiled_signature.kizu:180) needs the cross-module callee
// 'lexer::tokenize' function-signature-return fact -- which the gate's format_source-only IR facts
// do not carry -- so it raises "compiled signature: call return type not found". The lowering gate
// drives the real lowering over format_source's emitted IR facts and must stop here; this pins the
// measured next blocker as a behavior assertion rather than a comment (issue 1165 / 1162).
const formatDriverLoweringBlocker = "compiled signature: call return type not found"

// TestSelfhostFormatDriverLoweringGate emits the real format_source IR facts and drives the
// production compiled MIR lowering over them, asserting it reaches the measured next blocker. The
// branch-merge induction latch now carries the induction-init probe past the former
// "while body must end with the induction increment" stop, so the probe classifies the scan loop's
// 'index' counter and lowering advances into the real statement lowering, where the cross-module
// 'lexer::tokenize' call-return-type lookup is the next pinned blocker (issue 1165 / 1162).
func TestSelfhostFormatDriverLoweringGate(t *testing.T) {
	out, err := runSelfhostFormatDriverLoweringGate(t)
	if err == nil {
		t.Fatalf("format driver lowering gate unexpectedly succeeded\n%s", out)
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
