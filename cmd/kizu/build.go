package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/native"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/wasm"
)

const (
	browserESMRuntime = "browser/app.mjs"
	browserESMModule  = "app.mjs"
	browserESMWASM    = "app.wasm"
)

// stdErrorSets returns the numbers the runtime names its failures with. The
// runtime refers to every std error set whatever a program uses, so a build
// that cannot read them cannot say what it failed at.
func stdErrorSets() (map[string]map[string]int, error) {
	return project.StdErrorSets()
}

// linkModule emits one lowered module and returns an executable for it. The
// executable is kept rather than thrown away, so a program that has not changed
// since it was last run is not linked a second time.
func linkModule(module *ir.Module) (string, error) {
	ir.KeepTargetReachableFunctions(module, "", "main")
	llvmIR, err := llvm.EmitNative(module, runtime.GOOS == "darwin")
	if err != nil {
		return "", err
	}
	errorSets, err := stdErrorSets()
	if err != nil {
		return "", err
	}
	return native.Executable(native.Options{
		LLVMIR: llvmIR, ErrorSets: errorSets,
		LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang",
	})
}

// buildFile dispatches build subcommands.
func buildFile(args []string) error {
	if len(args) < 2 {
		usage()
		return fmt.Errorf("invalid build command")
	}
	switch args[0] {
	case "--emit-llvm":
		path, opt, err := parseOptFileArgs(args[1:])
		if err != nil {
			return err
		}
		return emitLLVMFile(path, opt)
	case "--target":
		return emitTargetFile(args[1], args[2:])
	default:
		usage()
		return fmt.Errorf("invalid build command")
	}
}

// lowerTarget lowers a package root the way build lowers it and any other
// target as one file. Every command that shows or links a lowered module goes
// through here, so `ir` and `build` are asked the same question about the same
// program instead of one of them refusing a directory.
func lowerTarget(path string, opt bool) (*ir.Module, error) {
	return lowerTargetForTarget(path, opt, stdtarget.Native)
}

// lowerTargetForTarget lowers a file or package for one selected build target.
func lowerTargetForTarget(
	path string,
	opt bool,
	target stdtarget.Target,
) (*ir.Module, error) {
	if isPackageRoot(path) {
		return lowerPackageForTarget(path, opt, target)
	}
	return lowerFileForTarget(path, opt, target)
}

