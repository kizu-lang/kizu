package types

import "github.com/kizu-lang/kizu/internal/ast"

// checkerMetadata owns the declaration registries shared by collection and
// body checking. Keeping them together makes their one-check lifetime explicit.
type checkerMetadata struct {
	functions     map[string]*functionType
	structs       map[string]*ast.StructDecl
	enums         map[string]*enumType
	errorSets     map[string]*errorSetType
	unions        map[string]*unionType
	contracts     map[string]*contractType
	impls         map[string]map[string]*functionType
	declaredTypes map[string]bool
}

// newCheckerMetadata creates empty registries for one check.
func newCheckerMetadata() checkerMetadata {
	return checkerMetadata{
		functions:     map[string]*functionType{},
		structs:       map[string]*ast.StructDecl{},
		enums:         map[string]*enumType{},
		errorSets:     map[string]*errorSetType{},
		unions:        map[string]*unionType{},
		contracts:     map[string]*contractType{},
		impls:         map[string]map[string]*functionType{},
		declaredTypes: map[string]bool{},
	}
}

// predeclareTypeNames lets recursive fields refer to later declarations through Box.
func (c *Checker) predeclareTypeNames(program *ast.Program) error {
	for _, decl := range program.Decls {
		name, ok := declaredTypeName(decl)
		if !ok {
			continue
		}
		c.declaredTypes[name] = true
	}
	return nil
}

// declaredTypeName returns the user type introduced by a declaration.
func declaredTypeName(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
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

// isTypeName reports whether name is a compiler or program type.
func (m *checkerMetadata) isTypeName(name string) bool {
	if knownTypes[Type(name)] || m.declaredTypes[name] || isKnownGenericBase(name) {
		return true
	}
	return m.structs[name] != nil || m.enums[name] != nil ||
		m.unions[name] != nil || m.errorSets[name] != nil
}

// isUserDeclaredType reports whether name has retained declaration metadata.
func (m *checkerMetadata) isUserDeclaredType(name string) bool {
	return m.structs[name] != nil || m.enums[name] != nil ||
		m.errorSets[name] != nil || m.unions[name] != nil ||
		m.contracts[name] != nil
}

// isPublicType reports whether a retained declaration is externally visible.
func (m *checkerMetadata) isPublicType(name string) bool {
	if decl := m.structs[name]; decl != nil {
		return decl.Public
	}
	if enum := m.enums[name]; enum != nil {
		return enum.public
	}
	if set := m.errorSets[name]; set != nil {
		return set.public
	}
	if union := m.unions[name]; union != nil {
		return union.public
	}
	if contract := m.contracts[name]; contract != nil {
		return contract.public
	}
	return false
}
