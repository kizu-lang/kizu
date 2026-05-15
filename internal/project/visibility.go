package project

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// CheckVisibility enforces public boundaries across resolved package modules.
func CheckVisibility(pkg *Package) error {
	index := newVisibilityIndex(pkg)
	diagnostics := []Diagnostic{}
	for _, module := range pkg.Modules {
		checkModuleVisibility(module, index, &diagnostics)
	}
	if len(diagnostics) > 0 {
		return DiagnosticError{Diagnostics: diagnostics}
	}
	return nil
}

type visibilityIndex struct {
	modules map[string]ParsedModule
	decls   map[string]map[string]declVisibility
	imports map[string]map[string]string
}

type declVisibility struct {
	public bool
	span   ast.Span
	file   string
	fields map[string]fieldVisibility
}

type fieldVisibility struct {
	public bool
	span   ast.Span
	file   string
}

// newVisibilityIndex records declarations and import aliases for package checks.
func newVisibilityIndex(pkg *Package) visibilityIndex {
	index := visibilityIndex{
		modules: map[string]ParsedModule{},
		decls:   map[string]map[string]declVisibility{},
		imports: map[string]map[string]string{},
	}
	for _, module := range pkg.Modules {
		index.modules[module.Module.Path] = module
		index.decls[module.Module.Path] = declsForModule(module)
		index.imports[module.Module.Path] = importsForVisibility(module)
	}
	return index
}

// declsForModule returns top-level declarations visible to module checks.
func declsForModule(module ParsedModule) map[string]declVisibility {
	decls := map[string]declVisibility{}
	for _, decl := range module.Program.Decls {
		name, item, ok := declVisibilityFor(module.Module.File, decl)
		if ok {
			decls[name] = item
		}
	}
	return decls
}

// declVisibilityFor returns visibility metadata for one declaration.
func declVisibilityFor(file string, decl ast.Decl) (string, declVisibility, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return d.Name, declVisibility{public: d.Public, span: d.Span, file: file}, true
	case *ast.StructDecl:
		return d.Name, structVisibility(file, d), true
	case *ast.EnumDecl:
		return d.Name, declVisibility{public: d.Public, span: d.Span, file: file}, true
	case *ast.UnionDecl:
		return d.Name, declVisibility{public: d.Public, span: d.Span, file: file}, true
	case *ast.ContractDecl:
		return d.Name, declVisibility{public: d.Public, span: d.Span, file: file}, true
	default:
		return "", declVisibility{}, false
	}
}

// structVisibility returns declaration and field visibility for a struct.
func structVisibility(file string, decl *ast.StructDecl) declVisibility {
	fields := map[string]fieldVisibility{}
	for _, field := range decl.Fields {
		fields[field.Name] = fieldVisibility{public: field.Public, span: field.Span, file: file}
	}
	return declVisibility{public: decl.Public, span: decl.Span, file: file, fields: fields}
}

// importsForVisibility returns import aliases keyed by local import name.
func importsForVisibility(module ParsedModule) map[string]string {
	imports := map[string]string{}
	for _, imported := range module.Imports {
		imports[imported.Name] = imported.Path
	}
	return imports
}

// checkModuleVisibility runs all cross-module visibility checks for one module.
func checkModuleVisibility(
	module ParsedModule,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	checkPublicSignatures(module, index, diagnostics)
	walkProgramExpressions(module, index, diagnostics)
}

// walkProgramExpressions checks namespace accesses and struct construction.
func walkProgramExpressions(
	module ParsedModule,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, decl := range module.Program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			walkBlock(module, d.Body, index, diagnostics)
		case *ast.ImplDecl:
			for _, method := range d.Methods {
				walkBlock(module, method.Body, index, diagnostics)
			}
		}
	}
}

// walkBlock checks expressions in a statement block.
func walkBlock(
	module ParsedModule,
	block *ast.BlockStmt,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		walkStatement(module, stmt, index, diagnostics)
	}
}

// walkStatement checks expressions contained by one statement.
func walkStatement(
	module ParsedModule,
	stmt ast.Statement,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		walkExpr(module, s.Value, index, diagnostics)
	case *ast.AssignStmt:
		walkExpr(module, s.Target, index, diagnostics)
		walkExpr(module, s.Value, index, diagnostics)
	case *ast.ReturnStmt:
		walkExpr(module, s.Value, index, diagnostics)
	case *ast.ExprStmt:
		walkExpr(module, s.Expr, index, diagnostics)
	case *ast.IfStmt:
		walkIfStmt(module, s, index, diagnostics)
	case *ast.WhileStmt:
		walkExpr(module, s.Condition, index, diagnostics)
		walkBlock(module, s.Body, index, diagnostics)
	case *ast.ForStmt:
		walkExpr(module, s.Start, index, diagnostics)
		walkExpr(module, s.End, index, diagnostics)
		walkBlock(module, s.Body, index, diagnostics)
	case *ast.UnsafeStmt:
		walkBlock(module, s.Body, index, diagnostics)
	case *ast.ComptimeIfStmt:
		walkComptimeIfStmt(module, s, index, diagnostics)
	case *ast.MatchStmt:
		walkMatchStmt(module, s, index, diagnostics)
	}
}

