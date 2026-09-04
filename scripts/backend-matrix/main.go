// Command backend-matrix reports which backends accept each conformance
// example, grouped by language feature, as the Markdown table in README.md.
//
// Run it from the repository root:
//
//	go run ./scripts/backend-matrix
//
// Each example's trailing conformance block is the oracle. Every route is
// checked against the command and output declared there, so a failure means a
// backend and the example's promise disagree.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kizu-lang/kizu/internal/conformance"
)

// featureGroup names one README row and the feature tags it collects.
type featureGroup struct {
	name string
	tags []string
}

// groups define the README rows in source order. Every tag an example
// declares belongs to exactly one row; an unassigned tag is reported as a
// warning so a new feature cannot quietly drop out of the table.
var groups = []featureGroup{
	{"fn / let / struct / literals", []string{
		"fn", "let", "var", "print", "i64", "bool", "void", "call", "return", "app",
		"assignment", "field", "field-access", "struct", "struct-literal",
		"string-literal", "multiline-string", "contextual-integer-literal",
		"copy", "block-exit", "edge-case", "error", "diagnostic", "diagnostics",
		"file-span", "related-span", "trap", "method", "receiver", "signature",
		"std", "field-assignment", "shadowing"}},
	{"arithmetic / bitwise / float", []string{
		"arithmetic", "comparison", "logical", "short-circuit", "bitwise",
		"shift", "wrap", "integer-literal", "float", "f64", "f32", "saturate",
		"nan"}},
	{"while / break / continue / for / label", []string{
		"while", "break", "continue", "for", "loop", "label"}},
	{"if / match", []string{
		"if", "if-expression", "match", "match-expression", "wildcard",
		"control-flow", "expression", "branch",
		"arm-block", "arm-empty", "arm-return"}},
	{"enum / union", []string{"enum", "union", "recursive-union"}},
	{"error union `!T` / try / errdefer", []string{
		"error-union", "error-set", "try", "typed-error", "errdefer",
		"absorption"}},
	{"optional `?T` / orelse / capture", []string{
		"optional", "null", "orelse", "capture", "if-capture", "get",
		"accessor"}},
	{"move / borrow", []string{
		"move", "ownership", "borrow", "mutable-borrow", "borrow-provenance",
		"field-borrow", "field-move", "field-path", "last-use", "escaping",
		"mutation", "owner", "owner-element", "owner-safe", "shared-borrow",
		"view", "view-capture", "take", "clone", "in-place"}},
	{"deinit / defer", []string{
		"deinit", "defer", "cleanup", "resource-element", "derived",
		"derived-deinit", "field-cleanup"}},
	{"arena / handle", []string{"arena", "handle"}},
	{"comptime / reflection", []string{
		"comptime", "comptime-for", "comptime-match", "reflection", "std-meta",
		"construct", "structural", "static-params", "field-static-param"}},
	{"cast / slice / stack buffer / box", []string{
		"cast", "deref", "slice", "slice-syntax", "index-slice", "[]u8", "box",
		"capacity", "recursive-ast", "stack-buffer", "mutable-slice-view"}},
	{"unsafe / raw pointer / extern C", []string{
		"unsafe", "unsafe-struct", "raw-pointer", "extern-c",
		"caller-obligation", "linkage"}},
	{"contract / generics", []string{
		"contract", "impl", "generics", "generic", "type-apply",
		"static-arguments", "function-pointer"}},
	{"std::array", []string{
		"std-array", "array", "token-list", "clear", "remove", "truncate",
		"zero-size"}},
	{"std::string", []string{
		"std-string", "string", "strings", "from-bytes", "append-bytes", "join",
		"trim", "unicode"}},
	{"std::map", []string{
		"std-map", "map", "symbol-table", "resolver", "integer-key",
		"iterator"}},
	{"std::mem / allocator", []string{
		"std-mem", "allocator", "user-allocator", "fixed-buffer"}},
	{"std::json", []string{"std-json", "encode", "decode", "nested"}},
	{"std::sort", []string{"std-sort"}},
	{"std::float", []string{"std-float", "append", "parse", "shortest"}},
	{"std::rand", []string{"std-rand", "seed", "deterministic"}},
	{"std::fmt", []string{"std-fmt", "formatting", "artifact"}},
	{"std::testing", []string{"std-testing"}},
	{"std::fs / path / io / process", []string{
		"std-fs", "std-path", "std-io", "std-process", "fs", "io",
		"explicit-io", "read-dir", "real-path", "pure-helper", "stderr"}},
	{"std::net / http", []string{
		"net", "http", "routing", "client", "url"}},
	{"async / coro", []string{"async", "evented", "coro", "task-set"}},
}

