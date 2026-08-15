package lsp

import "testing"

// implementationFixture defines a contract and one implementing type.
//
//	0  struct File {
//	1      name: []u8,
//	2  }
//	3
//	4  contract Writer {
//	5      fn write() -> !i64;
//	6  }
//	7
//	8  fn (self: &File) write() -> !i64 {
//	9      return 2;
//	10 }
func implementationFixture() string {
	return "struct File {\n" +
		"    name: []u8,\n" +
		"}\n" +
		"\n" +
		"contract Writer {\n" +
		"    fn write() -> !i64;\n" +
		"}\n" +
		"\n" +
		"fn (self: &File) write() -> !i64 {\n" +
		"    return 2;\n" +
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

	pos := positionIn(source, "fn write() -> !i64;", "write")
	locations := server.implementation(uri, pos)
	if len(locations) != 1 {
		t.Fatalf("implementation = %#v, want one method location", locations)
	}
	if got := textIn(source, locations[0].Range); got != "write" {
		t.Fatalf("implementation range covers %q, want %q", got, "write")
	}
	// The match must be the method on File (line 8), not the contract decl (line 5).
	if locations[0].Range.Start.Line != 8 {
		t.Fatalf("implementation method line = %d, want 8", locations[0].Range.Start.Line)
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
