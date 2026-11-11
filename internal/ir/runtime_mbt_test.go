package ir_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/types"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// The programs here come from the Quint models in spec/ir/bounds.qnt and
// spec/ir/defer.qnt (spec/ir/README.md says what they cover). Unlike the
// slot decision, neither property is a shape of the IR: an index check is
// right when an index out of range still stops the program, and a cleanup is
// right when it runs on the exits SPEC §6.3.1 names. So each fixture is
// rendered as one Kizu program, one function per case, lowered and optimized
// for wasm32-wasi in process, and run under node's WASI on every input the
// model predicts; what it prints is compared with the model.
//
// node is required; without it the tests skip, as the wasmtime tests in
// cmd/kizu do. KIZU_MBT_BIN=<kizu binary> builds with
// `<binary> build --target wasm32-wasi --emit wasm --opt` instead, so the
// same cases can be put to the compiler built from compiler/.
// KIZU_MBT_FIXTURE=<file> replays another fixture (of the test's model).

// runtimeCase is one case of a bounds or defer fixture: the statement tokens
// and, per input, what main prints.
type runtimeCase struct {
	Bucket string   `json:"bucket"`
	Prog   []string `json:"prog"`
	Expect []string `json:"expect"`
}

// boundsKnownShapes mirrors spec/ir/bounds_test.qnt.
var boundsKnownShapes = []runtimeCase{
	{Bucket: "for-over-length", Prog: []string{"for 0", "read I", "end"},
		Expect: []string{"3 0: 33 3", "0 0: 0 0"}},
	{Bucket: "for-over-length-with-pop", Prog: []string{"for 0", "read I", "pop", "end"},
		Expect: []string{"3 0: panic"}},
	{Bucket: "while-over-length-with-pop", Prog: []string{"while 0", "read I", "pop", "end"},
		Expect: []string{"3 0: 21 1"}},
	{Bucket: "guard-then-pop", Prog: []string{"ge K", "lt K", "read K", "pop", "read K", "end", "end"},
		Expect: []string{"2 1: panic", "2 0: 20 1", "2 -1: 0 2"}},
	{Bucket: "exit-unless-below", Prog: []string{"ge K", "exit K", "read K", "end"},
		Expect: []string{"2 2: 0 2", "2 1: 11 2"}},
	{Bucket: "checked-once", Prog: []string{"read K", "read K"},
		Expect: []string{"1 0: 20 1", "1 1: panic"}},
	{Bucket: "write-out-of-range", Prog: []string{"write K", "read K"},
		Expect: []string{"1 1: -7 1", "1 0: 0 1"}},
	{Bucket: "below-is-not-enough", Prog: []string{"lt K", "read K", "end"},
		Expect: []string{"2 -1: panic"}},
	{Bucket: "next-index", Prog: []string{"for 0", "read I1", "end"},
		Expect: []string{"3 0: panic", "0 0: 0 0"}},
}

// deferKnownShapes mirrors spec/ir/defer_test.qnt.
var deferKnownShapes = []runtimeCase{
	{Bucket: "loop-and-exits",
		Prog: []string{"defer 1", "errdefer 2", "loop 5", "defer 3", "continue 0", "break 1",
			"note 100", "end", "return 2", "fail 3", "note 101"},
		Expect: []string{"00: 100 3 100 3 101 1 0", "01: 3 3 101 1 0", "02: 3 101 1 0",
			"04: 100 3 100 3 1 0", "08: 100 3 100 3 2 1 -1"}},
	{Bucket: "errdefer-of-finished-turn",
		Prog:   []string{"loop 1", "errdefer 2", "note 103", "end", "fail 0"},
		Expect: []string{"00: 103 103 0", "01: 103 103 -1"}},
	{Bucket: "nested-error-unwind",
		Prog:   []string{"defer 1", "if 0", "errdefer 2", "loop 3", "defer 4", "fail 1", "end", "end"},
		Expect: []string{"03: 4 2 1 -1", "01: 4 4 1 0", "00: 1 0"}},
	{Bucket: "block-exit-order",
		Prog:   []string{"if 0", "defer 1", "defer 2", "note 100", "end", "note 101"},
		Expect: []string{"01: 100 2 1 101 0", "00: 101 0"}},
}

