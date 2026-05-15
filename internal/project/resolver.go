package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// Package is a parsed package with resolved module imports.
type Package struct {
	BaseDir  string
	Manifest Manifest
	Graph    Graph
	Modules  []ParsedModule
}

// ParsedModule is one parsed source file and its import edges.
type ParsedModule struct {
	Module  Module
	Program *ast.Program
	Imports []ResolvedImport
}

// ResolvedImport is one explicit import edge in the module graph.
type ResolvedImport struct {
	Path string
	Name string
	File string
}

// LoadPackage reads kizu.toml from baseDir and resolves all package modules.
func LoadPackage(baseDir string) (*Package, error) {
	source, err := os.ReadFile(filepath.Join(baseDir, "kizu.toml"))
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return nil, err
	}
	return ResolvePackage(baseDir, manifest)
}

// ResolvePackage resolves modules, parses source files, and validates imports.
func ResolvePackage(baseDir string, manifest Manifest) (*Package, error) {
	graph, err := ResolveModules(baseDir, manifest)
	if err != nil {
		return nil, err
	}
	modules, err := parseModules(graph)
	if err != nil {
		return nil, err
	}
	if err := resolveImports(modules); err != nil {
		return nil, err
	}
	pkg := &Package{BaseDir: baseDir, Manifest: manifest, Graph: graph, Modules: modules}
	if err := CheckVisibility(pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

// parseModules parses every source file in graph order.
func parseModules(graph Graph) ([]ParsedModule, error) {
	modules := make([]ParsedModule, 0, len(graph.Modules))
	for _, module := range graph.Modules {
		program, err := parseModule(module)
		if err != nil {
			return nil, err
		}
		modules = append(modules, ParsedModule{Module: module, Program: program})
	}
	return modules, nil
}

// parseModule reads and parses one module source file.
func parseModule(module Module) (*ast.Program, error) {
	source, err := os.ReadFile(module.File)
	if err != nil {
		return nil, err
	}
	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("module error: parse %s: %s",
			module.Path, strings.Join(p.Errors(), "; "))
	}
	return program, nil
}

// resolveImports validates import targets, import names, and import cycles.
func resolveImports(modules []ParsedModule) error {
	byPath := moduleIndex(modules)
	for idx := range modules {
		imports, err := importsForModule(modules[idx], byPath)
		if err != nil {
			return err
		}
		modules[idx].Imports = imports
	}
	return rejectImportCycles(modules)
}

// moduleIndex maps module paths to parsed modules.
func moduleIndex(modules []ParsedModule) map[string]ParsedModule {
	byPath := make(map[string]ParsedModule, len(modules))
	for _, module := range modules {
		byPath[module.Module.Path] = module
	}
	return byPath
}

// importsForModule resolves and validates one module's import declarations.
func importsForModule(
	module ParsedModule,
	byPath map[string]ParsedModule,
) ([]ResolvedImport, error) {
	names := map[string]string{}
	imports := []ResolvedImport{}
	for _, decl := range module.Program.Decls {
		importDecl, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		resolved, err := resolveImport(module.Module.Path, importDecl, byPath)
		if err != nil {
			return nil, err
		}
		if previous, exists := names[resolved.Name]; exists {
			return nil, fmt.Errorf("module error: `%s` imports `%s` and `%s` as `%s`",
				module.Module.Path, previous, resolved.Path, resolved.Name)
		}
		if topLevelDeclNameShadows(module.Program, resolved.Name) {
			return nil, fmt.Errorf("module error: `%s` declares `%s` that shadows an import",
				module.Module.Path, resolved.Name)
		}
		names[resolved.Name] = resolved.Path
		imports = append(imports, resolved)
	}
	return imports, nil
}

// resolveImport maps one import declaration to a known module.
func resolveImport(
	from string,
	decl *ast.ImportDecl,
	byPath map[string]ParsedModule,
) (ResolvedImport, error) {
	if len(decl.Path) == 0 {
		return ResolvedImport{}, fmt.Errorf("module error: `%s` has empty import", from)
	}
	path := strings.Join(decl.Path, "::")
	module, ok := byPath[path]
	if !ok {
		return ResolvedImport{}, fmt.Errorf("module error: `%s` imports missing module `%s`",
			from, path)
	}
	return ResolvedImport{
		Path: path,
		Name: decl.Path[len(decl.Path)-1],
		File: module.Module.File,
	}, nil
}

// topLevelDeclNameShadows reports whether a declaration shadows an import name.
func topLevelDeclNameShadows(program *ast.Program, name string) bool {
	for _, decl := range program.Decls {
		if declName(decl) == name {
			return true
		}
	}
	return false
}

// declName returns the top-level declaration name if the node has one.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return d.Name
	case *ast.StructDecl:
		return d.Name
	case *ast.EnumDecl:
		return d.Name
	case *ast.UnionDecl:
		return d.Name
	case *ast.ContractDecl:
		return d.Name
	case *ast.ImplDecl, *ast.SatisfyDecl, *ast.ImportDecl:
		return ""
	default:
		return ""
	}
}

// rejectImportCycles rejects cycles in the resolved import graph.
func rejectImportCycles(modules []ParsedModule) error {
	edges := importEdges(modules)
	state := map[string]int{}
	for _, module := range modules {
		if err := visitModule(module.Module.Path, edges, state, nil); err != nil {
			return err
		}
	}
	return nil
}

// importEdges returns an adjacency map from resolved module imports.
func importEdges(modules []ParsedModule) map[string][]string {
	edges := map[string][]string{}
	for _, module := range modules {
		for _, imported := range module.Imports {
			edges[module.Module.Path] = append(edges[module.Module.Path], imported.Path)
		}
	}
	return edges
}

// visitModule performs DFS cycle detection on one module path.
func visitModule(
	path string,
	edges map[string][]string,
	state map[string]int,
	stack []string,
) error {
	if state[path] == 2 {
		return nil
	}
	if state[path] == 1 {
		return fmt.Errorf("module error: cyclic import: %s", cyclePath(stack, path))
	}
	state[path] = 1
	stack = append(stack, path)
	for _, next := range edges[path] {
		if err := visitModule(next, edges, state, stack); err != nil {
			return err
		}
	}
	state[path] = 2
	return nil
}

// cyclePath formats the detected cycle path for diagnostics.
func cyclePath(stack []string, repeated string) string {
	for idx, path := range stack {
		if path == repeated {
			return strings.Join(append(stack[idx:], repeated), " -> ")
		}
	}
	return strings.Join(append(stack, repeated), " -> ")
}
