// Package stdmeta names the `std::meta` forms and the shape each one is
// spelled with.
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
)

// Form is one `std::meta` spelling.
type Form string

// The forms. A `Type` form is written where a type goes; the rest are written
// where an expression goes.
const (
	// IsStruct reports whether its type argument is a declared struct.
	IsStruct Form = "std::meta::is_struct"
	// IsOptional reports whether its type argument is `?T`.
	IsOptional Form = "std::meta::is_optional"
	// IsArray reports whether its type argument is `std::array::Array<T>`.
	IsArray Form = "std::meta::is_array"
	// IsBox reports whether its type argument is `std::mem::Box<T>`.
	IsBox Form = "std::meta::is_box"
	// IsMap reports whether its type argument is `std::map::Map<K, V>`.
	IsMap Form = "std::meta::is_map"
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
	IsStruct:        {StaticArgs: 1},
	IsOptional:      {StaticArgs: 1},
	IsArray:         {StaticArgs: 1},
	IsBox:           {StaticArgs: 1},
	IsMap:           {StaticArgs: 1},
	HasPublicFields: {StaticArgs: 1},
	Element:         {StaticArgs: 1, Type: true},
	PublicFields:    {StaticArgs: 1},
	FieldName:       {StaticArgs: 2, Capture: true},
	FieldType:       {StaticArgs: 2, Capture: true, Type: true},
	Field:           {StaticArgs: 2, Capture: true, Args: 1},
	Construct:       {StaticArgs: 2, Variadic: true, Worker: 2},
	Unsupported:     {StaticArgs: 1},
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
	case IsStruct, IsOptional, IsArray, IsBox, IsMap, HasPublicFields:
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