// knownDivergences are the divergences findings/ir-safety-quint-findings.md
// records and nobody has decided yet, by the error they show. A case that
// shows one is logged instead of failing the test, so the harness still
// tells a new divergence from them. TestKnownDivergencesReproduce fails
// when the smallest program of one no longer shows it: the entry goes when
// the divergence is resolved.
var knownDivergences = []struct {
	id      string
	pattern *regexp.Regexp
	sources [][]string
}{}

// knownDivergence names the known divergence err shows, or "".
func knownDivergence(err error) string {
	for _, known := range knownDivergences {
		if known.pattern.MatchString(err.Error()) {
			return known.id
		}
	}
	return ""
}

// TestKnownDivergencesReproduce builds the smallest program of each known
// divergence and fails when it builds: then the entry and its finding go.
func TestKnownDivergencesReproduce(t *testing.T) {
	if os.Getenv("KIZU_MBT_BIN") != "" {
		t.Skip("the known divergences are recorded for the in-process build")
	}
	for _, known := range knownDivergences {
		for _, source := range known.sources {
			_, err := buildWASI(t, renderCases(t, boundsModel, []runtimeCase{{Prog: source}}, []int{0}), false)
			if err == nil || knownDivergence(err) != known.id {
				t.Errorf("%s no longer reproduces on %v (%v): remove it from knownDivergences and resolve it in findings",
					known.id, source, err)
			}
		}
	}
}

// TestBoundsModelKnownShapes checks the harness on shapes with a known answer.
func TestBoundsModelKnownShapes(t *testing.T) {
	replayRuntime(t, boundsModel, boundsKnownShapes)
}

// TestBoundsModelTraces replays the sampled bounds programs.
func TestBoundsModelTraces(t *testing.T) {
	replayRuntime(t, boundsModel, readRuntimeFixture(t, "ir_bounds.json"))
}

// TestDeferModelKnownShapes checks the harness on shapes with a known answer.
func TestDeferModelKnownShapes(t *testing.T) {
	replayRuntime(t, deferModel, deferKnownShapes)
}

// TestDeferModelTraces replays the sampled defer programs.
func TestDeferModelTraces(t *testing.T) {
	replayRuntime(t, deferModel, readRuntimeFixture(t, "ir_defer.json"))
}

// runtimeModel is how one model's cases become a program and its runs.
type runtimeModel struct {
	// render writes case n as a function; dispatch writes main's call of it.
	render   func(n int, prog []string) (string, error)
	dispatch func(n int) string
	// prelude comes before the case functions; mainHead and mainTail frame
	// run_case(at), which runs the case whose arguments start at at.
	prelude, mainHead, mainTail string
	// width is how many arguments one run takes.
	width int
	// args turns an input of an expectation into the program's arguments.
	args func(input string) ([]string, error)
	// printed reads what one run printed in the model's terms.
	printed func(r wasiResult) string
}

