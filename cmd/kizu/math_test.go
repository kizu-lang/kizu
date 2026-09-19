package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// mathUnaryFunctions lists the one-argument std::math functions beside the Go
// function each is checked against, and how many units in the last place the
// two may differ by. The IEEE operations and the bit manipulations answer
// exactly. The rest follow the same algorithms Go does, but Go fuses a
// multiply and an add into one rounding on arm64 and runs exp as assembly
// there, which can move the last bit either way.
var mathUnaryFunctions = []struct {
	name string
	want func(float64) float64
	ulps uint64
}{
	{"sqrt", math.Sqrt, 0},
	{"floor", math.Floor, 0},
	{"ceil", math.Ceil, 0},
	{"trunc", math.Trunc, 0},
	{"round", math.Round, 0},
	{"abs", math.Abs, 0},
	{"exp", math.Exp, 1},
	{"exp2", math.Exp2, 1},
	{"expm1", math.Expm1, 1},
	{"log", math.Log, 1},
	{"log1p", math.Log1p, 1},
	{"log2", math.Log2, 1},
	{"log10", math.Log10, 1},
	{"sin", math.Sin, 1},
	{"cos", math.Cos, 1},
	// tan ends in a reciprocal, which doubles what the fused multiply-add
	// moved.
	{"tan", math.Tan, 2},
	{"asin", math.Asin, 1},
	{"acos", math.Acos, 1},
	{"atan", math.Atan, 1},
	{"sinh", math.Sinh, 1},
	{"cosh", math.Cosh, 1},
	{"tanh", math.Tanh, 1},
	{"asinh", math.Asinh, 1},
	{"acosh", math.Acosh, 1},
	{"atanh", math.Atanh, 1},
	{"cbrt", math.Cbrt, 1},
	{"round_even", math.RoundToEven, 0},
	{"frexp_fraction", func(x float64) float64 { f, _ := math.Frexp(x); return f }, 0},
	{"frexp_exponent", func(x float64) float64 { _, e := math.Frexp(x); return float64(e) }, 0},
	{"modf_whole", func(x float64) float64 { w, _ := math.Modf(x); return w }, 0},
	{"modf_fraction", func(x float64) float64 { _, f := math.Modf(x); return f }, 0},
}

// mathHelpers are the functions the generated program defines itself, to
// print one number of a std::math answer that has two.
const mathHelpers = `fn frexp_fraction(value: f64) -> f64 {
    return math::frexp(value).fraction;
}

fn frexp_exponent(value: f64) -> f64 {
    return cast<f64>(math::frexp(value).exponent);
}

fn modf_whole(value: f64) -> f64 {
    return math::modf(value).whole;
}

fn modf_fraction(value: f64) -> f64 {
    return math::modf(value).fraction;
}

`

// mathCallee spells how the generated program calls a function: a helper it
// defines by its bare name, anything else through the module.
func mathCallee(name string) string {
	if strings.Contains(mathHelpers, "fn "+name+"(") {
		return name
	}
	return "math::" + name
}

// mathBinaryFunctions lists the two-argument std::math functions beside the Go
// function each is checked against. min and max are spelled out because Go's
// math.Min and math.Max answer NaN when either argument is NaN, where
// std::math answers the other argument the way C's fmin and fmax do.
var mathBinaryFunctions = []struct {
	name string
	want func(float64, float64) float64
	ulps uint64
}{
	{"copysign", math.Copysign, 0},
	{"hypot", math.Hypot, 1},
	{"min", func(a, b float64) float64 {
		switch {
		case math.IsNaN(a):
			return b
		case math.IsNaN(b):
			return a
		}
		return math.Min(a, b)
	}, 0},
	{"max", func(a, b float64) float64 {
		switch {
		case math.IsNaN(a):
			return b
		case math.IsNaN(b):
			return a
		}
		return math.Max(a, b)
	}, 0},
	{"pow", math.Pow, 1},
	{"fmod", math.Mod, 0},
	{"atan2", math.Atan2, 1},
}

// mathTernaryFunctions lists the three-argument std::math functions beside
// the Go function each is checked against. fma is exact by definition, and
// Go's is a hardware instruction or exact software.
var mathTernaryFunctions = []struct {
	name string
	want func(float64, float64, float64) float64
	ulps uint64
}{
	{"fma", math.FMA, 0},
}

