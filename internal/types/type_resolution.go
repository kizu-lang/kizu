package types

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// typeResolutionIssueKind is one closed semantic failure from type
// resolution. Allocation and diagnostic ownership stay outside this result.
type typeResolutionIssueKind uint8

const (
	typeResolutionNone typeResolutionIssueKind = iota
	typeResolutionMissing
	typeResolutionUnknown
	typeResolutionWrapsOptional
	typeResolutionErrorSetRequired
	typeResolutionOptionalStaticArg
	typeResolutionSingleGenericArity
	typeResolutionMapArity
	typeResolutionMapKey
	typeResolutionUnknownGeneric
	typeResolutionUserGenericArity
	typeResolutionOptionalOptional
	typeResolutionOptionalErrorUnion
	typeResolutionMetaNotType
	typeResolutionMetaArity
	typeResolutionMetaCaptureUnbound
	typeResolutionMetaCaptureKind
	typeResolutionMetaCaptureOwner
	typeResolutionMetaVariantNoPayload
	typeResolutionMetaElementArity
	typeResolutionMetaElementUnsupported
)

// typeResolutionIssue carries copy values needed by the checker diagnostic
// boundary. Fields are interpreted by kind; no owned diagnostic crosses this
// resolver.
type typeResolutionIssue struct {
	kind     typeResolutionIssueKind
	subject  Type
	related  Type
	form     stdmeta.Form
	field    metaField
	expected int
	actual   int
}

// present reports whether resolution produced a semantic failure.
func (i typeResolutionIssue) present() bool {
	return i.kind != typeResolutionNone
}

// resolvedType turns one copy-only resolution result into the checker's
// existing diagnostic boundary.
func resolvedType(value Type, issue typeResolutionIssue) (Type, error) {
	if issue.present() {
		return "", typeResolutionError(issue)
	}
	return value, nil
}

// typeResolutionError constructs the unchanged diagnostic for one semantic
// resolution failure.
func typeResolutionError(issue typeResolutionIssue) error {
	switch issue.kind {
	case typeResolutionMissing:
		return errorf("type error: missing type")
	case typeResolutionUnknown:
		return errorf("type error: unknown type `%s`", issue.subject)
	case typeResolutionWrapsOptional:
		return errorf("type error: `%s` cannot wrap an optional yet", issue.subject)
	case typeResolutionErrorSetRequired:
		return errorf(
			"type error: the error of `E!T` must be an `error` set, got `%s`",
			issue.subject)
	case typeResolutionOptionalStaticArg:
		return errorf(
			"type error: optional `%s` cannot be a static argument yet", issue.subject)
	case typeResolutionSingleGenericArity:
		return errorf("type error: `%s` expects 1 static argument", issue.subject)
	case typeResolutionMapArity:
		return errorf("type error: std::map::Map expects 2 static arguments")
	case typeResolutionMapKey:
		return errorf("type error: std::map::Map key type must be one of %s",
			typ.MapKeyTypeNames())
	case typeResolutionUnknownGeneric:
		return errorf("type error: unknown generic type `%s`", issue.subject)
	case typeResolutionUserGenericArity:
		return errorf("type error: `%s` expects %d static arguments",
			issue.subject, issue.expected)
	case typeResolutionOptionalOptional:
		return errorf("type error: optional cannot wrap an optional `%s`", issue.subject)
	case typeResolutionOptionalErrorUnion:
		return errorf(
			"type error: optional cannot wrap an error union `%s`; spell it `E!?T`",
			issue.subject)
	default:
		return metaTypeResolutionError(issue)
	}
}

// metaTypeResolutionError constructs the std::meta half of the same closed
// diagnostic mapping.
func metaTypeResolutionError(issue typeResolutionIssue) error {
	switch issue.kind {
	case typeResolutionMetaNotType:
		return errorf("comptime error: `%s` does not name a type", issue.form)
	case typeResolutionMetaArity:
		return errorf("comptime error: `%s` expects %d static arguments, got %d",
			issue.form, issue.expected, issue.actual)
	case typeResolutionMetaCaptureUnbound:
		return errorf("comptime error: `%s` is not a comptime capture", issue.subject)
	case typeResolutionMetaCaptureKind:
		return errorf("comptime error: `%s` expects a %s capture, got `%s`",
			issue.form, metaCaptureKind(stdmeta.VariantForm(issue.form)), issue.subject)
	case typeResolutionMetaCaptureOwner:
		return errorf("comptime error: capture `%s` belongs to %s, not %s",
			issue.subject, issue.field.owner, issue.related)
	case typeResolutionMetaVariantNoPayload:
		return errorf(
			"comptime error: variant `%s::%s` carries no payload, so "+
				"`%s` names nothing; ask `%s` first",
			issue.field.owner, issue.field.name,
			stdmeta.VariantType, stdmeta.HasPayload)
	case typeResolutionMetaElementArity:
		return errorf("comptime error: `%s` expects 1 static argument", stdmeta.Element)
	case typeResolutionMetaElementUnsupported:
		return errorf(
			"comptime error: `%s` expects ?T, Array<T>, Box<T>, or Map<K, V>, got %s",
			stdmeta.Element, issue.subject)
	default:
		return errorf("internal error: unknown type resolution issue %d", issue.kind)
	}
}

