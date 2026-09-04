// Package stdmeta names the `std::meta` forms, the shape each one is spelled
// with, and the closed container rule shared by every compiler phase.
//
// These are not functions. Like `type<T>`, each form is resolved by the
// compiler where it is written, so no runtime primitive stands behind them
// (ADR-0113). What every phase needs to agree on is which spellings are forms
// and how many arguments each takes; that agreement lives here so the three
// checkers do not each carry their own list.
package stdmeta

import (
	"errors"
	"strings"

	"github.com/kizu-lang/kizu/internal/typ"
)

// Form is one `std::meta` spelling.
type Form string

// The forms. A `Type` form is written where a type goes; the rest are written
// where an expression goes.
const (
	// IsStruct reports whether its type argument is a declared struct.
	IsStruct Form = "std::meta::is_struct"
	// IsEnum reports whether its type argument is a declared tag enum.
	IsEnum Form = "std::meta::is_enum"
	// IsUnion reports whether its type argument is a declared tagged union.
	IsUnion Form = "std::meta::is_union"
	// IsOptional reports whether its type argument is `?T`.
	IsOptional Form = "std::meta::is_optional"
	// IsArray reports whether its type argument is `std::array::Array<T>`.
	IsArray Form = "std::meta::is_array"
	// IsBox reports whether its type argument is `std::mem::Box<T>`.
	IsBox Form = "std::meta::is_box"
	// IsMap reports whether its type argument is `std::map::Map<K, V>`.
	IsMap Form = "std::meta::is_map"
	// IsOwner reports whether values of its type argument carry a deinit
	// contract. Generic code that holds a `T` has to know whether releasing it
	// means releasing something inside it, and the answer is the same one the
	// checkers read (ast.OwnerType).
	IsOwner Form = "std::meta::is_owner"
	// IsError reports whether its type argument is a declared error set.
	IsError Form = "std::meta::is_error"
	// ReleaseNamesAllocator reports whether releasing its type argument names
	// an allocator, which is to say whether `deinit` takes one (ADR-0132).
	// A container releasing owner elements has to call each element's deinit,
	// and an owner that frees memory and one that closes a descriptor do not
	// take the same argument -- so generic cleanup asks rather than assumes.
	ReleaseNamesAllocator Form = "std::meta::release_names_allocator"
	// HasPublicFields reports whether a struct has any public field. A type
	// whose state is all private looks like an empty object to a walk, so a
	// walk that means to carry data asks this before treating one as a
	// destination.
	HasPublicFields Form = "std::meta::has_public_fields"
	// Element names what a container type holds.
	Element Form = "std::meta::element"
	// PublicFields lists a struct's public fields in declaration order.
	PublicFields Form = "std::meta::public_fields"
	// FieldName is one field's source name as a `[]u8`.
	FieldName Form = "std::meta::field_name"
	// FieldType names one field's type.
	FieldType Form = "std::meta::field_type"
	// Field borrows one field out of a borrowed struct.
	Field Form = "std::meta::field"
	// Variants lists an enum's tags or a union's variants in declaration
	// order. It is the variant side of PublicFields, and walks the same way.
	Variants Form = "std::meta::variants"
	// VariantName is one variant's source name as a `[]u8`.
	VariantName Form = "std::meta::variant_name"
	// VariantType names one variant's payload type.
	VariantType Form = "std::meta::variant_type"
	// HasPayload reports whether one variant carries a payload. A walk asks
	// before reading a payload type or binding one, because a tag carries no
	// value and an enum tag never does.
	HasPayload Form = "std::meta::has_payload"
	// Variant builds one variant's value: `T::v(payload)`, or `T::v` for a
	// variant that carries none. It is how a walk that means to *produce* a
	// sum value names the arm it produces, since the arm is not a type the
	// caller can write for itself.
	Variant Form = "std::meta::variant"
	// Construct builds a struct from its public fields, taking each field's
	// value from a worker it calls once per field (ADR-0115). It is how a
	// walk that means to *produce* a `T` avoids a place to accumulate one:
	// there is no half-built value, only the arguments of one struct literal.
	Construct Form = "std::meta::construct"
	// Unsupported fails compilation, naming the type that reached it. It is
	// how a walk over a closed set of types refuses the one it has no case
	// for: only the selected `comptime if` branch is checked, so writing this
	// in the last else makes the refusal a compile error rather than output
	// that is silently wrong.
	Unsupported Form = "std::meta::unsupported"
	// TypeName is the spelling of its type argument, as a static `[]u8`. It
	// is what a walk prints or compares when it has to name the type in hand.
	TypeName Form = "std::meta::type_name"
)

