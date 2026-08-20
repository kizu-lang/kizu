package source

import "testing"

// TestMapKeepsFilesIndependent checks handles keep their own path and text as the map grows.
func TestMapKeepsFilesIndependent(t *testing.T) {
	sources := NewMap()
	main := sources.Add("src/main.kizu", "fn main() {}")
	helper := sources.Add("src/helper.kizu", "pub fn helper() {}")

	if sources.Len() != 2 {
		t.Fatalf("len = %d, want 2", sources.Len())
	}
	if main.Path() != "src/main.kizu" || main.Text() != "fn main() {}" {
		t.Fatalf("main = (%q, %q)", main.Path(), main.Text())
	}
	if helper.Path() != "src/helper.kizu" || helper.Text() != "pub fn helper() {}" {
		t.Fatalf("helper = (%q, %q)", helper.Path(), helper.Text())
	}
}

// TestZeroIDNamesNoSource checks optional source locations use the ID zero value.
func TestZeroIDNamesNoSource(t *testing.T) {
	var id ID
	if !id.IsZero() || id.Path() != "" || id.Text() != "" {
		t.Fatalf("zero ID resolved to (%q, %q)", id.Path(), id.Text())
	}
}
