package buildcache

import (
	"os"
	"testing"
)

// TestGetOrBuildArtifactKeysOnContent checks a compiler-carried artifact is
// built once for the content it is made of and again when that content changes.
func TestGetOrBuildArtifactKeysOnContent(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	builds := 0
	build := func(output string) error {
		builds++
		return os.WriteFile(output, []byte("object"), 0o644)
	}
	first, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("source"), build)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	second, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("source"), build)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}
	if first != second || builds != 1 {
		t.Fatalf("got %q then %q after %d builds", first, second, builds)
	}
	changed, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("edited"), build)
	if err != nil {
		t.Fatalf("changed build failed: %v", err)
	}
	if changed == first || builds != 2 {
		t.Fatalf("got %q for edited content after %d builds", changed, builds)
	}
}

// TestGetOrBuildArtifactRebuildsPrunedArtifact checks an entry whose artifact is
// gone is a miss, so the path handed to the toolchain always names a real file.
func TestGetOrBuildArtifactRebuildsPrunedArtifact(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	builds := 0
	build := func(output string) error {
		builds++
		return os.WriteFile(output, []byte("object"), 0o644)
	}
	path, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("source"), build)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	again, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("source"), build)
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if again != path || builds != 2 {
		t.Fatalf("got %q after %d builds", again, builds)
	}
	if _, err := os.Stat(again); err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
}

// TestStatusAndPrune checks cache accounting and pruning.
func TestStatusAndPrune(t *testing.T) {
	cache := &Cache{Dir: t.TempDir(), MaxBytes: DefaultMaxBytes}
	build := func(output string) error {
		return os.WriteFile(output, []byte("object"), 0o644)
	}
	if _, err := cache.GetOrBuildArtifact("runtime.c", "native", []byte("source"), build); err != nil {
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
