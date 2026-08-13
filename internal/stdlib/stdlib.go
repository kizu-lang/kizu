// Package stdlib loads Kizu std wrapper declarations for compiler frontends.
package stdlib

import (
	"fmt"
	"github.com/kizu-lang/kizu/internal/manifest"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/token"
	"github.com/kizu-lang/kizu/internal/typ"
)

// DeclsForSource loads std wrapper declarations referenced by one source.
func DeclsForSource(source string) ([]ast.Decl, []parser.Diagnostic, error) {
	modules, err := ResolveModules(source)
	if err != nil {
		return nil, nil, err
	}
	if len(modules) == 0 {
		return nil, nil, nil
	}
	return ParseDecls(modules)
}

// DeclsForSources loads std wrapper declarations referenced by many sources.
func DeclsForSources(sources []string) ([]ast.Decl, []parser.Diagnostic, error) {
	var combined strings.Builder
	for _, source := range sources {
		combined.WriteString(source)
		combined.WriteByte('\n')
	}
	return DeclsForSource(combined.String())
}

// DeclsForSourcePath loads std wrapper declarations referenced by one file.
func DeclsForSourcePath(path string) ([]ast.Decl, []parser.Diagnostic, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return DeclsForSource(string(source))
}

// ResolveModules returns std modules in dependency-before-dependent order.
func ResolveModules(source string) ([]string, error) {
	referenced := referencedModules(source)
	if len(referenced) == 0 {
		return nil, nil
	}
	exports, err := loadModuleExports()
	if err != nil {
		return nil, err
	}
	resolver := &moduleResolver{
		visited:  map[string]bool{},
		visiting: map[string]bool{},
	}
	for _, module := range referenced {
		if !exports[module] {
			return nil, fmt.Errorf("std module `%s` is not exported", modulePath(module))
		}
		if err := resolver.visit(module); err != nil {
			return nil, err
		}
	}
	return resolver.modules, nil
}

// ParseDecls loads selected std wrappers from Kizu source.
func ParseDecls(modules []string) ([]ast.Decl, []parser.Diagnostic, error) {
	decls := []ast.Decl{}
	for _, module := range modules {
		moduleDecls, errs, err := parseModuleDecls(module)
		if err != nil || len(errs) > 0 {
			return nil, errs, err
		}
		decls = append(decls, moduleDecls...)
	}
	return decls, nil, nil
}

