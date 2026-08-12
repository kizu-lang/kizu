package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
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
	program *ast.Program
	// imports is the declared import set and defines the dependency edges the
	// ordering and cycle checks walk.
	imports map[string]string
	// namespaces is what name resolution sees: the imports plus the package root
	// namespace, which is reachable without an import and is not an edge.
	namespaces map[string]string
}

// moduleNamespaces returns the namespaces module can name. ADR-0049 makes
// [package].name the package root namespace, so every other module reaches the
// root module's declarations by that name -- a module cannot import the package
// it is part of, and treating the root as an import edge would make every
// package with a non-empty root a cycle.
func (c *graphChecker) moduleNamespaces(
	module *moduleUnit,
	imports map[string]string,
) map[string]string {
	namespaces := make(map[string]string, len(imports)+1)
	for alias, path := range imports {
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
		c.modules[module.Path] = &moduleUnit{path: module.Path, program: program}
		c.modulePaths[module.Path] = true
	}
	return nil
}

// parseModule parses one graph module from an override or its source file.
func (c *graphChecker) parseModule(module Module) (*ast.Program, error) {
	if source, ok := c.sourceOverrides[filepath.Clean(module.File)]; ok {
		return parseModuleSource(module.Path, source)
	}
	return parseModuleFile(module)
}

// parseModuleFile parses one graph module source.
func parseModuleFile(module Module) (*ast.Program, error) {
	source, err := os.ReadFile(module.File)
	if err != nil {
		return nil, err
	}
	return parseModuleSource(module.Path, string(source))
}

// parseModuleSource parses one graph module source string.
func parseModuleSource(_ string, source string) (*ast.Program, error) {
	p := parser.New(lexer.New(source))
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
			qualified := module.path + "::" + name
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
			qualified := module.path + "::" + fn.Name
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
		imports, err := c.resolveImports(module)
		if err != nil {
			return nil, err
		}
		module.imports = imports
		module.namespaces = c.moduleNamespaces(module, imports)
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

// resolveImports validates imports and returns last-segment aliases.
func (c *graphChecker) resolveImports(module *moduleUnit) (map[string]string, error) {
	imports := map[string]string{}
	for _, decl := range module.program.Decls {
		importDecl, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		path := strings.Join(importDecl.Path, "::")
		if !c.modulePaths[path] {
			return nil, fmt.Errorf("module error: missing import `%s` in `%s`", path, module.path)
		}
		alias := importDecl.Path[len(importDecl.Path)-1]
		if _, exists := imports[alias]; exists {
			return nil, fmt.Errorf("module error: duplicate import alias `%s` in `%s`", alias, module.path)
		}
		imports[alias] = path
	}
	if err := rejectImportShadowing(module, imports); err != nil {
		return nil, err
	}
	return imports, nil
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
	case *ast.UnionDecl:
		return d.Name, true
	case *ast.ContractDecl:
		return d.Name, true
	default:
		return "", false
	}
}

// qualifyModule rewrites one parsed module into package-qualified names.
func (c *graphChecker) qualifyModule(module *moduleUnit) (*ast.Program, error) {
	out := &ast.Program{}
	for _, decl := range module.program.Decls {
		qualified, err := c.qualifyDecl(module, decl)
		if err != nil {
			return nil, err
		}
		if qualified != nil {
			out.Decls = append(out.Decls, qualified)
		}
	}
	return out, nil
}

// qualifyDecl rewrites declaration type references for a package check.
func (c *graphChecker) qualifyDecl(module *moduleUnit, decl ast.Decl) (ast.Decl, error) {
	switch d := decl.(type) {
	case *ast.ImportDecl:
		return nil, nil
	case *ast.StructDecl:
		return c.qualifyStruct(module, d)
	case *ast.EnumDecl:
		cp := *d
		cp.Name = module.path + "::" + d.Name
		return &cp, nil
	case *ast.UnionDecl:
		return c.qualifyUnion(module, d)
	case *ast.ContractDecl:
		return c.qualifyContract(module, d)
	case *ast.ImplDecl:
		return c.qualifyImpl(module, d)
	case *ast.FunctionDecl:
		return c.qualifyFunction(module, d, module.path+"::"+d.Name)
	case *ast.TestDecl:
		return c.qualifyTestDecl(module, d)
	default:
		return decl, nil
	}
}

