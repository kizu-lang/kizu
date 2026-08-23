package types

import "github.com/kizu-lang/kizu/internal/ast"

// collectEnum registers and validates a tag enum declaration.
func (c *Checker) collectEnum(decl *ast.EnumDecl) error {
	if c.hasDuplicateTypeName(decl.Name) {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	enum := &enumType{name: decl.Name, tags: map[string]bool{}, public: decl.Public}
	for _, tag := range decl.Tags {
		if enum.tags[tag] {
			return errorf("type error: duplicate enum tag `%s::%s`", decl.Name, tag)
		}
		enum.tags[tag] = true
		enum.order = append(enum.order, tag)
	}
	c.enums[decl.Name] = enum
	return nil
}

// collectErrorSet registers and validates an error set declaration.
func (c *Checker) collectErrorSet(decl *ast.ErrorSetDecl) error {
	if c.hasDuplicateTypeName(decl.Name) {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	set := &errorSetType{name: decl.Name, members: map[string]bool{}, public: decl.Public}
	for _, member := range decl.Members {
		if set.members[member] {
			return errorf("type error: duplicate error `%s::%s`", decl.Name, member)
		}
		set.members[member] = true
	}
	set.tagged = &enumType{name: set.name, tags: set.members, public: set.public}
	c.errorSets[decl.Name] = set
	return nil
}
