package native

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExecutableKeyNamesTheRuntimeItLinks checks the executable is keyed by the
// runtime object it is linked with. Two builds of one program against different
// runtimes are two different executables, and a key that left the runtime out
// would hand back the older of them as if it were current.
func TestExecutableKeyNamesTheRuntimeItLinks(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	before := executableCacheTarget(options, "/cache/aaaa.out")
	after := executableCacheTarget(options, "/cache/bbbb.out")
	if before == after {
		t.Fatalf("two runtimes share the key %q", before)
	}
}

// TestExecutableAndRuntimeKeysDoNotCollide checks the two artifacts a native
// build stores are told apart by their keys. They are built from different
// content by the same toolchain, so only the name they are filed under keeps a
// runtime object from being handed back as a program.
func TestExecutableAndRuntimeKeysDoNotCollide(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	if runtimeCacheTarget(options) == executableCacheTarget(options, "/cache/aaaa.out") {
		t.Fatal("the runtime object and the executable share one key")
	}
}

// TestModuleForLinkerFollowsTheClang checks a Darwin link hands the linker the
// probe it writes: Apple's helper under Apple's clang, which takes any other
// probe name for a function to call, and the inline probe the module was
// emitted with under upstream clang, which stops at the helper's name. A link
// for another target keeps the module as it is without asking the linker.
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
		{"", "x86_64-unknown-linux-gnu", module},
	}
	for _, tt := range cases {
		dir := t.TempDir()
		if tt.version != "" {
			script := "#!/bin/sh\necho '" + tt.version + "'\n"
			if err := os.WriteFile(filepath.Join(dir, "clang"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", dir)
		got, err := moduleForLinker(Options{LLVMIR: module, Triple: tt.triple, Linker: "clang"})
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%q for %s: got %q, want %q", tt.version, tt.triple, got, tt.want)
		}
	}
}
