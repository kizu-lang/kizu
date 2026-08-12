package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	probeCorpusDir    = "selfhost/tests/probes"
	probeBaselinePath = "selfhost/tests/probes/baseline.tsv"
	probeGateDir      = "target/selfhost/probes"
	probeGateRunner   = "target/selfhost/probes/selfhost"
	probeGateCacheDir = "target/selfhost/probes/cache"
)

// probeCase is one row of the checked-in baseline joined with its source file.
// calls are the probe entry expressions the file declares on its `// probe-calls:`
// line; the file's own main prints exactly those, so the Go reference output and
// the staged package's output are the same value sequence in the same order.
//
// reach are the expressions on the optional `// probe-reach:` line: shapes the probe
// needs LOWERED but cannot hand to the driver. A function returning `!T` has no
// i64 ABI, and an i64 function cannot unwrap an error union -- `try` needs its
// container to return `!T`, and a discarded non-void call is refused by the staged
// backend by name. So the error-union shapes are called from the staged root with
// `try`, which is what makes them reachable and therefore lowered, and from the
// probe's own main the same way. The driver still reads only the probe's i64 entry:
// what a reach expression buys is a module that contains the shape at all, and a
// lowering that gets it wrong emits a module LLVM refuses.
type probeCase struct {
	name        string
	path        string
	calls       []string
	reach       []string
	runStatus   string
	stageStatus string
	note        string
}

// probeObservation is what one gate run measured for one probe.
type probeObservation struct {
	name        string
	reference   selfhostCommandResult
	subject     selfhostCommandResult
	runStatus   string
	stageStatus string
	stageStdout string
	stageNote   string
}

// TestSelfhostProbeCorpusMatchesBaseline keeps the checked-in baseline and the
// probe corpus in one-to-one correspondence. It runs in the default suite because
// it is the half of the pass-set guard that costs nothing: a probe added without a
// baseline row would otherwise be silently unmeasured, and a baseline row whose
// probe file was deleted would silently stop being a claim about anything.
func TestSelfhostProbeCorpusMatchesBaseline(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()
	baseline, err := loadProbeBaseline(probeBaselinePath)
	if err != nil {
		t.Fatalf("load probe baseline: %v", err)
	}
	corpus, err := listProbeSources(probeCorpusDir)
	if err != nil {
		t.Fatalf("list probe corpus: %v", err)
	}
	for _, name := range corpus {
		if _, ok := baseline[name]; !ok {
			t.Errorf("probe %s has no baseline row; add it to %s", name, probeBaselinePath)
		}
	}
	for name := range baseline {
		if !containsExactString(corpus, name) {
			t.Errorf("baseline row %s has no probe file in %s", name, probeCorpusDir)
		}
	}
}

// TestSelfhostProbeSourcesDeclareCalls checks every probe declares the entry
// expressions the staged package needs, and that its main prints exactly those.
// The two paths only compare if they evaluate the same calls in the same order.
func TestSelfhostProbeSourcesDeclareCalls(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()
	corpus, err := listProbeSources(probeCorpusDir)
	if err != nil {
		t.Fatalf("list probe corpus: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatalf("probe corpus is empty")
	}
	for _, name := range corpus {
		path := filepath.Join(probeCorpusDir, name+".kizu")
		calls, err := readProbeCalls(path)
		if err != nil {
			t.Errorf("read probe calls %s: %v", name, err)
			continue
		}
		printed, err := readProbePrintedCalls(path)
		if err != nil {
			t.Errorf("read probe main %s: %v", name, err)
			continue
		}
		if strings.Join(calls, "; ") != strings.Join(printed, "; ") {
			t.Errorf(
				"probe %s declares [%s] but main prints [%s]",
				name,
				strings.Join(calls, "; "),
				strings.Join(printed, "; "),
			)
		}
		countProbeReachDrift(t, name, path)
	}
}

// countProbeReachDrift checks a probe's reach line against its main. A reach
// expression is only lowered in the phase that calls it, so one the staged root
// reaches and main does not is a shape the run backend never sees -- and the probe
// would then claim a coverage it does not have.
func countProbeReachDrift(t *testing.T, name string, path string) {
	t.Helper()
	reach, err := readProbeReach(path)
	if err != nil {
		t.Errorf("read probe reach %s: %v", name, err)
		return
	}
	if len(reach) == 0 {
		return
	}
	tried, err := readProbeTriedCalls(path)
	if err != nil {
		t.Errorf("read probe main %s: %v", name, err)
		return
	}
	if strings.Join(reach, "; ") != strings.Join(tried, "; ") {
		t.Errorf(
			"probe %s reaches [%s] but main tries [%s]",
			name,
			strings.Join(reach, "; "),
			strings.Join(tried, "; "),
		)
	}
}

// TestSelfhostProbeDifferentialRecipes pins the gate's just recipes so the gate
// keeps being reachable the way the other selfhost gates are.
func TestSelfhostProbeDifferentialRecipes(t *testing.T) {
	content, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	gate := justRecipe(string(content), "selfhost-probe-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_PROBES=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostProbeDifferentialGate$")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_NATIVE=1")

	fast := justRecipe(string(content), "selfhost-fast-gate")
	requireRecipeFragment(t, fast, "just selfhost-probe-gate")
}

// TestSelfhostProbeDifferentialGate compares the selfhost compiler against the Go
// reference on every probe, through both selfhost backends, and refuses a run whose
// per-probe result set differs from the checked-in baseline.
//
// Two backends, because the selfhost compiler has two. `run <file>` lowers through
// selfhost::ir::code_render; the module `stage selfhost` emits is lowered by
// selfhost::backend::compiled_mir_lower, which is the shape-dispatching lowering the
// bootstrap depends on and the one being replaced. A probe only reaches the second
// by being a module of the staged package, so the staged phase compiles the corpus
// into a scratch copy of the selfhost package whose root evaluates each declared
// call and prints it.
func TestSelfhostProbeDifferentialGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_PROBES") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_PROBES=1 to run the selfhost probe gate")
	}
	report, failures := runSelfhostProbeDifferential(t)
	if failures > 0 {
		t.Fatalf("selfhost probe differential failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost probe differential report:\n%s", report)
}

