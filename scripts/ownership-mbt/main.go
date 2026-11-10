// Command ownership-mbt samples programs from the Quint model of the ownership
// checker and writes them, with the diagnostics the model predicts, as the
// fixture internal/ownership/mbt_test.go replays against the checker.
//
// Run it from the repository root with quint on PATH:
//
//	just spec-ownership
//
// Random runs mostly produce short programs with common errors, so the samples
// are stratified: each program falls into a bucket named by what the model
// predicts and which constructs it uses, and each bucket keeps at most -per
// programs. A bucket that a rare branch fills stays as large as one that
// thousands of runs fill.
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
		fmt.Fprintln(os.Stderr, "ownership-mbt:", err)
		os.Exit(1)
	}
}

// Case is one program of the fixture: its statements in the token form
// mbt_test.go renders, and the diagnostic classes the model allows. An empty
// Expect means the checker must accept.
type Case struct {
	Bucket string   `json:"bucket"`
	Prog   []string `json:"prog"`
	Expect []string `json:"expect"`
}

// Fixture is the file mbt_test.go reads.
type Fixture struct {
	Note  string `json:"note"`
	Cases []Case `json:"cases"`
}

// sampler collects the distinct finished programs of several quint runs.
type sampler struct {
	model   string
	dir     string
	samples int
	seen    map[string]bool
	buckets map[string][]Case
	// finished and fresh count, per mode, the runs that finished a program
	// and the programs no earlier run had found.
	finished map[string]int
	fresh    map[string]int
}

// modes are the generator's initial actions (README.md says what each gives).
var modes = []string{"initGuided", "initOneMistake", "initFree", "initTargeted"}

