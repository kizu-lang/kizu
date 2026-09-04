package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// floatTextBits lists the bit patterns std::float::append is checked on:
// the edges of the format, the classic hard cases, and a deterministic
// spread of pseudo-random doubles.
func floatTextBits() []uint64 {
	bits := []uint64{
		0x0000000000000000, 0x8000000000000000, // ±0
		0x0000000000000001, 0x000FFFFFFFFFFFFF, // smallest and largest subnormal
		0x0010000000000000, 0x7FEFFFFFFFFFFFFF, // smallest normal, largest finite
		0x3FF0000000000000, 0x4000000000000000, 0x4008000000000000, // 1, 2, 3
		0x3FB999999999999A, 0x3FD5555555555555, // 0.1, 1/3
		0x44B52D02C7E14AF6, 0x4415AF1D78B58C40, // 1e23, 1e20
		0x4340000000000000, 0x4340000000000001, // 2^53, 2^53 + 2
		0x4330000000000001, 0x3F1A36E2EB1C432D, // 2^52 + 1, 1e-4
		0x3EB0C6F7A0B5ED8D, 0x3E7AD7F29ABCAF48, // 1e-6, 1e-7
		0x4415AF1D78B58C40, 0x444B1AE4D6E2EF50, // 1e20, 1e21
		0x3C9CD2B297D889BC, 0x0000000000000003, // 1e-16, 3 * 5e-324
	}
	// A linear congruential generator keeps the spread the same on every run.
	state := uint64(0x9E3779B97F4A7C15)
	for len(bits) < 1200 {
		state = state*6364136223846793005 + 1442695040888963407
		candidate := state
		exponent := (candidate >> 52) & 0x7FF
		if exponent == 0x7FF {
			continue
		}
		bits = append(bits, candidate)
	}
	return bits
}

// floatTextParseCases lists decimal spellings whose nearest double is easy to
// get wrong: ties, values a hair from a tie, the subnormal edge, and inputs
// longer than any fast path.
func floatTextParseCases() []string {
	return []string{
		"0.1", "1e23", "8.98846567431158e307", "2.2250738585072011e-308",
		"2.2250738585072012e-308", "2.2250738585072014e-308",
		"0.500000000000000166533453693773481063544750213623046875",
		"9007199254740993", "9007199254740993.00000000000000001",
		"9007199254740992.999999999999999", "1e-400", "4.9e-324",
		"2.4703282292062327e-324", "2.4703282292062328e-324",
		"1.7976931348623158e308", "1.7976931348623157e308",
		"0.000000000000000000000000000000000000000000000000000000000000000000000001",
		"123456789012345678901234567890123456789012345678901234567890e-30",
		"1" + strings.Repeat("0", 300), "0." + strings.Repeat("0", 300) + "1",
		"3.14159265358979323846264338327950288419716939937510582097494459",
		"100", "1e21", "1E+2", "-0", "+7.5", "1_000",
	}
}

// expectedFloatText renders a double the way std::float::append promises,
// from the shortest digits Go's strconv produces.
func expectedFloatText(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "inf"
	}
	if math.IsInf(value, -1) {
		return "-inf"
	}
	sign := ""
	if math.Signbit(value) {
		sign = "-"
		value = -value
	}
	if value == 0 {
		return sign + "0.0"
	}
	text := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(text, "e")
	exponent, _ := strconv.Atoi(exponentText)
	digits := strings.ReplaceAll(mantissa, ".", "")
	point := exponent + 1
	switch {
	case point > 0 && point <= 21:
		if len(digits) <= point {
			return sign + digits + strings.Repeat("0", point-len(digits)) + ".0"
		}
		return sign + digits[:point] + "." + digits[point:]
	case point <= 0 && point > -6:
		return sign + "0." + strings.Repeat("0", -point) + digits
	default:
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		return sign + out + "e" + strconv.Itoa(point-1)
	}
}

// floatTextProgram writes the Kizu program that prints, for every bit
// pattern, its std::float::append spelling and whether parsing that spelling
// gives the pattern back, and then the bits every parse case reads to.
func floatTextProgram(bits []uint64, parses []string) string {
	var source strings.Builder
	source.WriteString("import std::float;\nimport std::mem;\nimport std::string;\n\n")
	source.WriteString("fn show(allocator: Allocator, raw: u64) -> !void {\n")
	source.WriteString("    let value = float::from_bits(raw);\n")
	source.WriteString("    var text = string::new(allocator);\n")
	source.WriteString("    defer text.deinit(allocator);\n")
	source.WriteString("    try float::append(allocator, &var text, value);\n")
	source.WriteString("    let bytes = text.as_bytes();\n    print(bytes);\n")
	source.WriteString("    if try float::parse(allocator, bytes) |back| {\n")
	source.WriteString("        print(float::bits(back) == raw);\n")
	source.WriteString("    } else {\n        print(false);\n    }\n    return;\n}\n\n")
	source.WriteString("fn read(allocator: Allocator, text: []u8) -> !void {\n")
	source.WriteString("    if try float::parse(allocator, text) |value| {\n")
	source.WriteString("        print(cast<i64>(float::bits(value)));\n")
	source.WriteString("    } else {\n        print(\"null\");\n    }\n    return;\n}\n\n")
	source.WriteString("fn main() -> !void {\n    let allocator = mem::page_allocator();\n")
	for _, raw := range bits {
		fmt.Fprintf(&source, "    try show(allocator, cast<u64>(%d) << 32 | %d);\n",
			raw>>32, raw&0xFFFFFFFF)
	}
	for _, text := range parses {
		fmt.Fprintf(&source, "    try read(allocator, %q);\n", text)
	}
	source.WriteString("    return;\n}\n")
	return source.String()
}

// floatTextWant lists the lines the program prints when std::float agrees
// with strconv.
func floatTextWant(bits []uint64, parses []string) []string {
	want := make([]string, 0, 2*len(bits)+len(parses))
	for _, raw := range bits {
		want = append(want, expectedFloatText(math.Float64frombits(raw)), "true")
	}
	for _, text := range parses {
		value, parseErr := strconv.ParseFloat(text, 64)
		if strings.Contains(text, "_") || (parseErr != nil && !math.IsInf(value, 0)) {
			want = append(want, "null")
			continue
		}
		want = append(want, strconv.FormatInt(int64(math.Float64bits(value)), 10))
	}
	return want
}

// TestFloatText compares std::float::append and std::float::parse with Go's
// strconv on the same values: the Go seed is the oracle for the std
// conversion, the way it is for the compiler.
func TestFloatText(t *testing.T) {
	bits := floatTextBits()
	parses := floatTextParseCases()
	dir := t.TempDir()
	path := filepath.Join(dir, "float_text.kizu")
	if err := os.WriteFile(path, []byte(floatTextProgram(bits, parses)), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runKizuEnv(nil, "run", path)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	want := floatTextWant(bits, parses)
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d\n%s", len(lines), len(want), output)
	}
	failures := 0
	for i := range want {
		if lines[i] == want[i] {
			continue
		}
		failures++
		if failures > 20 {
			continue
		}
		label := fmt.Sprintf("parse %q", parses[max(i-2*len(bits), 0)])
		if i < 2*len(bits) {
			label = fmt.Sprintf("bits 0x%016X", bits[i/2])
		}
		t.Errorf("%s: got %q, want %q", label, lines[i], want[i])
	}
	if failures > 20 {
		t.Errorf("%d more mismatches", failures-20)
	}
}
