package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/buildcache"
	"github.com/kizu-lang/kizu/internal/cimport"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/native"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/types"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// main dispatches the kizu command line interface.
func main() {
	if len(os.Args) < 2 || (len(os.Args) < 3 && !commandAllowsNoTarget(os.Args[1])) {
		usage()
		os.Exit(2)
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		var status exitStatus
		if errors.As(err, &status) {
			os.Exit(status.code)
		}
		printError(err)
		os.Exit(1)
	}
}

// exitStatus exits without printing an extra Go diagnostic.
type exitStatus struct {
	code int
}

// Error renders the process exit status for tests and wrapping.
func (s exitStatus) Error() string {
	return fmt.Sprintf("exit status %d", s.code)
}

// printError writes a stable top-level CLI error prefix.
func printError(err error) {
	var structured *diag.Diagnostic
	if errors.As(err, &structured) {
		_, _ = fmt.Fprintln(os.Stderr, structured.CLIError())
		return
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "error:") {
		_, _ = fmt.Fprintln(os.Stderr, msg)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "error: "+msg)
}

// printParserDiagnostics writes parser diagnostics using the shared CLI severity prefix.
func printParserDiagnostics(diags []parser.Diagnostic) {
	for _, diag := range diags {
		_, _ = fmt.Fprintln(os.Stderr, diag.CLIError())
	}
}

// dispatch runs one CLI command.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "parse":
		return runSelfhostFrontendCommand("parse", args)
	case "run":
		if selfhostRunEnabled() {
			return runSelfhostFrontendCommand("run", args)
		}
		path, programArgs := splitProgramArgs(args)
		return runFile(path, programArgs)
	case "check":
		return runSelfhostFrontendCommand("check", args)
	case "test":
		path, programArgs := splitProgramArgs(args)
		return testFile(path, programArgs)
	case "fmt":
		return fmtCommand(args)
	case "init":
		return initCommand(args)
	case "ir":
		return irCommand(args)
	case "build":
		return buildFile(args)
	case "cache":
		return cacheCommand(args)
	case "why-rebuild":
		return whyRebuildFile(args[0])
	case "import-c-header":
		return importCHeaderFile(args[0])
	default:
		usage()
		return fmt.Errorf("unknown command `%s`", cmd)
	}
}

// selfhostRunEnvVar is the rollback-friendly switch point for routing the public
// `kizu run <file>` command through the selfhost-owned compiled artifact path
// (selfhost::cli::execute::run_file_cli, which lowers a run-codegen program, links
// it, and executes the native artifact) instead of the Go interpreter. It defaults
// off so the general Go-owned run surface is unchanged; supported shapes that the
// selfhost backend can lower are owned end to end when it is on, and unsupported
// shapes surface explicit selfhost diagnostics rather than falling back to Go.
//
// This gate is the deliberate switch boundary for #1151 / parent #1070. It is not a
// permanent compatibility branch: it is removed (default flipped to selfhost) once
// `run` is selfhost-owned for the general language/runtime surface tracked by #1070.
const selfhostRunEnvVar = "KIZU_SELFHOST_RUN"

// selfhostRunEnabled reports whether the public `run` command is routed through the
// selfhost-owned compiled artifact path. See selfhostRunEnvVar.
func selfhostRunEnabled() bool {
	return os.Getenv(selfhostRunEnvVar) == "1"
}

// runSelfhostFrontendCommand executes frontend commands through Kizu-owned code.
func runSelfhostFrontendCommand(command string, args []string) error {
	processArgs, err := selfhostFrontendProcessArgs(command, args)
	if err != nil {
		return err
	}
	manifestPath, err := findRepoFile("selfhost/kizu.toml")
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(filepath.Dir(manifestPath))
	oldWd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(repoRoot); err != nil {
		return err
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		return err
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	code, err := interp.NewWithProcessArgs(os.Stdout, processArgs).
		RunEntryInt(program, "selfhost::cli_main")
	if err != nil {
		return err
	}
	if code != 0 {
		return exitStatus{code: int(code)}
	}
	return nil
}

// selfhostFrontendProcessArgs preserves CLI validation for Kizu while normalizing real targets.
func selfhostFrontendProcessArgs(command string, args []string) ([]string, error) {
	processArgs := make([]string, 0, len(args)+1)
	processArgs = append(processArgs, command)
	processArgs = append(processArgs, args...)
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		absTarget, err := filepath.Abs(args[0])
		if err != nil {
			return nil, err
		}
		processArgs[1] = absTarget
		return processArgs, nil
	}
	if command != "fmt" || len(args) != 2 || !isFmtWriteFlag(args[0]) ||
		strings.HasPrefix(args[1], "-") {
		return processArgs, nil
	}
	absTarget, err := filepath.Abs(args[1])
	if err != nil {
		return nil, err
	}
	processArgs[2] = absTarget
	return processArgs, nil
}

