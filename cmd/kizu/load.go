package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kizu-lang/kizu/internal/manifest"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

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
	manifest, err := manifest.ParseManifest(string(source))
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
	return graph, program, nil
}

// lowerFile parses, checks, lowers, and optionally optimizes source to typed SSA IR.
func lowerFile(path string, opt bool) (*ir.Module, error) {
	program, err := loadFileProgram(path)
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
		if err := ir.Optimize(module); err != nil {
			return nil, err
		}
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
		if err := ir.Optimize(module); err != nil {
			return nil, err
		}
	}
	if err := addPackageMain(module, graph.PackageName+"::main"); err != nil {
		return nil, err
	}
	return module, nil
}

// addPackageMain exposes the root module main as the native entrypoint, so a
// package uses the same entry rule as a single file: `fn main`. It writes IR by
// hand, so like addTestMain it verifies what it produced.
func addPackageMain(module *ir.Module, entry string) error {
	if moduleFunction(module, "main") != nil {
		return nil
	}
	entryFn := moduleFunction(module, entry)
	if entryFn == nil || len(entryFn.Params) != 0 {
		return nil
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
	return ir.Verify(module)
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
	p := parser.New(lexer.NewFile(path, string(b)))
	program := p.ParseProgram()
	return program, p.Diagnostics(), nil
}

// loadFileProgram resolves a source file together with the std it imports. The
// resolving is the same pass a package module gets, so a name means the same
// thing in a loose file as it does inside a package.
func loadFileProgram(path string) (*ast.Program, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return project.LoadSource(path, string(source))
}
