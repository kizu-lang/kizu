package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostBorrowedDeclaredStructFieldTypeGate verifies that field lookup
// auto-dereferences a borrowed declared receiver before consulting field facts.
func TestSelfhostBorrowedDeclaredStructFieldTypeGate(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(
		program,
		"selfhost::types_oracle::borrowed_declared_struct_field_gate",
	)
	if err != nil {
		t.Fatalf("borrowed declared struct field gate failed: %v\n%s", err, out.String())
	}
	if got := out.String(); got != "borrowed-declared-struct-field-ok\n" {
		t.Fatalf("borrowed declared struct field output = %q", got)
	}
}

// TestSelfhostExactStructFieldOwnerGate verifies that a field lookup resolves
// against the receiver's exact declared owner, so two structs sharing a field
// name cannot resolve to each other's field type.
func TestSelfhostExactStructFieldOwnerGate(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(
		program,
		"selfhost::types_oracle::exact_struct_field_owner_gate",
	)
	if err != nil {
		t.Fatalf("exact struct field owner gate failed: %v\n%s", err, out.String())
	}
	if got := out.String(); got != "exact-struct-field-owner-ok\n" {
		t.Fatalf("exact struct field owner output = %q", got)
	}
}

// TestSelfhostFastDiagnosticsUsePackageExpressionFacts keeps cross-file
// receiver resolution on the package-wide fact set built from parsed ASTs.
func TestSelfhostFastDiagnosticsUsePackageExpressionFacts(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/cli/check.kizu")
	required := []string{
		"var expression_types = expression_facts::init(allocator)",
		"function_calls::collect_function_signatures_from_ast(",
		"types::collect_nominal_resolution_facts_from_ast(",
		"types::collect_struct_field_resolution_facts_from_ast(",
		"types::first_pre_move_check_diagnostic_ast_node_with_facts(",
		"types::first_post_move_check_diagnostic_ast_node_with_facts(",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("package fast diagnostics missing exact expression fact flow %q", fragment)
		}
	}
}

// TestSelfhostExpressionFactsSeparateSourceAndOwnedFieldStorage prevents
// source-backed slices from sharing a getter contract with owned field spellings,
// and pins one interned spelling to one owned buffer. A single shared, growable
// storage moves every earlier spelling each time it reallocates, and the views
// callers park in local type maps then read the freed block: the same file
// answered "check: ok" or "unbalanced type delimiters" run to run.
func TestSelfhostExpressionFactsSeparateSourceAndOwnedFieldStorage(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/types/expression_facts.kizu")
	required := []string{
		"source_indexes: std::map::Map<[]u8, i64>",
		"field_indexes: std::map::Map<[]u8, i64>",
		"spellings: std::array::Array<std::string::String>",
		"fn contains_source(",
		"fn get_source(",
		"fn insert_source(",
		"fn contains_field(",
		") -> ![]u8 borrows self",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expression facts missing split ownership contract %q", fragment)
		}
	}
	forbidden := []string{
		"fn get(self: &ExpressionTypeFacts",
		"pub source_values:",
		"spelling_storage",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("expression facts retained ambiguous getter %q", fragment)
		}
	}
}

// TestSelfhostTypeGate executes the Kizu-owned type checker oracle entry.
func TestSelfhostTypeGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countSelfhostTypeGateFailures(t); failures > 0 {
		t.Fatalf("selfhost type gate failures=%d", failures)
	}
}

// countSelfhostTypeGateFailures returns failures for oracle summary logging.
func countSelfhostTypeGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostTypeGate(t)
	if err != nil {
		t.Errorf("type gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"type-modules\n",
		"type-production-symbols\n",
		"type-production-functions\n",
		"type-production-typed-nodes\n",
		"type-production-diagnostics\n0\n",
		"type-symbols\n9\n",
		"type-typed-nodes\n9\n",
		"type-diagnostics\n19\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("type gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return 0
}

// runSelfhostTypeGate loads the selfhost package and runs its type checker oracle.
func runSelfhostTypeGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::types_oracle::gate")
	return out.String(), err
}
