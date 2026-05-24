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
		return "ptr"
	}
}

// llvmType maps Kizu IR types to LLVM IR types.
func (e *emitter) llvmType(typ string) string {
	if _, ok := errorUnionSuccessType(typ); ok {
		return llvmErrorUnionTypeName(typ)
	}
	if _, ok := e.module.Structs[typ]; ok {
		return llvmStructTypeName(typ)
	}
	return llvmPrimitiveType(typ)
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
	switch typ {
	case "void", "bool", "[]u8",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"usize", "isize":
		return true
	default:
		return false
	}
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