// qualifyStruct rewrites a struct declaration and its field types.
func (c *graphChecker) qualifyStruct(
	module *moduleUnit,
	decl *ast.StructDecl,
) (*ast.StructDecl, error) {
	cp := *decl
	cp.Name = module.path + "::" + decl.Name
	cp.Fields = append([]ast.Field(nil), decl.Fields...)
	for idx := range cp.Fields {
		typ, err := c.resolveType(module, cp.Fields[idx].TypeName)
		if err != nil {
			return nil, err
		}
		cp.Fields[idx].TypeName = typ
	}
	return &cp, nil
}

// qualifyUnion rewrites a union declaration and its payload types.
func (c *graphChecker) qualifyUnion(
	module *moduleUnit,
	decl *ast.UnionDecl,
) (*ast.UnionDecl, error) {
	cp := *decl
	cp.Name = module.path + "::" + decl.Name
	cp.Variants = append([]ast.UnionVariant(nil), decl.Variants...)
	for idx := range cp.Variants {
		if cp.Variants[idx].Payload == "" {
			continue
		}
		typ, err := c.resolveType(module, cp.Variants[idx].Payload)
		if err != nil {
			return nil, err
		}
		cp.Variants[idx].Payload = typ
	}
	return &cp, nil
}

// qualifyContract rewrites contract method signature type references.
func (c *graphChecker) qualifyContract(
	module *moduleUnit,
	decl *ast.ContractDecl,
) (*ast.ContractDecl, error) {
	cp := *decl
	cp.Name = module.path + "::" + decl.Name
	cp.Methods = append([]*ast.FunctionDecl(nil), decl.Methods...)
	for idx, method := range cp.Methods {
		qualified, err := c.qualifyFunction(module, method, method.Name)
		if err != nil {
			return nil, err
		}
		cp.Methods[idx] = qualified
	}
	return &cp, nil
}

// qualifyImpl rewrites an impl receiver, contract name, and method bodies.
func (c *graphChecker) qualifyImpl(module *moduleUnit, decl *ast.ImplDecl) (*ast.ImplDecl, error) {
	cp := *decl
	typeName, err := c.resolveType(module, decl.TypeName)
	if err != nil {
		return nil, err
	}
	cp.TypeName = typeName
	if decl.ContractName != "" {
		contractName, err := c.resolveType(module, decl.ContractName)
		if err != nil {
			return nil, err
		}
		cp.ContractName = contractName
	}
	cp.Methods = append([]*ast.FunctionDecl(nil), decl.Methods...)
	for idx, method := range cp.Methods {
		qualified, err := c.qualifyFunction(module, method, method.Name)
		if err != nil {
			return nil, err
		}
		cp.Methods[idx] = qualified
	}
	return &cp, nil
}

// qualifyFunction rewrites a function signature and body type references.
func (c *graphChecker) qualifyFunction(
	module *moduleUnit,
	decl *ast.FunctionDecl,
	name string,
) (*ast.FunctionDecl, error) {
	cp := *decl
	cp.Name = name
	cp.Params = append([]ast.Param(nil), decl.Params...)
	for idx := range cp.Params {
		typ, err := c.resolveType(module, cp.Params[idx].TypeName)
		if err != nil {
			return nil, err
		}
		cp.Params[idx].TypeName = typ
	}
	if cp.ReturnType != "" {
		typ, err := c.resolveType(module, cp.ReturnType)
		if err != nil {
			return nil, err
		}
		cp.ReturnType = typ
	}
	body, err := c.qualifyBlock(module, decl.Body)
	if err != nil {
		return nil, err
	}
	cp.Body = body
	return &cp, nil
}

// qualifyTestDecl rewrites type-bearing expressions inside a test block.
func (c *graphChecker) qualifyTestDecl(
	module *moduleUnit,
	decl *ast.TestDecl,
) (*ast.TestDecl, error) {
	cp := *decl
	body, err := c.qualifyBlock(module, decl.Body)
	cp.Body = body
	return &cp, err
}

