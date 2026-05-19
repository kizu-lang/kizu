package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/buildcache"
	"github.com/kizu-lang/kizu/internal/cimport"
	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/native"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// main dispatches the kizu command line interface.
func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		printError(err)
		os.Exit(1)
	}
}

// printError writes a stable top-level CLI error prefix.
func printError(err error) {
	msg := err.Error()
	if strings.HasPrefix(msg, "error:") {
		_, _ = fmt.Fprintln(os.Stderr, msg)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "error: "+msg)
}

// dispatch runs one CLI command.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "parse":
		return parseFile(args[0])
	case "run":
		path, programArgs := splitProgramArgs(args)
		return runFile(path, programArgs)
	case "check":
		return checkFile(args[0])
	case "test":
		path, programArgs := splitProgramArgs(args)
		return testFile(path, programArgs)
	case "fmt":
		return fmtFile(args[0])
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

// usage prints the supported command line shape.
func usage() {
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu <parse|run|check|test|fmt> <file> [-- args...]")
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
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
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
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
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
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
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
	var source strings.Builder
	for _, module := range graph.Modules {
		data, err := os.ReadFile(module.File)
		if err != nil {
			return nil, err
		}
		source.Write(data)
		source.WriteByte('\n')
	}
	modules, err := resolveStdModules(source.String())
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		return nil, nil
	}
	decls, errs, err := parseStdDecls(modules)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("parse std failed: %s", errs[0])
	}
	return decls, nil
}