// usage prints the supported command line shape.
func usage() {
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu <parse|run|check|test> <file> [-- args...]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu init [path]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu fmt [--write] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu ir [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --emit-llvm [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target native [native-options] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target wasm32-wasi [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu cache <status|prune>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu why-rebuild <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu import-c-header <file>")
}

// parseFile parses a source file and prints its AST summary.
func parseFile(path string) error {
	program, errs, err := parsePath(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	_, _ = fmt.Println(program.String())
	return nil
}

// runFile parses a source file and executes it with the interpreter.
func runFile(path string, args []string) error {
	if isPackageRoot(path) {
		return runPackage(path, args)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	return interp.NewWithProcessArgs(os.Stdout, args).Run(program)
}

// runPackage resolves a package root and executes the root module main.
func runPackage(path string, args []string) error {
	graph, program, err := loadPackageProgram(path)
	if err != nil {
		return err
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	entry := graph.Root + "::main"
	return interp.NewWithProcessArgs(os.Stdout, args).RunEntry(program, entry)
}

// checkFile parses a source file and runs static checks.
func checkFile(path string) error {
	if isPackageRoot(path) {
		return checkPackage(path)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}

// checkPackage resolves a package root and runs package-level static checks.
func checkPackage(path string) error {
	_, program, err := loadPackageProgram(path)
	if err != nil {
		return err
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}

// isPackageRoot reports whether path names a directory or kizu.toml manifest.
func isPackageRoot(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir() || filepath.Base(path) == "kizu.toml"
}

// loadPackageGraph parses kizu.toml and resolves the package module graph.
func loadPackageGraph(path string) (project.Graph, error) {
	root := path
	manifestPath := filepath.Join(path, "kizu.toml")
	if filepath.Base(path) == "kizu.toml" {
		root = filepath.Dir(path)
		manifestPath = path
	}
	source, err := os.ReadFile(manifestPath)
	if err != nil {
		return project.Graph{}, err
	}
	manifest, err := project.ParseManifest(string(source))
	if err != nil {
		return project.Graph{}, err
	}
	return project.ResolveModules(root, manifest)
}

// loadPackageProgram resolves a package root and loads its merged program.
func loadPackageProgram(path string) (project.Graph, *ast.Program, error) {
	graph, err := loadPackageGraph(path)
	if err != nil {
		return project.Graph{}, nil, err
	}
	program, err := project.LoadProgram(graph)
	if err != nil {
		return project.Graph{}, nil, err
	}
	stdDecls, err := packageStdDecls(graph)
	if err != nil {
		return project.Graph{}, nil, err
	}
	program.Decls = append(stdDecls, program.Decls...)
	return graph, program, nil
}

// packageStdDecls loads std wrapper declarations referenced by package modules.
func packageStdDecls(graph project.Graph) ([]ast.Decl, error) {
	sources := make([]string, 0, len(graph.Modules))
	for _, module := range graph.Modules {
		data, err := os.ReadFile(module.File)
		if err != nil {
			return nil, err
		}
		sources = append(sources, string(data))
	}
	decls, errs, err := stdlib.DeclsForSources(sources)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, &errs[0]
	}
	return decls, nil
}

// testFile runs Kizu test blocks and reports a minimal test result.
func testFile(path string, args []string) error {
	if isPackageRoot(path) {
		return testPackage(path, args)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	if err := interp.NewWithProcessArgs(os.Stdout, args).RunTests(program); err != nil {
		return err
	}
	_, _ = fmt.Println("test: ok")
	return nil
}

// testPackage resolves a package root and runs package test blocks.
func testPackage(path string, args []string) error {
	_, program, err := loadPackageProgram(path)
	if err != nil {
		return err
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	if err := interp.NewWithProcessArgs(os.Stdout, args).RunTests(program); err != nil {
		return err
	}
	_, _ = fmt.Println("test: ok")
	return nil
}

// splitProgramArgs separates the source path from optional Kizu process args.
func splitProgramArgs(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) >= 2 && args[1] == "--" {
		return args[0], args[2:]
	}
	return args[0], nil
}

// fmtCommand prints or rewrites a Kizu source file in canonical form.
//
// usage: kizu fmt [--write] <file>
//
//	--write: rewrite the file in-place.
func fmtCommand(args []string) error {
	return runSelfhostFrontendCommand("fmt", args)
}

// isFmtWriteFlag reports whether an argument selects in-place formatting.
func isFmtWriteFlag(arg string) bool {
	return arg == "--write" || arg == "-w"
}

// irCommand parses options and dumps typed SSA IR.
func irCommand(args []string) error {
	path, opt, err := parseOptFileArgs(args)
	if err != nil {
		return err
	}
	module, err := lowerFile(path, opt)
	if err != nil {
		return err
	}
	_, _ = fmt.Println(ir.Dump(module))
	return nil
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

// emitLLVMFile lowers a checked source file to LLVM IR text.
func emitLLVMFile(path string, opt bool) error {
	if isPackageRoot(path) {
		module, err := lowerPackage(path, opt)
		if err != nil {
			return err
		}
		output, err := llvm.Emit(module)
		if err != nil {
			return err
		}
		_, _ = fmt.Println(output)
		return nil
	}

	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	result, err := cache.GetOrBuild(path, cacheTarget("emit-llvm", opt), func() (string, error) {
		module, err := lowerFile(path, opt)
		if err != nil {
			return "", err
		}
		return llvm.Emit(module)
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Println(result.Output)
	return nil
}

// emitTargetFile lowers a checked source file to the requested target.
func emitTargetFile(target string, args []string) error {
	if target == "native" {
		return emitNativeFile(args)
	}
	if target != "wasm32-wasi" {
		usage()
		return fmt.Errorf("invalid build target `%s`", target)
	}
	path, opt, err := parseOptFileArgs(args)
	if err != nil {
		return err
	}
	return emitWASMFile(path, opt)
}

// emitWASMFile lowers a checked source file to WASI WebAssembly text.
func emitWASMFile(path string, opt bool) error {
	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	result, err := cache.GetOrBuild(path, cacheTarget("wasm32-wasi", opt), func() (string, error) {
		module, err := lowerFile(path, opt)
		if err != nil {
			return "", err
		}
		return wasm.Emit(module)
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Println(result.Output)
	return nil
}

// emitNativeFile lowers and links a source file into a native executable.
func emitNativeFile(args []string) error {
	options, err := parseNativeBuildArgs(args)
	if err != nil {
		return err
	}
	var module *ir.Module
	if isPackageRoot(options.Path) {
		module, err = lowerPackage(options.Path, options.Opt)
	} else {
		module, err = lowerFile(options.Path, options.Opt)
	}
	if err != nil {
		return err
	}
	llvmIR, err := llvm.Emit(module)
	if err != nil {
		return err
	}
	if err := native.Build(native.Options{
		LLVMIR: llvmIR, Output: options.Output, Triple: options.Triple,
		CPU: options.CPU, ABI: options.ABI, LibC: options.LibC,
		Runtime: options.Runtime, Emit: options.Emit, Linker: options.Linker,
	}); err != nil {
		return err
	}
	_, _ = fmt.Println(options.Output)
	return nil
}

// parseOptFileArgs parses an optional --opt flag followed by one file path.
func parseOptFileArgs(args []string) (string, bool, error) {
	if len(args) == 1 {
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

// cacheTarget includes the optimization level in cache-shaping inputs.
func cacheTarget(target string, opt bool) string {
	if opt {
		return target + "-opt"
	}
	return target
}

// cacheCommand dispatches cache maintenance commands.
func cacheCommand(args []string) error {
	if len(args) != 1 {
		usage()
		return fmt.Errorf("invalid cache command")
	}
	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		return printCacheStatus(cache)
	case "prune":
		return pruneCache(cache)
	default:
		usage()
		return fmt.Errorf("invalid cache command")
	}
}

// printCacheStatus prints cache size, entry count, and limit.
func printCacheStatus(cache *buildcache.Cache) error {
	status, err := cache.Status()
	if err != nil {
		return err
	}
	_, _ = fmt.Printf("cache dir: %s\n", status.Dir)
	_, _ = fmt.Printf("entries: %d\n", status.Entries)
	_, _ = fmt.Printf("size bytes: %d\n", status.SizeBytes)
	_, _ = fmt.Printf("max bytes: %d\n", status.MaxBytes)
	return nil
}

// pruneCache removes local cache entries.
func pruneCache(cache *buildcache.Cache) error {
	entries, bytes, err := cache.Prune()
	if err != nil {
		return err
	}
	_, _ = fmt.Printf("cache pruned: removed %d entries, freed %d bytes\n", entries, bytes)
	return nil
}

// whyRebuildFile explains the cache state for a source file.
func whyRebuildFile(path string) error {
	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	reason, err := cache.WhyRebuild(path, "emit-llvm")
	if err != nil {
		return err
	}
	_, _ = fmt.Println(reason)
	return nil
}

// importCHeaderFile converts supported C prototypes into Kizu extern declarations.
func importCHeaderFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	output, err := cimport.Import(string(b))
	if err != nil {
		return err
	}
	_, _ = fmt.Println(output)
	return nil
}

// lowerFile parses, checks, lowers, and optionally optimizes source to typed SSA IR.
func lowerFile(path string, opt bool) (*ir.Module, error) {
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		printParserDiagnostics(errs)
		return nil, fmt.Errorf("parse failed")
	}
	if err := checkProgram(program); err != nil {
		return nil, err
	}
	module, err := ir.Lower(program)
	if err != nil {
		return nil, err
	}
	if opt {
		ir.Optimize(module)
	}
	return module, nil
}

// lowerPackage resolves a package graph and lowers its qualified program to typed SSA IR.
func lowerPackage(path string, opt bool) (*ir.Module, error) {
	graph, program, err := loadPackageProgram(path)
	if err != nil {
		return nil, err
	}
	if err := checkProgram(program); err != nil {
		return nil, err
	}
	module, err := ir.Lower(program)
	if err != nil {
		return nil, err
	}
	if opt {
		ir.Optimize(module)
	}
	addPackageMain(module, graph.Root+"::cli_main")
	return module, nil
}

// addPackageMain exposes a package CLI entry as the native executable entrypoint.
func addPackageMain(module *ir.Module, entry string) {
	if moduleFunction(module, "main") != nil {
		return
	}
	entryFn := moduleFunction(module, entry)
	if entryFn == nil || len(entryFn.Params) != 0 || entryFn.Return != "!i64" {
		return
	}
	result := ir.Value{Name: "%1", Type: entryFn.Return}
	mainFn := &ir.Function{
		Name:   "main",
		Return: entryFn.Return,
		Blocks: []*ir.Block{{
			Name: "entry",
			Instrs: []*ir.Instr{{
				Result: result,
				Op:     "call." + entry,
			}},
			Terminator: ir.Terminator{Op: "return", Value: result},
		}},
	}
	module.Functions = append(module.Functions, mainFn)
}

// moduleFunction returns the lowered function with the requested symbol.
func moduleFunction(module *ir.Module, name string) *ir.Function {
	for _, fn := range module.Functions {
		if fn.Name == name {
			return fn
		}
	}
	return nil
}

// checkProgram runs static checks required before compilation or execution.
func checkProgram(program *ast.Program) error {
	if err := types.New().Check(program); err != nil {
		return err
	}
	if err := ownership.New().Check(program); err != nil {
		return err
	}
	return nil
}

// parsePath reads and parses a source file.
func parsePath(path string) (*ast.Program, []parser.Diagnostic, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	p := parser.New(lexer.New(string(b)))
	program := p.ParseProgram()
	return program, p.Diagnostics(), nil
}

// parsePathWithStd parses a source file and appends Kizu std wrappers.
func parsePathWithStd(path string) (*ast.Program, []parser.Diagnostic, error) {
	program, errs, err := parsePath(path)
	if err != nil || len(errs) > 0 {
		return program, errs, err
	}
	stdDecls, stdErrs, err := stdlib.DeclsForSourcePath(path)
	if err != nil || len(stdErrs) > 0 {
		return program, stdErrs, err
	}
	program.Decls = append(stdDecls, program.Decls...)
	return program, nil, nil
}

// resolveStdModules returns std modules in dependency-before-dependent order.
func resolveStdModules(source string) ([]string, error) {
	return stdlib.ResolveModules(source)
}

// findRepoFile searches upward for a repository-relative file.
func findRepoFile(name string) (string, error) {
	return stdlib.FindRepoFile(name)
}
