package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// floatTextReference is what std::float is checked against: the bit
// patterns append is given with the text it has to write, and the decimal
// spellings parse is given with the bits it has to read them to (as a signed
// integer, or "null" for a spelling that is not a number). The file is
// written by testdata/float_text.py from Python's repr and float, which are
// David Gay's shortest round-trip and correctly rounded conversions; how the
// digits are laid out is docs/std/float.md.
type floatTextReference struct {
	bits    []uint64
	spelled []string
	parses  []string
	read    []string
}

// loadFloatTextReference reads testdata/float_text.txt.
func loadFloatTextReference(t *testing.T) floatTextReference {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "float_text.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ref floatTextReference
	for number, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("float_text.txt:%d: want three fields, got %q", number+1, line)
		}
		switch fields[0] {
		case "append":
			raw, err := strconv.ParseUint(fields[1], 16, 64)
			if err != nil {
				t.Fatalf("float_text.txt:%d: %v", number+1, err)
			}
			ref.bits = append(ref.bits, raw)
			ref.spelled = append(ref.spelled, fields[2])
		case "parse":
			ref.parses = append(ref.parses, fields[1])
			ref.read = append(ref.read, fields[2])
		default:
			t.Fatalf("float_text.txt:%d: unknown kind %q", number+1, fields[0])
		}
	}
	return ref
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

// want lists the lines the program prints when std::float agrees with the
// reference.
func (ref floatTextReference) want() []string {
	want := make([]string, 0, 2*len(ref.bits)+len(ref.parses))
	for _, text := range ref.spelled {
		want = append(want, text, "true")
	}
	return append(want, ref.read...)
}

// TestFloatText checks std::float::append and std::float::parse against
// the reference in testdata/float_text.txt.
func TestFloatText(t *testing.T) {
	ref := loadFloatTextReference(t)
	bits, parses := ref.bits, ref.parses
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
	want := ref.want()
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

// TestFloatTextWASM runs the same std::float check on the wasm32-wasi target,
// so the float instructions of the wasm backend and its binary encoder answer
// exactly what the native target does.
func TestFloatTextWASM(t *testing.T) {
	wasmtime, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime is required for binary execution")
	}
	ref := loadFloatTextReference(t)
	bits, parses := ref.bits, ref.parses
	dir := t.TempDir()
	source := filepath.Join(dir, "float_text.kizu")
	if err := os.WriteFile(source, []byte(floatTextProgram(bits, parses)), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, "float_text.wasm")
	build, err := kizuCommand("build", "--target", "wasm32-wasi", "--emit", "wasm",
		"-o", artifact, source).CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, build)
	}
	output, err := exec.Command(wasmtime, "run", artifact).CombinedOutput()
	if err != nil {
		t.Fatalf("wasmtime failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	want := ref.want()
	if len(lines) != len(want) {
		for index := range want {
			if index >= len(lines) || lines[index] != want[index] {
				t.Fatalf("got %d lines, want %d; first difference at line %d: got %q, want %q",
					len(lines), len(want), index, lines[min(index, len(lines)-1)], want[index])
			}
		}
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want[i])
		}
	}
}
