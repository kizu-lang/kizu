package llvm

import "strings"

// llvmType maps Kizu IR types to LLVM IR types.
func llvmType(typ string) string {
	if strings.HasPrefix(typ, "!") && typ != "!void" {
		return llvmType(strings.TrimPrefix(typ, "!"))
	}
	switch typ {
	case "void":
		return "void"
	case "bool":
		return "i1"
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return integerLLVMType(typ)
	case "[]const u8":
		return "ptr"
	default:
		return "ptr"
	}
}

// structLLVMName returns a stable LLVM identified struct name.
func structLLVMName(name string) string {
	replacer := strings.NewReplacer(".", "_", ":", "_", "<", "_", ">", "_", " ", "_")
	return "%struct." + replacer.Replace(name)
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

// llvmZero returns a valid placeholder operand for an omitted value.
func llvmZero(typ string) string {
	switch llvmType(typ) {
	case "i1":
		return "false"
	case "i8", "i16", "i32", "i64":
		return "0"
	default:
		return "null"
	}
}

// llvmOperand coerces an opaque placeholder into a valid operand for typ.
func llvmOperand(operand string, typ string) string {
	if operand != "null" {
		return operand
	}
	return llvmZero(typ)
}

// llvmReturnOperand coerces opaque placeholder returns to the function result type.
func llvmReturnOperand(operand string, valueType string, returnType string) string {
	return llvmTypedOperand(operand, valueType, returnType)
}

// llvmTypedOperand coerces placeholder or mismatched opaque values to wantType.
func llvmTypedOperand(operand string, valueType string, wantType string) string {
	if strings.HasPrefix(valueType, "!") && strings.TrimPrefix(valueType, "!") == wantType {
		return llvmOperand(operand, wantType)
	}
	if llvmType(valueType) == llvmType(wantType) {
		return llvmOperand(operand, wantType)
	}
	return llvmZero(wantType)
}

// llvmLocal maps Kizu SSA names to LLVM local identifiers.
func llvmLocal(name string) string {
	if len(name) > 1 && name[0] == '%' && isDecimal(name[1:]) {
		return "%v" + name[1:]
	}
	return name
}

// isDecimal reports whether text only contains ASCII decimal digits.
func isDecimal(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
