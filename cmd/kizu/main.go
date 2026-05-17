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
	"github.com/kizu-lang/kizu/internal/project"
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
	case "selfhost-lex":
		return selfHostLexFile(args[0])
	case "selfhost-parse":
		return selfHostParseFile(args[0])
	case "selfhost-resolve":
		return selfHostResolveTarget(args[0])
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
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu selfhost-lex <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu selfhost-parse <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu selfhost-resolve <file|package>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu ir [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --emit-llvm [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target wasm32-wasi [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu cache <status|prune>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu why-rebuild <file|package>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu import-c-header <file>")
}

// parseFile parses a source file and prints its AST summary.
func parseFile(path string) error {
	if os.Getenv("KIZU_SELFHOST_PARSER") == "1" {
		return selfHostParseFile(path)
	}
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
	return interp.NewWithProcessArgs(os.Stdout, args).Run(program)
}

// checkFile parses a source file and runs static checks.
func checkFile(path string) error {
	if os.Getenv("KIZU_SELFHOST_RESOLVER") == "1" {
		return selfHostResolveTarget(path)
	}
	if packageTarget(path) {
		return checkPackageTarget(path)
	}
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

// packageTarget reports whether path names a package directory or manifest.
func packageTarget(path string) bool {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return true
	}
	return filepath.Base(path) == "kizu.toml"
}

// checkPackageTarget resolves a package and runs per-module static checks.
func checkPackageTarget(path string) error {
	baseDir := path
	if filepath.Base(path) == "kizu.toml" {
		baseDir = filepath.Dir(path)
	}
	pkg, err := loadPackageTarget(baseDir)
	if err != nil {
		return err
	}
	for _, module := range pkg.Modules {
		if err := checkPackageProgram(pkg, module); err != nil {
			return fmt.Errorf("%s: %w", module.Module.Path, err)
		}
	}
	_, _ = fmt.Println("check: ok")
	return nil
}

// checkPackageProgram checks one module with imported public type names visible.
func checkPackageProgram(pkg *project.Package, module project.ParsedModule) error {
	decls := project.ImportedPublicDecls(pkg, module)
	if err := types.New().WithExternalDecls(decls).
		Check(module.Program); err != nil {
		return err
	}
	return ownership.New().WithExternalDecls(decls).Check(module.Program)
}

// loadPackageTarget loads user packages and the compiler-owned std package.
func loadPackageTarget(baseDir string) (*project.Package, error) {
	if filepath.Base(baseDir) == "std" {
		return project.LoadStdPackage(baseDir)
	}
	return project.LoadPackage(baseDir)
}

// testFile runs a single Kizu test source or package component test target.
func testFile(path string, args []string) error {
	if packageTarget(path) {
		return testPackageTarget(path)
	}
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
	if err := interp.NewWithProcessArgs(os.Stdout, args).Run(program); err != nil {
		return err
	}
	_, _ = fmt.Println("test: ok")
	return nil
}

// testPackageTarget checks a package and its explicit component test modules.
func testPackageTarget(path string) error {
	baseDir := path
	if filepath.Base(path) == "kizu.toml" {
		baseDir = filepath.Dir(path)
	}
	pkg, err := loadPackageTarget(baseDir)
	if err != nil {
		return err
	}
	count := 0
	tests := []string{}
	for _, module := range pkg.Modules {
		if err := checkPackageProgram(pkg, module); err != nil {
			return err
		}
		count += componentTestCount(module)
		tests = append(tests, componentTestNames(module)...)
	}
	if count == 0 {
		return fmt.Errorf("test error: no package component tests found")
	}
	runner := interp.NewWithProcessArgs(os.Stdout, nil)
	runner.Register(packageRuntimeProgram(pkg))
	for _, testName := range tests {
		if err := runner.RunFunction(testName); err != nil {
			return fmt.Errorf("test error: %s: %w", testName, err)
		}
	}
	_, _ = fmt.Printf("test: ok (%d component tests)\n", count)
	return nil
}

// selfHostLexFile runs the Kizu-owned lexer bootstrap command for one file.
func selfHostLexFile(path string) error {
	return runSelfHostFunction("compiler.lex_file_snapshot", path)
}

// selfHostParseFile runs the Kizu-owned parser bootstrap command for one file.
func selfHostParseFile(path string) error {
	return runSelfHostFunction("compiler.parse_file_snapshot", path)
}

// selfHostResolveTarget runs the Kizu-owned resolver bootstrap target.
func selfHostResolveTarget(path string) error {
	source, err := selfHostResolverSourcePath(path)
	if err != nil {
		return err
	}
	return runSelfHostFunction("compiler.resolve_file_snapshot", source)
}

// selfHostResolverSourcePath maps a package target to its configured root source.
func selfHostResolverSourcePath(path string) (string, error) {
	if !packageTarget(path) {
		return path, nil
	}
	baseDir := path
	if filepath.Base(path) == "kizu.toml" {
		baseDir = filepath.Dir(path)
	}
	source, err := os.ReadFile(filepath.Join(baseDir, "kizu.toml"))
	if err != nil {
		return "", err
	}
	manifest, err := project.ParseManifest(string(source))
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, manifest.Root), nil
}

// runSelfHostFunction runs a named self-host package function with one path arg.
func runSelfHostFunction(name string, path string) error {
	selfhostPath, err := selfHostPackagePath()
	if err != nil {
		return err
	}
	pkg, err := project.LoadPackage(selfhostPath)
	if err != nil {
		return err
	}
	for _, module := range pkg.Modules {
		if err := checkPackageProgram(pkg, module); err != nil {
			return fmt.Errorf("%s: %w", module.Module.Path, err)
		}
	}
	runner := interp.NewWithProcessArgs(os.Stdout, []string{path})
	runner.Register(packageRuntimeProgram(pkg))
	return runner.RunFunction(name)
}

// selfHostPackagePath resolves the checked-in selfhost package from common CWDs.
func selfHostPackagePath() (string, error) {
	for _, path := range []string{"selfhost", "../../selfhost"} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("selfhost package was not found")
}

// componentTestCount returns explicitly named component test functions.
func componentTestCount(module project.ParsedModule) int {
	if !strings.HasSuffix(filepath.Base(module.Module.File), "_test.kizu") {
		return 0
	}
	count := 0
	for _, decl := range module.Program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if ok && strings.HasSuffix(fn.Name, "_test") {
			count++
		}
	}
	return count
}

