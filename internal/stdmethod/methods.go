package stdmethod

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Method is one std method: a std function whose first parameter is `self`.
//
// std/src/*.kizu writes container methods this way, forwarding to a
// `std::builtin::*` primitive:
//
//	fn append<T>(self: std::array::Array<T>, value: T) -> !void {
//	    return std::builtin::array_append<T>(self, value);
//	}
//
// The declaration carries the method's arity, parameter types, return type and
// type parameters, so a checker can read the signature here instead of
// restating it.
type Method struct {
	Decl       *ast.FunctionDecl
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
		if !ok || !fn.Std || len(fn.Params) == 0 || fn.Params[0].Name != "self" {
			continue
		}
		receiver := baseTypeName(typ.Text(fn.Params[0].TypeName))
		if receiver == "" {
			continue
		}
		methods := index[receiver]
		if methods == nil {
			methods = map[string]Method{}
			index[receiver] = methods
		}
		methods[methodName(fn.Name)] = methodFromDecl(fn)
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
		Decl:       fn,
		TypeParams: fn.TypeParamNames(),
		Params:     params,
		Return:     typ.Text(fn.ReturnType),
	}
}

// baseTypeName strips static arguments, so `std::array::Array<T>` keys as
// `std::array::Array`.
func baseTypeName(typeName string) string {
	if base, _, ok := typ.SplitApply(typeName); ok {
		return base
	}
	return typeName
}

// methodName takes the last segment of a qualified std function name, which is
// how a receiver call spells it.
func methodName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
