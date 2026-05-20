package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

type selfhostPipelineStage struct {
	component string
	fragments []string
}

// TestSelfhostPipelineGate executes the one-pass selfhost production pipeline gate.
func TestSelfhostPipelineGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countSelfhostPipelineGateFailures(t); failures > 0 {
		t.Fatalf("selfhost pipeline gate failures=%d", failures)
	}
}

// runSelfhostPipelineOracle checks production summaries without repeated gate runs.
func runSelfhostPipelineOracle(t *testing.T) []selfhostOracleResult {
	t.Helper()
	out, err := runSelfhostPipelineGate(t)
	if err != nil {
		t.Errorf("selfhost pipeline gate failed: %v\n%s", err, out)
		return pipelineFailureResults(1)
	}
	results := countSelfhostPipelineStageFailures(t, out)
	results[len(results)-1].failures += countSelfhostBackendArtifactFileFailures(t)
	return results
}

// countSelfhostPipelineGateFailures returns all one-pass pipeline gate failures.
func countSelfhostPipelineGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostPipelineGate(t)
	if err != nil {
		t.Errorf("selfhost pipeline gate failed: %v\n%s", err, out)
		return 1
	}
	failures := 0
	for _, result := range countSelfhostPipelineStageFailures(t, out) {
		failures += result.failures
	}
	return failures + countSelfhostBackendArtifactFileFailures(t)
}

// countSelfhostPipelineStageFailures validates output fragments per production stage.
func countSelfhostPipelineStageFailures(
	t *testing.T,
	out string,
) []selfhostOracleResult {
	t.Helper()
	stages := selfhostPipelineStages()
	results := make([]selfhostOracleResult, 0, len(stages))
	for _, stage := range stages {
		results = append(results, selfhostOracleResult{
			component: stage.component,
			corpus:    "selfhost-production",
			scanned:   1,
			compared:  1,
			failures:  countPipelineFragments(t, stage.component, out, stage.fragments),
		})
	}
	return results
}

// selfhostPipelineStages returns production fragments emitted by pipeline_oracle.
func selfhostPipelineStages() []selfhostPipelineStage {
	return []selfhostPipelineStage{
		{
			component: "resolver",
			fragments: []string{
				"resolver-modules\n",
				"resolver-production-symbols\n",
				"resolver-production-diagnostics\n0\n",
			},
		},
		{
			component: "types",
			fragments: []string{
				"type-modules\n",
				"type-production-symbols\n",
				"type-production-functions\n",
				"type-production-typed-nodes\n",
				"type-production-diagnostics\n0\n",
			},
		},
		{
			component: "ownership",
			fragments: []string{
				"ownership-production-resources\n",
				"ownership-production-checked-nodes\n",
				"ownership-production-borrows\n",
				"ownership-production-errors\n0\n",
			},
		},
		{
			component: "ir-handoff",
			fragments: []string{
				"ir-handoff-blocks\n",
				"ir-handoff-entry-points\n1\n",
				"ir-handoff-diagnostics\n0\n",
			},
		},
		{
			component: "ir-artifact",
			fragments: []string{
				"ir-artifact-path\ntarget/selfhost/selfhost.ir\n",
				"ir-manifest-path\ntarget/selfhost/selfhost.ir.manifest\n",
				"backend-artifact-bytes\n",
			},
		},
		{
			component: "backend-artifact",
			fragments: []string{
				"llvm-artifact-path\ntarget/selfhost/selfhost.ll\n",
				"llvm-metadata-path\ntarget/selfhost/selfhost.ll.meta\n",
				"runtime-storage-path\ntarget/selfhost/selfhost.storage.ll\n",
				"runtime-storage-metadata-path\ntarget/selfhost/selfhost.storage.ll.meta\n",
				"host-capability-path\ntarget/selfhost/selfhost.host.ll\n",
				"host-capability-metadata-path\ntarget/selfhost/selfhost.host.ll.meta\n",
				"llvm-artifact-bytes\n",
				"llvm-metadata-bytes\n",
				"runtime-storage-bytes\n",
				"runtime-storage-metadata-bytes\n",
				"host-capability-bytes\n",
				"host-capability-metadata-bytes\n",
			},
		},
	}
}

// countPipelineFragments returns missing output fragments for one pipeline stage.
func countPipelineFragments(
	t *testing.T,
	component string,
	out string,
	fragments []string,
) int {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(out, fragment) {
			t.Errorf("selfhost pipeline %s output missing %q\ngot:\n%s", component, fragment, out)
			return 1
		}
	}
	return 0
}

// pipelineFailureResults reports one pipeline-level failure for every production stage.
func pipelineFailureResults(failures int) []selfhostOracleResult {
	stages := selfhostPipelineStages()
	results := make([]selfhostOracleResult, 0, len(stages))
	for _, stage := range stages {
		results = append(results, selfhostOracleResult{
			component: stage.component,
			corpus:    "selfhost-production",
			scanned:   1,
			compared:  1,
			failures:  failures,
		})
	}
	return results
}

// runSelfhostPipelineGate loads selfhost once and runs the production pipeline oracle.
func runSelfhostPipelineGate(t *testing.T) (string, error) {
	t.Helper()
	if err := os.RemoveAll("../../target/selfhost"); err != nil {
		return "", err
	}
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
	err = interp.New(&out).RunEntry(program, "selfhost::pipeline_oracle::gate")
	return out.String(), err
}