// readRuntimeFixture reads the cases of a fixture in testdata, or the one
// KIZU_MBT_FIXTURE names.
func readRuntimeFixture(t *testing.T, name string) []runtimeCase {
	t.Helper()
	path := os.Getenv("KIZU_MBT_FIXTURE")
	if path == "" {
		path = filepath.Join("..", "..", "compiler", "tests", "mbt", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []runtimeCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	return fixture.Cases
}

// replayRuntime builds every case into one program, runs it on each input
// the cases name, and reports each run whose output differs from the model.
func replayRuntime(t *testing.T, m runtimeModel, cases []runtimeCase) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to run the wasm32-wasi build")
	}
	built, known, binary := buildCases(t, m, cases)
	type run struct {
		c     int
		input string
		want  string
	}
	var runs []run
	var argv [][]string
	for n, c := range cases {
		if !built[n] {
			continue
		}
		for _, expect := range c.Expect {
			input, want, ok := strings.Cut(expect, ": ")
			if !ok {
				t.Fatalf("%s: bad expectation %q", c.Bucket, expect)
			}
			args, err := m.args(input)
			if err != nil {
				t.Fatalf("%s: %v", c.Bucket, err)
			}
			runs = append(runs, run{n, input, want})
			argv = append(argv, append([]string{strconv.Itoa(n)}, args...))
		}
	}
	started := time.Now()
	results, err := runWASI(node, binary, argv)
	t.Logf("ran %d times in %v", len(argv), time.Since(started).Round(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	failed := map[int]bool{}
	for n := range cases {
		if !built[n] && !known[n] {
			failed[n] = true
		}
	}
	for index, r := range runs {
		got := m.printed(results[index])
		if got == r.want {
			continue
		}
		c := cases[r.c]
		if !failed[r.c] {
			fn, _ := m.render(r.c, c.Prog)
			t.Errorf("%s: case%d on input %s: the model prints %q, the program printed %q\nprogram %v\n%s",
				c.Bucket, r.c, r.input, r.want, got, c.Prog, fn)
		} else {
			t.Errorf("%s: case%d on input %s: the model prints %q, the program printed %q",
				c.Bucket, r.c, r.input, r.want, got)
		}
		failed[r.c] = true
	}
	t.Logf("%d programs, %d runs, %d programs diverged, %d show a known divergence",
		len(cases), len(runs), len(failed), len(known))
}

// buildCases builds the cases into one program. When that fails it halves
// the set until it finds the cases that do not build on their own, reports
// them -- the model predicts what each prints, so a program the compiler
// rejects diverges -- and builds the rest together.
func buildCases(t *testing.T, m runtimeModel, cases []runtimeCase) (map[int]bool, map[int]bool, string) {
	t.Helper()
	all := make([]int, len(cases))
	for n := range cases {
		all[n] = n
	}
	built, known := map[int]bool{}, map[int]bool{}
	var split func(chosen []int)
	split = func(chosen []int) {
		_, err := buildWASI(t, renderCases(t, m, cases, chosen), false)
		if err == nil {
			for _, n := range chosen {
				built[n] = true
			}
			return
		}
		if len(chosen) == 1 {
			n := chosen[0]
			if id := knownDivergence(err); id != "" {
				t.Logf("%s: case%d does not build (%s in findings/ir-safety-quint-findings.md): %v",
					cases[n].Bucket, n, id, err)
				known[n] = true
				return
			}
			fn, _ := m.render(n, cases[n].Prog)
			t.Errorf("%s: case%d does not build: %v\nprogram %v\n%s", cases[n].Bucket, n, err, cases[n].Prog, fn)
			return
		}
		split(chosen[:len(chosen)/2])
		split(chosen[len(chosen)/2:])
	}
	binary, err := buildWASI(t, renderCases(t, m, cases, all), true)
	if err == nil {
		for _, n := range all {
			built[n] = true
		}
		return built, known, binary
	}
	t.Logf("building the cases together failed (%v); looking for the ones that do not build", err)
	split(all)
	kept := []int{}
	for _, n := range all {
		if built[n] {
			kept = append(kept, n)
		}
	}
	binary, err = buildWASI(t, renderCases(t, m, cases, kept), true)
	if err != nil {
		t.Fatalf("the cases that build alone do not build together: %v", err)
	}
	return built, known, binary
}

// renderCases writes the chosen cases as one program.
func renderCases(t *testing.T, m runtimeModel, cases []runtimeCase, chosen []int) string {
	t.Helper()
	var source strings.Builder
	source.WriteString(m.prelude)
	for _, n := range chosen {
		fn, err := m.render(n, cases[n].Prog)
		if err != nil {
			t.Fatalf("%s: %v", cases[n].Bucket, err)
		}
		source.WriteString(fn)
	}
	source.WriteString(m.mainHead)
	for _, n := range chosen {
		source.WriteString(m.dispatch(n))
	}
	source.WriteString(m.mainTail)
	fmt.Fprintf(&source, `
fn main() -> !void {
    var at = 0;
    while at < process::arg_count() {
        try run_case(at);
        print("=");
        at = at + %d;
    }
    return;
}
`, m.width)
	return source.String()
}

// buildWASI lowers, optimizes and encodes a program the way
// `kizu build --target wasm32-wasi --emit wasm --opt` does, or runs the
// binary KIZU_MBT_BIN names, and returns the path of the module. In process
// it also logs how many index checks the optimizer answered, which says the
// cases reach the code under test.
func buildWASI(t *testing.T, source string, report bool) (string, error) {
	out := filepath.Join(t.TempDir(), "cases.wasm")
	if bin := os.Getenv("KIZU_MBT_BIN"); bin != "" {
		path := filepath.Join(t.TempDir(), "cases.kizu")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			return "", err
		}
		output, err := exec.Command(bin, "build", "--target", "wasm32-wasi", "--emit", "wasm",
			"--opt", "-o", out, path).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%v\n%s", err, output)
		}
		return out, nil
	}
	target := stdtarget.WasmWASI
	program, err := project.LoadSource("cases.kizu", source)
	if err != nil {
		return "", err
	}
	if err := types.NewForTarget(target).Check(program); err != nil {
		return "", err
	}
	checker := ownership.NewForTarget(target)
	if err := checker.Check(program); err != nil {
		return "", err
	}
	module, err := ir.LowerForTarget(program, checker.Result(), target)
	if err != nil {
		return "", err
	}
	if err := ir.Optimize(module, target); err != nil {
		return "", err
	}
	if report {
		t.Logf("index checks answered in the case functions: %d", answeredChecks(module))
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	lowered, err := wasm.LowerTarget(module, wasm.TargetWASI)
	if err != nil {
		return "", err
	}
	encoded, err := lowered.Binary()
	if err != nil {
		return "", err
	}
	return out, os.WriteFile(out, encoded, 0o644)
}

// answeredChecks counts the Array accesses of the case functions the
// optimizer turned into an unchecked borrow (EliminateBoundsChecks names
// their values %bce<n>.at). It is a coverage figure for the log, not a
// property: how many it answers is the optimizer's precision.
func answeredChecks(module *ir.Module) int {
	count := 0
	for _, fn := range module.Functions {
		if !strings.HasPrefix(fn.Name, "case") && fn.Name != "main" {
			continue
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Result.Name, "%bce") && strings.HasSuffix(instr.Result.Name, ".at") {
					count++
				}
			}
		}
	}
	return count
}

