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
	"math"
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

// Const is `const T`, which only a static argument list writes.
type Const struct{ Elem Type }

// ErrorUnion is `!T`, or `E!T` when Err is set.
// Func is a function pointer type: `fn(i64) -> i64`, or `unsafe fn(...) -> T`
// when the function it points at carries an obligation (SPEC §12). The two are
// different types, so an unsafe function cannot reach a safe call.
type Func struct {
	Params []Type
	Result Type
	Unsafe bool
}

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

// typeNode marks Const as a type.
func (*Const) typeNode() {}

// typeNode marks ErrorUnion as a type.
func (*ErrorUnion) typeNode() {}

// typeNode marks Func as a type.
func (*Func) typeNode() {}

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

// String returns the spelling of a const type argument.
func (t *Const) String() string { return "const " + t.Elem.String() }

// String returns the spelling of an error union.
func (t *ErrorUnion) String() string {
	if t.Err == nil {
		return "!" + t.Ok.String()
	}
	return t.Err.String() + "!" + t.Ok.String()
}

// String returns the spelling of a function pointer type.
func (t *Func) String() string {
	params := make([]string, 0, len(t.Params))
	for _, param := range t.Params {
		params = append(params, param.String())
	}
	head := "fn("
	if t.Unsafe {
		head = "unsafe fn("
	}
	return head + strings.Join(params, ", ") + ") -> " + t.Result.String()
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

// equal reports whether other is a function pointer with the same shape.
func (t *Func) equal(other Type) bool {
	b, ok := other.(*Func)
	if !ok || t.Unsafe != b.Unsafe || len(t.Params) != len(b.Params) {
		return false
	}
	for i := range t.Params {
		if !Equal(t.Params[i], b.Params[i]) {
			return false
		}
	}
	return Equal(t.Result, b.Result)
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
	case *Const:
		elem, err := MapNames(node.Elem, rename)
		return &Const{Elem: elem}, err
	case *Func:
		return mapFuncNode(node, rename)
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

// mapFuncNode rewrites the parameter and result types of a function pointer.
func mapFuncNode(node *Func, rename func(path []string) ([]string, error)) (Type, error) {
	out := &Func{Params: make([]Type, 0, len(node.Params)), Unsafe: node.Unsafe}
	for _, param := range node.Params {
		mapped, err := MapNames(param, rename)
		if err != nil {
			return nil, err
		}
		out.Params = append(out.Params, mapped)
	}
	result, err := MapNames(node.Result, rename)
	if err != nil {
		return nil, err
	}
	out.Result = result
	return out, nil
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
	case *Buffer:
		Walk(node.Elem, visit)
	case *Borrow:
		Walk(node.Elem, visit)
	case *Optional:
		Walk(node.Elem, visit)
	case *Const:
		Walk(node.Elem, visit)
	case *Func:
		for _, param := range node.Params {
			Walk(param, visit)
		}
		Walk(node.Result, visit)
	case *ErrorUnion:
		if node.Err != nil {
			Walk(node.Err, visit)
		}
		Walk(node.Ok, visit)
	}
}

// ErrorUnionParts returns the error type and T of an error union, and reports
// whether value is one. `!T` gives a nil error type. The caller parses text at
// its boundary; structural questions do not parse or allocate.
func ErrorUnionParts(value Type) (Type, Type, bool) {
	node, ok := value.(*ErrorUnion)
	if !ok {
		return nil, nil, false
	}
	return node.Err, node.Ok, true
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

// BorrowOptionalElem splits a borrow optional `?&T` / `?&var T` into its
// payload type and mutability, and reports whether text is one. This is the
// one definition of that split; both checkers ask here so the answer cannot
// drift.
func BorrowOptionalElem(text string) (string, bool, bool) {
	if elem, found := strings.CutPrefix(text, "?&var "); found {
		return elem, true, true
	}
	if elem, found := strings.CutPrefix(text, "?&"); found {
		return elem, false, true
	}
	return "", false, false
}

// AbsorbsErrorSet reports whether a value of type got fills a slot declared
// want by the absorption `try` does. `!T` declares no error set (ADR-0087), so
// an `E!T` reaching it arrives with E absorbed. A declared `E!T` named the one
// set it takes and absorbs nothing.
func AbsorbsErrorSet(want Type, got Type) bool {
	wantSet, wantSuccess, isUnion := ErrorUnionParts(want)
	if !isUnion || wantSet != nil {
		return false
	}
	gotSet, gotSuccess, isUnion := ErrorUnionParts(got)
	return isUnion && gotSet != nil && Equal(gotSuccess, wantSuccess)
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

// CleanupMethod is the method name that discharges an owner's consume
// obligation (ADR-0091). There is one: `deinit` releases the value and whatever
// it holds, so a container needs no second name for the case where its elements
// own something (ADR-0119). This is the one spelling; every layer reads it here.
const CleanupMethod = "deinit"

// substituteName instantiates a name: the whole name when it is a parameter,
// and its static arguments otherwise.
func substituteName(node *Name, subst map[string]Type) Type {
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
}

// substituteFunc instantiates the parameter and result types of a function
// pointer.
func substituteFunc(node *Func, subst map[string]Type) Type {
	out := &Func{Result: Substitute(node.Result, subst), Unsafe: node.Unsafe}
	for _, param := range node.Params {
		out.Params = append(out.Params, Substitute(param, subst))
	}
	return out
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
		return substituteName(node, subst)
	case *Slice:
		return &Slice{Elem: Substitute(node.Elem, subst)}
	case *Buffer:
		return &Buffer{Size: node.Size, Elem: Substitute(node.Elem, subst)}
	case *Borrow:
		return &Borrow{Elem: Substitute(node.Elem, subst), Mut: node.Mut}
	case *Optional:
		return &Optional{Elem: Substitute(node.Elem, subst)}
	case *Const:
		return &Const{Elem: Substitute(node.Elem, subst)}
	case *Func:
		return substituteFunc(node, subst)
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

// mapKeyTypes lists what std::map::Map accepts as a key besides `[]u8`. A key
// is hashed and compared as the bytes it occupies, so a type qualifies when
// its bytes are its value: no padding to read, no pointer to follow, and no
// two byte patterns that have to compare equal. The integer types answer that;
// f32/f64 do not, because `0.0` and `-0.0` are equal with different bytes and
// a NaN is unequal to its own. How wide each one is stays with the backend
// that lays it out, so there is one answer to that and not two.
var mapKeyTypes = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true,
	"u8": true, "u16": true, "u32": true, "u64": true,
	"isize": true, "usize": true,
}

// IsMapKey reports whether text spells a type std::map::Map accepts as a key.
// Every layer that gates a key asks here, so the checkers, the lowering and
// the backend cannot disagree about what a key is.
func IsMapKey(text string) bool {
	return text == "[]u8" || mapKeyTypes[text]
}

// MapKeyTypeNames lists the accepted key spellings for a diagnostic, in the
// order a reader scans them.
func MapKeyTypeNames() string {
	return "[]u8, i8, i16, i32, i64, u8, u16, u32, u64, isize, usize"
}

// ShiftInt64 shifts an i64 by a non-negative amount with the width rule of
// SPEC §6.9.2: an amount at or past 64 leaves 0 from `<<` and the sign from
// `>>`. Compile-time evaluation and IR constant folding both compute from
// this one definition; a backend computes the same thing at run time.
func ShiftInt64(op string, value int64, amount int64) int64 {
	if amount >= 64 {
		if op == "<<" {
			return 0
		}
		amount = 63
	}
	if op == "<<" {
		return value << uint(amount)
	}
	return value >> uint(amount)
}

// CleanFloatLiteral returns the spelling of a floating-point literal with
// its `_` separators removed and the exponent marker lowercased, the one
// form the rest of the compiler reads.
func CleanFloatLiteral(text string) string {
	return strings.ToLower(strings.ReplaceAll(text, "_", ""))
}

// ParseFloatLiteral converts a cleaned floating-point literal to the f64 it
// names, rounding to the nearest value with ties to even, and reports false
// for a spelling that is not a number or whose magnitude no f64 holds. The
// selfhost compiler reads the same literals through `std::float::parse`.
func ParseFloatLiteral(text string) (float64, bool) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}
