// Command unsafecap-mbt samples programs from the Quint model of the `unsafe`
// marker check and writes them, with the diagnostics the model predicts, as
// the fixture internal/unsafecap/mbt_test.go replays against the checker.
//
// Run it from the repository root with quint on PATH:
//
//	just spec-unsafecap
//
// Random runs mostly produce short programs with common errors, so the samples
// are stratified: each program falls into a bucket named by what the model
// predicts and which constructs it uses, and each bucket keeps at most -per
// programs. The statement syntax of the fixture is described in mbt_test.go.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// main reports a failure on stderr and in the exit status.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "unsafecap-mbt:", err)
		os.Exit(1)
	}
}

// Func is one generated function: `fn` or `unsafe fn`, in the file that
// declares the unsafe struct ("home") or another file of the module ("away").
type Func struct {
	Kind string   `json:"kind"`
	File string   `json:"file"`
	Body []string `json:"body"`
}

// Case is one program of the fixture and the diagnostic classes the model
// allows. An empty Expect means the checker must accept.
type Case struct {
	Bucket string   `json:"bucket"`
	G      Func     `json:"g"`
	F      Func     `json:"f"`
	Expect []string `json:"expect"`
}

// Fixture is the file mbt_test.go reads.
type Fixture struct {
	Note  string `json:"note"`
	Cases []Case `json:"cases"`
}

