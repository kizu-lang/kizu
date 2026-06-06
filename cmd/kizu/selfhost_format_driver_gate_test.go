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
//     resolve_let_value_kizu_type reuses the generic cross-module callee suffix resolver, reads the
//     real std::kizu::lexer::tokenize function-signature-return fact, and unwraps
//     '!std::array::Array<std::kizu::lexer::Token>' to the local's Kizu type. The same blocker is
//     also crossed for the if-then body local 'last_import' read by 'last_import.end'
//     (selfhost/src/parser/format.kizu:31): resolve_local_in_block_or_empty now descends into an
//     if-then/else block, the same way it descends into a while body, so a local bound inside the
//     then-block resolves its Kizu type for later statements in that block.
//   - "compiled function: type-llvm mapping not found": the then-block's second statement
//     'let last_import = try format_tokens.get(next_index - 1);'
//     (selfhost/src/parser/format.kizu:30) lowers an Array.get over the cross-module
//     'std::array::Array<std::kizu::lexer::Token>' payload, so lower_multi_let_array_get extracts
//     the element type 'std::kizu::lexer::Token' and the 'last_import.end' field read resolves its
//     struct layout. The gate now supplies the cross-module return payload type facts -- the
//     std::kizu::lexer::Token type-llvm + struct-field facts -- by deriving the payload struct from
//     the real lexer::tokenize signature and the lexer AST (the same source of truth the production
//     IR fact emission carries), so both the element LLVM type and the field lookup resolve.
//   - "compiled mir: unsupported then-body statement kind": the then-block's third statement
//     'if !has_line_comment_between(source, 0, last_import.end) { ... }'
//     (selfhost/src/parser/format.kizu:31) is a nested 'if'. lower_void_then_body now dispatches
//     the 'If' and 'ExprStmt' kinds too -- mirroring the top-level body loop's per-kind dispatch --
//     so the nested 'if' lowers through lower_multi_if_statement, which recognizes its void
//     then-block shape and descends back into lower_void_then_body for the inner statement list.
//     Lowering advances into the nested then-body rather than stopping at the outer dispatch.
//   - "compiled mir: try call statement must be a method call": the nested then-body's first
//     statement 'try append_sorted_imports(out, source, &format_tokens, &leading_imports);'
//     (selfhost/src/parser/format.kizu:32) is a free-function try-call statement, not a
//     'receiver.append_bytes/append_byte' method call. lower_multi_expr_statement now dispatches a
//     non-field callee to lower_void_free_try_call_statement, which validates the callee returns
//     '!void' (an error-union void success) and lowers it to a VoidTryCall statement -- the call
//     propagates a runtime failure as this function's own error union and binds no value, mirroring
//     LetTryCall's failure propagation without a fake local. The '&var std::string::String' arg
//     'out' resolves through kizu_type_to_llvm's new 'var_' mutable-borrow strip (a borrow shares
//     its pointee's ABI), so the call args lower and lowering advances past the try-call.
//   - "compiled mir: void then-body assignment statement not yet supported": the nested then-body's
//     try-call statement is followed by six scalar reassignments
//     ('index = next_index;' .. 'at_line_start = true;', selfhost/src/parser/format.kizu:33-38),
//     each updating a local bound before the then-body. lower_void_then_body now dispatches the
//     'Assign' kind through lower_void_then_body_assign, which validates the Var target resolves to
//     a bound local, lowers the value (Var / FieldExpr / String / CastExpr / Bool) through the
//     generic expr lowering, checks the value's LLVM type matches the target's, and rides the
//     LetExpr carrier bound to the existing target. All six reassignments lower, so lowering
//     advances past the assignment list to the void then-block renderer.
var formatDriverCrossedLoweringBlockers = []string{
	"compiled signature: call return type not found",
	"compiled function: stdlib return not found",
	"compiled mir: unsupported call arg kind",
	"compiled mir: unsupported then block",
	"compiled mir: local type not found",
	"compiled function: type-llvm mapping not found",
	"compiled mir: unsupported then-body statement kind",
	"compiled mir: try call statement must be a method call",
	"compiled mir: void then-body assignment statement not yet supported",
}

// formatDriverLoweringBlocker is the exact diagnostic the compiled MIR lowering now raises. With
// the nested then-body's six scalar reassignments ('index = next_index;' .. 'at_line_start =
// true;', selfhost/src/parser/format.kizu:33-38) lowered through lower_void_then_body_assign, the
// nested void then-body lowers completely. lower_multi_if_statement then has a fully-lowered void
// then-body in hand but cannot yet emit it -- a void then-block renders as a then-only conditional
// branch into a fall-through join, and that renderer does not exist -- so it raises "compiled mir:
// then-block rendering not yet supported" at selfhost/src/backend/compiled_mir_lower.kizu:5227.
// Rendering the lowered void then-block is the next capability, so the gate pins this measured
// blocker as a behavior assertion (issue 1201).
const formatDriverLoweringBlocker = "compiled mir: void then-block " +
	"rendering not yet supported"

// TestSelfhostFormatDriverLoweringGate emits the real format_source IR facts and drives the
// production compiled MIR lowering over them, asserting it reaches the measured next blocker. The
// no-else void multi-statement then-block of the first top-level 'if' descends through
// lower_void_then_body, lowering its two 'let' bindings: 'let next_index = try
// index_after_leading_imports(&format_tokens);' and 'let last_import = try
// format_tokens.get(next_index - 1);', then its nested 'if !has_line_comment_between(source, 0,
// last_import.end) { ... }'. lower_void_then_body dispatches the nested 'If' through
// lower_multi_if_statement, which recognizes the nested void then-block and descends back into
// lower_void_then_body for the inner statement list: the try-call statement 'try
// append_sorted_imports(out, source, &format_tokens, &leading_imports);' followed by six scalar
// reassignments ('index = next_index;' .. 'at_line_start = true;'). The try-call lowers through
// lower_void_free_try_call_statement and the reassignments through lower_void_then_body_assign, so
// the nested void then-body lowers completely. The next pinned blocker is rendering that lowered
// void then-block (issue 1201).
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