// FindRepoFile searches for a repository-relative file from common dev roots.
func FindRepoFile(name string) (string, error) {
	for _, start := range repoSearchRoots() {
		if start == "" {
			continue
		}
		if path, ok := findRepoFileFrom(start, name); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("open %s: no such file or directory", name)
}

// repoSearchRoots returns roots that work from repo commands, dev binaries, and tests.
func repoSearchRoots() []string {
	roots := []string{}
	if envRoot := os.Getenv("KIZU_REPO_ROOT"); envRoot != "" {
		roots = append(roots, envRoot)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	if _, sourceFile, _, ok := runtime.Caller(0); ok {
		roots = append(roots, filepath.Dir(sourceFile))
	}
	return roots
}

// findRepoFileFrom searches upward from start for name.
func findRepoFileFrom(start string, name string) (string, bool) {
	dir := filepath.Clean(start)
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// loadModuleExports reads the std manifest package surface.
func loadModuleExports() (map[string]bool, error) {
	path, err := FindRepoFile("std/kizu.toml")
	if err != nil {
		return nil, err
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	manifest, err := manifest.ParseStdManifest(string(source))
	if err != nil {
		return nil, err
	}
	return moduleExports(manifest.Exports)
}

// moduleExports converts package-qualified std paths to resolver module names.
func moduleExports(paths []string) (map[string]bool, error) {
	exports := map[string]bool{}
	for _, path := range paths {
		module, ok := strings.CutPrefix(path, "std::")
		if !ok || module == "" {
			return nil, fmt.Errorf("std manifest error: invalid export `%s`", path)
		}
		exports[module] = true
	}
	return exports, nil
}

type moduleResolver struct {
	modules  []string
	visited  map[string]bool
	visiting map[string]bool
}

// visit adds one std module after recursively adding its std-source dependencies.
func (r *moduleResolver) visit(module string) error {
	if r.visited[module] {
		return nil
	}
	if r.visiting[module] {
		return nil
	}
	r.visiting[module] = true
	source, err := readModuleSource(module)
	if err != nil {
		return err
	}
	for _, dependency := range referencedModules(source) {
		if err := r.visit(dependency); err != nil {
			return err
		}
	}
	r.visiting[module] = false
	r.visited[module] = true
	r.modules = append(r.modules, module)
	return nil
}

// readModuleSource reads one std source module by its short module name.
func readModuleSource(module string) (string, error) {
	path, err := FindRepoFile(moduleFile(module))
	if err != nil {
		return "", err
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(source), nil
}

// referencedModules scans source text for std module namespace references.
func referencedModules(source string) []string {
	refs := scanModuleRefs(source)
	modules := []string{}
	for _, module := range sourceModuleOrder {
		if refs[module] {
			modules = append(modules, module)
		}
	}
	return modules
}

// scanModuleRefs records std namespace uses outside strings and comments.
func scanModuleRefs(source string) map[string]bool {
	refs := map[string]bool{}
	lex := lexer.New(source)
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			return refs
		}
		if tok.Type == token.Ident && tok.Literal == "std" {
			parts := readNamespaceParts(lex)
			recordModuleRefs(refs, parts)
		}
	}
}

// readNamespaceParts reads a namespace chain after the initial root name.
func readNamespaceParts(lex *lexer.Lexer) []string {
	parts := []string{}
	for {
		sep := lex.NextToken()
		if sep.Type != token.DoubleColon {
			return parts
		}
		part := lex.NextToken()
		if part.Type != token.Ident {
			return parts
		}
		parts = append(parts, part.Literal)
	}
}

// recordModuleRefs marks known std modules that prefix a namespace chain.
func recordModuleRefs(refs map[string]bool, parts []string) {
	for _, module := range sourceModuleOrder {
		moduleParts := strings.Split(module, "::")
		if hasModulePrefix(parts, moduleParts) {
			refs[module] = true
		}
	}
}

// hasModulePrefix reports whether parts start with module and name an item below it.
func hasModulePrefix(parts []string, module []string) bool {
	if len(parts) <= len(module) {
		return false
	}
	for idx := range module {
		if parts[idx] != module[idx] {
			return false
		}
	}
	return true
}

var sourceModuleOrder = []string{
	"mem",
	"array",
	"string",
	"fmt",
	"testing",
	"kizu::ast",
	"kizu::lexer",
	"kizu::diagnostic",
	"kizu::parser",
	"fs",
	"path_bits",
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

// parseModuleDecls loads one std wrapper module from Kizu source.
func parseModuleDecls(module string) ([]ast.Decl, []parser.Diagnostic, error) {
	path, err := FindRepoFile(moduleFile(module))
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
			renameStruct(module, d)
		case *ast.EnumDecl:
			renameEnum(module, d)
		case *ast.UnionDecl:
			renameUnion(module, d)
		case *ast.FunctionDecl:
			renameFunction(module, d)
			d.Std = true
		case *ast.ImplDecl:
			renameImpl(module, d)
		default:
			return nil, nil, fmt.Errorf("std %s error: unsupported declaration %T", module, decl)
		}
		decls = append(decls, decl)
	}
	return decls, nil, nil
}

// parsePath reads and parses a Kizu source file.
func parsePath(path string) (*ast.Program, []parser.Diagnostic, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	return program, p.Diagnostics(), nil
}

// moduleFile maps a std namespace module name to its source file path.
func moduleFile(module string) string {
	return "std/src/" + strings.ReplaceAll(module, "::", "/") + ".kizu"
}

// modulePath renders a resolver module name as its public namespace path.
func modulePath(module string) string {
	return "std::" + module
}

// renameStruct rewrites a std wrapper struct into its qualified form.
func renameStruct(module string, decl *ast.StructDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
	for idx := range decl.Fields {
		decl.Fields[idx].TypeName = qualifyTypeName(module, decl.Fields[idx].TypeName)
	}
}

// renameEnum rewrites a std wrapper enum into its qualified form.
func renameEnum(module string, decl *ast.EnumDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
}

// renameUnion rewrites a std wrapper union into its qualified form.
func renameUnion(module string, decl *ast.UnionDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
	for idx := range decl.Variants {
		decl.Variants[idx].Payload = qualifyTypeName(module, decl.Variants[idx].Payload)
	}
}

// renameFunction rewrites a std wrapper function into its qualified form.
func renameFunction(module string, fn *ast.FunctionDecl) {
	fn.Name = "std." + strings.ReplaceAll(module, "::", ".") + "." + fn.Name
	renameFunctionTypes(module, fn)
	renameBlockExprs(module, fn.Body)
}

// renameImpl rewrites a std wrapper impl block into its qualified form.
func renameImpl(module string, decl *ast.ImplDecl) {
	decl.TypeName = qualifyTypeName(module, decl.TypeName)
	for _, method := range decl.Methods {
		method.Public = false
		method.Std = true
		renameFunctionTypes(module, method)
		renameBlockExprs(module, method.Body)
	}
}

// renameFunctionTypes qualifies module-local std type names in one function.
func renameFunctionTypes(module string, fn *ast.FunctionDecl) {
	fn.ReturnType = qualifyTypeName(module, fn.ReturnType)
	for idx := range fn.Params {
		fn.Params[idx].TypeName = qualifyTypeName(module, fn.Params[idx].TypeName)
	}
}

// renameBlockExprs qualifies module-local type names inside std function bodies.
func renameBlockExprs(module string, block *ast.BlockStmt) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		renameStmtExprs(module, stmt)
	}
}

