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
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
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

// checkFile parses a source file and runs static checks.
func checkFile(path string) error {
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

// testFile runs a single Kizu test source and reports a minimal test result.
func testFile(path string, args []string) error {
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
	usesStdPath, err := sourceUsesStdPath(path)
	if err != nil {
		return program, nil, err
	}
	if !usesStdPath {
		return program, nil, nil
	}
	stdDecls, stdErrs, err := parseStdPathDecls()
	if err != nil || len(stdErrs) > 0 {
		return program, stdErrs, err
	}
	program.Decls = append(stdDecls, program.Decls...)
	return program, nil, nil
}

// sourceUsesStdPath reports whether a source file references std::path wrappers.
func sourceUsesStdPath(path string) (bool, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(source), "std::path::"), nil
}

// parseStdPathDecls loads std::path wrappers from Kizu source.
func parseStdPathDecls() ([]ast.Decl, []string, error) {
	path, err := findRepoFile("std/src/path.kizu")
	if err != nil {
		return nil, nil, err
	}
	program, errs, err := parsePath(path)
	if err != nil || len(errs) > 0 {
		return nil, errs, err
	}
	decls := make([]ast.Decl, 0, len(program.Decls))
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			return nil, nil, fmt.Errorf("std path error: unsupported declaration %T", decl)
		}
		fn.Name = "std.path." + fn.Name
		fn.Public = false
		decls = append(decls, fn)
	}
	return decls, nil, nil
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