// qualifyBlock rewrites type-bearing expressions inside a block.
func (c *graphChecker) qualifyBlock(
	module *moduleUnit,
	block *ast.BlockStmt,
) (*ast.BlockStmt, error) {
	if block == nil {
		return nil, nil
	}
	cp := &ast.BlockStmt{Statements: append([]ast.Statement(nil), block.Statements...)}
	for idx, stmt := range cp.Statements {
		qualified, err := c.qualifyStmt(module, stmt)
		if err != nil {
			return nil, err
		}
		cp.Statements[idx] = qualified
	}
	return cp, nil
}

// qualifyStmt rewrites type-bearing expressions inside one statement.
func (c *graphChecker) qualifyStmt(module *moduleUnit, stmt ast.Statement) (ast.Statement, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		cp := *s
		value, err := c.qualifyExpr(module, s.Value)
		cp.Value = value
		return &cp, err
	case *ast.AssignStmt:
		return c.qualifyAssignStmt(module, s)
	case *ast.ReturnStmt:
		cp := *s
		value, err := c.qualifyExpr(module, s.Value)
		cp.Value = value
		return &cp, err
	case *ast.DeferStmt:
		return c.qualifyDeferStmt(module, s)
	case *ast.ErrDeferStmt:
		return c.qualifyErrDeferStmt(module, s)
	case *ast.ExprStmt:
		cp := *s
		expr, err := c.qualifyExpr(module, s.Expr)
		cp.Expr = expr
		return &cp, err
	case *ast.IfStmt:
		return c.qualifyIfStmt(module, s)
	case *ast.WhileStmt:
		cp := *s
		condition, err := c.qualifyExpr(module, s.Condition)
		if err != nil {
			return nil, err
		}
		cp.Condition = condition
		cp.Body, err = c.qualifyBlock(module, s.Body)
		return &cp, err
	case *ast.ForStmt:
		return c.qualifyForStmt(module, s)
	case *ast.MatchStmt:
		return c.qualifyMatchStmt(module, s)
	case *ast.UnsafeStmt:
		cp := *s
		body, err := c.qualifyBlock(module, s.Body)
		cp.Body = body
		return &cp, err
	case *ast.ComptimeIfStmt:
		return c.qualifyComptimeIfStmt(module, s)
	default:
		return stmt, nil
	}
}

// qualifyDeferStmt rewrites type-bearing expressions in a deferred cleanup.
func (c *graphChecker) qualifyDeferStmt(
	module *moduleUnit,
	stmt *ast.DeferStmt,
) (*ast.DeferStmt, error) {
	cp := *stmt
	expr, err := c.qualifyExpr(module, stmt.Expr)
	cp.Expr = expr
	return &cp, err
}

// qualifyErrDeferStmt rewrites type-bearing expressions in an errdefer cleanup.
func (c *graphChecker) qualifyErrDeferStmt(
	module *moduleUnit,
	stmt *ast.ErrDeferStmt,
) (*ast.ErrDeferStmt, error) {
	cp := *stmt
	expr, err := c.qualifyExpr(module, stmt.Expr)
	cp.Expr = expr
	return &cp, err
}

// qualifyAssignStmt rewrites both sides of an assignment statement.
func (c *graphChecker) qualifyAssignStmt(
	module *moduleUnit,
	stmt *ast.AssignStmt,
) (*ast.AssignStmt, error) {
	cp := *stmt
	var err error
	cp.Target, err = c.qualifyExpr(module, stmt.Target)
	if err != nil {
		return nil, err
	}
	cp.Value, err = c.qualifyExpr(module, stmt.Value)
	return &cp, err
}

// qualifyIfStmt rewrites all expressions reachable from an if node.
func (c *graphChecker) qualifyIfStmt(module *moduleUnit, stmt *ast.IfStmt) (*ast.IfStmt, error) {
	cp := *stmt
	condition, err := c.qualifyExpr(module, stmt.Condition)
	if err != nil {
		return nil, err
	}
	cp.Condition = condition
	cp.Consequence, err = c.qualifyBlock(module, stmt.Consequence)
	if err != nil {
		return nil, err
	}
	cp.Alternative, err = c.qualifyBlock(module, stmt.Alternative)
	return &cp, err
}

