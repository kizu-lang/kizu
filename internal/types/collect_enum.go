package types

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/ast"
)

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

// collectErrorSet registers and validates an error set declaration. A
// combined set (`error C = A or B;`) is registered here and resolved after
// every declaration is collected, so the sets it names may be declared later
// in the file.
func (c *Checker) collectErrorSet(decl *ast.ErrorSetDecl) error {
	if c.hasDuplicateTypeName(decl.Name) {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	if len(decl.Combines) > 0 {
		c.errorSets[decl.Name] = &errorSetType{
			name:     decl.Name,
			members:  map[string]bool{},
			public:   decl.Public,
			combines: decl.Combines,
		}
		return nil
	}
	set := &errorSetType{name: decl.Name, members: map[string]bool{}, public: decl.Public}
	set.values = map[string]bool{}
	set.byName = map[string][]string{}
	for _, member := range decl.Members {
		if set.members[member] {
			return errorf("type error: duplicate error `%s::%s`", decl.Name, member)
		}
		set.members[member] = true
		key := errorValueKey(decl.Name, member)
		set.values[key] = true
		set.valueOrder = append(set.valueOrder, key)
		set.byName[member] = []string{decl.Name}
	}
	set.tagged = &enumType{name: set.name, tags: set.members, public: set.public}
	c.errorSets[decl.Name] = set
	return nil
}

// resolveErrorSetCompositions fills in the member values of every combined
// error set once all declarations are collected.
func (c *Checker) resolveErrorSetCompositions() error {
	names := make([]string, 0, len(c.errorSets))
	for name, set := range c.errorSets {
		if set.byName == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := c.resolveErrorSetComposition(name, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

// resolveErrorSetComposition resolves one combined set. The union of unions is
// a union: a set reached through two of its parts contributes each value once,
// and a combined part contributes its already-resolved values.
func (c *Checker) resolveErrorSetComposition(name string, opening map[string]bool) error {
	set := c.errorSets[name]
	if set.byName != nil {
		return nil
	}
	if opening[name] {
		return errorf("type error: error set `%s` is combined from itself", name)
	}
	opening[name] = true
	values := map[string]bool{}
	var order []string
	for _, ref := range set.combines {
		origin := c.errorSets[ref]
		if origin == nil {
			return errorf(
				"type error: `%s` combines `%s`, which is not a declared error set",
				name, ref)
		}
		if err := c.resolveErrorSetComposition(ref, opening); err != nil {
			return err
		}
		for _, key := range origin.valueOrder {
			if values[key] {
				continue
			}
			values[key] = true
			order = append(order, key)
		}
	}
	byName := map[string][]string{}
	for _, key := range order {
		origin, bare := splitErrorValueKey(key)
		byName[bare] = append(byName[bare], origin)
	}
	set.values, set.valueOrder, set.byName = values, order, byName
	delete(opening, name)
	return nil
}
