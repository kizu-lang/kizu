package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

func TestSelfhostTypeSpellingAndConstructorRegressionGates(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}

	successCases := []struct {
		entry string
		want  string
	}{
		{
			entry: "selfhost::types_oracle::constructor_resolution_regression_gate",
			want:  "constructor-resolution-regression-ok\n",
		},
		{
			entry: "selfhost::types_oracle::nested_type_spelling_regression_gate",
			want:  "nested-type-spelling-regression-ok\n",
		},
		{
			entry: "selfhost::types_oracle::unknown_owner_field_regression_gate",
			want:  "unknown-owner-field-regression-ok\n",
		},
		{
			entry: "selfhost::types::expression_infer::gate_qualified_generic_method_result",
			want:  "resolver::BindingKind\n",
		},
	}
	for _, tc := range successCases {
		t.Run(tc.entry, func(t *testing.T) {
			var out bytes.Buffer
			if err := interp.New(&out).RunEntry(program, tc.entry); err != nil {
				t.Fatalf("gate failed: %v\n%s", err, out.String())
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}

	errorCases := []struct {
		entry string
		want  string
	}{
		{
			entry: "selfhost::types_oracle::malformed_empty_type_arguments_gate",
			want:  "generic type arguments missing",
		},
		{
			entry: "selfhost::types_oracle::malformed_trailing_type_argument_gate",
			want:  "trailing type argument separator",
		},
		{
			entry: "selfhost::types_oracle::malformed_unbalanced_type_gate",
			want:  "unbalanced type delimiters",
		},
		{
			entry: "selfhost::types_oracle::malformed_nested_empty_type_gate",
			want:  "generic type arguments missing",
		},
	}
	for _, tc := range errorCases {
		t.Run(tc.entry, func(t *testing.T) {
			err := interp.New(&bytes.Buffer{}).RunEntry(program, tc.entry)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSelfhostFieldInferenceHasNoUnknownOwnerNameFallback(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/types/expression_infer.kizu")
	if strings.Contains(content, "fn field_name_type(") {
		t.Fatal("field inference retained a global field-name type fallback")
	}
	if !strings.Contains(content, "let struct_field = try struct_field_declared_type(") {
		t.Fatal("field inference does not consult exact declared field facts")
	}
}

func TestSelfhostTypeConstructorUsesIntrinsicRegistry(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/type_constructor.kizu")
	required := []string{
		"intrinsic_type_contract::from_source_name(name)",
		"intrinsic_type_contract::arity(kind) > 0",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("type constructor registry missing %q", fragment)
		}
	}
	forbidden := []string{
		"std::array::Array",
		"std::map::Map",
		"std::channel::Channel",
		"std::mem::Box",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("type constructor registry hardcodes %q", fragment)
		}
	}
}
