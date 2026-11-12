package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mathFunction is a std::math function the test calls and how many units in
// the last place its answer may be from the correctly rounded one. The IEEE
// operations, the bit manipulations and fma are exact. docs/std/math.md
// promises the rest within 1 ulp of the true value, which is at most 1 ulp
// from the true value rounded.
type mathFunction struct {
	name string
	ulps uint64
}

// mathUnaryFunctions lists the one-argument std::math functions, in the
// order of the `unary` line of testdata/std_math.txt.
var mathUnaryFunctions = []mathFunction{
	{"sqrt", 0}, {"floor", 0}, {"ceil", 0}, {"trunc", 0}, {"round", 0}, {"abs", 0},
	{"exp", 1}, {"exp2", 1}, {"expm1", 1}, {"log", 1}, {"log1p", 1}, {"log2", 1}, {"log10", 1},
	{"sin", 1}, {"cos", 1}, {"sin_fused", 1}, {"cos_fused", 1}, {"exp_fused", 1}, {"log_fused", 1},
	{"tan", 1}, {"asin", 1}, {"acos", 1}, {"atan", 1},
	// sinh below 1/2 is Cephes's rational function, 2 ulp at worst on these
	// values (sinh(0.03024484113708804)).
	{"sinh", 2}, {"cosh", 1}, {"tanh", 1}, {"asinh", 1}, {"acosh", 1}, {"atanh", 1},
	{"cbrt", 1}, {"round_even", 0},
	{"frexp_fraction", 0}, {"frexp_exponent", 0}, {"modf_whole", 0}, {"modf_fraction", 0},
}

// mathHelpers are the functions the generated program defines itself, to
// print one number of a std::math answer that has two, and to call the fused
// kernels (`_with<true>`) by a bare name.
const mathHelpers = `fn frexp_fraction(value: f64) -> f64 {
    return math::frexp<f64>(value).fraction;
}

fn frexp_exponent(value: f64) -> f64 {
    return cast<f64>(math::frexp<f64>(value).exponent);
}

fn modf_whole(value: f64) -> f64 {
    return math::modf<f64>(value).whole;
}

fn modf_fraction(value: f64) -> f64 {
    return math::modf<f64>(value).fraction;
}

fn sin_fused(value: f64) -> f64 {
    return math::sin_with<true>(value);
}

fn cos_fused(value: f64) -> f64 {
    return math::cos_with<true>(value);
}

fn exp_fused(value: f64) -> f64 {
    return math::exp_with<true>(value);
}

fn log_fused(value: f64) -> f64 {
    return math::log_with<true>(value);
}

fn pow_fused(base: f64, exponent: f64) -> f64 {
    return math::pow_with<true>(base, exponent);
}

`

// mathCallee spells how the generated program calls a function: a helper it
// defines by its bare name, anything else through the module with f64 as
// the float type.
func mathCallee(name string) string {
	if mathIsHelper(name) {
		return name
	}
	return "math::" + name + "<f64>"
}

// mathBinaryFunctions lists the two-argument std::math functions, in the
// order of the `binary` line of testdata/std_math.txt.
var mathBinaryFunctions = []mathFunction{
	{"copysign", 0}, {"hypot", 1}, {"min", 0}, {"max", 0},
	{"pow", 1}, {"pow_fused", 1}, {"fmod", 0}, {"atan2", 1},
}

// mathTernaryFunctions lists the three-argument std::math functions, in the
// order of the `ternary` line of testdata/std_math.txt.
var mathTernaryFunctions = []mathFunction{{"fma", 0}}

// mathReference is testdata/std_math.txt: the values std::math is called on
// and, in the order mathProgram prints them, the answers it has to give. The
// file is written by testdata/std_math.py, whose comment says how each answer
// is the true value rounded once.
type mathReference struct {
	bits   []uint64
	want   []uint64
	single []uint32
	// singleWant holds, per single value, one answer per function that is
	// not a helper, in the order of mathUnaryFunctions.
	singleWant [][]uint32
}