// renameStmtExprs qualifies module-local type names inside one statement.
func renameStmtExprs(module string, stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		renameExpr(module, s.Value)
	case *ast.AssignStmt:
		renameExpr(module, s.Target)
		renameExpr(module, s.Value)
	case *ast.ReturnStmt:
		renameExpr(module, s.Value)
	case *ast.ExprStmt:
		renameExpr(module, s.Expr)
	case *ast.IfStmt:
		renameExpr(module, s.Condition)
		renameBlockExprs(module, s.Consequence)
		renameBlockExprs(module, s.Alternative)
	case *ast.WhileStmt:
		renameExpr(module, s.Condition)
		renameBlockExprs(module, s.Body)
	case *ast.ForStmt:
		renameExpr(module, s.Start)
		renameExpr(module, s.End)
		renameBlockExprs(module, s.Body)
	case *ast.MatchStmt:
		renameExpr(module, s.Value)
		for _, arm := range s.Arms {
			renameStmtExprs(module, arm.Body)
		}
	case *ast.UnsafeStmt:
		renameBlockExprs(module, s.Body)
	case *ast.ComptimeIfStmt:
		renameExpr(module, s.Condition)
		renameBlockExprs(module, s.Consequence)
		renameBlockExprs(module, s.Alternative)
	}
}

// renameExpr qualifies module-local struct literal type names recursively.
func renameExpr(module string, expr ast.Expression) {
	if renameTypeExpr(module, expr) {
		return
	}
	switch e := expr.(type) {
	case *ast.StructLiteralExpr:
		e.TypeName = qualifyTypeName(module, e.TypeName)
		for idx := range e.Fields {
			renameExpr(module, e.Fields[idx].Value)
		}
	case *ast.PrefixExpr:
		renameExpr(module, e.Right)
	case *ast.BinaryExpr:
		renameExpr(module, e.Left)
		renameExpr(module, e.Right)
	case *ast.ComptimeExpr:
		renameExpr(module, e.Expr)
	case *ast.CallExpr:
		renameExpr(module, e.Callee)
		for _, arg := range e.Args {
			renameExpr(module, arg)
		}
	case *ast.TryExpr:
		renameExpr(module, e.Value)
	case *ast.IndexExpr:
		renameExpr(module, e.Target)
		renameExpr(module, e.Index)
		renameExpr(module, e.Start)
		renameExpr(module, e.End)
	case *ast.FieldExpr:
		if e.Namespace {
			renameNamespaceReceiver(module, e)
			return
		}
		renameExpr(module, e.Receiver)
	case *ast.DerefExpr:
		renameExpr(module, e.Receiver)
	}
}