// testFile runs a single Kizu test source and reports a minimal test result.
func testFile(path string, args []string) error {
	if isPackageRoot(path) {
		return testPackage(path, args)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
		return fmt.Errorf("parse failed")
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	if err := interp.NewWithProcessArgs(os.Stdout, args).Run(program); err != nil {
		return err
	}
	_, _ = fmt.Println("test: ok")
	return nil
}

// testPackage resolves a package root and runs its root module as a test.
func testPackage(path string, args []string) error {
	graph, program, err := loadPackageProgram(path)
	if err != nil {
		return err
	}
	if err := checkProgram(program); err != nil {
		return err
	}
	entry := graph.Root + "::main"
	if err := interp.NewWithProcessArgs(os.Stdout, args).RunEntry(program, entry); err != nil {
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

// fmtFile prints the stable formatter output for a Kizu source file.
func fmtFile(path string) error {
	program, errs, err := parsePath(path)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
		return fmt.Errorf("format failed")
	}
	_, _ = fmt.Println(program.String())
	return nil
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
	module, err := lowerFile(options.Path, options.Opt)
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
		for _, msg := range errs {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
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
func parsePath(path string) (*ast.Program, []string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	p := parser.New(lexer.New(string(b)))
	program := p.ParseProgram()
	return program, p.Errors(), nil
}

// parsePathWithStd parses a source file and appends Kizu std wrappers.
func parsePathWithStd(path string) (*ast.Program, []string, error) {
	program, errs, err := parsePath(path)
	if err != nil || len(errs) > 0 {
		return program, errs, err
	}
	modules, err := sourceStdModules(path)
	if err != nil {
		return program, nil, err
	}
	if len(modules) == 0 {
		return program, nil, nil
	}
	stdDecls, stdErrs, err := parseStdDecls(modules)
	if err != nil || len(stdErrs) > 0 {
		return program, stdErrs, err
	}
	program.Decls = append(stdDecls, program.Decls...)
	return program, nil, nil
}

// sourceStdModules reports which Kizu std wrapper modules a source references.
func sourceStdModules(path string) ([]string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return resolveStdModules(string(source))
}

// resolveStdModules returns std modules in dependency-before-dependent order.
func resolveStdModules(source string) ([]string, error) {
	resolver := &stdModuleResolver{
		visited:  map[string]bool{},
		visiting: map[string]bool{},
	}
	for _, module := range referencedStdModules(source) {
		if err := resolver.visit(module); err != nil {
			return nil, err
		}
	}
	return resolver.modules, nil
}

type stdModuleResolver struct {
	modules  []string
	visited  map[string]bool
	visiting map[string]bool
}

// visit adds one std module after recursively adding its std-source dependencies.
func (r *stdModuleResolver) visit(module string) error {
	if r.visited[module] {
		return nil
	}
	if r.visiting[module] {
		return nil
	}
	r.visiting[module] = true
	source, err := readStdModuleSource(module)
	if err != nil {
		return err
	}
	for _, dependency := range referencedStdModules(source) {
		if err := r.visit(dependency); err != nil {
			return err
		}
	}
	r.visiting[module] = false
	r.visited[module] = true
	r.modules = append(r.modules, module)
	return nil
}

// readStdModuleSource reads one std source module by its short module name.
func readStdModuleSource(module string) (string, error) {
	path, err := findRepoFile(stdModuleFile(module))
	if err != nil {
		return "", err
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(source), nil
}

// referencedStdModules scans source text for std module namespace references.
func referencedStdModules(source string) []string {
	modules := []string{}
	for _, module := range stdSourceModuleOrder {
		if strings.Contains(source, "std::"+module+"::") {
			modules = append(modules, module)
		}
	}
	return modules
}

var stdSourceModuleOrder = []string{
	"mem",
	"array",
	"string",
	"fmt",
	"testing",
	"kizu::ast",
	"kizu::lexer",
	"kizu::parser",
	"fs",
	"path",
	"io",
	"process",
	"map",
	"task",
	"channel",
	"thread",
	"sync",
	"atomic",
}

// parseStdDecls loads selected std wrappers from Kizu source.
func parseStdDecls(modules []string) ([]ast.Decl, []string, error) {
	decls := []ast.Decl{}
	for _, module := range modules {
		moduleDecls, errs, err := parseStdModuleDecls(module)
		if err != nil || len(errs) > 0 {
			return nil, errs, err
		}
		decls = append(decls, moduleDecls...)
	}
	return decls, nil, nil
}

// parseStdModuleDecls loads one std wrapper module from Kizu source.
func parseStdModuleDecls(module string) ([]ast.Decl, []string, error) {
	path, err := findRepoFile(stdModuleFile(module))
	if err != nil {
		return nil, nil, err
	}
	program, errs, err := parsePath(path)
	if err != nil || len(errs) > 0 {
		return nil, errs, err
	}
	decls := make([]ast.Decl, 0, len(program.Decls))
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			renameStdStruct(module, d)
		case *ast.EnumDecl:
			renameStdEnum(module, d)
		case *ast.UnionDecl:
			renameStdUnion(module, d)
		case *ast.FunctionDecl:
			renameStdFunction(module, d)
			d.Std = true
		case *ast.ImplDecl:
			renameStdImpl(module, d)
		default:
			return nil, nil, fmt.Errorf("std %s error: unsupported declaration %T", module, decl)
		}
		decls = append(decls, decl)
	}
	return decls, nil, nil
}

// stdModuleFile maps a std namespace module name to its source file path.
func stdModuleFile(module string) string {
	return "std/src/" + strings.ReplaceAll(module, "::", "/") + ".kizu"
}

// renameStdStruct rewrites a std wrapper struct into its qualified form.
func renameStdStruct(module string, decl *ast.StructDecl) {
	decl.Name = qualifyStdTypeName(module, decl.Name)
	for idx := range decl.Fields {
		decl.Fields[idx].TypeName = qualifyStdTypeName(module, decl.Fields[idx].TypeName)
	}
}

// renameStdEnum rewrites a std wrapper enum into its qualified form.
func renameStdEnum(module string, decl *ast.EnumDecl) {
	decl.Name = qualifyStdTypeName(module, decl.Name)
}

// renameStdUnion rewrites a std wrapper union into its qualified form.
func renameStdUnion(module string, decl *ast.UnionDecl) {
	decl.Name = qualifyStdTypeName(module, decl.Name)
	for idx := range decl.Variants {
		decl.Variants[idx].Payload = qualifyStdTypeName(module, decl.Variants[idx].Payload)
	}
}

// renameStdFunction rewrites a std wrapper function into its qualified form.
func renameStdFunction(module string, fn *ast.FunctionDecl) {
	fn.Name = "std." + strings.ReplaceAll(module, "::", ".") + "." + fn.Name
	fn.Public = false
	renameStdFunctionTypes(module, fn)
	renameStdBlockExprs(module, fn.Body)
}

// renameStdImpl rewrites a std wrapper impl block into its qualified form.
func renameStdImpl(module string, decl *ast.ImplDecl) {
	decl.TypeName = qualifyStdTypeName(module, decl.TypeName)
	for _, method := range decl.Methods {
		method.Public = false
		method.Std = true
		renameStdFunctionTypes(module, method)
		renameStdBlockExprs(module, method.Body)
	}
}

// renameStdFunctionTypes qualifies module-local std type names in one function.
func renameStdFunctionTypes(module string, fn *ast.FunctionDecl) {
	fn.ReturnType = qualifyStdTypeName(module, fn.ReturnType)
	for idx := range fn.Params {
		fn.Params[idx].TypeName = qualifyStdTypeName(module, fn.Params[idx].TypeName)
	}
}

// renameStdBlockExprs qualifies module-local type names inside std function bodies.
func renameStdBlockExprs(module string, block *ast.BlockStmt) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		renameStdStmtExprs(module, stmt)
	}
}

// renameStdStmtExprs qualifies module-local type names inside one statement.
func renameStdStmtExprs(module string, stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		renameStdExpr(module, s.Value)
	case *ast.AssignStmt:
		renameStdExpr(module, s.Target)
		renameStdExpr(module, s.Value)
	case *ast.ReturnStmt:
		renameStdExpr(module, s.Value)
	case *ast.ExprStmt:
		renameStdExpr(module, s.Expr)
	case *ast.IfStmt:
		renameStdExpr(module, s.Condition)
		renameStdBlockExprs(module, s.Consequence)
		renameStdBlockExprs(module, s.Alternative)
	case *ast.WhileStmt:
		renameStdExpr(module, s.Condition)
		renameStdBlockExprs(module, s.Body)
	case *ast.ForStmt:
		renameStdExpr(module, s.Start)
		renameStdExpr(module, s.End)
		renameStdBlockExprs(module, s.Body)
	case *ast.MatchStmt:
		renameStdExpr(module, s.Value)
		for _, arm := range s.Arms {
			renameStdStmtExprs(module, arm.Body)
		}
	case *ast.UnsafeStmt:
		renameStdBlockExprs(module, s.Body)
	case *ast.ComptimeIfStmt:
		renameStdExpr(module, s.Condition)
		renameStdBlockExprs(module, s.Consequence)
		renameStdBlockExprs(module, s.Alternative)
	}
}