// mathBits lists the values std::math is checked on: the float text bit
// patterns, which cover the edges of the format and a spread of random
// doubles, plus the halves and near-halves that rounding is about, both
// infinities, and NaN.
func mathBits() []uint64 {
	bits := floatTextBits()
	for _, value := range []float64{
		0.5, -0.5, 1.5, -1.5, 2.5, -2.5, 0.49999999999999994, -0.49999999999999994,
		4503599627370495.5, -4503599627370495.5, 4503599627370496, 9007199254740993,
		2, 3, 10, 0.001, 100.5, 1e-300, 1e300, 700, 709.7, 709.8, -745, -745.2,
		1023.5, 1024, -1074, -1074.5, 1e-10, -1e-10, 0.25, -0.75, 6.5, -7,
		math.Pi, -math.Pi, math.Pi / 2, math.Pi / 4, 3 * math.Pi / 4, 2 * math.Pi, 0.66, 0.7, 2.5,
		536870911, 536870912, 536870913, 1e15, 1e22, 1e50, 1e100, 1e200, 1e300,
		0.1, 10, -1, 1e308, 1e-308, 4.9e-324, 3, 4, 0.5000000000000001, 21.5, 22, 0.625, 0.7,
		math.Inf(1), math.Inf(-1), math.NaN(),
	} {
		bits = append(bits, math.Float64bits(value))
	}
	return bits
}

// mathProgram writes a program that applies every std::math function to every
// value, and every two-argument one to each value beside its successor, and
// prints the bits of each answer on its own line.
func mathProgram(bits []uint64) string {
	var b strings.Builder
	b.WriteString("import std::float;\nimport std::math;\n\n")
	b.WriteString("fn show(value: f64) -> void {\n    print(cast<i64>(float::bits(value)));\n}\n\n")
	b.WriteString(mathHelpers)
	// A u64 literal cannot exceed the i64 range, so a pattern is written as
	// its two halves.
	b.WriteString("fn from(high: u64, low: u64) -> f64 {\n")
	b.WriteString("    return float::from_bits(high << 32 | low);\n}\n\n")
	from := func(pattern uint64) string {
		return fmt.Sprintf("from(%d, %d)", pattern>>32, pattern&0xFFFFFFFF)
	}
	var calls []string
	for _, pattern := range bits {
		for _, function := range mathUnaryFunctions {
			calls = append(calls, fmt.Sprintf("    show(%s(%s));\n",
				mathCallee(function.name), from(pattern)))
		}
	}
	for i := range bits[:len(bits)-1] {
		for _, function := range mathBinaryFunctions {
			calls = append(calls, fmt.Sprintf("    show(%s(%s, %s));\n",
				mathCallee(function.name), from(bits[i]), from(bits[i+1])))
		}
	}
	for i := range bits[:len(bits)-2] {
		for _, function := range mathTernaryFunctions {
			calls = append(calls, fmt.Sprintf("    show(%s(%s, %s, %s));\n",
				mathCallee(function.name), from(bits[i]), from(bits[i+1]), from(bits[i+2])))
		}
	}
	// A wasm function holds at most 50000 locals and every call here takes a
	// few, so the calls are dealt out to parts main runs in order.
	const callsPerPart = 1000
	parts := 0
	for start := 0; start < len(calls); start += callsPerPart {
		fmt.Fprintf(&b, "fn part%d() -> void {\n", parts)
		for _, call := range calls[start:min(start+callsPerPart, len(calls))] {
			b.WriteString(call)
		}
		b.WriteString("}\n\n")
		parts++
	}
	b.WriteString("fn main() -> void {\n")
	for part := range parts {
		fmt.Fprintf(&b, "    part%d();\n", part)
	}
	b.WriteString("}\n")
	return b.String()
}

// mathExpectation is one answer Go's math gives and how far std::math may be
// from it, or nothing to compare with where Go's answer is not the oracle.
type mathExpectation struct {
	value float64
	ulps  uint64
	skip  bool
}

// goAnswersWrongly reports the cases Go's math is known to answer wrongly, so
// that they are not held against std::math. On amd64 Go's Exp and Log are
// assembly whose argument reduction gives up at the edges: Log of a subnormal
// comes back as Log of the smallest normal, and Exp overflows a little below
// the true threshold. The arm64 build runs the same algorithms std::math does
// and checks these cases exactly.
func goAnswersWrongly(name string, x float64) bool {
	if runtime.GOARCH != "amd64" {
		return false
	}
	const smallestNormal = 2.2250738585072014e-308
	switch name {
	case "log", "log10":
		return x != 0 && math.Abs(x) < smallestNormal
	case "exp":
		return x > 709
	}
	return false
}

