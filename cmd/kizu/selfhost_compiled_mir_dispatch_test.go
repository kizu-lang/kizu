package main

import (
	"strings"
	"testing"
)

// TestSelfhostCompiledMIRGuardsParserOnlyLoopProbes keeps parser-only loop shape
// detectors out of non-parser compiled components.
func TestSelfhostCompiledMIRGuardsParserOnlyLoopProbes(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	body := selfhostKizuFunctionBody(t, lower, "pub fn lower_multi_statement_function(")
	guard := "if is_parser_component {"
	guardIndex := strings.Index(body, guard)
	if guardIndex < 0 {
		t.Fatalf("compiled MIR lowerer missing parser component guard %q", guard)
	}

	for _, probe := range []string{
		"is_grammar_postfix_loop_shape(",
		"is_type_apply_loop_shape(",
		"is_while_match_loop_shape(",
		"is_precedence_loop_shape(",
		"is_dual_cursor_loop_shape(",
		"is_trailing_token_loop_shape(",
		"is_value_cursor_append_loop_shape(",
		"is_guarded_cursor_return_loop_shape(",
	} {
		probeIndex := strings.Index(body, probe)
		if probeIndex < 0 {
			t.Fatalf("compiled MIR lowerer missing parser-only probe %q", probe)
		}
		if probeIndex < guardIndex {
			t.Fatalf("parser-only probe %q is not guarded by the parser component prefix", probe)
		}
	}

	embeddedGuard := "if is_parser_component and try is_embedded_value_cursor_append_loop_shape("
	if !strings.Contains(body, embeddedGuard) {
		t.Fatalf("embedded value-cursor probe missing parser component guard %q", embeddedGuard)
	}

	helper := selfhostKizuFunctionBody(t, lower, "fn compiled_mir_lower_is_parser_component(")
	parserPrefix := `std::mem::starts_with(function_name, "std::kizu::parser::")`
	if !strings.Contains(helper, parserPrefix) {
		t.Fatal("parser component guard must be prefix-limited to std::kizu::parser")
	}
}

// TestSelfhostCompiledMIRUsesPerFunctionStructuralCache keeps hot top-level
// statement and call-argument lookups on the per-function lowering cache.
func TestSelfhostCompiledMIRUsesPerFunctionStructuralCache(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	multi := selfhostKizuFunctionBody(t, lower, "pub fn lower_multi_statement_function(")
	for _, fragment := range []string{
		"try compiled_mir_types::build_fn_node_index(",
		"compiled_mir_types::fn_body_child_sequence_from(",
		"compiled_mir_types::fn_node_kind(",
	} {
		if !strings.Contains(multi, fragment) {
			t.Fatalf("lower_multi_statement_function missing cached lookup %q", fragment)
		}
	}
	if strings.Contains(multi, "let stmt_kind = try ir_contract::body_node_kind_from(") {
		t.Fatal("top-level statement dispatch should use the cached node kind table")
	}

	topLevelHasKind := selfhostKizuFunctionBody(t, lower, "fn top_level_has_statement_kind(")
	for _, fragment := range []string{
		"compiled_mir_types::fn_body_child_sequence_from(",
		"compiled_mir_types::fn_node_kind(",
	} {
		if !strings.Contains(topLevelHasKind, fragment) {
			t.Fatalf("top_level_has_statement_kind missing cached lookup %q", fragment)
		}
	}

	types := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_types.kizu")
	countArgs := selfhostKizuFunctionBody(t, types, "pub fn count_call_args_cached(")
	cachedArgCount := `return try fn_body_edge_count_from(` +
		`cache, ir_bytes, function_name, call_node, "arg", bs` +
		`);`
	if !strings.Contains(countArgs, cachedArgCount) {
		t.Fatal("cached call-argument count should use one edge-count lookup")
	}
	if strings.Contains(countArgs, "while true") {
		t.Fatal("cached call-argument count should not rescan once per ordinal")
	}
}

// TestSelfhostPackageDefinitionConsumerUsesIrIndex keeps the backend on the
// generic package-definition contract. Reachability belongs to the frontend
// package graph; the backend consumes indexed facts and lowers each definition.
func TestSelfhostPackageDefinitionConsumerUsesIrIndex(t *testing.T) {
	programLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_program_llvm.kizu")
	consumer := selfhostKizuFunctionBody(t, programLLVM, "pub fn append_reachable_functions(")
	for _, fragment := range []string{
		`let prefix = "package-dependency ";`,
		"ir_index::first_entry_with_fact_prefix(lookup_index, ir_bytes, prefix)",
		"ir_index::entry_key_starts_with(lookup_index, ir_bytes, entry, prefix)",
		`let name_prefix = "package-definition-name ";`,
	} {
		if !strings.Contains(consumer, fragment) {
			t.Fatalf("generic package dependency consumer missing %q", fragment)
		}
	}
	assertPackageDefinitionEmissionForwardsIndexedFacts(t, consumer)
	emit := selfhostKizuFunctionBody(t, programLLVM, "fn emit_numeric_package_definition(")
	if !strings.Contains(emit, "compiled_llvm::append_compiled_function_auto_indexed(") {
		t.Fatal("generic package definition should lower through indexed compiled lowering")
	}
	if strings.Contains(programLLVM, "collect_component_compiled_body_callees") {
		t.Fatal("backend-local component reachability collector should remain removed")
	}
}