// renameStdExpr qualifies module-local struct literal type names recursively.
func renameStdExpr(module string, expr ast.Expression) {
	if renameStdTypeExpr(module, expr) {
		return
	}
	switch e := expr.(type) {
	case *ast.StructLiteralExpr:
		e.TypeName = qualifyStdTypeName(module, e.TypeName)
		for idx := range e.Fields {
			renameStdExpr(module, e.Fields[idx].Value)
		}
	case *ast.PrefixExpr:
		renameStdExpr(module, e.Right)
	case *ast.BinaryExpr:
		renameStdExpr(module, e.Left)
		renameStdExpr(module, e.Right)
	case *ast.ComptimeExpr:
		renameStdExpr(module, e.Expr)
	case *ast.CallExpr:
		renameStdExpr(module, e.Callee)
		for _, arg := range e.Args {
			renameStdExpr(module, arg)
		}
	case *ast.TryExpr:
		renameStdExpr(module, e.Value)
	case *ast.IndexExpr:
		renameStdExpr(module, e.Target)
		renameStdExpr(module, e.Index)
		renameStdExpr(module, e.Start)
		renameStdExpr(module, e.End)
	case *ast.FieldExpr:
		if e.Namespace {
			renameStdNamespaceReceiver(module, e)
			return
		}
		renameStdExpr(module, e.Receiver)
	case *ast.DerefExpr:
		renameStdExpr(module, e.Receiver)
	}
}

// renameStdTypeExpr qualifies type-bearing expression nodes.
func renameStdTypeExpr(module string, expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.TypeApplyExpr:
		renameStdExpr(module, e.Callee)
		e.TypeArg = qualifyStdTypeName(module, e.TypeArg)
	case *ast.CastExpr:
		e.TargetType = qualifyStdTypeName(module, e.TargetType)
		renameStdExpr(module, e.Value)
	case *ast.ArenaNewExpr:
		e.TypeName = qualifyStdTypeName(module, e.TypeName)
	default:
		return false
	}
	return true
}

// renameStdNamespaceReceiver qualifies module-local namespace type receivers.
func renameStdNamespaceReceiver(module string, expr *ast.FieldExpr) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		ident.Name = qualifyStdTypeName(module, ident.Name)
		return
	}
	renameStdExpr(module, expr.Receiver)
}

// qualifyStdTypeName maps local std wrapper type names to public std names.
func qualifyStdTypeName(module string, typ string) string {
	if strings.HasPrefix(typ, "!") {
		return "!" + qualifyStdTypeName(module, strings.TrimPrefix(typ, "!"))
	}
	if base, argsText, ok := splitStdGenericType(typ); ok {
		args, argsOK := splitStdGenericArgs(argsText)
		if !argsOK {
			return typ
		}
		for idx := range args {
			args[idx] = qualifyStdTypeName(module, args[idx])
		}
		return base + "<" + strings.Join(args, ", ") + ">"
	}
	if module == "string" && typ == "String" {
		return "std::string::String"
	}
	if module == "kizu::ast" {
		switch typ {
		case "SourceFile", "Span", "TokenId", "SymbolId", "BinaryOp", "ChildRange",
			"NodeId", "Ast", "AstNode", "AstData", "IntNode", "VarNode", "BinaryNode",
			"CallNode", "BlockNode", "IfNode", "LetNode", "ReturnNode", "FnDeclNode", "ParseResult":
			return "std::kizu::ast::" + typ
		}
	}
	if module == "kizu::lexer" {
		switch typ {
		case "TokenKind", "Token":
			return "std::kizu::lexer::" + typ
		}
	}
	return typ
}

// splitStdGenericType extracts base and raw arguments from a simple type string.
func splitStdGenericType(name string) (string, string, bool) {
	start := strings.Index(name, "<")
	if start < 0 || !strings.HasSuffix(name, ">") {
		return "", "", false
	}
	return name[:start], name[start+1 : len(name)-1], true
}

// splitStdGenericArgs splits top-level generic arguments for std type rewriting.
func splitStdGenericArgs(arg string) ([]string, bool) {
	parts := []string{}
	depth := 0
	start := 0
	for idx, r := range arg {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(arg[start:idx]))
				start = idx + 1
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	parts = append(parts, strings.TrimSpace(arg[start:]))
	return parts, true
}

// findRepoFile searches upward for a repository-relative file.
func findRepoFile(name string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("open %s: no such file or directory", name)
		}
		dir = parent
	}
}