// run samples every generator mode over several seeds and writes the fixture.
func run() error {
	model := flag.String("model", "spec/ownership/ownership.qnt", "the Quint model")
	out := flag.String("out", "internal/ownership/testdata/ownership_mbt.json",
		"the fixture to write")
	// quint keeps a run's traces in memory until it writes them: 10,000 ran
	// out of the 4 GB node heap, so a wider sweep takes more seeds.
	samples := flag.Int("samples", 3000, "runs per seed and mode")
	seeds := flag.Int("seeds", 3, "seeds per mode")
	per := flag.Int("per", 1, "programs kept per bucket")
	only := flag.String("modes", strings.Join(modes, ","), "the generator modes to run")
	flag.Parse()
	modes = strings.Split(*only, ",")

	dir, err := os.MkdirTemp("", "ownership-mbt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	s := &sampler{model: *model, dir: dir, samples: *samples,
		seen: map[string]bool{}, buckets: map[string][]Case{},
		finished: map[string]int{}, fresh: map[string]int{}}
	for _, init := range modes {
		for k := 1; k <= *seeds; k++ {
			// Neighboring seeds can fall into the same sequence of runs after a
			// few dozen of them (seen with 0x1 and 0x2 on the rust backend), so
			// the seeds are spread over 32 bits.
			seed := uint32(k) * 0x9e3779b9
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
	s.report(fixture)
	return nil
}

// report prints how many programs each mode found and how they spread over the
// verdicts, so that a change to the generator shows which strata it thinned.
func (s *sampler) report(fixture Fixture) {
	for _, init := range modes {
		fmt.Printf("%-15s %6d finished %6d new\n", init, s.finished[init], s.fresh[init])
	}
	type stratum struct{ programs, buckets, kept int }
	verdicts := map[string]*stratum{}
	at := func(name string) *stratum {
		verdict, _, _ := strings.Cut(name, "/")
		if verdicts[verdict] == nil {
			verdicts[verdict] = &stratum{}
		}
		return verdicts[verdict]
	}
	for name, cases := range s.buckets {
		at(name).programs += len(cases)
		at(name).buckets++
	}
	for _, c := range fixture.Cases {
		at(c.Bucket).kept++
	}
	names := make([]string, 0, len(verdicts))
	for name := range verdicts {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%-50s %8s %8s %6s\n", "verdict", "programs", "buckets", "kept")
	for _, name := range names {
		v := verdicts[name]
		fmt.Printf("%-50s %8d %8d %6d\n", name, v.programs, v.buckets, v.kept)
	}
	fmt.Printf("%d distinct programs, %d buckets, %d kept\n",
		len(s.seen), len(s.buckets), len(fixture.Cases))
}

// sample runs quint once from one initial action and seed and files each new
// finished program in its bucket. The run checks the soundness invariant too:
// a model that breaks it has no business predicting the checker. The traces
// are removed once read: a wide sweep writes gigabytes of them.
func (s *sampler) sample(init string, seed uint32) error {
	traces := filepath.Join(s.dir, fmt.Sprintf("%s_%d", init, seed))
	if err := os.MkdirAll(traces, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(traces)
	cmd := exec.Command("quint", "run", s.model,
		"--init", init, "--backend", "rust", "--seed", fmt.Sprintf("0x%x", seed),
		"--max-samples", fmt.Sprint(s.samples), "--n-traces", fmt.Sprint(s.samples),
		"--max-steps", "25", "--invariant", "soundness",
		"--out-itf", filepath.Join(traces, "t_{seq}.itf.json"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("quint run --init %s --seed 0x%x: %v\n%s", init, seed, err, output)
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
		if !ok {
			continue
		}
		s.finished[init]++
		key := strings.Join(c.Prog, ";")
		if s.seen[key] {
			continue
		}
		s.seen[key] = true
		s.fresh[init]++
		s.buckets[c.Bucket] = append(s.buckets[c.Bucket], c)
	}
	return nil
}

// fixture keeps at most per programs of each bucket.
func (s *sampler) fixture(per int) Fixture {
	fixture := Fixture{
		Note: "generated by `just spec-ownership` from spec/ownership/ownership.qnt; do not edit",
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
			return hashProg(cases[i].Prog) < hashProg(cases[j].Prog)
		})
		if len(cases) > per {
			cases = cases[:per]
		}
		fixture.Cases = append(fixture.Cases, cases...)
	}
	return fixture
}

// hashProg orders programs without favoring short ones.
func hashProg(prog []string) uint64 {
	h := fnv.New64a()
	for _, stmt := range prog {
		_, _ = h.Write([]byte(stmt))
		_, _ = h.Write([]byte{';'})
	}
	return h.Sum64()
}

// itfValue is a Quint value in the Informal Trace Format.
type itfValue struct {
	Tag    string            `json:"tag"`
	Value  json.RawMessage   `json:"value"`
	BigInt string            `json:"#bigint"`
	Set    []json.RawMessage `json:"#set"`
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
	var done bool
	if err := json.Unmarshal(last["done"], &done); err != nil || !done {
		return Case{}, false, err
	}
	var stmts []itfValue
	if err := json.Unmarshal(last["prog"], &stmts); err != nil {
		return Case{}, false, err
	}
	c := Case{Prog: []string{}, Expect: []string{}}
	for _, stmt := range stmts {
		token, err := stmtToken(stmt)
		if err != nil {
			return Case{}, false, err
		}
		c.Prog = append(c.Prog, token)
	}
	var expected itfValue
	if err := json.Unmarshal(last["expected"], &expected); err != nil {
		return Case{}, false, err
	}
	for _, raw := range expected.Set {
		var kind itfValue
		if err := json.Unmarshal(raw, &kind); err != nil {
			return Case{}, false, err
		}
		c.Expect = append(c.Expect, kind.Tag)
	}
	sort.Strings(c.Expect)
	c.Bucket = bucket(c)
	return c, true, nil
}

// tokens names each statement of the model in the fixture.
var tokens = map[string]string{
	"View": "view", "UseView": "use_view", "Borrow": "borrow", "UseBorrow": "use_borrow",
	"ReadOwner": "read", "Mutate": "mutate", "Deinit": "deinit", "MoveOut": "move",
	"DeferDeinit": "defer", "Unrelated": "unrelated",
	"LoopBegin": "loop", "WhileBegin": "while", "LoopEnd": "end",
}

// stmtToken writes one statement as its token and, for those that name a
// binding, the binding's id.
func stmtToken(stmt itfValue) (string, error) {
	token, ok := tokens[stmt.Tag]
	if !ok {
		return "", fmt.Errorf("unknown statement %q", stmt.Tag)
	}
	var arg itfValue
	if err := json.Unmarshal(stmt.Value, &arg); err != nil {
		return "", err
	}
	if arg.BigInt != "" {
		return token + " " + arg.BigInt, nil
	}
	return token, nil
}

// shortest is the length up to which a program is its own stratum: the
// smallest programs (one statement and the closing deinit, or a lone read) are
// the boundary of the generator, and in a bucket with longer ones the hash
// seldom keeps them.
const shortest = 2

// bucket names the stratum of a case: the predicted classes and the constructs
// whose interaction the model is about.
func bucket(c Case) string {
	verdict := "accept"
	if len(c.Expect) > 0 {
		verdict = strings.Join(c.Expect, "+")
	}
	has := map[string]bool{}
	// The loop depth each binding was declared at: a read deeper than that is
	// read again by the next turn of the loops in between.
	declaredAt := map[string]int{}
	depth := 0
	for _, stmt := range c.Prog {
		fields := strings.Fields(stmt)
		switch fields[0] {
		case "loop", "while":
			depth++
			if depth == 2 {
				has["nested"] = true
			}
		case "end":
			depth--
		case "view", "borrow":
			declaredAt[fields[1]] = depth
		case "use_view", "use_borrow":
			if depth > declaredAt[fields[1]] {
				has["carried"] = true
			}
		}
		has[fields[0]] = true
	}
	has["short"] = len(c.Prog) <= shortest
	var features []string
	for _, feature := range []string{
		"loop", "while", "nested", "carried", "defer", "unrelated", "mutate", "short",
	} {
		if has[feature] {
			features = append(features, feature)
		}
	}
	return verdict + "/" + strings.Join(features, ",")
}
