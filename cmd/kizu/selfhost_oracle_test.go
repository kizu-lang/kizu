package main

import (
	"io/fs"
	"path/filepath"
	"sort"
	"testing"
)

type selfhostOracleResult struct {
	component          string
	corpus             string
	scanned            int
	compared           int
	unsupported        int
	seeds              int
	failures           int
	unsupportedReasons map[string]int
	unsupportedSamples map[string]string
}

// TestSelfhostOracleRunner is the one-command Go/Kizu component parity gate.
func TestSelfhostOracleRunner(t *testing.T) {
	results := []selfhostOracleResult{
		runSelfhostLexerOracle(t),
		runSelfhostLexerTokenizeOracle(t),
		runSelfhostLexerSourceOracle(t),
		runSelfhostLexerTokenizeSourceOracle(t),
		runSelfhostParserOracle(t),
		runSelfhostParserSourceOracle(t),
		runSelfhostParserErrorOracle(t),
		runSelfhostResolverOracle(t),
	}
	failures := 0
	for _, result := range results {
		failures += result.failures
		logSelfhostOracleResult(t, result)
	}
	if failures > 0 {
		t.Fatalf("selfhost oracle failures=%d", failures)
	}
}

// runSelfhostLexerOracle compares the supported examples through the lexer oracle.
func runSelfhostLexerOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	examples, stats := collectLexerParityExamples(t)
	seeds := lexerParitySeedCases(t)
	cases := append(seeds, examples...)
	got := runStdKizuLexerParityHarness(t, cases)
	failures := countLexerParityFailures(cases, got)
	if failures > 0 {
		assertLexerParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component:          "lexer",
		corpus:             "examples",
		scanned:            stats.scanned,
		compared:           len(cases),
		unsupported:        stats.unsupported,
		seeds:              len(seeds),
		failures:           failures,
		unsupportedReasons: stats.unsupportedReasons,
		unsupportedSamples: stats.unsupportedSamples,
	}
}

// runSelfhostLexerTokenizeOracle compares the Array-backed tokenization path.
func runSelfhostLexerTokenizeOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	cases := lexerParitySeedCases(t)
	got := runStdKizuLexerTokenizeParityHarness(t, cases)
	failures := countLexerParityFailures(cases, got)
	if failures > 0 {
		assertLexerParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component: "lexer-tokenize",
		corpus:    "seeds",
		scanned:   len(cases),
		compared:  len(cases),
		seeds:     len(cases),
		failures:  failures,
	}
}

// runSelfhostLexerSourceOracle compares the source-owned selfhost package lexer surface.
func runSelfhostLexerSourceOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	cases := collectLexerParitySelfhostSources(t)
	got := runStdKizuLexerParityHarness(t, cases)
	failures := countLexerParityFailures(cases, got)
	if failures > 0 {
		assertLexerParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component: "lexer",
		corpus:    "selfhost",
		scanned:   len(cases),
		compared:  len(cases),
		failures:  failures,
	}
}

// runSelfhostLexerTokenizeSourceOracle compares selfhost sources through token arrays.
func runSelfhostLexerTokenizeSourceOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	cases := collectLexerParitySelfhostSources(t)
	got := runStdKizuLexerTokenizeParityHarness(t, cases)
	failures := countLexerParityFailures(cases, got)
	if failures > 0 {
		assertLexerParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component: "lexer-tokenize",
		corpus:    "selfhost",
		scanned:   len(cases),
		compared:  len(cases),
		failures:  failures,
	}
}

// runSelfhostParserOracle compares the supported examples through the parser oracle.
func runSelfhostParserOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	examples, stats := collectParserParityExamples(t)
	seeds := parserParitySeedCases(t)
	cases := append(seeds, examples...)
	got := runStdKizuParserParityHarness(t, cases)
	failures := countParserParityFailures(cases, got)
	if failures > 0 {
		assertParserParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component:          "parser",
		corpus:             "examples",
		scanned:            stats.scanned,
		compared:           len(cases),
		unsupported:        stats.unsupported,
		seeds:              len(seeds),
		failures:           failures,
		unsupportedReasons: stats.unsupportedReasons,
		unsupportedSamples: stats.unsupportedSamples,
	}
}

