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
)

// ParsedModule is one source module after parsing and import extraction.
type ParsedModule struct {
	Path    string
	File    string
	Program *ast.Program
	Imports []string
}

// ResolvedPackage is a parsed package graph ready for static checks.
type ResolvedPackage struct {
	Root    string
	Modules []ParsedModule
	Program *ast.Program
}

// ResolvePackage loads a manifest path or package directory into one checked program.
func ResolvePackage(path string) (ResolvedPackage, []string, error) {
	baseDir, manifest, err := loadManifest(path)
	if err != nil {
		return ResolvedPackage{}, nil, err
	}
	graph, err := ResolveModules(baseDir, manifest)
	if err != nil {
		return ResolvedPackage{}, nil, err
	}
	modules, errs, err := parseModules(graph)
	if err != nil || len(errs) > 0 {
		return ResolvedPackage{}, errs, err
	}
	if err := validateImports(graph, modules); err != nil {
		return ResolvedPackage{}, nil, err
	}
	if err := validateVisibility(modules); err != nil {
		return ResolvedPackage{}, nil, err
	}
	ordered, err := topoModules(graph, modules)
	if err != nil {
		return ResolvedPackage{}, nil, err
	}
	program := flattenModules(graph.Root, ordered)
	return ResolvedPackage{Root: graph.Root, Modules: ordered, Program: program}, nil, nil
}

// loadManifest reads kizu.toml from a file or package directory.
func loadManifest(path string) (string, Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", Manifest{}, err
	}
	manifestPath := path
	if info.IsDir() {
		manifestPath = filepath.Join(path, "kizu.toml")
	}
	source, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", Manifest{}, err
	}
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return "", Manifest{}, err
	}
	return filepath.Dir(manifestPath), manifest, nil
}

// parseModules parses every source file in the graph.
func parseModules(graph Graph) (map[string]ParsedModule, []string, error) {
	out := map[string]ParsedModule{}
	for _, module := range graph.Modules {
		source, err := os.ReadFile(module.File)
		if err != nil {
			return nil, nil, err
		}
		p := parser.New(lexer.New(string(source)))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil, qualifyParseErrors(module.Path, p.Errors()), nil
		}
		out[module.Path] = ParsedModule{
			Path: module.Path, File: module.File, Program: program, Imports: importsOf(program),
		}
	}
	return out, nil, nil
}

// qualifyParseErrors adds module context to parser errors.
func qualifyParseErrors(module string, errs []string) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		out = append(out, "module "+module+": "+err)
	}
	return out
}

// importsOf returns explicit imports from one module.
func importsOf(program *ast.Program) []string {
	imports := []string{}
	for _, decl := range program.Decls {
		if importDecl, ok := decl.(*ast.ImportDecl); ok {
			imports = append(imports, strings.Join(importDecl.Path, "::"))
		}
	}
	return imports
}

// validateImports checks missing modules and ambiguous imported aliases.
func validateImports(graph Graph, modules map[string]ParsedModule) error {
	moduleSet := map[string]bool{}
	for _, module := range graph.Modules {
		moduleSet[module.Path] = true
	}
	for _, module := range modules {
		aliases := map[string]string{}
		for _, imported := range module.Imports {
			if !moduleSet[imported] {
				return fmt.Errorf("module error: `%s` imports missing module `%s`",
					module.Path, imported)
			}
			alias := lastSegment(imported)
			if previous, ok := aliases[alias]; ok {
				return fmt.Errorf("module error: `%s` imports `%s` and `%s` as `%s`",
					module.Path, previous, imported, alias)
			}
			if hasTopLevelDecl(module.Program, alias) {
				return fmt.Errorf("module error: `%s` declaration shadows imported module `%s`",
					module.Path, alias)
			}
			aliases[alias] = imported
		}
	}
	return nil
}

