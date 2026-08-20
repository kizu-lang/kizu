package types

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// metaField is one field a `comptime for` capture is bound to for the length
// of one expansion. It carries what every form written against the capture
// needs: the struct it came from, the source name, and the field's type.
type metaField struct {
	owner Type
	name  string
	typ   Type
}

// checkComptimeForStmt expands a compile-time loop and checks each expansion.
// The body is ordinary Kizu code checked once per field, not a token stream
// rewritten before checking (SPEC §13.1).
func (c *Checker) checkComptimeForStmt(
	stmt *ast.ComptimeForStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	fields, err := c.comptimeForFields(stmt.List)
	if err != nil {
		return false, err
	}
	previous, had := c.metaFields[stmt.Name]
	defer func() {
		if had {
			c.metaFields[stmt.Name] = previous
			return
		}
		delete(c.metaFields, stmt.Name)
	}()
	returns := true
	for _, field := range fields {
		c.metaFields[stmt.Name] = field
		expansionReturns, err := c.checkBlock(stmt.Body, env.child(), wantReturn, unsafe)
		if err != nil {
			return false, err
		}
		returns = returns && expansionReturns
	}
	// An empty struct expands to nothing, and nothing cannot return.
	return returns && len(fields) > 0, nil
}

// comptimeForFields reads the list a `comptime for` walks. Only the field list
// is iterable: an integer range is what the runtime `for` is for.
func (c *Checker) comptimeForFields(list ast.Expression) ([]metaField, error) {
	call, ok := list.(*ast.CallExpr)
	if !ok {
		return nil, errorf("comptime error: comptime for expects `std::meta::public_fields<T>()`")
	}
	apply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return nil, errorf("comptime error: comptime for expects `std::meta::public_fields<T>()`")
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok || name != string(stdmeta.PublicFields) {
		return nil, errorf("comptime error: comptime for expects `std::meta::public_fields<T>()`")
	}
	if len(call.Args) != 0 {
		return nil, errorf("comptime error: `%s` takes no arguments", name)
	}
	return c.publicFields(c.instantiateTypeArgText(apply.TypeArg))
}

// publicFields lists a struct's public fields in declaration order. The order
// is the source order so that what a program emits does not depend on how the
// checker happens to store fields.
func (c *Checker) publicFields(typeArg string) ([]metaField, error) {
	args, err := typ.SplitArgs(typeArg)
	if err != nil || len(args) != 1 {
		return nil, errorf("comptime error: `%s` expects 1 static argument",
			stdmeta.PublicFields)
	}
	owner, err := c.parseType(args[0])
	if err != nil {
		return nil, err
	}
	decl, ok := c.structs[string(owner)]
	if !ok {
		return nil, errorf("comptime error: `%s` expects a struct, got %s",
			stdmeta.PublicFields, owner)
	}
	fields := make([]metaField, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		if !field.Public {
			continue
		}
		fields = append(fields, metaField{
			owner: owner,
			name:  field.Name,
			typ:   Type(typ.Text(field.TypeName)),
		})
	}
	return fields, nil
}

// metaCapture resolves the capture named in a form's static arguments.
func (c *Checker) metaCapture(form stdmeta.Form, args []string) (metaField, error) {
	if len(args) != 2 {
		return metaField{}, errorf("comptime error: `%s` expects 2 static arguments", form)
	}
	field, ok := c.metaFields[args[1]]
	if !ok {
		return metaField{}, errorf(
			"comptime error: `%s` is not a comptime for capture", args[1])
	}
	owner, err := c.parseType(args[0])
	if err != nil {
		return metaField{}, err
	}
	if owner != field.owner {
		return metaField{}, errorf("comptime error: capture `%s` belongs to %s, not %s",
			args[1], field.owner, owner)
	}
	return field, nil
}

// resolveMetaTypeText rewrites the forms written where a type goes into the
// type they name. Text with no form in it comes back unchanged, so every
// caller can run it over any spelling.
func (c *Checker) resolveMetaTypeText(text string) (string, error) {
	form, args, ok := stdmeta.SplitApply(text)
	if !ok {
		return text, nil
	}
	for idx, arg := range args {
		// A form's own arguments carry the type parameters of the body being
		// instantiated, so they are bound before the form is resolved.
		resolved, err := c.resolveMetaTypeText(string(substituteTypeParams(Type(arg), c.typeArgValues)))
		if err != nil {
			return "", err
		}
		args[idx] = resolved
	}
	switch form {
	case stdmeta.FieldType:
		field, err := c.metaCapture(form, args)
		if err != nil {
			return "", err
		}
		return string(field.typ), nil
	case stdmeta.Element:
		return c.metaElement(args)
	default:
		return "", errorf("comptime error: `%s` does not name a type", form)
	}
}

