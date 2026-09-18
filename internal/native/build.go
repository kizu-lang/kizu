package native

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/buildcache"
)

// Options describes one native link request.
type Options struct {
	LLVMIR string
	// ErrorSets maps each declared error set to the number its members lower to.
	// The runtime returns those numbers, and generating the constants from the
	// declarations is what keeps the two from drifting: the order a set is
	// written in is the only thing that decides them.
	ErrorSets map[string]map[string]int
	Output    string
	Triple    string
	CPU       string
	ABI       string
	LibC      string
	Runtime   string
	Emit      string
	Linker    string
	Opt       bool
}

// Build links a lowered program into the executable the caller names, and
// records next to it what it was built from. It is the artifact command: the
// output is a file the user asked for by name, so it is written where they
// asked rather than read out of the cache.
func Build(options Options) error {
	if options.Output == "" {
		return fmt.Errorf("native error: output path is required")
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	source, err := runtimeSourceFor(options)
	if err != nil {
		return err
	}
	version, err := linkerVersion(options)
	if err != nil {
		return err
	}
	runtimePath, err := runtimeObject(source, version, options)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0o755); err != nil {
		return err
	}
	command, err := link(runtimePath, options.Output, options, version)
	if err != nil {
		return err
	}
	return writeMetadata(options, command)
}

// Executable returns an executable for one lowered program, linking it only
// when nothing has it yet. `run` and `test` want a program to execute rather
// than a file at a name they chose, and the same IR linked against the same
// runtime by the same toolchain is the same executable however often it is
// asked for. Keeping it also keeps its identity on disk, which is what lets a
// system that inspects a binary the first time it runs do that once. The
// toolchain is asked nothing until there is something to build: a run that
// finds its executable already made needs no C toolchain at all.
func Executable(options Options) (string, error) {
	if err := validateOptions(options); err != nil {
		return "", err
	}
	source, err := runtimeSourceFor(options)
	if err != nil {
		return "", err
	}
	cache, err := buildcache.New()
	if err != nil {
		return "", err
	}
	return cache.GetOrBuildArtifact(
		"native-exe",
		executableCacheTarget(options, source),
		[]byte(options.LLVMIR),
		func(output string) error {
			version, err := linkerVersion(options)
			if err != nil {
				return err
			}
			runtimePath, err := runtimeObject(source, version, options)
			if err != nil {
				return err
			}
			_, err = link(runtimePath, output, options, version)
			return err
		},
	)
}

