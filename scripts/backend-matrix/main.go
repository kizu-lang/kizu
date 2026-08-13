// Command backend-matrix reports which backends accept each conformance
// example, grouped by language feature, as the Markdown table in README.md.
//
// Run it from the repository root:
//
//	go run ./scripts/backend-matrix
//
// The interpreter column is the oracle: every listed example is a passing
// conformance case, so a failure there means the manifest and the tree
// disagree. The backend columns are the ones that carry information.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// featureGroup names one README row and the manifest tags it collects.
type featureGroup struct {
	name string
	tags []string
}

// groups define the README rows in source order.
var groups = []featureGroup{
	{"fn / let / struct / literals", []string{
		"fn", "let", "var", "print", "i64", "bool", "void", "call", "return", "app",
		"assignment", "field", "field-access", "struct", "struct-literal",
		"string-literal", "multiline-string", "contextual-integer-literal",
		"copy", "block-exit", "edge-case", "error", "diagnostic", "diagnostics",
		"file-span", "related-span", "trap"}},
	{"arithmetic / comparison / logical", []string{
		"arithmetic", "comparison", "logical", "short-circuit"}},
	{"while / break / continue / for / label", []string{
		"while", "break", "continue", "for", "loop", "label"}},
	{"if / match", []string{
		"if", "if-expression", "match", "match-expression", "wildcard",
		"control-flow", "expression"}},
	{"enum / union", []string{"enum", "union"}},
	{"error union `!T` / try / errdefer", []string{
		"error-union", "error-set", "try", "typed-error", "errdefer"}},
	{"move / borrow", []string{
		"move", "ownership", "borrow", "mutable-borrow", "borrow-provenance",
		"field-borrow", "last-use", "escaping", "mutation"}},
	{"deinit / defer", []string{"deinit", "defer", "cleanup", "resource-element"}},
	{"arena / handle", []string{"arena", "handle"}},
	{"comptime", []string{"comptime"}},
	{"cast / slice / raw pointer / box", []string{
		"cast", "deref", "slice", "slice-syntax", "index-slice", "[]u8", "box",
		"local-buffer", "capacity", "recursive-ast"}},
	{"contract / dyn / generics", []string{
		"contract", "dyn", "impl", "generics", "type-apply", "static-arguments"}},
	{"std::array", []string{"std-array", "token-list"}},
	{"std::string", []string{"std-string"}},
	{"std::map", []string{"std-map", "symbol-table", "resolver"}},
	{"std::mem / allocator", []string{"std-mem", "allocator"}},
	{"std::testing", []string{"std-testing"}},
	{"std::fmt", []string{"std-fmt", "artifact"}},
	{"std::fs / path / io / process", []string{
		"std-fs", "std-path", "std-io", "std-process", "fs", "io",
		"explicit-io", "blocking", "failing", "read-dir", "pure-helper"}},
	{"TaskGroup / channel / queue / parallel", []string{
		"taskgroup", "task", "queue", "deferred-task", "channel",
		"owned-message", "parallel-for", "parallel-map", "partition", "cancel"}},
	{"thread / atomic / mutex", []string{
		"thread", "threaded", "atomic", "seq-cst", "sync"}},
}

// routes are the CLI paths each example is put through. `run` builds a native
// executable and runs it, so it is judged against the manifest stdout rather
// than against the weaker fact that the command exited zero. `wasm` is judged
// the same way, by running what it emitted: a module that exits zero while
// emitting text no runtime can load is not a working backend.
var routes = []string{"check", "run", "llvm", "wasm"}

// manifestCase is the subset of a conformance entry this command reads.
type manifestCase struct {
	Mode     string   `json:"mode"`
	Path     string   `json:"path"`
	Args     []string `json:"args"`
	Stdout   *string  `json:"stdout"`
	Features []string `json:"features"`
}

// result records one example and whether each route accepted it.
type result struct {
	features []string
	ok       map[string]bool
	err      map[string]string
}

// main runs every runnable example through every route and prints the table.
func main() {
	cases, err := loadCases()
	if err != nil {
		fail(err)
	}
	bin, cleanup, err := buildKizu()
	if err != nil {
		fail(err)
	}
	defer cleanup()
	results := runAll(bin, cases)
	warnUnknownTags(results)
	printTable(results)
	printFailures(results)
}

