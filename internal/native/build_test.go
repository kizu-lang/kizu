package native

import "testing"

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