// mathWant lists the answers Go's math gives in the order mathProgram prints
// them.
func mathWant(bits []uint64) []mathExpectation {
	var want []mathExpectation
	for _, pattern := range bits {
		for _, function := range mathUnaryFunctions {
			x := math.Float64frombits(pattern)
			want = append(want, mathExpectation{
				value: function.want(x), ulps: function.ulps, skip: goAnswersWrongly(function.name, x)})
		}
	}
	for i := range bits[:len(bits)-1] {
		for _, function := range mathBinaryFunctions {
			a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
			want = append(want, mathExpectation{value: function.want(a, b), ulps: function.ulps})
		}
	}
	for i := range bits[:len(bits)-2] {
		for _, function := range mathTernaryFunctions {
			a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
			c := math.Float64frombits(bits[i+2])
			want = append(want, mathExpectation{value: function.want(a, b, c), ulps: function.ulps})
		}
	}
	return want
}

// mathMatches reports whether got is within the allowed units in the last
// place of want. Every NaN matches every NaN, because which NaN comes back is
// not part of what any function promises; an infinity matches only itself, and
// a zero is one unit from the smallest subnormal of its sign.
func mathMatches(got float64, want mathExpectation) bool {
	if want.skip {
		return true
	}
	if math.IsNaN(want.value) || math.IsNaN(got) {
		return math.IsNaN(want.value) && math.IsNaN(got)
	}
	a, b := math.Float64bits(got), math.Float64bits(want.value)
	if a == b {
		return true
	}
	if math.IsInf(got, 0) || math.IsInf(want.value, 0) {
		return false
	}
	if math.Signbit(got) != math.Signbit(want.value) {
		return false
	}
	distance := a - b
	if b > a {
		distance = b - a
	}
	return distance <= want.ulps
}

// mathLabel names the case behind one output line.
func mathLabel(bits []uint64, line int) string {
	unary := len(bits) * len(mathUnaryFunctions)
	if line < unary {
		function := mathUnaryFunctions[line%len(mathUnaryFunctions)]
		value := math.Float64frombits(bits[line/len(mathUnaryFunctions)])
		return fmt.Sprintf("%s(%g)", function.name, value)
	}
	line -= unary
	binary := (len(bits) - 1) * len(mathBinaryFunctions)
	if line < binary {
		function := mathBinaryFunctions[line%len(mathBinaryFunctions)]
		i := line / len(mathBinaryFunctions)
		a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
		return fmt.Sprintf("%s(%g, %g)", function.name, a, b)
	}
	line -= binary
	function := mathTernaryFunctions[line%len(mathTernaryFunctions)]
	i := line / len(mathTernaryFunctions)
	a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
	return fmt.Sprintf("%s(%g, %g, %g)", function.name, a, b, math.Float64frombits(bits[i+2]))
}

// compareMathOutput checks one run's output line by line against Go's math.
func compareMathOutput(t *testing.T, bits []uint64, output string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	want := mathWant(bits)
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d\n%s", len(lines), len(want), output)
	}
	failures := 0
	for i := range want {
		pattern, err := strconv.ParseInt(lines[i], 10, 64)
		if err != nil {
			t.Fatalf("%s: printed %q", mathLabel(bits, i), lines[i])
		}
		got := math.Float64frombits(uint64(pattern))
		if mathMatches(got, want[i]) {
			continue
		}
		failures++
		if failures > 20 {
			continue
		}
		t.Errorf("%s: got %v (%#x), want %v (%#x)", mathLabel(bits, i),
			got, math.Float64bits(got), want[i].value, math.Float64bits(want[i].value))
	}
	if failures > 20 {
		t.Errorf("%d more mismatches", failures-20)
	}
}

// TestMath compares std::math with Go's math on the same values: the Go seed is
// the oracle for the std functions, the way it is for the compiler.
func TestMath(t *testing.T) {
	bits := mathBits()
	dir := t.TempDir()
	path := filepath.Join(dir, "math.kizu")
	if err := os.WriteFile(path, []byte(mathProgram(bits)), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runKizuEnv(nil, "run", path)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, output)
	}
	compareMathOutput(t, bits, output)
}

// TestMathWASM runs the same std::math comparison on the wasm32-wasi target, so
// the float instructions of the wasm backend and its binary encoder answer
// exactly what the native target does.
func TestMathWASM(t *testing.T) {
	wasmtime, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime is required for binary execution")
	}
	bits := mathBits()
	dir := t.TempDir()
	source := filepath.Join(dir, "math.kizu")
	if err := os.WriteFile(source, []byte(mathProgram(bits)), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, "math.wasm")
	build, err := kizuCommand("build", "--target", "wasm32-wasi", "--emit", "wasm",
		"-o", artifact, source).CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, build)
	}
	output, err := exec.Command(wasmtime, "run", artifact).CombinedOutput()
	if err != nil {
		t.Fatalf("wasmtime failed: %v\n%s", err, output)
	}
	compareMathOutput(t, bits, string(output))
}
