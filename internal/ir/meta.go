package ir

import (
	"fmt"
	"sort"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/quote"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/typ"
)

// metaField is the field or variant a comptime capture names for the length of
// one expansion. A variant that carries no payload has no type, so typ is
// empty for it.
type metaField struct {
	owner   string
	name    string
	typ     string
	variant bool
	// origin is the error set that declared a member of an error set, and ""
	// otherwise; the member is spelled and built through that set.
	origin string
}

// variantOwner names the declaration a variant is built from: the declaring
// set for an error member, the type itself otherwise.
func (f metaField) variantOwner() string {
	if f.origin != "" {
		return f.origin
	}
	return f.owner
}

// variantSpelling is what `variant_name` answers: the bare tag of an enum or
// union, and `Origin::Member` for an error (SPEC §11.2).
func (f metaField) variantSpelling() string {
	if f.origin != "" {
		return f.origin + "::" + f.name
	}
	return f.name
}

// variantKey identifies a variant the way an arm names it, so a combined
// error set that receives one member name from two sets keeps them apart.
func (f metaField) variantKey() string {
	if f.origin != "" {
		return f.origin + "::" + f.name
	}
	return f.name
}

// matchArmKey identifies the variant an arm covers, paired with variantKey.
func matchArmKey(arm ast.MatchArm) string {
	if arm.TagSet != "" {
		return arm.TagSet + "::" + arm.Tag
	}
	return arm.Tag
}

// lowerComptimeForStmt lowers the body once per field. The loop itself has no
// runtime form: what reaches the IR is the expansions, in field order.
func (l *lowerer) lowerComptimeForStmt(stmt *ast.ComptimeForStmt) error {
	fields, err := l.comptimeForFields(stmt.List)
	if err != nil {
		return err
	}
	previous, had := l.metaFields[stmt.Name]
	defer func() {
		if had {
			l.metaFields[stmt.Name] = previous
			return
		}
		delete(l.metaFields, stmt.Name)
	}()
	for _, field := range fields {
		l.metaFields[stmt.Name] = field
		if err := l.lowerBlock(stmt.Body); err != nil {
			return err
		}
	}
	return nil
}

// comptimeForFields reads the list a `comptime for` walks.
func (l *lowerer) comptimeForFields(list ast.Expression) ([]metaField, error) {
	call, ok := list.(*ast.CallExpr)
	if !ok {
		return nil, errComptimeForList
	}
	apply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return nil, errComptimeForList
	}
	switch stdmeta.Form(apply.Callee.String()) {
	case stdmeta.PublicFields:
		return l.publicFields(l.resolveTypeArgs(apply.TypeArg))
	case stdmeta.Variants:
		return l.variants(l.resolveTypeArgs(apply.TypeArg))
	default:
		return nil, errComptimeForList
	}
}

// errComptimeForList names the lists a `comptime for` accepts.
var errComptimeForList = fmt.Errorf(
	"ir error: comptime for expects `std::meta::public_fields<T>()` or " +
		"`std::meta::variants<T>()`")

// variants lists an enum's tags or a union's variants in declaration order.
func (l *lowerer) variants(typeArg string) ([]metaField, error) {
	if decl, ok := l.enumDecls[typeArg]; ok {
		out := make([]metaField, 0, len(decl.Tags))
		for _, tag := range decl.Tags {
			out = append(out, metaField{owner: typeArg, name: tag, variant: true})
		}
		return out, nil
	}
	if set, ok := l.module.ErrorSets[typeArg]; ok {
		return l.errorMembers(typeArg, set), nil
	}
	decl, ok := l.unionDecls[typeArg]
	if !ok {
		return nil, fmt.Errorf("ir error: `%s` expects an enum, union, or error set, got %s",
			stdmeta.Variants, typeArg)
	}
	out := make([]metaField, 0, len(decl.Variants))
	for _, variant := range decl.Variants {
		out = append(out, metaField{
			owner:   typeArg,
			name:    variant.Name,
			typ:     stdmeta.ResolveElementTypeForms(typ.Text(variant.Payload)),
			variant: true,
		})
	}
	return out, nil
}

