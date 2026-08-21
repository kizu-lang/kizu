package types

import (
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Type is the static type name used by the v0 checker.
type Type string

const (
	typeBool       Type = "bool"
	typeField      Type = "Field"
	typeFunction   Type = "Function"
	typeI64        Type = "i64"
	typeU8         Type = "u8"
	typeByteString Type = "[]u8"
	typeType       Type = "type"
	typeVoid       Type = "void"
)

var knownTypes = map[Type]bool{
	typeBool:              true,
	typeI64:               true,
	typeByteString:        true,
	typeVoid:              true,
	"i8":                  true,
	"i16":                 true,
	"i32":                 true,
	typeU8:                true,
	"u16":                 true,
	"u32":                 true,
	"u64":                 true,
	"usize":               true,
	"isize":               true,
	"f32":                 true,
	"f64":                 true,
	typeField:             true,
	typeFunction:          true,
	typeType:              true,
	"Io":                  true,
	"Allocator":           true,
	"std::fs::Metadata":   true,
	"std::fs::DirEntry":   true,
	"std::string::String": true,
}

var numericTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	typeU8:  true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var copyTypes = map[Type]bool{
	typeBool:            true,
	typeI64:             true,
	typeByteString:      true,
	typeVoid:            true,
	"i8":                true,
	"i16":               true,
	"i32":               true,
	typeU8:              true,
	"u16":               true,
	"u32":               true,
	"u64":               true,
	"usize":             true,
	"isize":             true,
	"f32":               true,
	"f64":               true,
	"Io":                true,
	"Allocator":         true,
	"std::fs::Metadata": true,
	"std::fs::DirEntry": true,
}

var signedNumericTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var integerTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"u8":    true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
}

type enumType struct {
	name string
	tags map[string]bool
	// order lists the tags as they were declared, which is what
	// `std::meta::variants` walks. A map answers membership; only the source
	// order keeps what a walk emits from depending on how tags are stored.
	order  []string
	public bool
}

// errorSetType is a declared set of failures. Its members carry nothing, so the
// set is the whole of what a failure says about itself.
type errorSetType struct {
	name    string
	members map[string]bool
	public  bool
	// tagged is the same set seen as something a match runs over. Asking which
	// failure it is, is the question a match on an enum asks, so it is answered
	// by the same code rather than a second copy of it.
	tagged *enumType
}

type unionType struct {
	name       string
	typeParams []string
	variants   map[string]string
	// order lists the variants as they were declared, for the same reason an
	// enum keeps one.
	order  []string
	public bool
}

// A functionType is what a call site sees, plus the body the passes that check
// or instantiate it run over. The two are separate fields rather than one
// declaration so that reading the signature cannot reach the body by accident.
//
// name is not sig.Name: an impl method is registered under the qualified
// `Type.method`, while the signature keeps the name as it was declared.
type functionType struct {
	name            string
	sig             ast.FunctionSignature
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
	returnType      Type
	body            *ast.BlockStmt
	implicitReturn  bool
}

type contractType struct {
	name    string
	methods map[string]*functionType
	public  bool
}

// sortedMethodNames lists a contract's methods in a stable order, so a type that
// misses two of them is always told about the same one first.
func sortedMethodNames(methods map[string]*functionType) []string {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isBufferType reports whether t is a fixed-length stack buffer (`[N]u8`).
func isBufferType(t Type) bool {
	s := string(t)
	return len(s) > 1 && s[0] == '[' && s[1] >= '0' && s[1] <= '9'
}

// containsBufferType reports whether a spelling mentions a stack buffer.
// A stack buffer is local-only in v1 (ADR-0097): it cannot appear in
// signatures, fields, payloads, or container elements.
func containsBufferType(t Type) bool {
	s := string(t)
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '[' && s[i+1] >= '0' && s[i+1] <= '9' {
			return true
		}
	}
	return false
}

// containsBorrowOptional reports whether a spelling mentions `?&T`. A borrow
// optional is positional-only: it appears as an at/at_mut capture condition
// and never crosses a user signature.
func containsBorrowOptional(t Type) bool {
	return strings.Contains(string(t), "?&")
}

// isBorrowedViewReturnType reports whether typ returns a non-owned view.
func isBorrowedViewReturnType(typ Type) bool {
	success := unwrapReturnSuccessType(typ)
	text := string(success)
	return strings.HasPrefix(text, "&") || strings.HasPrefix(text, "[]")
}