// loadProbeBaseline parses the checked-in per-probe result baseline.
func loadProbeBaseline(path string) (map[string]probeCase, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	baseline := map[string]probeCase{}
	for lineNo, line := range strings.Split(string(content), "\n") {
		item, ok, err := parseProbeBaselineLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if !ok {
			continue
		}
		if _, duplicate := baseline[item.name]; duplicate {
			return nil, fmt.Errorf("%s:%d: duplicate baseline row %s", path, lineNo+1, item.name)
		}
		baseline[item.name] = item
	}
	return baseline, nil
}

// parseProbeBaselineLine parses one baseline row.
func parseProbeBaselineLine(line string) (probeCase, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return probeCase{}, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 4 {
		return probeCase{}, false, fmt.Errorf("expected name run stage note")
	}
	item := probeCase{
		name:        fields[0],
		runStatus:   fields[1],
		stageStatus: fields[2],
		note:        strings.Join(fields[3:], " "),
	}
	for _, status := range []string{item.runStatus, item.stageStatus} {
		if status != "ok" && status != "mismatch" && status != "refused" {
			return probeCase{}, false, fmt.Errorf("unknown status %q", status)
		}
	}
	return item, true, nil
}

// listProbeSources returns the sorted probe names in the corpus directory.
func listProbeSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".kizu") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".kizu"))
	}
	sort.Strings(names)
	return names, nil
}

var probeCallsPattern = regexp.MustCompile(`(?m)^// probe-calls:\s*(.+)$`)

var probeReachPattern = regexp.MustCompile(`(?m)^// probe-reach:\s*(.+)$`)

var probePrintPattern = regexp.MustCompile(`(?m)^\s*print\((probe_[^;]+)\);\s*$`)

var probeTryPattern = regexp.MustCompile(`(?m)^\s*try ([^;]+);\s*$`)

// readProbeCalls reads the entry expressions a probe declares.
func readProbeCalls(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	match := probeCallsPattern.FindStringSubmatch(string(content))
	if match == nil {
		return nil, fmt.Errorf("%s has no `// probe-calls:` line", path)
	}
	var calls []string
	for _, call := range strings.Split(match[1], ";") {
		call = strings.TrimSpace(call)
		if call != "" {
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("%s declares no probe calls", path)
	}
	return calls, nil
}

// readProbeReach reads the expressions a probe needs lowered but cannot return
// through the driver. The line is optional: a probe that has no error-union shape
// needs nothing beyond its entry.
func readProbeReach(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	match := probeReachPattern.FindStringSubmatch(string(content))
	if match == nil {
		return nil, nil
	}
	var reach []string
	for _, call := range strings.Split(match[1], ";") {
		call = strings.TrimSpace(call)
		if call != "" {
			reach = append(reach, call)
		}
	}
	if len(reach) == 0 {
		return nil, fmt.Errorf("%s declares an empty `// probe-reach:` line", path)
	}
	return reach, nil
}

// readProbeTriedCalls reads the expressions the probe's own main reaches with `try`.
// A reach expression only earns its place if BOTH phases lower the shape: the staged
// root reaches it for the stage backend, and main reaches it for the run backend.
func readProbeTriedCalls(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tried []string
	for _, match := range probeTryPattern.FindAllStringSubmatch(string(content), -1) {
		tried = append(tried, strings.TrimSpace(match[1]))
	}
	return tried, nil
}

// readProbePrintedCalls reads the expressions the probe's own main prints.
func readProbePrintedCalls(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var printed []string
	for _, match := range probePrintPattern.FindAllStringSubmatch(string(content), -1) {
		printed = append(printed, strings.TrimSpace(match[1]))
	}
	if len(printed) == 0 {
		return nil, fmt.Errorf("%s prints no probe calls", path)
	}
	return printed, nil
}

// loadProbeCases joins the baseline with the corpus sources.
func loadProbeCases() ([]probeCase, error) {
	baseline, err := loadProbeBaseline(probeBaselinePath)
	if err != nil {
		return nil, err
	}
	names, err := listProbeSources(probeCorpusDir)
	if err != nil {
		return nil, err
	}
	var cases []probeCase
	for _, name := range names {
		item, ok := baseline[name]
		if !ok {
			return nil, fmt.Errorf("probe %s has no baseline row", name)
		}
		item.path = filepath.Join(probeCorpusDir, name+".kizu")
		item.calls, err = readProbeCalls(item.path)
		if err != nil {
			return nil, err
		}
		item.reach, err = readProbeReach(item.path)
		if err != nil {
			return nil, err
		}
		cases = append(cases, item)
	}
	if len(cases) != len(baseline) {
		return nil, fmt.Errorf("baseline has %d rows for %d probes", len(baseline), len(cases))
	}
	return cases, nil
}
