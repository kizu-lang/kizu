package ir

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

// The programs here come from the Quint model of the slot decision in
// spec/ir/slots.qnt (spec/ir/README.md says what it covers). Each case is a
// set of struct types with same-named methods, three struct locals and three
// integer locals, and statements that call the methods; the model says which
// locals the lowerer gives storage. The test renders the case to Kizu, lowers
// it, reads which locals of main got a `local.slot`, and compares. A
// divergence is a lowerer bug or a model bug, and which one is a question for
// SPEC.md: record it in findings/ir-safety-quint-findings.md rather than
// adjusting either side to agree.
//
// KIZU_MBT_BIN=<kizu binary> reads `<binary> ir` instead of lowering in
// process, so the same cases can be put to the compiler built from
// compiler/ or to an older one. KIZU_MBT_FIXTURE=<file> replays another
// fixture.

// slotCase is one case of the fixture scripts/ir-mbt writes for the slot
// model.
type slotCase struct {
	Bucket string   `json:"bucket"`
	Types  []string `json:"types"`
	Decls  []string `json:"decls"`
	Prog   []string `json:"prog"`
	Expect []string `json:"expect"`
}

// slotKnownShapes mirrors spec/ir/slots_test.qnt: shapes whose answer is
// known from #1914 and tests/behavior/src/method_lend_scope, so a harness
// that renders or reads wrongly fails here before it disagrees with the model
// anywhere else. A is Slot, B is Tally and C is Counter.
var slotKnownShapes = []slotCase{
	{
		Bucket: "issue1914/shared-methods-lend-nothing",
		Types:  []string{"A", "B", "C"},
		Decls:  lendScopeDecls,
		Prog:   []string{"call 0 poke 0", "call 1 note 1"},
		Expect: []string{},
	},
	{
		Bucket: "issue1914/own-methods-still-lend",
		Types:  []string{"A", "B", "C"},
		Decls:  lendScopeDecls,
		Prog:   []string{"call 2 poke 0", "call 2 note 1"},
		Expect: []string{"n1", "x2"},
	},
	{
		Bucket: "deferred-receiver-is-storage",
		Types:  []string{"A", "B", "C"},
		Decls:  lendScopeDecls,
		Prog:   []string{"defer 0 poke 0"},
		Expect: []string{"x0"},
	},
	{
		Bucket: "lent-inside-block",
		Types:  []string{"A", "B", "C"},
		Decls:  lendScopeDecls,
		Prog:   []string{"if", "loop 0", "bump 2", "end", "end", "bump_explicit 0"},
		Expect: []string{"n0", "n2"},
	},
}

// lendScopeDecls are the declarations of method_lend_scope in the model's terms.
var lendScopeDecls = []string{
	"A.poke RShared AVal", "B.note RShared AVal", "C.note RShared ALent", "C.poke RLent AVal",
}

// TestSlotModelKnownShapes checks the harness on shapes with a known answer.
func TestSlotModelKnownShapes(t *testing.T) {
	lower := slotLowerer(t)
	for _, c := range slotKnownShapes {
		if problem := slotCompare(c, lower); problem != "" {
			t.Errorf("%s", problem)
		}
	}
}

