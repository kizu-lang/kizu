package main

import (
	"strings"
	"testing"
)

// TestSelfhostArbitraryRootPackageClosure covers the claim that any checked
// function can serve as the emission root, not just a function named main. The
// gate picks a root by source identity and the three assertions below cover what
// that has to mean: the emitted facts are exactly the root's closure, the
// selection is identity-based rather than name-based, and a root that cannot be
// trusted is refused instead of narrowed.
func TestSelfhostArbitraryRootPackageClosure(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t,
		"selfhost::ir::source_root_facts_gate::"+
			"arbitrary_source_root_facts_gate",
	)
	if err != nil {
		t.Fatalf("arbitrary source root facts gate failed: %v\n%s", err, out)
	}

	assertArbitrarySourceRootFacts(t, out)
	assertArbitrarySourceRootSelection(t)
	assertCheckedEntryRootsFailClosed(t)
}

// assertArbitrarySourceRootFacts checks the emitted fact stream is exactly the
// selected root's closure: one program entry naming that root, the root and the
// helper it calls present with the call resolved between them, and the unrelated
// definition absent down to its body facts. The last check keeps the source-root
// closure from picking up the external ABI manifest roots, which belong to the
// separate whole-package closure.
func assertArbitrarySourceRootFacts(t *testing.T, out string) {
	t.Helper()
	var entries []string
	var definitions []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "program-entry ") {
			entries = append(entries, line)
		}
		if strings.HasPrefix(line, "package-definition-name ") {
			fields := strings.Fields(line)
			if len(fields) != 4 {
				t.Fatalf("malformed package definition name fact %q", line)
			}
			definitions = append(definitions, fields[3])
		}
	}
	if len(entries) != 1 {
		t.Fatalf("program entry fact count = %d, want 1\n%s", len(entries), out)
	}
	entryFields := strings.Fields(entries[0])
	if len(entryFields) != 4 || entryFields[3] != "app::entry::main" {
		t.Fatalf("program entry fact = %q, want exact main identity", entries[0])
	}
	if !containsExactString(definitions, "app::entry::main") {
		t.Fatalf("selected root definition missing: %v", definitions)
	}
	if !containsExactString(definitions, "app::entry::helper") {
		t.Fatalf("reachable helper definition missing: %v", definitions)
	}
	if containsExactString(definitions, "app::entry::unrelated") {
		t.Fatalf("unreachable definition was emitted: %v", definitions)
	}
	if strings.Contains(out, "body-node app::entry::unrelated ") {
		t.Fatalf("unreachable body facts were emitted\n%s", out)
	}
	if !strings.Contains(
		out,
		"body-call-target app::entry::main ",
	) || !strings.Contains(out, " app::entry::helper\n") {
		t.Fatalf("selected root dependency was not resolved exactly\n%s", out)
	}
	if strings.Contains(out, "external-abi-entrypoint ") {
		t.Fatalf("source-root closure leaked external ABI roots\n%s", out)
	}
}

// assertArbitrarySourceRootSelection requires the root to be found by matching
// source id and name span through the catalog. The forbidden "main" literal is
// the point of the gate: a hardcoded entry name would make every other root
// unreachable no matter what the caller asked for.
func assertArbitrarySourceRootSelection(t *testing.T) {
	t.Helper()
	source := readSelfhostFile(
		t, "../../selfhost/src/ir/executable_functions.kizu",
	)
	rootSelection := selfhostKizuFunctionBody(
		t, source, "fn source_function_root_target(",
	)
	requireSourceFragments(t, "source root exact identity", rootSelection, []string{
		"file.id == root.source_id",
		"package_exact_lookup::component_for_source(catalog, file)",
		"package_exact_lookup::function_entry_by_name_span(",
		"package_catalog::target_at_entry(catalog, entry)",
	})
	if strings.Contains(rootSelection, `"main"`) {
		t.Fatal("source root selection hardcodes the main function name")
	}
}

// assertCheckedEntryRootsFailClosed requires the two ways a checked entry root
// can be untrustworthy -- the package still has diagnostics, or the root does not
// belong to the source it claims -- to be errors naming their cause, rather than
// a silently reduced closure.
func assertCheckedEntryRootsFailClosed(t *testing.T) {
	t.Helper()
	for _, failure := range []struct {
		entry   string
		message string
	}{
		{
			entry:   "checked_entry_diagnostic_failure_gate",
			message: "checked entry root requires a diagnostic-free package",
		},
		{
			entry:   "checked_entry_source_mismatch_gate",
			message: "checked entry root source identity mismatch",
		},
	} {
		failureOut, failureErr := runSelfhostAbiParamsGate(
			t,
			"selfhost::ir::source_root_facts_gate::"+failure.entry,
		)
		if failureErr == nil {
			t.Fatalf(
				"%s unexpectedly succeeded\n%s",
				failure.entry,
				failureOut,
			)
		}
		if !strings.Contains(failureErr.Error(), failure.message) {
			t.Fatalf(
				"%s error = %v, want %q",
				failure.entry,
				failureErr,
				failure.message,
			)
		}
	}
}

// containsExactString reports whether values holds wanted verbatim. Fact names
// are compared exactly on purpose: a substring match would let a longer
// qualified name satisfy an assertion about a shorter one.
func containsExactString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
