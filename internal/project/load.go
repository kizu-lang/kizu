package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/types"
)

// LoadProgram parses every module in graph and returns a qualified package program.
func LoadProgram(graph Graph) (*ast.Program, error) {
	return LoadProgramWithSources(graph, nil)
}

// LoadProgramWithSources parses graph using source overrides keyed by module file path.
func LoadProgramWithSources(graph Graph, sources map[string]string) (*ast.Program, error) {
	checker := &graphChecker{
		modules:         map[string]*moduleUnit{},
		modulePaths:     map[string]bool{},
		types:           map[string]typeExport{},
		functions:       map[string]functionExport{},
		sourceOverrides: cleanSourceOverrides(sources),
	}
	if err := checker.load(graph); err != nil {
		return nil, err
	}
	if err := checker.collectTypes(); err != nil {
		return nil, err
	}
	if err := checker.collectFunctions(); err != nil {
		return nil, err
	}
	return checker.program()
}

// LoadSource resolves one program that is not part of a package. It is the same
// work a package module gets -- imports bound, names resolved through them --
// on a graph of one module with no path to qualify by. A single file and a
// package module read the same, so they are read by the same code.
func LoadSource(file string, source string) (*ast.Program, error) {
	graph := Graph{Modules: []Module{{Path: "", File: file}}}
	return LoadProgramWithSources(graph, map[string]string{file: source})
}

// CheckGraph parses and type-checks every module in graph as one package.
func CheckGraph(graph Graph) error {
	program, err := LoadProgram(graph)
	if err != nil {
		return err
	}
	return types.New().Check(program)
}

type graphChecker struct {
	packageRoot     string
	modules         map[string]*moduleUnit
	modulePaths     map[string]bool
	types           map[string]typeExport
	functions       map[string]functionExport
	sourceOverrides map[string]string
}

type moduleUnit struct {
	path    string
	file    string
	program *ast.Program
	// imports is the declared import set and defines the dependency edges the
	// ordering and cycle checks walk.
	imports map[string]string
	// namespaces is what name resolution sees: the imports plus the package root
	// namespace, which is reachable without an import and is not an edge.
	namespaces map[string]string
}

// name is what a diagnostic calls this module. A program that is not part of a
// package has no module path, so it is named by the file it was read from.
func (m *moduleUnit) name() string {
	if m.path == "" {
		return m.file
	}
	return m.path
}

// qualify returns what a name declared in this module is filed under. A module
// with no path is a program that is not part of a package: there is nothing to
// qualify it by, and its declarations keep the names they are written with.
func (m *moduleUnit) qualify(name string) string {
	if m.path == "" {
		return name
	}
	return m.path + "::" + name
}

// moduleNamespaces returns the namespaces module can name. ADR-0049 makes
// [package].name the package root namespace, so every other module reaches the
// root module's declarations by that name -- a module cannot import the package
// it is part of, and treating the root as an import edge would make every
// package with a non-empty root a cycle.
func (c *graphChecker) moduleNamespaces(
	module *moduleUnit,
	imports map[string]string,
	std map[string]string,
) map[string]string {
	namespaces := make(map[string]string, len(imports)+len(std)+1)
	for alias, path := range imports {
		namespaces[alias] = path
	}
	for alias, path := range std {
		namespaces[alias] = path
	}
	if c.packageRoot != "" && c.modulePaths[c.packageRoot] && module.path != c.packageRoot {
		if _, taken := namespaces[c.packageRoot]; !taken {
			namespaces[c.packageRoot] = c.packageRoot
		}
	}
	return namespaces
}

type typeExport struct {
	module string
	public bool
}

type functionExport struct {
	module string
	public bool
}

// load parses every source file and indexes module paths.
func (c *graphChecker) load(graph Graph) error {
	c.packageRoot = graph.Root
	for _, module := range graph.Modules {
		program, err := c.parseModule(module)
		if err != nil {
			return err
		}
		c.modules[module.Path] = &moduleUnit{path: module.Path, file: module.File, program: program}
		c.modulePaths[module.Path] = true
	}
	return nil
}

// parseModule parses one graph module from an override or its source file.
func (c *graphChecker) parseModule(module Module) (*ast.Program, error) {
	if source, ok := c.sourceOverrides[filepath.Clean(module.File)]; ok {
		return parseModuleSource(module.File, source)
	}
	return parseModuleFile(module)
}

// parseModuleFile parses one graph module source.
func parseModuleFile(module Module) (*ast.Program, error) {
	source, err := os.ReadFile(module.File)
	if err != nil {
		return nil, err
	}
	return parseModuleSource(module.File, string(source))
}

// parseModuleSource parses one graph module source string.
func parseModuleSource(file string, source string) (*ast.Program, error) {
	p := parser.New(lexer.NewFile(file, source))
	program := p.ParseProgram()
	if diagnostics := p.Diagnostics(); len(diagnostics) > 0 {
		return nil, &diagnostics[0]
	}
	return program, nil
}

// cleanSourceOverrides normalizes source override file paths for graph lookups.
func cleanSourceOverrides(sources map[string]string) map[string]string {
	if len(sources) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(sources))
	for path, source := range sources {
		cleaned[filepath.Clean(path)] = source
	}
	return cleaned
}

// collectTypes indexes user-declared module types before resolving references.
func (c *graphChecker) collectTypes() error {
	for _, module := range c.modules {
		for _, decl := range module.program.Decls {
			name, public, ok := declaredType(decl)
			if !ok {
				continue
			}
			qualified := module.qualify(name)
			if _, exists := c.types[qualified]; exists {
				return fmt.Errorf("module error: duplicate type `%s`", qualified)
			}
			c.types[qualified] = typeExport{module: module.path, public: public}
		}
	}
	return nil
}