// wasiResult is one run of the module.
type wasiResult struct {
	Code   json.RawMessage `json:"code"`
	Stdout string          `json:"stdout"`
	Stderr string          `json:"stderr"`
}

// runWASI runs the module once per argument list in one node process.
func runWASI(node, binary string, argv [][]string) ([]wasiResult, error) {
	request, err := json.Marshal(map[string]any{"wasm": binary, "runs": argv})
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(node, "--no-warnings", filepath.Join("testdata", "wasi_runs.mjs"))
	cmd.Stdin = bytes.NewReader(request)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("node: %v\n%s", err, stderr.String())
	}
	var results []wasiResult
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, err
	}
	if len(results) != len(argv) {
		return nil, fmt.Errorf("node ran %d of %d", len(results), len(argv))
	}
	return results, nil
}

// decimalFn reads a non-negative decimal argument: std has no parser, and
// an input read at run time is one the optimizer cannot see through.
const decimalFn = `fn decimal(text: []u8) -> i64 {
    var n = 0;
    for 0..mem::len(text) |i| {
        n = n * 10 + cast<i64>(text[i]) - 48;
    }
    return n;
}

`

// printedLines joins the lines a run printed, or names how it ended when it
// did not exit 0.
func printedLines(r wasiResult) string {
	code := strings.TrimSpace(string(r.Code))
	lines := strings.Fields(r.Stdout)
	text := strings.Join(lines, " ")
	if code != "0" {
		return fmt.Sprintf("exit %s: %s %s", code, text, strings.TrimSpace(r.Stderr))
	}
	return text
}

// ---------------------------------------------------------------- bounds

