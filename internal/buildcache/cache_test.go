package buildcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestContractSnapshotCoversSwitchBoundary checks the self-host switch oracle.
func TestContractSnapshotCoversSwitchBoundary(t *testing.T) {
	lines := strings.Join(ContractSnapshotLines(), "\n")
	required := []string{
		"owner\ngo",
		"switch\nblocked",
		"blocked until\nfilesystem APIs",
		"blocked until\nhashing APIs",
		"blocked until\nmodule graph APIs",
		"blocked until\nartifact layout APIs",
		"compiler version\n" + Version,
		"default max bytes\n" + strconv.FormatInt(DefaultMaxBytes, 10),
		"expected\ncache hit",
		"expected\ncache miss: source changed",
		"expected\ncache miss: source changed without public interface change",
		"expected\ncache miss: public interface changed",
		"expected\ncache miss: manifest changed",
		"expected\ncache miss: module graph changed",
		"expected\ncache miss: stdlib changed",
		"status\nreports size and entries",
		"prune\nremoves entries predictably",
	}
	for _, want := range required {
		if !strings.Contains(lines, want) {
			t.Fatalf("cache contract is missing %q", want)
		}
	}
}

// TestGetOrBuildCachesNoOpRebuild checks that identical inputs hit cache.
func TestGetOrBuildCachesNoOpRebuild(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	source := writeTempSource(t, `fn main() { print("hello"); }`)
	builds := 0
	build := func() (string, error) {
		builds++
		return "artifact", nil
	}
	first, err := cache.GetOrBuild(source, "emit-llvm", build)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	second, err := cache.GetOrBuild(source, "emit-llvm", build)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}
	if first.Hit || !second.Hit || builds != 1 {
		t.Fatalf("got first hit=%v second hit=%v builds=%d", first.Hit, second.Hit, builds)
	}
}