// routes are the CLI paths each example is put through. `run` builds a native
// executable and runs it, so it is judged against the output the example
// declares rather than against the weaker fact that the command exited zero. `wasm` is judged
// the same way, by running what it emitted: a module that exits zero while
// emitting text no runtime can load is not a working backend. `wasm-opt`
// applies the same output oracle after the typed-SSA optimizer. `wasm-bin`
// repeats it through the direct binary renderer, while `browser` attaches the
// JavaScript host adapter to the browser binary.
var routes = []string{"check", "run", "llvm", "wasm", "wasm-opt", "wasm-bin", "browser"}

// wasmExecutionTimeout keeps a broken generated loop from stopping the whole
// coverage run. Examples are intentionally small and normally finish well
// below this bound even when several Wasmtime processes share a host.
const wasmExecutionTimeout = 10 * time.Second

type failureKind string

const (
	failureLowering failureKind = "lowering"
	failureTarget   failureKind = "target unsupported"
	failureRuntime  failureKind = "runtime"
	failureOutput   failureKind = "output mismatch"
)

// result records one example and whether each route accepted it.
type result struct {
	features []string
	ok       map[string]bool
	err      map[string]string
	kind     map[string]failureKind
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

// loadCases returns the runnable single-file examples. A package is left out
// because the table is per example file, and a failing case is left out because
// the columns report what a backend accepts, not what it rejects on purpose.
func loadCases() (map[string]conformance.Case, error) {
	declared, err := conformance.Discover(".")
	if err != nil {
		return nil, fmt.Errorf("%w; run from the repository root", err)
	}
	cases := map[string]conformance.Case{}
	for _, entry := range declared {
		if entry.Command == "run" && !entry.MustFail && strings.HasSuffix(entry.Path, ".kizu") {
			cases[entry.Path] = entry
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
func routeArgs(route string, entry conformance.Case, artifact string) []string {
	switch route {
	case "check":
		return []string{"check", entry.Path}
	case "run":
		return append([]string{"run", entry.Path}, entry.Args...)
	case "llvm":
		return []string{"build", "--emit-llvm", entry.Path}
	case "wasm-opt":
		return []string{"build", "--target", "wasm32-wasi", "--opt", entry.Path}
	case "wasm-bin":
		return []string{"build", "--target", "wasm32-wasi", "--emit", "wasm",
			"-o", artifact, entry.Path}
	case "browser":
		return []string{"build", "--target", "wasm32-browser", "--emit", "wasm",
			"-o", artifact, entry.Path}
	default:
		return []string{"build", "--target", "wasm32-wasi", entry.Path}
	}
}

// runAll puts every example through every route, bounded by CPU count.
func runAll(bin string, cases map[string]conformance.Case) map[string]*result {
	results := map[string]*result{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, runtime.NumCPU())
	for path, entry := range cases {
		wg.Add(1)
		go func(path string, entry conformance.Case) {
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
func runRoutes(bin string, entry conformance.Case) *result {
	res := &result{
		features: entry.Features,
		ok:       map[string]bool{},
		err:      map[string]string{},
		kind:     map[string]failureKind{},
	}
	for _, route := range routes {
		got, message, kind := runRoute(bin, route, entry)
		if message != "" {
			res.ok[route] = false
			res.err[route] = message
			res.kind[route] = kind
			continue
		}
		res.ok[route] = true
		if entry.Stdout == nil || !routeObservesOutput(route) {
			continue
		}
		if got != *entry.Stdout {
			res.ok[route] = false
			res.err[route] = fmt.Sprintf("output mismatch: want %q, got %q",
				truncate(*entry.Stdout), truncate(got))
			res.kind[route] = failureOutput
		}
	}
	return res
}

// runRoute builds one route and executes target artifacts when output is its
// oracle. It returns a classified message rather than mutating shared state.
func runRoute(bin string, route string, entry conformance.Case) (string, string, failureKind) {
	artifact, cleanup, err := newRouteArtifact(route)
	if err != nil {
		return "", firstLine(err.Error()), failureRuntime
	}
	defer cleanup()
	cmd := exec.Command(bin, routeArgs(route, entry, artifact)...)
	cmd.Env = append(os.Environ(), entry.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := firstLine(stderr.String() + stdout.String())
		return "", message, classifyBuildFailure(message)
	}
	var got string
	switch route {
	case "wasm", "wasm-opt":
		got, err = runWat(stdout.Bytes(), entry.Args, entry.Env, entry.Dirs)
	case "wasm-bin":
		got, err = runWasmFile(artifact, entry.Args, entry.Env, entry.Dirs)
	case "browser":
		got, err = runBrowserWasmFile(artifact)
	default:
		got = stdout.String()
	}
	if err != nil {
		return "", firstLine(err.Error()), failureRuntime
	}
	return got, "", ""
}

// newRouteArtifact reserves a binary path only for routes that write one.
func newRouteArtifact(route string) (string, func(), error) {
	if route != "wasm-bin" && route != "browser" {
		return "", func() {}, nil
	}
	file, err := os.CreateTemp("", "kizu-matrix-*.wasm")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// routeObservesOutput reports which route is judged by the program's stdout.
func routeObservesOutput(route string) bool {
	return route == "run" || route == "wasm" || route == "wasm-opt" ||
		route == "wasm-bin" || route == "browser"
}

// classifyBuildFailure separates a deliberate target boundary from an
// unimplemented common lowering path.
func classifyBuildFailure(message string) failureKind {
	if strings.Contains(message, "wasm error: target ") &&
		strings.Contains(message, " does not support ") {
		return failureTarget
	}
	return failureLowering
}

// runWat loads emitted WebAssembly text with wasmtime and returns what it
// printed, so the wasm column reports whether a module runs rather than whether
// the emitter exited zero.
func runWat(wat []byte, args []string, env []string, dirs []string) (string, error) {
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
	return runWasmFile(file.Name(), args, env, dirs)
}

// runWasmFile runs one text or binary module with its declared host inputs.
func runWasmFile(path string, args []string, env []string, dirs []string) (string, error) {
	wasmtimeArgs := []string{"run"}
	for _, binding := range env {
		wasmtimeArgs = append(wasmtimeArgs, "--env", binding)
	}
	for _, dir := range dirs {
		wasmtimeArgs = append(wasmtimeArgs, "--dir", dir)
	}
	wasmtimeArgs = append(wasmtimeArgs, path)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	wasmtimeArgs = append(wasmtimeArgs, args...)
	ctx, cancel := context.WithTimeout(context.Background(), wasmExecutionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wasmtime", wasmtimeArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("wasmtime: timed out after %s", wasmExecutionTimeout)
		}
		return "", fmt.Errorf("wasmtime: %s", firstLine(stderr.String()+err.Error()))
	}
	return stdout.String(), nil
}

// runBrowserWasmFile attaches the same JavaScript adapter used by pages. It is
// broad engine-level coverage; tests/browser/smoke.html is the real-browser
// boundary check.
func runBrowserWasmFile(path string) (string, error) {
	cmd := exec.Command("node", "scripts/run-browser-wasm.mjs", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("browser host: %s", firstLine(stderr.String()+err.Error()))
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
		kinds := map[failureKind]int{}
		for _, res := range results {
			if res.ok[route] {
				count++
			} else if kind := res.kind[route]; kind != "" {
				kinds[kind]++
			}
		}
		fmt.Printf("- %s: %d/%d", route, count, len(results))
		for _, kind := range []failureKind{
			failureTarget, failureLowering, failureRuntime, failureOutput,
		} {
			if kinds[kind] > 0 {
				fmt.Printf(", %s %d", kind, kinds[kind])
			}
		}
		fmt.Println()
	}
}

// printFailures lists the distinct reasons each backend rejected an example.
func printFailures(results map[string]*result) {
	for _, route := range routes {
		reasons := map[string]int{}
		for _, res := range results {
			if msg := res.err[route]; msg != "" {
				label := msg
				if kind := res.kind[route]; kind != "" {
					label = "[" + string(kind) + "] " + msg
				}
				reasons[label]++
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

// warnUnknownTags reports feature tags no group collects, so the table
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
	fmt.Fprintf(os.Stderr, "warning: %d feature tags have no group: %s\n",
		len(keys), strings.Join(keys, ", "))
}

// fail reports a fatal error and exits.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "backend-matrix:", err)
	os.Exit(1)
}
