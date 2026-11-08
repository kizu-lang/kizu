// Command ir-mbt samples programs from the Quint models of internal/ir in
// spec/ir and writes them, with what each model predicts, as the fixtures the
// replay tests in internal/ir read. -model all (the default) writes every
// fixture.
//
// Run it from the repository root with quint on PATH:
//
//	just spec-ir
//
// Random runs mostly produce the common shapes, so the samples are
// stratified: each program falls into a bucket named by what the model
// predicts and which constructs it uses, and each bucket keeps at most -per
// programs. A bucket a rare branch fills stays as large as one thousands of
// runs fill.
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
	"strconv"
	"strings"
)

// main reports a failure on stderr and in the exit status.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ir-mbt:", err)
		os.Exit(1)
	}
}

// model names one Quint model, the runs that sample it, and how a finished
// trace becomes a fixture case.
type model struct {
	file  string
	out   string
	inits []string
	read  func(last map[string]json.RawMessage) (Case, error)
	// per is how many programs each bucket keeps. Every bounds program runs
	// twenty times and most stop on some input, which costs a fresh wasm
	// instance each, so its buckets keep one.
	per int
}

// Case is one program of a fixture. Prog is the statement tokens the replay
// test renders; Decls and Types are the declarations around it, in the
// model's terms; Expect is what the model predicts, which each replay test
// reads in its own way.
type Case struct {
	Bucket string   `json:"bucket"`
	Types  []string `json:"types,omitempty"`
	Decls  []string `json:"decls,omitempty"`
	Prog   []string `json:"prog"`
	Expect []string `json:"expect"`
}

// Fixture is the file a replay test reads.
type Fixture struct {
	Note  string `json:"note"`
	Cases []Case `json:"cases"`
}

var models = map[string]model{
	"slots": {
		file:  "spec/ir/slots.qnt",
		out:   "internal/ir/testdata/slots_mbt.json",
		inits: []string{"init"},
		read:  readSlotsCase,
		per:   2,
	},
	"bounds": {
		file:  "spec/ir/bounds.qnt",
		out:   "internal/ir/testdata/bounds_mbt.json",
		inits: []string{"init"},
		read:  readBoundsCase,
		per:   1,
	},
	"defer": {
		file:  "spec/ir/defer.qnt",
		out:   "internal/ir/testdata/defer_mbt.json",
		inits: []string{"init"},
		read:  readDeferCase,
		per:   2,
	},
}

// run samples the chosen models over several seeds and writes their fixtures.
func run() error {
	name := flag.String("model", "all", "which model of spec/ir to sample (slots, bounds, defer, all)")
	samples := flag.Int("samples", 1000, "runs per seed and initial action")
	seeds := flag.Int("seeds", 3, "seeds per initial action")
	steps := flag.Int("steps", 20, "steps per run")
	per := flag.Int("per", 0, "programs kept per bucket (default: the model's)")
	out := flag.String("out", "", "the fixture to write (default: the model's)")
	flag.Parse()

	names := []string{*name}
	if *name == "all" {
		if *out != "" {
			return fmt.Errorf("-out needs one -model")
		}
		names = []string{"slots", "bounds", "defer"}
	}
	for _, name := range names {
		m, ok := models[name]
		if !ok {
			return fmt.Errorf("unknown model %q", name)
		}
		path := *out
		if path == "" {
			path = m.out
		}
		keep := *per
		if keep == 0 {
			keep = m.per
		}
		if err := sample(name, m, path, *samples, *seeds, *steps, keep); err != nil {
			return err
		}
	}
	return nil
}

