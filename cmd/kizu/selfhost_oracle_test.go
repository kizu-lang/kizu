package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

const (
	selfhostGateEnv   = "KIZU_RUN_SELFHOST_GATES"
	selfhostOracleEnv = "KIZU_RUN_SELFHOST_ORACLE"

	selfhostOracleBudget = 60 * time.Second
)

// requireSelfhostGate skips heavyweight selfhost integration gates by default.
func requireSelfhostGate(t *testing.T) {
	t.Helper()
	if os.Getenv(selfhostGateEnv) != "1" {
		t.Skipf("set %s=1 to run heavyweight selfhost gates", selfhostGateEnv)
	}
}

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
	if os.Getenv(selfhostOracleEnv) != "1" {
		t.Skipf("set %s=1 to run the aggregate selfhost oracle", selfhostOracleEnv)
	}
	start := time.Now()
	results := []selfhostOracleResult{
		timedSelfhostOracle(t, "lexer-production", runSelfhostLexerProductionOracle),
		timedSelfhostOracle(t, "parser-production", runSelfhostParserProductionOracle),
		timedSelfhostOracle(t, "source", runSelfhostSourceOracle),
		timedSelfhostOracle(t, "format", runSelfhostFormatOracle),
	}
	pipelineStart := time.Now()
	results = append(results, runSelfhostPipelineOracle(t)...)
	t.Logf("oracle elapsed pipeline=%s", time.Since(pipelineStart))
	failures := 0
	for _, result := range results {
		failures += result.failures
		logSelfhostOracleResult(t, result)
	}
	elapsed := time.Since(start)
	t.Logf("oracle elapsed aggregate=%s budget=%s", elapsed, selfhostOracleBudget)
	if failures > 0 {
		t.Fatalf("selfhost oracle failures=%d", failures)
	}
	if elapsed > selfhostOracleBudget {
		t.Fatalf("selfhost oracle exceeded budget elapsed=%s budget=%s", elapsed, selfhostOracleBudget)
	}
}

// timedSelfhostOracle logs one aggregate oracle component wall time.
func timedSelfhostOracle(
	t *testing.T,
	name string,
	run func(*testing.T) selfhostOracleResult,
) selfhostOracleResult {
	t.Helper()
	start := time.Now()
	result := run(t)
	t.Logf("oracle elapsed %s=%s", name, time.Since(start))
	return result
}

// runSelfhostLexerProductionOracle checks the selfhost lexer component gate.
func runSelfhostLexerProductionOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countSelfhostLexerGateFailures(t)
	return selfhostOracleResult{
		component: "lexer-production",
		corpus:    "selfhost",
		scanned:   1,
		compared:  1,
		failures:  failures,
	}
}

// runSelfhostParserProductionOracle checks the selfhost parser component gate.
func runSelfhostParserProductionOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countSelfhostParserGateFailures(t)
	return selfhostOracleResult{
		component: "parser-production",
		corpus:    "selfhost",
		scanned:   1,
		compared:  1,
		failures:  failures,
	}
}

// runSelfhostSourceOracle checks the Kizu-owned source manager component gate.
func runSelfhostSourceOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countSelfhostSourceGateFailures(t)
	return selfhostOracleResult{
		component: "source",
		corpus:    "selfhost",
		scanned:   4,
		compared:  4,
		failures:  failures,
	}
}

// runSelfhostFormatOracle checks the Kizu-owned formatter component gate.
func runSelfhostFormatOracle(t *testing.T) selfhostOracleResult {
	t.Helper()
	failures := countSelfhostFormatGateFailures(t)
	return selfhostOracleResult{
		component: "format",
		corpus:    "selfhost",
		scanned:   1,
		compared:  1,
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