// runSelfhostParserSourceOracle compares the source-owned selfhost package parser surface.
func runSelfhostParserSourceOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	cases := collectParserParitySelfhostSources(t)
	got := runStdKizuParserParityHarness(t, cases)
	failures := countParserParityFailures(cases, got)
	if failures > 0 {
		assertParserParityCases(t, cases, got)
	}
	return selfhostOracleResult{
		component: "parser",
		corpus:    "selfhost",
		scanned:   len(cases),
		compared:  len(cases),
		failures:  failures,
	}
}

// runSelfhostParserErrorOracle checks parser errors stay recoverable and readable.
func runSelfhostParserErrorOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countStdKizuParserErrorSeedFailures(t)
	cases := stdKizuParserErrorSeedCases()
	return selfhostOracleResult{
		component: "parser-errors",
		corpus:    "negative-seeds",
		scanned:   len(cases),
		compared:  len(cases),
		failures:  failures,
	}
}

// runSelfhostResolverOracle checks the Kizu-owned resolver component gate.
func runSelfhostResolverOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countSelfhostResolverGateFailures(t)
	return selfhostOracleResult{
		component: "resolver",
		corpus:    "selfhost",
		scanned:   4,
		compared:  4,
		failures:  failures,
	}
}

// collectLexerParitySelfhostSources rejects lexer gaps in the selfhost source package.
func collectLexerParitySelfhostSources(t *testing.T) []lexerParityCase {
	t.Helper()
	cases := []lexerParityCase{}
	err := filepath.WalkDir(parserParitySelfhostRoot, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return err
		}
		next, ok, reason := lexerParityFileCase(path, parserParitySelfhostRoot, "selfhost")
		if !ok {
			t.Fatalf("%s is unsupported: %s", parserParityCaseName(
				parserParitySelfhostRoot,
				"selfhost",
				path,
			), reason)
		}
		cases = append(cases, next)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	return cases
}

// countLexerParityFailures returns a compact failure count for oracle logging.
func countLexerParityFailures(cases []lexerParityCase, got map[string]string) int {
	wantNames := map[string]bool{}
	failures := 0
	for _, testCase := range cases {
		wantNames[testCase.name] = true
		actual, ok := got[testCase.name]
		if !ok || actual != testCase.want {
			failures++
		}
	}
	for name := range got {
		if !wantNames[name] {
			failures++
		}
	}
	return failures
}

// countParserParityFailures returns a compact failure count for oracle logging.
func countParserParityFailures(cases []parserParityCase, got map[string]string) int {
	wantNames := map[string]bool{}
	failures := 0
	for _, testCase := range cases {
		wantNames[testCase.name] = true
		actual, ok := got[testCase.name]
		if !ok || actual != testCase.want {
			failures++
		}
	}
	for name := range got {
		if !wantNames[name] {
			failures++
		}
	}
	return failures
}

// logSelfhostOracleResult reports the stable summary required by issue gates.
func logSelfhostOracleResult(t *testing.T, result selfhostOracleResult) {
	t.Helper()
	t.Logf(
		"oracle component=%s corpus=%s scanned=%d compared=%d unsupported=%d failures=%d seeds=%d",
		result.component,
		result.corpus,
		result.scanned,
		result.compared,
		result.unsupported,
		result.failures,
		result.seeds,
	)
	logSelfhostOracleUnsupported(t, result)
}

// logSelfhostOracleUnsupported reports unsupported reasons with samples.
func logSelfhostOracleUnsupported(t *testing.T, result selfhostOracleResult) {
	t.Helper()
	if len(result.unsupportedReasons) == 0 {
		return
	}
	reasons := make([]string, 0, len(result.unsupportedReasons))
	for reason := range result.unsupportedReasons {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool {
		if result.unsupportedReasons[reasons[i]] == result.unsupportedReasons[reasons[j]] {
			return reasons[i] < reasons[j]
		}
		return result.unsupportedReasons[reasons[i]] > result.unsupportedReasons[reasons[j]]
	})
	for index, reason := range reasons {
		if index == 5 {
			break
		}
		t.Logf(
			"oracle unsupported[%d] component=%s reason=%s count=%d sample=%s",
			index+1,
			result.component,
			reason,
			result.unsupportedReasons[reason],
			result.unsupportedSamples[reason],
		)
	}
}