// componentTestNames returns qualified package runtime test entry names.
func componentTestNames(module project.ParsedModule) []string {
	if !strings.HasSuffix(filepath.Base(module.Module.File), "_test.kizu") {
		return nil
	}
	names := []string{}
	prefix := runtimeModulePrefix(module)
	for _, decl := range module.Program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if ok && strings.HasSuffix(fn.Name, "_test") {
			names = append(names, prefix+"."+fn.Name)
		}
	}
	return names
}

// packageRuntimeProgram flattens package modules into explicit runtime names.
func packageRuntimeProgram(pkg *project.Package) *ast.Program {
	program := &ast.Program{}
	for _, module := range pkg.Modules {
		functions, types := runtimeLocalNames(module.Program)
		for _, decl := range module.Program.Decls {
			if clone := runtimeQualifiedDecl(module, decl, functions, types); clone != nil {
				program.Decls = append(program.Decls, clone)
			}
		}
	}
	return program
}

// runtimeLocalNames returns top-level names rewritten inside qualified clones.
func runtimeLocalNames(program *ast.Program) (map[string]bool, map[string]bool) {
	functions := map[string]bool{}
	types := map[string]bool{}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			functions[d.Name] = true
		case *ast.StructDecl:
			types[d.Name] = true
		case *ast.EnumDecl:
			types[d.Name] = true
		case *ast.UnionDecl:
			types[d.Name] = true
		}
	}
	return functions, types
}

