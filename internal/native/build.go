package native

import (
	_ "embed"
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
	runtimePath, err := runtimeObject(options)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0o755); err != nil {
		return err
	}
	command, err := link(runtimePath, options.Output, options)
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
// system that inspects a binary the first time it runs do that once.
func Executable(options Options) (string, error) {
	if err := validateOptions(options); err != nil {
		return "", err
	}
	runtimePath, err := runtimeObject(options)
	if err != nil {
		return "", err
	}
	cache, err := buildcache.New()
	if err != nil {
		return "", err
	}
	return cache.GetOrBuildArtifact(
		"native-exe",
		executableCacheTarget(options, runtimePath),
		[]byte(options.LLVMIR),
		func(output string) error {
			_, err := link(runtimePath, output, options)
			return err
		},
	)
}

// link writes the IR where the toolchain can read it and links it with the
// runtime into output. The IR is transient because the executable is what is
// worth keeping: it is the thing that is expensive to make and cheap to name.
func link(runtimePath string, output string, options Options) ([]string, error) {
	tmp, err := os.MkdirTemp("", "kizu-native-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	irPath := filepath.Join(tmp, "main.ll")
	if err := os.WriteFile(irPath, []byte(options.LLVMIR), 0o644); err != nil {
		return nil, err
	}
	return runClang(irPath, runtimePath, output, options)
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

// runtimeObject returns the compiled runtime to link, compiling it only when
// nothing has it yet. The runtime is part of the compiler, not part of the
// program: its source is a constant of this binary and the numbers it names
// failures with are read from std, so compiling it once per program is the same
// work reaching the same answer every time.
func runtimeObject(options Options) (string, error) {
	if err := requireRuntimeErrorSets(options.ErrorSets); err != nil {
		return "", err
	}
	source := errorSetConstants(options.ErrorSets) + runtimeSource
	cache, err := buildcache.New()
	if err != nil {
		return "", err
	}
	return cache.GetOrBuildArtifact(
		"native-runtime.c",
		runtimeCacheTarget(options),
		[]byte(source),
		func(output string) error { return compileRuntime(source, output, options) },
	)
}

// runtimeCacheTarget spells what changes the object but is not in its source:
// the toolchain that builds it, the machine it is built for, and what it is
// asked to produce.
func runtimeCacheTarget(options Options) string {
	return strings.Join(append([]string{"native-runtime"}, toolchainKey(options)...), "/")
}

// executableCacheTarget spells what the executable is made of besides the IR:
// the same toolchain the runtime is keyed by, and the runtime object itself.
// That object is stored under a name that is its own key, so naming it here
// makes a program built against an older runtime a different artifact rather
// than the same one.
func executableCacheTarget(options Options, runtimePath string) string {
	key := append([]string{"native-exe"}, toolchainKey(options)...)
	return strings.Join(append(key, filepath.Base(runtimePath)), "/")
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
var runtimeErrorSets = []string{"std::fs::Error", "std::io::Error", "std::process::Error"}

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
	return append(flags, clangOptimizationFlag(options.Opt))
}

// clangOptimizationFlag selects the native toolchain optimization level.
// `--opt` is asked for when the binary's own speed is what matters, so it asks
// for the most the toolchain offers: on the compiler itself -O3 is 1.4% faster
// and 1.1% smaller in peak memory than -O2, for 4.6% more time spent building.
func clangOptimizationFlag(opt bool) string {
	if opt {
		return "-O3"
	}
	return "-O0"
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