// qualifyForStmt rewrites range bounds and loop body expressions.
func (c *graphChecker) qualifyForStmt(module *moduleUnit, stmt *ast.ForStmt) (*ast.ForStmt, error) {
	cp := *stmt
	var err error
	cp.Start, err = c.qualifyExpr(module, stmt.Start)
	if err != nil {
		return nil, err
	}
	cp.End, err = c.qualifyExpr(module, stmt.End)
	if err != nil {
		return nil, err
	}
	cp.Body, err = c.qualifyBlock(module, stmt.Body)
	return &cp, err
}

// qualifyComptimeIfStmt rewrites both branches of a compile-time conditional.
func (c *graphChecker) qualifyComptimeIfStmt(
	module *moduleUnit,
	stmt *ast.ComptimeIfStmt,
) (*ast.ComptimeIfStmt, error) {
	cp := *stmt
	condition, err := c.qualifyExpr(module, stmt.Condition)
	if err != nil {
		return nil, err
	}
	cp.Condition = condition
	cp.Consequence, err = c.qualifyBlock(module, stmt.Consequence)
	if err != nil {
		return nil, err
	}
	cp.Alternative, err = c.qualifyBlock(module, stmt.Alternative)
	return &cp, err
}

// qualifyMatchStmt rewrites the matched value and all arm bodies.
func (c *graphChecker) qualifyMatchStmt(
	module *moduleUnit,
	stmt *ast.MatchStmt,
) (*ast.MatchStmt, error) {
	cp := *stmt
	var err error
	cp.Value, err = c.qualifyExpr(module, stmt.Value)
	if err != nil {
		return nil, err
	}
	cp.Arms = append([]ast.MatchArm(nil), stmt.Arms...)
	for idx := range cp.Arms {
		cp.Arms[idx].Body, err = c.qualifyStmt(module, cp.Arms[idx].Body)
		if err != nil {
			return nil, err
		}
	}
	return &cp, nil
}

// qualifyExpr rewrites type names carried by expressions.
func (c *graphChecker) qualifyExpr(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	switch e := expr.(type) {
	case *ast.ComptimeExpr:
		return c.qualifyComptimeExpr(module, e)
	case *ast.PrefixExpr:
		return c.qualifyPrefixExpr(module, e)
	case *ast.BinaryExpr:
		return c.qualifyBinaryExpr(module, e)
	case *ast.CallExpr:
		return c.qualifyCallExpr(module, e)
	case *ast.CastExpr:
		return c.qualifyCastExpr(module, e)
	case *ast.TryExpr:
		return c.qualifyTryExpr(module, e)
	default:
		return c.qualifyTypeOrControlExpr(module, expr)
	}
}

// qualifyTypeOrControlExpr rewrites type-bearing and control expressions.
func (c *graphChecker) qualifyTypeOrControlExpr(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	switch e := expr.(type) {
	case *ast.TypeApplyExpr:
		return c.qualifyTypeApplyExpr(module, e)
	case *ast.TypeExpr:
		cp := *e
		typ, err := c.resolveType(module, e.TypeName)
		if err != nil {
			return &cp, err
		}
		cp.TypeName = typ
		return &cp, nil
	case *ast.ArenaNewExpr:
		cp := *e
		typ, err := c.resolveType(module, e.TypeName)
		if err != nil {
			return &cp, err
		}
		cp.TypeName = typ
		cp.Allocator, err = c.qualifyExpr(module, e.Allocator)
		return &cp, err
	case *ast.StructLiteralExpr:
		return c.qualifyStructLiteral(module, e)
	case *ast.FieldExpr:
		return c.qualifyFieldExpr(module, e)
	case *ast.IndexExpr:
		return c.qualifyIndexExpr(module, e)
	case *ast.DerefExpr:
		return c.qualifyDerefExpr(module, e)
	case *ast.IfStmt:
		return c.qualifyIfStmt(module, e)
	case *ast.MatchStmt:
		return c.qualifyMatchStmt(module, e)
	default:
		return expr, nil
	}
}

