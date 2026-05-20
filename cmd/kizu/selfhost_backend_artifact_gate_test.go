package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostBackendArtifactGate executes the Kizu-owned LLVM artifact smoke.
func TestSelfhostBackendArtifactGate(t *testing.T) {
	if failures := countSelfhostBackendArtifactGateFailures(t); failures > 0 {
		t.Fatalf("selfhost backend artifact gate failures=%d", failures)
	}
}

// countSelfhostBackendArtifactGateFailures returns backend artifact gate failures.
func countSelfhostBackendArtifactGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostBackendArtifactGate(t)
	if err != nil {
		t.Errorf("backend artifact gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"llvm-artifact-path\ntarget/selfhost/selfhost.ll\n",
		"llvm-metadata-path\ntarget/selfhost/selfhost.ll.meta\n",
		"llvm-artifact-bytes\n",
		"llvm-metadata-bytes\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("backend artifact gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return countSelfhostBackendArtifactFileFailures(t)
}

// countSelfhostBackendArtifactFileFailures validates deterministic LLVM artifacts.
func countSelfhostBackendArtifactFileFailures(t *testing.T) int {
	t.Helper()
	llBytes, err := os.ReadFile("../../target/selfhost/selfhost.ll")
	if err != nil {
		t.Errorf("read LLVM artifact: %v", err)
		return 1
	}
	metaBytes, err := os.ReadFile("../../target/selfhost/selfhost.ll.meta")
	if err != nil {
		t.Errorf("read LLVM artifact metadata: %v", err)
		return 1
	}
	return countTextualLLVMValidationFailures(t, string(llBytes), string(metaBytes))
}

// countTextualLLVMValidationFailures applies the documented textual IR validation.
func countTextualLLVMValidationFailures(t *testing.T, llContent string, metaContent string) int {
	t.Helper()
	requiredLL := []string{
		"; kizu selfhost bootstrap ll v0\n",
		"source_filename = \"target/selfhost/selfhost.ir\"\n",
		"%kizu.slice.u8 = type { ptr, i64 }\n",
		"%kizu.owned = type { ptr }\n",
		"declare %kizu.owned @kizu_rt_io_blocking()\n",
		"declare %kizu.error.slice.u8 @kizu_rt_fs_read_file",
		"declare %kizu.error.void @kizu_rt_fs_write_file",
		"declare void @kizu_rt_owned_deinit(%kizu.owned)\n",
		"declare void @kizu_rt_trap(%kizu.slice.u8) noreturn\n",
		"define i64 @kizu_selfhost__smoke() {\n",
	}
	for _, fragment := range requiredLL {
		if !strings.Contains(llContent, fragment) {
			t.Errorf("LLVM artifact missing %q:\n%s", fragment, llContent)
			return 1
		}
	}
	return countLLVMMetadataValidationFailures(t, metaContent)
}

// countLLVMMetadataValidationFailures validates artifact metadata for stage comparison.
func countLLVMMetadataValidationFailures(t *testing.T, metaContent string) int {
	t.Helper()
	requiredMeta := []string{
		"kizu-llvm-artifact-v0\n",
		"abi selfhost-abi-v0\n",
		"ir target/selfhost/selfhost.ir\n",
		"manifest target/selfhost/selfhost.ir.manifest\n",
		"output target/selfhost/selfhost.ll\n",
		"validation go test ./cmd/kizu -run TestSelfhostBackendArtifactGate\n",
		"external @kizu_rt_io_blocking\n",
		"external @kizu_rt_fs_read_file\n",
		"external @kizu_rt_fs_write_file\n",
		"external @kizu_rt_owned_deinit\n",
		"external @kizu_rt_trap\n",
		"unsupported-policy blocker\n",
	}
	for _, fragment := range requiredMeta {
		if !strings.Contains(metaContent, fragment) {
			t.Errorf("LLVM artifact metadata missing %q:\n%s", fragment, metaContent)
			return 1
		}
	}
	return 0
}

// runSelfhostBackendArtifactGate loads the selfhost package and runs the backend gate.
func runSelfhostBackendArtifactGate(t *testing.T) (string, error) {
	t.Helper()
	if err := os.RemoveAll("../../target/selfhost"); err != nil {
		return "", err
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		return "", err
	}
	defer restore()

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::backend_artifact_gate")
	return out.String(), err
}
