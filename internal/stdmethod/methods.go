package stdmethod

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
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
	out := typeName
	for idx, param := range m.TypeParams {
		out = substituteTypeParam(out, param, typeArgs[idx])
	}
	return out
}

// substituteTypeParam replaces one type parameter where it stands as a whole
// type name, so `T` in `!T`, `[]T`, `&T` and `Array<T>` resolves but `Text`
// does not.
func substituteTypeParam(typeName string, param string, arg string) string {
	var out strings.Builder
	for i := 0; i < len(typeName); {
		if !strings.HasPrefix(typeName[i:], param) ||
			isTypeNameChar(byteAt(typeName, i-1)) ||
			isTypeNameChar(byteAt(typeName, i+len(param))) {
			out.WriteByte(typeName[i])
			i++
			continue
		}
		out.WriteString(arg)
		i += len(param)
	}
	return out.String()
}

// byteAt returns the byte at index, or 0 when the index is out of range.
func byteAt(text string, index int) byte {
	if index < 0 || index >= len(text) {
		return 0
	}
	return text[index]
}

// isTypeNameChar reports whether b can appear inside a type name.
func isTypeNameChar(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
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
		receiver := baseTypeName(fn.Params[0].TypeName)
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
			TypeName:  param.TypeName,
			Borrow:    param.Borrow,
			MutBorrow: param.MutBorrow,
		})
	}
	return Method{
		Decl:       fn,
		TypeParams: fn.TypeParams,
		Params:     params,
		Return:     fn.ReturnType,
	}
}

// baseTypeName strips static arguments, so `std::array::Array<T>` keys as
// `std::array::Array`.
func baseTypeName(typeName string) string {
	if idx := strings.Index(typeName, "<"); idx >= 0 {
		return typeName[:idx]
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