// metaElement names what a container holds.
func (c *Checker) metaElement(args []string) (string, error) {
	if len(args) != 1 {
		return "", errorf("comptime error: `%s` expects 1 static argument", stdmeta.Element)
	}
	container, err := c.parseType(args[0])
	if err != nil {
		return "", err
	}
	if elem, ok := optionalElem(container); ok {
		return string(elem), nil
	}
	base, elem, ok := splitGenericType(string(container))
	if ok && (base == "std::array::Array" || base == "std::mem::Box") {
		return elem, nil
	}
	// A map holds two types. What it *contains* is the value: the key is how
	// an entry is found, and every map key is `[]u8` today.
	if ok && base == "std::map::Map" {
		args, err := typ.SplitArgs(elem)
		if err != nil || len(args) != 2 {
			return "", errorf("comptime error: `%s` cannot read %s", stdmeta.Element, container)
		}
		return args[1], nil
	}
	return "", errorf(
		"comptime error: `%s` expects ?T, Array<T>, Box<T>, or Map<K, V>, got %s",
		stdmeta.Element, container)
}

// checkMetaApply types the forms written where an expression goes. ok reports
// whether name was a form at all, so the ordinary call paths stay untouched.
func (c *Checker) checkMetaApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	shape, ok := stdmeta.Lookup(name)
	if !ok {
		return "", false, nil
	}
	if shape.Type {
		return "", true, errorf("comptime error: `%s` names a type, not a value", name)
	}
	staticArgs, err := typ.SplitArgs(typeArg)
	if err != nil {
		return "", true, errorf("comptime error: `%s` has an unreadable static argument list", name)
	}
	if len(staticArgs) != shape.StaticArgs {
		return "", true, errorf("comptime error: `%s` expects %d static arguments, got %d",
			name, shape.StaticArgs, len(staticArgs))
	}
	if !shape.Variadic && len(args) != shape.Args {
		return "", true, errorf("comptime error: `%s` expects %d arguments, got %d",
			name, shape.Args, len(args))
	}
	typ, err := c.checkMetaForm(stdmeta.Form(name), staticArgs, args, env, unsafe)
	return typ, true, err
}

// checkMetaForm types one recognized form after its shape has been checked.
func (c *Checker) checkMetaForm(
	form stdmeta.Form,
	staticArgs []string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	switch form {
	case stdmeta.FieldName:
		if _, err := c.metaCapture(form, staticArgs); err != nil {
			return "", err
		}
		return typeByteString, nil
	case stdmeta.Field:
		return c.checkMetaFieldBorrow(form, staticArgs, args, env, unsafe)
	case stdmeta.Construct:
		return c.checkMetaConstruct(staticArgs, args, env, unsafe)
	case stdmeta.IsStruct, stdmeta.IsOptional, stdmeta.IsOwner:
		if _, err := c.metaPredicate(form, staticArgs); err != nil {
			return "", err
		}
		return typeBool, nil
	case stdmeta.Unsupported:
		return "", c.metaUnsupported(staticArgs)
	case stdmeta.PublicFields:
		return "", errorf("comptime error: `%s` is only written as a comptime for list", form)
	default:
		return "", errorf("comptime error: unsupported form `%s`", form)
	}
}

// checkMetaConstruct types `std::meta::construct<T, worker>(args...)` by
// checking the code it stands for. The expansion is built in one place
// (ast.ConstructExpansion) so this and the ownership checker and lowering all
// read the same statements.
func (c *Checker) checkMetaConstruct(
	staticArgs []string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	owner, fields, err := c.constructFields(staticArgs[0])
	if err != nil {
		return "", err
	}
	statements, literal := ast.ConstructExpansion(
		string(owner), staticArgs[1], fields, args, c.deinitOwners)
	scope := env.child()
	for _, stmt := range statements {
		if _, err := c.checkStmt(stmt, scope, c.currentReturn, unsafe); err != nil {
			return "", err
		}
	}
	built, err := c.checkExpr(literal, scope, unsafe)
	if err != nil {
		return "", err
	}
	return Type("!" + string(built)), nil
}

// constructFields reads the public fields `construct` fills, and refuses a type
// that has none: it would be built from nothing, and the values the caller
// meant to put in it would go nowhere.
func (c *Checker) constructFields(typeArg string) (Type, []ast.ConstructField, error) {
	owner, err := c.parseType(typeArg)
	if err != nil {
		return "", nil, err
	}
	fields, err := c.publicFields(string(owner))
	if err != nil {
		return "", nil, err
	}
	if len(fields) == 0 {
		return "", nil, errorf("comptime error: `%s` has no public field to construct `%s` from",
			stdmeta.Construct, owner)
	}
	out := make([]ast.ConstructField, 0, len(fields))
	for _, field := range fields {
		out = append(out, ast.ConstructField{Name: field.name, Type: string(field.typ)})
	}
	return owner, out, nil
}

