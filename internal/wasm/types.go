package wasm

import (
	"fmt"
	"math"
	"strings"

	"github.com/kizu-lang/kizu/internal/typ"
)

// wasmType maps Kizu IR types to WebAssembly value types. Memory-backed
// aggregates and references are i32 addresses; named tags share the i64 scalar
// representation used by integer comparisons.
func (e *emitter) wasmType(typ string) string {
	if isIntegerType(typ) || e.isNamedI64Type(typ) {
		return "i64"
	}
	if isFloatType(typ) {
		return typ
	}
	return "i32"
}

// isFloatType reports whether typ is `f32` or `f64`, each held in the wasm
// value type of the same name.
func isFloatType(typ string) bool {
	return typ == "f32" || typ == "f64"
}

// wasmFloatBinaryOp maps a Kizu arithmetic operator on a float type to a
// WebAssembly operation.
func wasmFloatBinaryOp(op string, typ string) string {
	switch op {
	case "-":
		return typ + ".sub"
	case "*":
		return typ + ".mul"
	case "/":
		return typ + ".div"
	default:
		return typ + ".add"
	}
}

// wasmFloatCompareOp maps a Kizu comparison on a float type to a WebAssembly
// operation. Every one is ordered except `ne`, which is true for NaN, as
// IEEE 754 says.
func wasmFloatCompareOp(op string, typ string) string {
	switch op {
	case "!=":
		return typ + ".ne"
	case "<":
		return typ + ".lt"
	case "<=":
		return typ + ".le"
	case ">":
		return typ + ".gt"
	case ">=":
		return typ + ".ge"
	default:
		return typ + ".eq"
	}
}

// isIntegerType reports whether a Kizu type is a scalar integer. Every integer
// width is held in one wasm i64, so the width only matters to the frontend.
func isIntegerType(typ string) bool {
	switch typ {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return true
	default:
		return false
	}
}

// wasmBinaryOp maps a Kizu binary operator on typ to a WebAssembly integer
// operation. Division and remainder read the sign of the type.
func wasmBinaryOp(op string, typ string) string {
	switch op {
	case "+":
		return "i64.add"
	case "-":
		return "i64.sub"
	case "*":
		return "i64.mul"
	case "/":
		if isUnsignedIntegerType(typ) {
			return "i64.div_u"
		}
		return "i64.div_s"
	case "%":
		if isUnsignedIntegerType(typ) {
			return "i64.rem_u"
		}
		return "i64.rem_s"
	case "&":
		return "i64.and"
	case "|":
		return "i64.or"
	case "^":
		return "i64.xor"
	default:
		return "i64.add"
	}
}

// isUnsignedIntegerType reports whether typ is one of the unsigned widths.
func isUnsignedIntegerType(typ string) bool {
	return strings.HasPrefix(typ, "u")
}

// narrowResult wraps an arithmetic result to the width of typ, since every
// integer lives in an i64 local: an unsigned type keeps its low bits, a
// signed one is sign-extended from its top bit, and a 64-bit type is left
// alone. This is what makes `+ - * <<` wrap as SPEC §6.9.2 says.
func narrowResult(typ string, expr string) string {
	switch typ {
	case "u8":
		return "(i64.and " + expr + " (i64.const 255))"
	case "u16":
		return "(i64.and " + expr + " (i64.const 65535))"
	case "u32":
		return "(i64.and " + expr + " (i64.const 4294967295))"
	case "i8":
		return "(i64.extend8_s " + expr + ")"
	case "i16":
		return "(i64.extend16_s " + expr + ")"
	case "i32":
		return "(i64.extend32_s " + expr + ")"
	default:
		return expr
	}
}

// wrapsResult reports whether op can leave the width of its type, so that
// its result needs narrowResult. Comparisons, `& | ^`, `/ %`, and `>>`
// cannot.
func wrapsResult(op string) bool {
	return op == "+" || op == "-" || op == "*" || op == "<<"
}

// integerBitWidth reports the width a Kizu integer type shifts within. The
// value itself is held in an i64 local; the width only bounds the amount.
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

// floatConstExpr spells a floating-point constant exactly: the bits of the
// value reinterpreted, since a decimal in the text format would round again.
func floatConstExpr(kind string, literal string) (string, bool) {
	value, ok := typ.ParseFloatLiteral(literal)
	if !ok {
		return "", false
	}
	if kind == "f32" {
		// An f32 is spelled as the f64 it widens to, demoted: the demotion is
		// exact, and the bits of an f64 are the one primitive both compilers
		// have.
		value = float64(float32(value))
		return fmt.Sprintf("(f32.demote_f64 (f64.reinterpret_i64 (i64.const %d)))",
			int64(math.Float64bits(value))), true
	}
	return fmt.Sprintf("(f64.reinterpret_i64 (i64.const %d))", int64(math.Float64bits(value))), true
}
