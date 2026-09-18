package native

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExecutableKeyNamesTheRuntimeItLinks checks the executable is keyed by the
// runtime source it is linked with. Two builds of one program against different
// runtimes are two different executables, and a key that left the runtime out
// would hand back the older of them as if it were current.
func TestExecutableKeyNamesTheRuntimeItLinks(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	before := executableCacheTarget(options, "int kizu_runtime_v1;")
	after := executableCacheTarget(options, "int kizu_runtime_v2;")
	if before == after {
		t.Fatalf("two runtimes share the key %q", before)
	}
}

// TestRuntimeKeyNamesTheClang checks the runtime object is keyed by the clang
// that compiles it. An upgrade keeps the driver's path and flags, so the
// version line is the only thing that tells the old object from the one the
// new clang would make.
func TestRuntimeKeyNamesTheClang(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	before := runtimeCacheTarget(options, "clang version 16.0.6")
	after := runtimeCacheTarget(options, "clang version 21.1.8")
	if before == after {
		t.Fatalf("two clangs share the key %q", before)
	}
}

// TestExecutableAndRuntimeKeysDoNotCollide checks the two artifacts a native
// build stores are told apart by their keys. They are built from different
// content by the same toolchain, so only the name they are filed under keeps a
// runtime object from being handed back as a program.
func TestExecutableAndRuntimeKeysDoNotCollide(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	runtime := runtimeCacheTarget(options, "clang version 21.1.8")
	if runtime == executableCacheTarget(options, "int kizu_runtime;") {
		t.Fatal("the runtime object and the executable share one key")
	}
}

// TestModuleForLinkerFollowsTheClang checks a Darwin link hands the linker the
// probe it writes: Apple's helper under Apple's clang, which takes any other
// probe name for a function to call, and the inline probe the module was
// emitted with under upstream clang, which stops at the helper's name. A link
// for another target keeps the module as it is whatever links it.
func TestModuleForLinkerFollowsTheClang(t *testing.T) {
	module := "attributes #0 = { " + inlineStackProbe + " \"stack-probe-size\"=\"4096\" }\n"
	apple := "attributes #0 = { " + appleStackProbe + " \"stack-probe-size\"=\"4096\" }\n"
	cases := []struct {
		version string
		triple  string
		want    string
	}{
		{"Apple clang version 15.0.0 (clang-1500.3.9.4)", "arm64-apple-darwin", apple},
		{"clang version 21.1.8", "arm64-apple-darwin", module},
		{"Apple clang version 15.0.0 (clang-1500.3.9.4)", "x86_64-unknown-linux-gnu", module},
	}
	for _, tt := range cases {
		got := moduleForLinker(Options{LLVMIR: module, Triple: tt.triple, Linker: "clang"}, tt.version)
		if got != tt.want {
			t.Errorf("%q for %s: got %q, want %q", tt.version, tt.triple, got, tt.want)
		}
	}
}

// TestLinkerVersionIsTheFirstLine checks the linker is named by the first line
// of what `--version` prints, without the target and directory lines that
// follow it or the newline that ends it.
func TestLinkerVersionIsTheFirstLine(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"echo 'Apple clang version 15.0.0 (clang-1500.3.9.4) '\n" +
		"echo 'Target: arm64-apple-darwin23.6.0'\n"
	if err := os.WriteFile(filepath.Join(dir, "clang"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := linkerVersion(Options{Linker: "clang"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Apple clang version 15.0.0 (clang-1500.3.9.4)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
