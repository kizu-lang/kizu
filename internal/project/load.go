package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/source"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/typ"
)

// LoadProgram parses every module in graph and returns a qualified package program.
func LoadProgram(graph Graph) (*ast.Program, error) {
	return loadProgramWithSources(graph, nil, false)
}

// LoadProgramWithSources parses graph using source overrides keyed by module file path.
//
// std is loaded here too, as the package it is. Every frontend that reads a Kizu
// program comes through this, so a program means the same thing whichever one
// read it, and std needs no loader of its own.
func LoadProgramWithSources(graph Graph, sources map[string]string) (*ast.Program, error) {
	return loadProgramWithSources(graph, sources, false)
}

// LoadTestProgram parses production and test files into one qualified package program.
func LoadTestProgram(graph Graph) (*ast.Program, error) {
	return loadProgramWithSources(graph, nil, true)
}

// LoadTestProgramWithSources parses a test graph with source overrides by file path.
func LoadTestProgramWithSources(graph Graph, sources map[string]string) (*ast.Program, error) {
	return loadProgramWithSources(graph, sources, true)
}

// loadProgramWithSources parses the selected package view and its reachable std modules.
func loadProgramWithSources(
	graph Graph,
	sources map[string]string,
	includeTests bool,
) (*ast.Program, error) {
	checker := &graphChecker{
		modules:         map[string]*moduleGroup{},
		packages:        map[string]bool{},
		types:           map[string]typeExport{},
		functions:       map[string]functionExport{},
		parsedTypes:     typ.NewTable(),
		sourceOverrides: cleanSourceOverrides(sources),
		sources:         source.NewMap(),
	}
	if err := checker.loadPackage(graph, includeTests); err != nil {
		return nil, err
	}
	if err := checker.loadStd(); err != nil {
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
	graph := Graph{Modules: []Module{{Path: "", Files: []string{file}}}}
	return LoadProgramWithSources(graph, map[string]string{file: source})
}

type graphChecker struct {
	modules map[string]*moduleGroup
	// packages names every package in this program, so an import of a package
	// root can be told from one that names nothing.
	packages map[string]bool
	// order is the order declarations are handed on in. A module comes after
	// what it is built on, and std comes before the package that imports it.
	order           []string
	types           map[string]typeExport
	functions       map[string]functionExport
	parsedTypes     *typ.Table
	sourceOverrides map[string]string
	// sources owns each input once while syntax records carry IDs.
	sources *source.Map
}

// moduleGroup owns the files that share one directory-derived module namespace.
type moduleGroup struct {
	path  string
	files []*moduleFile
	// imports is the union of file-local imports used for dependency ordering.
	imports map[string]string
}

// moduleFile is one source file and its file-local import scope.
type moduleFile struct {
	// pkg is the package this file's module belongs to, and the namespace its
	// siblings reach it through.
	pkg     string
	path    string
	file    string
	program *ast.Program
	// imports is the declared import set and defines the dependency edges the
	// ordering and cycle checks walk.
	imports map[string]string
	// namespaces is what name resolution sees: the imports plus the package root
	// namespace, which is reachable without an import and is not an edge.
	namespaces map[string]string
	// used records the namespaces resolution actually went through, so an import
	// that nothing needed can be told from one that carried a name.
	used map[string]bool
}

// use records that a name was reached through one of this module's namespaces.
func (m *moduleFile) use(name string) {
	if m.used == nil {
		m.used = map[string]bool{}
	}
	m.used[name] = true
}

// name is what a diagnostic calls this module. A program that is not part of a
// package has no module path, so it is named by the file it was read from.
func (m *moduleFile) name() string {
	if m.path == "" {
		return m.file
	}
	return m.path
}

// qualify returns what a name declared in this module is filed under. A module
// with no path is a program that is not part of a package: there is nothing to
// qualify it by, and its declarations keep the names they are written with.
func (m *moduleFile) qualify(name string) string {
	if m.path == "" {
		return name
	}
	return m.path + "::" + name
}

// moduleNamespaces returns the namespaces a file can name. ADR-0049 makes
// [package].name an implicit package namespace in every module, including the
// root module. It is not an import edge: an explicit import still records its
// own dependency, while an implicit fully qualified reference does not make
// every package a cycle.
func (c *graphChecker) moduleNamespaces(
	module *moduleFile,
	imports map[string]string,
) map[string]string {
	namespaces := make(map[string]string, len(imports)+1)
	for alias, path := range imports {
		namespaces[alias] = path
	}
	if module.pkg != "" {
		if _, taken := namespaces[module.pkg]; !taken {
			namespaces[module.pkg] = module.pkg
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

// loadPackage parses one package's selected module files and records their groups.
func (c *graphChecker) loadPackage(graph Graph, includeTests bool) error {
	if graph.PackageName != "" {
		c.packages[graph.PackageName] = true
	}
	for _, module := range graph.Modules {
		files := module.SourceFiles(includeTests)
		if len(files) == 0 {
			continue
		}
		group := &moduleGroup{
			path:    module.Path,
			imports: map[string]string{},
		}
		for _, file := range files {
			program, err := c.parseModuleFile(file)
			if err != nil {
				return err
			}
			group.files = append(group.files, &moduleFile{
				pkg:     graph.PackageName,
				path:    module.Path,
				file:    file,
				program: program,
			})
		}
		c.modules[module.Path] = group
		c.order = append(c.order, module.Path)
	}
	return nil
}

// loadStd parses the std modules this program imports, and puts them before it.
// What a program reaches is what its files declare they import, so a file that
// imports nothing from std reads no std source at all.
func (c *graphChecker) loadStd() error {
	graph, err := StdGraph()
	if err != nil {
		return err
	}
	modules, sources, err := stdModulesFor(graph, c.stdImportPaths())
	if err != nil {
		return err
	}
	// The walk that decided which modules to load already read them, so the
	// parse takes that text rather than opening the same files again.
	c.addSourceOverrides(sources)
	before := c.order
	c.order = nil
	std := Graph{PackageName: graph.PackageName, Modules: modules}
	if err := c.loadPackage(std, false); err != nil {
		return err
	}
	c.order = append(c.order, before...)
	return nil
}

// stdImportPaths returns the std paths this program's modules declare they
// import, in a stable order.
func (c *graphChecker) stdImportPaths() []string {
	// `print` is spelled in std::fmt (SPEC §14.1), so every program reaches
	// that module whether or not it imports it.
	seen := map[string]bool{stdlib.PrintModule: true}
	paths := []string{stdlib.PrintModule}
	for _, group := range sortedModuleGroups(c.modules) {
		for _, module := range group.files {
			for _, decl := range module.program.Decls {
				importDecl, ok := decl.(*ast.ImportDecl)
				if !ok || importDecl.Path[0] != stdlib.Root {
					continue
				}
				path := strings.Join(importDecl.Path, "::")
				if !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

// parseModuleFile parses one module source file from an override or disk.
func (c *graphChecker) parseModuleFile(file string) (*ast.Program, error) {
	if source, ok := c.sourceOverrides[filepath.Clean(file)]; ok {
		return c.parseModuleSource(file, source)
	}
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return c.parseModuleSource(file, string(source))
}

// parseModuleSource parses one graph module source string.
func (c *graphChecker) parseModuleSource(file string, text string) (*ast.Program, error) {
	input := c.sources.Add(file, text)
	p := parser.New(lexer.NewSource(input))
	program := p.ParseProgram()
	if diagnostics := p.Diagnostics(); len(diagnostics) > 0 {
		return nil, &diagnostics[0]
	}
	return program, nil
}

// addSourceOverrides records source text a caller already read, keyed the way
// parseModule looks it up.
func (c *graphChecker) addSourceOverrides(sources map[string]string) {
	if c.sourceOverrides == nil {
		c.sourceOverrides = make(map[string]string, len(sources))
	}
	for path, source := range sources {
		c.sourceOverrides[filepath.Clean(path)] = source
	}
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
	for _, group := range sortedModuleGroups(c.modules) {
		for _, module := range group.files {
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
	}
	return nil
}

// collectFunctions indexes user-declared module functions before resolving calls.
func (c *graphChecker) collectFunctions() error {
	for _, group := range sortedModuleGroups(c.modules) {
		for _, module := range group.files {
			for _, decl := range module.program.Decls {
				fn, ok := decl.(*ast.FunctionDecl)
				if !ok || fn.Receiver {
					// A method is filed under its receiver's type, not in this
					// module, so it is not a name a path can reach.
					continue
				}
				qualified := module.qualify(fn.Name)
				if _, exists := c.functions[qualified]; exists {
					return fmt.Errorf("module error: duplicate function `%s`", qualified)
				}
				c.functions[qualified] = functionExport{module: module.path, public: fn.Public}
			}
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
	for _, group := range sortedModuleGroups(c.modules) {
		for _, module := range group.files {
			imports, err := c.resolveImports(group, module)
			if err != nil {
				return nil, err
			}
			module.imports = imports
			module.namespaces = c.moduleNamespaces(module, imports)
			for _, path := range imports {
				group.imports[path] = path
			}
		}
	}
	modules, err := c.orderedModules()
	if err != nil {
		return nil, err
	}
	for _, group := range modules {
		for _, module := range group.files {
			qualified, err := c.qualifyModule(module)
			if err != nil {
				return nil, err
			}
			if err := module.rejectUnusedImports(); err != nil {
				return nil, err
			}
			merged.Decls = append(merged.Decls, qualified.Decls...)
		}
	}
	// Owner-ness is a whole-program question -- holding an owner makes a type
	// one -- so the derived cleanups are filled in once the modules are merged,
	// before any phase reads the program.
	ast.AddDerivedDeinits(merged)
	return merged, nil
}

// modulesInLoadOrder returns modules in the order they were loaded: std before
// the package that imports it, and inside each package a module after the ones
// it is built on.
func (c *graphChecker) modulesInLoadOrder() []*moduleGroup {
	out := make([]*moduleGroup, 0, len(c.order))
	for _, path := range c.order {
		out = append(out, c.modules[path])
	}
	return out
}

// orderedModules returns dependency modules before modules that import them.
func (c *graphChecker) orderedModules() ([]*moduleGroup, error) {
	out := []*moduleGroup{}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	for _, module := range c.modulesInLoadOrder() {
		if err := c.visitModule(module, visiting, visited, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// visitModule performs a deterministic DFS over explicit module imports.
func (c *graphChecker) visitModule(
	module *moduleGroup,
	visiting map[string]bool,
	visited map[string]bool,
	out *[]*moduleGroup,
) error {
	if visited[module.path] {
		return nil
	}
	if visiting[module.path] {
		return fmt.Errorf("module error: import cycle at `%s`", module.path)
	}
	visiting[module.path] = true
	for _, path := range sortedImportPaths(module.imports) {
		imported, ok := c.modules[path]
		if !ok {
			// A package root binds a namespace without naming one module, so
			// there is no single dependency to order this one after.
			continue
		}
		if err := c.visitModule(imported, visiting, visited, out); err != nil {
			return err
		}
	}
	visiting[module.path] = false
	visited[module.path] = true
	*out = append(*out, module)
	return nil
}

// resolveImports validates imports and returns last-segment aliases.
func (c *graphChecker) resolveImports(
	group *moduleGroup,
	module *moduleFile,
) (map[string]string, error) {
	imports := map[string]string{}
	for _, decl := range module.program.Decls {
		importDecl, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		path := strings.Join(importDecl.Path, "::")
		alias := importDecl.Path[len(importDecl.Path)-1]
		if _, taken := imports[alias]; taken {
			return nil, duplicateImport(alias, module)
		}
		if err := c.bindImport(module, path); err != nil {
			return nil, err
		}
		imports[alias] = path
	}
	return imports, rejectImportShadowing(group, module, imports)
}

// bindImport rejects an import that names nothing or names something the module
// may not reach. An import that resolves to no module and no package has
// nothing behind it, so the name it would bind would stand for nothing.
func (c *graphChecker) bindImport(module *moduleFile, path string) error {
	if _, ok := c.modules[path]; !ok && !c.packages[path] {
		return fmt.Errorf("module error: missing import `%s` in `%s`", path, module.name())
	}
	if !ReachableFrom(path, module.path) {
		return internalModuleError(path, module)
	}
	return nil
}

// duplicateImport reports two imports competing for one name.
func duplicateImport(alias string, module *moduleFile) error {
	return fmt.Errorf("module error: duplicate import alias `%s` in `%s`", alias, module.path)
}

// rejectUnusedImports refuses an import nothing was reached through. Once names
// are abbreviated, the import list is where a reader looks to find out what a
// name stands for, and a line that stands for nothing sends them somewhere the
// program never goes. It runs after resolution, so what counts as used is what
// resolution actually went through rather than a second reading of the source.
func (m *moduleFile) rejectUnusedImports() error {
	for _, alias := range sortedAliases(m.imports) {
		if m.used[alias] {
			continue
		}
		return fmt.Errorf("module error: unused import `%s` in `%s`", m.namespaces[alias], m.name())
	}
	return nil
}

// sortedAliases lists the names a module's imports bind, in a stable order so
// that a file with two unused imports always reports the same one first.
func sortedAliases(imports map[string]string) []string {
	aliases := make([]string, 0, len(imports))
	for alias := range imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

// rejectImportShadowing checks local declarations do not shadow import aliases.
func rejectImportShadowing(
	group *moduleGroup,
	module *moduleFile,
	imports map[string]string,
) error {
	for _, file := range group.files {
		for _, decl := range file.program.Decls {
			name, ok := declaredTopLevelName(decl)
			if !ok {
				continue
			}
			if _, exists := imports[name]; exists {
				return fmt.Errorf("module error: declaration `%s` shadows import in `%s`",
					name, module.path)
			}
		}
	}
	return nil
}

// declaredTopLevelName returns the local namespace name introduced by decl.
func declaredTopLevelName(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		// A method's name lives under its receiver's type, so it is not a name
		// in this module and shadows nothing.
		return d.Name, !d.Receiver
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
