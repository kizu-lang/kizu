package llvm

import "strings"

// llvmType maps Kizu IR types to LLVM IR types.
func llvmType(typ string) string {
	switch typ {
	case "void":
		return "void"
	case "bool":
		return "i1"
	case "int", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return integerLLVMType(typ)
	case "string":
		return "ptr"
	default:
		return "ptr"
	}
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
