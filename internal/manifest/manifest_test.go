package manifest

import (
	"strings"
	"testing"
)

// TestParseManifest checks the accepted kizu.toml subset.
func TestParseManifest(t *testing.T) {
	source := `[package]
name = "app"
version = "0.1.0"

[modules]
paths = ["src", "lib"]
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if manifest.PackageName != "app" || manifest.Version != "0.1.0" {
		t.Fatalf("unexpected package fields: %#v", manifest)
	}
	if len(manifest.Paths) != 2 || manifest.Paths[0] != "src" || manifest.Paths[1] != "lib" {
		t.Fatalf("got paths %#v", manifest.Paths)
	}
}

// TestParseManifestRejectsRemovedRoot keeps module identity directory-derived.
func TestParseManifestRejectsRemovedRoot(t *testing.T) {
	_, err := ParseManifest(`[package]
name = "app"

[modules]
root = "src/main.kizu"
`)
	if err == nil {
		t.Fatal("removed modules.root was accepted")
	}
}

// TestParseManifestRejectsReservedPackageName checks the std namespace guard.
func TestParseManifestRejectsReservedPackageName(t *testing.T) {
	_, err := ParseManifest(`[package]
name = "std"
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
paths = ["src"]
`)
	if err != nil {
		t.Fatalf("parse std manifest failed: %v", err)
	}
	if manifest.PackageName != "std" || manifest.Paths[0] != "src" {
		t.Fatalf("unexpected std manifest %#v", manifest)
	}
}

// TestParseManifestNativeSearchPaths reads the `[native]` directories the
// linker is told to look in for what `@link_lib` / `@link_framework` name.
func TestParseManifestNativeSearchPaths(t *testing.T) {
	source := `[package]
name = "app"

[native]
library_search = ["/opt/homebrew/lib", "vendor/lib"]
framework_search = ["vendor/frameworks"]
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(manifest.LibrarySearch) != 2 || manifest.LibrarySearch[1] != "vendor/lib" {
		t.Fatalf("got library search %#v", manifest.LibrarySearch)
	}
	if len(manifest.FrameworkSearch) != 1 || manifest.FrameworkSearch[0] != "vendor/frameworks" {
		t.Fatalf("got framework search %#v", manifest.FrameworkSearch)
	}
}

// TestParseManifestRejectsUnknownNativeKey keeps the `[native]` section to
// the search paths: a library name belongs beside its extern declaration.
func TestParseManifestRejectsUnknownNativeKey(t *testing.T) {
	_, err := ParseManifest(`[package]
name = "app"

[native]
libraries = ["m"]
`)
	if err == nil || !strings.Contains(err.Error(), "unsupported key `native.libraries`") {
		t.Fatalf("expected unsupported key error, got %v", err)
	}
}