// validateVisibility rejects references to private top-level declarations.
func validateVisibility(modules map[string]ParsedModule) error {
	exports := exportedDecls(modules)
	for _, module := range modules {
		ctx := visibilityContext{
			module:  module.Path,
			imports: importAliasMap(module.Imports),
			exports: exports,
		}
		for _, decl := range module.Program.Decls {
			if err := checkDeclVisibility(ctx, decl); err != nil {
				return err
			}
		}
	}
	return nil
}

type visibilityContext struct {
	module  string
	imports map[string]string
	exports map[string]map[string]bool
}

// exportedDecls returns public top-level declaration names per module.
func exportedDecls(modules map[string]ParsedModule) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, module := range modules {
		out[module.Path] = map[string]bool{}
		for _, decl := range module.Program.Decls {
			name := declName(decl)
			if name != "" && declPublic(decl) {
				out[module.Path][name] = true
			}
		}
	}
	return out
}

// importAliasMap maps final path segment to imported module path.
func importAliasMap(imports []string) map[string]string {
	out := map[string]string{}
	for _, imported := range imports {
		out[lastSegment(imported)] = imported
	}
	return out
}

// checkDeclVisibility validates external type references inside one declaration.
func checkDeclVisibility(ctx visibilityContext, decl ast.Decl) error {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return checkFunctionVisibility(ctx, d)
	case *ast.StructDecl:
		return checkFieldsVisibility(ctx, d.Fields)
	case *ast.UnionDecl:
		return checkUnionVisibility(ctx, d)
	case *ast.ContractDecl:
		return checkMethodsVisibility(ctx, d.Methods)
	case *ast.ImplDecl:
		return checkImplVisibility(ctx, d)
	case *ast.SatisfyDecl:
		return checkSatisfyVisibility(ctx, d)
	}
	return nil
}

// checkFieldsVisibility validates struct field types.
func checkFieldsVisibility(ctx visibilityContext, fields []ast.Field) error {
	for _, field := range fields {
		if err := checkTypeVisibility(ctx, field.TypeName); err != nil {
			return err
		}
	}
	return nil
}

// checkUnionVisibility validates union payload types.
func checkUnionVisibility(ctx visibilityContext, union *ast.UnionDecl) error {
	for _, variant := range union.Variants {
		if err := checkTypeVisibility(ctx, variant.Payload); err != nil {
			return err
		}
	}
	return nil
}

// checkMethodsVisibility validates a list of method signatures or bodies.
func checkMethodsVisibility(ctx visibilityContext, methods []*ast.FunctionDecl) error {
	for _, method := range methods {
		if err := checkFunctionVisibility(ctx, method); err != nil {
			return err
		}
	}
	return nil
}

// checkImplVisibility validates an impl target and methods.
func checkImplVisibility(ctx visibilityContext, impl *ast.ImplDecl) error {
	if err := checkTypeVisibility(ctx, impl.TypeName); err != nil {
		return err
	}
	return checkMethodsVisibility(ctx, impl.Methods)
}

// checkSatisfyVisibility validates a contract satisfaction declaration.
func checkSatisfyVisibility(ctx visibilityContext, satisfy *ast.SatisfyDecl) error {
	if err := checkTypeVisibility(ctx, satisfy.ContractName); err != nil {
		return err
	}
	return checkTypeVisibility(ctx, satisfy.TypeName)
}

// checkFunctionVisibility validates function signature and body references.
func checkFunctionVisibility(ctx visibilityContext, fn *ast.FunctionDecl) error {
	for _, param := range fn.Params {
		if err := checkTypeVisibility(ctx, param.TypeName); err != nil {
			return err
		}
	}
	if err := checkTypeVisibility(ctx, fn.ReturnType); err != nil {
		return err
	}
	if fn.Body == nil {
		return nil
	}
	return checkBlockVisibility(ctx, fn.Body)
}