// Shape is how one form is written: how many static arguments it takes,
// whether the last of them is a `comptime for` capture, how many runtime
// arguments follow, and whether the form names a type rather than a value.
// A variadic form takes any number of runtime arguments and passes them on
// unchanged, so Args says nothing about it.
type Shape struct {
	StaticArgs int
	Capture    bool
	Args       int
	Type       bool
	Variadic   bool
	// Worker marks the static argument that names a function this form calls.
	// It is 0 when the form calls nothing.
	Worker int
}

var forms = map[Form]Shape{
	IsStruct:              {StaticArgs: 1},
	IsEnum:                {StaticArgs: 1},
	IsUnion:               {StaticArgs: 1},
	IsOptional:            {StaticArgs: 1},
	IsArray:               {StaticArgs: 1},
	IsBox:                 {StaticArgs: 1},
	IsMap:                 {StaticArgs: 1},
	IsOwner:               {StaticArgs: 1},
	IsError:               {StaticArgs: 1},
	ReleaseNamesAllocator: {StaticArgs: 1},
	HasPublicFields:       {StaticArgs: 1},
	Element:               {StaticArgs: 1, Type: true},
	PublicFields:          {StaticArgs: 1},
	FieldName:             {StaticArgs: 2, Capture: true},
	FieldType:             {StaticArgs: 2, Capture: true, Type: true},
	Field:                 {StaticArgs: 2, Capture: true, Args: 1},
	Variants:              {StaticArgs: 1},
	VariantName:           {StaticArgs: 2, Capture: true},
	VariantType:           {StaticArgs: 2, Capture: true, Type: true},
	HasPayload:            {StaticArgs: 2, Capture: true},
	Variant:               {StaticArgs: 2, Capture: true, Variadic: true},
	Construct:             {StaticArgs: 2, Variadic: true, Worker: 2},
	Unsupported:           {StaticArgs: 1},
	TypeName:              {StaticArgs: 1},
}

// Lookup reports the shape of a form, and whether name is one at all.
func Lookup(name string) (Shape, bool) {
	shape, ok := forms[Form(name)]
	return shape, ok
}

// Predicate reports whether a form answers a `comptime if` condition. These
// are the forms a compile-time expression may evaluate beyond the literals and
// operators of SPEC §13.
func Predicate(name string) bool {
	switch Form(name) {
	case IsStruct, IsEnum, IsUnion, IsOptional, IsArray, IsBox, IsMap, IsOwner,
		IsError, ReleaseNamesAllocator, HasPublicFields, HasPayload:
		return true
	default:
		return false
	}
}

// VariantForm reports whether a form reads a variant capture rather than a
// field capture. A field and a variant are answers to different questions
// about different declarations (SPEC §6.7, §6.8), so a form written against
// the wrong capture is an error rather than a silent read of the other one.
func VariantForm(form Form) bool {
	switch form {
	case VariantName, VariantType, HasPayload, Variant:
		return true
	default:
		return false
	}
}

// Names returns every form, so a test can enumerate them.
func Names() []Form {
	out := make([]Form, 0, len(forms))
	for name := range forms {
		out = append(out, name)
	}
	return out
}

