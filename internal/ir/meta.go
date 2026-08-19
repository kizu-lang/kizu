package ir

import (
	"fmt"

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

// comptimeForFields reads the field list a `comptime for` walks.
func (l *lowerer) comptimeForFields(list ast.Expression) ([]metaField, error) {
	call, ok := list.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("ir error: comptime for expects `std::meta::public_fields<T>()`")
	}
	apply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return nil, fmt.Errorf("ir error: comptime for expects `std::meta::public_fields<T>()`")
	}
	if apply.Callee.String() != string(stdmeta.PublicFields) {
		return nil, fmt.Errorf("ir error: comptime for expects `std::meta::public_fields<T>()`")
	}
	return l.publicFields(l.resolveTypeArgs(apply.TypeArg))
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
			typ:   typ.Text(field.TypeName),
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
		return metaField{}, fmt.Errorf("ir error: `%s` is not a comptime for capture", args[1])
	}
	return field, nil
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
	case stdmeta.FieldName:
		field, err := l.metaCapture(form, typeArg)
		if err != nil {
			return Value{}, true, err
		}
		return l.emitConst("[]u8", fmt.Sprintf("%q", field.name)), true, nil
	case stdmeta.Field:
		value, err := l.lowerMetaFieldBorrow(form, typeArg, args)
		return value, true, err
	case stdmeta.IsStruct, stdmeta.IsOptional:
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

// metaPredicate answers a compile-time predicate about a type.
func (l *lowerer) metaPredicate(form stdmeta.Form, typeArg string) (bool, error) {
	subject := l.resolveTypeArgs(typeArg)
	switch form {
	case stdmeta.IsStruct:
		_, ok := l.structDecls[subject]
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
		_, ok := mapValueType(subject)
		return ok, nil
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
	case stdmeta.FieldType:
		if len(args) != 2 {
			return text
		}
		field, ok := l.metaFields[args[1]]
		if !ok {
			return text
		}
		return field.typ
	case stdmeta.Element:
		if len(args) != 1 {
			return text
		}
		return metaElementType(l.resolveType(args[0]))
	default:
		return text
	}
}

// metaElementType names what a container holds.
func metaElementType(container string) string {
	if elem, ok := typ.OptionalElem(container); ok {
		return elem
	}
	if elem, ok := arrayElementType(container); ok {
		return elem
	}
	if elem, ok := boxElementType(container); ok {
		return elem
	}
	return container
}