// assertPackageDefinitionEmissionForwardsIndexedFacts keeps the per-definition
// emission call on the caller-owned index and canonical fact table, lowering the
// name resolved from the package-definition-name table. Whitespace is normalised
// so this pins the argument contract rather than the source line wrapping.
func assertPackageDefinitionEmissionForwardsIndexedFacts(t *testing.T, consumer string) {
	t.Helper()
	start := strings.Index(consumer, "try emit_numeric_package_definition(")
	if start < 0 {
		t.Fatal("generic package dependency consumer does not emit each definition")
	}
	end := strings.Index(consumer[start:], ");")
	if end < 0 {
		t.Fatal("generic package definition emission call is unterminated")
	}
	call := strings.Join(strings.Fields(consumer[start:start+end]), " ")
	const forwarded = "try emit_numeric_package_definition( " +
		"out, lookup_index, canonical_facts, ir_bytes,"
	if !strings.HasPrefix(call, forwarded) {
		t.Fatalf(
			"generic package definition emission does not forward the caller-owned "+
				"index and canonical facts: %q",
			call,
		)
	}
	if !strings.HasSuffix(call, ", name") {
		t.Fatalf(
			"generic package definition emission does not lower the indexed definition name: %q",
			call,
		)
	}
	assertPackageDefinitionEmissionSharesOneDefinitionSlot(t, consumer, call)
}

// assertPackageDefinitionEmissionSharesOneDefinitionSlot pins what the prefix and
// suffix above cannot see: the component id, the function id, and the name handed
// to one emission call must all be read at the same definition slot. Constant ids,
// or ids read at a different slot than the name, both emit a definition under
// another definition's key while every other assertion here still passes.
func assertPackageDefinitionEmissionSharesOneDefinitionSlot(
	t *testing.T,
	consumer string,
	call string,
) {
	t.Helper()
	slot := packageDefinitionNameSlotExpression(t, consumer)
	args := selfhostCallArguments(t, call)
	if len(args) != 7 {
		t.Fatalf("generic package definition emission takes %d arguments, want 7: %q", len(args), call)
	}
	component, function := args[4], args[5]
	read := ".get(" + slot + ")"
	for _, arg := range []string{component, function} {
		if !strings.Contains(arg, read) {
			t.Fatalf(
				"generic package definition emission passes %q, which is not read at the "+
					"definition slot %q that resolved the name",
				arg, slot,
			)
		}
	}
	if component == function {
		t.Fatalf("component and function ids come from one table: %q", component)
	}
}

// packageDefinitionNameSlotExpression returns the slot expression the consumer
// uses to resolve one definition's name, and requires the start and end of that
// name to be read at that one slot. Deriving the expression keeps the argument
// assertion tied to the binding it has to agree with, so renaming the slot stays
// legal while reading a different slot does not.
func packageDefinitionNameSlotExpression(t *testing.T, consumer string) string {
	t.Helper()
	const marker = "let name = ir_bytes[try name_starts.get("
	start := strings.Index(consumer, marker)
	if start < 0 {
		t.Fatal("generic package dependency consumer does not resolve a definition name by slot")
	}
	rest := consumer[start+len(marker):]
	end := strings.Index(rest, ")")
	if end <= 0 {
		t.Fatal("generic package definition name slot expression is unterminated")
	}
	slot := rest[:end]
	want := ")..try name_ends.get(" + slot + ")];"
	if !strings.HasPrefix(rest[end:], want) {
		t.Fatalf(
			"generic package definition name is not the ir_bytes range between name_starts "+
				"and name_ends at slot %q: %q",
			slot, rest[end:min(end+len(want)+16, len(rest))],
		)
	}
	return slot
}

// selfhostCallArguments splits a whitespace-normalised Kizu call at its top-level
// commas. Nested calls keep their own commas, so an argument that is itself an
// indexed read stays one argument.
func selfhostCallArguments(t *testing.T, call string) []string {
	t.Helper()
	open := strings.Index(call, "(")
	if open < 0 {
		t.Fatalf("not a call: %q", call)
	}
	args := []string{}
	depth := 0
	current := strings.Builder{}
	for _, r := range call[open+1:] {
		switch {
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth--
		case r == ',' && depth == 0:
			args = append(args, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if trailing := strings.TrimSpace(current.String()); trailing != "" {
		args = append(args, trailing)
	}
	return args
}
