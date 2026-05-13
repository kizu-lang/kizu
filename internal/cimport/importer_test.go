package cimport

import (
	"strings"
	"testing"
)

// TestImportFunctionPrototypes checks supported C prototypes become Kizu externs.
func TestImportFunctionPrototypes(t *testing.T) {
	header := `
// Simple libc-like functions.
int puts(const char *s);
void write_byte(unsigned char *p, unsigned char value);
size_t len(const uint8_t *data, size_t n);
`
	got, err := Import(header)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	want := strings.Join([]string{
		`extern "c" fn len(data: ptr<const u8>, n: usize) -> usize`,
		`extern "c" fn puts(s: ptr<const i8>) -> i32`,
		`extern "c" fn write_byte(p: ptr<u8>, value: u8) -> void`,
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestImportGeneratesNamesForUnnamedParameters checks nameless C parameters.
func TestImportGeneratesNamesForUnnamedParameters(t *testing.T) {
	got, err := Import(`int read_i32(const int32_t *);`)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	want := `extern "c" fn read_i32(p1: ptr<const i32>) -> i32`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestImportRejectsUnsupportedSyntax checks readable unsupported feature errors.
func TestImportRejectsUnsupportedSyntax(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{name: "macro", header: `#define X 1`, want: "preprocessor directives are unsupported"},
		{name: "typedef", header: `typedef int my_int;`, want: "typedef is unsupported"},
		{name: "variadic", header: `int printf(const char *fmt, ...);`, want: "variadic"},
		{name: "array", header: `void fill(char buf[8]);`, want: "arrays are unsupported"},
		{name: "unknown type", header: `widget make_widget(void);`, want: "unsupported C type"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Import(tt.header)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}
