// Command cimport-mbt samples programs from the Quint model of the C header
// import and writes them, with what the model predicts, as the fixture
// internal/cimport/mbt_test.go replays against the importer and the checker.
//
// Run it from the repository root with quint on PATH:
//
//	just spec-cimport
//
// Each program is a C header and an optional Kizu caller of its first
// function. The model predicts two outcomes: under the faithful mapping
// (`expect`, `decls`) and under the importer as it is (`implemented`, written
// only where it differs, with the findings that explain it). Random runs
// mostly repeat common shapes, so the samples are stratified: each program
// falls into a bucket named by the predicted classes, the findings and the
// rarest construct it uses, and each bucket keeps at most -per programs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// main reports a failure on stderr and in the exit status.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cimport-mbt:", err)
		os.Exit(1)
	}
}

// Caller is the Kizu function that calls the header's first function.
type Caller struct {
	Marked bool     `json:"marked"`
	Args   []string `json:"args"`
	Ret    string   `json:"ret"`
}

// Prediction is one outcome: the diagnostic classes the pipeline may report
// (empty: it must accept), and the declarations the importer writes when it
// imports the header at all.
type Prediction struct {
	Expect []string `json:"expect"`
	Decls  *string  `json:"decls,omitempty"`
}

// Case is one program of the fixture.
type Case struct {
	Bucket      string      `json:"bucket"`
	Header      string      `json:"header"`
	Caller      *Caller     `json:"caller,omitempty"`
	Spec        Prediction  `json:"spec"`
	Implemented *Prediction `json:"implemented,omitempty"`
	Findings    []string    `json:"findings,omitempty"`
}

// Fixture is the file mbt_test.go reads.
type Fixture struct {
	Note  string `json:"note"`
	Cases []Case `json:"cases"`
}

// sampler collects the distinct programs of several quint runs.
type sampler struct {
	model     string
	main      string
	invariant string
	dir       string
	samples   int
	seen      map[string]bool
	buckets   map[string][]Case
}

// run samples every generator mode over several seeds and writes the fixture.
func run() error {
	model := flag.String("model", "spec/cimport/cimport.qnt", "the Quint model")
	mainModule := flag.String("main", "spec", "the module of the model to run")
	out := flag.String("out", "internal/cimport/testdata/cimport_mbt.json", "the fixture to write")
	samples := flag.Int("samples", 1000, "runs per seed and mode")
	seeds := flag.Int("seeds", 3, "seeds per mode")
	per := flag.Int("per", 3, "programs kept per bucket")
	invariant := flag.String("invariant", "soundness",
		"the invariant each run checks; \"\" for none, to sample a mutant model")
	flag.Parse()

	dir, err := os.MkdirTemp("", "cimport-mbt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	s := &sampler{model: *model, main: *mainModule, invariant: *invariant, dir: dir, samples: *samples,
		seen: map[string]bool{}, buckets: map[string][]Case{}}
	for _, init := range []string{"initGuided", "initOneMistake", "initFree", "initReadonly"} {
		for seed := 1; seed <= *seeds; seed++ {
			if err := s.sample(init, seed); err != nil {
				return err
			}
		}
	}
	fixture := s.fixture(*per)
	text, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(text, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%d distinct programs, %d buckets, %d kept\n",
		len(s.seen), len(s.buckets), len(fixture.Cases))
	return nil
}

