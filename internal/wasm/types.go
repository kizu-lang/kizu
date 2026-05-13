package wasm

import "strings"

// wasmType maps Kizu IR types to WebAssembly value types.
func wasmType(typ string) string {
	switch typ {
	case "bool":
		return "i32"
	case "int", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return "i64"
	case "string":
		return "i32"
	default:
		return "i32"
	}
}

// wasmBinaryOp maps a Kizu binary operator to a WebAssembly integer operation.
func wasmBinaryOp(op string) string {
	switch op {
	case "+":
		return "i64.add"
	case "-":
		return "i64.sub"
	case "*":
		return "i64.mul"
	case "/":
		return "i64.div_s"
	case "%":
		return "i64.rem_s"
	default:
		return "i64.add"
	}
}

// wasmCompareOp maps a Kizu comparison to a WebAssembly integer operation.
func wasmCompareOp(op string) string {
	switch op {
	case "==":
		return "i64.eq"
	case "!=":
		return "i64.ne"
	case "<":
		return "i64.lt_s"
	case "<=":
		return "i64.le_s"
	case ">":
		return "i64.gt_s"
	case ">=":
		return "i64.ge_s"
	default:
		return "i64.eq"
	}
}

// symbolName converts an IR value name to a stable WebAssembly local name.
func symbolName(name string) string {
	name = strings.TrimPrefix(name, "%")
	replacer := strings.NewReplacer(".", "_", "-", "_", "<", "_", ">", "_")
	name = replacer.Replace(name)
	if name != "" && name[0] >= '0' && name[0] <= '9' {
		name = "v" + name
	}
	return "$" + name
}

// stringBytes escapes data bytes for WAT data segments.
func stringBytes(value string) string {
	var out strings.Builder
	for _, b := range []byte(value) {
		switch b {
		case '\\', '"':
			out.WriteByte('\\')
			out.WriteString(hexByte(b))
		case '\n':
			out.WriteString("\\0a")
		case '\t':
			out.WriteString("\\09")
		default:
			if b < 0x20 || b >= 0x7f {
				out.WriteByte('\\')
				out.WriteString(hexByte(b))
			} else {
				out.WriteByte(b)
			}
		}
	}
	return out.String()
}

// hexByte returns a two-character lowercase hexadecimal byte.
func hexByte(value byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0f]})
}