// checkBlockVisibility validates external references in one block.
func checkBlockVisibility(ctx visibilityContext, block *ast.BlockStmt) error {
	for _, stmt := range block.Statements {
		if err := checkStmtVisibility(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// checkStmtVisibility validates external references in one statement.
func checkStmtVisibility(ctx visibilityContext, stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return checkExprVisibility(ctx, s.Value)
	case *ast.AssignStmt:
		if err := checkExprVisibility(ctx, s.Target); err != nil {
			return err
		}
		return checkExprVisibility(ctx, s.Value)
	case *ast.ReturnStmt:
		return checkExprVisibility(ctx, s.Value)
	case *ast.IfStmt:
		return checkBranchVisibility(ctx, s.Condition, s.Consequence, s.Alternative)
	case *ast.WhileStmt:
		if err := checkExprVisibility(ctx, s.Condition); err != nil {
			return err
		}
		return checkBlockVisibility(ctx, s.Body)
	case *ast.ForStmt:
		return checkForVisibility(ctx, s)
	case *ast.MatchStmt:
		return checkMatchVisibility(ctx, s)
	case *ast.UnsafeStmt:
		return checkBlockVisibility(ctx, s.Body)
	case *ast.ComptimeIfStmt:
		return checkBranchVisibility(ctx, s.Condition, s.Consequence, s.Alternative)
	case *ast.ExprStmt:
		return checkExprVisibility(ctx, s.Expr)
	default:
		return nil
	}
}

// checkBranchVisibility validates an if-like branch.
func checkBranchVisibility(
	ctx visibilityContext,
	condition ast.Expression,
	consequence *ast.BlockStmt,
	alternative *ast.BlockStmt,
) error {
	if err := checkExprVisibility(ctx, condition); err != nil {
		return err
	}
	if err := checkBlockVisibility(ctx, consequence); err != nil {
		return err
	}
	if alternative == nil {
		return nil
	}
	return checkBlockVisibility(ctx, alternative)
}

// checkForVisibility validates a for-loop statement.
func checkForVisibility(ctx visibilityContext, stmt *ast.ForStmt) error {
	if err := checkExprVisibility(ctx, stmt.Start); err != nil {
		return err
	}
	if err := checkExprVisibility(ctx, stmt.End); err != nil {
		return err
	}
	return checkBlockVisibility(ctx, stmt.Body)
}

// checkMatchVisibility validates match value and arm bodies.
func checkMatchVisibility(ctx visibilityContext, stmt *ast.MatchStmt) error {
	if err := checkExprVisibility(ctx, stmt.Value); err != nil {
		return err
	}
	for _, arm := range stmt.Arms {
		if err := checkStmtVisibility(ctx, arm.Body); err != nil {
			return err
		}
	}
	return nil
}

// checkExprVisibility validates external references in one expression.
func checkExprVisibility(ctx visibilityContext, expr ast.Expression) error {
	switch e := expr.(type) {
	case nil, *ast.IdentExpr, *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return nil
	case *ast.PrefixExpr:
		return checkExprVisibility(ctx, e.Right)
	case *ast.BinaryExpr:
		return checkBinaryVisibility(ctx, e)
	case *ast.CallExpr:
		return checkCallVisibility(ctx, e)
	case *ast.TypeApplyExpr:
		if err := checkExprVisibility(ctx, e.Callee); err != nil {
			return err
		}
		return checkTypeVisibility(ctx, e.TypeArg)
	case *ast.CastExpr:
		if err := checkTypeVisibility(ctx, e.TargetType); err != nil {
			return err
		}
		return checkExprVisibility(ctx, e.Value)
	case *ast.TryExpr:
		return checkExprVisibility(ctx, e.Value)
	default:
		return checkAggregateVisibility(ctx, expr)
	}
}

// checkAggregateVisibility validates aggregate, namespace, and control expressions.
func checkAggregateVisibility(ctx visibilityContext, expr ast.Expression) error {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return checkIndexVisibility(ctx, e)
	case *ast.ArenaNewExpr:
		return checkTypeVisibility(ctx, e.TypeName)
	case *ast.StructLiteralExpr:
		return checkStructLiteralVisibility(ctx, e)
	case *ast.FieldExpr:
		return checkFieldVisibility(ctx, e)
	case *ast.DerefExpr:
		return checkExprVisibility(ctx, e.Receiver)
	case *ast.IfStmt:
		return checkStmtVisibility(ctx, e)
	case *ast.MatchStmt:
		return checkStmtVisibility(ctx, e)
	case *ast.ComptimeExpr:
		return checkExprVisibility(ctx, e.Expr)
	default:
		return nil
	}
}

// checkBinaryVisibility validates both operands of a binary expression.
func checkBinaryVisibility(ctx visibilityContext, expr *ast.BinaryExpr) error {
	if err := checkExprVisibility(ctx, expr.Left); err != nil {
		return err
	}
	return checkExprVisibility(ctx, expr.Right)
}

// checkCallVisibility validates callee and arguments.
func checkCallVisibility(ctx visibilityContext, expr *ast.CallExpr) error {
	if err := checkExprVisibility(ctx, expr.Callee); err != nil {
		return err
	}
	for _, arg := range expr.Args {
		if err := checkExprVisibility(ctx, arg); err != nil {
			return err
		}
	}
	return nil
}

// checkIndexVisibility validates checked index and slice operands.
func checkIndexVisibility(ctx visibilityContext, expr *ast.IndexExpr) error {
	for _, item := range []ast.Expression{expr.Target, expr.Index, expr.Start, expr.End} {
		if err := checkExprVisibility(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// checkStructLiteralVisibility validates struct literal type and values.
func checkStructLiteralVisibility(ctx visibilityContext, expr *ast.StructLiteralExpr) error {
	if err := checkTypeVisibility(ctx, expr.TypeName); err != nil {
		return err
	}
	for _, field := range expr.Fields {
		if err := checkExprVisibility(ctx, field.Value); err != nil {
			return err
		}
	}
	return nil
}

// checkFieldVisibility validates external namespace lookups.
func checkFieldVisibility(ctx visibilityContext, expr *ast.FieldExpr) error {
	if !expr.Namespace {
		return checkExprVisibility(ctx, expr.Receiver)
	}
	parts, ok := namespaceParts(expr)
	if !ok || len(parts) < 2 {
		return nil
	}
	return checkImportedName(ctx, parts[0], parts[1])
}

// checkTypeVisibility validates one type spelling.
func checkTypeVisibility(ctx visibilityContext, typ string) error {
	if typ == "" {
		return nil
	}
	for _, prefix := range []string{"!Error!", "!", "?", "&mut ", "&", "[]", "const "} {
		if strings.HasPrefix(typ, prefix) {
			return checkTypeVisibility(ctx, strings.TrimPrefix(typ, prefix))
		}
	}
	base, args, ok := splitGeneric(typ)
	if ok {
		if err := checkTypeVisibility(ctx, base); err != nil {
			return err
		}
		for _, arg := range args {
			if err := checkTypeVisibility(ctx, arg); err != nil {
				return err
			}
		}
		return nil
	}
	parts := strings.Split(typ, "::")
	if len(parts) < 2 {
		return nil
	}
	return checkImportedName(ctx, parts[0], parts[1])
}

// checkImportedName validates that alias::name is public in the imported module.
func checkImportedName(ctx visibilityContext, alias string, name string) error {
	if alias == "std" {
		return nil
	}
	module, ok := ctx.imports[alias]
	if !ok {
		return nil
	}
	if ctx.exports[module][name] {
		return nil
	}
	return fmt.Errorf("module error: `%s` cannot access private declaration `%s::%s`",
		ctx.module, module, name)
}

// topoModules returns dependencies before dependents and rejects import cycles.
func topoModules(graph Graph, modules map[string]ParsedModule) ([]ParsedModule, error) {
	ordered := []ParsedModule{}
	visited := map[string]bool{}
	visiting := map[string]int{}
	stack := []string{}
	var visit func(string) error
	visit = func(path string) error {
		if visited[path] {
			return nil
		}
		if idx, ok := visiting[path]; ok {
			return fmt.Errorf("module error: cyclic import detected: %s",
				strings.Join(append(stack[idx:], path), " -> "))
		}
		module := modules[path]
		visiting[path] = len(stack)
		stack = append(stack, path)
		for _, imported := range sortedStrings(module.Imports) {
			if err := visit(imported); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		delete(visiting, path)
		visited[path] = true
		ordered = append(ordered, module)
		return nil
	}
	for _, module := range graph.Modules {
		if err := visit(module.Path); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// flattenModules rewrites module-local declarations into one checker program.
func flattenModules(root string, modules []ParsedModule) *ast.Program {
	contexts := moduleContexts(modules)
	program := &ast.Program{}
	for _, module := range modules {
		ctx := contexts[module.Path]
		ctx.root = module.Path == root
		for _, decl := range module.Program.Decls {
			if _, ok := decl.(*ast.ImportDecl); ok {
				continue
			}
			program.Decls = append(program.Decls, rewriteDecl(root, module.Path, ctx, decl))
		}
	}
	return program
}

type moduleContext struct {
	path    string
	root    bool
	imports map[string]string
	decls   map[string]bool
}

// moduleContexts builds per-module alias and local declaration maps.
func moduleContexts(modules []ParsedModule) map[string]moduleContext {
	out := map[string]moduleContext{}
	for _, module := range modules {
		ctx := moduleContext{path: module.Path, imports: map[string]string{}, decls: map[string]bool{}}
		for _, imported := range module.Imports {
			ctx.imports[lastSegment(imported)] = imported
		}
		for _, decl := range module.Program.Decls {
			if name := declName(decl); name != "" {
				ctx.decls[name] = true
			}
		}
		out[module.Path] = ctx
	}
	return out
}

// rewriteDecl qualifies module-bound names before single-program checks.
func rewriteDecl(root string, modulePath string, ctx moduleContext, decl ast.Decl) ast.Decl {
	rootModule := modulePath == root
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		d.Name = rewriteDeclName(rootModule, modulePath, d.Name)
		rewriteFunction(ctx, d)
	case *ast.StructDecl:
		d.Name = rewriteTypeName(rootModule, modulePath, d.Name)
		for idx := range d.Fields {
			d.Fields[idx].TypeName = rewriteType(ctx, d.Fields[idx].TypeName)
		}
	case *ast.EnumDecl:
		d.Name = rewriteTypeName(rootModule, modulePath, d.Name)
	case *ast.UnionDecl:
		d.Name = rewriteTypeName(rootModule, modulePath, d.Name)
		for idx := range d.Variants {
			d.Variants[idx].Payload = rewriteType(ctx, d.Variants[idx].Payload)
		}
	case *ast.ContractDecl:
		d.Name = rewriteTypeName(rootModule, modulePath, d.Name)
		for _, method := range d.Methods {
			rewriteFunction(ctx, method)
		}
	case *ast.ImplDecl:
		d.TypeName = rewriteType(ctx, d.TypeName)
		for _, method := range d.Methods {
			rewriteFunction(ctx, method)
		}
	case *ast.SatisfyDecl:
		d.ContractName = rewriteType(ctx, d.ContractName)
		d.TypeName = rewriteType(ctx, d.TypeName)
	}
	return decl
}

// rewriteFunction qualifies signature and body references.
func rewriteFunction(ctx moduleContext, fn *ast.FunctionDecl) {
	for idx := range fn.Params {
		fn.Params[idx].TypeName = rewriteType(ctx, fn.Params[idx].TypeName)
	}
	fn.ReturnType = rewriteType(ctx, fn.ReturnType)
	if fn.Body != nil {
		rewriteBlock(ctx, fn.Body)
	}
}

// rewriteBlock qualifies expressions inside a block.
func rewriteBlock(ctx moduleContext, block *ast.BlockStmt) {
	for _, stmt := range block.Statements {
		rewriteStmt(ctx, stmt)
	}
}

// rewriteStmt qualifies expressions inside one statement.
func rewriteStmt(ctx moduleContext, stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		s.Value = rewriteExpr(ctx, s.Value)
	case *ast.AssignStmt:
		s.Target = rewriteExpr(ctx, s.Target)
		s.Value = rewriteExpr(ctx, s.Value)
	case *ast.ReturnStmt:
		s.Value = rewriteExpr(ctx, s.Value)
	case *ast.IfStmt:
		s.Condition = rewriteExpr(ctx, s.Condition)
		rewriteBlock(ctx, s.Consequence)
		if s.Alternative != nil {
			rewriteBlock(ctx, s.Alternative)
		}
	case *ast.WhileStmt:
		s.Condition = rewriteExpr(ctx, s.Condition)
		rewriteBlock(ctx, s.Body)
	case *ast.ForStmt:
		s.Start = rewriteExpr(ctx, s.Start)
		s.End = rewriteExpr(ctx, s.End)
		rewriteBlock(ctx, s.Body)
	case *ast.MatchStmt:
		s.Value = rewriteExpr(ctx, s.Value)
		for idx := range s.Arms {
			rewriteStmt(ctx, s.Arms[idx].Body)
		}
	case *ast.UnsafeStmt:
		rewriteBlock(ctx, s.Body)
	case *ast.ComptimeIfStmt:
		s.Condition = rewriteExpr(ctx, s.Condition)
		rewriteBlock(ctx, s.Consequence)
		if s.Alternative != nil {
			rewriteBlock(ctx, s.Alternative)
		}
	case *ast.ExprStmt:
		s.Expr = rewriteExpr(ctx, s.Expr)
	}
}

// rewriteExpr qualifies namespace expressions and local function references.
func rewriteExpr(ctx moduleContext, expr ast.Expression) ast.Expression {
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.IdentExpr:
		if ctx.decls[e.Name] {
			return &ast.IdentExpr{Name: localFunctionName(ctx, e.Name)}
		}
	case *ast.PrefixExpr:
		e.Right = rewriteExpr(ctx, e.Right)
	case *ast.BinaryExpr:
		rewriteBinaryExpr(ctx, e)
	case *ast.CallExpr:
		rewriteCallExpr(ctx, e)
	case *ast.TypeApplyExpr:
		e.Callee = rewriteExpr(ctx, e.Callee)
		e.TypeArg = rewriteType(ctx, e.TypeArg)
	case *ast.CastExpr:
		e.TargetType = rewriteType(ctx, e.TargetType)
		e.Value = rewriteExpr(ctx, e.Value)
	case *ast.TryExpr:
		e.Value = rewriteExpr(ctx, e.Value)
	default:
		return rewriteAggregateExpr(ctx, expr)
	}
	return expr
}

// rewriteAggregateExpr qualifies aggregate, namespace, and control expressions.
func rewriteAggregateExpr(ctx moduleContext, expr ast.Expression) ast.Expression {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		rewriteIndexExpr(ctx, e)
	case *ast.ArenaNewExpr:
		e.TypeName = rewriteType(ctx, e.TypeName)
	case *ast.StructLiteralExpr:
		rewriteStructLiteralExpr(ctx, e)
	case *ast.FieldExpr:
		if e.Namespace {
			return rewriteNamespaceExpr(ctx, e)
		}
		e.Receiver = rewriteExpr(ctx, e.Receiver)
	case *ast.DerefExpr:
		e.Receiver = rewriteExpr(ctx, e.Receiver)
	case *ast.IfStmt:
		rewriteStmt(ctx, e)
	case *ast.MatchStmt:
		rewriteStmt(ctx, e)
	case *ast.ComptimeExpr:
		e.Expr = rewriteExpr(ctx, e.Expr)
	}
	return expr
}

// rewriteBinaryExpr qualifies both operands of a binary expression.
func rewriteBinaryExpr(ctx moduleContext, expr *ast.BinaryExpr) {
	expr.Left = rewriteExpr(ctx, expr.Left)
	expr.Right = rewriteExpr(ctx, expr.Right)
}

// rewriteCallExpr qualifies a callee and its arguments.
func rewriteCallExpr(ctx moduleContext, expr *ast.CallExpr) {
	expr.Callee = rewriteExpr(ctx, expr.Callee)
	for idx := range expr.Args {
		expr.Args[idx] = rewriteExpr(ctx, expr.Args[idx])
	}
}

// rewriteIndexExpr qualifies checked index and slice operands.
func rewriteIndexExpr(ctx moduleContext, expr *ast.IndexExpr) {
	expr.Target = rewriteExpr(ctx, expr.Target)
	expr.Index = rewriteExpr(ctx, expr.Index)
	expr.Start = rewriteExpr(ctx, expr.Start)
	expr.End = rewriteExpr(ctx, expr.End)
}

// rewriteStructLiteralExpr qualifies a struct literal and field values.
func rewriteStructLiteralExpr(ctx moduleContext, expr *ast.StructLiteralExpr) {
	expr.TypeName = rewriteType(ctx, expr.TypeName)
	for idx := range expr.Fields {
		expr.Fields[idx].Value = rewriteExpr(ctx, expr.Fields[idx].Value)
	}
}

// rewriteNamespaceExpr expands imported aliases to full package paths.
func rewriteNamespaceExpr(ctx moduleContext, expr *ast.FieldExpr) ast.Expression {
	parts, ok := namespaceParts(expr)
	if !ok || len(parts) == 0 {
		expr.Receiver = rewriteExpr(ctx, expr.Receiver)
		return expr
	}
	if imported, ok := ctx.imports[parts[0]]; ok {
		full := append(strings.Split(imported, "::"), parts[1:]...)
		return namespaceExpr(full)
	}
	if parts[0] == "std" {
		return expr
	}
	if ctx.decls[parts[0]] {
		full := append(strings.Split(ctx.path, "::"), parts...)
		return namespaceExpr(full)
	}
	return expr
}

// namespaceParts returns a namespace chain as string parts.
func namespaceParts(expr ast.Expression) ([]string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return []string{e.Name}, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return nil, false
		}
		left, ok := namespaceParts(e.Receiver)
		if !ok {
			return nil, false
		}
		return append(left, e.Name), true
	default:
		return nil, false
	}
}

// namespaceExpr builds a namespace AST chain from parts.
func namespaceExpr(parts []string) ast.Expression {
	if len(parts) == 0 {
		return &ast.IdentExpr{Name: "<error>"}
	}
	if len(parts) >= 2 && startsUpper(parts[len(parts)-2]) {
		return &ast.FieldExpr{
			Receiver: &ast.IdentExpr{Name: strings.Join(parts[:len(parts)-1], "::")},
			Name:     parts[len(parts)-1], Namespace: true,
		}
	}
	var expr ast.Expression = &ast.IdentExpr{Name: parts[0]}
	for _, part := range parts[1:] {
		expr = &ast.FieldExpr{Receiver: expr, Name: part, Namespace: true}
	}
	return expr
}

// rewriteType qualifies local and imported type spellings.
func rewriteType(ctx moduleContext, typ string) string {
	if typ == "" {
		return ""
	}
	for _, prefix := range []string{"!Error!", "!", "?", "&mut ", "&", "[]", "const "} {
		if strings.HasPrefix(typ, prefix) {
			return prefix + rewriteType(ctx, strings.TrimPrefix(typ, prefix))
		}
	}
	base, args, ok := splitGeneric(typ)
	if ok {
		rewritten := make([]string, 0, len(args))
		for _, arg := range args {
			rewritten = append(rewritten, rewriteType(ctx, arg))
		}
		return rewriteTypeBase(ctx, base) + "<" + strings.Join(rewritten, ", ") + ">"
	}
	return rewriteTypeBase(ctx, typ)
}

// rewriteTypeBase qualifies one non-generic type base.
func rewriteTypeBase(ctx moduleContext, typ string) string {
	if strings.HasPrefix(typ, "std::") || isPrimitiveType(typ) {
		return typ
	}
	parts := strings.Split(typ, "::")
	if imported, ok := ctx.imports[parts[0]]; ok {
		return imported + "::" + strings.Join(parts[1:], "::")
	}
	if ctx.decls[parts[0]] {
		return ctx.path + "::" + strings.Join(parts, "::")
	}
	return typ
}

// splitGeneric separates Base<Args> type spellings without parsing full syntax.
func splitGeneric(typ string) (string, []string, bool) {
	start := strings.Index(typ, "<")
	if start < 0 || !strings.HasSuffix(typ, ">") {
		return "", nil, false
	}
	body := typ[start+1 : len(typ)-1]
	args := splitTopLevelComma(body)
	if len(args) == 0 {
		return "", nil, false
	}
	return typ[:start], args, true
}

// splitTopLevelComma splits generic arguments at top-level commas.
func splitTopLevelComma(body string) []string {
	parts := []string{}
	depth := 0
	start := 0
	for idx, ch := range body {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(body[start:idx]))
				start = idx + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(body[start:]))
	return parts
}