// qualifyComptimeExpr rewrites the expression evaluated at compile time.
func (c *graphChecker) qualifyComptimeExpr(
	module *moduleUnit,
	expr *ast.ComptimeExpr,
) (*ast.ComptimeExpr, error) {
	cp := *expr
	value, err := c.qualifyExpr(module, expr.Expr)
	cp.Expr = value
	return &cp, err
}

// qualifyPrefixExpr rewrites the operand of a unary expression.
func (c *graphChecker) qualifyPrefixExpr(
	module *moduleUnit,
	expr *ast.PrefixExpr,
) (*ast.PrefixExpr, error) {
	cp := *expr
	right, err := c.qualifyExpr(module, expr.Right)
	cp.Right = right
	return &cp, err
}

// qualifyBinaryExpr rewrites both sides of a binary expression.
func (c *graphChecker) qualifyBinaryExpr(
	module *moduleUnit,
	expr *ast.BinaryExpr,
) (*ast.BinaryExpr, error) {
	cp := *expr
	var err error
	cp.Left, err = c.qualifyExpr(module, expr.Left)
	if err != nil {
		return nil, err
	}
	cp.Right, err = c.qualifyExpr(module, expr.Right)
	return &cp, err
}

// qualifyCastExpr rewrites the target type and value of a cast expression.
func (c *graphChecker) qualifyCastExpr(
	module *moduleUnit,
	expr *ast.CastExpr,
) (*ast.CastExpr, error) {
	cp := *expr
	typ, err := c.resolveType(module, expr.TargetType)
	if err != nil {
		return nil, err
	}
	cp.TargetType = typ
	cp.Value, err = c.qualifyExpr(module, expr.Value)
	return &cp, err
}

// qualifyTryExpr rewrites the fallible expression wrapped by try.
func (c *graphChecker) qualifyTryExpr(
	module *moduleUnit,
	expr *ast.TryExpr,
) (*ast.TryExpr, error) {
	cp := *expr
	value, err := c.qualifyExpr(module, expr.Value)
	cp.Value = value
	return &cp, err
}

// qualifyCallExpr rewrites callees and arguments inside a call expression.
func (c *graphChecker) qualifyCallExpr(
	module *moduleUnit,
	expr *ast.CallExpr,
) (*ast.CallExpr, error) {
	cp := *expr
	var err error
	cp.Callee, err = c.qualifyCallee(module, expr.Callee)
	if err != nil {
		return nil, err
	}
	cp.Args = append([]ast.Expression(nil), expr.Args...)
	for idx := range cp.Args {
		cp.Args[idx], err = c.qualifyExpr(module, cp.Args[idx])
		if err != nil {
			return nil, err
		}
	}
	return &cp, nil
}

// qualifyTypeApplyExpr rewrites explicit static type arguments in constructor calls.
func (c *graphChecker) qualifyTypeApplyExpr(
	module *moduleUnit,
	expr *ast.TypeApplyExpr,
) (*ast.TypeApplyExpr, error) {
	cp := *expr
	callee, err := c.qualifyExpr(module, expr.Callee)
	if err != nil {
		return nil, err
	}
	cp.Callee = callee
	args, err := splitTypeArgs(expr.TypeArg)
	if err != nil {
		return nil, err
	}
	for idx, arg := range args {
		args[idx], err = c.resolveType(module, arg)
		if err != nil {
			return nil, err
		}
	}
	cp.TypeArg = strings.Join(args, ", ")
	return &cp, nil
}

