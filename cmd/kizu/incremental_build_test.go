package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// incrementalPackage is a package of three modules: main calls into shapes
// and text, and text calls into shapes too. A debug build compiles each
// module as a unit of its own. The function the edit touches holds a loop,
// so an optimized build's IR inliner leaves it a call and the edit stays in
// its own unit.
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
    var total = 0;
    var step = 0;
    while step < 1 {
        total = total + value * 1;
        step = step + 1;
    }
    return total;
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
		requireIncrementalMatchesClean(t, kizuBinaryPath, debugUnits)
	})
	t.Run("selfhost", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds the selfhost compiler")
		}
		t.Parallel()
		requireIncrementalMatchesClean(t, sharedSelfhost(t), debugUnits)
	})
}

// TestThinLTOIncrementalBuildMatchesClean runs the same cycle for an
// optimized build linked with ThinLTO: each module's bitcode is cached as a
// debug build's object is, and the linker's own ThinLTO cache, which the
// warm builds read and the clean one starts without, must not make the
// executable differ either. The private function the edit touches is
// internal to its unit, and the public one the other units call is not.
// Only Darwin is run: its linker reads ThinLTO bitcode wherever Xcode is,
// where Linux needs lld, which the CI runner does not have.
func TestThinLTOIncrementalBuildMatchesClean(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ThinLTO is checked with Apple's linker")
	}
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		requireIncrementalMatchesClean(t, kizuBinaryPath, thinLTOUnits)
	})
	t.Run("selfhost", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds the selfhost compiler")
		}
		t.Parallel()
		requireIncrementalMatchesClean(t, sharedSelfhost(t), thinLTOUnits)
	})
}

// unitBuild is how a build compiles its units: the flags that ask for it
// and the kind `cache status` counts the compiled units as.
type unitBuild struct {
	flags []string
	kind  string
}

var (
	debugUnits   = unitBuild{kind: "native-obj"}
	thinLTOUnits = unitBuild{flags: []string{"--opt", "--thinlto"}, kind: "native-bc"}
)

// requireIncrementalMatchesClean runs the edit-and-rebuild cycle with one
// compiler.
func requireIncrementalMatchesClean(t *testing.T, kizu string, mode unitBuild) {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "app")
	for name, source := range incrementalPackage {
		writeTestFile(t, filepath.Join(pkg, name), []byte(source))
	}
	warm := filepath.Join(root, "warm-cache")
	incrementalBuild(t, kizu, mode, warm, pkg, filepath.Join(root, "first"))
	before := cachedObjects(t, kizu, mode, warm)
	if before < 4 {
		t.Fatalf("first build cached %d objects, want one per module and the program unit", before)
	}
	edited := filepath.Join(pkg, "src/shapes/shapes.kizu")
	source := []byte(incrementalPackage["src/shapes/shapes.kizu"])
	source = regexp.MustCompile(`value \* 1`).ReplaceAll(source, []byte("value * 2"))
	writeTestFile(t, edited, source)
	rebuilt := incrementalBuild(t, kizu, mode, warm, pkg, filepath.Join(root, "rebuilt"))
	if after := cachedObjects(t, kizu, mode, warm); after != before+1 {
		t.Errorf("rebuild after editing one module compiled %d objects, want 1", after-before)
	}
	cleanCache := filepath.Join(root, "clean-cache")
	clean := incrementalBuild(t, kizu, mode, cleanCache, pkg, filepath.Join(root, "clean"))
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
func incrementalBuild(
	t *testing.T, kizu string, mode unitBuild, cacheDir string, pkg string, dir string,
) string {
	t.Helper()
	output := filepath.Join(dir, "app")
	args := append([]string{"build", "--target", "native"}, mode.flags...)
	build := exec.Command(kizu, append(args, "-o", output, pkg)...)
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return output
}

// cachedObjects returns how many compiled units of the mode's kind a cache
// directory holds, as `cache status` reports it.
func cachedObjects(t *testing.T, kizu string, mode unitBuild, cacheDir string) int {
	t.Helper()
	status := exec.Command(kizu, "cache", "status")
	status.Env = append(os.Environ(), "KIZU_CACHE_DIR="+cacheDir)
	out, err := status.Output()
	if err != nil {
		t.Fatalf("cache status: %v", err)
	}
	line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(mode.kind) + `: (\d+) entries`)
	match := line.FindSubmatch(out)
	if match == nil {
		t.Fatalf("cache status lists no objects:\n%s", out)
	}
	count, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// TestThinLTORejectsMisuse checks that both compilers refuse --thinlto where
// it cannot apply before compiling anything: without --opt, and for an
// object, whose code a ThinLTO build would only make in a link it does not
// run.
func TestThinLTORejectsMisuse(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "without opt",
			args: []string{"--thinlto"},
			want: "native error: --thinlto links an optimized build; add --opt",
		},
		{
			name: "object",
			args: []string{"--opt", "--thinlto", "--emit", "obj"},
			want: "native error: --thinlto makes the code when an executable is linked; build --emit exe",
		},
	}
	compilers := map[string]func(*testing.T) string{
		"go": func(*testing.T) string { return kizuBinaryPath },
		"selfhost": func(t *testing.T) string {
			if testing.Short() {
				t.Skip("builds the selfhost compiler")
			}
			return sharedSelfhost(t)
		},
	}
	for compiler, path := range compilers {
		for _, tt := range cases {
			t.Run(compiler+"/"+tt.name, func(t *testing.T) {
				kizu := path(t)
				args := append([]string{"build", "--target", "native"}, tt.args...)
				args = append(args, "-o", filepath.Join(t.TempDir(), "out"), "../../examples/hello.kizu")
				out, err := exec.Command(kizu, args...).CombinedOutput()
				if err == nil {
					t.Fatalf("expected the build to fail\n%s", out)
				}
				if !strings.Contains(string(out), tt.want) {
					t.Fatalf("got %q, want substring %q", out, tt.want)
				}
			})
		}
	}
}