// emitLLVMFile lowers a checked source file or package to LLVM IR text.
// The text is emitted fresh every time: a text cache keyed by the source
// would have to key on the compiler itself to stay honest (ADR-0126).
func emitLLVMFile(path string, opt bool) error {
	module, err := lowerTarget(path, opt)
	if err != nil {
		return err
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	output, err := llvm.Emit(module)
	if err != nil {
		return err
	}
	_, _ = fmt.Println(output)
	return nil
}

// emitTargetFile lowers a checked source file to the requested target.
func emitTargetFile(target string, args []string) error {
	if target == "native" {
		return emitNativeFile(args)
	}
	var wasmTarget wasm.Target
	switch target {
	case "wasm32-wasi":
		wasmTarget = wasm.TargetWASI
	case "wasm32-browser":
		wasmTarget = wasm.TargetBrowser
	default:
		usage()
		return fmt.Errorf("invalid build target `%s`", target)
	}
	options, err := parseWASMBuildArgs(args, wasmTarget == wasm.TargetBrowser)
	if err != nil {
		return err
	}
	return emitWASMFile(options, wasmTarget)
}

type wasmBuildOptions struct {
	Path   string
	Opt    bool
	Emit   string
	Output string
}

type wasmBuildParser struct {
	args     []string
	options  wasmBuildOptions
	index    int
	emitSeen bool
	browser  bool
}

// parseWASMBuildArgs accepts an explicit text or binary renderer while keeping
// the legacy `[--opt] <file>` WAT-to-stdout shape.
func parseWASMBuildArgs(args []string, browser bool) (wasmBuildOptions, error) {
	parser := wasmBuildParser{
		args: args, options: wasmBuildOptions{Emit: "wat"}, browser: browser,
	}
	for parser.index < len(parser.args) {
		if err := parser.parseArgument(); err != nil {
			return wasmBuildOptions{}, err
		}
		parser.index++
	}
	return parser.finish()
}

// parseArgument applies one option or source path to the accumulated command.
func (p *wasmBuildParser) parseArgument() error {
	switch p.args[p.index] {
	case "--opt":
		return p.parseOpt()
	case "--emit":
		return p.parseEmit()
	case "-o", "--output":
		return p.parseOutput()
	default:
		return p.parsePath()
	}
}

// parseOpt accepts the optimization switch once.
func (p *wasmBuildParser) parseOpt() error {
	if p.options.Opt {
		return fmt.Errorf("duplicate --opt")
	}
	p.options.Opt = true
	return nil
}

// parseEmit selects the WAT or binary renderer once.
func (p *wasmBuildParser) parseEmit() error {
	if p.emitSeen {
		return fmt.Errorf("invalid wasm build arguments")
	}
	p.emitSeen = true
	if p.index+1 >= len(p.args) {
		if p.browser {
			return fmt.Errorf("--emit needs wat, wasm, or esm")
		}
		return fmt.Errorf("--emit needs wat or wasm")
	}
	p.index++
	format := p.args[p.index]
	if format != "wat" && format != "wasm" && !(p.browser && format == "esm") {
		return fmt.Errorf("invalid wasm emit `%s`", format)
	}
	p.options.Emit = format
	return nil
}

// parseOutput retains one explicit artifact path.
func (p *wasmBuildParser) parseOutput() error {
	if p.index+1 >= len(p.args) || p.options.Output != "" {
		return fmt.Errorf("invalid wasm output path")
	}
	p.index++
	p.options.Output = p.args[p.index]
	return nil
}

// parsePath retains the single non-option source path.
func (p *wasmBuildParser) parsePath() error {
	path := p.args[p.index]
	if strings.HasPrefix(path, "-") || p.options.Path != "" {
		return fmt.Errorf("invalid wasm build arguments")
	}
	p.options.Path = path
	return nil
}

// finish validates requirements that depend on more than one argument.
func (p *wasmBuildParser) finish() (wasmBuildOptions, error) {
	if p.options.Path == "" {
		return wasmBuildOptions{}, fmt.Errorf("wasm build needs one source or package path")
	}
	if p.options.Emit == "wasm" && p.options.Output == "" {
		return wasmBuildOptions{}, fmt.Errorf("binary wasm output requires -o <path>")
	}
	if p.options.Emit == "esm" && p.options.Output == "" {
		return wasmBuildOptions{}, fmt.Errorf("browser esm output requires -o <directory>")
	}
	return p.options, nil
}

// emitWASMFile lowers a checked source file or package once and renders its
// selected WebAssembly host target fresh like emitLLVMFile (ADR-0126).
func emitWASMFile(options wasmBuildOptions, target wasm.Target) error {
	module, err := lowerTargetForTarget(
		options.Path,
		options.Opt,
		stdTargetForWASM(target),
	)
	if err != nil {
		return err
	}
	// WebAssembly modules expose only target-selected entry/export roots, so
	// unreachable functions must not drag unused host capabilities into imports.
	ir.KeepTargetReachableFunctions(module, wasmExportABI(target), "main")
	lowered, err := wasm.LowerTarget(module, target)
	if err != nil {
		return err
	}
	if options.Emit == "wasm" || options.Emit == "esm" {
		output, err := lowered.Binary()
		if err != nil {
			return err
		}
		if options.Emit == "esm" {
			return emitBrowserESM(options.Output, output)
		}
		return os.WriteFile(options.Output, output, 0o644)
	}
	output := lowered.WAT()
	if options.Output != "" {
		return os.WriteFile(options.Output, []byte(output), 0o644)
	}
	_, _ = fmt.Println(output)
	return nil
}

// emitBrowserESM writes the fixed JavaScript host module beside the binary it
// loads. The runtime is read before touching the output directory, so a broken
// installation cannot leave a wasm-only bundle that looks complete.
func emitBrowserESM(outputDir string, wasmBytes []byte) error {
	runtimePath, err := stdlib.FindLibFile(browserESMRuntime)
	if err != nil {
		return err
	}
	runtimeBytes, err := os.ReadFile(runtimePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, browserESMWASM), wasmBytes, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, browserESMModule), runtimeBytes, 0o644)
}

// stdTargetForWASM maps a backend host contract to the compile-time target.
func stdTargetForWASM(target wasm.Target) stdtarget.Target {
	if target == wasm.TargetBrowser {
		return stdtarget.WasmBrowser
	}
	return stdtarget.WasmWASI
}

// wasmExportABI returns the explicit exports one WebAssembly host can enter.
func wasmExportABI(target wasm.Target) string {
	if target == wasm.TargetBrowser {
		return "browser"
	}
	return ""
}

