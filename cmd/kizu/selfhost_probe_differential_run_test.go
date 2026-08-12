package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const probeReferencePath = "target/selfhost/probes/kizu"

// runSelfhostProbeDifferential runs both phases and diffs the result set against
// the checked-in baseline.
func runSelfhostProbeDifferential(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	cases, err := loadProbeCases()
	if err != nil {
		t.Errorf("load probe cases: %v", err)
		return "", 1
	}
	runner, failures := prepareProbeGateRunner(t)
	if failures > 0 {
		return "", failures
	}
	start := time.Now()
	var report strings.Builder
	appendProbeGateHeader(&report, runner, len(cases))
	observations := runProbeRunPhase(t, runner, cases)
	failures += runProbeStagePhase(t, runner, cases, observations)
	failures += countProbeBaselineDrift(t, &report, cases, observations)
	appendProbeGateFooter(&report, start, failures)
	if err := writeProbeGateReport(report.String()); err != nil {
		t.Errorf("write probe gate report: %v", err)
		failures++
	}
	return report.String(), failures
}

// prepareProbeGateRunner returns the selfhost compiler under test. It is built
// from the working tree unless KIZU_SELFHOST_PROBE_RUNNER names one, which is how
// a compiler from another commit is put under the same gate.
func prepareProbeGateRunner(t *testing.T) (string, int) {
	t.Helper()
	for _, dir := range []string{probeGateDir, probeGateCacheDir, "target/selfhost/reports"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Errorf("prepare %s: %v", dir, err)
			return "", 1
		}
	}
	if override := strings.TrimSpace(os.Getenv("KIZU_SELFHOST_PROBE_RUNNER")); override != "" {
		if _, err := os.Stat(override); err != nil {
			t.Errorf("KIZU_SELFHOST_PROBE_RUNNER %s: %v", override, err)
			return "", 1
		}
		return override, 0
	}
	build := buildNativeSelfhost(t, nativeSelfhostBuildConfig{
		name:       "probe-gate selfhost",
		outputPath: probeGateRunner,
		cacheDir:   probeGateCacheDir,
	})
	if build.code != 0 {
		t.Errorf("probe gate runner build failed: %s", build.stderr)
		return "", 1
	}
	return probeGateRunner, 0
}

// runProbeRunPhase compares `<runner> run <probe>` against the Go reference for
// every probe, in parallel, and records what each probe did.
func runProbeRunPhase(
	t *testing.T,
	runner string,
	cases []probeCase,
) map[string]*probeObservation {
	t.Helper()
	reference, err := buildProbeReference()
	if err != nil {
		t.Errorf("build Go reference: %v", err)
		return map[string]*probeObservation{}
	}
	observations := map[string]*probeObservation{}
	var mutex sync.Mutex
	var group sync.WaitGroup
	for _, item := range cases {
		observations[item.name] = &probeObservation{name: item.name}
		group.Add(1)
		go func(item probeCase) {
			defer group.Done()
			observed := observeProbeRun(reference, runner, item)
			mutex.Lock()
			defer mutex.Unlock()
			observations[item.name] = observed
		}(item)
	}
	group.Wait()
	return observations
}

// observeProbeRun runs one probe through both paths and classifies the pair.
func observeProbeRun(reference string, runner string, item probeCase) *probeObservation {
	observed := &probeObservation{
		name:      item.name,
		reference: runProbeCommand(reference, "run", item.path),
		subject:   runProbeCommand(runner, "run", item.path),
	}
	observed.runStatus = "ok"
	if observed.subject.code != observed.reference.code ||
		observed.subject.stdout != observed.reference.stdout ||
		observed.subject.stderr != observed.reference.stderr {
		observed.runStatus = classifyProbeDisagreement(observed.subject)
		observed.stageNote = probeMismatchNote(observed)
	}
	return observed
}

// classifyProbeDisagreement separates a backend that declines a shape from one
// that answers it wrongly. A refusal is loud and ships no wrong answer; a mismatch
// is a wrong answer nobody sees. They are different findings and the baseline keeps
// them apart.
func classifyProbeDisagreement(subject selfhostCommandResult) string {
	if subject.code != 0 && strings.Contains(subject.stderr, "not supported") {
		return "refused"
	}
	if subject.code != 0 && strings.Contains(subject.stderr, "unsupported") {
		return "refused"
	}
	return "mismatch"
}

// probeMismatchNote renders a short, stable description of a run disagreement.
func probeMismatchNote(observed *probeObservation) string {
	return fmt.Sprintf(
		"go[%d %q %q] selfhost[%d %q %q]",
		observed.reference.code,
		oneLine(observed.reference.stdout),
		oneLine(observed.reference.stderr),
		observed.subject.code,
		oneLine(observed.subject.stdout),
		oneLine(observed.subject.stderr),
	)
}

