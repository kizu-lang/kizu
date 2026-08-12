package manifest

import "testing"

// TestParseManifest checks the accepted kizu.toml subset.
func TestParseManifest(t *testing.T) {
	source := `[package]
name = "app"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src", "lib"]
exports = ["app", "app::lexer"]
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if manifest.PackageName != "app" || manifest.Version != "0.1.0" {
		t.Fatalf("unexpected package fields: %#v", manifest)
	}
	if manifest.Root != "src/main.kizu" {
		t.Fatalf("got root %q", manifest.Root)
	}
	if len(manifest.Paths) != 2 || manifest.Paths[0] != "src" || manifest.Paths[1] != "lib" {
		t.Fatalf("got paths %#v", manifest.Paths)
	}
	if len(manifest.Exports) != 2 || manifest.Exports[1] != "app::lexer" {
		t.Fatalf("got exports %#v", manifest.Exports)
	}
}

// TestParseManifestRejectsReservedPackageName checks the std namespace guard.
func TestParseManifestRejectsReservedPackageName(t *testing.T) {
	_, err := ParseManifest(`[package]
name = "std"

[modules]
root = "src/main.kizu"
`)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestParseStdManifestAllowsReservedPackageName checks std manifest parsing.
func TestParseStdManifestAllowsReservedPackageName(t *testing.T) {
	manifest, err := ParseStdManifest(`[package]
name = "std"

[modules]
root = "std"
paths = ["src"]
exports = ["std::mem"]
`)
	if err != nil {
		t.Fatalf("parse std manifest failed: %v", err)
	}
	if manifest.PackageName != "std" || manifest.Exports[0] != "std::mem" {
		t.Fatalf("unexpected std manifest %#v", manifest)
	}
}

// TestParseManifestRejectsOutsidePackageExport checks export path ownership.
func TestParseManifestRejectsOutsidePackageExport(t *testing.T) {
	_, err := ParseManifest(`[package]
name = "app"

[modules]
root = "src/main.kizu"
exports = ["other::lexer"]
`)
	if err == nil {
		t.Fatal("expected outside export error")
	}
}