// renameTypeExpr qualifies type-bearing expression nodes.
func renameTypeExpr(module string, expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.TypeApplyExpr:
		renameExpr(module, e.Callee)
		e.TypeArg = qualifyTypeName(module, e.TypeArg)
	case *ast.CastExpr:
		e.TargetType = qualifyTypeName(module, e.TargetType)
		renameExpr(module, e.Value)
	case *ast.ArenaNewExpr:
		e.TypeName = qualifyTypeName(module, e.TypeName)
		renameExpr(module, e.Allocator)
	default:
		return false
	}
	return true
}

// renameNamespaceReceiver qualifies module-local namespace type receivers.
func renameNamespaceReceiver(module string, expr *ast.FieldExpr) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		ident.Name = qualifyTypeName(module, ident.Name)
		return
	}
	renameExpr(module, expr.Receiver)
}

// qualifyTypeName maps local std wrapper type names to public std names.
func qualifyTypeName(module string, typ string) string {
	if strings.HasPrefix(typ, "!") {
		return "!" + qualifyTypeName(module, strings.TrimPrefix(typ, "!"))
	}
	if base, argsText, ok := splitGenericType(typ); ok {
		args, argsOK := splitGenericArgs(argsText)
		if !argsOK {
			return typ
		}
		for idx := range args {
			args[idx] = qualifyTypeName(module, args[idx])
		}
		return base + "<" + strings.Join(args, ", ") + ">"
	}
	if qualified, ok := qualifySimpleType(module, typ); ok {
		return qualified
	}
	return typ
}

// qualifySimpleType maps one non-generic std-local type name.
func qualifySimpleType(module string, typ string) (string, bool) {
	if module == "string" && typ == "String" {
		return "std::string::String", true
	}
	if module == "fs" && (typ == "DirEntry" || typ == "Metadata") {
		return "std::fs::" + typ, true
	}
	if module == "kizu::ast" {
		switch typ {
		case "SourceFile", "Span", "TokenId", "SymbolId", "BinaryOp", "PrefixOp", "ChildRange",
			"NodeId", "Ast", "AstNode", "AstData", "ProgramNode", "IntNode",
			"StringNode", "TypeNameNode", "VarNode", "BoolNode", "PrefixNode",
			"BinaryNode", "FieldExprNode", "DerefExprNode", "CallNode", "TypeApplyExprNode",
			"CastExprNode", "IndexExprNode", "StructLiteralExprNode", "StructFieldInitNode",
			"ArenaNewExprNode", "TryExprNode", "ComptimeExprNode",
			"BlockNode", "IfNode", "LetNode", "AssignNode", "ReturnNode", "DeferNode",
			"ErrDeferNode", "ExprStmtNode",
			"WhileNode", "ForNode", "BreakNode", "ContinueNode", "ParamNode", "FieldNode",
			"StructDeclNode", "ImportDeclNode", "EnumDeclNode", "UnionDeclNode",
			"ImplDeclNode", "UnionVariantNode", "MatchNode", "MatchArmNode", "UnsafeNode", "ComptimeIfNode",
			"FnDeclNode", "ContractDeclNode", "SynthLatchNode",
			"ParseResult":
			return "std::kizu::ast::" + typ, true
		}
	}
	if module == "kizu::lexer" {
		switch typ {
		case "TokenKind", "Token", "Position":
			return "std::kizu::lexer::" + typ, true
		}
	}
	if module == "kizu::diagnostic" {
		switch typ {
		case "FileSpan", "RelatedSpan", "Diagnostic":
			return "std::kizu::diagnostic::" + typ, true
		}
	}
	return "", false
}

// splitGenericType extracts base and raw arguments from a simple type string.
func splitGenericType(name string) (string, string, bool) {
	return typ.SplitApply(name)
}

// splitGenericArgs splits top-level generic arguments for std type rewriting.
func splitGenericArgs(arg string) ([]string, bool) {
	args, err := typ.SplitArgs(arg)
	if err != nil {
		return nil, false
	}
	return args, true
}