// sample runs quint once from one initial action and seed and files each new
// program in its bucket. The run checks the soundness invariant too: a model
// that breaks it has no business predicting the implementation.
func (s *sampler) sample(init string, seed int) error {
	traces := filepath.Join(s.dir, fmt.Sprintf("%s_%d", init, seed))
	if err := os.MkdirAll(traces, 0o755); err != nil {
		return err
	}
	// The TypeScript backend: the rust evaluator is fetched from GitHub on
	// first use, which an offline machine cannot do (spec/cimport/README.md).
	args := []string{"run", s.model, "--main", s.main,
		"--init", init, "--backend", "typescript", "--seed", fmt.Sprintf("0x%x", seed),
		"--max-samples", fmt.Sprint(s.samples), "--n-traces", fmt.Sprint(s.samples),
		"--max-steps", "1", "--out-itf", filepath.Join(traces, "t_{seq}.itf.json")}
	if s.invariant != "" {
		args = append(args, "--invariant", s.invariant)
	}
	if output, err := exec.Command("quint", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("quint run --init %s --seed %d: %v\n%s", init, seed, err, output)
	}
	files, err := filepath.Glob(filepath.Join(traces, "*.itf.json"))
	if err != nil {
		return err
	}
	for _, file := range files {
		c, ok, err := readCase(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		key := caseKey(c)
		if !ok || s.seen[key] {
			continue
		}
		s.seen[key] = true
		s.buckets[c.Bucket] = append(s.buckets[c.Bucket], c)
	}
	return nil
}

// caseKey identifies a program by its text.
func caseKey(c Case) string {
	key := c.Header
	if c.Caller != nil {
		key += fmt.Sprintf("|%v|%s|%s", c.Caller.Marked, strings.Join(c.Caller.Args, ","), c.Caller.Ret)
	}
	return key
}

// fixture keeps at most per programs of each bucket.
func (s *sampler) fixture(per int) Fixture {
	fixture := Fixture{
		Note: "generated by `just spec-cimport` from spec/cimport/cimport.qnt; do not edit",
	}
	names := make([]string, 0, len(s.buckets))
	for name := range s.buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cases := s.buckets[name]
		// A hash of the program, not the order quint found it in, picks the
		// kept ones, so a bucket keeps long programs as well as short ones.
		sort.Slice(cases, func(i, j int) bool {
			return hashKey(caseKey(cases[i])) < hashKey(caseKey(cases[j]))
		})
		if len(cases) > per {
			cases = cases[:per]
		}
		fixture.Cases = append(fixture.Cases, cases...)
	}
	return fixture
}

// hashKey orders programs without favoring short ones.
func hashKey(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

// The model's values as the Informal Trace Format writes them. The model has
// no integers in its state, so records, lists, sets, strings and bools are all.

type itfSet[T any] struct {
	Set []T `json:"#set"`
}

type q struct {
	C bool `json:"c"`
	V bool `json:"v"`
	R bool `json:"r"`
}

type cty struct {
	Base  []string `json:"base"`
	BC    bool     `json:"bc"`
	BV    bool     `json:"bv"`
	After bool     `json:"after"`
	Stars []q      `json:"stars"`
}

type param struct {
	Form string `json:"form"`
	Ty   cty    `json:"ty"`
	Name string `json:"name"`
}

type decl struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Ret    cty     `json:"ret"`
	Glue   bool    `json:"glue"`
	Plist  string  `json:"plist"`
	Params []param `json:"params"`
}

type kty struct {
	Base     string `json:"base"`
	Consts   []bool `json:"consts"`
	Nullable bool   `json:"nullable"`
}

type kdecl struct {
	Name   string `json:"name"`
	Params []struct {
		Name string `json:"name"`
		Ty   kty    `json:"ty"`
	} `json:"params"`
	Ret kty `json:"ret"`
}

type outcome struct {
	Errs  itfSet[string] `json:"errs"`
	Decls []kdecl        `json:"decls"`
}

type use struct {
	Call   bool     `json:"call"`
	Marked bool     `json:"marked"`
	Args   []string `json:"args"`
	Ret    string   `json:"ret"`
}

type callerTypes struct {
	Args []kty `json:"args"`
	Ret  kty   `json:"ret"`
}

// stateVar decodes a state variable by its name, whatever module prefix the
// instance gave it.
func stateVar(state map[string]json.RawMessage, name string, into any) error {
	for key, value := range state {
		if key == name || strings.HasSuffix(key, "::"+name) {
			return json.Unmarshal(value, into)
		}
	}
	return fmt.Errorf("no state variable %q", name)
}

