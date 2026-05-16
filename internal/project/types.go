package project

import (
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// ImportedPublicTypeNames returns imported public type names visible in module.
func ImportedPublicTypeNames(pkg *Package, module ParsedModule) []string {
	decls := ImportedPublicDecls(pkg, module)
	names := make([]string, 0, len(decls))
	for name, decl := range decls {
		if _, ok := publicTypeDeclName(decl); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ImportedPublicDecls returns imported public declarations visible in module.
func ImportedPublicDecls(pkg *Package, module ParsedModule) map[string]ast.Decl {
	modules := parsedModulesByPath(pkg)
	decls := map[string]ast.Decl{}
	for _, imported := range module.Imports {
		target, ok := modules[imported.Path]
		if !ok {
			continue
		}
		publicTypes := publicTypeNames(target.Program)
		for _, decl := range target.Program.Decls {
			name, cloned, ok := importedPublicDecl(imported.Name, decl, publicTypes)
			if ok {
				decls[name] = cloned
			}
		}
	}
	return decls
}

// importedPublicDecl returns a public declaration qualified through import alias.
func importedPublicDecl(
	alias string,
	decl ast.Decl,
	publicTypes map[string]bool,
) (string, ast.Decl, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		if !d.Public {
			return "", nil, false
		}
		return alias + "::" + d.Name, cloneImportedFunction(alias, d, publicTypes), true
	case *ast.StructDecl:
		if !d.Public {
			return "", nil, false
		}
		return alias + "::" + d.Name, cloneImportedStruct(alias, d, publicTypes), true
	case *ast.EnumDecl:
		if !d.Public {
			return "", nil, false
		}
		return alias + "::" + d.Name, cloneImportedEnum(alias, d), true
	default:
		name, ok := publicTypeDeclName(decl)
		if !ok {
			return "", nil, false
		}
		return alias + "::" + name, decl, true
	}
}

// publicTypeNames returns public type declarations by local name.
func publicTypeNames(program *ast.Program) map[string]bool {
	names := map[string]bool{}
	for _, decl := range program.Decls {
		name, ok := publicTypeDeclName(decl)
		if ok {
			names[name] = true
		}
	}
	return names
}

// cloneImportedFunction qualifies local public type references in one function.
func cloneImportedFunction(
	alias string,
	decl *ast.FunctionDecl,
	publicTypes map[string]bool,
) *ast.FunctionDecl {
	params := make([]ast.Param, 0, len(decl.Params))
	for _, param := range decl.Params {
		param.TypeName = qualifyImportedType(alias, param.TypeName, publicTypes)
		params = append(params, param)
	}
	cloned := *decl
	cloned.Name = alias + "::" + decl.Name
	cloned.Params = params
	cloned.ReturnType = qualifyImportedType(alias, decl.ReturnType, publicTypes)
	return &cloned
}

// cloneImportedStruct qualifies local public type references in one struct.
func cloneImportedStruct(
	alias string,
	decl *ast.StructDecl,
	publicTypes map[string]bool,
) *ast.StructDecl {
	fields := make([]ast.Field, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		field.TypeName = qualifyImportedType(alias, field.TypeName, publicTypes)
		fields = append(fields, field)
	}
	cloned := *decl
	cloned.Name = alias + "::" + decl.Name
	cloned.Fields = fields
	return &cloned
}

// cloneImportedEnum qualifies the imported enum declaration name.
func cloneImportedEnum(alias string, decl *ast.EnumDecl) *ast.EnumDecl {
	cloned := *decl
	cloned.Name = alias + "::" + decl.Name
	return &cloned
}

// qualifyImportedType qualifies direct references to public imported types.
func qualifyImportedType(alias string, typeName string, publicTypes map[string]bool) string {
	if strings.HasPrefix(typeName, "!") {
		return "!" + qualifyImportedType(alias, strings.TrimPrefix(typeName, "!"), publicTypes)
	}
	if idx := strings.Index(typeName, "!"); idx > 0 && idx < len(typeName)-1 {
		left := qualifyImportedType(alias, typeName[:idx], publicTypes)
		right := qualifyImportedType(alias, typeName[idx+1:], publicTypes)
		return left + "!" + right
	}
	if publicTypes[typeName] {
		return alias + "::" + typeName
	}
	return typeName
}

// parsedModulesByPath returns package modules keyed by module path.
func parsedModulesByPath(pkg *Package) map[string]ParsedModule {
	modules := map[string]ParsedModule{}
	for _, module := range pkg.Modules {
		modules[module.Module.Path] = module
	}
	return modules
}

// publicTypeDeclName returns the name of a public type declaration.
func publicTypeDeclName(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
	case *ast.StructDecl:
		return d.Name, d.Public
	case *ast.EnumDecl:
		return d.Name, d.Public
	case *ast.UnionDecl:
		return d.Name, d.Public
	case *ast.ContractDecl:
		return d.Name, d.Public
	default:
		return "", false
	}
}