var boundsModel = runtimeModel{
	render: renderBoundsCase,
	dispatch: func(n int) string {
		return fmt.Sprintf("    if which == %d {\n        s = try case%d(&var a, k, allocator);\n    }\n", n, n)
	},
	prelude: "import std::array;\nimport std::mem;\nimport std::process;\n\n" + decimalFn,
	width:   3,
	mainHead: `fn run_case(at: i64) -> !void {
    let allocator = mem::page_allocator();
    let which = decimal(try process::arg(at));
    let length = decimal(try process::arg(at + 1));
    let k = decimal(try process::arg(at + 2)) - 1;
    var a = array::new<i64>(allocator);
    defer a.deinit(allocator);
    for 0..length |i| {
        try a.append(allocator, 10 + i);
    }
    var s = 0;
`,
	mainTail: "    print(s);\n    print(a.len());\n    return;\n}\n",
	// `<len> <k>`; k is passed as k + 1 so it is never negative.
	args: func(input string) ([]string, error) {
		fields := strings.Fields(input)
		if len(fields) != 2 {
			return nil, fmt.Errorf("bad input %q", input)
		}
		k, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		return []string{fields[0], strconv.Itoa(k + 1)}, nil
	},
	// A stop is the bounds panic and nothing printed; anything else is the
	// two lines main prints.
	printed: func(r wasiResult) string {
		code := strings.TrimSpace(string(r.Code))
		if code != "0" && r.Stdout == "" && strings.Contains(r.Stderr, "index out of bounds") {
			return "panic"
		}
		return printedLines(r)
	},
}

// renderBoundsCase writes one bounds program as case<n>.
func renderBoundsCase(n int, prog []string) (string, error) {
	r := boundsRenderer{indent: "    "}
	fmt.Fprintf(&r.b, "fn case%d(a: &var array::Array<i64>, k: i64, allocator: Allocator) -> !i64 {\n    var s = 0;\n", n)
	for _, stmt := range prog {
		if err := r.stmt(stmt); err != nil {
			return "", err
		}
	}
	r.b.WriteString("    return s;\n}\n\n")
	return r.b.String(), nil
}

// boundsBlock is an open block: whether it is a while, whose counter its end
// steps, and the loop id it binds ("" for an if).
type boundsBlock struct {
	while bool
	id    string
}

// boundsRenderer is the state one bounds program is written with: the text so
// far, the indent, the blocks still open and the innermost loop's id.
type boundsRenderer struct {
	b      strings.Builder
	indent string
	open   []boundsBlock
	loop   string
}

// index spells an index token as a Kizu expression.
func (r *boundsRenderer) index(x string) (string, error) {
	switch x {
	case "K":
		return "k", nil
	case "C0":
		return "0", nil
	case "C2":
		return "2", nil
	case "I", "I1":
		if r.loop == "" {
			return "", fmt.Errorf("%s outside a loop", x)
		}
		if x == "I1" {
			return "i" + r.loop + " + 1", nil
		}
		return "i" + r.loop, nil
	}
	return "", fmt.Errorf("unknown index %q", x)
}

// stmt writes one statement.
func (r *boundsRenderer) stmt(stmt string) error {
	f := strings.Fields(stmt)
	var x string
	if len(f) == 2 && f[0] != "for" && f[0] != "while" {
		var err error
		if x, err = r.index(f[1]); err != nil {
			return err
		}
	}
	indent := r.indent
	switch f[0] {
	case "read":
		fmt.Fprintf(&r.b, "%ss = s + a.get_or_panic(%s);\n", indent, x)
	case "write":
		fmt.Fprintf(&r.b, "%sa.set(%s, s) catch return -7;\n", indent, x)
	case "pop":
		fmt.Fprintf(&r.b, "%slet _ = a.pop();\n", indent)
	case "push":
		fmt.Fprintf(&r.b, "%stry a.append(allocator, s);\n", indent)
	case "lt", "ge":
		op := map[string]string{"lt": "< a.len()", "ge": ">= 0"}[f[0]]
		fmt.Fprintf(&r.b, "%sif %s %s {\n", indent, x, op)
		r.push(boundsBlock{})
	case "exit":
		fmt.Fprintf(&r.b, "%sif %s >= a.len() {\n%s    return s;\n%s}\n", indent, x, indent, indent)
	case "for":
		fmt.Fprintf(&r.b, "%slet n%s = a.len();\n%sfor 0..n%s |i%s| {\n", indent, f[1], indent, f[1], f[1])
		r.push(boundsBlock{id: f[1]})
	case "while":
		fmt.Fprintf(&r.b, "%svar i%s = 0;\n%swhile i%s < a.len() {\n", indent, f[1], indent, f[1])
		r.push(boundsBlock{while: true, id: f[1]})
	case "end":
		return r.end()
	default:
		return fmt.Errorf("unknown statement %q", stmt)
	}
	return nil
}

