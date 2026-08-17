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
	"strconv"
	"strings"
)

// Type is one parsed type.
type Type interface {
	// String returns the spelling this type parsed from.
	String() string
	// equal reports whether other has the same structure.
	equal(other Type) bool
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

// Buffer is `[N]T`, a fixed-length stack buffer (ADR-0097).
type Buffer struct {
	Size int64
	Elem Type
}

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

// typeNode marks Buffer as a type.
func (*Buffer) typeNode() {}

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

// String returns the spelling of a fixed-length buffer type.
func (t *Buffer) String() string {
	return "[" + strconv.FormatInt(t.Size, 10) + "]" + t.Elem.String()
}

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

// Parse is the inverse of String. It reads a canonical spelling -- one this
// package printed, or one the compiler itself writes in Go -- back into the
// type it names.
//
// It is not the reader for source syntax: internal/parser reads what a source
// file writes, and a type it read reaches here already parsed. The two agree on
// every type the corpus contains, which TestTypeSpellingRoundTrip checks.
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

// Equal reports whether two types have the same structure. Two types are the
// same type when they are built the same way, not when they happen to be the
// same value in memory.
func Equal(left Type, right Type) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.equal(right)
}

// equal reports whether other is the same name with the same arguments.
func (t *Name) equal(other Type) bool {
	b, ok := other.(*Name)
	if !ok || len(t.Path) != len(b.Path) || len(t.Args) != len(b.Args) {
		return false
	}
	for i := range t.Path {
		if t.Path[i] != b.Path[i] {
			return false
		}
	}
	for i := range t.Args {
		if !Equal(t.Args[i], b.Args[i]) {
			return false
		}
	}
	return true
}

// equal reports whether other is a slice of the same element.
func (t *Slice) equal(other Type) bool {
	b, ok := other.(*Slice)
	return ok && Equal(t.Elem, b.Elem)
}

// equal reports whether other is a buffer of the same size and element.
func (t *Buffer) equal(other Type) bool {
	b, ok := other.(*Buffer)
	return ok && t.Size == b.Size && Equal(t.Elem, b.Elem)
}

// equal reports whether other is the same borrow of the same element.
func (t *Borrow) equal(other Type) bool {
	b, ok := other.(*Borrow)
	return ok && t.Mut == b.Mut && Equal(t.Elem, b.Elem)
}

// equal reports whether other is a nullable of the same element.
func (t *Optional) equal(other Type) bool {
	b, ok := other.(*Optional)
	return ok && Equal(t.Elem, b.Elem)
}

// equal reports whether other is a dyn of the same contract.
func (t *Dyn) equal(other Type) bool {
	b, ok := other.(*Dyn)
	return ok && Equal(t.Contract, b.Contract)
}

// equal reports whether other is a const of the same element.
func (t *Const) equal(other Type) bool {
	b, ok := other.(*Const)
	return ok && Equal(t.Elem, b.Elem)
}

// equal reports whether other is the same error union.
func (t *ErrorUnion) equal(other Type) bool {
	b, ok := other.(*ErrorUnion)
	return ok && Equal(t.Err, b.Err) && Equal(t.Ok, b.Ok)
}

// Text returns the spelling of t, and "" where a declaration wrote no type at
// all. A missing type is not a type, so it has no node to print.
func Text(t Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}

