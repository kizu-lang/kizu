package ownership

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

// The programs here come from the Quint model of the checker in
// spec/ownership/ownership.qnt (README.md there says what it covers). Each case
// is one finished program and the diagnostic classes the model allows for it;
// the test renders it to Kizu, checks it, and compares. A divergence is either
// a checker bug or a model bug, and which one is a question for SPEC.md: record
// it in findings/ownership-quint-findings.md rather than adjusting either side
// to agree.
//
// KIZU_MBT_BIN=<kizu binary> runs `<binary> check` on each program instead of
// the in-process checker, so the same cases can be put to the compiler built
// from compiler/. KIZU_MBT_FIXTURE=<file> replays another fixture, such as a
// wider sample than the checked-in one.

// mbtCase is one program of the fixture scripts/ownership-mbt/main.kizu writes.
type mbtCase struct {
	Bucket string   `json:"bucket"`
	Prog   []string `json:"prog"`
	Expect []string `json:"expect"`
}

// mbtShape writes a known shape: its statements separated by ";" and the
// diagnostic classes the model gives it.
func mbtShape(name string, prog string, expect ...string) mbtCase {
	return mbtCase{Bucket: name, Prog: strings.Split(prog, ";"), Expect: expect}
}

// mbtKnownShapes mirrors spec/ownership/ownership_test.qnt: shapes whose verdict
// is known from issues and examples, so a harness that renders or classifies
// wrongly fails here before it disagrees with the model anywhere else.
var mbtKnownShapes = []mbtCase{
	// #1913: a view read in a loop, then the deinit.
	mbtShape("issue1913/defer", "defer;view 0;loop 1;use_view 0;end"),
	mbtShape("issue1913/explicit", "view 0;loop 1;use_view 0;end;deinit"),
	mbtShape("issue1913/unrelated", "view 0;unrelated 2;loop 1;use_view 0;end;deinit"),
	mbtShape("issue1913/while", "defer;view 0;while 1;use_view 0;end"),
	mbtShape("deinitWhileViewed", "defer;view 0;deinit;use_view 0", "Borrowed", "UseAfterMove"),
	mbtShape("deinitWhileBorrowed", "borrow 0;deinit;use_borrow 0", "Borrowed"),
	mbtShape("loopCarriedView", "defer;view 0;loop 1;use_view 0;mutate;end", "Borrowed"),
	mbtShape("bodyLocalViewEndsWithTurn", "defer;loop 0;view 1;use_view 1;mutate;end"),
	mbtShape("lastUseEndsBorrow", "defer;borrow 0;use_borrow 0;mutate;read"),
	mbtShape("sharedBorrowSeesMutation", "defer;borrow 0;mutate;use_borrow 0", "Borrowed"),
	mbtShape("useAfterMove", "move;read", "UseAfterMove"),
	mbtShape("deferThenDeinit", "defer;deinit", "UseAfterMove"),
	mbtShape("doubleDefer", "defer;defer", "DoubleDefer"),
	mbtShape("neverDeinitialized", "read", "Leak"),
	mbtShape("tryBeforeDefer", "mutate;defer", "Leak"),
	mbtShape("consumeInLoop", "loop 0;deinit;end", "LoopConsume", "UseAfterMove"),
	mbtShape("deferInLoop", "loop 0;defer;end", "LoopConsume", "UseAfterMove"),
	mbtShape("deferInLoopWhileViewed", "view 0;loop 1;defer;end;use_view 0",
		"Borrowed", "LoopConsume", "UseAfterMove"),
	mbtShape("nestedCarriedView", "defer;view 0;loop 1;while 2;use_view 0;end;mutate;end",
		"Borrowed"),
	mbtShape("outerBodyViewEndsAfterInnerLoop",
		"defer;loop 0;view 1;loop 2;use_view 1;end;mutate;end"),
	mbtShape("nestedConsume", "loop 0;loop 1;deinit;end;end", "LoopConsume", "UseAfterMove"),
	mbtShape("shortest/deinit", "deinit"),
	mbtShape("shortest/move", "move"),
	mbtShape("shortest/defer", "defer"),
	mbtShape("shortest/try", "mutate", "Leak"),
	mbtShape("sharedViewAndBorrowCoexist", "view 0;borrow 1;use_view 0;use_borrow 1;deinit"),
}

// TestOwnershipModelKnownShapes checks the harness on shapes with a known verdict.
func TestOwnershipModelKnownShapes(t *testing.T) {
	check := mbtChecker(t)
	for _, c := range mbtKnownShapes {
		if problem := mbtCompare(c, check); problem != "" {
			t.Errorf("%s", problem)
		}
	}
}

