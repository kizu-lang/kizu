package types

import (
	"errors"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// metaField is one field or variant a comptime capture is bound to for the
// length of one expansion. It carries what every form written against the
// capture needs: the type it came from, the source name, and the type of the
// value it holds. A variant that carries no payload has none, so typ is empty
// for it -- which is the question `has_payload` answers.
type metaField struct {
	owner   Type
	name    string
	typ     Type
	variant bool
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

// comptimeForFields reads the list a `comptime for` walks. Only the two
// declared lists are iterable: an integer range is what the runtime `for` is
// for.
func (c *Checker) comptimeForFields(list ast.Expression) ([]metaField, error) {
	call, ok := list.(*ast.CallExpr)
	if !ok {
		return nil, errComptimeForList
	}
	apply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return nil, errComptimeForList
	}
	name, ok := qualifiedName(apply.Callee)
	if !ok {
		return nil, errComptimeForList
	}
	if len(call.Args) != 0 {
		return nil, errorf("comptime error: `%s` takes no arguments", name)
	}
	switch stdmeta.Form(name) {
	case stdmeta.PublicFields:
		return c.publicFields(c.instantiateTypeArgText(apply.TypeArg))
	case stdmeta.Variants:
		return c.variants(c.instantiateTypeArgText(apply.TypeArg))
	default:
		return nil, errComptimeForList
	}
}

// errComptimeForList names the lists a `comptime for` accepts.
var errComptimeForList = errorf(
	"comptime error: comptime for expects `std::meta::public_fields<T>()` or " +
		"`std::meta::variants<T>()`")

// variants lists an enum's tags or a union's variants in declaration order.
func (c *Checker) variants(typeArg string) ([]metaField, error) {
	args, err := typ.SplitArgs(typeArg)
	if err != nil || len(args) != 1 {
		return nil, errorf("comptime error: `%s` expects 1 static argument", stdmeta.Variants)
	}
	owner, err := c.parseType(args[0])
	if err != nil {
		return nil, err
	}
	if enum := c.enums[string(owner)]; enum != nil {
		out := make([]metaField, 0, len(enum.order))
		for _, tag := range enum.order {
			out = append(out, metaField{owner: owner, name: tag, variant: true})
		}
		return out, nil
	}
	union := c.unions[string(owner)]
	if union == nil {
		return nil, errorf("comptime error: `%s` expects an enum or union, got %s",
			stdmeta.Variants, owner)
	}
	out := make([]metaField, 0, len(union.order))
	for _, name := range union.order {
		out = append(out, metaField{
			owner:   owner,
			name:    name,
			typ:     Type(union.variants[name]),
			variant: true,
		})
	}
	return out, nil
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
			"comptime error: `%s` is not a comptime capture", args[1])
	}
	if field.variant != stdmeta.VariantForm(form) {
		return metaField{}, errorf("comptime error: `%s` expects a %s capture, got `%s`",
			form, metaCaptureKind(stdmeta.VariantForm(form)), args[1])
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

// metaFormError is the failure of a form that resolved its capture and still
// names no type. It is separated from every other failure because the two mean
// opposite things: a capture that is simply not bound here is what a
// declaration looks like before instantiation and keeps its spelling, while
// this is the program's mistake and has to be reported where it is written.
type metaFormError struct {
	err error
}

// Error returns the wrapped diagnostic.
func (e metaFormError) Error() string {
	return e.err.Error()
}

// isMetaFormError reports whether a failure is the program's mistake rather
// than a capture that is not bound yet.
func isMetaFormError(err error) bool {
	var formErr metaFormError
	return errors.As(err, &formErr)
}

// metaCaptureKind names a capture kind for a diagnostic.
func metaCaptureKind(variant bool) string {
	if variant {
		return "variant"
	}
	return "field"
}

// checkComptimeMatchStmt checks the match a `comptime match` stands for, with
// the capture bound to the arm's own variant while its body is checked. The
// dispatch itself is an ordinary match (ast.ComptimeMatchExpansion), so
// exhaustiveness and payload binding are checked by the rules match already
// has.
func (c *Checker) checkComptimeMatchStmt(
	stmt *ast.ComptimeMatchStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	valueType, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	owner := borrowElem(valueType)
	variants, err := c.variants(string(owner))
	if err != nil {
		return false, errorf("comptime error: comptime match expects an enum or union, got %s",
			valueType)
	}
	if stmt.Binding == "" && metaFieldsCarryPayload(variants) {
		return false, errorf(
			"comptime error: comptime match on `%s` needs a payload capture, as in "+
				"`comptime match value |variant, payload| { ... }`", owner)
	}
	return c.checkStmt(ast.ComptimeMatchExpansion(stmt, string(owner), metaVariantList(variants)),
		env, wantReturn, unsafe)
}

// metaFieldsCarryPayload reports whether any variant holds a payload, which is
// what makes the second capture necessary.
func metaFieldsCarryPayload(variants []metaField) bool {
	for _, variant := range variants {
		if variant.typ != "" {
			return true
		}
	}
	return false
}

// metaVariantList converts resolved variants into the shape the expansion
// reads, so the arms every phase builds come from one description.
func metaVariantList(variants []metaField) []ast.MetaVariant {
	out := make([]ast.MetaVariant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, ast.MetaVariant{Name: variant.name, Payload: string(variant.typ)})
	}
	return out
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
		resolved, err := c.resolveMetaTypeText(string(c.types.substituteTypeParams(
			Type(arg), c.typeArgValues)))
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
	case stdmeta.VariantType:
		variant, err := c.metaCapture(form, args)
		if err != nil {
			return "", err
		}
		if variant.typ == "" {
			return "", metaFormError{errorf(
				"comptime error: variant `%s::%s` carries no payload, so "+
					"`%s` names nothing; ask `%s` first",
				variant.owner, variant.name, stdmeta.VariantType, stdmeta.HasPayload)}
		}
		return string(variant.typ), nil
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
	case stdmeta.FieldName, stdmeta.VariantName:
		if _, err := c.metaCapture(form, staticArgs); err != nil {
			return "", err
		}
		return typeByteString, nil
	case stdmeta.Field:
		return c.checkMetaFieldBorrow(form, staticArgs, args, env, unsafe)
	case stdmeta.Variant:
		return c.checkMetaVariant(form, staticArgs, args, env, unsafe)
	case stdmeta.Construct:
		return c.checkMetaConstruct(staticArgs, args, env, unsafe)
	case stdmeta.IsStruct, stdmeta.IsEnum, stdmeta.IsUnion, stdmeta.IsOptional,
		stdmeta.IsOwner, stdmeta.HasPayload:
		if _, err := c.metaPredicate(form, staticArgs); err != nil {
			return "", err
		}
		return typeBool, nil
	case stdmeta.Unsupported:
		return "", c.metaUnsupported(staticArgs)
	case stdmeta.PublicFields, stdmeta.Variants:
		return "", errorf("comptime error: `%s` is only written as a comptime for list", form)
	default:
		return "", errorf("comptime error: unsupported form `%s`", form)
	}
}