// MapNames returns t with every name rewritten by rename, keeping the structure
// around it. Qualifying or resolving a type is a question about the names it
// mentions, so a caller says what a name becomes rather than walking a spelling
// looking for one.
func MapNames(t Type, rename func(path []string) ([]string, error)) (Type, error) {
	switch node := t.(type) {
	case nil:
		return nil, nil
	case *Name:
		return mapNameNode(node, rename)
	case *Slice:
		elem, err := MapNames(node.Elem, rename)
		return &Slice{Elem: elem}, err
	case *Buffer:
		elem, err := MapNames(node.Elem, rename)
		return &Buffer{Size: node.Size, Elem: elem}, err
	case *Borrow:
		elem, err := MapNames(node.Elem, rename)
		return &Borrow{Elem: elem, Mut: node.Mut}, err
	case *Optional:
		elem, err := MapNames(node.Elem, rename)
		return &Optional{Elem: elem}, err
	case *Dyn:
		contract, err := MapNames(node.Contract, rename)
		return &Dyn{Contract: contract}, err
	case *Const:
		elem, err := MapNames(node.Elem, rename)
		return &Const{Elem: elem}, err
	case *ErrorUnion:
		ok, err := MapNames(node.Ok, rename)
		if err != nil {
			return nil, err
		}
		out := &ErrorUnion{Ok: ok}
		if node.Err != nil {
			out.Err, err = MapNames(node.Err, rename)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return t, nil
	}
}

// mapNameNode rewrites a name node and its static arguments.
func mapNameNode(node *Name, rename func(path []string) ([]string, error)) (Type, error) {
	path, err := rename(node.Path)
	if err != nil {
		return nil, err
	}
	args := make([]Type, 0, len(node.Args))
	for _, arg := range node.Args {
		mapped, err := MapNames(arg, rename)
		if err != nil {
			return nil, err
		}
		args = append(args, mapped)
	}
	return &Name{Path: path, Args: args}, nil
}

// Walk calls visit for t and for every type inside it, outermost first. A
// caller that needs the types a spelling mentions -- not just the one it names
// -- asks here rather than searching the text for a punctuation mark.
func Walk(t Type, visit func(Type)) {
	visit(t)
	switch node := t.(type) {
	case *Name:
		for _, arg := range node.Args {
			Walk(arg, visit)
		}
	case *Slice:
		Walk(node.Elem, visit)
	case *Borrow:
		Walk(node.Elem, visit)
	case *Optional:
		Walk(node.Elem, visit)
	case *Dyn:
		Walk(node.Contract, visit)
	case *Const:
		Walk(node.Elem, visit)
	case *ErrorUnion:
		if node.Err != nil {
			Walk(node.Err, visit)
		}
		Walk(node.Ok, visit)
	}
}

// ErrorUnionParts returns the error type and T of an error union, and reports
// whether text is one. `!T` gives an empty error type. The answer comes from
// the structure, so the `!` inside `Array<!i64>` does not make that type an
// error union.
func ErrorUnionParts(text string) (string, string, bool) {
	parsed, err := Parse(text)
	if err != nil {
		return "", "", false
	}
	node, ok := parsed.(*ErrorUnion)
	if !ok {
		return "", "", false
	}
	errorType := ""
	if node.Err != nil {
		errorType = node.Err.String()
	}
	return errorType, node.Ok.String(), true
}

// OptionalElem returns T for an optional value type `?T`, and reports whether
// text is one. A `?ptr<...>` spelling keeps its raw-pointer C-ABI meaning and
// is not an optional value type. This is the one definition of that carve-out;
// every checker and backend asks here so the answer cannot drift.
func OptionalElem(text string) (string, bool) {
	if !strings.HasPrefix(text, "?") || strings.HasPrefix(text, "?ptr<") {
		return "", false
	}
	return text[1:], true
}

// AbsorbsErrorSet reports whether a value of type got fills a slot declared
// want by the absorption `try` does. `!T` declares no error set (ADR-0087), so
// an `E!T` reaching it arrives with E absorbed. A declared `E!T` named the one
// set it takes and absorbs nothing.
func AbsorbsErrorSet(want string, got string) bool {
	wantSet, wantSuccess, isUnion := ErrorUnionParts(want)
	if !isUnion || wantSet != "" {
		return false
	}
	gotSet, gotSuccess, isUnion := ErrorUnionParts(got)
	return isUnion && gotSet != "" && gotSuccess == wantSuccess
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

// SplitMethodName separates a receiver-qualified method name -- the
// `Receiver.method` pairing stdmethod.MethodName writes -- into its halves.
// Receiver spellings never contain `.`, so the last dot is the seam.
func SplitMethodName(name string) (string, string, bool) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", "", false
	}
	return name[:idx], name[idx+1:], true
}

// CleanupMethod reports whether a method name discharges an owner's consume
// obligation: `deinit` for plain owners, `deinit_all` for owner-element
// containers (ADR-0091). Which of the two a given receiver accepts is the type
// checker's rule; this is only the shared spelling of the pair.
func CleanupMethod(name string) bool {
	return name == "deinit" || name == "deinit_all"
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