// errorMembers lists an error set's members in declaration order, each with
// the set that declared it. A `{ }` set's members were numbered as they were
// declared, so their codes put them in order; a combined set walks the sets
// it names, in the order it named them.
func (l *lowerer) errorMembers(owner string, set Enum) []metaField {
	if set.Origins != nil {
		var out []metaField
		for _, origin := range set.Origins {
			for _, member := range l.errorMembers(owner, l.module.ErrorSets[origin]) {
				out = append(out, metaField{owner: owner, name: member.name, variant: true,
					origin: member.origin})
			}
		}
		return out
	}
	names := make([]string, 0, len(set.Tags))
	for name := range set.Tags {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return set.Tags[names[i]] < set.Tags[names[j]] })
	out := make([]metaField, 0, len(names))
	for _, name := range names {
		out = append(out, metaField{owner: owner, name: name, variant: true, origin: set.Name})
	}
	return out
}

// publicFields lists a struct's public fields in declaration order.
func (l *lowerer) publicFields(typeArg string) ([]metaField, error) {
	decl, ok := l.structDecls[typeArg]
	if !ok {
		return nil, fmt.Errorf("ir error: `%s` expects a struct, got %s",
			stdmeta.PublicFields, typeArg)
	}
	fields := make([]metaField, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		if !field.Public {
			continue
		}
		fields = append(fields, metaField{
			owner: typeArg,
			name:  field.Name,
			typ:   stdmeta.ResolveElementTypeForms(typ.Text(field.TypeName)),
		})
	}
	return fields, nil
}

// metaCapture resolves the capture named in a form's static arguments.
func (l *lowerer) metaCapture(form stdmeta.Form, typeArg string) (metaField, error) {
	args := splitStaticArgs(typeArg)
	if len(args) != 2 {
		return metaField{}, fmt.Errorf("ir error: `%s` expects 2 static arguments", form)
	}
	field, ok := l.metaFields[args[1]]
	if !ok {
		return metaField{}, fmt.Errorf("ir error: `%s` is not a comptime capture", args[1])
	}
	if field.variant != stdmeta.VariantForm(form) {
		return metaField{}, fmt.Errorf("ir error: `%s` is written against the wrong capture kind",
			form)
	}
	return field, nil
}

// bindMetaField binds one capture for the length of one expansion, returning
// the call that unbinds it. An empty name binds nothing.
func (l *lowerer) bindMetaField(name string, field metaField) func() {
	if name == "" {
		return func() {}
	}
	previous, had := l.metaFields[name]
	l.metaFields[name] = field
	return func() {
		if had {
			l.metaFields[name] = previous
			return
		}
		delete(l.metaFields, name)
	}
}

// matchArmVariants indexes by tag the variants a `comptime match` arm body is
// written against. A match written by hand carries no capture and gets nil.
func (l *lowerer) matchArmVariants(stmt *ast.MatchStmt) (map[string]metaField, error) {
	if stmt.MetaCapture == "" {
		return nil, nil
	}
	variants, err := l.variants(stmt.MetaOwner)
	if err != nil {
		return nil, err
	}
	out := make(map[string]metaField, len(variants))
	for _, variant := range variants {
		out[variant.variantKey()] = variant
	}
	return out, nil
}

// lowerComptimeMatchStmt lowers the match a `comptime match` stands for, with
// the capture bound to the arm's own variant while its body is lowered. The
// value is lowered first because it is what names the declaration the arms
// come from, and lowering it twice would emit it twice.
func (l *lowerer) lowerComptimeMatchStmt(stmt *ast.ComptimeMatchStmt) error {
	subject, err := l.lowerMatchValue(stmt.Value)
	if err != nil {
		return err
	}
	owner := subject.union.Name
	if owner == "" {
		owner = subject.enum.Name
	}
	variants, err := l.variants(owner)
	if err != nil {
		return err
	}
	list := make([]ast.MetaVariant, 0, len(variants))
	for _, variant := range variants {
		list = append(list, ast.MetaVariant{
			Name: variant.name, HasPayload: variant.typ != "", Origin: variant.origin,
		})
	}
	return l.lowerMatchBody(subject, ast.ComptimeMatchExpansion(stmt, owner, list))
}

