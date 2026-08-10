package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// selfhostMirEnvGateOutput is the phi set the SSA environment construction in
// selfhost/src/backend/mir_env.kizu and mir_env_loop.kizu produces for eight
// hand-built control-flow graphs: the shapes that produced the alias defects the
// per-statement-shape lowering in compiled_mir_lower.kizu keeps re-deriving.
//
// Each line is `<block> <result> <type> <- <value>@<block>...`. A gate with no
// phis prints its label alone, which is itself the assertion for the arm that
// binds its own name.
const selfhostMirEnvGateOutput = "loop-with-continue\n" +
	"while.header loop.0 i64 <- n0@entry add@if.end mul@if.then\n" +
	"loop-with-break\n" +
	"while.header loop.0 i64 <- n0@entry add@if.end\n" +
	"while.end exit.1 i64 <- loop.0@while.header zero@if.then\n" +
	"jump-out-of-arm\n" +
	"if2.end join.0 i64 <- x3@if2.then x4@if2.else\n" +
	"arm-binds-its-own-name\n" +
	"join-needs-the-name-in-the-environment\n" +
	"if.end join.0 i64 <- a@if.then x@if.else\n" +
	"loop-over-shadowed-name\n" +
	"while.header loop.0 i64 <- n1@entry add@while.body\n" +
	"two-loops-over-one-name\n" +
	"w1.header loop.0 i64 <- n0@entry add1@w1.body\n" +
	"w2.header loop.1 i64 <- loop.0@w1.end add2@w2.body\n" +
	"nested-loops\n" +
	"o.header loop.0 i64 <- n0@entry loop.1@i.end\n" +
	"i.header loop.1 i64 <- loop.0@o.body add@i.body\n"

// TestSelfhostMirEnvGate runs the environment construction over a loop with a
// continue, a loop with a break, a jump out of an if arm, a name an arm binds
// itself, a join over a name the environment does or does not hold, a loop over
// a shadowed name, two loops over one name, and a nested loop. The if-joins in
// compiled_mir_lower.kizu now go through this construction, so these are the
// answers the emitted module rests on.
func TestSelfhostMirEnvGate(t *testing.T) {
	out, err := runSelfhostMirEnvGate(t, "selfhost::backend::mir_env_gate::gate")
	if err != nil {
		t.Fatalf("mir env gate failed: %v\n%s", err, out)
	}
	if out != selfhostMirEnvGateOutput {
		t.Fatalf("mir env gate output mismatch\nwant:\n%sgot:\n%s", selfhostMirEnvGateOutput, out)
	}
}

// runSelfhostMirEnvGate loads the selfhost package and runs one gate entry,
// returning what it printed. The gate builds its control-flow graphs by hand, so
// it needs no fixture and runs in under a second.
func runSelfhostMirEnvGate(t *testing.T, entry string) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}
