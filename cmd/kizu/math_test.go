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

// mathUnaryFunctions lists the one-argument std::math functions beside the Go
// function each is checked against.
var mathUnaryFunctions = []struct {
	name string
	want func(float64) float64
}{
	{"sqrt", math.Sqrt},
	{"floor", math.Floor},
	{"ceil", math.Ceil},
	{"trunc", math.Trunc},
	{"round", math.Round},
	{"abs", math.Abs},
}

// mathBinaryFunctions lists the two-argument std::math functions beside the Go
// function each is checked against. min and max are spelled out because Go's
// math.Min and math.Max answer NaN when either argument is NaN, where
// std::math answers the other argument the way C's fmin and fmax do.
var mathBinaryFunctions = []struct {
	name string
	want func(float64, float64) float64
}{
	{"copysign", math.Copysign},
	{"hypot", math.Hypot},
	{"min", func(a, b float64) float64 {
		switch {
		case math.IsNaN(a):
			return b
		case math.IsNaN(b):
			return a
		}
		return math.Min(a, b)
	}},
	{"max", func(a, b float64) float64 {
		switch {
		case math.IsNaN(a):
			return b
		case math.IsNaN(b):
			return a
		}
		return math.Max(a, b)
	}},
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
			calls = append(calls, fmt.Sprintf("    show(math::%s(%s));\n", function.name, from(pattern)))
		}
	}
	for i := range bits[:len(bits)-1] {
		for _, function := range mathBinaryFunctions {
			calls = append(calls, fmt.Sprintf("    show(math::%s(%s, %s));\n",
				function.name, from(bits[i]), from(bits[i+1])))
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

// mathWant lists the answers Go's math gives in the order mathProgram prints
// them. Every NaN is spelled the same way, because which NaN comes back is not
// part of what any function promises.
func mathWant(bits []uint64) []string {
	spell := func(value float64) string {
		if math.IsNaN(value) {
			return "NaN"
		}
		return strconv.FormatInt(int64(math.Float64bits(value)), 10)
	}
	var want []string
	for _, pattern := range bits {
		for _, function := range mathUnaryFunctions {
			want = append(want, spell(function.want(math.Float64frombits(pattern))))
		}
	}
	for i := range bits[:len(bits)-1] {
		for _, function := range mathBinaryFunctions {
			a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
			want = append(want, spell(function.want(a, b)))
		}
	}
	return want
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
	function := mathBinaryFunctions[line%len(mathBinaryFunctions)]
	i := line / len(mathBinaryFunctions)
	a, b := math.Float64frombits(bits[i]), math.Float64frombits(bits[i+1])
	return fmt.Sprintf("%s(%g, %g)", function.name, a, b)
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
		got := lines[i]
		if pattern, err := strconv.ParseInt(got, 10, 64); err == nil {
			if math.IsNaN(math.Float64frombits(uint64(pattern))) {
				got = "NaN"
			}
		}
		if got == want[i] {
			continue
		}
		failures++
		if failures > 20 {
			continue
		}
		t.Errorf("%s: got %s, want %s", mathLabel(bits, i), got, want[i])
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