// collectFunctions indexes user-declared module functions before resolving calls.
func (c *graphChecker) collectFunctions() error {
	for _, module := range c.modules {
		for _, decl := range module.program.Decls {
			fn, ok := decl.(*ast.FunctionDecl)
			if !ok {
				continue
			}
			qualified := module.qualify(fn.Name)
			if _, exists := c.functions[qualified]; exists {
				return fmt.Errorf("module error: duplicate function `%s`", qualified)
			}
			c.functions[qualified] = functionExport{module: module.path, public: fn.Public}
		}
	}
	return nil
}

// declaredType returns the top-level type name declared by decl.
func declaredType(decl ast.Decl) (string, bool, bool) {
	switch d := decl.(type) {
	case *ast.StructDecl:
		return d.Name, d.Public, true
	case *ast.EnumDecl:
		return d.Name, d.Public, true
	case *ast.ErrorSetDecl:
		return d.Name, d.Public, true
	case *ast.UnionDecl:
		return d.Name, d.Public, true
	case *ast.ContractDecl:
		return d.Name, d.Public, true
	default:
		return "", false, false
	}
}

// program validates imports and returns package-qualified declarations.
func (c *graphChecker) program() (*ast.Program, error) {
	merged := &ast.Program{}
	for _, module := range sortedModuleUnits(c.modules) {
		imports, std, err := c.resolveImports(module)
		if err != nil {
			return nil, err
		}
		module.imports = imports
		module.namespaces = c.moduleNamespaces(module, imports, std)
	}
	modules, err := c.orderedModules()
	if err != nil {
		return nil, err
	}
	for _, module := range modules {
		qualified, err := c.qualifyModule(module)
		if err != nil {
			return nil, err
		}
		merged.Decls = append(merged.Decls, qualified.Decls...)
	}
	return merged, nil
}

// orderedModules returns dependency modules before modules that import them.
func (c *graphChecker) orderedModules() ([]*moduleUnit, error) {
	out := []*moduleUnit{}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	for _, module := range sortedModuleUnits(c.modules) {
		if err := c.visitModule(module, visiting, visited, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// visitModule performs a deterministic DFS over explicit module imports.
func (c *graphChecker) visitModule(
	module *moduleUnit,
	visiting map[string]bool,
	visited map[string]bool,
	out *[]*moduleUnit,
) error {
	if visited[module.path] {
		return nil
	}
	if visiting[module.path] {
		return fmt.Errorf("module error: import cycle at `%s`", module.path)
	}
	visiting[module.path] = true
	for _, path := range sortedImportPaths(module.imports) {
		if err := c.visitModule(c.modules[path], visiting, visited, out); err != nil {
			return err
		}
	}
	visiting[module.path] = false
	visited[module.path] = true
	*out = append(*out, module)
	return nil
}

// resolveImports validates imports and returns last-segment aliases. An import
// of std binds a name without becoming an edge in this package's module graph:
// std is a different package, so it is not part of the ordering this graph
// decides, and it can never take part in a cycle within it.
func (c *graphChecker) resolveImports(
	module *moduleUnit,
) (map[string]string, map[string]string, error) {
	imports := map[string]string{}
	std := map[string]string{}
	for _, decl := range module.program.Decls {
		importDecl, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		path := strings.Join(importDecl.Path, "::")
		alias := importDecl.Path[len(importDecl.Path)-1]
		if _, taken := imports[alias]; taken {
			return nil, nil, duplicateImport(alias, module)
		}
		if _, taken := std[alias]; taken {
			return nil, nil, duplicateImport(alias, module)
		}
		bound, err := c.bindImport(module, path)
		if err != nil {
			return nil, nil, err
		}
		if bound == stdBinding {
			std[alias] = path
			continue
		}
		imports[alias] = path
	}
	if err := rejectImportShadowing(module, imports); err != nil {
		return nil, nil, err
	}
	return imports, std, rejectImportShadowing(module, std)
}

// importKind separates an import that is an edge in this graph from one that
// only brings a name in from another package.
type importKind int

const (
	packageBinding importKind = iota + 1
	stdBinding
)

// bindImport reports which package an import names, and rejects one that names
// nothing. An import that resolves to neither this package nor std has no
// module behind it, so the name it would bind would stand for nothing.
func (c *graphChecker) bindImport(module *moduleUnit, path string) (importKind, error) {
	if c.modulePaths[path] {
		return packageBinding, nil
	}
	if _, ok, err := stdlib.Importable(path); err != nil {
		return 0, err
	} else if ok {
		return stdBinding, nil
	}
	return 0, fmt.Errorf("module error: missing import `%s` in `%s`", path, module.path)
}

// duplicateImport reports two imports competing for one name.
func duplicateImport(alias string, module *moduleUnit) error {
	return fmt.Errorf("module error: duplicate import alias `%s` in `%s`", alias, module.path)
}

// rejectImportShadowing checks local declarations do not shadow import aliases.
func rejectImportShadowing(module *moduleUnit, imports map[string]string) error {
	for _, decl := range module.program.Decls {
		name, ok := declaredTopLevelName(decl)
		if !ok {
			continue
		}
		if _, exists := imports[name]; exists {
			return fmt.Errorf("module error: declaration `%s` shadows import in `%s`", name, module.path)
		}
	}
	return nil
}

// declaredTopLevelName returns the local namespace name introduced by decl.
func declaredTopLevelName(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return d.Name, true
	case *ast.StructDecl:
		return d.Name, true
	case *ast.EnumDecl:
		return d.Name, true
	case *ast.ErrorSetDecl:
		return d.Name, true
	case *ast.UnionDecl:
		return d.Name, true
	case *ast.ContractDecl:
		return d.Name, true
	default:
		return "", false
	}
}
