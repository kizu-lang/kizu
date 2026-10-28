package native

import (
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"os"
	"path/filepath"
	"strings"
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

// TestRuntimeKeyNamesTheSDK keys the runtime by the SDK Apple's clang reads
// from SDKROOT: one compiled against the macOS SDK is not one for iOS.
func TestRuntimeKeyNamesTheSDK(t *testing.T) {
	options := Options{LibC: "on", Runtime: "hosted", Emit: "obj", Linker: "clang",
		Triple: "arm64-apple-ios17.0"}
	t.Setenv("SDKROOT", "")
	unset := runtimeCacheTarget(options, "Apple clang version 17.0.0")
	t.Setenv("SDKROOT", "/sdk/iPhoneOS.sdk")
	set := runtimeCacheTarget(options, "Apple clang version 17.0.0")
	if unset == set {
		t.Fatalf("two SDKs share the key %q", set)
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
// emitted with under upstream clang, which stops at the helper's name. A
// Windows link drops the probe, which that target reads as a function name. A
// link for another target keeps the module as it is whatever links it.
func TestModuleForLinkerFollowsTheClang(t *testing.T) {
	module := "attributes #0 = { " + inlineStackProbe + " \"stack-probe-size\"=\"4096\" }\n"
	apple := "attributes #0 = { " + appleStackProbe + " \"stack-probe-size\"=\"4096\" }\n"
	windows := "attributes #0 = { \"stack-probe-size\"=\"4096\" }\n"
	cases := []struct {
		version string
		triple  string
		want    string
	}{
		{"Apple clang version 15.0.0 (clang-1500.3.9.4)", "arm64-apple-darwin", apple},
		{"clang version 21.1.8", "arm64-apple-darwin", module},
		{"Apple clang version 15.0.0 (clang-1500.3.9.4)", "x86_64-unknown-linux-gnu", module},
		{"clang version 21.1.8", "x86_64-w64-windows-gnu", windows},
	}
	for _, tt := range cases {
		target, err := TargetFor(tt.triple)
		if err != nil {
			t.Fatal(err)
		}
		got := moduleForLinker(Options{LLVMIR: module, Target: target, Linker: "clang"}, tt.version)
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

// TestLinkFlagsFollowTheLinkerOrder hands the linker the search paths before
// the libraries looked for in them, and libm and libpthread last whatever the
// program declared, since the runtime needs them.
func TestLinkFlagsFollowTheLinkerOrder(t *testing.T) {
	options := Options{
		Libraries: []string{"fftw3"}, Frameworks: []string{"Accelerate"},
		LibrarySearch: []string{"/opt/lib"}, FrameworkSearch: []string{"/opt/frameworks"},
	}
	got := strings.Join(linkFlags(options), " ")
	want := "-L/opt/lib -F/opt/frameworks -lfftw3 -lm -lpthread -framework Accelerate"
	if got != want {
		t.Fatalf("linkFlags = %q, want %q", got, want)
	}
	// A program that names libm itself is not handed it twice.
	if got := strings.Join(linkFlags(Options{Libraries: []string{"m"}}), " "); got != "-lm -lpthread" {
		t.Fatalf("linkFlags(m) = %q", got)
	}
	// A Windows executable is static and links Winsock and bcrypt besides.
	windows := Options{Target: stdtarget.NativeWindows, Libraries: []string{"user32"}}
	got = strings.Join(linkFlags(windows), " ")
	if want := "-static -luser32 -lm -lpthread -lws2_32 -lbcrypt"; got != want {
		t.Fatalf("linkFlags(windows) = %q, want %q", got, want)
	}
}

// TestWindowsRunsTheMinGWDriver runs llvm-mingw's driver for the machine a
// Windows triple names, and the linker as named for every other target.
func TestWindowsRunsTheMinGWDriver(t *testing.T) {
	cases := map[string]string{
		"x86_64-w64-windows-gnu":   "x86_64-w64-mingw32-clang",
		"aarch64-w64-windows-gnu":  "aarch64-w64-mingw32-clang",
		"x86_64-unknown-linux-gnu": "clang",
	}
	for triple, want := range cases {
		target, err := TargetFor(triple)
		if err != nil {
			t.Fatal(err)
		}
		if got := driver(Options{Target: target, Triple: triple, Linker: "clang"}); got != want {
			t.Errorf("driver(%s) = %q, want %q", triple, got, want)
		}
	}
}

// TestWindowsLinksUnderTheExeName links a Windows executable whose name has no
// extension under the name the MinGW driver would give it, and anything else
// under the name asked for.
func TestWindowsLinksUnderTheExeName(t *testing.T) {
	windows := Options{Target: stdtarget.NativeWindows, Emit: "exe"}
	if got := linkedOutput("out/app", windows); got != "out/app.exe" {
		t.Errorf("linkedOutput(app) = %q", got)
	}
	if got := linkedOutput("out/app.exe", windows); got != "out/app.exe" {
		t.Errorf("linkedOutput(app.exe) = %q", got)
	}
	windows.Emit = "obj"
	if got := linkedOutput("out/app", windows); got != "out/app" {
		t.Errorf("linkedOutput(obj) = %q", got)
	}
	linux := Options{Target: stdtarget.NativeLinux, Emit: "exe"}
	if got := linkedOutput("out/app", linux); got != "out/app" {
		t.Errorf("linkedOutput(linux) = %q", got)
	}
}

// TestExecutableKeyNamesTheLibrariesItLinks keys the executable by what it
// links: the same IR linked against another library is another executable.
func TestExecutableKeyNamesTheLibrariesItLinks(t *testing.T) {
	base := Options{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	withLibrary := base
	withLibrary.Libraries = []string{"fftw3"}
	if executableCacheTarget(base, "int r;") == executableCacheTarget(withLibrary, "int r;") {
		t.Fatalf("a linked library does not change the executable key")
	}
}

// TestFrameworkNeedsDarwin refuses `@link_framework` for a target whose
// linker has no frameworks, before clang is asked.
func TestFrameworkNeedsDarwin(t *testing.T) {
	options := Options{
		LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang",
		Target: stdtarget.NativeLinux, Frameworks: []string{"Accelerate"},
	}
	err := validateOptions(options)
	if err == nil || !strings.Contains(err.Error(), "needs a Darwin target") {
		t.Fatalf("expected a Darwin-only error, got %v", err)
	}
}

// TestTargetForTriple maps the OS a triple names to the target the comptime
// predicates and the manifest read, and refuses an OS the runtime has no
// build for and the MSVC environment Windows is not built for.
func TestTargetForTriple(t *testing.T) {
	cases := map[string]stdtarget.Target{
		"arm64-apple-darwin":            stdtarget.NativeDarwin,
		"x86_64-apple-macos":            stdtarget.NativeDarwin,
		"arm64-apple-ios17.0":           stdtarget.NativeDarwin,
		"arm64-apple-ios17.0-simulator": stdtarget.NativeDarwin,
		"x86_64-unknown-linux-gnu":      stdtarget.NativeLinux,
		"x86_64-w64-windows-gnu":        stdtarget.NativeWindows,
		"x86_64-w64-mingw32":            stdtarget.NativeWindows,
	}
	for triple, want := range cases {
		got, err := TargetFor(triple)
		if err != nil || got != want {
			t.Fatalf("TargetFor(%q) = (%v, %v), want %v", triple, got, err, want)
		}
	}
	if _, err := TargetFor("x86_64-unknown-freebsd"); err == nil ||
		!strings.Contains(err.Error(), "names no supported OS") {
		t.Fatalf("expected an unsupported OS error, got %v", err)
	}
	if _, err := TargetFor("x86_64-pc-windows-msvc"); err == nil ||
		!strings.Contains(err.Error(), "names the MSVC environment") {
		t.Fatalf("expected an MSVC error, got %v", err)
	}
	host, err := TargetFor("")
	if err != nil || !host.IsNative() {
		t.Fatalf("TargetFor(\"\") = (%v, %v)", host, err)
	}
}