// push opens a block; a loop's id becomes the innermost loop.
func (r *boundsRenderer) push(block boundsBlock) {
	r.open = append(r.open, block)
	if block.id != "" {
		r.loop = block.id
	}
	r.indent += "    "
}

// end closes the innermost block, stepping a while's counter first.
func (r *boundsRenderer) end() error {
	if len(r.open) == 0 {
		return fmt.Errorf("unbalanced block end")
	}
	top := r.open[len(r.open)-1]
	r.open = r.open[:len(r.open)-1]
	if top.while {
		fmt.Fprintf(&r.b, "%si%s = i%s + 1;\n", r.indent, top.id, top.id)
	}
	if top.id != "" {
		r.loop = ""
	}
	r.indent = r.indent[:len(r.indent)-4]
	fmt.Fprintf(&r.b, "%s}\n", r.indent)
	return nil
}

// ---------------------------------------------------------------- defer

var deferModel = runtimeModel{
	render: renderDeferCase,
	dispatch: func(n int) string {
		return fmt.Sprintf("    if which == %d {\n        if case%d(x) |done| {\n            print(done);\n"+
			"        } else |err| {\n            print(-1);\n        }\n    }\n", n, n)
	},
	prelude: `import std::mem;
import std::process;

error Exit {
    Boom,
}

fn note(id: i64) -> void {
    print(id);
}

fn bit(x: i64, b: i64) -> bool {
    var v = x;
    for 0..b |i| {
        v = v / 2;
    }
    return v % 2 == 1;
}

fn fail(yes: bool) -> Exit!void {
    if yes {
        return Exit::Boom;
    }
    return;
}

` + decimalFn,
	width: 2,
	mainHead: `fn run_case(at: i64) -> !void {
    let which = decimal(try process::arg(at));
    let x = decimal(try process::arg(at + 1));
`,
	mainTail: "    return;\n}\n",
	args: func(input string) ([]string, error) {
		x, err := strconv.Atoi(input)
		if err != nil {
			return nil, err
		}
		return []string{strconv.Itoa(x)}, nil
	},
	printed: printedLines,
}

// renderDeferCase writes one defer program as case<n>.
func renderDeferCase(n int, prog []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "fn case%d(x: i64) -> Exit!i64 {\n", n)
	indent := "    "
	depth := 0
	for _, stmt := range prog {
		f := strings.Fields(stmt)
		arg := ""
		if len(f) == 2 {
			arg = f[1]
		}
		guarded := func(body string) {
			fmt.Fprintf(&b, "%sif bit(x, %s) {\n%s    %s\n%s}\n", indent, arg, indent, body, indent)
		}
		switch f[0] {
		case "defer":
			fmt.Fprintf(&b, "%sdefer note(%s);\n", indent, arg)
		case "errdefer":
			fmt.Fprintf(&b, "%serrdefer note(%s);\n", indent, arg)
		case "note":
			fmt.Fprintf(&b, "%snote(%s);\n", indent, arg)
		case "if":
			fmt.Fprintf(&b, "%sif bit(x, %s) {\n", indent, arg)
			indent += "    "
			depth++
		case "loop":
			fmt.Fprintf(&b, "%sfor 0..2 |i%s| {\n", indent, arg)
			indent += "    "
			depth++
		case "end":
			if depth == 0 {
				return "", fmt.Errorf("unbalanced block end")
			}
			depth--
			indent = indent[:len(indent)-4]
			fmt.Fprintf(&b, "%s}\n", indent)
		case "return":
			guarded("return 0;")
		case "fail":
			fmt.Fprintf(&b, "%stry fail(bit(x, %s));\n", indent, arg)
		case "break":
			guarded("break;")
		case "continue":
			guarded("continue;")
		default:
			return "", fmt.Errorf("unknown statement %q", stmt)
		}
	}
	b.WriteString("    return 0;\n}\n\n")
	return b.String(), nil
}