// qualifyCallee rewrites imported function calls to their package function name.
func (c *graphChecker) qualifyCallee(
	module *moduleUnit,
	expr ast.Expression,
) (ast.Expression, error) {
	if ident, ok := expr.(*ast.IdentExpr); ok && declaresFunction(module, ident.Name) {
		return &ast.IdentExpr{Name: module.path + "::" + ident.Name}, nil
	}
	if field, ok := expr.(*ast.FieldExpr); ok && field.Namespace {
		if _, ok := c.resolveTypeNamespaceReceiver(module, field); ok {
			return c.qualifyFieldExpr(module, field)
		}
		name, ok, err := c.resolveNamespacePath(module, field)
		if err != nil {
			return nil, err
		}
		if ok {
			return &ast.IdentExpr{Name: name}, nil
		}
	}
	return c.qualifyExpr(module, expr)
}

// declaresFunction reports whether module has a local top-level function.
func declaresFunction(module *moduleUnit, name string) bool {
	for _, decl := range module.program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if ok && fn.Name == name {
			return true
		}
	}
	return false
}

// qualifyFieldExpr rewrites namespace receivers while preserving field names.
func (c *graphChecker) qualifyFieldExpr(
	module *moduleUnit,
	expr *ast.FieldExpr,
) (*ast.FieldExpr, error) {
	cp := *expr
	if expr.Namespace {
		receiver, ok := c.resolveTypeNamespaceReceiver(module, expr)
		if ok {
			cp.Receiver = &ast.IdentExpr{Name: receiver}
			return &cp, nil
		}
	}
	receiver, err := c.qualifyExpr(module, expr.Receiver)
	cp.Receiver = receiver
	return &cp, err
}

// qualifyIndexExpr rewrites target and index expressions.
func (c *graphChecker) qualifyIndexExpr(
	module *moduleUnit,
	expr *ast.IndexExpr,
) (*ast.IndexExpr, error) {
	cp := *expr
	var err error
	cp.Target, err = c.qualifyExpr(module, expr.Target)
	if err != nil {
		return nil, err
	}
	cp.Index, err = c.qualifyExpr(module, expr.Index)
	if err != nil {
		return nil, err
	}
	cp.Start, err = c.qualifyExpr(module, expr.Start)
	if err != nil {
		return nil, err
	}
	cp.End, err = c.qualifyExpr(module, expr.End)
	return &cp, err
}

// qualifyDerefExpr rewrites the receiver before explicit pointer dereference.
func (c *graphChecker) qualifyDerefExpr(
	module *moduleUnit,
	expr *ast.DerefExpr,
) (*ast.DerefExpr, error) {
	cp := *expr
	receiver, err := c.qualifyExpr(module, expr.Receiver)
	cp.Receiver = receiver
	return &cp, err
}

// qualifyStructLiteral rewrites struct literal type and field value expressions.
func (c *graphChecker) qualifyStructLiteral(
	module *moduleUnit,
	expr *ast.StructLiteralExpr,
) (*ast.StructLiteralExpr, error) {
	cp := *expr
	typ, err := c.resolveType(module, expr.TypeName)
	if err != nil {
		return nil, err
	}
	cp.TypeName = typ
	cp.Fields = append([]ast.FieldValue(nil), expr.Fields...)
	for idx := range cp.Fields {
		value, err := c.qualifyExpr(module, cp.Fields[idx].Value)
		if err != nil {
			return nil, err
		}
		cp.Fields[idx].Value = value
	}
	return &cp, nil
}

// resolveTypeNamespaceReceiver resolves the receiver of Type::Tag namespace lookups.
func (c *graphChecker) resolveTypeNamespaceReceiver(
	module *moduleUnit,
	expr *ast.FieldExpr,
) (string, bool) {
	parts, ok := namespaceParts(expr.Receiver)
	if !ok {
		return "", false
	}
	return c.resolveTypeNamespaceParts(module, parts)
}

// resolveNamespacePath resolves a complete module-qualified namespace chain.
func (c *graphChecker) resolveNamespacePath(
	module *moduleUnit,
	expr *ast.FieldExpr,
) (string, bool, error) {
	parts, ok := namespaceParts(expr)
	if !ok {
		return "", false, nil
	}
	return c.resolveNamespaceParts(module, parts)
}