// runtimeQualifiedDecl returns a module-qualified runtime declaration clone.
func runtimeQualifiedDecl(
	module project.ParsedModule,
	decl ast.Decl,
	functions map[string]bool,
	types map[string]bool,
) ast.Decl {
	prefix := runtimeModulePrefix(module)
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		clone.Body = runtimeBlockClone(d.Body, prefix, functions, types)
		return &clone
	case *ast.EnumDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		return &clone
	case *ast.UnionDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		return &clone
	default:
		return nil
	}
}

// runtimeBlockClone copies a block and qualifies local package runtime names.
func runtimeBlockClone(
	block *ast.BlockStmt,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) *ast.BlockStmt {
	if block == nil {
		return nil
	}
	clone := &ast.BlockStmt{Statements: make([]ast.Statement, 0, len(block.Statements))}
	for _, stmt := range block.Statements {
		clone.Statements = append(clone.Statements, runtimeStmtClone(stmt, prefix, functions, types))
	}
	return clone
}

// runtimeStmtClone copies one statement and rewrites contained expressions.
func runtimeStmtClone(
	stmt ast.Statement,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Statement {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		clone := *s
		clone.Value = runtimeExprClone(s.Value, prefix, functions, types)
		return &clone
	case *ast.AssignStmt:
		clone := *s
		clone.Target = runtimeExprClone(s.Target, prefix, functions, types)
		clone.Value = runtimeExprClone(s.Value, prefix, functions, types)
		return &clone
	case *ast.ReturnStmt:
		clone := *s
		clone.Value = runtimeExprClone(s.Value, prefix, functions, types)
		return &clone
	case *ast.ExprStmt:
		clone := *s
		clone.Expr = runtimeExprClone(s.Expr, prefix, functions, types)
		return &clone
	default:
		return runtimeControlStmtClone(stmt, prefix, functions, types)
	}
}

// runtimeControlStmtClone copies control-flow statements for package tests.
func runtimeControlStmtClone(
	stmt ast.Statement,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Statement {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		clone := *s
		clone.Condition = runtimeExprClone(s.Condition, prefix, functions, types)
		clone.Consequence = runtimeBlockClone(s.Consequence, prefix, functions, types)
		clone.Alternative = runtimeBlockClone(s.Alternative, prefix, functions, types)
		return &clone
	case *ast.WhileStmt:
		clone := *s
		clone.Condition = runtimeExprClone(s.Condition, prefix, functions, types)
		clone.Body = runtimeBlockClone(s.Body, prefix, functions, types)
		return &clone
	case *ast.UnsafeStmt:
		clone := *s
		clone.Body = runtimeBlockClone(s.Body, prefix, functions, types)
		return &clone
	default:
		return stmt
	}
}

// runtimeExprClone copies one expression and qualifies local runtime lookups.
func runtimeExprClone(
	expr ast.Expression,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.CallExpr:
		return runtimeCallClone(e, prefix, functions, types)
	case *ast.FieldExpr:
		return runtimeFieldClone(e, prefix, functions, types)
	case *ast.BinaryExpr:
		clone := *e
		clone.Left = runtimeExprClone(e.Left, prefix, functions, types)
		clone.Right = runtimeExprClone(e.Right, prefix, functions, types)
		return &clone
	case *ast.PrefixExpr:
		clone := *e
		clone.Right = runtimeExprClone(e.Right, prefix, functions, types)
		return &clone
	default:
		return runtimeOtherExprClone(expr, prefix, functions, types)
	}
}

// runtimeCallClone copies a call and qualifies same-module function callees.
func runtimeCallClone(
	expr *ast.CallExpr,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	if ident, ok := expr.Callee.(*ast.IdentExpr); ok && functions[ident.Name] {
		clone.Callee = &ast.IdentExpr{Name: prefix + "." + ident.Name}
	} else {
		clone.Callee = runtimeExprClone(expr.Callee, prefix, functions, types)
	}
	clone.Args = runtimeExprsClone(expr.Args, prefix, functions, types)
	return &clone
}