// lowerMetaApply lowers one `std::meta` form. ok reports whether the callee was
// a form at all, leaving every other call to the ordinary paths.
func (l *lowerer) lowerMetaApply(
	name string,
	typeArg string,
	args []ast.Expression,
) (Value, bool, error) {
	form := stdmeta.Form(name)
	if _, known := stdmeta.Lookup(name); !known {
		return Value{}, false, nil
	}
	switch form {
	case stdmeta.FieldName, stdmeta.VariantName:
		field, err := l.metaCapture(form, typeArg)
		if err != nil {
			return Value{}, true, err
		}
		return l.emitConst("[]u8", quote.Bytes(field.variantSpelling())), true, nil
	case stdmeta.TypeName:
		return l.emitConst("[]u8", quote.Bytes(l.resolveTypeArgs(typeArg))), true, nil
	case stdmeta.Field:
		value, err := l.lowerMetaFieldBorrow(form, typeArg, args)
		return value, true, err
	case stdmeta.Variant:
		value, err := l.lowerMetaVariant(form, typeArg, args)
		return value, true, err
	case stdmeta.Construct:
		value, err := l.lowerMetaConstruct(typeArg, args)
		return value, true, err
	case stdmeta.IsStruct, stdmeta.IsEnum, stdmeta.IsUnion, stdmeta.IsOptional,
		stdmeta.IsOwner, stdmeta.IsError, stdmeta.ReleaseNamesAllocator, stdmeta.HasPayload:
		known, err := l.metaPredicate(form, typeArg)
		if err != nil {
			return Value{}, true, err
		}
		return l.emitConst("bool", fmt.Sprintf("%t", known)), true, nil
	default:
		return Value{}, true, fmt.Errorf("ir error: `%s` cannot be called", name)
	}
}

// lowerMetaFieldBorrow lowers `std::meta::field<T, f>(value)` as the field read
// it is defined to be (ADR-0113).
func (l *lowerer) lowerMetaFieldBorrow(
	form stdmeta.Form,
	typeArg string,
	args []ast.Expression,
) (Value, error) {
	field, err := l.metaCapture(form, typeArg)
	if err != nil {
		return Value{}, err
	}
	if len(args) != 1 {
		return Value{}, fmt.Errorf("ir error: `%s` expects 1 argument, got %d", form, len(args))
	}
	return l.lowerFieldExpr(&ast.FieldExpr{Receiver: args[0], Name: field.name})
}

// lowerMetaVariant lowers `std::meta::variant<T, v>(payload)` as the
// `T::v(payload)` it is defined to be (ast.VariantExpansion).
func (l *lowerer) lowerMetaVariant(
	form stdmeta.Form,
	typeArg string,
	args []ast.Expression,
) (Value, error) {
	variant, err := l.metaCapture(form, typeArg)
	if err != nil {
		return Value{}, err
	}
	return l.lowerExpr(ast.VariantExpansion(variant.variantOwner(), variant.name, args))
}

// lowerMetaConstruct lowers `std::meta::construct<T, worker>(args...)` as the
// code it stands for. The expansion is built in one place
// (ast.ConstructExpansion), so what is emitted here is what the two checkers
// already read; lowering adds no rule of its own.
//
// The statements go in a cleanup frame of their own: the `errdefer` each owner
// field registers protects the fields already built, and it must not outlive
// the literal that takes them.
func (l *lowerer) lowerMetaConstruct(typeArg string, args []ast.Expression) (Value, error) {
	staticArgs := splitStaticArgs(typeArg)
	if len(staticArgs) != 2 {
		return Value{}, fmt.Errorf("ir error: `%s` expects 2 static arguments",
			stdmeta.Construct)
	}
	owner := l.resolveType(staticArgs[0])
	fields, err := l.publicFields(owner)
	if err != nil {
		return Value{}, err
	}
	expansion := make([]ast.ConstructField, 0, len(fields))
	for _, field := range fields {
		expansion = append(expansion, ast.ConstructField{Name: field.name, Type: field.typ})
	}
	statements, literal := ast.ConstructExpansion(
		owner, staticArgs[1], expansion, args, l.deinitOwners)
	l.deferFrames = append(l.deferFrames, nil)
	defer func() { l.deferFrames = l.deferFrames[:len(l.deferFrames)-1] }()
	for _, stmt := range statements {
		// The expansion registers cleanups the way a block does, which is the
		// one place `errdefer` is accepted; the statements are a block in
		// everything but the braces.
		if errDefer, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := l.recordCleanup(errDefer.Expr, true); err != nil {
				return Value{}, err
			}
			continue
		}
		if err := l.lowerStmt(stmt); err != nil {
			return Value{}, err
		}
	}
	built, err := l.lowerExpr(literal)
	if err != nil {
		return Value{}, err
	}
	// The union carries the enclosing function's error set, the same set the
	// expansion's `try`s already propagate into (ADR-0128).
	wrap := "!" + owner
	if errorPart, _, ok := errorUnionParts(l.types, l.current.Return); ok && errorPart != "" {
		wrap = errorPart + "!" + owner
	}
	return l.emit("error.ok", wrap, []Value{built}, ""), nil
}

