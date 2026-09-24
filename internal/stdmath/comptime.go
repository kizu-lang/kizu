package stdmath

import (
	"math"
	"strconv"
)

// Call evaluates one std::math function a compile-time float expression may
// call, by its qualified name, and reports whether the name is one. The set
// is the one whose value std fixes bit for bit on every target it names:
// sqrt is correctly rounded everywhere, pi and tau are literals, and sin and
// cos are this package's port of std.
func Call(name string, args []float64, native bool) (float64, bool) {
	switch {
	case name == "std::math::pi" && len(args) == 0:
		return 3.141592653589793, true
	case name == "std::math::tau" && len(args) == 0:
		return 6.283185307179586, true
	case name == "std::math::sqrt" && len(args) == 1:
		return Sqrt(args[0]), true
	case name == "std::math::sin" && len(args) == 1:
		return Sin(args[0], native), true
	case name == "std::math::cos" && len(args) == 1:
		return Cos(args[0], native), true
	default:
		return 0, false
	}
}

// Names reports whether name is a std::math function Call evaluates.
func Names(name string) bool {
	switch name {
	case "std::math::pi", "std::math::tau", "std::math::sqrt", "std::math::sin", "std::math::cos":
		return true
	default:
		return false
	}
}

// Binary evaluates + - * / on two floats, each rounded once as IEEE 754
// says, and reports whether op is one of them.
func Binary(op string, left, right float64) (float64, bool) {
	switch op {
	case "+":
		return left + right, true
	case "-":
		return left - right, true
	case "*":
		return float64(left * right), true
	case "/":
		return left / right, true
	default:
		return 0, false
	}
}

// Compare evaluates a float comparison and reports whether op is one.
func Compare(op string, left, right float64) (bool, bool) {
	switch op {
	case "==":
		return left == right, true
	case "!=":
		return left != right, true
	case "<":
		return left < right, true
	case "<=":
		return left <= right, true
	case ">":
		return left > right, true
	case ">=":
		return left >= right, true
	default:
		return false, false
	}
}

// Literal spells a folded float as IR constant text: C99 hexadecimal, as
// strconv formats it with 'x' and the shortest mantissa (`0x1.921fb54442d18p+01`).
// It names the bits exactly, which a decimal spelling can only do with a
// correctly rounded conversion each compiler would have to agree on.
func Literal(value float64) string {
	return strconv.FormatFloat(value, 'x', -1, 64)
}

// Finite reports whether a folded value can be a constant: a literal names a
// finite float, so an infinity or NaN at compile time is the program's error.
func Finite(value float64) bool {
	return !math.IsInf(value, 0) && !math.IsNaN(value)
}