// readCase reads the last state of one trace.
func readCase(file string) (Case, bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Case{}, false, err
	}
	var trace struct {
		States []map[string]json.RawMessage `json:"states"`
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		return Case{}, false, err
	}
	if len(trace.States) == 0 {
		return Case{}, false, nil
	}
	last := trace.States[len(trace.States)-1]
	var done bool
	if err := stateVar(last, "done", &done); err != nil || !done {
		return Case{}, false, err
	}
	var hdr []decl
	var u use
	var expected, implemented outcome
	var callerTy callerTypes
	var why itfSet[string]
	for name, into := range map[string]any{"hdr": &hdr, "use": &u, "expected": &expected,
		"implemented": &implemented, "caller": &callerTy, "why": &why} {
		if err := stateVar(last, name, into); err != nil {
			return Case{}, false, err
		}
	}
	c := Case{Header: renderHeader(hdr), Spec: predict(expected)}
	if u.Call {
		caller := &Caller{Marked: u.Marked, Args: []string{}, Ret: renderKTy(callerTy.Ret)}
		for _, arg := range callerTy.Args {
			caller.Args = append(caller.Args, renderKTy(arg))
		}
		c.Caller = caller
	}
	if impl := predict(implemented); !samePrediction(impl, c.Spec) {
		c.Implemented = &impl
		c.Findings = append([]string{}, why.Set...)
		sort.Strings(c.Findings)
		if len(c.Findings) == 0 {
			return Case{}, false, fmt.Errorf("the importer diverges here with no finding to explain it")
		}
	}
	c.Bucket = bucket(c, hdr, u)
	return c, true, nil
}

// predict writes one outcome as the fixture spells it.
func predict(o outcome) Prediction {
	p := Prediction{Expect: append([]string{}, o.Errs.Set...)}
	sort.Strings(p.Expect)
	if len(o.Decls) > 0 {
		lines := make([]string, 0, len(o.Decls))
		for _, d := range o.Decls {
			lines = append(lines, renderKDecl(d))
		}
		sort.Strings(lines) // the importer sorts what it writes
		text := strings.Join(lines, "\n")
		p.Decls = &text
	}
	return p
}

// samePrediction compares two outcomes as the fixture records them.
func samePrediction(a, b Prediction) bool {
	if strings.Join(a.Expect, ",") != strings.Join(b.Expect, ",") {
		return false
	}
	if (a.Decls == nil) != (b.Decls == nil) {
		return false
	}
	return a.Decls == nil || *a.Decls == *b.Decls
}

// bucket names a program by its predicted classes, its findings and the
// rarest construct it uses.
func bucket(c Case, hdr []decl, u use) string {
	classes := "accept"
	if len(c.Spec.Expect) > 0 {
		classes = strings.Join(c.Spec.Expect, "+")
	}
	if c.Implemented != nil {
		impl := "accept"
		if len(c.Implemented.Expect) > 0 {
			impl = strings.Join(c.Implemented.Expect, "+")
		}
		classes += "/impl:" + impl + "/" + strings.Join(c.Findings, "+")
	}
	return classes + "/" + feature(c, hdr, u)
}

// feature is the rarest construct of a program, in a fixed order. An argument
// the caller types other than as C does comes first: each kind is one rule of
// the checker, and the classes alone do not tell them apart.
func feature(c Case, hdr []decl, u use) string {
	if kind := oddArgument(u); kind != "" {
		return "arg-" + kind
	}
	first := hdr[0]
	params := first.Params
	has := func(pred func(p param) bool) bool {
		for _, p := range params {
			if pred(p) {
				return true
			}
		}
		return false
	}
	switch {
	case len(hdr) > 1:
		return "two-decls"
	case has(func(p param) bool { return p.Form == "typed" && len(p.Ty.Stars) >= 2 }):
		return "deep-pointer"
	case has(func(p param) bool { return p.Form == "typed" && p.Name == "" }):
		return "unnamed"
	case c.Caller != nil && len(c.Caller.Args) >= 2:
		return "call-2"
	case len(first.Ret.Stars) > 0:
		return "ptr-ret"
	case c.Caller != nil:
		return "call"
	case has(func(p param) bool { return p.Form == "typed" && len(p.Ty.Stars) > 0 }):
		return "pointer"
	}
	return "plain"
}