// SplitApply reads a written form out of a spelling like
// `std::meta::field_type<T, f>`, returning the form and its static arguments.
// A spelling that is not a form comes back with ok false, which is how a
// caller tells an ordinary type from one that has to be resolved.
func SplitApply(text string) (Form, []string, bool) {
	text = strings.TrimSpace(text)
	open := strings.IndexByte(text, '<')
	if open < 0 || !strings.HasSuffix(text, ">") {
		return "", nil, false
	}
	name := Form(strings.TrimSpace(text[:open]))
	if _, ok := forms[name]; !ok {
		return "", nil, false
	}
	args, err := splitArgs(text[open+1 : len(text)-1])
	if err != nil {
		return "", nil, false
	}
	return name, args, true
}

// ResolveElementTypeForms rewrites every valid, closed element<T> form to the
// type held by T. An invalid container or one that still depends on a type
// parameter is left in place for the validator or later instantiation.
func ResolveElementTypeForms(text string) string {
	parsed, err := typ.Parse(text)
	if err != nil {
		return text
	}
	return resolveElementTypeForms(parsed).String()
}

// resolveElementTypeForms resolves closed element forms recursively through
// wrappers and generic arguments.
func resolveElementTypeForms(node typ.Type) typ.Type {
	switch value := node.(type) {
	case *typ.Name:
		for index, arg := range value.Args {
			value.Args[index] = resolveElementTypeForms(arg)
		}
		if strings.Join(value.Path, "::") != string(Element) || len(value.Args) != 1 {
			return value
		}
		if element, ok := projectedElementType(value.Args[0]); ok {
			return resolveElementTypeForms(element)
		}
		return value
	case *typ.Slice:
		value.Elem = resolveElementTypeForms(value.Elem)
	case *typ.Buffer:
		value.Elem = resolveElementTypeForms(value.Elem)
	case *typ.Borrow:
		value.Elem = resolveElementTypeForms(value.Elem)
	case *typ.Optional:
		value.Elem = resolveElementTypeForms(value.Elem)
	case *typ.Const:
		value.Elem = resolveElementTypeForms(value.Elem)
	case *typ.ErrorUnion:
		if value.Err != nil {
			value.Err = resolveElementTypeForms(value.Err)
		}
		value.Ok = resolveElementTypeForms(value.Ok)
	}
	return node
}

// ElementType returns the child graph held by one valid, closed container.
// The returned node remains owned by the graph that owns container.
func ElementType(container typ.Type) (typ.Type, bool) {
	if optional, ok := container.(*typ.Optional); ok {
		return optional.Elem, true
	}
	name, ok := container.(*typ.Name)
	if !ok {
		return nil, false
	}
	switch strings.Join(name.Path, "::") {
	case "std::array::Array", "std::mem::Box":
		if len(name.Args) == 1 {
			return name.Args[0], true
		}
	case "std::map::Map":
		if len(name.Args) == 2 {
			return name.Args[1], true
		}
	}
	return nil, false
}

// projectedElementType keeps raw declaration projection from erasing a Map
// key error before the Checker validates the original type.
func projectedElementType(container typ.Type) (typ.Type, bool) {
	name, ok := container.(*typ.Name)
	if ok && strings.Join(name.Path, "::") == "std::map::Map" &&
		(len(name.Args) != 2 || !typ.IsMapKey(name.Args[0].String())) {
		return nil, false
	}
	return ElementType(container)
}

// splitArgs splits a static argument list on the commas that sit outside any
// nested `<...>`.
func splitArgs(text string) ([]string, error) {
	parts := []string{}
	depth := 0
	start := 0
	for idx, ch := range text {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
			if depth < 0 {
				return nil, errUnbalanced
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(text[start:idx]))
				start = idx + 1
			}
		}
	}
	if depth != 0 {
		return nil, errUnbalanced
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	for _, part := range parts {
		if part == "" {
			return nil, errUnbalanced
		}
	}
	return parts, nil
}

var errUnbalanced = errors.New("stdmeta: unbalanced static argument list")