// checkMetaVariant types `std::meta::variant<T, v>(payload)` by checking the
// `T::v(payload)` it stands for (ast.VariantExpansion). The arm a walk means
// to produce is not a type the caller can name, so the form names it; what is
// built is the ordinary union value, checked by the ordinary rules.
func (c *Checker) checkMetaVariant(
	form stdmeta.Form,
	staticArgs []string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	variant, err := c.metaCapture(form, staticArgs)
	if err != nil {
		return "", err
	}
	if err := checkVariantArgs(form, variant, len(args)); err != nil {
		return "", err
	}
	return c.checkExpr(
		ast.VariantExpansion(string(variant.owner), variant.name, args), env, unsafe)
}

// checkVariantArgs refuses a payload for a tag and a missing one for a variant
// that carries something. A variant holds at most one value (SPEC §6.8), so
// the count is fixed by which variant the capture names.
func checkVariantArgs(form stdmeta.Form, variant metaField, count int) error {
	want := 0
	if variant.typ != "" {
		want = 1
	}
	if count != want {
		return errorf("comptime error: `%s` builds `%s::%s` from %d arguments, got %d",
			form, variant.owner, variant.name, want, count)
	}
	return nil
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
	resolved, err := c.resolveMetaTypeText(text)
	if isMetaFormError(err) {
		return "", err
	}
	if err == nil && resolved != text {
		return resolved, nil
	}
	parsed, ok := c.types.lookup(Type(text))
	if !ok {
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

// metaPredicate answers a compile-time predicate about a type, or about one
// variant of one.
func (c *Checker) metaPredicate(form stdmeta.Form, staticArgs []string) (bool, error) {
	if form == stdmeta.HasPayload {
		variant, err := c.metaCapture(form, staticArgs)
		if err != nil {
			return false, err
		}
		return variant.typ != "", nil
	}
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
	case stdmeta.IsEnum:
		return c.enums[string(subject)] != nil, nil
	case stdmeta.IsUnion:
		return c.unions[string(subject)] != nil, nil
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