// emitNativeFile lowers and links a source file into a native executable.
func emitNativeFile(args []string) error {
	options, err := parseNativeBuildArgs(args)
	if err != nil {
		return err
	}
	// Native --opt controls clang's native optimization. The typed-SSA optimizer
	// remains scoped to `build --emit-llvm --opt` until it is package-scale safe.
	const nativeIROpt = false
	var module *ir.Module
	if isPackageRoot(options.Path) {
		module, err = lowerPackage(options.Path, nativeIROpt)
	} else {
		module, err = lowerFile(options.Path, nativeIROpt)
	}
	if err != nil {
		return err
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	llvmIR, err := llvm.EmitNative(module, nativeTargetIsDarwin(options.Triple))
	if err != nil {
		return err
	}
	errorSets, err := stdErrorSets()
	if err != nil {
		return err
	}
	if err := native.Build(native.Options{
		LLVMIR: llvmIR, Output: options.Output, ErrorSets: errorSets,
		Triple: options.Triple,
		CPU:    options.CPU, ABI: options.ABI, LibC: options.LibC,
		Runtime: options.Runtime, Emit: options.Emit, Linker: options.Linker,
		Opt: options.Opt,
	}); err != nil {
		return err
	}
	_, _ = fmt.Println(options.Output)
	return nil
}

// nativeTargetIsDarwin identifies the stack-probe ABI selected by the native
// target. An omitted triple names the host; explicit Apple Darwin and macOS
// triples use the same system helper.
func nativeTargetIsDarwin(triple string) bool {
	if triple == "" {
		return runtime.GOOS == "darwin"
	}
	lower := strings.ToLower(triple)
	return strings.Contains(lower, "darwin") || strings.Contains(lower, "macos")
}

// parseOptFileArgs parses an optional --opt flag followed by one file path.
// A lone `--opt` names the flag, not a file, so it is an argument error rather
// than a path the command tries to open.
func parseOptFileArgs(args []string) (string, bool, error) {
	if len(args) == 1 && args[0] != "--opt" {
		return args[0], false, nil
	}
	if len(args) == 2 && args[0] == "--opt" {
		return args[1], true, nil
	}
	usage()
	return "", false, fmt.Errorf("invalid command arguments")
}

// nativeBuildArgs stores Zig-style native build options accepted by the CLI.
type nativeBuildArgs struct {
	Path    string
	Output  string
	Triple  string
	CPU     string
	ABI     string
	LibC    string
	Runtime string
	Emit    string
	Linker  string
	Opt     bool
}

// parseNativeBuildArgs parses native build flags and derives defaults.
func parseNativeBuildArgs(args []string) (nativeBuildArgs, error) {
	options := nativeBuildArgs{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	for i := 0; i < len(args); i++ {
		var err error
		switch args[i] {
		case "--opt":
			options.Opt = true
		case "-o":
			i, options.Output, err = nextNativeArg(args, i, "-o")
		case "--triple":
			i, options.Triple, err = nextNativeArg(args, i, "--triple")
		case "--cpu":
			i, options.CPU, err = nextNativeArg(args, i, "--cpu")
		case "--abi":
			i, options.ABI, err = nextNativeArg(args, i, "--abi")
		case "--libc":
			i, options.LibC, err = nextNativeArg(args, i, "--libc")
		case "--runtime":
			i, options.Runtime, err = nextNativeArg(args, i, "--runtime")
		case "--emit":
			i, options.Emit, err = nextNativeArg(args, i, "--emit")
		case "--linker":
			i, options.Linker, err = nextNativeArg(args, i, "--linker")
		default:
			if options.Path != "" {
				usage()
				return nativeBuildArgs{}, fmt.Errorf("invalid command arguments")
			}
			options.Path = args[i]
		}
		if err != nil {
			usage()
			return nativeBuildArgs{}, err
		}
	}
	if options.Path == "" {
		usage()
		return nativeBuildArgs{}, fmt.Errorf("missing source file")
	}
	if options.Output == "" {
		options.Output = defaultNativeOutput(options.Path, options.Emit)
	}
	return options, nil
}

// nextNativeArg returns the value following a flag.
func nextNativeArg(args []string, index int, flag string) (int, string, error) {
	if index+1 >= len(args) {
		return index, "", fmt.Errorf("missing value after %s", flag)
	}
	return index + 1, args[index+1], nil
}

// defaultNativeOutput maps a source path to the default target/native artifact.
func defaultNativeOutput(path string, emit string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if emit == "obj" {
		name += ".o"
	}
	if emit == "llvm" {
		name += ".ll"
	}
	return filepath.Join("target", "native", name)
}
