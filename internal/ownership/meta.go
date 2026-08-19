package ownership

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// metaField is the field a `comptime for` capture names for the length of one
// expansion.
type metaField struct {
	owner string
	name  string
	typ   string
}

// checkComptimeForStmt checks ownership effects once per expansion. Each
// expansion is the same body checked against a different field, so a move in
// the body is a move in every expansion -- which is what makes moving an owner
// inside a multi-field loop the error it should be.
func (c *Checker) checkComptimeForStmt(stmt *ast.ComptimeForStmt, env *scope) error {
	fields, err := c.comptimeForFields(stmt.List)
	if err != nil {
		return err
	}
	previous, had := c.metaFields[stmt.Name]
	defer func() {
		if had {
			c.metaFields[stmt.Name] = previous
			return
		}
		delete(c.metaFields, stmt.Name)
	}()
	for _, field := range fields {
		c.metaFields[stmt.Name] = field
		if err := c.checkBlock(stmt.Body, env.child()); err != nil {
			return err
		}
	}
	return nil
}

// comptimeForFields reads the field list a `comptime for` walks.
func (c *Checker) comptimeForFields(list ast.Expression) ([]metaField, error) {
	call, ok := list.(*ast.CallExpr)
	if !ok {
		return nil, errorf("move error: comptime for expects `std::meta::public_fields<T>()`")
	}
	apply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return nil, errorf("move error: comptime for expects `std::meta::public_fields<T>()`")
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok || name != string(stdmeta.PublicFields) {
		return nil, errorf("move error: comptime for expects `std::meta::public_fields<T>()`")
	}
	return c.publicFields(c.instantiateTypeArgText(apply.TypeArg))
}

// publicFields lists a struct's public fields in declaration order.
func (c *Checker) publicFields(typeArg string) ([]metaField, error) {
	args, err := typ.SplitArgs(typeArg)
	if err != nil || len(args) != 1 {
		return nil, errorf("move error: `%s` expects 1 static argument", stdmeta.PublicFields)
	}
	owner := args[0]
	order, ok := c.structPublicOrder[owner]
	if !ok {
		return nil, errorf("move error: `%s` expects a struct, got %s",
			stdmeta.PublicFields, owner)
	}
	fields := make([]metaField, 0, len(order))
	for _, name := range order {
		fields = append(fields, metaField{owner: owner, name: name, typ: c.structs[owner][name]})
	}
	return fields, nil
}

// resolveMetaTypeText rewrites the forms written where a type goes into the
// type they name. Text with no form in it comes back unchanged.
func (c *Checker) resolveMetaTypeText(text string) string {
	form, args, ok := stdmeta.SplitApply(text)
	if !ok {
		return text
	}
	for idx, arg := range args {
		args[idx] = c.resolveMetaTypeText(arg)
	}
	switch form {
	case stdmeta.FieldType:
		if len(args) != 2 {
			return text
		}
		field, ok := c.metaFields[args[1]]
		if !ok {
			return text
		}
		return field.typ
	case stdmeta.Element:
		if len(args) != 1 {
			return text
		}
		return metaElementType(args[0])
	default:
		return text
	}
}

// metaGenericBase names the container a spelling applies, or "" for a type
// that applies none.
func metaGenericBase(typeName string) string {
	open := strings.IndexByte(typeName, '<')
	if open < 0 || !strings.HasSuffix(typeName, ">") {
		return ""
	}
	return typeName[:open]
}

// metaElementType names what a container holds.
func metaElementType(container string) string {
	if elem, ok := typ.OptionalElem(container); ok {
		return elem
	}
	base, elem, ok := splitGenericType(container)
	if ok && (base == "std::array::Array" || base == "std::mem::Box") {
		return elem
	}
	if ok && base == "std::map::Map" {
		if args, err := typ.SplitArgs(elem); err == nil && len(args) == 2 {
			return args[1]
		}
	}
	return container
}

