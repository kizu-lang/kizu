package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// incrementalPackage is a package of three modules: main calls into shapes
// and text, and text calls into shapes too. A debug build compiles each
// module as a unit of its own.
var incrementalPackage = map[string]string{
	"kizu.toml": `[package]
name = "app"
version = "0.1.0"

[modules]
paths = ["src"]
`,
	"src/main.kizu": `import app::shapes;
import app::text;

fn main() -> void {
    print(shapes::area(shapes::Rect { w: 3, h: 4 }));
    print(text::label());
    return;
}
`,
	"src/shapes/shapes.kizu": `pub struct Rect {
    pub w: i64,
    pub h: i64,
}

fn scale(value: i64) -> i64 {
    return value * 1;
}

pub fn area(rect: Rect) -> i64 {
    return scale(rect.w * rect.h);
}
`,
	"src/text/text.kizu": `import app::shapes;

pub fn label() -> i64 {
    return shapes::area(shapes::Rect { w: 1, h: 2 }) + 100;
}
`,
}

// TestIncrementalBuildMatchesClean edits the body of one module's private
// function and rebuilds against the cache the first build filled. The
// rebuild compiles one object, the edited module's, and links an executable
// identical to the one a build with an empty cache makes: what the cache
// hands back is what compiling again would have made.
func TestIncrementalBuildMatchesClean(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		requireIncrementalMatchesClean(t, kizuBinaryPath)
	})
	t.Run("selfhost", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds the selfhost compiler")
		}
		t.Parallel()
		requireIncrementalMatchesClean(t, sharedSelfhost(t))
	})
}

// requireIncrementalMatchesClean runs the edit-and-rebuild cycle with one
// compiler.
func requireIncrementalMatchesClean(t *testing.T, kizu string) {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "app")
	for name, source := range incrementalPackage {
		writeTestFile(t, filepath.Join(pkg, name), []byte(source))
	}
	warm := filepath.Join(root, "warm-cache")
	incrementalBuild(t, kizu, warm, pkg, filepath.Join(root, "first"))
	before := cachedObjects(t, kizu, warm)
	if before < 4 {
		t.Fatalf("first build cached %d objects, want one per module and the program unit", before)
	}
	edited := filepath.Join(pkg, "src/shapes/shapes.kizu")
	source := []byte(incrementalPackage["src/shapes/shapes.kizu"])
	source = regexp.MustCompile(`value \* 1`).ReplaceAll(source, []byte("value * 2"))
	writeTestFile(t, edited, source)
	rebuilt := incrementalBuild(t, kizu, warm, pkg, filepath.Join(root, "rebuilt"))
	if after := cachedObjects(t, kizu, warm); after != before+1 {
		t.Errorf("rebuild after editing one module compiled %d objects, want 1", after-before)
	}
	cleanCache := filepath.Join(root, "clean-cache")
	clean := incrementalBuild(t, kizu, cleanCache, pkg, filepath.Join(root, "clean"))
	first, err := os.ReadFile(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("incremental build is %d bytes, clean build %d", len(first), len(second))
	}
	normalizeMachOLinkIdentity(t, "incremental", first)
	normalizeMachOLinkIdentity(t, "clean", second)
	if differing, at := countDifferingBytes(first, second); differing != 0 {
		t.Fatalf("incremental build differs from clean build in %d bytes, first at %d", differing, at)
	}
	run := exec.Command(rebuilt)
	out, err := run.CombinedOutput()
	if err != nil || string(out) != "24\n104\n" {
		t.Fatalf("rebuilt program: %v\n%s", err, out)
	}
}

// incrementalBuild builds pkg with a cache directory into dir/app and returns
// the executable's path. Every build writes under the same file name, which
// the linker records in the executable.
func incrementalBuild(t *testing.T, kizu string, cacheDir string, pkg string, dir string) string {
	t.Helper()
	output := filepath.Join(dir, "app")
	build := exec.Command(kizu, "build", "--target", "native", "-o", output, pkg)
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return output
}

// cachedObjectsLine is how `cache status` counts the unit objects it holds.
var cachedObjectsLine = regexp.MustCompile(`(?m)^native-obj: (\d+) entries`)

// cachedObjects returns how many unit objects a cache directory holds, as
// `cache status` reports it.
func cachedObjects(t *testing.T, kizu string, cacheDir string) int {
	t.Helper()
	status := exec.Command(kizu, "cache", "status")
	status.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := status.Output()
	if err != nil {
		t.Fatalf("cache status: %v", err)
	}
	match := cachedObjectsLine.FindSubmatch(out)
	if match == nil {
		t.Fatalf("cache status lists no objects:\n%s", out)
	}
	count, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	return count
}