// walkIfStmt checks all expressions in an if statement.
func walkIfStmt(
	module ParsedModule,
	stmt *ast.IfStmt,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, stmt.Condition, index, diagnostics)
	walkBlock(module, stmt.Consequence, index, diagnostics)
	walkBlock(module, stmt.Alternative, index, diagnostics)
}

// walkComptimeIfStmt checks all expressions in a comptime if statement.
func walkComptimeIfStmt(
	module ParsedModule,
	stmt *ast.ComptimeIfStmt,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, stmt.Condition, index, diagnostics)
	walkBlock(module, stmt.Consequence, index, diagnostics)
	walkBlock(module, stmt.Alternative, index, diagnostics)
}

// walkMatchStmt checks all expressions in a match statement.
func walkMatchStmt(
	module ParsedModule,
	stmt *ast.MatchStmt,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, stmt.Value, index, diagnostics)
	for _, arm := range stmt.Arms {
		walkStatement(module, arm.Body, index, diagnostics)
	}
}

// walkExpr checks visibility-sensitive expression forms recursively.
func walkExpr(
	module ParsedModule,
	expr ast.Expression,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	switch e := expr.(type) {
	case *ast.PrefixExpr:
		walkExpr(module, e.Right, index, diagnostics)
	case *ast.BinaryExpr:
		walkExpr(module, e.Left, index, diagnostics)
		walkExpr(module, e.Right, index, diagnostics)
	case *ast.CallExpr:
		walkCallExpr(module, e, index, diagnostics)
	case *ast.TypeApplyExpr:
		walkExpr(module, e.Callee, index, diagnostics)
	case *ast.CastExpr:
		walkExpr(module, e.Value, index, diagnostics)
	case *ast.TryExpr:
		walkExpr(module, e.Value, index, diagnostics)
	case *ast.StructLiteralExpr:
		walkStructLiteral(module, e, index, diagnostics)
	case *ast.FieldExpr:
		checkFieldExpr(module, e, index, diagnostics)
	case *ast.DerefExpr:
		walkExpr(module, e.Receiver, index, diagnostics)
	case *ast.IfExpr:
		walkIfExpr(module, e, index, diagnostics)
	case *ast.ComptimeExpr:
		walkExpr(module, e.Expr, index, diagnostics)
	}
}

// walkCallExpr checks a call expression and its arguments.
func walkCallExpr(
	module ParsedModule,
	expr *ast.CallExpr,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, expr.Callee, index, diagnostics)
	for _, arg := range expr.Args {
		walkExpr(module, arg, index, diagnostics)
	}
}

// walkIfExpr checks a value-producing if expression.
func walkIfExpr(
	module ParsedModule,
	expr *ast.IfExpr,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, expr.Condition, index, diagnostics)
	walkBlock(module, expr.Consequence, index, diagnostics)
	walkBlock(module, expr.Alternative, index, diagnostics)
}

// walkStructLiteral checks field visibility for external struct construction.
func walkStructLiteral(
	module ParsedModule,
	expr *ast.StructLiteralExpr,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, field := range expr.Fields {
		walkExpr(module, field.Value, index, diagnostics)
	}
	target, name, ok := resolveTypeRef(module, expr.TypeName, index)
	if !ok {
		return
	}
	decl, ok := index.decls[target][name]
	if !ok {
		return
	}
	for _, field := range expr.Fields {
		item, exists := decl.fields[field.Name]
		if exists && !item.public {
			*diagnostics = append(*diagnostics,
				privateFieldDiagnostic(module, target, name, field, item))
		}
	}
}

// checkFieldExpr rejects private namespace declarations across modules.
func checkFieldExpr(
	module ParsedModule,
	expr *ast.FieldExpr,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	walkExpr(module, expr.Receiver, index, diagnostics)
	if !expr.Namespace {
		return
	}
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok {
		return
	}
	target, imported := index.imports[module.Module.Path][ident.Name]
	if !imported {
		return
	}
	decl, exists := index.decls[target][expr.Name]
	if exists && !decl.public {
		*diagnostics = append(*diagnostics, privateDeclDiagnostic(module, target, expr, decl))
	}
}

// checkPublicSignatures rejects public APIs that mention private imported types.
func checkPublicSignatures(module ParsedModule, index visibilityIndex, diagnostics *[]Diagnostic) {
	for _, decl := range module.Program.Decls {
		checkPublicDeclSignature(module, decl, index, diagnostics)
	}
}