// resolveType validates one compiler-owned type spelling without constructing
// a diagnostic.
func (c *Checker) resolveType(name string) (Type, typeResolutionIssue) {
	resolved := Type(name)
	rewritten, issue := c.resolveMetaType(resolved)
	if issue.present() && issue.kind != typeResolutionMetaCaptureUnbound {
		return "", issue
	}
	if !issue.present() {
		resolved = rewritten
	}
	parsed, ok := c.types.lookup(resolved)
	if !ok {
		return "", typeResolutionIssue{
			kind: typeResolutionUnknown, subject: resolved,
		}
	}
	return c.resolveTypeNode(parsed)
}

// resolveTypeNode validates one parser-owned type graph without constructing a
// diagnostic.
func (c *Checker) resolveTypeNode(parsed typ.Type) (Type, typeResolutionIssue) {
	if parsed == nil {
		return "", typeResolutionIssue{kind: typeResolutionMissing}
	}
	c.types.remember(parsed)
	name := Type(parsed.String())
	switch node := parsed.(type) {
	case *typ.ErrorUnion:
		return c.resolveErrorUnionType(name, node)
	case *typ.Borrow:
		return c.resolveWrappingType(name, node.Elem)
	case *typ.Slice:
		return c.resolveWrappingType(name, node.Elem)
	case *typ.Buffer:
		return c.resolveWrappingType(name, node.Elem)
	case *typ.Optional:
		return c.resolveNullableType(name, node.Elem)
	case *typ.Func:
		return c.resolveFuncType(name, node)
	case *typ.Name:
		if len(node.Args) == 0 {
			return c.resolveNamedType(name)
		}
		return c.resolveGenericType(
			name, Type(strings.Join(node.Path, "::")), node.Args)
	default:
		return "", typeResolutionIssue{
			kind: typeResolutionUnknown, subject: name,
		}
	}
}