// sample runs one model and writes its fixture to out.
func sample(name string, m model, out string, samples, seeds, steps, per int) error {
	dir, err := os.MkdirTemp("", "ir-mbt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	seen := map[string]bool{}
	buckets := map[string][]Case{}
	for _, init := range m.inits {
		for seed := 1; seed <= seeds; seed++ {
			traces := filepath.Join(dir, fmt.Sprintf("%s_%d", init, seed))
			if err := os.MkdirAll(traces, 0o755); err != nil {
				return err
			}
			// The run checks the model's soundness invariant too: a model
			// that breaks it has no business predicting the lowerer.
			cmd := exec.Command("quint", "run", m.file,
				"--init", init, "--backend", "rust", "--seed", fmt.Sprintf("0x%x", seed),
				"--max-samples", fmt.Sprint(samples), "--n-traces", fmt.Sprint(samples),
				"--max-steps", fmt.Sprint(steps), "--invariant", "soundness",
				"--out-itf", filepath.Join(traces, "t_{seq}.itf.json"))
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("quint run %s --init %s --seed %d: %v\n%s",
					m.file, init, seed, err, output)
			}
			files, err := filepath.Glob(filepath.Join(traces, "*.itf.json"))
			if err != nil {
				return err
			}
			for _, file := range files {
				last, ok, err := lastState(file)
				if err != nil {
					return fmt.Errorf("%s: %w", file, err)
				}
				if !ok {
					continue
				}
				c, err := m.read(last)
				if err != nil {
					return fmt.Errorf("%s: %w", file, err)
				}
				key := strings.Join(c.Types, ",") + "|" + strings.Join(c.Decls, ",") + "|" +
					strings.Join(c.Prog, ";")
				if seen[key] {
					continue
				}
				seen[key] = true
				buckets[c.Bucket] = append(buckets[c.Bucket], c)
			}
		}
	}
	fixture := stratify(buckets, per,
		fmt.Sprintf("generated by `just spec-ir` from %s; do not edit", m.file))
	text, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(text, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d distinct programs, %d buckets, %d kept\n",
		name, len(seen), len(buckets), len(fixture.Cases))
	return nil
}

// stratify keeps at most per cases of each bucket.
func stratify(buckets map[string][]Case, per int, note string) Fixture {
	fixture := Fixture{Note: note}
	names := make([]string, 0, len(buckets))
	for name := range buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cases := buckets[name]
		// A hash of the case, not the order quint found it in, picks the
		// kept ones, so a bucket keeps long programs as well as short ones.
		sort.Slice(cases, func(i, j int) bool { return hashCase(cases[i]) < hashCase(cases[j]) })
		if len(cases) > per {
			cases = cases[:per]
		}
		fixture.Cases = append(fixture.Cases, cases...)
	}
	return fixture
}

// hashCase orders cases without favoring short ones.
func hashCase(c Case) uint64 {
	h := fnv.New64a()
	for _, part := range [][]string{c.Types, c.Decls, c.Prog} {
		for _, s := range part {
			_, _ = h.Write([]byte(s))
			_, _ = h.Write([]byte{';'})
		}
		_, _ = h.Write([]byte{'|'})
	}
	return h.Sum64()
}

// itfValue is a Quint value in the Informal Trace Format.
type itfValue struct {
	Tag    string               `json:"tag"`
	Value  json.RawMessage      `json:"value"`
	BigInt string               `json:"#bigint"`
	Set    []json.RawMessage    `json:"#set"`
	Map    [][2]json.RawMessage `json:"#map"`
	Tup    []json.RawMessage    `json:"#tup"`
}

// lastState reads the last state of one trace. A run that ended before the
// program was finished reports false.
func lastState(file string) (map[string]json.RawMessage, bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false, err
	}
	var trace struct {
		States []map[string]json.RawMessage `json:"states"`
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, false, err
	}
	if len(trace.States) == 0 {
		return nil, false, nil
	}
	last := trace.States[len(trace.States)-1]
	var done bool
	if err := json.Unmarshal(last["done"], &done); err != nil || !done {
		return nil, false, err
	}
	return last, true, nil
}

// decode reads one ITF value.
func decode(raw json.RawMessage) (itfValue, error) {
	var v itfValue
	err := json.Unmarshal(raw, &v)
	return v, err
}