// resolveNamespaceParts resolves local or imported function namespace parts.
func (c *graphChecker) resolveNamespaceParts(
	module *moduleUnit,
	parts []string,
) (string, bool, error) {
	if len(parts) == 0 {
		return "", false, nil
	}
	if target, ok := module.namespaces[parts[0]]; ok {
		if len(parts) == 1 {
			return "", false, nil
		}
		return c.resolveImportedFunction(module, target, parts)
	}
	name := module.path + "::" + strings.Join(parts, "::")
	if _, ok := c.functions[name]; ok {
		return name, true, nil
	}
	return "", false, nil
}

// resolveImportedFunction validates visibility for a call through an import alias.
func (c *graphChecker) resolveImportedFunction(
	module *moduleUnit,
	target string,
	parts []string,
) (string, bool, error) {
	name := target + "::" + strings.Join(parts[1:], "::")
	exported, ok := c.functions[name]
	if !ok {
		sourceName := strings.Join(parts, "::")
		return "", false, fmt.Errorf("module error: unknown function `%s`", sourceName)
	}
	if exported.module != module.path && !exported.public {
		sourceName := strings.Join(parts, "::")
		return "", false, fmt.Errorf("module error: function `%s` is private", sourceName)
	}
	return name, true, nil
}

// resolveTypeNamespaceParts resolves namespace parts only when they name a type.
func (c *graphChecker) resolveTypeNamespaceParts(
	module *moduleUnit,
	parts []string,
) (string, bool) {
	if len(parts) == 0 {
		return "", false
	}
	if target, ok := module.namespaces[parts[0]]; ok && len(parts) > 1 {
		name := target + "::" + strings.Join(parts[1:], "::")
		if _, exists := c.types[name]; exists {
			return name, true
		}
	}
	name := module.path + "::" + strings.Join(parts, "::")
	if _, ok := c.types[name]; ok {
		return name, true
	}
	return "", false
}

// namespaceParts returns identifier segments from a namespace expression.
func namespaceParts(expr ast.Expression) ([]string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return []string{e.Name}, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return nil, false
		}
		parts, ok := namespaceParts(e.Receiver)
		if !ok {
			return nil, false
		}
		return append(parts, e.Name), true
	default:
		return nil, false
	}
}

// resolveType rewrites a source type name into its package-qualified form.
func (c *graphChecker) resolveType(module *moduleUnit, name string) (string, error) {
	resolver := typeResolver{checker: c, module: module}
	return resolver.resolve(name)
}

type typeResolver struct {
	checker *graphChecker
	module  *moduleUnit
}

// resolve handles wrappers such as borrows, error unions, slices, and generics.
func (r typeResolver) resolve(name string) (string, error) {
	switch {
	case strings.HasPrefix(name, "!"):
		return r.resolvePrefixed(name, "!")
	case strings.HasPrefix(name, "&var "):
		return r.resolvePrefixed(name, "&var ")
	case strings.HasPrefix(name, "&"):
		return r.resolvePrefixed(name, "&")
	case strings.HasPrefix(name, "?"):
		return r.resolvePrefixed(name, "?")
	case strings.HasPrefix(name, "[]"):
		return r.resolvePrefixed(name, "[]")
	case strings.HasPrefix(name, "const "):
		return r.resolvePrefixed(name, "const ")
	case strings.HasPrefix(name, "dyn "):
		return r.resolvePrefixed(name, "dyn ")
	}
	if errorType, successType, ok := splitTypedErrorUnion(name); ok {
		return r.resolveTypedErrorUnion(errorType, successType)
	}
	if base, args, ok := splitTypeApply(name); ok {
		return r.resolveGeneric(base, args)
	}
	return r.resolveBase(name)
}

// resolvePrefixed resolves a type wrapper after removing prefix.
func (r typeResolver) resolvePrefixed(name string, prefix string) (string, error) {
	inner, err := r.resolve(strings.TrimPrefix(name, prefix))
	if err != nil {
		return "", err
	}
	return prefix + inner, nil
}