// mathHeaders is what the function lines of testdata/std_math.txt have to
// say: the tables above, in their order.
func mathHeaders() map[string]string {
	names := func(functions []mathFunction, helpers bool) string {
		var out []string
		for _, function := range functions {
			if helpers || !mathIsHelper(function.name) {
				out = append(out, function.name)
			}
		}
		return strings.Join(out, " ")
	}
	return map[string]string{
		"unary":   names(mathUnaryFunctions, true),
		"binary":  names(mathBinaryFunctions, true),
		"ternary": names(mathTernaryFunctions, true),
		"single":  names(mathUnaryFunctions, false),
	}
}

// mathLine is one data line of testdata/std_math.txt: its kind, the
// arguments, and the answers.
type mathLine struct {
	kind    string
	inputs  []uint64
	answers []uint64
}

// readMathLines reads the data lines of testdata/std_math.txt, checking the
// function lines on the way.
func readMathLines(t *testing.T) []mathLine {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "std_math.txt"))
	if err != nil {
		t.Fatal(err)
	}
	headers := mathHeaders()
	var lines []mathLine
	for number, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		kind, rest, _ := strings.Cut(line, " ")
		if want, ok := headers[kind]; ok {
			if rest != want {
				t.Fatalf("std_math.txt:%d: %s functions are %q, the test calls %q",
					number+1, kind, rest, want)
			}
			continue
		}
		if kind == "#" {
			continue
		}
		var fields []uint64
		for _, field := range strings.Fields(rest) {
			value, err := strconv.ParseUint(field, 16, 64)
			if err != nil {
				t.Fatalf("std_math.txt:%d: %v", number+1, err)
			}
			fields = append(fields, value)
		}
		inputs := map[string]int{"u": 1, "b": 2, "t": 3, "s": 1}[kind]
		if inputs == 0 || len(fields) <= inputs {
			t.Fatalf("std_math.txt:%d: cannot read %q", number+1, line)
		}
		lines = append(lines, mathLine{kind, fields[:inputs], fields[inputs:]})
	}
	return lines
}

// loadMathReference reads testdata/std_math.txt. The arguments of the b and
// t lines have to be the values of the u lines taken two and three at a
// time, the way mathProgram calls the functions.
func loadMathReference(t *testing.T) mathReference {
	t.Helper()
	var ref mathReference
	var binary, ternary []uint64
	var operands [][]uint64
	for _, line := range readMathLines(t) {
		switch line.kind {
		case "u":
			ref.bits = append(ref.bits, line.inputs[0])
			ref.want = append(ref.want, line.answers...)
		case "b":
			operands = append(operands, line.inputs)
			binary = append(binary, line.answers...)
		case "t":
			operands = append(operands, line.inputs)
			ternary = append(ternary, line.answers...)
		case "s":
			ref.single = append(ref.single, uint32(line.inputs[0]))
			answers := make([]uint32, 0, len(line.answers))
			for _, answer := range line.answers {
				answers = append(answers, uint32(answer))
			}
			ref.singleWant = append(ref.singleWant, answers)
		}
	}
	if len(operands) != 2*len(ref.bits)-3 {
		t.Fatalf("std_math.txt has %d b and t lines for %d values", len(operands), len(ref.bits))
	}
	for i, inputs := range operands {
		start := i
		if i >= len(ref.bits)-1 {
			start = i - (len(ref.bits) - 1)
		}
		for j, input := range inputs {
			if input != ref.bits[start+j] {
				t.Fatalf("std_math.txt: line %d of b and t is not on the u values in order", i+1)
			}
		}
	}
	ref.want = append(append(ref.want, binary...), ternary...)
	return ref
}