// integer reads an ITF integer.
func integer(raw json.RawMessage) (string, error) {
	v, err := decode(raw)
	if err != nil {
		return "", err
	}
	if v.BigInt == "" {
		return "", fmt.Errorf("not an integer: %s", raw)
	}
	return v.BigInt, nil
}

// fields reads an ITF record whose fields are all integers.
func fields(raw json.RawMessage) (map[string]string, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, value := range record {
		n, err := integer(value)
		if err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

// ---------------------------------------------------------------- slots

var slotTypes = []string{"A", "B", "C"}
var slotMethods = []string{"poke", "note"}

// readSlotsCase turns the last state of a spec/ir/slots.qnt trace into a
// case: the type of each struct local, each declared method as
// `<Type>.<method> <recv> <arg>`, the statements, and the locals the model
// gives storage.
func readSlotsCase(last map[string]json.RawMessage) (Case, error) {
	c := Case{Types: []string{}, Decls: []string{}, Prog: []string{}, Expect: []string{}}
	var types []json.RawMessage
	if err := json.Unmarshal(last["xtype"], &types); err != nil {
		return c, err
	}
	for _, raw := range types {
		n, err := integer(raw)
		if err != nil {
			return c, err
		}
		c.Types = append(c.Types, slotTypes[atoi(n)])
	}
	decls, err := decode(last["decls"])
	if err != nil {
		return c, err
	}
	for _, entry := range decls.Map {
		key, err := decode(entry[0])
		if err != nil {
			return c, err
		}
		t, err := integer(key.Tup[0])
		if err != nil {
			return c, err
		}
		m, err := integer(key.Tup[1])
		if err != nil {
			return c, err
		}
		var d struct {
			Declared bool     `json:"declared"`
			Recv     itfValue `json:"recv"`
			Arg      itfValue `json:"arg"`
		}
		if err := json.Unmarshal(entry[1], &d); err != nil {
			return c, err
		}
		if d.Declared {
			c.Decls = append(c.Decls, fmt.Sprintf("%s.%s %s %s",
				slotTypes[atoi(t)], slotMethods[atoi(m)], d.Recv.Tag, d.Arg.Tag))
		}
	}
	sort.Strings(c.Decls)
	var stmts []itfValue
	if err := json.Unmarshal(last["prog"], &stmts); err != nil {
		return c, err
	}
	for _, stmt := range stmts {
		token, err := slotToken(stmt)
		if err != nil {
			return c, err
		}
		c.Prog = append(c.Prog, token)
	}
	expected, err := decode(last["expected"])
	if err != nil {
		return c, err
	}
	for _, raw := range expected.Set {
		local, err := decode(raw)
		if err != nil {
			return c, err
		}
		n, err := integer(local.Value)
		if err != nil {
			return c, err
		}
		c.Expect = append(c.Expect, strings.ToLower(local.Tag)+n)
	}
	sort.Strings(c.Expect)
	var differs bool
	if err := json.Unmarshal(last["nameDiffers"], &differs); err != nil {
		return c, err
	}
	c.Bucket = slotsBucket(c, differs)
	return c, nil
}

// slotToken writes one statement of the slot model as its token.
func slotToken(stmt itfValue) (string, error) {
	switch stmt.Tag {
	case "Call", "DeferCall":
		site, err := fields(stmt.Value)
		if err != nil {
			return "", err
		}
		token := "call"
		if stmt.Tag == "DeferCall" {
			token = "defer"
		}
		return fmt.Sprintf("%s %s %s %s", token, site["x"], slotMethods[atoi(site["m"])], site["k"]), nil
	case "Bump", "BumpExplicit", "LoopBegin":
		n, err := integer(stmt.Value)
		if err != nil {
			return "", err
		}
		token := map[string]string{"Bump": "bump", "BumpExplicit": "bump_explicit", "LoopBegin": "loop"}[stmt.Tag]
		return token + " " + n, nil
	case "IfBegin":
		return "if", nil
	case "BlockEnd":
		return "end", nil
	}
	return "", fmt.Errorf("unknown statement %q", stmt.Tag)
}

// slotsBucket names the stratum of a slot case: how many locals get storage,
// whether deciding by name would differ, and the constructs used.
func slotsBucket(c Case, nameDiffers bool) string {
	has := map[string]bool{}
	for _, stmt := range c.Prog {
		has[strings.Fields(stmt)[0]] = true
	}
	receivers, arguments := 0, 0
	for _, local := range c.Expect {
		if strings.HasPrefix(local, "x") {
			receivers++
		} else {
			arguments++
		}
	}
	features := []string{fmt.Sprintf("x%d", receivers), fmt.Sprintf("n%d", arguments)}
	if nameDiffers {
		features = append(features, "name-differs")
	}
	for _, feature := range []string{"defer", "bump_explicit", "if", "loop"} {
		if has[feature] {
			features = append(features, feature)
		}
	}
	return strings.Join(features, ",")
}

// atoi reads an integer the model wrote.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("ir-mbt: not an integer: %q", s))
	}
	return n
}

