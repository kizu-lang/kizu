// Package stdlib loads Kizu std wrapper declarations for compiler frontends.
package stdlib

import (
	"fmt"
	"github.com/kizu-lang/kizu/internal/manifest"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

// Root is the namespace the standard library lives under. A user package may
// not be named this, so a path starting here always means std.
const Root = "std"

// Importable reports whether path names a std module a program outside std may
// import, and returns the modules it pulls in with it. A module that is not
// exported is package-internal: std reaches it, nothing else does.
func Importable(path string) ([]string, bool, error) {
	if path != Root && !strings.HasPrefix(path, Root+"::") {
		return nil, false, nil
	}
	exports, err := loadModuleExports()
	if err != nil {
		return nil, false, err
	}
	wanted, ok := importedNames(path, exports)
	if !ok {
		return nil, false, nil
	}
	modules, err := resolveAll(wanted)
	if err != nil {
		return nil, false, err
	}
	return modules, true, nil
}

// importedNames returns the std modules one import path names, and whether the
// path names anything at all. What it names and what has to be read are not the
// same: a module the compiler provides itself has no source to load, so it is
// importable and contributes no declarations. Importing the root names every
// module under it, because that is what a path through the root can reach.
func importedNames(path string, exports map[string]bool) ([]string, bool) {
	if path == Root {
		return sortedExports(exports), true
	}
	module, ok := strings.CutPrefix(path, Root+"::")
	if !ok || !exports[module] {
		return nil, false
	}
	if !hasSource(module) {
		return nil, true
	}
	return []string{module}, true
}

// hasSource reports whether a std module is written in Kizu. One that is not is
// provided by the compiler, and there is nothing to parse for it.
func hasSource(module string) bool {
	_, err := FindLibFile(moduleFile(module))
	return err == nil
}

// resolveAll visits modules and everything they are built on, dependency first.
func resolveAll(modules []string) ([]string, error) {
	resolver := &moduleResolver{visited: map[string]bool{}, visiting: map[string]bool{}}
	for _, module := range modules {
		if err := resolver.visit(module); err != nil {
			return nil, err
		}
	}
	return resolver.modules, nil
}

// declaredImports returns the std modules a source declares it imports. It
// reads declarations rather than uses: what a file depends on is what its
// import list says.
func declaredImports(source string, exports map[string]bool) []string {
	modules := []string{}
	for _, path := range stdImportPaths(source) {
		names, ok := importedNames(path, exports)
		if !ok {
			continue
		}
		modules = append(modules, names...)
	}
	return modules
}

// stdImportPaths returns the std paths a source declares it imports.
func stdImportPaths(source string) []string {
	paths := []string{}
	lex := lexer.New(source)
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			return paths
		}
		if tok.Type != token.Import {
			continue
		}
		root := lex.NextToken()
		if root.Type != token.Ident || root.Literal != Root {
			continue
		}
		paths = append(paths, strings.Join(append([]string{Root}, readNamespaceParts(lex)...), "::"))
	}
}

// sortedExports lists exported modules in the order their sources are loaded,
// so a program that imports the root sees them in dependency order.
func sortedExports(exports map[string]bool) []string {
	modules := make([]string, 0, len(exports))
	for _, module := range sourceModuleOrder {
		if exports[module] {
			modules = append(modules, module)
		}
	}
	return modules
}

// ResolveModules returns std modules in dependency-before-dependent order.
// TODO(import-migration): drop referencedModules once every source declares its
// std imports. Until then a file is served by whichever of the two it uses.
func ResolveModules(source string) ([]string, error) {
	exports, err := loadModuleExports()
	if err != nil {
		return nil, err
	}
	referenced := referencedModules(source)
	for _, module := range referenced {
		if !exports[module] {
			return nil, fmt.Errorf("std module `%s` is not exported", modulePath(module))
		}
	}
	wanted := append(declaredImports(source, exports), referenced...)
	if len(wanted) == 0 {
		return nil, nil
	}
	return resolveAll(wanted)
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

// LibDirEnv names the environment variable that overrides where the library
// tree is. A caller that already knows the path passes it with SetLibDir; this
// is for the ones that do not run the CLI, and for a development shell.
const LibDirEnv = "KIZU_LIB_DIR"

// libDir is the library tree this process reads std from, once decided.
var libDir struct {
	sync.Once
	path string
	err  error
}

// SetLibDir points this process at a library tree, overriding what it would
// otherwise find. It has to be called before anything reads std.
func SetLibDir(path string) {
	libDir.Do(func() {})
	libDir.path, libDir.err = path, nil
}

// LibDir returns the library tree std is read from. There is one rule: the
// caller says where it is, or it sits next to the running binary. Nothing is
// searched for, and the current directory is never consulted -- a program has
// to mean the same thing whatever directory it is compiled from.
func LibDir() (string, error) {
	libDir.Do(func() { libDir.path, libDir.err = resolveLibDir() })
	return libDir.path, libDir.err
}

// resolveLibDir decides the library tree from the environment or the binary.
func resolveLibDir() (string, error) {
	if fromEnv := os.Getenv(LibDirEnv); fromEnv != "" {
		return fromEnv, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		resolved = executable
	}
	// <prefix>/bin/kizu -> <prefix>/lib/kizu
	return filepath.Join(filepath.Dir(filepath.Dir(resolved)), "lib", "kizu"), nil
}

// FindLibFile returns the path of one file inside the library tree.
func FindLibFile(name string) (string, error) {
	dir, err := LibDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf(
			"open %s: no such file or directory"+
				"\nhelp: the library tree is `%s`;"+
				" set %s or pass --lib-dir to point somewhere else",
			path, dir, LibDirEnv,
		)
	}
	return path, nil
}