// oddArgument is the first kind of argument, in a fixed order, that the
// caller types other than as C does, or "".
func oddArgument(u use) string {
	if !u.Call {
		return ""
	}
	for _, kind := range []string{"slice", "nullable", "wide", "readonly"} {
		for _, sel := range u.Args {
			if sel == kind {
				return kind
			}
		}
	}
	return ""
}

// renderHeader writes the C declarations of the model, one per line.
func renderHeader(hdr []decl) string {
	var b strings.Builder
	for _, d := range hdr {
		switch d.Kind {
		case "define":
			b.WriteString("#define X 1\n") // no `;`: it runs into the next declaration
		case "typedef":
			b.WriteString("typedef int my_int;\n")
		case "struct":
			b.WriteString("struct point { int x; };\n")
		case "enum":
			b.WriteString("enum color { RED };\n")
		case "var":
			b.WriteString("int counter;\n")
		default:
			b.WriteString(renderProto(d) + ";\n")
		}
	}
	return b.String()
}

// renderProto writes one prototype.
func renderProto(d decl) string {
	ret := renderCTy(d.Ret)
	glued := d.Glue && len(d.Ret.Stars) > 0 && d.Ret.Stars[len(d.Ret.Stars)-1] == (q{})
	sep := " "
	if glued {
		sep = ""
	}
	var params string
	switch d.Plist {
	case "void":
		params = "void"
	case "empty":
		params = ""
	default:
		parts := make([]string, 0, len(d.Params))
		for _, p := range d.Params {
			parts = append(parts, renderParam(p))
		}
		params = strings.Join(parts, ", ")
	}
	return ret + sep + d.Name + "(" + params + ")"
}

// renderParam writes one parameter.
func renderParam(p param) string {
	switch p.Form {
	case "ellipsis":
		return "..."
	case "fnptr":
		return "int (*cb)(int)"
	case "empty":
		return ""
	case "array":
		name := p.Name
		if name == "" {
			name = "buf"
		}
		return renderCTy(p.Ty) + " " + name + "[8]"
	}
	text := renderCTy(p.Ty)
	switch {
	case p.Name == "":
		return text
	case strings.HasSuffix(text, "*"):
		return text + p.Name // `char *s`
	default:
		return text + " " + p.Name
	}
}

// renderCTy writes a C type: `const char *const *`.
func renderCTy(t cty) string {
	quals := func(c, v, r bool) []string {
		var out []string
		if c {
			out = append(out, "const")
		}
		if v {
			out = append(out, "volatile")
		}
		if r {
			out = append(out, "restrict")
		}
		return out
	}
	words := append([]string{}, t.Base...)
	if t.After {
		words = append(words, quals(t.BC, t.BV, false)...)
	} else {
		words = append(quals(t.BC, t.BV, false), words...)
	}
	text := strings.Join(words, " ")
	for _, s := range t.Stars {
		text += " *"
		if qs := quals(s.C, s.V, s.R); len(qs) > 0 {
			text += strings.Join(qs, " ")
		}
	}
	return text
}

// renderKTy writes a Kizu type: consts are innermost first.
func renderKTy(t kty) string {
	if t.Base == "slice" {
		return "[]u8"
	}
	out := t.Base
	for _, c := range t.Consts {
		if c {
			out = "ptr<const " + out + ">"
		} else {
			out = "ptr<" + out + ">"
		}
	}
	if t.Nullable {
		out = "?" + out
	}
	return out
}

// renderKDecl writes one declaration the way the importer does.
func renderKDecl(d kdecl) string {
	params := make([]string, 0, len(d.Params))
	for _, p := range d.Params {
		params = append(params, p.Name+": "+renderKTy(p.Ty))
	}
	return fmt.Sprintf("extern \"c\" fn %s(%s) -> %s",
		d.Name, strings.Join(params, ", "), renderKTy(d.Ret))
}