// resolveFuncType validates the parameter and result types a function pointer
// spells. The pointer itself names no declaration, so there is nothing to look
// up: it is well formed when the types it mentions are.
func (c *Checker) resolveFuncType(
	name Type,
	node *typ.Func,
) (Type, typeResolutionIssue) {
	for _, param := range node.Params {
		if _, issue := c.resolveTypeNode(param); issue.present() {
			return "", issue
		}
	}
	if _, issue := c.resolveTypeNode(node.Result); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolveWrappingType validates the element of a borrow, slice, buffer, or
// error-union success wrapper.
func (c *Checker) resolveWrappingType(
	name Type,
	elem typ.Type,
) (Type, typeResolutionIssue) {
	if _, ok := elem.(*typ.Optional); ok {
		return "", typeResolutionIssue{
			kind: typeResolutionWrapsOptional, subject: name,
		}
	}
	if _, issue := c.resolveTypeNode(elem); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolveNamedType validates primitive, declared, and type-parameter names.
func (c *Checker) resolveNamedType(name Type) (Type, typeResolutionIssue) {
	if c.typeParams.contains(string(name)) || c.isTypeName(string(name)) {
		return name, typeResolutionIssue{}
	}
	return "", typeResolutionIssue{
		kind: typeResolutionUnknown, subject: name,
	}
}

// resolveErrorUnionType validates `!T` and the typed `Error!T` spelling.
func (c *Checker) resolveErrorUnionType(
	name Type,
	node *typ.ErrorUnion,
) (Type, typeResolutionIssue) {
	if node.Err != nil {
		errName, issue := c.resolveTypeNode(node.Err)
		if issue.present() {
			return "", issue
		}
		if c.errorSets[string(errName)] == nil {
			return "", typeResolutionIssue{
				kind: typeResolutionErrorSetRequired, subject: errName,
			}
		}
	}
	if _, ok := node.Ok.(*typ.Optional); ok {
		if _, issue := c.resolveTypeNode(node.Ok); issue.present() {
			return "", issue
		}
		return name, typeResolutionIssue{}
	}
	return c.resolveWrappingType(name, node.Ok)
}

// resolveGenericType validates supported generic-like type spellings.
func (c *Checker) resolveGenericType(
	name Type,
	base Type,
	args []typ.Type,
) (Type, typeResolutionIssue) {
	if optional, ok := firstOptionalTypeArg(args); ok {
		return "", typeResolutionIssue{
			kind: typeResolutionOptionalStaticArg, subject: optional,
		}
	}
	if shape, ok := stdmeta.Lookup(string(base)); ok {
		return c.resolveMetaTypeForm(name, stdmeta.Form(base), shape, args)
	}
	switch base {
	case "std::mem::Box":
		arg, issue := singleTypeArg(base, args)
		if issue.present() {
			return "", issue
		}
		if _, issue := c.resolveTypeNode(arg); issue.present() {
			return "", issue
		}
		return name, typeResolutionIssue{}
	case "std::map::Map":
		return c.resolveMapType(name, args)
	case "ptr":
		arg, issue := singleTypeArg(base, args)
		if issue.present() {
			return "", issue
		}
		return c.resolvePointerType(name, arg)
	}

	if declaration := c.structs[string(base)]; declaration != nil {
		return c.resolveUserGenericType(name, base, args, declaration.TypeParams)
	}
	if union := c.unions[string(base)]; union != nil {
		return c.resolveUserGenericType(name, base, args, union.typeParams)
	}

	if !isKnownGenericBase(string(base)) {
		return "", typeResolutionIssue{
			kind: typeResolutionUnknownGeneric, subject: base,
		}
	}
	arg, issue := singleTypeArg(base, args)
	if issue.present() {
		return "", issue
	}
	if _, issue := c.resolveTypeNode(arg); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolveMetaTypeForm validates a std::meta form written where a type goes.
func (c *Checker) resolveMetaTypeForm(
	name Type,
	form stdmeta.Form,
	shape stdmeta.Shape,
	args []typ.Type,
) (Type, typeResolutionIssue) {
	if !shape.Type {
		return "", typeResolutionIssue{
			kind: typeResolutionMetaNotType, form: form,
		}
	}
	if len(args) != shape.StaticArgs {
		return "", typeResolutionIssue{
			kind: typeResolutionMetaArity, form: form,
			expected: shape.StaticArgs, actual: len(args),
		}
	}
	resolved, issue := c.resolveMetaType(name)
	if issue.present() {
		if issue.kind == typeResolutionMetaCaptureUnbound {
			return name, typeResolutionIssue{}
		}
		return "", issue
	}
	if resolved == name {
		return name, typeResolutionIssue{}
	}
	return c.resolveType(string(resolved))
}

// resolveUserGenericType validates static type arguments for user declarations.
func (c *Checker) resolveUserGenericType(
	name Type,
	base Type,
	args []typ.Type,
	types []string,
) (Type, typeResolutionIssue) {
	if len(args) != len(types) {
		return "", typeResolutionIssue{
			kind:    typeResolutionUserGenericArity,
			subject: base, expected: len(types), actual: len(args),
		}
	}
	for _, arg := range args {
		if _, issue := c.resolveTypeNode(arg); issue.present() {
			return "", issue
		}
	}
	return name, typeResolutionIssue{}
}

// resolveMapType validates the symbol-table map spelling.
func (c *Checker) resolveMapType(
	name Type,
	args []typ.Type,
) (Type, typeResolutionIssue) {
	if len(args) != 2 {
		return "", typeResolutionIssue{kind: typeResolutionMapArity}
	}
	key := Type(args[0].String())
	if !typ.IsMapKey(string(key)) && !c.typeParams.contains(string(key)) {
		return "", typeResolutionIssue{kind: typeResolutionMapKey}
	}
	if _, issue := c.resolveTypeNode(args[1]); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolveNullableType validates nullable pointer and value types.
func (c *Checker) resolveNullableType(
	name Type,
	elem typ.Type,
) (Type, typeResolutionIssue) {
	switch elem.(type) {
	case *typ.Optional:
		return "", typeResolutionIssue{
			kind: typeResolutionOptionalOptional, subject: Type(elem.String()),
		}
	case *typ.ErrorUnion:
		return "", typeResolutionIssue{
			kind: typeResolutionOptionalErrorUnion, subject: Type(elem.String()),
		}
	}
	if _, issue := c.resolveTypeNode(elem); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolvePointerType validates raw pointer element types.
func (c *Checker) resolvePointerType(
	name Type,
	arg typ.Type,
) (Type, typeResolutionIssue) {
	if constant, ok := arg.(*typ.Const); ok {
		arg = constant.Elem
	}
	if _, issue := c.resolveTypeNode(arg); issue.present() {
		return "", issue
	}
	return name, typeResolutionIssue{}
}

// resolveMetaType rewrites one top-level std::meta type form.
func (c *Checker) resolveMetaType(text Type) (Type, typeResolutionIssue) {
	parsed, ok := c.types.lookup(text)
	if !ok {
		return text, typeResolutionIssue{}
	}
	node, ok := parsed.(*typ.Name)
	if !ok || len(node.Args) == 0 {
		return text, typeResolutionIssue{}
	}
	base := strings.Join(node.Path, "::")
	if _, ok := stdmeta.Lookup(base); !ok {
		return text, typeResolutionIssue{}
	}
	form := stdmeta.Form(base)
	args := make([]Type, 0, len(node.Args))
	for _, arg := range node.Args {
		substituted := c.types.substituteTypeParams(
			c.types.remember(arg), c.typeArgValues)
		resolved, issue := c.resolveMetaType(substituted)
		if issue.present() {
			return "", issue
		}
		args = append(args, resolved)
	}
	switch form {
	case stdmeta.FieldType:
		field, issue := c.resolveMetaCapture(form, args)
		if issue.present() {
			return "", issue
		}
		return field.typ, typeResolutionIssue{}
	case stdmeta.VariantType:
		variant, issue := c.resolveMetaCapture(form, args)
		if issue.present() {
			return "", issue
		}
		if variant.typ == "" {
			return "", typeResolutionIssue{
				kind: typeResolutionMetaVariantNoPayload, field: variant,
			}
		}
		return variant.typ, typeResolutionIssue{}
	case stdmeta.Element:
		return c.resolveMetaElement(args)
	default:
		return "", typeResolutionIssue{
			kind: typeResolutionMetaNotType, form: form,
		}
	}
}

// resolveMetaCapture resolves a field or variant capture as copy data.
func (c *Checker) resolveMetaCapture(
	form stdmeta.Form,
	args []Type,
) (metaField, typeResolutionIssue) {
	if len(args) != 2 {
		return metaField{}, typeResolutionIssue{
			kind: typeResolutionMetaArity, form: form,
			expected: 2, actual: len(args),
		}
	}
	field, ok := c.metaFields[string(args[1])]
	if !ok {
		return metaField{}, typeResolutionIssue{
			kind: typeResolutionMetaCaptureUnbound, subject: args[1],
		}
	}
	if field.variant != stdmeta.VariantForm(form) {
		return metaField{}, typeResolutionIssue{
			kind: typeResolutionMetaCaptureKind,
			form: form, subject: args[1], field: field,
		}
	}
	owner, issue := c.resolveType(string(args[0]))
	if issue.present() {
		return metaField{}, issue
	}
	if owner != field.owner {
		return metaField{}, typeResolutionIssue{
			kind:    typeResolutionMetaCaptureOwner,
			subject: args[1], related: owner, field: field,
		}
	}
	return field, typeResolutionIssue{}
}

// resolveMetaElement names the type held by one supported container.
func (c *Checker) resolveMetaElement(args []Type) (Type, typeResolutionIssue) {
	if len(args) != 1 {
		return "", typeResolutionIssue{kind: typeResolutionMetaElementArity}
	}
	container, issue := c.resolveType(string(args[0]))
	if issue.present() {
		return "", issue
	}
	if parsed, ok := c.types.lookup(container); ok {
		if element, ok := stdmeta.ElementType(parsed); ok {
			return c.resolveType(element.String())
		}
	}
	return "", typeResolutionIssue{
		kind: typeResolutionMetaElementUnsupported, subject: container,
	}
}

// firstOptionalTypeArg returns the first optional static type argument.
func firstOptionalTypeArg(args []typ.Type) (Type, bool) {
	for _, arg := range args {
		text := Type(arg.String())
		if _, ok := optionalElem(text); ok {
			return text, true
		}
	}
	return "", false
}

// singleTypeArg returns the only argument for a one-parameter generic.
func singleTypeArg(base Type, args []typ.Type) (typ.Type, typeResolutionIssue) {
	if len(args) != 1 {
		return nil, typeResolutionIssue{
			kind: typeResolutionSingleGenericArity, subject: base,
		}
	}
	return args[0], typeResolutionIssue{}
}