// TestSlotModelTraces replays the sampled programs.
func TestSlotModelTraces(t *testing.T) {
	path := os.Getenv("KIZU_MBT_FIXTURE")
	if path == "" {
		path = filepath.Join("testdata", "slots_mbt.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []slotCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	lower := slotLowerer(t)
	failed := 0
	for _, c := range fixture.Cases {
		if problem := slotCompare(c, lower); problem != "" {
			failed++
			t.Errorf("%s", problem)
		}
	}
	t.Logf("%d programs, %d diverged", len(fixture.Cases), failed)
}

// slotCompare lowers one case and describes how the slots of main differ
// from the model's, or returns "" when they do not.
func slotCompare(c slotCase, lower func(string) (string, error)) string {
	source, err := slotRender(c)
	if err != nil {
		return fmt.Sprintf("%s: %v", c.Bucket, err)
	}
	describe := func(what string) string {
		return fmt.Sprintf("%s: %s\nmodel gives storage to %v\ndecls %v types %v\nprogram %v\n%s",
			c.Bucket, what, c.Expect, c.Decls, c.Types, c.Prog, source)
	}
	dump, err := lower(source)
	if err != nil {
		return describe("the front end or the lowerer rejected the program: " + err.Error())
	}
	got, err := slottedLocals(dump)
	if err != nil {
		return describe(err.Error())
	}
	want := append([]string{}, c.Expect...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		return describe(fmt.Sprintf("the lowerer gave storage to %v", got))
	}
	return ""
}

// The initial values that name the locals in the dump: x<i> is built from
// slotStructBase+i, n<k> is slotIntBase+k. Nothing else in a rendered main is
// a constant this large.
const (
	slotStructBase = 100
	slotIntBase    = 200
)

var (
	slotConstLine  = regexp.MustCompile(`^\s+(%\d+): i64 = const (\d+)$`)
	slotStructLine = regexp.MustCompile(`^\s+(%\d+): [ABC] = struct\.new \{n: (%\d+): i64\}$`)
	slotSlotLine   = regexp.MustCompile(`^\s+%\d+: &var \S+ = local\.slot (%\d+): \S+$`)
)

// slottedLocals reads which locals of main a lowered module gives storage:
// the value each local is declared with is a constant, or a struct built
// from one, that names it, and a `local.slot` of that value is its storage.
func slottedLocals(dump string) ([]string, error) {
	start := strings.Index(dump, "fn main() -> void {")
	if start < 0 {
		return nil, fmt.Errorf("no main in the lowered module")
	}
	body := dump[start:]
	if end := strings.Index(body, "\n}"); end >= 0 {
		body = body[:end]
	}
	names := map[string]string{}
	found := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if m := slotConstLine.FindStringSubmatch(line); m != nil {
			var n int
			fmt.Sscan(m[2], &n)
			switch {
			case n >= slotStructBase && n < slotStructBase+3:
				names[m[1]] = fmt.Sprintf("x%d", n-slotStructBase)
			case n >= slotIntBase && n < slotIntBase+3:
				names[m[1]] = fmt.Sprintf("n%d", n-slotIntBase)
			}
			continue
		}
		if m := slotStructLine.FindStringSubmatch(line); m != nil {
			if name, ok := names[m[2]]; ok && strings.HasPrefix(name, "x") {
				names[m[1]] = name
			}
			continue
		}
		if m := slotSlotLine.FindStringSubmatch(line); m != nil {
			name, ok := names[m[1]]
			if !ok {
				return nil, fmt.Errorf("a local.slot of %s, which names no local:\n%s", m[1], body)
			}
			found[name] = true
		}
	}
	got := make([]string, 0, len(found))
	for name := range found {
		got = append(got, name)
	}
	sort.Strings(got)
	return got, nil
}

// slotRender writes a case as Kizu: the types, their declared methods, the
// free function that takes `&var i64`, and main.
func slotRender(c slotCase) (string, error) {
	var b strings.Builder
	for _, name := range []string{"A", "B", "C"} {
		fmt.Fprintf(&b, "struct %s {\n    n: i64,\n}\n\n", name)
	}
	for _, decl := range c.Decls {
		fields := strings.Fields(decl)
		if len(fields) != 3 {
			return "", fmt.Errorf("bad declaration %q", decl)
		}
		typeName, method, _ := strings.Cut(fields[0], ".")
		recv := map[string]string{"RShared": "&", "RLent": "&var ", "RValue": ""}[fields[1]]
		arg := map[string]string{"AVal": "i64", "ALent": "&var i64"}[fields[2]]
		fmt.Fprintf(&b, "fn (self: %s%s) %s(total: %s) -> void {\n", recv, typeName, method, arg)
		lentRecv, lentArg := fields[1] == "RLent", fields[2] == "ALent"
		switch {
		case lentRecv && lentArg:
			b.WriteString("    self.n = self.n + 1;\n    total.* = total.* + self.n;\n")
		case lentRecv:
			b.WriteString("    self.n = self.n + total;\n")
		case lentArg:
			b.WriteString("    total.* = total.* + self.n;\n")
		default:
			b.WriteString("    print(self.n + total);\n")
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("fn bump(total: &var i64) -> void {\n    total.* = total.* + 1;\n}\n\n")
	b.WriteString("fn main() -> void {\n")
	for i, typeName := range c.Types {
		fmt.Fprintf(&b, "    var x%d = %s { n: %d };\n", i, typeName, slotStructBase+i)
	}
	for k := 0; k < 3; k++ {
		fmt.Fprintf(&b, "    var n%d = %d;\n", k, slotIntBase+k)
	}
	indent := "    "
	for _, stmt := range c.Prog {
		f := strings.Fields(stmt)
		switch f[0] {
		case "call", "defer":
			prefix := ""
			if f[0] == "defer" {
				prefix = "defer "
			}
			fmt.Fprintf(&b, "%s%sx%s.%s(n%s);\n", indent, prefix, f[1], f[2], f[3])
		case "bump":
			fmt.Fprintf(&b, "%sbump(n%s);\n", indent, f[1])
		case "bump_explicit":
			fmt.Fprintf(&b, "%sbump(&var n%s);\n", indent, f[1])
		case "if":
			fmt.Fprintf(&b, "%sif n0 > 0 {\n", indent)
			indent += "    "
		case "loop":
			fmt.Fprintf(&b, "%sfor 0..2 |i%s| {\n", indent, f[1])
			indent += "    "
		case "end":
			if len(indent) <= 4 {
				return "", fmt.Errorf("unbalanced block end")
			}
			indent = indent[:len(indent)-4]
			fmt.Fprintf(&b, "%s}\n", indent)
		default:
			return "", fmt.Errorf("unknown statement %q", stmt)
		}
	}
	for i := range c.Types {
		fmt.Fprintf(&b, "    print(x%d.n);\n", i)
	}
	for k := 0; k < 3; k++ {
		fmt.Fprintf(&b, "    print(n%d);\n", k)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// slotLowerer returns how a case is lowered to a dump: in process, the type
// and ownership checks and Lower that `kizu ir` runs, or the binary
// KIZU_MBT_BIN names.
func slotLowerer(t *testing.T) func(string) (string, error) {
	bin := os.Getenv("KIZU_MBT_BIN")
	if bin == "" {
		return func(source string) (string, error) {
			program, err := project.LoadSource("mbt.kizu", source)
			if err != nil {
				return "", err
			}
			if err := types.New().Check(program); err != nil {
				return "", err
			}
			checker := ownership.New()
			if err := checker.Check(program); err != nil {
				return "", err
			}
			module, err := Lower(program, checker.Result())
			if err != nil {
				return "", err
			}
			return Dump(module), nil
		}
	}
	path := filepath.Join(t.TempDir(), "mbt.kizu")
	return func(source string) (string, error) {
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			return "", err
		}
		output, err := exec.Command(bin, "ir", path).CombinedOutput()
		if err != nil {
			first, _, _ := strings.Cut(string(output), "\n")
			return "", fmt.Errorf("%s", first)
		}
		return string(output), nil
	}
}
