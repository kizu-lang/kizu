package main

import (
	"strings"
	"testing"
)

type stdKizuParserErrorCase struct {
	name string
	path string
	want string
}

// TestStdKizuParserErrorSeeds checks recoverable parser errors stay readable.
func TestStdKizuParserErrorSeeds(t *testing.T) {
	if failures := countStdKizuParserErrorSeedFailures(t); failures > 0 {
		t.Fatalf("parser error seed failures=%d", failures)
	}
}

// stdKizuParserErrorSeedCases returns negative sources that exercise parser !T paths.
func stdKizuParserErrorSeedCases() []stdKizuParserErrorCase {
	return []stdKizuParserErrorCase{
		{
			name: "missing_rparen",
			path: "examples/negative/std_kizu_parser_missing_rparen.kizu",
			want: "expected right paren",
		},
		{
			name: "block_expected_statement",
			path: "examples/negative/std_kizu_parser_block_expected_statement.kizu",
			want: "expected semicolon",
		},
	}
}

// countStdKizuParserErrorSeedFailures returns failures for oracle summary logging.
func countStdKizuParserErrorSeedFailures(t *testing.T) int {
	t.Helper()
	failures := 0
	for _, testCase := range stdKizuParserErrorSeedCases() {
		out, err := runKizu("run", testCase.path)
		if err == nil {
			t.Errorf("%s: expected parser error, got success\n%s", testCase.name, out)
			failures++
			continue
		}
		if !strings.Contains(out, "error:") {
			t.Errorf("%s: missing readable error prefix in %q", testCase.name, out)
			failures++
		}
		if !strings.Contains(out, testCase.want) {
			t.Errorf("%s: got %q, want substring %q", testCase.name, out, testCase.want)
			failures++
		}
		if strings.Contains(out, "move error:") || strings.Contains(out, "panic") {
			t.Errorf("%s: parser error collapsed into host/internal failure: %q", testCase.name, out)
			failures++
		}
	}
	return failures
}
