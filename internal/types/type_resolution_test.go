package types

import (
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
)

// TestTypeResolutionReturnsCopyIssues keeps semantic type failures below the
// checker diagnostic boundary.
func TestTypeResolutionReturnsCopyIssues(t *testing.T) {
	checker := New()
	checker.declaredTypes["User"] = true
	checker.declaredTypes["Other"] = true
	checker.declaredTypes["Pair"] = true
	checker.structs["Pair"] = &ast.StructDecl{
		Name: "Pair", TypeParams: []string{"T", "U"},
	}
	checker.errorSets["Problem"] = &errorSetType{name: "Problem"}

	for _, tc := range []struct {
		name    string
		text    string
		kind    typeResolutionIssueKind
		subject Type
	}{
		{name: "unknown name", text: "Missing", kind: typeResolutionUnknown, subject: "Missing"},
		{
			name: "unknown typed error", text: "Missing!bool",
			kind: typeResolutionUnknown, subject: "Missing",
		},
		{
			name: "wrapper over optional", text: "[]?i64",
			kind: typeResolutionWrapsOptional, subject: "[]?i64",
		},
		{
			name: "typed error needs a set", text: "i64!bool",
			kind: typeResolutionErrorSetRequired, subject: "i64",
		},
		{
			name: "optional static argument", text: "std::array::Array<?i64>",
			kind: typeResolutionOptionalStaticArg, subject: "?i64",
		},
		{
			name: "optional pointer static argument", text: "?ptr<?i64>",
			kind: typeResolutionOptionalStaticArg, subject: "?i64",
		},
		{
			name: "single generic arity", text: "std::array::Array<i64, bool>",
			kind: typeResolutionSingleGenericArity, subject: "std::array::Array",
		},
		{name: "map arity", text: "std::map::Map<[]u8>", kind: typeResolutionMapArity},
		{name: "map key", text: "std::map::Map<f64, bool>", kind: typeResolutionMapKey},
		{
			name: "unknown generic", text: "Mystery<i64>",
			kind: typeResolutionUnknownGeneric, subject: "Mystery",
		},
		{
			name: "user generic arity", text: "Pair<i64>",
			kind: typeResolutionUserGenericArity, subject: "Pair",
		},
		{name: "nested optional", text: "??i64", kind: typeResolutionOptionalOptional, subject: "?i64"},
		{
			name: "optional over error union", text: "?!i64",
			kind: typeResolutionOptionalErrorUnion, subject: "!i64",
		},
		{
			name: "element of scalar", text: "std::meta::element<i64>",
			kind: typeResolutionMetaElementUnsupported, subject: "i64",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, issue := checker.resolveType(tc.text)
			if value != "" || !issue.present() || issue.kind != tc.kind ||
				issue.subject != tc.subject {
				t.Fatalf("value = %q, issue = %#v", value, issue)
			}
		})
	}
}

// TestTypeResolutionRetainsValuesAndDefersOnlyUnboundCaptures pins the success
// side of the same copy result.
func TestTypeResolutionRetainsValuesAndDefersOnlyUnboundCaptures(t *testing.T) {
	checker := New()
	checker.declaredTypes["User"] = true
	checker.declaredTypes["Other"] = true
	checker.errorSets["Problem"] = &errorSetType{name: "Problem"}

	for _, text := range []string{
		"i64",
		"[]i64",
		"std::map::Map<[]u8, i64>",
		"?ptr<const i64>",
		"Problem!?i64",
		"std::meta::field_type<User, f>",
	} {
		value, issue := checker.resolveType(text)
		if issue.present() || value != Type(text) {
			t.Fatalf("resolveType(%q) = %q, %#v", text, value, issue)
		}
	}

	for _, tc := range []struct {
		text string
		want Type
	}{
		{text: "std::meta::element<?i64>", want: "i64"},
		{text: "std::meta::element<std::array::Array<i64>>", want: "i64"},
		{text: "std::meta::element<std::mem::Box<i64>>", want: "i64"},
		{text: "std::meta::element<std::map::Map<[]u8, i64>>", want: "i64"},
	} {
		value, issue := checker.resolveType(tc.text)
		if issue.present() || value != tc.want {
			t.Fatalf("resolveType(%q) = %q, %#v", tc.text, value, issue)
		}
	}

	checker.metaFields["f"] = metaField{
		owner: "User", name: "value", typ: "i64",
	}
	value, issue := checker.resolveType("std::meta::field_type<User, f>")
	if issue.present() || value != typeI64 {
		t.Fatalf("bound field type = %q, %#v", value, issue)
	}
	checker.metaFields["g"] = metaField{
		owner: "User", name: "nested", typ: typeI64,
	}
	checker.metaFields["f"] = metaField{
		owner: "User", name: "value",
		typ: "std::meta::field_type<User, g>",
	}
	value, issue = checker.resolveType("std::meta::field_type<User, f>")
	if issue.present() || value != typeI64 {
		t.Fatalf("nested bound field type = %q, %#v", value, issue)
	}

	_, issue = checker.resolveType("std::meta::field_type<Other, f>")
	if issue.kind != typeResolutionMetaCaptureOwner ||
		issue.subject != "f" || issue.related != "Other" || issue.field.owner != "User" {
		t.Fatalf("owner mismatch issue = %#v", issue)
	}
}

// TestTypeResolutionNodeReportsMissing pins the parser-node entry's nil case.
func TestTypeResolutionNodeReportsMissing(t *testing.T) {
	checker := New()
	value, issue := checker.resolveTypeNode(nil)
	if value != "" || issue.kind != typeResolutionMissing {
		t.Fatalf("resolveTypeNode(nil) = %q, %#v", value, issue)
	}
}