// oneLine collapses captured process text so a failure fits one report line.
func oneLine(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
}

// buildProbeReference builds the Go reference compiler once per gate run.
func buildProbeReference() (string, error) {
	build := exec.Command("go", "build", "-o", probeReferencePath, "./cmd/kizu")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return probeReferencePath, nil
}

// runProbeCommand runs one compiler command and captures its user-visible result.
func runProbeCommand(exePath string, args ...string) selfhostCommandResult {
	start := time.Now()
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		absExe = exePath
	}
	run := exec.Command(absExe, args...)
	run.Env = append(os.Environ(), "KIZU_CACHE_DIR="+probeGateCacheDir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	return selfhostCommandResult{
		name:    strings.Join(args, " "),
		command: absExe + " " + strings.Join(args, " "),
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// countProbeBaselineDrift compares this run's per-probe results to the baseline.
// A probe that used to pass and no longer does is a failure; so is a probe that
// now passes while the baseline still records a disagreement, because a baseline
// nobody updates stops describing the compiler.
func countProbeBaselineDrift(
	t *testing.T,
	report *strings.Builder,
	cases []probeCase,
	observations map[string]*probeObservation,
) int {
	t.Helper()
	failures := 0
	for _, item := range cases {
		observed, ok := observations[item.name]
		if !ok {
			t.Errorf("probe %s was not observed", item.name)
			failures++
			continue
		}
		failures += countProbeStatusDrift(t, item, observed)
		appendProbeResult(report, item, observed)
	}
	return failures
}

// countProbeStatusDrift reports the run and stage status changes for one probe.
func countProbeStatusDrift(t *testing.T, item probeCase, observed *probeObservation) int {
	t.Helper()
	failures := 0
	if observed.runStatus != item.runStatus {
		t.Errorf(
			"probe %s run status %s, baseline %s: %s",
			item.name, observed.runStatus, item.runStatus, observed.stageNote,
		)
		failures++
	}
	if observed.stageStatus != item.stageStatus {
		t.Errorf(
			"probe %s stage status %s, baseline %s: %s",
			item.name, observed.stageStatus, item.stageStatus, observed.stageNote,
		)
		failures++
	}
	return failures
}

// appendProbeGateHeader writes durable gate metadata.
func appendProbeGateHeader(out *strings.Builder, runner string, count int) {
	fmt.Fprintf(out, "kizu-selfhost-probe-differential-v0\n")
	fmt.Fprintf(out, "corpus %s\n", probeCorpusDir)
	fmt.Fprintf(out, "baseline %s\n", probeBaselinePath)
	fmt.Fprintf(out, "runner %s\n", runner)
	fmt.Fprintf(out, "reference go cmd/kizu run\n")
	fmt.Fprintf(out, "run.backend selfhost::ir::code_render\n")
	fmt.Fprintf(out, "stage.backend selfhost::backend::compiled_mir_lower\n")
	fmt.Fprintf(out, "probes %d\n", count)
}

// appendProbeResult writes one probe's observed result group.
func appendProbeResult(out *strings.Builder, item probeCase, observed *probeObservation) {
	fmt.Fprintf(out, "probe.%s.run %s\n", item.name, observed.runStatus)
	fmt.Fprintf(out, "probe.%s.stage %s\n", item.name, observed.stageStatus)
	fmt.Fprintf(out, "probe.%s.reference.exit %d\n", item.name, observed.reference.code)
	fmt.Fprintf(
		out,
		"probe.%s.reference.stdout.sha256 %s\n",
		item.name,
		textFingerprint(observed.reference.stdout),
	)
	fmt.Fprintf(out, "probe.%s.run.exit %d\n", item.name, observed.subject.code)
	fmt.Fprintf(
		out,
		"probe.%s.run.stdout.sha256 %s\n",
		item.name,
		textFingerprint(observed.subject.stdout),
	)
	fmt.Fprintf(
		out,
		"probe.%s.stage.stdout.sha256 %s\n",
		item.name,
		textFingerprint(observed.stageStdout),
	)
	if observed.stageNote != "" {
		fmt.Fprintf(out, "probe.%s.note %s\n", item.name, observed.stageNote)
	}
}

// appendProbeGateFooter writes elapsed time and pass/fail status.
func appendProbeGateFooter(out *strings.Builder, start time.Time, failures int) {
	fmt.Fprintf(out, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(out, "comparison.status pass\n")
		return
	}
	fmt.Fprintf(out, "comparison.status fail\n")
}

// writeProbeGateReport persists the probe gate report.
func writeProbeGateReport(report string) error {
	return os.WriteFile(
		"target/selfhost/reports/probe-differential.txt",
		[]byte(report),
		0o644,
	)
}
