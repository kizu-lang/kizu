package native

import (
	"strings"
	"testing"
)

// TestArtifactPathChecksStableNames verifies deterministic artifact locations.
func TestArtifactPathChecksStableNames(t *testing.T) {
	filePath := ArtifactPath("examples/hello.kizu", darwinArm64Target)
	if !strings.HasSuffix(filePath, "target/native/aarch64-apple-darwin/debug/hello") {
		t.Fatalf("unexpected file artifact path %q", filePath)
	}
	packagePath := ArtifactPath("selfhost", darwinArm64Target)
	if packagePath != "target/kizu-selfhost" {
		t.Fatalf("unexpected selfhost artifact path %q", packagePath)
	}
}

// TestRuntimeLLVMExportsPrintSymbols checks the runtime ABI names are stable.
func TestRuntimeLLVMExportsPrintSymbols(t *testing.T) {
	runtime := RuntimeLLVM()
	for _, want := range []string{
		"define void @kizu_print_string",
		"define void @kizu_print_int",
		"define void @kizu_print_bool",
		"declare i32 @printf",
		"declare ptr @fopen",
		"define ptr @kizu_read_file",
		"define i64 @kizu_bytes_len",
		"define i8 @kizu_byte_at",
		"define ptr @kizu_array_new",
		"define void @kizu_array_append",
		"define ptr @kizu_array_at",
		"define i64 @kizu_array_len",
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("runtime missing %q:\n%s", want, runtime)
		}
	}
}

// TestSysrootFromFlagsExtractsNixIsysroot checks direct lld SDK discovery.
func TestSysrootFromFlagsExtractsNixIsysroot(t *testing.T) {
	flags := "-O2 -isysroot /nix/store/sdk/MacOSX.sdk -isystem /nix/store/include"
	got := sysrootFromFlags(flags)
	if got != "/nix/store/sdk/MacOSX.sdk" {
		t.Fatalf("got %q", got)
	}
}

// TestLibraryPathsFromFlagsExtractsSearchDirs checks explicit lld library paths.
func TestLibraryPathsFromFlagsExtractsSearchDirs(t *testing.T) {
	flags := "-L/nix/store/libSystem/lib -L /nix/store/llvm/lib -framework CoreFoundation"
	got := strings.Join(libraryPathsFromFlags(flags), " ")
	want := "-L/nix/store/libSystem/lib -L/nix/store/llvm/lib"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWithNativeEntryWrapsVoidMain checks the executable entry point wrapper.
func TestWithNativeEntryWrapsVoidMain(t *testing.T) {
	got, err := withNativeEntry("define void @main() {\nentry:\n  ret void\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define void @kizu_user_main()",
		"define i32 @main(i32 %argc, ptr %argv)",
		"store i64 %wide_argc, ptr @kizu_argc",
		"call void @kizu_user_main()",
		"ret i32 0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapped IR missing %q:\n%s", want, got)
		}
	}
}

// TestNixLibSystemPathFindsLibraryDir checks Nix libSystem discovery.
func TestNixLibSystemPathFindsLibraryDir(t *testing.T) {
	flags := "-idirafter /nix/store/hash-libSystem-11.0.0/include -isystem /nix/store/include"
	got := nixLibSystemPath(flags)
	if got != "/nix/store/hash-libSystem-11.0.0/lib" {
		t.Fatalf("got %q", got)
	}
}