// loadCases returns the runnable single-file examples from every manifest.
func loadCases() (map[string]manifestCase, error) {
	files, err := filepath.Glob("tests/conformance/v0_*.json")
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no conformance manifest found; run from the repository root")
	}
	cases := map[string]manifestCase{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Cases []manifestCase `json:"cases"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		for _, entry := range manifest.Cases {
			if entry.Mode == "run" && strings.HasSuffix(entry.Path, ".kizu") {
				cases[entry.Path] = entry
			}
		}
	}
	return cases, nil
}

// buildKizu compiles the CLI once so each route call is a plain exec.
func buildKizu() (string, func(), error) {
	dir, err := os.MkdirTemp("", "backend-matrix")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	bin := filepath.Join(dir, "kizu")
	build := exec.Command("go", "build", "-o", bin, "./cmd/kizu")
	if out, err := build.CombinedOutput(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("go build ./cmd/kizu: %v\n%s", err, out)
	}
	return bin, cleanup, nil
}

// routeArgs returns the CLI arguments for one route and example.
func routeArgs(route string, entry manifestCase) []string {
	switch route {
	case "check":
		return []string{"check", entry.Path}
	case "run":
		return append([]string{"run", entry.Path}, entry.Args...)
	case "llvm":
		return []string{"build", "--emit-llvm", entry.Path}
	default:
		return []string{"build", "--target", "wasm32-wasi", entry.Path}
	}
}

// runAll puts every example through every route, bounded by CPU count.
func runAll(bin string, cases map[string]manifestCase) map[string]*result {
	results := map[string]*result{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, runtime.NumCPU())
	for path, entry := range cases {
		wg.Add(1)
		go func(path string, entry manifestCase) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			res := runRoutes(bin, entry)
			mu.Lock()
			results[path] = res
			mu.Unlock()
		}(path, entry)
	}
	wg.Wait()
	return results
}

// runRoutes runs one example through every route.
func runRoutes(bin string, entry manifestCase) *result {
	res := &result{features: entry.Features, ok: map[string]bool{}, err: map[string]string{}}
	for _, route := range routes {
		cmd := exec.Command(bin, routeArgs(route, entry)...)
		cmd.Env = append(os.Environ(), "KIZU_TEST_ENV=env-ok")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		res.ok[route] = err == nil
		if err != nil {
			res.err[route] = firstLine(stderr.String() + stdout.String())
			continue
		}
		got := stdout.String()
		if route == "wasm" {
			got, err = runWat(stdout.Bytes(), entry.Args)
			if err != nil {
				res.ok[route] = false
				res.err[route] = firstLine(err.Error())
				continue
			}
		}
		if entry.Stdout == nil || (route != "run" && route != "wasm") {
			continue
		}
		if got != *entry.Stdout {
			res.ok[route] = false
			res.err[route] = fmt.Sprintf("output mismatch: want %q, got %q",
				truncate(*entry.Stdout), truncate(got))
		}
	}
	return res
}

// runWat loads emitted WebAssembly text with wasmtime and returns what it
// printed, so the wasm column reports whether a module runs rather than whether
// the emitter exited zero.
func runWat(wat []byte, args []string) (string, error) {
	file, err := os.CreateTemp("", "kizu-matrix-*.wat")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(wat); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	cmd := exec.Command("wasmtime", append([]string{file.Name()}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("wasmtime: %s", firstLine(stderr.String()+err.Error()))
	}
	return stdout.String(), nil
}

// truncate keeps a mismatch report short enough to read.
func truncate(text string) string {
	if len(text) > 60 {
		return text[:60] + "..."
	}
	return text
}

// firstLine trims command output to its first line for reporting.
func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return "(no output)"
}

// tally counts how many examples in one group each route accepted.
func tally(results map[string]*result, group featureGroup) (int, map[string]int) {
	want := map[string]bool{}
	for _, tag := range group.tags {
		want[tag] = true
	}
	total := 0
	ok := map[string]int{}
	for _, res := range results {
		if !res.matches(want) {
			continue
		}
		total++
		for _, route := range routes {
			if res.ok[route] {
				ok[route]++
			}
		}
	}
	return total, ok
}

// matches reports whether an example carries any tag of a group.
func (r *result) matches(want map[string]bool) bool {
	for _, tag := range r.features {
		if want[tag] {
			return true
		}
	}
	return false
}

// cell renders one table cell.
func cell(ok int, total int) string {
	switch {
	case total == 0:
		return "n/a"
	case ok == total:
		return "✅"
	case ok == 0:
		return "❌"
	default:
		return fmt.Sprintf("%d/%d", ok, total)
	}
}

// printTable writes the Markdown table README.md embeds.
func printTable(results map[string]*result) {
	header := "| Feature | Examples"
	divider := "| --- | ---:"
	for _, route := range routes {
		header += " | " + route
		divider += " | :--:"
	}
	fmt.Println(header + " |")
	fmt.Println(divider + " |")
	for _, group := range groups {
		total, ok := tally(results, group)
		cells := make([]string, 0, len(routes))
		for _, route := range routes {
			cells = append(cells, cell(ok[route], total))
		}
		fmt.Printf("| %s | %d | %s |\n", group.name, total, strings.Join(cells, " | "))
	}
	fmt.Printf("\n%d runnable examples.\n", len(results))
	for _, route := range routes {
		count := 0
		for _, res := range results {
			if res.ok[route] {
				count++
			}
		}
		fmt.Printf("- %s: %d/%d\n", route, count, len(results))
	}
}

// printFailures lists the distinct reasons each backend rejected an example.
func printFailures(results map[string]*result) {
	for _, route := range routes {
		reasons := map[string]int{}
		for _, res := range results {
			if msg := res.err[route]; msg != "" {
				reasons[msg]++
			}
		}
		if len(reasons) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "\n%s rejected:\n", route)
		for _, msg := range sortedByCount(reasons) {
			fmt.Fprintf(os.Stderr, "  %2d  %s\n", reasons[msg], msg)
		}
	}
}

// sortedByCount orders reasons by frequency, then text.
func sortedByCount(reasons map[string]int) []string {
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] != reasons[keys[j]] {
			return reasons[keys[i]] > reasons[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// warnUnknownTags reports manifest tags no group collects, so the table
// cannot silently drop a feature that examples already cover.
func warnUnknownTags(results map[string]*result) {
	known := map[string]bool{}
	for _, group := range groups {
		for _, tag := range group.tags {
			known[tag] = true
		}
	}
	missing := map[string]bool{}
	for _, res := range results {
		for _, tag := range res.features {
			if !known[tag] {
				missing[tag] = true
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	keys := make([]string, 0, len(missing))
	for tag := range missing {
		keys = append(keys, tag)
	}
	sort.Strings(keys)
	fmt.Fprintf(os.Stderr, "warning: %d manifest tags have no group: %s\n",
		len(keys), strings.Join(keys, ", "))
}

// fail reports a fatal error and exits.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "backend-matrix:", err)
	os.Exit(1)
}