// ---------------------------------------------------------------- bounds

// readBoundsCase turns the last state of a spec/ir/bounds.qnt trace into a
// case: the statements, and for each input `<len> <k>` what main prints --
// `<result> <length>`, or `panic`.
func readBoundsCase(last map[string]json.RawMessage) (Case, error) {
	c := Case{Prog: []string{}, Expect: []string{}}
	var stmts []itfValue
	if err := json.Unmarshal(last["prog"], &stmts); err != nil {
		return c, err
	}
	for _, stmt := range stmts {
		token, err := boundsToken(stmt)
		if err != nil {
			return c, err
		}
		c.Prog = append(c.Prog, token)
	}
	expected, err := decode(last["expected"])
	if err != nil {
		return c, err
	}
	panics, errors := 0, 0
	for _, entry := range expected.Map {
		key, err := decode(entry[0])
		if err != nil {
			return c, err
		}
		length, err := integer(key.Tup[0])
		if err != nil {
			return c, err
		}
		k, err := integer(key.Tup[1])
		if err != nil {
			return c, err
		}
		var o struct {
			Stopped bool            `json:"stopped"`
			Result  json.RawMessage `json:"result"`
			Len     json.RawMessage `json:"len"`
		}
		if err := json.Unmarshal(entry[1], &o); err != nil {
			return c, err
		}
		printed := "panic"
		if o.Stopped {
			panics++
		} else {
			result, err := integer(o.Result)
			if err != nil {
				return c, err
			}
			n, err := integer(o.Len)
			if err != nil {
				return c, err
			}
			if result == "-7" {
				errors++
			}
			printed = result + " " + n
		}
		c.Expect = append(c.Expect, fmt.Sprintf("%s %s: %s", length, k, printed))
	}
	sort.Strings(c.Expect)
	answered, err := decode(last["answered"])
	if err != nil {
		return c, err
	}
	c.Bucket = boundsBucket(c, len(answered.Set) > 0, panics > 0, errors > 0)
	return c, nil
}

// boundsToken writes one statement of the bounds model as its token.
func boundsToken(stmt itfValue) (string, error) {
	index := func() (string, error) {
		v, err := decode(stmt.Value)
		if err != nil {
			return "", err
		}
		return v.Tag, nil
	}
	simple := map[string]string{
		"Read": "read", "Write": "write", "GuardLt": "lt", "GuardGe": "ge", "ExitUnlessLt": "exit",
	}
	if token, ok := simple[stmt.Tag]; ok {
		x, err := index()
		if err != nil {
			return "", err
		}
		return token + " " + x, nil
	}
	switch stmt.Tag {
	case "Pop", "Push", "End":
		return strings.ToLower(stmt.Tag), nil
	case "ForLen", "WhileLen":
		n, err := integer(stmt.Value)
		if err != nil {
			return "", err
		}
		return map[string]string{"ForLen": "for", "WhileLen": "while"}[stmt.Tag] + " " + n, nil
	}
	return "", fmt.Errorf("unknown statement %q", stmt.Tag)
}

