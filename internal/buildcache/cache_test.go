package buildcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// writeTempSource writes source to a temp Kizu file.
func writeTempSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