// mathIsHelper reports whether the generated program defines a function
// itself rather than calling std::math for it.
func mathIsHelper(name string) bool {
	return strings.Contains(mathHelpers, "fn "+name+"(")
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

// mathAllowance lists, in the order mathProgram prints its lines, how many
// units in the last place each answer may be off.
func mathAllowance(bits []uint64) []uint64 {
	var ulps []uint64
	for range bits {
		for _, function := range mathUnaryFunctions {
			ulps = append(ulps, function.ulps)
		}
	}
	for range bits[:len(bits)-1] {
		for _, function := range mathBinaryFunctions {
			ulps = append(ulps, function.ulps)
		}
	}
	for range bits[:len(bits)-2] {
		for _, function := range mathTernaryFunctions {
			ulps = append(ulps, function.ulps)
		}
	}
	return ulps
}

// mathMatches reports whether got is within `ulps` units in the last place
// of want, both as bit patterns of a float `width` bits wide. Every NaN
// matches every NaN, because which NaN comes back is not part of what any
// function promises; an infinity matches only itself, and a zero is one unit
// from the smallest subnormal of its sign.
func mathMatches(got, want uint64, width int, ulps uint64) bool {
	sign := uint64(1) << (width - 1)
	exponent := uint64(0x7FF) << 52
	if width == 32 {
		exponent = 0xFF << 23
	}
	nan := func(bits uint64) bool { return bits&exponent == exponent && bits&^(sign|exponent) != 0 }
	inf := func(bits uint64) bool { return bits&^sign == exponent }
	if nan(got) || nan(want) {
		return nan(got) && nan(want)
	}
	if got == want {
		return true
	}
	if inf(got) || inf(want) || got&sign != want&sign {
		return false
	}
	distance := got - want
	if want > got {
		distance = want - got
	}
	return distance <= ulps
}

// mathLabel names the case behind one output line.
func mathLabel(bits []uint64, line int) string {
	value := func(i int) float64 { return math.Float64frombits(bits[i]) }
	unary := len(bits) * len(mathUnaryFunctions)
	if line < unary {
		function := mathUnaryFunctions[line%len(mathUnaryFunctions)]
		return fmt.Sprintf("%s(%g)", function.name, value(line/len(mathUnaryFunctions)))
	}
	line -= unary
	binary := (len(bits) - 1) * len(mathBinaryFunctions)
	if line < binary {
		function := mathBinaryFunctions[line%len(mathBinaryFunctions)]
		i := line / len(mathBinaryFunctions)
		return fmt.Sprintf("%s(%g, %g)", function.name, value(i), value(i+1))
	}
	line -= binary
	function := mathTernaryFunctions[line%len(mathTernaryFunctions)]
	i := line / len(mathTernaryFunctions)
	return fmt.Sprintf("%s(%g, %g, %g)", function.name, value(i), value(i+1), value(i+2))
}

// compareMathOutput checks one run's output line by line against the
// reference.
func compareMathOutput(t *testing.T, ref mathReference, output string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	ulps := mathAllowance(ref.bits)
	if len(lines) != len(ref.want) || len(ulps) != len(ref.want) {
		t.Fatalf("got %d lines, the reference has %d answers for %d calls\n%s",
			len(lines), len(ref.want), len(ulps), output)
	}
	failures := 0
	for i, want := range ref.want {
		pattern, err := strconv.ParseInt(lines[i], 10, 64)
		if err != nil {
			t.Fatalf("%s: printed %q", mathLabel(ref.bits, i), lines[i])
		}
		if mathMatches(uint64(pattern), want, 64, ulps[i]) {
			continue
		}
		failures++
		if failures > 20 {
			continue
		}
		got := math.Float64frombits(uint64(pattern))
		t.Errorf("%s: got %v (%#x), want %v (%#x)", mathLabel(ref.bits, i),
			got, uint64(pattern), math.Float64frombits(want), want)
	}
	if failures > 20 {
		t.Errorf("%d more mismatches", failures-20)
	}
}

// TestMath checks std::math against the correctly rounded answers in
// testdata/std_math.txt.
func TestMath(t *testing.T) {
	ref := loadMathReference(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "math.kizu")
	if err := os.WriteFile(path, []byte(mathProgram(ref.bits)), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runKizuEnv(nil, "run", path)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, output)
	}
	compareMathOutput(t, ref, output)
}