// loadModuleExports reads the std manifest package surface.
func loadModuleExports() (map[string]bool, error) {
	path, err := FindLibFile("std/kizu.toml")
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
	path, err := FindLibFile(moduleFile(module))
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
	"fs",
	"path_bits",
	"path",
	"io",
	"process",
	"map",
}

// parseModuleDecls loads one std wrapper module from Kizu source.
func parseModuleDecls(module string) ([]ast.Decl, []parser.Diagnostic, error) {
	path, err := FindLibFile(moduleFile(module))
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
		case *ast.ErrorSetDecl:
			renameErrorSet(module, d)
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
	return filepath.Join("std", "src", strings.ReplaceAll(module, "::", "/")+".kizu")
}

// modulePath renders a resolver module name as its public namespace path.
func modulePath(module string) string {
	return "std::" + module
}

// renameStruct rewrites a std wrapper struct into its qualified form.
func renameStruct(module string, decl *ast.StructDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
	for idx := range decl.Fields {
		decl.Fields[idx].TypeName = qualifyType(module, decl.Fields[idx].TypeName)
	}
}

// renameEnum rewrites a std wrapper enum into its qualified form.
func renameEnum(module string, decl *ast.EnumDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
}

// renameErrorSet rewrites a std error set into its qualified form. The name
// comes from the module it is declared in rather than from a table of known std
// type names, so declaring one is all it takes to have it.
func renameErrorSet(module string, decl *ast.ErrorSetDecl) {
	decl.Name = modulePath(module) + "::" + decl.Name
}

// renameUnion rewrites a std wrapper union into its qualified form.
func renameUnion(module string, decl *ast.UnionDecl) {
	decl.Name = qualifyTypeName(module, decl.Name)
	for idx := range decl.Variants {
		decl.Variants[idx].Payload = qualifyType(module, decl.Variants[idx].Payload)
	}
}

// renameFunction rewrites a std wrapper function into its qualified form.
func renameFunction(module string, fn *ast.FunctionDecl) {
	fn.Name = modulePath(module) + "::" + fn.Name
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
	fn.ReturnType = qualifyType(module, fn.ReturnType)
	for idx := range fn.Params {
		fn.Params[idx].TypeName = qualifyType(module, fn.Params[idx].TypeName)
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
		e.TargetType = qualifyType(module, e.TargetType)
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

// qualifyType maps local std wrapper type names to public std names, wherever
// they stand in a type. Walking the structure reaches a name inside `[]T` and
// `&T` as well as inside `<...>`, which spelling-level rewriting did not.
func qualifyType(module string, t typ.Type) typ.Type {
	switch node := t.(type) {
	case nil:
		return nil
	case *typ.Name:
		args := make([]typ.Type, 0, len(node.Args))
		for _, arg := range node.Args {
			args = append(args, qualifyType(module, arg))
		}
		path := node.Path
		if qualified, ok := qualifySimpleType(module, strings.Join(path, "::")); ok {
			path = strings.Split(qualified, "::")
		}
		return &typ.Name{Path: path, Args: args}
	case *typ.Slice:
		return &typ.Slice{Elem: qualifyType(module, node.Elem)}
	case *typ.Borrow:
		return &typ.Borrow{Elem: qualifyType(module, node.Elem), Mut: node.Mut}
	case *typ.Optional:
		return &typ.Optional{Elem: qualifyType(module, node.Elem)}
	case *typ.Dyn:
		return &typ.Dyn{Contract: qualifyType(module, node.Contract)}
	case *typ.Const:
		return &typ.Const{Elem: qualifyType(module, node.Elem)}
	case *typ.ErrorUnion:
		out := &typ.ErrorUnion{Ok: qualifyType(module, node.Ok)}
		if node.Err != nil {
			out.Err = qualifyType(module, node.Err)
		}
		return out
	default:
		return t
	}
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

// ErrorSets returns every error set std declares, with the number each member
// lowers to. The runtime refers to them all whatever a program uses, so they
// come from std rather than from the modules one program happened to load.
func ErrorSets() (map[string]map[string]int, error) {
	errorSetsOnce.Do(func() {
		errorSetsCache, errorSetsErr = readErrorSets()
	})
	return errorSetsCache, errorSetsErr
}

var (
	errorSetsOnce      sync.Once
	errorSetsCache     map[string]map[string]int
	errorCodeBaseCache int
	errorSetsErr       error
)

// readErrorSets parses std once for the numbers its error set members lower to.
// Numbering is global: an error value is one integer, and that integer means
// the same member in every error union it travels through, so no union-to-union
// conversion exists. Code 0 is reserved for "no error".
func readErrorSets() (map[string]map[string]int, error) {
	sets := map[string]map[string]int{}
	code := 1
	for _, module := range sourceModuleOrder {
		decls, errs, err := parseModuleDecls(module)
		if err != nil {
			return nil, err
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("std %s error: %v", module, errs[0])
		}
		for _, decl := range decls {
			set, ok := decl.(*ast.ErrorSetDecl)
			if !ok {
				continue
			}
			members := map[string]int{}
			for _, member := range set.Members {
				members[member] = code
				code++
			}
			sets[set.Name] = members
		}
	}
	errorCodeBaseCache = code
	return sets, nil
}

// ErrorCodeBase returns the first code available to error sets a program
// declares itself, one past the last code std claims. It is the counter
// readErrorSets stopped at, so the two cannot drift.
func ErrorCodeBase() (int, error) {
	if _, err := ErrorSets(); err != nil {
		return 0, err
	}
	return errorCodeBaseCache, nil
}