// unwrapReturnSuccessType extracts the success payload of !T-like return types.
func unwrapReturnSuccessType(typ Type) Type {
	if elem, ok := errorUnionElement(typ); ok {
		return Type(elem)
	}
	if _, elem, ok := errorUnionParts(typ); ok {
		return Type(elem)
	}
	return typ
}

// borrowWrappedType returns the full spelling for a borrow-bearing field or parameter.
func borrowWrappedType(borrow bool, mutable bool, typ string) string {
	if !borrow {
		return typ
	}
	if mutable {
		return "&var " + typ
	}
	return "&" + typ
}

// typeParamSet returns a lookup for function-level type parameters.
func typeParamSet(params []string) map[string]bool {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]bool, len(params))
	for _, param := range params {
		out[param] = true
	}
	return out
}

// sameType reports exact type equality.
func sameType(left Type, right Type) bool {
	return left == right
}

// substituteTypeParams instantiates a generic type spelling. A parameter is
// replaced where the whole name matches, so `T` leaves `Timer` alone; a
// spelling this checker cannot parse is left as it stands, because rejecting it
// belongs to parseType and its diagnostic.
func substituteTypeParams(declared Type, subst map[string]Type) Type {
	if replacement, ok := subst[string(declared)]; ok {
		return replacement
	}
	parsed, err := typ.Parse(string(declared))
	if err != nil {
		return declared
	}
	return Type(typ.Substitute(parsed, parsedSubst(subst)).String())
}

// parsedSubst parses the replacement types once per substitution.
func parsedSubst(subst map[string]Type) map[string]typ.Type {
	out := make(map[string]typ.Type, len(subst))
	for name, replacement := range subst {
		parsed, err := typ.Parse(string(replacement))
		if err != nil {
			continue
		}
		out[name] = parsed
	}
	return out
}

// argTexts returns the spelling of each static argument.
func argTexts(args []typ.Type) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.String())
	}
	return out
}

// isKnownGenericBase reports whether base names a generic type the compiler
// provides. parseGenericType gives each one its own argument rules; this answers
// the prior question of whether the spelling is a type at all, which is also
// what stops a function from taking it.
func isKnownGenericBase(base string) bool {
	switch base {
	case "std::mem::Box", "std::map::Map", "ptr",
		"std::arena::Arena", "std::arena::Handle", "option", "std::array::Array":
		return true
	default:
		return false
	}
}

// optionalElem returns T for an optional value type `?T` (typ.OptionalElem
// with this package's Type spelling).
func optionalElem(t Type) (Type, bool) {
	elem, ok := typ.OptionalElem(string(t))
	return Type(elem), ok
}

// absorbsErrorUnion reports whether returning a result that fails one way from a
// function that declares no error set is the same absorption `try` does. A
// declared `E!T` is not this, because it named the one set it accepts.
func absorbsErrorUnion(want Type, got Type) bool {
	return typ.ParseAbsorbsErrorSet(string(want), string(got))
}

// explicitBorrowType extracts &T and &var T spellings.
func explicitBorrowType(typ Type) (string, bool, Type, bool) {
	text := string(typ)
	if !strings.HasPrefix(text, "&") {
		return "", false, "", false
	}
	rest := strings.TrimPrefix(text, "&")
	mutable := false
	if strings.HasPrefix(rest, "var ") {
		mutable = true
		rest = strings.TrimPrefix(rest, "var ")
	}
	if rest == "" {
		return "", false, "", false
	}
	return "", mutable, Type(rest), true
}

// joinTypes renders static arguments for an instantiation key.
func joinTypes(types []Type) string {
	parts := make([]string, 0, len(types))
	for _, typ := range types {
		parts = append(parts, string(typ))
	}
	return strings.Join(parts, ", ")
}

// pointerElement extracts the element type from ptr<T> or ?ptr<T>.
func pointerElement(typ Type) (string, bool) {
	name := strings.TrimPrefix(string(typ), "?")
	base, arg, ok := splitGenericType(name)
	if !ok || base != "ptr" {
		return "", false
	}
	return arg, true
}