// runtimeFieldClone copies field and namespace expressions.
func runtimeFieldClone(
	expr *ast.FieldExpr,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	if expr.Namespace {
		if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && types[ident.Name] {
			clone.Receiver = &ast.IdentExpr{Name: prefix + "." + ident.Name}
			return &clone
		}
	}
	clone.Receiver = runtimeExprClone(expr.Receiver, prefix, functions, types)
	return &clone
}

// runtimeOtherExprClone copies less common expression nodes.
func runtimeOtherExprClone(
	expr ast.Expression,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	switch e := expr.(type) {
	case *ast.TryExpr:
		clone := *e
		clone.Value = runtimeExprClone(e.Value, prefix, functions, types)
		return &clone
	case *ast.StructLiteralExpr:
		clone := *e
		clone.Fields = runtimeFieldValuesClone(e.Fields, prefix, functions, types)
		return &clone
	default:
		return expr
	}
}

// runtimeExprsClone copies an expression list.
func runtimeExprsClone(
	exprs []ast.Expression,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) []ast.Expression {
	clone := make([]ast.Expression, 0, len(exprs))
	for _, expr := range exprs {
		clone = append(clone, runtimeExprClone(expr, prefix, functions, types))
	}
	return clone
}

// runtimeFieldValuesClone copies struct literal field initializers.
func runtimeFieldValuesClone(
	fields []ast.FieldValue,
	prefix string,
	functions map[string]bool,
	types map[string]bool,
) []ast.FieldValue {
	clone := make([]ast.FieldValue, 0, len(fields))
	for _, field := range fields {
		field.Value = runtimeExprClone(field.Value, prefix, functions, types)
		clone = append(clone, field)
	}
	return clone
}