// TestWhyRebuildExplainsChangedSource checks rebuild reasons for edits.
func TestWhyRebuildExplainsChangedSource(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	source := writeTempSource(t, `fn main() { print("hello"); }`)
	_, err := cache.GetOrBuild(source, "emit-llvm", func() (string, error) {
		return "artifact", nil
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	hit, err := cache.WhyRebuild(source, "emit-llvm")
	if err != nil {
		t.Fatalf("why hit failed: %v", err)
	}
	if !strings.Contains(hit, "cache hit") {
		t.Fatalf("got %q", hit)
	}
	if err := os.WriteFile(source, []byte(`fn main() { print("changed"); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	miss, err := cache.WhyRebuild(source, "emit-llvm")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	if miss != "cache miss: source changed" {
		t.Fatalf("got %q", miss)
	}
}

// TestStatusAndPrune checks cache accounting and pruning.
func TestStatusAndPrune(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	source := writeTempSource(t, `fn main() { print("hello"); }`)
	_, err := cache.GetOrBuild(source, "emit-llvm", func() (string, error) {
		return "artifact", nil
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	status, err := cache.Status()
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Entries != 1 || status.SizeBytes == 0 || status.MaxBytes != DefaultMaxBytes {
		t.Fatalf("bad status: %+v", status)
	}
	removed, freed, err := cache.Prune()
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if removed != 1 || freed == 0 {
		t.Fatalf("removed=%d freed=%d", removed, freed)
	}
}

// TestPackageCacheNoOpRebuild checks that unchanged module packages hit cache.
func TestPackageCacheNoOpRebuild(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	root := writeTempPackage(t)
	builds := 0
	build := func() (string, error) {
		builds++
		return "package artifact", nil
	}
	first, err := cache.GetOrBuild(root, "check", build)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	second, err := cache.GetOrBuild(root, "check", build)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}
	if first.Hit || !second.Hit || builds != 1 {
		t.Fatalf("got first hit=%v second hit=%v builds=%d", first.Hit, second.Hit, builds)
	}
}

// TestPackageWhyRebuildExplainsPrivateEdit checks body-only rebuild reasons.
func TestPackageWhyRebuildExplainsPrivateEdit(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	root := writeTempPackage(t)
	mustBuildPackage(t, cache, root)
	writePackageFile(t, root, "src/lexer.kizu", lexerSource(`print("changed");`))
	miss, err := cache.WhyRebuild(root, "check")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	want := "cache miss: source changed without public interface change"
	if miss != want {
		t.Fatalf("got %q want %q", miss, want)
	}
}

// TestPackageWhyRebuildExplainsPublicInterfaceEdit checks signature changes.
func TestPackageWhyRebuildExplainsPublicInterfaceEdit(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	root := writeTempPackage(t)
	mustBuildPackage(t, cache, root)
	writePackageFile(t, root, "src/lexer.kizu", strings.ReplaceAll(
		lexerSource(`return;`), "pub fn lex", "pub fn scan"))
	miss, err := cache.WhyRebuild(root, "check")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	if miss != "cache miss: public interface changed" {
		t.Fatalf("got %q", miss)
	}
}

// TestPackageWhyRebuildExplainsManifestEdit checks manifest fingerprinting.
func TestPackageWhyRebuildExplainsManifestEdit(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	root := writeTempPackage(t)
	mustBuildPackage(t, cache, root)
	writePackageFile(t, root, "kizu.toml", packageManifest("0.2.1"))
	miss, err := cache.WhyRebuild(root, "check")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	if miss != "cache miss: manifest changed" {
		t.Fatalf("got %q", miss)
	}
}

// TestPackageWhyRebuildExplainsGraphEdit checks module graph fingerprinting.
func TestPackageWhyRebuildExplainsGraphEdit(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	root := writeTempPackage(t)
	mustBuildPackage(t, cache, root)
	writePackageFile(t, root, "src/parser.kizu", "pub fn parse() -> void { return; }\n")
	miss, err := cache.WhyRebuild(root, "check")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	if miss != "cache miss: module graph changed" {
		t.Fatalf("got %q", miss)
	}
}

// TestCurrentStdlibHashUsesSourceSkeleton checks std sources shape cache keys.
func TestCurrentStdlibHashUsesSourceSkeleton(t *testing.T) {
	hash, err := currentStdlibHash()
	if err != nil {
		t.Fatalf("stdlib hash failed: %v", err)
	}
	if hash == "" || hash == fallbackStdlibHash {
		t.Fatalf("got stdlib hash %q", hash)
	}
}

// TestWhyRebuildExplainsStdlibEdit checks stdlib fingerprinting.
func TestWhyRebuildExplainsStdlibEdit(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	source := writeTempSource(t, `fn main() { print("hello"); }`)
	input, err := newInput(source, "check")
	if err != nil {
		t.Fatalf("input failed: %v", err)
	}
	writeCacheEntry(t, cache, Entry{
		Key:        "previous-stdlib",
		Target:     input.target,
		SourcePath: input.sourcePath,
		SourceHash: input.sourceHash,
		Version:    Version,
		StdlibHash: "previous-stdlib-hash",
		CreatedAt:  time.Now().UTC(),
	})
	miss, err := cache.WhyRebuild(source, "check")
	if err != nil {
		t.Fatalf("why miss failed: %v", err)
	}
	if miss != "cache miss: stdlib changed" {
		t.Fatalf("got %q", miss)
	}
}

// writeTempSource writes source to a temp Kizu file.
func writeTempSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeCacheEntry writes one cache metadata entry for rebuild reason tests.
func writeCacheEntry(t *testing.T, cache *Cache, entry Entry) {
	t.Helper()
	if err := os.MkdirAll(cache.Dir, 0o755); err != nil {
		t.Fatalf("mkdir cache failed: %v", err)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("marshal cache entry failed: %v", err)
	}
	if err := os.WriteFile(cache.metaPath(entry.Key), data, 0o644); err != nil {
		t.Fatalf("write cache entry failed: %v", err)
	}
}

// writeTempPackage writes a small module package fixture.
func writeTempPackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePackageFile(t, root, "kizu.toml", packageManifest("0.2.0"))
	writePackageFile(t, root, "src/main.kizu", strings.TrimSpace(`
import app::lexer;

pub fn main() -> void {
    return;
}
`)+"\n")
	writePackageFile(t, root, "src/lexer.kizu", lexerSource(`return;`))
	return root
}

// mustBuildPackage stores one package cache entry or fails the test.
func mustBuildPackage(t *testing.T, cache *Cache, root string) {
	t.Helper()
	if _, err := cache.GetOrBuild(root, "check", func() (string, error) {
		return "package artifact", nil
	}); err != nil {
		t.Fatalf("build failed: %v", err)
	}
}

// packageManifest returns the test package manifest source.
func packageManifest(version string) string {
	return `[package]
name = "app"
version = "` + version + `"

[modules]
root = "src/main.kizu"
paths = ["src"]
`
}

// lexerSource returns the test lexer module with a configurable private body.
func lexerSource(body string) string {
	return `pub enum TokenKind {
    Ident
    Eof
}

pub fn lex(source: []const u8) -> void {
    helper(source);
    return;
}

fn helper(source: []const u8) -> void {
    ` + body + `
}
`
}

// writePackageFile writes one file inside a package fixture.
func writePackageFile(t *testing.T, root string, rel string, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}
