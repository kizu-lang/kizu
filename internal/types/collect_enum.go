package types

import "github.com/kizu-lang/kizu/internal/ast"

// enumCollectionIssueKind identifies one enum-shaped declaration conflict.
type enumCollectionIssueKind uint8

const (
	enumCollectionDuplicateType enumCollectionIssueKind = iota
	enumCollectionDuplicateTag
	enumCollectionDuplicateError
)

// enumCollectionIssue carries source names back to the Checker diagnostic
// boundary without constructing text inside metadata storage.
type enumCollectionIssue struct {
	kind   enumCollectionIssueKind
	owner  string
	member string
}

// collectEnum registers and validates a tag enum declaration.
func (c *Checker) collectEnum(decl *ast.EnumDecl) error {
	if issue, found := c.collectEnumDecision(decl); found {
		return enumCollectionDiagnostic(issue)
	}
	return nil
}

// collectEnumDecision retains an enum or reports its first copy-only conflict.
func (c *Checker) collectEnumDecision(decl *ast.EnumDecl) (enumCollectionIssue, bool) {
	if c.hasDuplicateTypeName(decl.Name) {
		return enumCollectionIssue{kind: enumCollectionDuplicateType, owner: decl.Name}, true
	}
	enum := &enumType{name: decl.Name, tags: map[string]bool{}, public: decl.Public}
	for _, tag := range decl.Tags {
		if enum.tags[tag] {
			return enumCollectionIssue{
				kind: enumCollectionDuplicateTag, owner: decl.Name, member: tag,
			}, true
		}
		enum.tags[tag] = true
		enum.order = append(enum.order, tag)
	}
	c.enums[decl.Name] = enum
	return enumCollectionIssue{}, false
}

// collectErrorSet registers and validates an error set declaration.
func (c *Checker) collectErrorSet(decl *ast.ErrorSetDecl) error {
	if issue, found := c.collectErrorSetDecision(decl); found {
		return enumCollectionDiagnostic(issue)
	}
	return nil
}

// collectErrorSetDecision retains an error set or reports its first conflict.
func (c *Checker) collectErrorSetDecision(decl *ast.ErrorSetDecl) (enumCollectionIssue, bool) {
	if c.hasDuplicateTypeName(decl.Name) {
		return enumCollectionIssue{kind: enumCollectionDuplicateType, owner: decl.Name}, true
	}
	set := &errorSetType{name: decl.Name, members: map[string]bool{}, public: decl.Public}
	for _, member := range decl.Members {
		if set.members[member] {
			return enumCollectionIssue{
				kind: enumCollectionDuplicateError, owner: decl.Name, member: member,
			}, true
		}
		set.members[member] = true
	}
	set.tagged = &enumType{name: set.name, tags: set.members, public: set.public}
	c.errorSets[decl.Name] = set
	return enumCollectionIssue{}, false
}

// enumCollectionDiagnostic constructs the exact source-facing conflict text.
func enumCollectionDiagnostic(issue enumCollectionIssue) error {
	switch issue.kind {
	case enumCollectionDuplicateTag:
		return errorf("type error: duplicate enum tag `%s::%s`", issue.owner, issue.member)
	case enumCollectionDuplicateError:
		return errorf("type error: duplicate error `%s::%s`", issue.owner, issue.member)
	default:
		return errorf("type error: duplicate type `%s`", issue.owner)
	}
}