// runtimeModulePrefix returns the import alias used by source expressions.
func runtimeModulePrefix(module project.ParsedModule) string {
	parts := strings.Split(module.Module.Path, "::")
	return parts[len(parts)-1]
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
		module, err := lowerTarget(path, opt)
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
		module, err := lowerTarget(path, opt)
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

// lowerTarget lowers either a single source file or a package directory.
func lowerTarget(path string, opt bool) (*ir.Module, error) {
	if packageTarget(path) {
		return lowerPackageTarget(path, opt)
	}
	return lowerFile(path, opt)
}

// lowerPackageTarget resolves, checks, flattens, and lowers a package.
func lowerPackageTarget(path string, opt bool) (*ir.Module, error) {
	baseDir := path
	if filepath.Base(path) == "kizu.toml" {
		baseDir = filepath.Dir(path)
	}
	pkg, err := loadPackageTarget(baseDir)
	if err != nil {
		return nil, err
	}
	for _, module := range pkg.Modules {
		if strings.HasSuffix(filepath.Base(module.Module.File), "_test.kizu") {
			continue
		}
		if err := checkPackageProgram(pkg, module); err != nil {
			return nil, fmt.Errorf("%s: %w", module.Module.Path, err)
		}
	}
	module, err := ir.Lower(packageBuildProgram(pkg))
	if err != nil {
		return nil, err
	}
	if opt {
		ir.Optimize(module)
	}
	return module, nil
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

// whyRebuildFile explains the cache state for a source file or package.
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

// packageBuildProgram returns a single AST that the current IR lowerer accepts.
func packageBuildProgram(pkg *project.Package) *ast.Program {
	program := &ast.Program{}
	for _, module := range pkg.Modules {
		if strings.HasSuffix(filepath.Base(module.Module.File), "_test.kizu") {
			continue
		}
		functions, types := runtimeLocalNames(module.Program)
		imports := runtimeImportPrefixes(module)
		prefix := runtimeModulePrefix(module)
		for _, decl := range module.Program.Decls {
			root := module.Module.Path == pkg.Graph.Root
			clone := buildQualifiedDecl(decl, prefix, root, imports, functions, types)
			if clone != nil {
				program.Decls = append(program.Decls, clone)
			}
		}
	}
	return program
}

// runtimeImportPrefixes maps import aliases to flattened runtime prefixes.
func runtimeImportPrefixes(module project.ParsedModule) map[string]string {
	imports := map[string]string{}
	for _, imported := range module.Imports {
		imports[imported.Name] = runtimePathPrefix(imported.Path)
	}
	return imports
}

// runtimePathPrefix returns the flattened name prefix for one module path.
func runtimePathPrefix(path string) string {
	parts := strings.Split(path, "::")
	return parts[len(parts)-1]
}

// buildQualifiedDecl returns a package-build declaration clone.
func buildQualifiedDecl(
	decl ast.Decl,
	prefix string,
	root bool,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Decl {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		if root && d.Name == "main" {
			clone.Name = "main"
		}
		clone.Params = buildParamsClone(d.Params, prefix, imports, types)
		clone.ReturnType = buildTypeName(d.ReturnType, prefix, imports, types)
		clone.Body = buildBlockClone(d.Body, prefix, imports, functions, types)
		return &clone
	case *ast.StructDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		clone.Fields = buildFieldsClone(d.Fields, prefix, imports, types)
		return &clone
	case *ast.EnumDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		return &clone
	case *ast.UnionDecl:
		clone := *d
		clone.Name = prefix + "." + d.Name
		clone.Variants = buildUnionVariantsClone(d.Variants, prefix, imports, types)
		return &clone
	default:
		return nil
	}
}

// buildParamsClone copies parameter type names into flattened package names.
func buildParamsClone(
	params []ast.Param,
	prefix string,
	imports map[string]string,
	types map[string]bool,
) []ast.Param {
	clone := make([]ast.Param, 0, len(params))
	for _, param := range params {
		param.TypeName = buildTypeName(param.TypeName, prefix, imports, types)
		clone = append(clone, param)
	}
	return clone
}

// buildFieldsClone copies struct field type names into flattened package names.
func buildFieldsClone(
	fields []ast.Field,
	prefix string,
	imports map[string]string,
	types map[string]bool,
) []ast.Field {
	clone := make([]ast.Field, 0, len(fields))
	for _, field := range fields {
		field.TypeName = buildTypeName(field.TypeName, prefix, imports, types)
		clone = append(clone, field)
	}
	return clone
}

// buildUnionVariantsClone copies union payload type names into flattened names.
func buildUnionVariantsClone(
	variants []ast.UnionVariant,
	prefix string,
	imports map[string]string,
	types map[string]bool,
) []ast.UnionVariant {
	clone := make([]ast.UnionVariant, 0, len(variants))
	for _, variant := range variants {
		variant.Payload = buildTypeName(variant.Payload, prefix, imports, types)
		clone = append(clone, variant)
	}
	return clone
}

// buildBlockClone copies a block and qualifies names for package build.
func buildBlockClone(
	block *ast.BlockStmt,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) *ast.BlockStmt {
	if block == nil {
		return nil
	}
	clone := &ast.BlockStmt{Statements: make([]ast.Statement, 0, len(block.Statements))}
	for _, stmt := range block.Statements {
		cloneStmt := buildStmtClone(stmt, prefix, imports, functions, types)
		clone.Statements = append(clone.Statements, cloneStmt)
	}
	return clone
}

// buildStmtClone copies one statement and qualifies contained expressions.
func buildStmtClone(
	stmt ast.Statement,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Statement {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		clone := *s
		clone.Value = buildExprClone(s.Value, prefix, imports, functions, types)
		return &clone
	case *ast.AssignStmt:
		clone := *s
		clone.Target = buildExprClone(s.Target, prefix, imports, functions, types)
		clone.Value = buildExprClone(s.Value, prefix, imports, functions, types)
		return &clone
	case *ast.ReturnStmt:
		clone := *s
		clone.Value = buildExprClone(s.Value, prefix, imports, functions, types)
		return &clone
	case *ast.ExprStmt:
		clone := *s
		clone.Expr = buildExprClone(s.Expr, prefix, imports, functions, types)
		return &clone
	default:
		return buildControlStmtClone(stmt, prefix, imports, functions, types)
	}
}

// buildControlStmtClone copies control statements for package build.
func buildControlStmtClone(
	stmt ast.Statement,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Statement {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		clone := *s
		clone.Condition = buildExprClone(s.Condition, prefix, imports, functions, types)
		clone.Consequence = buildBlockClone(s.Consequence, prefix, imports, functions, types)
		clone.Alternative = buildBlockClone(s.Alternative, prefix, imports, functions, types)
		return &clone
	case *ast.WhileStmt:
		clone := *s
		clone.Condition = buildExprClone(s.Condition, prefix, imports, functions, types)
		clone.Body = buildBlockClone(s.Body, prefix, imports, functions, types)
		return &clone
	case *ast.ForStmt:
		clone := *s
		clone.Start = buildExprClone(s.Start, prefix, imports, functions, types)
		clone.End = buildExprClone(s.End, prefix, imports, functions, types)
		clone.Body = buildBlockClone(s.Body, prefix, imports, functions, types)
		return &clone
	case *ast.UnsafeStmt:
		clone := *s
		clone.Body = buildBlockClone(s.Body, prefix, imports, functions, types)
		return &clone
	case *ast.ComptimeIfStmt:
		clone := *s
		clone.Condition = buildExprClone(s.Condition, prefix, imports, functions, types)
		clone.Consequence = buildBlockClone(s.Consequence, prefix, imports, functions, types)
		clone.Alternative = buildBlockClone(s.Alternative, prefix, imports, functions, types)
		return &clone
	default:
		return stmt
	}
}

// buildExprClone copies one expression and qualifies package names.
func buildExprClone(
	expr ast.Expression,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.CallExpr:
		return buildCallClone(e, prefix, imports, functions, types)
	case *ast.FieldExpr:
		return buildFieldClone(e, prefix, imports, functions, types)
	case *ast.BinaryExpr:
		return buildBinaryClone(e, prefix, imports, functions, types)
	case *ast.PrefixExpr:
		clone := *e
		clone.Right = buildExprClone(e.Right, prefix, imports, functions, types)
		return &clone
	case *ast.TryExpr:
		clone := *e
		clone.Value = buildExprClone(e.Value, prefix, imports, functions, types)
		return &clone
	case *ast.CastExpr:
		clone := *e
		clone.TargetType = buildTypeName(e.TargetType, prefix, imports, types)
		clone.Value = buildExprClone(e.Value, prefix, imports, functions, types)
		return &clone
	case *ast.StructLiteralExpr:
		return buildStructLiteralClone(e, prefix, imports, functions, types)
	case *ast.IfExpr:
		return buildIfExprClone(e, prefix, imports, functions, types)
	case *ast.ComptimeExpr:
		clone := *e
		clone.Expr = buildExprClone(e.Expr, prefix, imports, functions, types)
		return &clone
	case *ast.TypeApplyExpr:
		clone := *e
		clone.Callee = buildExprClone(e.Callee, prefix, imports, functions, types)
		clone.TypeArg = buildTypeName(e.TypeArg, prefix, imports, types)
		return &clone
	case *ast.ArenaNewExpr:
		clone := *e
		clone.TypeName = buildTypeName(e.TypeName, prefix, imports, types)
		return &clone
	default:
		return expr
	}
}

// buildBinaryClone copies a binary expression.
func buildBinaryClone(
	expr *ast.BinaryExpr,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	clone.Left = buildExprClone(expr.Left, prefix, imports, functions, types)
	clone.Right = buildExprClone(expr.Right, prefix, imports, functions, types)
	return &clone
}

// buildIfExprClone copies an if expression.
func buildIfExprClone(
	expr *ast.IfExpr,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	clone.Condition = buildExprClone(expr.Condition, prefix, imports, functions, types)
	clone.Consequence = buildBlockClone(expr.Consequence, prefix, imports, functions, types)
	clone.Alternative = buildBlockClone(expr.Alternative, prefix, imports, functions, types)
	return &clone
}

// buildCallClone copies one call and qualifies package function callees.
func buildCallClone(
	expr *ast.CallExpr,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	if ident, ok := expr.Callee.(*ast.IdentExpr); ok && functions[ident.Name] {
		clone.Callee = &ast.IdentExpr{Name: prefix + "." + ident.Name}
	} else if field, ok := expr.Callee.(*ast.FieldExpr); ok && field.Namespace {
		if name, ok := buildNamespaceName(field, prefix, imports, types); ok {
			clone.Callee = &ast.IdentExpr{Name: name}
		} else {
			clone.Callee = buildExprClone(expr.Callee, prefix, imports, functions, types)
		}
	} else {
		clone.Callee = buildExprClone(expr.Callee, prefix, imports, functions, types)
	}
	clone.Args = buildExprsClone(expr.Args, prefix, imports, functions, types)
	return &clone
}

// buildFieldClone copies field and namespace expressions.
func buildFieldClone(
	expr *ast.FieldExpr,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	if expr.Namespace {
		if name, ok := buildNamespaceName(expr, prefix, imports, types); ok {
			return &ast.IdentExpr{Name: name}
		}
	}
	clone := *expr
	clone.Receiver = buildExprClone(expr.Receiver, prefix, imports, functions, types)
	return &clone
}

// buildStructLiteralClone copies one struct literal with flattened type names.
func buildStructLiteralClone(
	expr *ast.StructLiteralExpr,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) ast.Expression {
	clone := *expr
	clone.TypeName = buildTypeName(expr.TypeName, prefix, imports, types)
	clone.Fields = make([]ast.FieldValue, 0, len(expr.Fields))
	for _, field := range expr.Fields {
		field.Value = buildExprClone(field.Value, prefix, imports, functions, types)
		clone.Fields = append(clone.Fields, field)
	}
	return &clone
}

// buildExprsClone copies an expression list for package build.
func buildExprsClone(
	exprs []ast.Expression,
	prefix string,
	imports map[string]string,
	functions map[string]bool,
	types map[string]bool,
) []ast.Expression {
	clone := make([]ast.Expression, 0, len(exprs))
	for _, expr := range exprs {
		clone = append(clone, buildExprClone(expr, prefix, imports, functions, types))
	}
	return clone
}

// buildNamespaceName maps a namespace expression to its flattened name.
func buildNamespaceName(
	expr *ast.FieldExpr,
	prefix string,
	imports map[string]string,
	types map[string]bool,
) (string, bool) {
	parts, ok := namespaceParts(expr)
	if !ok || len(parts) == 0 {
		return "", false
	}
	if mapped, ok := imports[parts[0]]; ok {
		parts[0] = mapped
		return strings.Join(parts, "."), true
	}
	if types[parts[0]] {
		parts[0] = prefix + "." + parts[0]
		return strings.Join(parts, "."), true
	}
	if parts[0] == "std" {
		return strings.Join(parts, "."), true
	}
	return "", false
}

// namespaceParts returns every segment in a namespace expression.
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

// buildTypeName rewrites package-local and imported type names for IR lowering.
func buildTypeName(
	name string,
	prefix string,
	imports map[string]string,
	types map[string]bool,
) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "!") || strings.HasPrefix(name, "?") {
		return name[:1] + buildTypeName(name[1:], prefix, imports, types)
	}
	for alias, mapped := range imports {
		name = strings.ReplaceAll(name, alias+"::", mapped+".")
	}
	if types[name] {
		return prefix + "." + name
	}
	return name
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