// metaCapture resolves the capture named in a form's static arguments.
func (c *Checker) metaCapture(form stdmeta.Form, args []string) (metaField, error) {
	if len(args) != 2 {
		return metaField{}, errorf("move error: `%s` expects 2 static arguments", form)
	}
	field, ok := c.metaFields[args[1]]
	if !ok {
		return metaField{}, errorf("move error: `%s` is not a comptime for capture", args[1])
	}
	return field, nil
}

// checkMetaApply applies the ownership effect of one `std::meta` form. ok
// reports whether name was a form at all.
func (c *Checker) checkMetaApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	form := stdmeta.Form(name)
	if _, ok := stdmeta.Lookup(name); !ok {
		return "", false, nil
	}
	staticArgs, err := typ.SplitArgs(typeArg)
	if err != nil {
		return "", true, errorf("move error: `%s` has an unreadable static argument list", name)
	}
	switch form {
	case stdmeta.FieldName:
		if _, err := c.metaCapture(form, staticArgs); err != nil {
			return "", true, err
		}
		return "[]u8", true, nil
	case stdmeta.IsStruct, stdmeta.IsOptional:
		return "bool", true, nil
	case stdmeta.Field:
		typeName, err := c.checkMetaFieldBorrow(form, staticArgs, args, env)
		return typeName, true, err
	case stdmeta.Construct:
		typeName, err := c.checkMetaConstruct(staticArgs, args, env)
		return typeName, true, err
	default:
		return "", true, errorf("move error: `%s` cannot be called", name)
	}
}

// checkMetaConstruct applies the ownership effect of
// `std::meta::construct<T, worker>(args...)`: the code it stands for. Each
// worker result is an owner until the struct literal takes it, and the
// `errdefer` the expansion carries is what releases the fields already built
// when a later worker fails. The expansion is built in one place
// (ast.ConstructExpansion), so this reads the same statements the type checker
// and lowering do.
func (c *Checker) checkMetaConstruct(
	staticArgs []string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(staticArgs) != 2 {
		return "", errorf("move error: `%s` expects 2 static arguments", stdmeta.Construct)
	}
	owner := c.resolveMetaTypeText(staticArgs[0])
	fields, err := c.publicFields(owner)
	if err != nil {
		return "", err
	}
	expansion := make([]ast.ConstructField, 0, len(fields))
	for _, field := range fields {
		expansion = append(expansion, ast.ConstructField{Name: field.name, Type: field.typ})
	}
	statements, literal := ast.ConstructExpansion(
		owner, staticArgs[1], expansion, args, c.deinitOwners)
	scope := env.child()
	mark := len(c.liveErrDefers)
	defer func() { c.liveErrDefers = c.liveErrDefers[:mark] }()
	for _, stmt := range statements {
		// The expansion registers cleanups the way a block does, which is the
		// one place `errdefer` is accepted; the statements are a block in
		// everything but the braces.
		if errDefer, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := c.checkErrDeferStmt(errDefer, scope); err != nil {
				return "", err
			}
			continue
		}
		if err := c.checkStmt(stmt, scope); err != nil {
			return "", err
		}
	}
	built, err := c.readExpr(literal, scope)
	if err != nil {
		return "", err
	}
	return "!" + built, nil
}

// checkMetaFieldBorrow borrows one field out of a borrowed struct. The borrow
// is the field path borrow of §9: the same target, the same tracking, the same
// conflicts (ADR-0113), so this synthesizes that path rather than inventing a
// second borrow rule.
func (c *Checker) checkMetaFieldBorrow(
	form stdmeta.Form,
	staticArgs []string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	field, err := c.metaCapture(form, staticArgs)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", errorf("move error: `%s` expects 1 argument, got %d", form, len(args))
	}
	path := &ast.FieldExpr{Receiver: args[0], Name: field.name}
	if _, err := c.readExpr(path, env); err != nil {
		return "", err
	}
	return field.typ, nil
}