// TestOwnershipModelTraces replays the sampled programs.
func TestOwnershipModelTraces(t *testing.T) {
	path := os.Getenv("KIZU_MBT_FIXTURE")
	if path == "" {
		path = filepath.Join("..", "..", "compiler", "tests", "mbt", "ownership.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []mbtCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	check := mbtChecker(t)
	failed := 0
	for _, c := range fixture.Cases {
		if problem := mbtCompare(c, check); problem != "" {
			failed++
			t.Errorf("%s", problem)
		}
	}
	t.Logf("%d programs, %d diverged", len(fixture.Cases), failed)
}

// mbtCompare checks one case and describes how the checker diverged from the
// model, or returns "" when it did not.
func mbtCompare(c mbtCase, check func(string) error) string {
	source, err := mbtRender(c.Prog)
	if err != nil {
		return fmt.Sprintf("%s: %v", c.Bucket, err)
	}
	got := check(source)
	describe := func(what string) string {
		return fmt.Sprintf("%s: %s\nmodel allows %v\nprogram %v\n%s",
			c.Bucket, what, c.Expect, c.Prog, source)
	}
	if got == nil {
		if len(c.Expect) == 0 {
			return ""
		}
		return describe("checker accepted")
	}
	kind := mbtKind(got.Error())
	if kind == "" {
		return describe("checker reported a diagnostic outside the model: " + got.Error())
	}
	if len(c.Expect) == 0 {
		return describe("checker rejected: " + got.Error())
	}
	for _, want := range c.Expect {
		if want == kind {
			return ""
		}
	}
	return describe("checker reported " + kind + ": " + got.Error())
}

// mbtKind names the diagnostic class of a checker message, as the Kind type of
// the model has them, or "" for a message the model does not know.
func mbtKind(message string) string {
	switch {
	case strings.Contains(message, "while string is borrowed"),
		strings.Contains(message, "cannot be moved while borrowed"):
		return "Borrowed"
	case strings.Contains(message, "already has a registered `defer` cleanup"):
		return "DoubleDefer"
	case strings.Contains(message, "moved value `s` was "):
		return "UseAfterMove"
	case strings.Contains(message, "is never deinitialized"),
		strings.Contains(message, "would leak on this `try`"):
		return "Leak"
	case strings.Contains(message, "is consumed inside a loop"):
		return "LoopConsume"
	}
	return ""
}

// mbtPrelude is the frame every program is a body of. `s` is the one owner the
// model follows; `sink` consumes what it is given.
const mbtPrelude = `import std::mem;
import std::string;

fn sink(allocator: Allocator, x: string::String) {
    x.deinit(allocator);
}

fn main() -> !void {
    let allocator = mem::page_allocator();
    var s = try string::from_bytes(allocator, "ab");
`

// mbtLines is the Kizu each statement token stands for, with # for its id.
var mbtLines = map[string][]string{
	"view":       {"let v# = s.as_bytes();"},
	"use_view":   {"print(v#);"},
	"borrow":     {"let r# = &s;"},
	"use_borrow": {"print(r#.len());"},
	"read":       {"print(s.len());"},
	"mutate":     {`try s.append_bytes(allocator, "x");`},
	"deinit":     {"s.deinit(allocator);"},
	"move":       {"sink(allocator, move s);"},
	"defer":      {"defer s.deinit(allocator);"},
	"unrelated":  {"var u# = string::new(allocator);", "defer u#.deinit(allocator);"},
	"loop":       {"for 0..2 |i#| {"},
	"while":      {"var i# = 0;", "while i# < 2 {"},
}

// mbtRender writes a program in the fixture's token form as Kizu.
func mbtRender(prog []string) (string, error) {
	var b strings.Builder
	b.WriteString(mbtPrelude)
	indent := "    "
	// The counters of the open `while` loops, innermost last, or "" for a `for`.
	var counters []string
	for _, stmt := range prog {
		token, id, _ := strings.Cut(stmt, " ")
		if token == "end" {
			if len(counters) == 0 {
				return "", fmt.Errorf("unbalanced loop end")
			}
			if counter := counters[len(counters)-1]; counter != "" {
				b.WriteString(indent + counter + " = " + counter + " + 1;\n")
			}
			counters = counters[:len(counters)-1]
			indent = indent[:len(indent)-4]
			b.WriteString(indent + "}\n")
			continue
		}
		lines, ok := mbtLines[token]
		if !ok {
			return "", fmt.Errorf("unknown statement %q", stmt)
		}
		for _, line := range lines {
			b.WriteString(indent + strings.ReplaceAll(line, "#", id) + "\n")
		}
		switch token {
		case "loop":
			counters = append(counters, "")
		case "while":
			counters = append(counters, "i"+id)
		default:
			continue
		}
		indent += "    "
	}
	b.WriteString("    return;\n}\n")
	return b.String(), nil
}

// mbtChecker returns the checker the cases are put to: the in-process type and
// ownership checks `kizu check` runs, or the binary KIZU_MBT_BIN names.
func mbtChecker(t *testing.T) func(string) error {
	bin := os.Getenv("KIZU_MBT_BIN")
	if bin == "" {
		return func(source string) error {
			program, err := project.LoadSource("mbt.kizu", source)
			if err != nil {
				return err
			}
			if err := types.New().Check(program); err != nil {
				return err
			}
			return New().Check(program)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mbt.kizu")
	return func(source string) error {
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			return err
		}
		output, err := exec.Command(bin, "check", path).CombinedOutput()
		if err == nil {
			return nil
		}
		first, _, _ := strings.Cut(string(output), "\n")
		return fmt.Errorf("%s", first)
	}
}
