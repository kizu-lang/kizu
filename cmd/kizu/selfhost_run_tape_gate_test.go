package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostRunTapeLoweringGate emits the production codegen component facts and
// drives the compiled MIR lowering over the run codegen IR v1 tape cluster
// (selfhost::ir::codegen::lower_code_expr and its record builders). It is the
// focused debugging gate for the generic run tape work: while a measured blocker
// remains it fails with the exact lowering error, and once the cluster lowers it
// asserts the success marker.
func TestSelfhostRunTapeLoweringGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_RUN_TAPE") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_RUN_TAPE=1 to run the selfhost run tape gate")
	}
	out, err := runSelfhostRunTapeLoweringGate(t)
	if err != nil {
		t.Fatalf("run tape lowering gate failed: %v\n%s", err, out)
	}
	if out != "run-tape-lowering-ok\n" {
		t.Fatalf(
			"run tape lowering gate output mismatch\nwant:\nrun-tape-lowering-ok\ngot:\n%s",
			out,
		)
	}
}

// TestSelfhostRunInterpreterDebugGatesStayOffJustfile keeps the multi-minute
// interpreted run backend internals out of routine recipe discovery. They remain
// available through explicit raw go test commands when a blocker needs them.
func TestSelfhostRunInterpreterDebugGatesStayOffJustfile(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	for _, recipe := range []string{
		"selfhost-run-tape-gate:",
		"selfhost-run-render-gate:",
	} {
		if strings.Contains(content, recipe) {
			t.Fatalf("interpreter-heavy debug recipe %q must stay out of justfile", recipe)
		}
	}
}

// runSelfhostRunTapeLoweringGate loads the selfhost package and drives the gate.
func runSelfhostRunTapeLoweringGate(t *testing.T) (string, error) {
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
	const entry = "selfhost::backend::run_tape_gate::run_tape_lowering_gate"
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}

// TestSelfhostRunStageEmitGate drives the full production LLVM emission over the
// on-disk IR facts through the interpreter, surfacing the exact blocker message
// when the hosted stage exits silently. Focused debugging only.
func TestSelfhostRunStageEmitGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_RUN_STAGE_EMIT") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_RUN_STAGE_EMIT=1 to run the stage emit gate")
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatalf("load selfhost: %v", err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("check selfhost: %v", err)
	}
	var out bytes.Buffer
	const entry = "selfhost::backend::run_tape_gate::run_stage_emit_gate"
	if err := interp.New(&out).RunEntry(program, entry); err != nil {
		t.Fatalf("stage emit gate failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "stage-emit-ok") {
		t.Fatalf("stage emit gate output mismatch:\n%s", out.String())
	}
}
