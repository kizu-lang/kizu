// Package typ is the parsed form of a Kizu type spelling.
//
// The compiler stores types as the text a source file writes, which makes every
// question about a type a question about its spelling. Asking those questions
// with string surgery is how `[]T` substitution reached into `[]u8` and produced
// `[]i648`. Parse once here, ask the question of the structure, and print the
// answer back.
package typ

import (
	"fmt"
	"strings"
)

// Type is one parsed type.
type Type interface {
	// String returns the spelling this type parsed from.
	String() string
	typeNode()
}

// Name is a primitive, a declared type, a type parameter, or a generic
// application: `i64`, `Point`, `T`, `std::map::Map<[]u8, i64>`.
type Name struct {
	// Path is the `::` separated name, already split.
	Path []string
	// Args is the `<...>` list, empty when the name carries none.
	Args []Type
}

// Slice is `[]T`.
type Slice struct{ Elem Type }

// Borrow is `&T` or, when Mut is set, `&var T`.
type Borrow struct {
	Elem Type
	Mut  bool
}

// Optional is `?T`.
type Optional struct{ Elem Type }

// Dyn is `dyn Contract`.
type Dyn struct{ Contract Type }

// Const is `const T`, which only a static argument list writes.
type Const struct{ Elem Type }

// ErrorUnion is `!T`, or `E!T` when Err is set.
type ErrorUnion struct {
	Err Type
	Ok  Type
}

// typeNode marks Name as a type.
func (*Name) typeNode() {}

// typeNode marks Slice as a type.
func (*Slice) typeNode() {}

// typeNode marks Borrow as a type.
func (*Borrow) typeNode() {}

// typeNode marks Optional as a type.
func (*Optional) typeNode() {}

// typeNode marks Dyn as a type.
func (*Dyn) typeNode() {}

// typeNode marks Const as a type.
func (*Const) typeNode() {}

// typeNode marks ErrorUnion as a type.
func (*ErrorUnion) typeNode() {}

// String returns the spelling of a name and its static arguments.
func (t *Name) String() string {
	name := strings.Join(t.Path, "::")
	if len(t.Args) == 0 {
		return name
	}
	args := make([]string, 0, len(t.Args))
	for _, arg := range t.Args {
		args = append(args, arg.String())
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

// String returns the spelling of a slice type.
func (t *Slice) String() string { return "[]" + t.Elem.String() }

// String returns the spelling of a borrow type.
func (t *Borrow) String() string {
	if t.Mut {
		return "&var " + t.Elem.String()
	}
	return "&" + t.Elem.String()
}

// String returns the spelling of a nullable type.
func (t *Optional) String() string { return "?" + t.Elem.String() }

// String returns the spelling of a dyn contract type.
func (t *Dyn) String() string { return "dyn " + t.Contract.String() }

// String returns the spelling of a const type argument.
func (t *Const) String() string { return "const " + t.Elem.String() }

// String returns the spelling of an error union.
func (t *ErrorUnion) String() string {
	if t.Err == nil {
		return "!" + t.Ok.String()
	}
	return t.Err.String() + "!" + t.Ok.String()
}

// Parse reads one type spelling. It rejects text a Kizu type cannot be, so a
// caller that ignores the error would be asking about a type that does not
// exist.
func Parse(text string) (Type, error) {
	p := &parser{input: text}
	parsed, err := p.parseType()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.input) {
		return nil, fmt.Errorf("type error: trailing text in `%s`", text)
	}
	return parsed, nil
}

// ParseArgs reads a static argument list such as `[]u8, i64`, keeping a nested
// spelling in one piece. It is the one place that knows a `,` inside `<...>`
// does not separate arguments.
func ParseArgs(text string) ([]Type, error) {
	parts, err := SplitArgs(text)
	if err != nil {
		return nil, err
	}
	args := make([]Type, 0, len(parts))
	for _, part := range parts {
		parsed, err := Parse(part)
		if err != nil {
			return nil, err
		}
		args = append(args, parsed)
	}
	return args, nil
}

// SplitArgs splits a static argument list on the commas that separate its
// entries. It exists for the lists that still hold values rather than types;
// once a static argument list is structured, only ParseArgs remains.
func SplitArgs(text string) ([]string, error) {
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
				return nil, fmt.Errorf("type error: unbalanced `>` in `%s`", text)
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(text[start:idx]))
				start = idx + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("type error: unbalanced `<` in `%s`", text)
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("type error: empty static argument in `%s`", text)
		}
	}
	return parts, nil
}

// SplitApply separates `Base` and `Args` in a `Base<Args>` spelling, without
// looking inside either. It is the one place that knows the closing `>` of a
// name is its last byte.
func SplitApply(name string) (string, string, bool) {
	open := strings.IndexByte(name, '<')
	if open < 1 || !strings.HasSuffix(name, ">") {
		return "", "", false
	}
	args := name[open+1 : len(name)-1]
	if args == "" {
		return "", "", false
	}
	return name[:open], args, true
}

// Substitute replaces every type parameter named in subst, wherever it appears
// in the structure. A name is replaced only when the whole name matches, so a
// parameter `T` leaves `Timer` alone.
func Substitute(t Type, subst map[string]Type) Type {
	if len(subst) == 0 {
		return t
	}
	switch node := t.(type) {
	case *Name:
		if len(node.Path) == 1 && len(node.Args) == 0 {
			if replacement, ok := subst[node.Path[0]]; ok {
				return replacement
			}
		}
		if len(node.Args) == 0 {
			return node
		}
		args := make([]Type, 0, len(node.Args))
		for _, arg := range node.Args {
			args = append(args, Substitute(arg, subst))
		}
		return &Name{Path: node.Path, Args: args}
	case *Slice:
		return &Slice{Elem: Substitute(node.Elem, subst)}
	case *Borrow:
		return &Borrow{Elem: Substitute(node.Elem, subst), Mut: node.Mut}
	case *Optional:
		return &Optional{Elem: Substitute(node.Elem, subst)}
	case *Dyn:
		return &Dyn{Contract: Substitute(node.Contract, subst)}
	case *Const:
		return &Const{Elem: Substitute(node.Elem, subst)}
	case *ErrorUnion:
		out := &ErrorUnion{Ok: Substitute(node.Ok, subst)}
		if node.Err != nil {
			out.Err = Substitute(node.Err, subst)
		}
		return out
	default:
		return t
	}
}

// SubstituteText replaces type parameters in a spelling and returns the
// spelling back, for callers that still hold types as text.
func SubstituteText(text string, subst map[string]string) (string, error) {
	if len(subst) == 0 {
		return text, nil
	}
	parsed, err := Parse(text)
	if err != nil {
		return "", err
	}
	replacements := make(map[string]Type, len(subst))
	for name, replacement := range subst {
		value, err := Parse(replacement)
		if err != nil {
			return "", err
		}
		replacements[name] = value
	}
	return Substitute(parsed, replacements).String(), nil
}
