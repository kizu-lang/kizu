package llvm

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	typpkg "github.com/kizu-lang/kizu/internal/typ"
)

// cabiStructs are C structs of every shape the conventions tell apart.
func cabiStructs() map[string]ir.Struct {
	fields := func(types ...string) ir.Struct {
		st := ir.Struct{}
		for index, typ := range types {
			st.Fields = append(st.Fields, ir.Field{Name: string(rune('a' + index)), Type: typ})
		}
		return st
	}
	return map[string]ir.Struct{
		"O16": fields("u16", "u16", "u64"),
		"I4":  fields("i32"),
		"B3":  fields("u8", "u8", "u8"),
		"I12": fields("i32", "i32", "i32"),
		"F2":  fields("f32", "f32"),
		"F3":  fields("f32", "f32", "f32"),
		"D2":  fields("f64", "f64"),
		"D4":  fields("f64", "f64", "f64", "f64"),
		"L3":  fields("i64", "i64", "i64"),
		"DI":  fields("f64", "i32"),
		"FID": fields("f32", "i32", "f64"),
		"BP":  fields("bool", "ptr<u8>"),
		"NF":  fields("F2", "f32"),
	}
}

// cabiEmitter is an emitter over cabiStructs for one target.
func cabiEmitter(darwin bool, arch Arch) *emitter {
	return &emitter{
		module: &ir.Module{Structs: cabiStructs()},
		types:  typpkg.NewTable(),
		darwin: darwin,
		arch:   arch,
	}
}

// TestCStructSignatures pins how each struct shape is passed and returned on
// both conventions. The expectations are clang's declarations of the same C
// structs; where they differ, the difference is noted and moves the same
// bytes through the same registers.
func TestCStructSignatures(t *testing.T) {
	tests := []struct {
		name   string
		arm64  string
		linux  string
		x86_64 string
	}{
		{"O16", "[2 x i64]", "[2 x i64]", "i64, i64"},
		// clang returns I4 and B3 as i32 / i24 on arm64; the word is x0 either way.
		{"I4", "i64", "i64", "i32"},
		{"B3", "i64", "i64", "i24"},
		{"I12", "[2 x i64]", "[2 x i64]", "i64, i32"},
		{"F2", "[2 x float]", "[2 x float] alignstack(8)", "<2 x float>"},
		{"F3", "[3 x float]", "[3 x float] alignstack(8)", "<2 x float>, float"},
		{"D2", "[2 x double]", "[2 x double] alignstack(8)", "double, double"},
		{"D4", "[4 x double]", "[4 x double] alignstack(8)", "ptr byval(%kizu.struct.D4) align 8"},
		{"L3", "ptr", "ptr", "ptr byval(%kizu.struct.L3) align 8"},
		// clang loads DI's second eightbyte as i32 and BP's first as i8; the
		// register is the same.
		{"DI", "[2 x i64]", "[2 x i64]", "double, i64"},
		{"FID", "[2 x i64]", "[2 x i64]", "i64, double"},
		{"BP", "[2 x i64]", "[2 x i64]", "i64, i64"},
		{"NF", "[3 x float]", "[3 x float] alignstack(8)", "<2 x float>, float"},
	}
	targets := []struct {
		darwin bool
		arch   Arch
		want   func(int) string
	}{
		{true, ArchArm64, func(i int) string { return tests[i].arm64 }},
		{false, ArchArm64, func(i int) string { return tests[i].linux }},
		{false, ArchX86_64, func(i int) string { return tests[i].x86_64 }},
	}
	for _, target := range targets {
		e := cabiEmitter(target.darwin, target.arch)
		for index, tt := range tests {
			result, params := e.cCallSignature("void", []cArg{{typ: tt.name, byValue: true}})
			if result != "void" {
				t.Fatalf("%s: result %q", tt.name, result)
			}
			if got := strings.Join(params, ", "); got != target.want(index) {
				t.Errorf("darwin=%v arch=%d %s: got %q, want %q",
					target.darwin, target.arch, tt.name, got, target.want(index))
			}
		}
	}
}

// TestCStructReturns pins the result each struct shape comes back as.
func TestCStructReturns(t *testing.T) {
	tests := []struct {
		name   string
		arm64  string
		x86_64 string
	}{
		{"O16", "[2 x i64]", "{ i64, i64 }"},
		{"I12", "[2 x i64]", "{ i64, i32 }"},
		{"F2", "{ float, float }", "<2 x float>"},
		{"F3", "{ float, float, float }", "{ <2 x float>, float }"},
		{"D2", "{ double, double }", "{ double, double }"},
		{"D4", "{ double, double, double, double }", "void"},
		{"L3", "void", "void"},
		{"FID", "[2 x i64]", "{ i64, double }"},
		{"NF", "{ float, float, float }", "{ <2 x float>, float }"},
	}
	for _, arch := range []Arch{ArchArm64, ArchX86_64} {
		e := cabiEmitter(false, arch)
		for _, tt := range tests {
			want := tt.arm64
			if arch == ArchX86_64 {
				want = tt.x86_64
			}
			result, params := e.cCallSignature(tt.name, nil)
			if result != want {
				t.Errorf("arch=%d %s: got %q, want %q", arch, tt.name, result, want)
			}
			sret := len(params) == 1 && strings.HasPrefix(params[0], "ptr sret(")
			if sret != (want == "void") {
				t.Errorf("arch=%d %s: params %q", arch, tt.name, params)
			}
		}
	}
}

// TestCStructOutOfRegisters passes a struct in memory on x86-64 once the
// integer registers it needs are taken, as clang does, where AAPCS64 leaves
// the same choice to the backend.
func TestCStructOutOfRegisters(t *testing.T) {
	e := cabiEmitter(false, ArchX86_64)
	args := []cArg{}
	for range 5 {
		args = append(args, cArg{typ: "i64", param: "i64"})
	}
	args = append(args, cArg{typ: "O16", byValue: true}, cArg{typ: "D2", byValue: true})
	_, params := e.cCallSignature("void", args)
	got := strings.Join(params[5:], ", ")
	want := "ptr byval(%kizu.struct.O16) align 8, double, double"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