// rewriteDeclName returns a function key visible to existing checkers.
func rewriteDeclName(root bool, modulePath string, name string) string {
	if root {
		return name
	}
	return internalFunctionName(modulePath, name)
}

// rewriteTypeName returns a top-level type name visible to existing checkers.
func rewriteTypeName(root bool, modulePath string, name string) string {
	if root {
		return name
	}
	return modulePath + "::" + name
}

// internalFunctionName converts app::lexer::lex to app.lexer.lex.
func internalFunctionName(modulePath string, name string) string {
	return strings.ReplaceAll(modulePath, "::", ".") + "." + name
}

// localFunctionName returns the checker key for a same-module function reference.
func localFunctionName(ctx moduleContext, name string) string {
	if ctx.root {
		return name
	}
	return internalFunctionName(ctx.path, name)
}

// declName returns the source name of a top-level declaration.
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
	default:
		return ""
	}
}

// declPublic reports whether a declaration is visible outside its module.
func declPublic(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return d.Public
	case *ast.StructDecl:
		return d.Public
	case *ast.EnumDecl:
		return d.Public
	case *ast.UnionDecl:
		return d.Public
	case *ast.ContractDecl:
		return d.Public
	default:
		return false
	}
}

// hasTopLevelDecl reports whether a module declares a source-level name.
func hasTopLevelDecl(program *ast.Program, name string) bool {
	for _, decl := range program.Decls {
		if declName(decl) == name {
			return true
		}
	}
	return false
}

// lastSegment returns the final module path segment.
func lastSegment(path string) string {
	parts := strings.Split(path, "::")
	return parts[len(parts)-1]
}

// sortedStrings returns a stable sorted copy.
func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// isPrimitiveType reports type names that never need module qualification.
func isPrimitiveType(typ string) bool {
	switch typ {
	case "bool", "void", "Io", "i8", "i16", "i32", "i64", "u8", "u16", "u32",
		"u64", "usize", "isize", "f32", "f64", "Function":
		return true
	default:
		return false
	}
}

// startsUpper reports whether name starts with an ASCII uppercase letter.
func startsUpper(name string) bool {
	if name == "" {
		return false
	}
	return 'A' <= name[0] && name[0] <= 'Z'
}