// link writes the IR where the toolchain can read it and links it with the
// runtime into output. The IR is transient because the executable is what is
// worth keeping: it is the thing that is expensive to make and cheap to name.
func link(runtimePath string, output string, options Options, version string) ([]string, error) {
	tmp, err := os.MkdirTemp("", "kizu-native-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	llvmIR := moduleForLinker(options, version)
	irPath := filepath.Join(tmp, "main.ll")
	if err := os.WriteFile(irPath, []byte(llvmIR), 0o644); err != nil {
		return nil, err
	}
	return runClang(irPath, runtimePath, output, options)
}

// The stack probe a module asks for, as the emitter writes it and as Apple's
// clang wants it on Darwin. A frame wider than a page touches each page it
// crosses with the probe, which is what the coroutine guards rely on. Every
// module is emitted asking for LLVM's own inline probe; Apple's clang is the
// one toolchain that wants another. On arm64 Darwin it calls the system
// __chkstk_darwin helper, and before Apple clang 17 it takes any other probe
// name for a function to call, so the module would not link. Upstream LLVM
// writes the inline probe and stops at the helper's name.
const (
	inlineStackProbe = `"probe-stack"="inline-asm"`
	appleStackProbe  = `"probe-stack"="__chkstk_darwin"`
)

// moduleForLinker returns the module as the linker is to compile it: with
// Apple's stack probe when Apple's clang, named by its version line, links a
// Darwin target. The choice waits until the link so that the module, and the
// executable cache keyed by it, is the same whichever clang links it.
func moduleForLinker(options Options, version string) string {
	if !TargetIsDarwin(options.Triple) || !strings.Contains(version, "Apple clang") {
		return options.LLVMIR
	}
	return strings.Replace(options.LLVMIR, inlineStackProbe, appleStackProbe, 1)
}

// linkerVersion returns the line the linker names itself with, the first of
// its `--version` output: `Apple clang version 17.0.0 (clang-1700.0.13.3)`,
// `clang version 21.1.8`. It is what tells one clang from another where the
// path and flags are the same: the runtime object is keyed by it, and the
// Darwin stack probe is chosen by it.
func linkerVersion(options Options) (string, error) {
	out, err := exec.Command(options.Linker, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("native error: %s --version failed: %w", options.Linker, err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.Trim(line, " \t\r"), nil
}

// TargetIsDarwin reports whether a native target triple names Darwin. An
// omitted triple names the host; explicit Apple Darwin and macOS triples name
// Darwin too.
func TargetIsDarwin(triple string) bool {
	if triple == "" {
		return runtime.GOOS == "darwin"
	}
	lower := strings.ToLower(triple)
	return strings.Contains(lower, "darwin") || strings.Contains(lower, "macos")
}

// validateOptions rejects native build modes that do not have a concrete backend yet.
func validateOptions(options Options) error {
	if options.LibC != "on" {
		return fmt.Errorf("native error: --libc %s is not implemented yet", options.LibC)
	}
	if options.Runtime != "hosted" {
		return fmt.Errorf("native error: --runtime %s is not implemented yet", options.Runtime)
	}
	if options.Emit != "exe" {
		return fmt.Errorf("native error: --emit %s is not implemented yet", options.Emit)
	}
	if options.CPU != "" {
		return fmt.Errorf("native error: --cpu is not implemented yet")
	}
	if options.ABI != "" {
		return fmt.Errorf("native error: --abi is not implemented yet")
	}
	if options.Linker != "clang" {
		return fmt.Errorf("native error: --linker %s is not implemented yet", options.Linker)
	}
	return nil
}

// runtimeSourceFor returns the runtime C source a program is linked with. The
// runtime is part of the compiler, not part of the program: its source is a
// constant of this binary and the numbers it names failures with are read from
// std.
func runtimeSourceFor(options Options) (string, error) {
	if err := requireRuntimeErrorSets(options.ErrorSets); err != nil {
		return "", err
	}
	return errorSetConstants(options.ErrorSets) + runtimeSource, nil
}

// runtimeObject returns the compiled runtime to link, compiling it only when
// nothing has it yet. Compiling the same source with the same clang once per
// program is the same work reaching the same answer every time.
func runtimeObject(source string, version string, options Options) (string, error) {
	cache, err := buildcache.New()
	if err != nil {
		return "", err
	}
	return cache.GetOrBuildArtifact(
		"native-runtime.c",
		runtimeCacheTarget(options, version),
		[]byte(source),
		func(output string) error { return compileRuntime(source, output, options) },
	)
}

// runtimeCacheTarget spells what changes the object but is not in its source:
// the toolchain that builds it, the machine it is built for, what it is asked
// to produce, and which clang it is. The clang is named by its version line
// because the path and flags stay the same across an upgrade, and an object
// the old clang made must not be linked into what the new one builds.
func runtimeCacheTarget(options Options, version string) string {
	key := append([]string{"native-runtime"}, toolchainKey(options)...)
	return strings.Join(append(key, version), "/")
}

// executableCacheTarget spells what the executable is made of besides the IR:
// the same driver and flags the runtime is keyed by, and the runtime source
// it is linked with, by hash, so a program built against an older runtime is
// a different artifact rather than the same one. The clang's version is not
// part of it: an executable already made was linked whole by one clang, and
// asking which one would cost every run the toolchain it does not need.
func executableCacheTarget(options Options, runtimeSource string) string {
	key := append([]string{"native-exe"}, toolchainKey(options)...)
	sum := sha256.Sum256([]byte(runtimeSource))
	return strings.Join(append(key, hex.EncodeToString(sum[:])), "/")
}

// toolchainKey spells what builds an artifact rather than what it is built
// from: the driver, the machine it targets, and the flags it is asked to honour.
func toolchainKey(options Options) []string {
	key := []string{options.Linker, runtime.GOOS + "-" + runtime.GOARCH}
	return append(key, clangFlags(options)...)
}

// compileRuntime compiles the runtime source into one object file.
func compileRuntime(source string, output string, options Options) error {
	dir, err := os.MkdirTemp("", "kizu-runtime-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	sourcePath := filepath.Join(dir, "runtime.c")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		return err
	}
	args := append(clangFlags(options), "-c", sourcePath, "-o", output)
	out, err := exec.Command(options.Linker, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("native error: %s failed: %w\n%s", options.Linker, err, out)
	}
	return nil
}

// runtimeErrorSets names the sets the runtime reports its failures with. It
// refers to all of them whatever a program uses, so a build that does not carry
// them would fail in the C compiler with an undeclared name.
var runtimeErrorSets = []string{
	"std::coro::Error",
	"std::crypto::Error",
	"std::fs::Error",
	"std::io::Error",
	"std::net::Error",
	"std::process::Error",
}

// requireRuntimeErrorSets rejects a build that cannot name what the runtime
// returns.
func requireRuntimeErrorSets(sets map[string]map[string]int) error {
	for _, name := range runtimeErrorSets {
		if len(sets[name]) == 0 {
			return fmt.Errorf("native error: error set `%s` is missing", name)
		}
	}
	return nil
}

// errorSetConstants writes the number each error set member lowers to, so the
// runtime names a failure the same way the program that reads it does.
func errorSetConstants(sets map[string]map[string]int) string {
	if len(sets) == 0 {
		return ""
	}
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("/* Generated from the error set declarations. */\n")
	for _, name := range names {
		members := sets[name]
		spellings := make([]string, 0, len(members))
		for member := range members {
			spellings = append(spellings, member)
		}
		sort.Strings(spellings)
		for _, member := range spellings {
			fmt.Fprintf(&out, "#define %s %d\n", errorConstantName(name, member), members[member])
		}
	}
	out.WriteString("\n")
	return out.String()
}

// errorConstantName spells one member as a C identifier.
func errorConstantName(set string, member string) string {
	replacer := strings.NewReplacer("::", "_", ".", "_")
	return "KIZU_ERR_" + strings.ToUpper(replacer.Replace(set)+"_"+camelToSnake(member))
}

// camelToSnake separates the words in a member name for the C spelling.
func camelToSnake(name string) string {
	var out strings.Builder
	for index, r := range name {
		if index > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// runClang invokes the configured C/LLVM toolchain with explicit inputs.
func runClang(irPath string, runtimePath string, output string, options Options) ([]string, error) {
	args := append(clangFlags(options), irPath, runtimePath, "-o", output)
	cmd := exec.Command(options.Linker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("native error: %s failed: %w\n%s", options.Linker, err, out)
	}
	return append([]string{options.Linker}, args...), nil
}

// clangFlags spells what the toolchain is asked to produce. The runtime object
// is keyed by these, so the key cannot name one build and the compile another.
func clangFlags(options Options) []string {
	flags := []string{}
	if options.Triple != "" {
		flags = append(flags, "-target", options.Triple)
	}
	return append(flags, clangOptimizationFlags(options.Opt)...)
}

// clangOptimizationFlags select the native toolchain optimization level.
// `--opt` is asked for when the binary's own speed is what matters, so it asks
// for the most the toolchain offers: on the compiler itself -O3 is 1.4% faster
// and 1.1% smaller in peak memory than -O2, for 4.6% more time spent building.
//
// It also keeps the code generator from merging identical instruction tails
// of blocks that jump to one successor. Arms of a dispatch loop that end in
// the same store and step are otherwise merged into one shared tail, and
// every iteration of those arms takes a jump into it; with LLVM 16 a
// tagged-union interpreter loop runs 9% slower for it, and none of the
// programs it was measured on runs slower without the merge.
//
// And it starts every loop on a 32-byte boundary. The arm64 code generator
// aligns none by default, so where a hot loop falls depends on how much code
// precedes it, runtime included: a change to the runtime that moved the
// entry point by 92 bytes made a loop over two million doubles 14% slower,
// with not one instruction of the loop changed. Aligned, the loop is where it
// was in either build.
func clangOptimizationFlags(opt bool) []string {
	if opt {
		return []string{
			"-O3", "-falign-loops=32", "-Xclang", "-mllvm", "-Xclang", "-enable-tail-merge=false",
		}
	}
	return []string{"-O0"}
}

// Metadata records explicit native build inputs next to the output artifact.
type Metadata struct {
	Target  string   `json:"target"`
	Triple  string   `json:"triple"`
	CPU     string   `json:"cpu"`
	ABI     string   `json:"abi"`
	LibC    string   `json:"libc"`
	Runtime string   `json:"runtime"`
	Emit    string   `json:"emit"`
	Linker  string   `json:"linker"`
	OptMode string   `json:"optimization_mode"`
	Output  string   `json:"output"`
	Command []string `json:"command"`
}

// writeMetadata writes the explicit build configuration used for this artifact.
func writeMetadata(options Options, command []string) error {
	metadata := Metadata{
		Target: "native", Triple: options.Triple, CPU: options.CPU, ABI: options.ABI,
		LibC: options.LibC, Runtime: options.Runtime, Emit: options.Emit,
		Linker: options.Linker, OptMode: optimizationModeName(options.Opt),
		Output: options.Output, Command: command,
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(options.Output+".kizu-build.json", append(data, '\n'), 0o644)
}

// optimizationModeName records the native optimization mode in artifact metadata.
func optimizationModeName(opt bool) string {
	if opt {
		return "opt"
	}
	return "debug"
}

// runtimeSource is the C runtime every native build links with, embedded from
// runtime/runtime.c so the source is one file rather than one Go literal. It
// lives in a subdirectory because the Go toolchain refuses a .c file in a
// non-cgo package directory. The bytes
// are what the runtime object cache key is derived from, so they are carried
// unchanged.
//
//go:embed runtime/runtime.c
var runtimeSource string
