package main

import (
	"fmt"
	"os"

	"tiny-safe/internal/ast"
	"tiny-safe/internal/buildcache"
	"tiny-safe/internal/cimport"
	"tiny-safe/internal/interp"
	"tiny-safe/internal/ir"
	"tiny-safe/internal/lexer"
	"tiny-safe/internal/llvm"
	"tiny-safe/internal/ownership"
	"tiny-safe/internal/parser"
	"tiny-safe/internal/types"
	"tiny-safe/internal/wasm"
)

// main dispatches the kizu command line interface.
func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dispatch runs one CLI command.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "parse":
		return parseFile(args[0])
	case "run":
		return runFile(args[0])
	case "check":
		return checkFile(args[0])
	case "ir":
		return irFile(args[0])
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
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu <parse|run|check|ir> <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --emit-llvm <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target wasm32-wasi <file>")
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
func runFile(path string) error {
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
	if err := checkProgram(program); err != nil {
		return err
	}
	return interp.New(os.Stdout).Run(program)
}

// checkFile parses a source file and runs static checks.
func checkFile(path string) error {
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
	if err := checkProgram(program); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}

// irFile parses, checks, lowers, and dumps typed SSA IR.
func irFile(path string) error {
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
	if err := checkProgram(program); err != nil {
		return err
	}
	module, err := ir.Lower(program)
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
		if len(args) != 2 {
			usage()
			return fmt.Errorf("invalid build command")
		}
		return emitLLVMFile(args[1])
	case "--target":
		return emitTargetFile(args[1], args)
	default:
		usage()
		return fmt.Errorf("invalid build command")
	}
}

// emitLLVMFile lowers a checked source file to LLVM IR text.
func emitLLVMFile(path string) error {
	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	result, err := cache.GetOrBuild(path, "emit-llvm", func() (string, error) {
		module, err := lowerFile(path)
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
	if len(args) != 3 || args[1] != "wasm32-wasi" {
		usage()
		return fmt.Errorf("invalid build target `%s`", target)
	}
	return emitWASMFile(args[2])
}

// emitWASMFile lowers a checked source file to WASI WebAssembly text.
func emitWASMFile(path string) error {
	cache, err := buildcache.New()
	if err != nil {
		return err
	}
	result, err := cache.GetOrBuild(path, "wasm32-wasi", func() (string, error) {
		module, err := lowerFile(path)
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

// lowerFile parses, checks, and lowers source to typed SSA IR.
func lowerFile(path string) (*ir.Module, error) {
	program, errs, err := parsePath(path)
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
	return ir.Lower(program)
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