// resolveMetaTypeDeep resolves the forms inside a wrapped spelling. A worker
// returns `!std::meta::field_type<T, f>`, so the form sits under the error
// union rather than at the top, and the wrapper has to be rebuilt around what
// the form resolved to.
func (c *Checker) resolveMetaTypeDeep(text string) (string, error) {
	if resolved, err := c.resolveMetaTypeText(text); err == nil && resolved != text {
		return resolved, nil
	}
	parsed, err := typ.Parse(text)
	if err != nil {
		return text, nil
	}
	switch node := parsed.(type) {
	case *typ.ErrorUnion:
		inner, err := c.resolveMetaTypeDeep(node.Ok.String())
		if err != nil {
			return "", err
		}
		return (&typ.ErrorUnion{Err: node.Err, Ok: &typ.Name{Path: []string{inner}}}).String(), nil
	case *typ.Optional:
		inner, err := c.resolveMetaTypeDeep(node.Elem.String())
		if err != nil {
			return "", err
		}
		return "?" + inner, nil
	case *typ.Slice:
		inner, err := c.resolveMetaTypeDeep(node.Elem.String())
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	case *typ.Name:
		if len(node.Args) == 0 {
			return text, nil
		}
		args := make([]typ.Type, 0, len(node.Args))
		for _, arg := range node.Args {
			inner, err := c.resolveMetaTypeDeep(arg.String())
			if err != nil {
				return "", err
			}
			args = append(args, &typ.Name{Path: []string{inner}})
		}
		return (&typ.Name{Path: node.Path, Args: args}).String(), nil
	default:
		return text, nil
	}
}

// checkMetaFieldBorrow types `std::meta::field<T, f>(value)`, which borrows one
// field out of a borrowed struct exactly as `&value.<name>` does (ADR-0113).
func (c *Checker) checkMetaFieldBorrow(
	form stdmeta.Form,
	staticArgs []string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	field, err := c.metaCapture(form, staticArgs)
	if err != nil {
		return "", err
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if borrowElem(got) != field.owner && got != field.owner {
		return "", errorf("comptime error: `%s` expects &%s, got %s", form, field.owner, got)
	}
	return field.typ, nil
}

// metaUnsupported fails compilation for the type that reached it, naming the
// function that refused it so the message says who has no case for it.
func (c *Checker) metaUnsupported(staticArgs []string) error {
	if len(staticArgs) != 1 {
		return errorf("comptime error: `%s` expects 1 static argument", stdmeta.Unsupported)
	}
	subject, err := c.parseType(staticArgs[0])
	if err != nil {
		return err
	}
	if c.currentFunction == nil {
		return errorf("comptime error: type `%s` is not supported here", subject)
	}
	return errorf("comptime error: `%s` does not support type `%s`",
		c.currentFunction.name, subject)
}

// metaPredicate answers a compile-time predicate about a type.
func (c *Checker) metaPredicate(form stdmeta.Form, staticArgs []string) (bool, error) {
	if len(staticArgs) != 1 {
		return false, errorf("comptime error: `%s` expects 1 static argument", form)
	}
	subject, err := c.parseType(staticArgs[0])
	if err != nil {
		return false, err
	}
	switch form {
	case stdmeta.IsStruct:
		_, ok := c.structs[string(subject)]
		return ok, nil
	case stdmeta.IsOptional:
		_, ok := optionalElem(subject)
		return ok, nil
	case stdmeta.IsArray:
		return metaGenericBase(string(subject)) == "std::array::Array", nil
	case stdmeta.IsBox:
		return metaGenericBase(string(subject)) == "std::mem::Box", nil
	case stdmeta.IsMap:
		return metaGenericBase(string(subject)) == "std::map::Map", nil
	case stdmeta.IsOwner:
		return c.ownerType(subject), nil
	case stdmeta.HasPublicFields:
		fields, err := c.publicFields(string(subject))
		return err == nil && len(fields) > 0, nil
	default:
		return false, errorf("comptime error: `%s` is not a predicate", form)
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

// borrowElem reads the type behind a borrow spelling.
func borrowElem(typeName Type) Type {
	text := string(typeName)
	text = strings.TrimPrefix(text, "&var ")
	text = strings.TrimPrefix(text, "&")
	return Type(text)
}