// resolveGeneric resolves a generic base and each static type argument.
func (r typeResolver) resolveGeneric(base string, args string) (string, error) {
	resolvedBase, err := r.resolveBase(base)
	if err != nil {
		return "", err
	}
	parts, err := splitTypeArgs(args)
	if err != nil {
		return "", err
	}
	for idx, part := range parts {
		parts[idx], err = r.resolve(part)
		if err != nil {
			return "", err
		}
	}
	return resolvedBase + "<" + strings.Join(parts, ", ") + ">", nil
}

// resolveTypedErrorUnion resolves Error!T names across module boundaries.
func (r typeResolver) resolveTypedErrorUnion(errorType string, successType string) (string, error) {
	resolvedError, err := r.resolve(errorType)
	if err != nil {
		return "", err
	}
	resolvedSuccess, err := r.resolve(successType)
	if err != nil {
		return "", err
	}
	return resolvedError + "!" + resolvedSuccess, nil
}

// resolveBase resolves one non-generic type base.
func (r typeResolver) resolveBase(name string) (string, error) {
	if isPrimitiveType(name) || strings.HasPrefix(name, "std::") {
		return name, nil
	}
	if strings.Contains(name, "::") {
		return r.resolveQualifiedBase(name)
	}
	local := r.module.path + "::" + name
	if _, ok := r.checker.types[local]; ok {
		return local, nil
	}
	return name, nil
}

// resolveQualifiedBase resolves an imported module type by last-segment alias.
func (r typeResolver) resolveQualifiedBase(name string) (string, error) {
	parts := strings.Split(name, "::")
	targetModule, ok := r.module.namespaces[parts[0]]
	if !ok {
		return "", fmt.Errorf("module error: `%s` is not imported in `%s`", parts[0], r.module.path)
	}
	qualified := targetModule + "::" + strings.Join(parts[1:], "::")
	exported, ok := r.checker.types[qualified]
	if !ok {
		return "", fmt.Errorf("module error: unknown type `%s`", name)
	}
	if exported.module != r.module.path && !exported.public {
		return "", fmt.Errorf("module error: type `%s` is private", name)
	}
	return qualified, nil
}

// isPrimitiveType reports source-level types that do not need module lookup.
func isPrimitiveType(name string) bool {
	switch name {
	case "bool", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "void", "Io", "Allocator", "Function", "Self",
		"type":
		return true
	default:
		return false
	}
}

// splitTypeApply separates Base<Args> type spellings.
func splitTypeApply(name string) (string, string, bool) {
	start := strings.Index(name, "<")
	if start < 0 || !strings.HasSuffix(name, ">") {
		return "", "", false
	}
	return name[:start], strings.TrimSuffix(name[start+1:], ">"), true
}

// splitTypeArgs splits comma-separated static type arguments with nested angle support.
func splitTypeArgs(args string) ([]string, error) {
	parts := []string{}
	depth := 0
	start := 0
	for idx, ch := range args {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(args[start:idx]))
				start = idx + 1
			}
		}
		if depth < 0 {
			return nil, fmt.Errorf("module error: invalid static arguments `%s`", args)
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("module error: invalid static arguments `%s`", args)
	}
	parts = append(parts, strings.TrimSpace(args[start:]))
	return parts, nil
}

// splitTypedErrorUnion separates Error!T while leaving prefix !T to resolvePrefixed.
func splitTypedErrorUnion(name string) (string, string, bool) {
	depth := 0
	for index, ch := range name {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
		case '!':
			if depth == 0 && index > 0 && index < len(name)-1 {
				return name[:index], name[index+1:], true
			}
		}
	}
	return "", "", false
}

// sortedModuleUnits returns modules in deterministic path order.
func sortedModuleUnits(modules map[string]*moduleUnit) []*moduleUnit {
	paths := make([]string, 0, len(modules))
	for path := range modules {
		paths = append(paths, path)
	}
	sortStrings(paths)
	out := make([]*moduleUnit, 0, len(paths))
	for _, path := range paths {
		out = append(out, modules[path])
	}
	return out
}

// sortedImportPaths returns imported module paths in deterministic order.
func sortedImportPaths(imports map[string]string) []string {
	paths := make([]string, 0, len(imports))
	for _, path := range imports {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
}

// sortStrings sorts values without exposing a helper dependency to callers.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
