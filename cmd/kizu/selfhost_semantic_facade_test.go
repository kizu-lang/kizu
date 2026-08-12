package main

import (
	"strings"
	"testing"
)

// TestSelfhostSemanticFacade exercises the semantic facade's observable
// contract: a clean package reports ready, type and ownership problems each
// surface as their own diagnostic, and a parse failure is an error rather than a
// diagnostic because there is nothing to check.
func TestSelfhostSemanticFacade(t *testing.T) {
	for _, test := range []struct {
		entry string
		want  string
	}{
		{
			entry: "ready_and_owned_parse_lifetime_gate",
			want:  "semantic-ready",
		},
		{
			entry: "type_diagnostic_gate",
			want:  "semantic-type-diagnostic",
		},
		{
			entry: "ownership_diagnostic_gate",
			want:  "semantic-ownership-diagnostic",
		},
	} {
		t.Run(test.entry, func(t *testing.T) {
			out, err := runSelfhostPackageGate(
				t,
				"selfhost::semantic_gate::"+test.entry,
			)
			if err != nil {
				t.Fatalf("semantic facade gate failed: %v\n%s", err, out)
			}
			if strings.TrimSpace(out) != test.want {
				t.Fatalf("output = %q, want %q", out, test.want)
			}
		})
	}

	out, err := runSelfhostPackageGate(
		t,
		"selfhost::semantic_gate::parse_failure_gate",
	)
	if err == nil {
		t.Fatalf("parse failure unexpectedly succeeded\n%s", out)
	}
	if !strings.Contains(err.Error(), "parse failed") {
		t.Fatalf("parse failure error = %v, want parse failed", err)
	}
}

// TestSelfhostSemanticFacadeStructure keeps the facade a pure composition over
// already-loaded sources: each phase runs exactly once, and the forbidden names
// are the whole-package entry points and file-path handling that would make the
// facade re-parse instead of consuming what the caller loaded. The parser
// assertions cover the other half of that deal -- the owned parse result has to
// free its per-file parses, or reusing it would leak.
func TestSelfhostSemanticFacadeStructure(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/semantic.kizu")
	body := selfhostKizuFunctionBody(t, source, "fn check_loaded_sources(")

	for _, call := range []string{
		"parser::parse_source_files(",
		"resolver::resolve_parsed_sources(",
		"types::check_parsed_sources(",
		"ownership::check_parsed_package(",
	} {
		if got := strings.Count(body, call); got != 1 {
			t.Errorf("%s count = %d, want 1", call, got)
		}
	}

	for _, forbidden := range []string{
		"resolver::resolve_sources(",
		"types::check_sources(",
		"ownership::check_package(",
		"fast_diagnostic",
		"loader::",
		".kizu",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("semantic facade contains forbidden path %q", forbidden)
		}
	}

	requireSourceFragments(t, "semantic owned result", source, []string{
		"pub parsed: parser::ParsedSourceFiles",
		"pub package: data::CheckedPackage",
		"return checked.package.diagnostics == 0",
		"self.parsed.deinit()",
		"errdefer parsed.deinit()",
		"must not\n    // parse files again",
	})

	parserSource := readSelfhostFile(t, "../../selfhost/src/parser.kizu")
	requireSourceFragments(t, "parsed source deep cleanup", parserSource, []string{
		"while self.parsed.len() > 0",
		"let parsed = self.parsed.pop_or_panic()",
		"parsed.deinit()",
		"self.source_indexes.deinit()",
		"self.parsed.deinit()",
		"errdefer parsed_files.deinit()",
	})
}