// sampler collects the distinct finished programs of several quint runs.
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
	model := flag.String("model", "spec/unsafecap/unsafecap.qnt", "the Quint model")
	mainModule := flag.String("main", "spec", "the module of the model to run")
	out := flag.String("out", "internal/unsafecap/testdata/unsafecap_mbt.json", "the fixture to write")
	samples := flag.Int("samples", 1000, "runs per seed and mode")
	seeds := flag.Int("seeds", 3, "seeds per mode")
	per := flag.Int("per", 2, "programs kept per bucket")
	invariant := flag.String("invariant", "soundness",
		"the invariant each run checks; \"\" for none, to sample a mutant model")
	flag.Parse()

	dir, err := os.MkdirTemp("", "unsafecap-mbt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	s := &sampler{model: *model, main: *mainModule, invariant: *invariant, dir: dir, samples: *samples,
		seen: map[string]bool{}, buckets: map[string][]Case{}}
	for _, init := range []string{"initGuided", "initOneMistake", "initFree"} {
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
// finished program in its bucket. The run checks the soundness invariant too:
// a model that breaks it has no business predicting the checker, except a
// mutant sampled on purpose to see the checker disagree with it.
func (s *sampler) sample(init string, seed int) error {
	traces := filepath.Join(s.dir, fmt.Sprintf("%s_%d", init, seed))
	if err := os.MkdirAll(traces, 0o755); err != nil {
		return err
	}
	args := []string{"run", s.model, "--main", s.main,
		"--init", init, "--backend", "rust", "--seed", fmt.Sprintf("0x%x", seed),
		"--max-samples", fmt.Sprint(s.samples), "--n-traces", fmt.Sprint(s.samples),
		"--max-steps", "12", "--out-itf", filepath.Join(traces, "t_{seq}.itf.json")}
	if s.invariant != "" {
		args = append(args, "--invariant", s.invariant)
	}
	cmd := exec.Command("quint", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
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
	return strings.Join([]string{
		c.G.Kind, c.G.File, strings.Join(c.G.Body, ";"),
		c.F.Kind, c.F.File, strings.Join(c.F.Body, ";"),
	}, "|")
}

// fixture keeps at most per programs of each bucket.
func (s *sampler) fixture(per int) Fixture {
	fixture := Fixture{
		Note: "generated by `just spec-unsafecap` from spec/unsafecap/unsafecap.qnt; do not edit",
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

// itfValue is a Quint value in the Informal Trace Format.
type itfValue struct {
	Tag    string            `json:"tag"`
	Value  json.RawMessage   `json:"value"`
	BigInt string            `json:"#bigint"`
	Set    []json.RawMessage `json:"#set"`
}

// stateVar finds a state variable by its name, whatever module prefix the
// instance gave it.
func stateVar(state map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	for key, value := range state {
		if key == name || strings.HasSuffix(key, "::"+name) {
			return value, true
		}
	}
	return nil, false
}

// readCase reads the last state of one trace. A run that ended before the
// program was finished reports false.
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
	rawDone, _ := stateVar(last, "done")
	var done bool
	if err := json.Unmarshal(rawDone, &done); err != nil || !done {
		return Case{}, false, err
	}
	rawProg, _ := stateVar(last, "prog")
	var prog struct {
		G itfFunc `json:"g"`
		F itfFunc `json:"f"`
	}
	if err := json.Unmarshal(rawProg, &prog); err != nil {
		return Case{}, false, err
	}
	c := Case{Expect: []string{}}
	if c.G, err = prog.G.decode(); err != nil {
		return Case{}, false, err
	}
	if c.F, err = prog.F.decode(); err != nil {
		return Case{}, false, err
	}
	rawExpected, _ := stateVar(last, "expected")
	var expected itfValue
	if err := json.Unmarshal(rawExpected, &expected); err != nil {
		return Case{}, false, err
	}
	for _, raw := range expected.Set {
		kind, err := kindName(raw)
		if err != nil {
			return Case{}, false, err
		}
		c.Expect = append(c.Expect, kind)
	}
	sort.Strings(c.Expect)
	c.Bucket = bucket(c)
	return c, true, nil
}

// kindName writes a diagnostic class as the fixture spells it: the tag, and
// for Missing the operation kind, as internal/unsafecap names it.
func kindName(raw json.RawMessage) (string, error) {
	var kind itfValue
	if err := json.Unmarshal(raw, &kind); err != nil {
		return "", err
	}
	if kind.Tag != "Missing" {
		return kind.Tag, nil
	}
	var capability itfValue
	if err := json.Unmarshal(kind.Value, &capability); err != nil {
		return "", err
	}
	name, ok := capNames[capability.Tag]
	if !ok {
		return "", fmt.Errorf("unknown operation kind %q", capability.Tag)
	}
	return "Missing:" + name, nil
}

// capNames maps the Cap of the model to the names of internal/unsafecap.
var capNames = map[string]string{
	"PtrRead": "ptr_read", "PtrWrite": "ptr_write", "PtrDeref": "ptr_deref",
	"PtrCast": "ptr_cast", "PtrIntCast": "ptr_int_cast", "PtrOffset": "ptr_offset",
	"PtrView": "ptr_view", "ExternCall": "extern_call", "UnsafeCall": "unsafe_call",
	"StructInvariant": "struct_invariant", "Volatile": "volatile",
}

type itfFunc struct {
	Kind itfValue  `json:"kind"`
	File itfValue  `json:"file"`
	Body []itfStmt `json:"body"`
}

type itfLeaf struct {
	M itfValue `json:"m"`
	A itfValue `json:"a"`
}

type itfExpr struct {
	M       itfValue `json:"m"`
	Shape   itfValue `json:"shape"`
	L       itfLeaf  `json:"l"`
	R       itfLeaf  `json:"r"`
	LSafety bool     `json:"lSafety"`
	RSafety bool     `json:"rSafety"`
}

type itfStmt struct {
	Form   itfValue `json:"form"`
	M      itfValue `json:"m"`
	E      itfExpr  `json:"e"`
	Ty     itfValue `json:"ty"`
	Name   itfValue `json:"name"`
	Void   itfValue `json:"void"`
	Safety bool     `json:"safety"`
}

// decode writes a function of the trace in the fixture syntax.
func (f itfFunc) decode() (Func, error) {
	out := Func{Kind: map[string]string{"SafeFn": "fn", "UnsafeFn": "unsafe"}[f.Kind.Tag],
		File: strings.ToLower(f.File.Tag), Body: []string{}}
	for _, stmt := range f.Body {
		text, err := stmt.token()
		if err != nil {
			return Func{}, err
		}
		out.Body = append(out.Body, text)
	}
	return out, nil
}

// atomTokens names each operand of the model in the fixture.
var atomTokens = map[string]string{
	"Lit": "lit", "SafeCall": "sf", "SafeFnPtr": "fs", "FieldRead": "field",
	"Read": "read", "Deref": "deref", "VolRead": "vread", "Offset": "offset",
	"Cast": "cast", "IntCast": "intcast", "ExtCall": "ext", "UfCall": "uf",
	"UnsafeFnPtr": "fu", "ExternFnPtr": "fe", "Build": "build", "CallG": "g",
}

// formTokens names each statement form of the model in the fixture.
var formTokens = map[string]string{
	"Print": "print", "PtrWriteS": "ptr_write", "VolWriteS": "vol_write",
	"DerefSet": "deref_set", "FieldSet": "field_set", "ViewS": "view",
	"DerefBorrow": "deref_borrow", "DeferS": "defer", "Bind": "bind",
	"FieldBorrow": "field_borrow",
}

// marks writes n markers as "!" each, or "-" for none where a word is needed.
func marks(n string, word bool) string {
	count := 0
	fmt.Sscan(n, &count)
	if count == 0 && word {
		return "-"
	}
	return strings.Repeat("!", count)
}

// token writes an operand with its markers.
func (l itfLeaf) token() (string, error) {
	atom, ok := atomTokens[l.A.Tag]
	if !ok {
		return "", fmt.Errorf("unknown operand %q", l.A.Tag)
	}
	return marks(l.M.BigInt, false) + atom, nil
}

// token writes an expression: a lone operand, a sum or a block.
func (e itfExpr) token() (string, error) {
	left, err := e.L.token()
	if err != nil {
		return "", err
	}
	right, err := e.R.token()
	if err != nil {
		return "", err
	}
	safety := func(on bool) string {
		if on {
			return "$"
		}
		return ""
	}
	switch e.Shape.Tag {
	case "Single":
		return left, nil
	case "Sum":
		return marks(e.M.BigInt, false) + "(" + left + "+" + right + ")", nil
	case "Block":
		return marks(e.M.BigInt, false) + "{" + safety(e.LSafety) + left + ";" +
			safety(e.RSafety) + right + "}", nil
	}
	return "", fmt.Errorf("unknown shape %q", e.Shape.Tag)
}

// token writes one statement in the fixture syntax (mbt_test.go).
func (s itfStmt) token() (string, error) {
	form, ok := formTokens[s.Form.Tag]
	if !ok {
		return "", fmt.Errorf("unknown statement form %q", s.Form.Tag)
	}
	parts := []string{form}
	switch form {
	case "print":
		e, err := s.E.token()
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	case "ptr_write", "vol_write", "deref_set", "field_set":
		e, err := s.E.token()
		if err != nil {
			return "", err
		}
		parts = append(parts, marks(s.M.BigInt, true), e)
	case "view", "deref_borrow", "field_borrow":
		parts = append(parts, marks(s.M.BigInt, true))
	case "defer":
		parts = append(parts, marks(s.M.BigInt, true), strings.ToLower(s.Void.Tag))
	case "bind":
		ty := map[string]string{"SafeTy": "safe", "UnsafeTy": "unsafe", "ExternTy": "extern"}[s.Ty.Tag]
		parts = append(parts, ty, strings.ToLower(s.Name.Tag), marks(s.M.BigInt, true))
	}
	text := strings.Join(parts, " ")
	if s.Safety {
		text = "$ " + text
	}
	return text, nil
}

// callsG finds the operand `g` (a call of g) in a statement.
var callsG = regexp.MustCompile(`(^|[ !(+;{$])g([)+;}]|$)`)

// bucket names the stratum of a case: the predicted classes and the rarest
// construct it uses. A prediction of one class keeps the
// operation kind; with several, a missing marker is one class whatever its kind,
// so the buckets stay few enough to replay in a few seconds.
func bucket(c Case) string {
	verdict := bucketVerdict(c.Expect)
	has := bucketConstructs(c)
	// The rarest construct a program uses names its stratum; a subset of
	// constructs per bucket would make more buckets than are worth replaying.
	feature := "plain"
	for _, name := range []string{"calls-g", "fnptr", "block", "away", "unsafe-fn", "covering-op", "sum"} {
		if has[name] {
			feature = name
			break
		}
	}
	return verdict + "/" + feature
}

// bucketVerdict is the predicted part of a bucket: the one predicted kind, or
// the sorted classes when there are several.
func bucketVerdict(expect []string) string {
	if len(expect) == 0 {
		return "accept"
	}
	if len(expect) == 1 {
		return expect[0]
	}
	classes := map[string]bool{}
	for _, kind := range expect {
		name, _, _ := strings.Cut(kind, ":")
		classes[name] = true
	}
	var names []string
	for name := range classes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

// bucketConstructs is the set of constructs a case uses.
func bucketConstructs(c Case) map[string]bool {
	has := map[string]bool{}
	for _, fn := range []Func{c.G, c.F} {
		if fn.Kind == "unsafe" && len(fn.Body) > 0 {
			has["unsafe-fn"] = true
		}
		if fn.File == "away" && len(fn.Body) > 0 {
			has["away"] = true
		}
		for _, stmt := range fn.Body {
			if strings.Contains(stmt, "{") {
				has["block"] = true
			}
			if strings.Contains(stmt, "(") {
				has["sum"] = true
			}
			form := strings.Fields(strings.TrimPrefix(stmt, "$ "))[0]
			switch form {
			case "ptr_write", "vol_write":
				has["covering-op"] = true
			case "bind":
				has["fnptr"] = true
			}
		}
	}
	for _, stmt := range c.F.Body {
		if callsG.MatchString(stmt) {
			has["calls-g"] = true
		}
	}
	return has
}