// checkPublicDeclSignature validates one public declaration signature.
func checkPublicDeclSignature(
	module ParsedModule,
	decl ast.Decl,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		if d.Public {
			checkFunctionTypes(module, d, index, diagnostics)
		}
	case *ast.StructDecl:
		if d.Public {
			checkFieldTypes(module, d.Fields, index, diagnostics)
		}
	case *ast.UnionDecl:
		if d.Public {
			checkUnionTypes(module, d, index, diagnostics)
		}
	case *ast.ContractDecl:
		if d.Public {
			checkContractTypes(module, d, index, diagnostics)
		}
	}
}

// checkFunctionTypes checks imported type visibility in a function signature.
func checkFunctionTypes(
	module ParsedModule,
	fn *ast.FunctionDecl,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, param := range fn.Params {
		checkTypeVisibility(module, param.TypeName, fn.Span, index, diagnostics)
	}
	checkTypeVisibility(module, fn.ReturnType, fn.Span, index, diagnostics)
}

// checkFieldTypes checks imported type visibility in struct fields.
func checkFieldTypes(
	module ParsedModule,
	fields []ast.Field,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, field := range fields {
		checkTypeVisibility(module, field.TypeName, field.Span, index, diagnostics)
	}
}

// checkUnionTypes checks imported type visibility in union payloads.
func checkUnionTypes(
	module ParsedModule,
	decl *ast.UnionDecl,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, variant := range decl.Variants {
		checkTypeVisibility(module, variant.Payload, decl.Span, index, diagnostics)
	}
}

// checkContractTypes checks imported type visibility in contract methods.
func checkContractTypes(
	module ParsedModule,
	decl *ast.ContractDecl,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, method := range decl.Methods {
		checkFunctionTypes(module, method, index, diagnostics)
	}
}

// checkTypeVisibility rejects private imported declarations in type names.
func checkTypeVisibility(
	module ParsedModule,
	typeName string,
	span ast.Span,
	index visibilityIndex,
	diagnostics *[]Diagnostic,
) {
	for _, ref := range namespaceTypeRefs(typeName) {
		target, ok := index.imports[module.Module.Path][ref.alias]
		if !ok {
			continue
		}
		decl, ok := index.decls[target][ref.name]
		if ok && !decl.public {
			*diagnostics = append(*diagnostics, privateTypeDiagnostic(module, target, ref, span, decl))
		}
	}
}

// resolveTypeRef maps an imported Alias::Type name to a target module.
func resolveTypeRef(
	module ParsedModule,
	typeName string,
	index visibilityIndex,
) (string, string, bool) {
	refs := namespaceTypeRefs(typeName)
	if len(refs) != 1 {
		return "", "", false
	}
	target, ok := index.imports[module.Module.Path][refs[0].alias]
	if !ok {
		return "", "", false
	}
	return target, refs[0].name, true
}

type typeRef struct {
	alias string
	name  string
}

// namespaceTypeRefs extracts imported Alias::Type references from a type string.
func namespaceTypeRefs(typeName string) []typeRef {
	refs := []typeRef{}
	for _, token := range strings.FieldsFunc(typeName, typeSeparator) {
		parts := strings.Split(token, "::")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			refs = append(refs, typeRef{alias: parts[0], name: parts[1]})
		}
	}
	return refs
}

// typeSeparator reports separators outside namespace components.
func typeSeparator(r rune) bool {
	return strings.ContainsRune(" \t\n\r,<>[]?!&*()", r)
}

// privateTypeDiagnostic reports a private imported type in a public signature.
func privateTypeDiagnostic(
	module ParsedModule,
	target string,
	ref typeRef,
	span ast.Span,
	decl declVisibility,
) Diagnostic {
	return Diagnostic{
		Message: fmt.Sprintf("public signature exposes private type `%s::%s`", ref.alias, ref.name),
		Primary: Location{Module: module.Module.Path, File: module.Module.File, Span: span},
		Related: []Related{{
			Message:  "private type declared here",
			Location: Location{Module: target, File: decl.file, Span: decl.span},
		}},
	}
}

// privateDeclDiagnostic reports private top-level namespace access.
func privateDeclDiagnostic(
	module ParsedModule,
	target string,
	expr *ast.FieldExpr,
	decl declVisibility,
) Diagnostic {
	return Diagnostic{
		Message: fmt.Sprintf("private declaration `%s` is not visible", expr.String()),
		Primary: Location{Module: module.Module.Path, File: module.Module.File, Span: expr.Span},
		Related: []Related{{
			Message:  "private declaration is here",
			Location: Location{Module: target, File: decl.file, Span: decl.span},
		}},
	}
}

// privateFieldDiagnostic reports private field construction across modules.
func privateFieldDiagnostic(
	module ParsedModule,
	target string,
	typeName string,
	field ast.FieldValue,
	item fieldVisibility,
) Diagnostic {
	return Diagnostic{
		Message: fmt.Sprintf("private field `%s::%s.%s` is not visible",
			target, typeName, field.Name),
		Primary: Location{Module: module.Module.Path, File: module.Module.File, Span: field.Span},
		Related: []Related{{
			Message:  "private field is here",
			Location: Location{Module: target, File: item.file, Span: item.span},
		}},
	}
}
