package project

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/ast"
)

// ImportedPublicTypeNames returns imported public type names visible in module.
func ImportedPublicTypeNames(pkg *Package, module ParsedModule) []string {
	modules := parsedModulesByPath(pkg)
	names := []string{}
	for _, imported := range module.Imports {
		target, ok := modules[imported.Path]
		if !ok {
			continue
		}
		for _, decl := range target.Program.Decls {
			if name, ok := publicTypeDeclName(decl); ok {
				names = append(names, imported.Name+"::"+name)
			}
		}
	}
	sort.Strings(names)
	return names
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
