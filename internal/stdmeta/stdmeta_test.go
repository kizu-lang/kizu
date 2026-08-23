package stdmeta

import "testing"

// TestResolveElementTypeForms distinguishes a closed semantic type from a
// form that must remain deferred until a type parameter is bound.
func TestResolveElementTypeForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "array",
			in:   "std::meta::element<std::array::Array<i64>>",
			want: "i64",
		},
		{
			name: "nested wrapper",
			in:   "!std::array::Array<std::meta::element<std::mem::Box<i64>>>",
			want: "!std::array::Array<i64>",
		},
		{
			name: "nested element",
			in: "std::meta::element<std::meta::element<" +
				"std::array::Array<std::array::Array<i64>>>>",
			want: "i64",
		},
		{
			name: "unbound container",
			in:   "std::meta::element<T>",
			want: "std::meta::element<T>",
		},
		{
			name: "invalid map key",
			in:   "std::meta::element<std::map::Map<i64, bool>>",
			want: "std::meta::element<std::map::Map<i64, bool>>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveElementTypeForms(tc.in); got != tc.want {
				t.Fatalf("ResolveElementTypeForms(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
