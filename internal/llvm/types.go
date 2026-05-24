package llvm

import "strings"

// localName turns Kizu SSA names into valid named LLVM local identifiers.
func localName(name string) string {
	if strings.HasPrefix(name, "%") {
		return "%kizu." + strings.TrimPrefix(name, "%")
	}
	return name
}

// llvmPrimitiveType maps scalar and runtime-view Kizu IR types to LLVM IR types.
func llvmPrimitiveType(typ string) string {
	switch typ {
	case "void":
		return "void"
	case "bool":
		return "i1"
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return integerLLVMType(typ)
	case "[]u8":
		return "%kizu.slice.u8"
	default:
		if isArenaHandleType(typ) {
			return "i64"
		}
		return "ptr"
	}
}

// llvmType maps Kizu IR types to LLVM IR types.
func (e *emitter) llvmType(typ string) string {
	if _, ok := errorUnionSuccessType(typ); ok {
		return llvmErrorUnionTypeName(typ)
	}
	if strings.HasPrefix(typ, "std::arena::Handle<") {
		return "i64"
	}
	if strings.HasPrefix(typ, "std::arena::Arena<") {
		return "ptr"
	}
	if _, ok := e.module.Structs[typ]; ok {
		return llvmStructTypeName(typ)
	}
	if _, ok := e.module.Enums[typ]; ok {
		return "i64"
	}
	if _, ok := e.module.Unions[typ]; ok {
		return llvmUnionTypeName(typ)
	}
	return llvmPrimitiveType(typ)
}

// usesIndirectStructParamABI reports whether module-local functions pass a
// value through an explicit byval pointer instead of target aggregate lowering.
func (e *emitter) usesIndirectStructParamABI(typ string) bool {
	_, ok := e.module.Structs[typ]
	return ok
}

// derefLLVMType returns T for borrowed Kizu types &T and &var T.
func derefLLVMType(typ string) string {
	if strings.HasPrefix(typ, "&var ") {
		return strings.TrimPrefix(typ, "&var ")
	}
	if strings.HasPrefix(typ, "&") {
		return strings.TrimPrefix(typ, "&")
	}
	return typ
}

// integerLLVMType maps Kizu integer spellings to LLVM integer widths.
func integerLLVMType(typ string) string {
	switch typ {
	case "i8", "u8":
		return "i8"
	case "i16", "u16":
		return "i16"
	case "i32", "u32":
		return "i32"
	default:
		return "i64"
	}
}

// integerBitWidth reports the LLVM integer width for scalar Kizu integers.
func integerBitWidth(typ string) (int, bool) {
	switch typ {
	case "i8", "u8":
		return 8, true
	case "i16", "u16":
		return 16, true
	case "i32", "u32":
		return 32, true
	case "i64", "u64", "usize", "isize":
		return 64, true
	default:
		return 0, false
	}
}

// isUnsignedIntegerType reports whether comparisons and widening should be unsigned.
func isUnsignedIntegerType(typ string) bool {
	return strings.HasPrefix(typ, "u") || typ == "usize"
}

// llvmBinaryOp maps a Kizu binary operator to an LLVM integer instruction.
func llvmBinaryOp(op string) string {
	switch op {
	case "+":
		return "add"
	case "-":
		return "sub"
	case "*":
		return "mul"
	case "/":
		return "sdiv"
	case "%":
		return "srem"
	default:
		return "add"
	}
}

// llvmPredicate maps Kizu comparisons to signed LLVM integer predicates.
func llvmPredicate(op string) string {
	switch op {
	case "==":
		return "eq"
	case "!=":
		return "ne"
	case "<":
		return "slt"
	case "<=":
		return "sle"
	case ">":
		return "sgt"
	case ">=":
		return "sge"
	default:
		return "eq"
	}
}

// llvmTypedPredicate maps a Kizu comparison to a predicate for one integer type.
func llvmTypedPredicate(op string, typ string) string {
	if op == "==" || op == "!=" || !isUnsignedIntegerType(typ) {
		return llvmPredicate(op)
	}
	switch op {
	case "<":
		return "ult"
	case "<=":
		return "ule"
	case ">":
		return "ugt"
	case ">=":
		return "uge"
	default:
		return llvmPredicate(op)
	}
}

// llvmBool maps Kizu bool constants to LLVM constants.
func llvmBool(value string) string {
	if value == "true" {
		return "true"
	}
	return "false"
}

// llvmStructTypeName returns a stable named LLVM type for a declared struct.
func llvmStructTypeName(name string) string {
	var out strings.Builder
	out.WriteString("%kizu.struct.")
	out.WriteString(llvmNamePart(name))
	return out.String()
}

// llvmErrorUnionTypeName returns a stable named LLVM type for a recoverable result.
func llvmErrorUnionTypeName(name string) string {
	errorName, success, ok := errorUnionParts(name)
	if !ok {
		return "%kizu.error.unknown"
	}
	var out strings.Builder
	out.WriteString("%kizu.error.")
	if errorName != "" {
		out.WriteString(llvmNamePart(errorName))
		out.WriteByte('.')
	}
	out.WriteString(llvmNamePart(success))
	return out.String()
}

// llvmUnionTypeName returns a stable named LLVM type for a tagged union.
func llvmUnionTypeName(name string) string {
	return "%kizu.union." + llvmNamePart(name)
}

// llvmFunctionName returns a stable LLVM symbol for a Kizu function name.
func llvmFunctionName(name string) string {
	return llvmNamePart(name)
}

// isArenaHandleType reports whether typ is std::arena::Handle<T>.
func isArenaHandleType(typ string) bool {
	return strings.HasPrefix(typ, "std::arena::Handle<") && strings.HasSuffix(typ, ">")
}

// llvmNamePart keeps generated LLVM type names deterministic and readable.
func llvmNamePart(name string) string {
	if name == "[]u8" {
		return "slice.u8"
	}
	var out strings.Builder
	for _, ch := range []byte(name) {
		if ch == '_' ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') {
			out.WriteByte(ch)
			continue
		}
		out.WriteByte('_')
	}
	return out.String()
}

// errorUnionSuccessType returns T for !T or Error!T.
func errorUnionSuccessType(typ string) (string, bool) {
	_, success, ok := errorUnionParts(typ)
	return success, ok
}

// errorUnionParts returns Error and T for Error!T, or empty Error and T for !T.
func errorUnionParts(typ string) (string, string, bool) {
	if strings.HasPrefix(typ, "!") && len(typ) > 1 {
		return "", strings.TrimPrefix(typ, "!"), true
	}
	idx := strings.Index(typ, "!")
	if idx <= 0 || idx == len(typ)-1 {
		return "", "", false
	}
	return typ[:idx], typ[idx+1:], true
}

// isLowerableErrorUnionSuccess reports whether the current backend can carry T.
func isLowerableErrorUnionSuccess(typ string) bool {
	return typ != ""
}

// escapeString emits a minimal LLVM string literal escape.
func escapeString(value string) string {
	var out strings.Builder
	for _, ch := range []byte(value) {
		switch ch {
		case '\\':
			out.WriteString("\\5C")
		case '"':
			out.WriteString("\\22")
		case '\n':
			out.WriteString("\\0A")
		default:
			out.WriteByte(ch)
		}
	}
	return out.String()
}