// boundsBucket names the stratum of a bounds case: whether the rule answers
// an access, whether some input stops it or makes `set` fail, and the
// constructs used.
func boundsBucket(c Case, answered, panics, errors bool) string {
	features := []string{}
	for _, f := range []struct {
		on   bool
		name string
	}{{answered, "answered"}, {panics, "panic"}, {errors, "set-error"}} {
		if f.on {
			features = append(features, f.name)
		}
	}
	has := map[string]bool{}
	depth, nested := 0, false
	for _, stmt := range c.Prog {
		head := strings.Fields(stmt)[0]
		has[head] = true
		switch head {
		case "lt", "ge", "for", "while":
			depth++
			nested = nested || depth > 1
		case "end":
			depth--
		}
	}
	for _, feature := range []string{"for", "while", "pop", "push", "lt", "ge", "exit", "write"} {
		if has[feature] {
			features = append(features, feature)
		}
	}
	if nested {
		features = append(features, "nested")
	}
	return strings.Join(features, ",")
}

// ---------------------------------------------------------------- defer

// readDeferCase turns the last state of a spec/ir/defer.qnt trace into a
// case: the statements, and for each input `<x>` the lines main prints.
func readDeferCase(last map[string]json.RawMessage) (Case, error) {
	c := Case{Prog: []string{}, Expect: []string{}}
	var stmts []itfValue
	if err := json.Unmarshal(last["prog"], &stmts); err != nil {
		return c, err
	}
	for _, stmt := range stmts {
		var token string
		switch stmt.Tag {
		case "IfBegin":
			token = "if"
		case "LoopBegin":
			token = "loop"
		default:
			token = strings.ToLower(stmt.Tag)
		}
		if stmt.Tag != "End" {
			n, err := integer(stmt.Value)
			if err != nil {
				return c, err
			}
			token += " " + n
		}
		c.Prog = append(c.Prog, token)
	}
	expected, err := decode(last["expected"])
	if err != nil {
		return c, err
	}
	errors := false
	for _, entry := range expected.Map {
		x, err := integer(entry[0])
		if err != nil {
			return c, err
		}
		var lines []json.RawMessage
		if err := json.Unmarshal(entry[1], &lines); err != nil {
			return c, err
		}
		words := []string{}
		for _, line := range lines {
			n, err := integer(line)
			if err != nil {
				return c, err
			}
			words = append(words, n)
		}
		if len(words) > 0 && words[len(words)-1] == "-1" {
			errors = true
		}
		c.Expect = append(c.Expect, fmt.Sprintf("%02d: %s", atoi(x), strings.Join(words, " ")))
	}
	sort.Strings(c.Expect)
	c.Bucket = deferBucket(c, errors)
	return c, nil
}

// deferBucket names the stratum of a defer case: whether some input takes
// the error return, and the constructs used.
func deferBucket(c Case, errors bool) string {
	features := []string{}
	if errors {
		features = append(features, "error-exit")
	}
	has := map[string]bool{}
	depth, nested := 0, false
	for _, stmt := range c.Prog {
		head := strings.Fields(stmt)[0]
		has[head] = true
		switch head {
		case "if", "loop":
			depth++
			nested = nested || depth > 1
		case "end":
			depth--
		}
	}
	for _, feature := range []string{"defer", "errdefer", "loop", "if", "return", "fail", "break", "continue"} {
		if has[feature] {
			features = append(features, feature)
		}
	}
	if nested {
		features = append(features, "nested")
	}
	return strings.Join(features, ",")
}
