package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type supportedCorpusCase struct {
	kind      string
	name      string
	command   string
	target    string
	exitCode  int
	stdoutKey string
	stderrKey string
}

// TestSelfhostSupportedCorpusGate runs the #460 manifest through the stage0-native artifact.
func TestSelfhostSupportedCorpusGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_CORPUS") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_CORPUS=1 to run selfhost corpus")
	}
	report, failures := runSelfhostSupportedCorpus(t)
	if failures > 0 {
		t.Fatalf("selfhost supported corpus failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost supported corpus report:\n%s", report)
}

// runSelfhostSupportedCorpus executes manifest entries with the stage0-native artifact.
func runSelfhostSupportedCorpus(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage0-native/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require supported corpus runner: %v", err)
		return "", 1
	}
	cases, err := loadSupportedCorpus("selfhost/tests/supported-corpus.tsv")
	if err != nil {
		t.Errorf("load supported corpus: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendSupportedCorpusHeader(&report, len(cases))
	failures := 0
	for _, item := range cases {
		result := runSelfhostCommand(
			t,
			runner,
			item.command,
			item.target,
		)
		expectedOut := supportedCorpusText(item.stdoutKey)
		expectedErr := supportedCorpusText(item.stderrKey)
		if result.code != item.exitCode || result.stdout != expectedOut ||
			result.stderr != expectedErr {
			t.Errorf("corpus %s mismatch", item.name)
			failures++
		}
		appendSupportedCorpusResult(&report, item, result)
	}
	fmt.Fprintf(&report, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(&report, "comparison.status pass\n")
	} else {
		fmt.Fprintf(&report, "comparison.status fail\n")
	}
	if err := writeSelfhostGateReport(
		"target/selfhost/reports/supported-corpus.txt",
		report.String(),
	); err != nil {
		t.Errorf("write supported corpus report: %v", err)
		failures++
	}
	return report.String(), failures
}

// requireSupportedCorpusRunner checks that the stage0-native selfhost
// executable exists and can be run.
func requireSupportedCorpusRunner(runner string) error {
	info, err := os.Stat(runner)
	if err != nil {
		return fmt.Errorf("%s missing; run `just selfhost-native` first: %w", runner, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", runner)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", runner)
	}
	return nil
}

// loadSupportedCorpus parses the checked-in manifest.
func loadSupportedCorpus(path string) ([]supportedCorpusCase, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []supportedCorpusCase
	for lineNo, line := range strings.Split(string(bytes), "\n") {
		item, ok, err := parseSupportedCorpusLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ok {
			cases = append(cases, item)
		}
	}
	return cases, nil
}

// parseSupportedCorpusLine parses one manifest row.
func parseSupportedCorpusLine(line string) (supportedCorpusCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return supportedCorpusCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 7 {
		return supportedCorpusCase{}, false, fmt.Errorf("expected 7 fields")
	}
	code, err := strconv.Atoi(fields[4])
	if err != nil {
		return supportedCorpusCase{}, false, err
	}
	return supportedCorpusCase{
		kind:      fields[0],
		name:      fields[1],
		command:   fields[2],
		target:    fields[3],
		exitCode:  code,
		stdoutKey: fields[5],
		stderrKey: fields[6],
	}, true, nil
}

// supportedCorpusText resolves a stable expected output key.
func supportedCorpusText(key string) string {
	switch key {
	case "empty":
		return ""
	case "check-ok":
		return "check: ok\n"
	case "moved-value-error":
		return "error: move error: moved value `name` was used at 12:11\n"
	default:
		return "unsupported expected output key: " + key + "\n"
	}
}

// appendSupportedCorpusHeader writes manifest metadata.
func appendSupportedCorpusHeader(out *strings.Builder, count int) {
	fmt.Fprintf(out, "kizu-selfhost-supported-corpus-v0\n")
	fmt.Fprintf(out, "manifest selfhost/tests/supported-corpus.tsv\n")
	fmt.Fprintf(out, "selector manifest-active-rows\n")
	fmt.Fprintf(out, "excluded outside-selector issues #497 #495\n")
	fmt.Fprintf(out, "runner target/selfhost/stage0-native/selfhost\n")
	fmt.Fprintf(out, "runner.build stage0-native (go backend)\n")
	fmt.Fprintf(out, "cases %d\n", count)
}

// appendSupportedCorpusResult writes one corpus result line group.
func appendSupportedCorpusResult(
	out *strings.Builder,
	item supportedCorpusCase,
	result selfhostCommandResult,
) {
	fmt.Fprintf(out, "case.%s.kind %s\n", item.name, item.kind)
	fmt.Fprintf(out, "case.%s.command %s %s\n", item.name, item.command, item.target)
	fmt.Fprintf(out, "case.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "case.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "case.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}