// rawPointerDerefType returns the value type read by raw pointer dereference.
func rawPointerDerefType(typ Type) (Type, error) {
	elem, ok := pointerElement(typ)
	if !ok {
		return "", errorf("type error: `%s` is not a raw pointer", typ)
	}
	if strings.HasPrefix(string(typ), "?") {
		return "", errorf("type error: nullable raw pointer `%s` cannot be dereferenced", typ)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// assignableRawPointerDerefType returns the value type written through a raw pointer.
func assignableRawPointerDerefType(typ Type) (Type, error) {
	elem, ok := pointerElement(typ)
	if !ok {
		return "", errorf("type error: `%s` is not a raw pointer", typ)
	}
	if strings.HasPrefix(string(typ), "?") {
		return "", errorf("type error: nullable raw pointer `%s` cannot be dereferenced", typ)
	}
	if strings.HasPrefix(elem, "const ") {
		return "", errorf("type error: cannot assign through const raw pointer `%s`", typ)
	}
	return Type(elem), nil
}

// isPointerType reports whether typ is ptr<T> or ?ptr<T>.
func isPointerType(typ Type) bool {
	_, ok := pointerElement(typ)
	return ok
}

// containsRawPointer reports whether a type spelling mentions ptr<T> anywhere,
// including behind `?`, `[]`, and static type arguments.
func containsRawPointer(typ Type) bool {
	return containsWrappedType(typ, isPointerType)
}

// containsTypeValue reports whether a type spelling contains comptime-only type.
func containsTypeValue(typ Type) bool {
	return containsWrappedType(typ, func(typ Type) bool {
		return typ == typeType
	})
}

// containsWrappedType recursively checks prefixes and static type arguments.
func containsWrappedType(typ Type, match func(Type) bool) bool {
	text := string(typ)
	for {
		switch {
		case strings.HasPrefix(text, "!"):
			text = strings.TrimPrefix(text, "!")
		case strings.HasPrefix(text, "&var "):
			text = strings.TrimPrefix(text, "&var ")
		case strings.HasPrefix(text, "&"):
			text = strings.TrimPrefix(text, "&")
		case strings.HasPrefix(text, "?"):
			text = strings.TrimPrefix(text, "?")
		case strings.HasPrefix(text, "[]"):
			text = strings.TrimPrefix(text, "[]")
		case strings.HasPrefix(text, "const "):
			text = strings.TrimPrefix(text, "const ")
		default:
			if match(Type(text)) {
				return true
			}
			base, arg, ok := splitGenericType(text)
			if !ok {
				return false
			}
			if match(Type(base)) {
				return true
			}
			args, ok := splitGenericArgs(arg)
			if !ok {
				return false
			}
			for _, item := range args {
				if containsWrappedType(Type(item), match) {
					return true
				}
			}
			return false
		}
	}
}

// methodMatches checks a method against the contract method it stands for. The
// contract writes no receiver, so the comparison starts after the method's own.
func methodMatches(want *functionType, got *functionType) bool {
	if len(got.params) == 0 {
		return false
	}
	gotParams := got.params[1:]
	if len(want.params) != len(gotParams) || !sameType(want.returnType, got.returnType) {
		return false
	}
	for idx, wantParam := range want.params {
		if !sameType(wantParam, gotParams[idx]) ||
			want.borrowParams[idx] != got.borrowParams[idx+1] ||
			want.mutBorrowParams[idx] != got.mutBorrowParams[idx+1] {
			return false
		}
	}
	return true
}

// errorUnionElement extracts T from legacy !T.
func errorUnionElement(typ Type) (string, bool) {
	_, success, ok := errorUnionParts(typ)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(union Type) (string, string, bool) {
	return typ.ParseErrorUnionParts(string(union))
}

// splitGenericType extracts base and raw arguments from base<args>.
func splitGenericType(name string) (string, string, bool) {
	return typ.SplitApply(name)
}

// splitGenericArgs extracts top-level comma-separated static arguments.
func splitGenericArgs(arg string) ([]string, bool) {
	args, err := typ.SplitArgs(arg)
	if err != nil {
		return nil, false
	}
	return args, true
}

// singleGenericArg returns the only argument for one-parameter generic types.
func singleGenericArg(base string, args []string) (string, error) {
	if len(args) != 1 {
		return "", errorf("type error: `%s` expects 1 static argument", base)
	}
	return args[0], nil
}
