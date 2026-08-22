package stdmethod

import (
	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Method is one method declared with a receiver slot.
//
// std/src/*/*.kizu writes container methods this way, forwarding to a
// `std::internal::builtin::*` primitive:
//
//	fn (self: std::array::Array<T>) append<T>(value: T) -> !void {
//	    return std::internal::builtin::array_append<T>(self, value);
//	}
//
// The signature carries the method's arity, parameter types, return type and
// type parameters, so a checker can read them here instead of restating them.
// It is the signature rather than the declaration because this index is what
// decides what a `value.method(...)` call site sees, and that must not be able
// to come from a body. The rendered fields below are the same facts spelled as
// text, kept so a call check does not re-render them.
type Method struct {
	Sig        ast.FunctionSignature
	TypeParams []string
	Params     []ParamType
	Return     string
}

// ParamType is one declared parameter, after the receiver.
type ParamType struct {
	TypeName  string
	Borrow    bool
	MutBorrow bool
}

// Substitute returns typeName with each declared type parameter replaced by the
// matching concrete argument.
func (m Method) Substitute(typeName string, typeArgs []string) string {
	if len(m.TypeParams) == 0 || len(typeArgs) != len(m.TypeParams) {
		return typeName
	}
	subst := make(map[string]string, len(m.TypeParams))
	for idx, param := range m.TypeParams {
		subst[param] = typeArgs[idx]
	}
	out, err := typ.SubstituteText(typeName, subst)
	if err != nil {
		// std declares this type, so it parses; a spelling that does not is a
		// std source error the type checker reports against the declaration.
		return typeName
	}
	return out
}

// MethodIndex maps a receiver's base type name to its std methods by name.
type MethodIndex map[string]map[string]Method

// IndexMethods collects the std method declarations reachable in decls.
func IndexMethods(decls []ast.Decl) MethodIndex {
	index := MethodIndex{}
	for _, decl := range decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || !fn.Receiver || len(fn.Params) == 0 {
			continue
		}
		receiver, name, ok := SplitMethodName(fn.Name)
		if !ok {
			continue
		}
		methods := index[receiver]
		if methods == nil {
			methods = map[string]Method{}
			index[receiver] = methods
		}
		methods[name] = methodFromDecl(fn)
	}
	return index
}

// methodFromDecl reads one declaration's signature.
func methodFromDecl(fn *ast.FunctionDecl) Method {
	params := make([]ParamType, 0, len(fn.Params)-1)
	for _, param := range fn.Params[1:] {
		params = append(params, ParamType{
			TypeName:  typ.Text(param.TypeName),
			Borrow:    param.Borrow,
			MutBorrow: param.MutBorrow,
		})
	}
	return Method{
		Sig:        fn.FunctionSignature,
		TypeParams: fn.TypeParamNames(),
		Params:     params,
		Return:     typ.Text(fn.ReturnType),
	}
}

// MethodName is what a method is filed under: the type it is a method on and the
// name a call spells. This and SplitMethodName are the one place that pairing is
// written down.
func MethodName(receiver string, name string) string {
	return receiver + "." + name
}

// SplitMethodName separates a method's name into the type it is a method on and
// the name a call spells.
func SplitMethodName(name string) (string, string, bool) {
	return typ.SplitMethodName(name)
}

// CallName returns the name a call spells for a declared function. A method is
// filed under its receiver's type, and a call names only the part after it.
func CallName(name string) string {
	if _, called, ok := SplitMethodName(name); ok {
		return called
	}
	return name
}
