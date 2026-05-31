package lsp

import "testing"

// implementationFixture defines a contract and one implementing type.
//
//	0  struct File {
//	1      name: []u8,
//	2  }
//	3
//	4  contract Writer {
//	5      fn write(self: &Self) -> !i64;
//	6  }
//	7
//	8  impl Writer for File {
//	9      fn write(self: &Self) -> !i64 {
//	10         return 2;
//	11     }
//	12 }
func implementationFixture() string {
	return "struct File {\n" +
		"    name: []u8,\n" +
		"}\n" +
		"\n" +
		"contract Writer {\n" +
		"    fn write(self: &Self) -> !i64;\n" +
		"}\n" +
		"\n" +
		"impl Writer for File {\n" +
		"    fn write(self: &Self) -> !i64 {\n" +
		"        return 2;\n" +
		"    }\n" +
		"}\n"
}

// TestImplementationFromContractName jumps to the implementing type.
func TestImplementationFromContractName(t *testing.T) {
	uri := "file:///main.kizu"
	source := implementationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	locations := server.implementation(uri, positionIn(source, "contract Writer", "Writer"))
	if len(locations) != 1 {
		t.Fatalf("implementation = %#v, want one location", locations)
	}
	if got := textIn(source, locations[0].Range); got != "File" {
		t.Fatalf("implementation range covers %q, want %q", got, "File")
	}
}

// TestImplementationFromContractMethod jumps to the implementing method.
func TestImplementationFromContractMethod(t *testing.T) {
	uri := "file:///main.kizu"
	source := implementationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	pos := positionIn(source, "fn write(self: &Self) -> !i64;", "write")
	locations := server.implementation(uri, pos)
	if len(locations) != 1 {
		t.Fatalf("implementation = %#v, want one method location", locations)
	}
	if got := textIn(source, locations[0].Range); got != "write" {
		t.Fatalf("implementation range covers %q, want %q", got, "write")
	}
	// The match must be the impl method (line 9), not the contract decl (line 5).
	if locations[0].Range.Start.Line != 9 {
		t.Fatalf("implementation method line = %d, want 9", locations[0].Range.Start.Line)
	}
}

// TestImplementationNonContract returns nothing for a plain identifier.
func TestImplementationNonContract(t *testing.T) {
	uri := "file:///main.kizu"
	source := implementationFixture()
	server := NewServer(nil, nil)
	server.documents[uri] = source

	locations := server.implementation(uri, positionIn(source, "struct File", "File"))
	if len(locations) != 0 {
		t.Fatalf("implementation on struct = %#v, want empty", locations)
	}
}

// TestImplementationUnknownDocument returns an empty slice.
func TestImplementationUnknownDocument(t *testing.T) {
	server := NewServer(nil, nil)
	if locations := server.implementation("file:///missing.kizu", Position{}); len(locations) != 0 {
		t.Fatalf("implementation on missing doc = %#v, want empty", locations)
	}
}
