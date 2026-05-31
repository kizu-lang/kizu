package lsp

import "testing"

// TestTypeDefinitionJumpsToStruct checks a local binding resolves to the
// declaration of its struct type.
func TestTypeDefinitionJumpsToStruct(t *testing.T) {
	uri := "file:///main.kizu"
	source := navigationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	locations := server.typeDefinition(uri, positionIn(source, "let trace = Trace", "trace"))
	if len(locations) != 1 {
		t.Fatalf("typeDefinition = %#v, want one location", locations)
	}
	if locations[0].URI != uri {
		t.Fatalf("typeDefinition uri = %q, want %q", locations[0].URI, uri)
	}
	if got := textIn(source, locations[0].Range); got != "Trace" {
		t.Fatalf("typeDefinition range covers %q, want %q", got, "Trace")
	}
}

// TestTypeDefinitionParameterType checks a parameter resolves to its type decl.
func TestTypeDefinitionParameterType(t *testing.T) {
	uri := "file:///main.kizu"
	source := navigationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	// inspect's parameter is i64, a builtin with no declaration: expect no jump.
	locations := server.typeDefinition(uri, positionIn(source, "value: i64", "value"))
	if len(locations) != 0 {
		t.Fatalf("typeDefinition on builtin type = %#v, want empty", locations)
	}
}

// TestTypeDefinitionUnknownDocument checks unknown docs yield an empty slice.
func TestTypeDefinitionUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if locations := server.typeDefinition("file:///missing.kizu", Position{}); len(locations) != 0 {
		t.Fatalf("typeDefinition on missing doc = %#v, want empty", locations)
	}
}

// TestBaseTypeNameReducesWrappers checks the type-name reduction rules.
func TestBaseTypeNameReducesWrappers(t *testing.T) {
	cases := map[string]string{
		"Trace":             "Trace",
		"&Trace":            "Trace",
		"&var Trace":        "Trace",
		"?Trace":            "Trace",
		"!Trace":            "Trace",
		"[]Trace":           "Trace",
		"&var []Trace":      "Trace",
		"std::pkg::Trace":   "Trace",
		"[]std::pkg::Trace": "Trace",
	}
	for input, want := range cases {
		if got := baseTypeName(input); got != want {
			t.Errorf("baseTypeName(%q) = %q, want %q", input, got, want)
		}
	}
}