// resolveMetaTypeDeep resolves the forms inside a wrapped spelling. A worker
// returns `!std::meta::field_type<T, f>`, so the form sits under the error
// union rather than at the top, and the wrapper is rebuilt around what the
// form resolved to.
func (l *lowerer) resolveMetaTypeDeep(text string) string {
	if resolved := l.resolveMetaTypeText(text); resolved != text {
		return resolved
	}
	parsed, err := l.types.Parse(text)
	if err != nil {
		return text
	}
	switch node := parsed.(type) {
	case *typ.ErrorUnion:
		inner := l.resolveMetaTypeDeep(node.Ok.String())
		return (&typ.ErrorUnion{Err: node.Err, Ok: &typ.Name{Path: []string{inner}}}).String()
	case *typ.Optional:
		return "?" + l.resolveMetaTypeDeep(node.Elem.String())
	case *typ.Slice:
		return "[]" + l.resolveMetaTypeDeep(node.Elem.String())
	case *typ.Name:
		if len(node.Args) == 0 {
			return text
		}
		args := make([]typ.Type, 0, len(node.Args))
		for _, arg := range node.Args {
			args = append(args, &typ.Name{Path: []string{l.resolveMetaTypeDeep(arg.String())}})
		}
		return (&typ.Name{Path: node.Path, Args: args}).String()
	default:
		return text
	}
}

// metaPredicate answers a compile-time predicate about a type.
func (l *lowerer) metaPredicate(form stdmeta.Form, typeArg string) (bool, error) {
	if form == stdmeta.HasPayload {
		variant, err := l.metaCapture(form, typeArg)
		if err != nil {
			return false, err
		}
		return variant.typ != "", nil
	}
	subject := l.resolveTypeArgs(typeArg)
	switch form {
	case stdmeta.IsStruct:
		_, ok := l.structDecls[subject]
		return ok, nil
	case stdmeta.IsEnum:
		_, ok := l.enumDecls[subject]
		return ok, nil
	case stdmeta.IsUnion:
		_, ok := l.unionDecls[subject]
		return ok, nil
	case stdmeta.IsError:
		_, ok := l.module.ErrorSets[subject]
		return ok, nil
	case stdmeta.IsOptional:
		_, ok := typ.OptionalElem(subject)
		return ok, nil
	case stdmeta.IsArray:
		_, ok := arrayElementType(subject)
		return ok, nil
	case stdmeta.IsBox:
		_, ok := boxElementType(subject)
		return ok, nil
	case stdmeta.IsMap:
		_, _, ok := mapTypeArgs(subject)
		return ok, nil
	case stdmeta.IsOwner:
		return ast.OwnerType(l.deinitOwners, subject), nil
	case stdmeta.ReleaseNamesAllocator:
		return ast.ReleaseNames(l.releaseAllocators, subject), nil
	case stdmeta.HasPublicFields:
		fields, err := l.publicFields(subject)
		return err == nil && len(fields) > 0, nil
	default:
		return false, fmt.Errorf("ir error: `%s` is not a predicate", form)
	}
}

// metaPredicateCall answers a `std::meta` predicate written as a comptime
// condition, and reports whether the call was one.
func (l *lowerer) metaPredicateCall(expr *ast.CallExpr) (bool, bool) {
	apply, ok := expr.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return false, false
	}
	name := apply.Callee.String()
	if !stdmeta.Predicate(name) || len(expr.Args) != 0 {
		return false, false
	}
	value, err := l.metaPredicate(stdmeta.Form(name), apply.TypeArg)
	if err != nil {
		return false, false
	}
	return value, true
}

// resolveMetaTypeText rewrites the forms written where a type goes. Text with
// no form in it comes back unchanged.
func (l *lowerer) resolveMetaTypeText(text string) string {
	form, args, ok := stdmeta.SplitApply(text)
	if !ok {
		return text
	}
	for idx, arg := range args {
		args[idx] = l.resolveMetaTypeText(arg)
	}
	switch form {
	case stdmeta.FieldType, stdmeta.VariantType:
		if len(args) != 2 {
			return text
		}
		field, ok := l.metaFields[args[1]]
		if !ok || field.typ == "" {
			return text
		}
		return field.typ
	case stdmeta.Element:
		if len(args) != 1 {
			return text
		}
		container, err := l.types.Parse(l.resolveType(args[0]))
		if err == nil {
			if element, ok := stdmeta.ElementType(container); ok {
				return element.String()
			}
		}
		return text
	default:
		return text
	}
}