// TestMathWASM runs the same std::math check on the wasm32-wasi target, so
// the float instructions of the wasm backend and its binary encoder answer
// exactly what the native target does: std::math promises the same bits on
// every target, so the two outputs must be equal line for line.
func TestMathWASM(t *testing.T) {
	wasmtime, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime is required for binary execution")
	}
	ref := loadMathReference(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "math.kizu")
	if err := os.WriteFile(source, []byte(mathProgram(ref.bits)), 0o644); err != nil {
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
	compareMathOutput(t, ref, string(output))
	native, err := runKizuEnv(nil, "run", source)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, native)
	}
	wasmLines := strings.Split(string(output), "\n")
	nativeLines := strings.Split(native, "\n")
	for i := range wasmLines {
		if i >= len(nativeLines) || wasmLines[i] == nativeLines[i] ||
			mathBothNaN(wasmLines[i], nativeLines[i]) {
			continue
		}
		t.Fatalf("%s: wasm printed %s, native %s", mathLabel(ref.bits, i), wasmLines[i], nativeLines[i])
	}
	if len(wasmLines) != len(nativeLines) {
		t.Fatalf("wasm printed %d lines, native %d", len(wasmLines), len(nativeLines))
	}
}

// mathBothNaN reports whether two printed bit patterns are both NaN. wasm
// leaves the sign and payload of a NaN an operation produces to the engine,
// so only NaN-ness is the same bits across targets.
func mathBothNaN(left, right string) bool {
	a, errA := strconv.ParseInt(left, 10, 64)
	b, errB := strconv.ParseInt(right, 10, 64)
	return errA == nil && errB == nil &&
		math.IsNaN(math.Float64frombits(uint64(a))) && math.IsNaN(math.Float64frombits(uint64(b)))
}

// mathSingleProgram writes a program that applies every one-argument
// std::math function to every f32 value and prints the bits of each f32
// answer, widened to f64 so that print has one integer to write.
func mathSingleProgram(values []uint32) string {
	var b strings.Builder
	b.WriteString("import std::float;\nimport std::math;\n\n")
	b.WriteString("fn show(value: f32) -> void {\n")
	b.WriteString("    print(cast<i64>(float::bits(cast<f64>(value))));\n}\n\n")
	b.WriteString("fn main() -> void {\n")
	for i, value := range values {
		bits := math.Float64bits(float64(math.Float32frombits(value)))
		fmt.Fprintf(&b, "    let v%d: f32 = cast<f32>(float::from_bits(cast<u64>(%d) << 32 | %d));\n",
			i, bits>>32, bits&0xFFFFFFFF)
		for _, function := range mathUnaryFunctions {
			if mathIsHelper(function.name) {
				continue
			}
			fmt.Fprintf(&b, "    show(math::%s<f32>(v%d));\n", function.name, i)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// TestMathSingle checks the f32 instantiations against the true values
// rounded once to f32. Each computes in f64 and rounds again, so it is
// within the same allowance the f64 path has.
func TestMathSingle(t *testing.T) {
	ref := loadMathReference(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "math32.kizu")
	if err := os.WriteFile(path, []byte(mathSingleProgram(ref.single)), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runKizuEnv(nil, "run", path)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	line := 0
	for i, value := range ref.single {
		answer := 0
		for _, function := range mathUnaryFunctions {
			if mathIsHelper(function.name) {
				continue
			}
			x := math.Float32frombits(value)
			if line >= len(lines) {
				t.Fatalf("output stops after %d lines", line)
			}
			pattern, err := strconv.ParseInt(lines[line], 10, 64)
			if err != nil {
				t.Fatalf("%s<f32>(%g): printed %q", function.name, x, lines[line])
			}
			got := math.Float32bits(float32(math.Float64frombits(uint64(pattern))))
			want := ref.singleWant[i][answer]
			if !mathMatches(uint64(got), uint64(want), 32, function.ulps) {
				t.Errorf("%s<f32>(%g): got %v, want %v", function.name, x,
					math.Float32frombits(got), math.Float32frombits(want))
			}
			line++
			answer++
		}
	}
}
