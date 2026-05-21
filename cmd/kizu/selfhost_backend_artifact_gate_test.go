package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostBackendArtifactGate executes the Kizu-owned LLVM artifact smoke.
func TestSelfhostBackendArtifactGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countSelfhostBackendArtifactGateFailures(t); failures > 0 {
		t.Fatalf("selfhost backend artifact gate failures=%d", failures)
	}
}

// TestSelfhostRuntimeStorageGate exercises the checked-in no-Go storage template.
func TestSelfhostRuntimeStorageGate(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is required for selfhost runtime storage smoke")
	}
	if failures := countSelfhostRuntimeStorageGateFailures(t); failures > 0 {
		t.Fatalf("selfhost runtime storage gate failures=%d", failures)
	}
}

// countSelfhostRuntimeStorageGateFailures validates the runtime template directly.
func countSelfhostRuntimeStorageGateFailures(t *testing.T) int {
	t.Helper()
	storageBytes, err := os.ReadFile("../../selfhost/runtime/selfhost.storage.ll")
	if err != nil {
		t.Errorf("read runtime storage template: %v", err)
		return 1
	}
	if failures := countRuntimeStorageLLFailures(t, string(storageBytes)); failures > 0 {
		return failures
	}
	metaBytes, err := os.ReadFile("../../selfhost/runtime/selfhost.storage.ll.meta.tail")
	if err != nil {
		t.Errorf("read runtime storage metadata template: %v", err)
		return 1
	}
	if failures := countRuntimeStorageTemplateMetadataFailures(t, string(metaBytes)); failures > 0 {
		return failures
	}
	failures := runRuntimeStorageSmoke(
		t,
		"selfhost/runtime/selfhost.storage.ll",
		"selfhost/runtime/selfhost.host.ll",
	)
	failures += runRuntimeStorageCountingSmoke(t, "selfhost/runtime/selfhost.storage.ll")
	return failures
}

// countRuntimeStorageTemplateMetadataFailures checks template-only metadata.
func countRuntimeStorageTemplateMetadataFailures(t *testing.T, metaContent string) int {
	t.Helper()
	required := []string{
		"array-storage copy-element-byte-buffer\n",
		"array-at returns-stored-element\n",
		"array-deinit releases-element-buffer\n",
		"array-invalid-element-diagnostic invalid array element\n",
		"array-oob-diagnostic array index out of bounds\n",
		"string-storage byte-buffer\n",
		"string-as-bytes returns-stored-bytes\n",
		"string-deinit releases-byte-buffer\n",
		"string-invalid-slice-diagnostic invalid slice\n",
		"map-storage string-key-i64-two-entry\n",
		"map-key-ownership copies-key-bytes\n",
		"map-missing-key-diagnostic Map.get key not found\n",
		"map-capacity-diagnostic map capacity exceeded\n",
		"reachable arena ast-node-storage\n",
		"reachable handle ast-node-id\n",
		"arena-allocator-boundary explicit\n",
		"arena-handle-provenance checked\n",
		"arena-invalid-handle-diagnostic invalid arena handle\n",
	}
	for _, fragment := range required {
		if !strings.Contains(metaContent, fragment) {
			t.Errorf("runtime storage metadata template missing %q", fragment)
			return 1
		}
	}
	return 0
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
		"runtime-storage-path\ntarget/selfhost/selfhost.storage.ll\n",
		"runtime-storage-metadata-path\ntarget/selfhost/selfhost.storage.ll.meta\n",
		"host-capability-path\ntarget/selfhost/selfhost.host.ll\n",
		"host-capability-metadata-path\ntarget/selfhost/selfhost.host.ll.meta\n",
		"llvm-artifact-bytes\n",
		"llvm-metadata-bytes\n",
		"runtime-storage-bytes\n",
		"runtime-storage-metadata-bytes\n",
		"host-capability-bytes\n",
		"host-capability-metadata-bytes\n",
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
	storageBytes, err := os.ReadFile("../../target/selfhost/selfhost.storage.ll")
	if err != nil {
		t.Errorf("read runtime storage artifact: %v", err)
		return 1
	}
	storageMetaBytes, err := os.ReadFile("../../target/selfhost/selfhost.storage.ll.meta")
	if err != nil {
		t.Errorf("read runtime storage metadata: %v", err)
		return 1
	}
	hostBytes, err := os.ReadFile("../../target/selfhost/selfhost.host.ll")
	if err != nil {
		t.Errorf("read host capability artifact: %v", err)
		return 1
	}
	hostMetaBytes, err := os.ReadFile("../../target/selfhost/selfhost.host.ll.meta")
	if err != nil {
		t.Errorf("read host capability metadata: %v", err)
		return 1
	}
	failures := countTextualLLVMValidationFailures(t, string(llBytes), string(metaBytes))
	failures += countRuntimeStorageValidationFailures(
		t,
		string(storageBytes),
		string(storageMetaBytes),
	)
	failures += countHostCapabilityValidationFailures(
		t,
		string(hostBytes),
		string(hostMetaBytes),
	)
	if failures == 0 {
		failures += countHostedCompilerCLISmokeFailures(t)
	}
	return failures
}

// countTextualLLVMValidationFailures applies the documented textual IR validation.
func countTextualLLVMValidationFailures(t *testing.T, llContent string, metaContent string) int {
	t.Helper()
	requiredLL := []string{
		"; kizu selfhost bootstrap ll v0\n",
		"source_filename = \"target/selfhost/selfhost.ir\"\n",
		"%kizu.slice.u8 = type { ptr, i64 }\n",
		"%kizu.owned = type { ptr }\n",
		"%kizu.handle = type { ptr, i64 }\n",
		"%kizu.error.bool = type { i1, i1, %kizu.slice.u8 }\n",
		"%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }\n",
		"declare %kizu.owned @kizu_rt_mem_page_allocator()\n",
		"declare %kizu.owned @kizu_rt_io_blocking()\n",
		"declare %kizu.error.bool @kizu_rt_fs_exists",
		"declare %kizu.error.metadata @kizu_rt_fs_metadata",
		"declare %kizu.error.owned @kizu_rt_fs_read_dir",
		"declare %kizu.error.slice.u8 @kizu_rt_fs_read_file",
		"declare %kizu.error.void @kizu_rt_fs_write_file",
		"declare %kizu.error.void @kizu_rt_fs_create_dir",
		"declare %kizu.error.void @kizu_rt_fs_rename",
		"declare %kizu.error.void @kizu_rt_io_write_stdout",
		"declare %kizu.error.void @kizu_rt_io_write_stderr",
		"declare i64 @kizu_rt_process_arg_count()\n",
		"declare %kizu.error.slice.u8 @kizu_rt_process_arg",
		"declare %kizu.error.slice.u8 @kizu_rt_process_env",
		"declare i64 @kizu_rt_process_exit_code",
		"declare void @kizu_rt_process_exit(i64) noreturn\n",
		"declare void @kizu_rt_owned_deinit(%kizu.owned)\n",
		"declare void @kizu_rt_trap(%kizu.slice.u8) noreturn\n",
		"declare i64 @kizu_selfhost__runtime_storage_smoke()\n",
		"declare i64 @kizu_selfhost__host_capability_smoke()\n",
		"define i1 @kizu_selfhost__slice_equal",
		"define i1 @kizu_selfhost__slice_starts_with_dash",
		"define i64 @kizu_selfhost__cli_main() {\n",
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
		"runtime-storage target/selfhost/selfhost.storage.ll\n",
		"host-runtime target/selfhost/selfhost.host.ll\n",
		"host-runtime-metadata target/selfhost/selfhost.host.ll.meta\n",
		"storage-backend selfhost-runtime-ll\n",
		"host-capabilities selfhost-host-v0\n",
		"go-stdprim-storage none\n",
		"go-stdprim-host none\n",
		"linker-process deferred issue-459\n",
		"entry @kizu_selfhost__cli_main\n",
		"cli-command check selfhost\n",
		"cli-command stage selfhost\n",
		"cli-command check examples/hello.kizu\n",
		"cli-command check examples/negative/moved_value.kizu\n",
		"cli-command parse selfhost/tests/cli/parse_ok_minimal.kizu\n",
		"cli-command parse selfhost/tests/cli/parse_invalid_missing_expr.kizu\n",
		"cli-command run selfhost/tests/cli/run_hello.kizu\n",
		"cli-command run selfhost/tests/cli/run_invalid_missing_expr.kizu\n",
		"cli-command test selfhost/tests/cli/test_expect_ok.kizu\n",
		"cli-command test selfhost/tests/cli/test_expect_failure.kizu\n",
		"cli-parity-manifest selfhost/tests/cli/parse-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/check-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/run-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/test-parity.tsv\n",
		"cli-hosted-smoke no-go\n",
		"validation go test ./cmd/kizu -run TestSelfhostBackendArtifactGate\n",
		"external @kizu_rt_mem_page_allocator\n",
		"external @kizu_rt_io_blocking\n",
		"external @kizu_rt_fs_exists\n",
		"external @kizu_rt_fs_metadata\n",
		"external @kizu_rt_fs_read_dir\n",
		"external @kizu_rt_fs_read_file\n",
		"external @kizu_rt_fs_write_file\n",
		"external @kizu_rt_fs_create_dir\n",
		"external @kizu_rt_fs_rename\n",
		"external @kizu_rt_io_write_stdout\n",
		"external @kizu_rt_io_write_stderr\n",
		"external @kizu_rt_process_arg_count\n",
		"external @kizu_rt_process_arg\n",
		"external @kizu_rt_process_env\n",
		"external @kizu_rt_process_exit_code\n",
		"external @kizu_rt_process_exit\n",
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

// countRuntimeStorageValidationFailures validates the non-Go runtime storage artifact.
func countRuntimeStorageValidationFailures(t *testing.T, llContent string, metaContent string) int {
	t.Helper()
	if failures := countRuntimeStorageLLFailures(t, llContent); failures > 0 {
		return failures
	}
	failures := countRuntimeStorageMetadataFailures(t, metaContent)
	failures += countRuntimeStorageLinkSmokeFailures(t)
	return failures
}

// countRuntimeStorageLLFailures checks storage symbols and smoke calls.
func countRuntimeStorageLLFailures(t *testing.T, llContent string) int {
	t.Helper()
	for _, group := range runtimeStorageRequiredLLFragmentGroups() {
		if failures := countRuntimeStorageMissingLLFragments(t, llContent, group); failures > 0 {
			return failures
		}
	}
	forbiddenMarkers := []string{"std.builtin", "stdprim", "internal/interp", "internal global"}
	for _, forbidden := range forbiddenMarkers {
		if strings.Contains(llContent, forbidden) {
			t.Errorf("runtime storage artifact contains Go fallback marker %q", forbidden)
			return 1
		}
	}
	return 0
}

// countRuntimeStorageMissingLLFragments checks one fragment group.
func countRuntimeStorageMissingLLFragments(
	t *testing.T,
	llContent string,
	requiredLL []string,
) int {
	t.Helper()
	for _, fragment := range requiredLL {
		if !strings.Contains(llContent, fragment) {
			t.Errorf("runtime storage artifact missing %q:\n%s", fragment, llContent)
			return 1
		}
	}
	return 0
}

// runtimeStorageRequiredLLFragmentGroups returns bounded validation groups.
func runtimeStorageRequiredLLFragmentGroups() [][]string {
	return [][]string{
		runtimeStorageLayoutLLFragments(),
		runtimeStorageFunctionLLFragments(),
		runtimeStorageSmokeLLFragments(),
	}
}

// runtimeStorageLayoutLLFragments returns storage layout fragments.
func runtimeStorageLayoutLLFragments() []string {
	return []string{
		"; kizu selfhost runtime storage ll v0\n",
		"%kizu.handle = type { ptr, i64 }\n",
		"%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }\n",
		"%kizu.rt.string = type { ptr, ptr, i64, i64 }\n",
		"%kizu.rt.map = type { ptr, i64, ptr, i64, i64, ptr, i64, i64 }\n",
		"%kizu.rt.arena = type { ptr, i64, i64, i1 }\n",
		"@.kizu.rt.arena_invalid_handle",
		"@.kizu.rt.array_smoke",
		"@.kizu.rt.invalid_array_element",
		"@.kizu.rt.string_smoke",
		"@.kizu.rt.map_key_alpha",
		"@.kizu.rt.map_key_beta",
		"@.kizu.rt.map_key_not_found",
		"@.kizu.rt.map_full",
		"@.kizu.rt.invalid_slice",
		"declare ptr @kizu_rt_alloc(ptr, i64)\n",
		"declare void @kizu_rt_free(ptr, ptr)\n",
		"declare void @llvm.memcpy.p0.p0.i64",
	}
}

// runtimeStorageFunctionLLFragments returns storage function fragments.
func runtimeStorageFunctionLLFragments() []string {
	return []string{
		"define %kizu.owned @kizu_rt_array_new",
		"define %kizu.error.void @kizu_rt_array_append",
		"define %kizu.error.slice.u8 @kizu_rt_array_at",
		"define %kizu.owned @kizu_rt_string_new",
		"define %kizu.error.void @kizu_rt_string_append_bytes",
		"define %kizu.error.void @kizu_rt_string_append_byte",
		"define %kizu.slice.u8 @kizu_rt_string_as_bytes",
		"define %kizu.owned @kizu_rt_map_new",
		"define %kizu.error.void @kizu_rt_map_insert",
		"define i1 @kizu_rt_map_key_equal",
		"define i1 @kizu_rt_map_contains",
		"define %kizu.error.i64 @kizu_rt_map_get_i64",
		"define %kizu.owned @kizu_rt_diagnostic_buffer_new",
		"define %kizu.error.void @kizu_rt_diagnostic_push",
		"define %kizu.owned @kizu_rt_arena_new",
		"define %kizu.handle @kizu_rt_arena_add",
		"define %kizu.error.slice.u8 @kizu_rt_arena_get",
		"define void @kizu_rt_arena_deinit",
		"define i1 @kizu_selfhost__runtime_array_first_payload_ok",
		"define i1 @kizu_selfhost__runtime_array_second_payload_ok",
		"define i1 @kizu_selfhost__runtime_invalid_array_element_message_ok",
		"define i1 @kizu_selfhost__runtime_array_oob_message_ok",
		"define i1 @kizu_selfhost__runtime_invalid_slice_message_ok",
		"define i64 @kizu_selfhost__runtime_string_invalid_smoke() {\n",
		"define i64 @kizu_selfhost__runtime_storage_smoke() {\n",
	}
}

// runtimeStorageSmokeLLFragments returns smoke call fragments.
func runtimeStorageSmokeLLFragments() []string {
	return []string{
		"call %kizu.error.void @kizu_rt_array_append",
		"call %kizu.error.slice.u8 @kizu_rt_array_at",
		"call %kizu.error.void @kizu_rt_string_append_bytes",
		"call %kizu.error.void @kizu_rt_string_append_byte",
		"call %kizu.slice.u8 @kizu_rt_string_as_bytes",
		"call i64 @kizu_selfhost__runtime_string_invalid_smoke",
		"call %kizu.error.void @kizu_rt_map_insert",
		"call i1 @kizu_rt_map_contains",
		"call %kizu.error.i64 @kizu_rt_map_get_i64",
		"call %kizu.error.void @kizu_rt_diagnostic_push",
		"call %kizu.handle @kizu_rt_arena_add",
		"call %kizu.error.slice.u8 @kizu_rt_arena_get",
		"call void @kizu_rt_arena_deinit",
	}
}

// countRuntimeStorageMetadataFailures checks reachable storage metadata and guards.
func countRuntimeStorageMetadataFailures(t *testing.T, metaContent string) int {
	t.Helper()
	requiredMeta := []string{
		"kizu-runtime-storage-v0\n",
		"abi selfhost-abi-v0\n",
		"ir target/selfhost/selfhost.ir\n",
		"manifest target/selfhost/selfhost.ir.manifest\n",
		"linked-llvm target/selfhost/selfhost.ll\n",
		"output target/selfhost/selfhost.storage.ll\n",
		"storage-backend selfhost-runtime-ll\n",
		"allocator-boundary explicit\n",
		"external @kizu_rt_alloc\n",
		"external @kizu_rt_free\n",
		"go-stdprim-storage none\n",
		"interpreter-storage none\n",
		"reachable array token-list\n",
		"reachable array ast-child-list\n",
		"array-storage copy-element-byte-buffer\n",
		"array-at returns-stored-element\n",
		"array-deinit releases-element-buffer\n",
		"array-invalid-element-diagnostic invalid array element\n",
		"array-oob-diagnostic array index out of bounds\n",
		"reachable string diagnostic-buffer\n",
		"reachable string path-buffer\n",
		"string-storage byte-buffer\n",
		"string-as-bytes returns-stored-bytes\n",
		"string-deinit releases-byte-buffer\n",
		"string-invalid-slice-diagnostic invalid slice\n",
		"reachable map resolver-symbol-table\n",
		"reachable map type-symbol-table\n",
		"reachable map ownership-state-table\n",
		"map-storage string-key-i64-two-entry\n",
		"map-key-ownership copies-key-bytes\n",
		"map-missing-key-diagnostic Map.get key not found\n",
		"map-capacity-diagnostic map capacity exceeded\n",
		"reachable diagnostic compiler-failure-buffer\n",
		"reachable arena ast-node-storage\n",
		"reachable handle ast-node-id\n",
		"arena-allocator-boundary explicit\n",
		"arena-handle-provenance checked\n",
		"arena-invalid-handle-diagnostic invalid arena handle\n",
		"deferred box issue-496\n",
	}
	for _, fragment := range requiredMeta {
		if !strings.Contains(metaContent, fragment) {
			t.Errorf("runtime storage metadata missing %q:\n%s", fragment, metaContent)
			return 1
		}
	}
	return 0
}

// countRuntimeStorageLinkSmokeFailures links and runs the storage smoke.
func countRuntimeStorageLinkSmokeFailures(t *testing.T) int {
	t.Helper()
	failures := runRuntimeStorageSmoke(
		t,
		"target/selfhost/selfhost.storage.ll",
		"target/selfhost/selfhost.host.ll",
	)
	failures += runRuntimeStorageCountingSmoke(t, "target/selfhost/selfhost.storage.ll")
	return failures
}

// runRuntimeStorageSmoke links a storage artifact with host capabilities.
func runRuntimeStorageSmoke(t *testing.T, storagePath string, hostPath string) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("runtime storage smoke requires clang: %v", err)
		return 1
	}
	tempDir := t.TempDir()
	harnessPath := filepath.Join(tempDir, "runtime_storage_main.c")
	exePath := filepath.Join(tempDir, "runtime-storage-smoke")
	if err := os.WriteFile(harnessPath, []byte(runtimeStorageHarnessSource), 0o644); err != nil {
		t.Errorf("write runtime storage harness: %v", err)
		return 1
	}
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		storagePath,
		hostPath,
		"selfhost/runtime/selfhost.hosted.c",
		harnessPath,
		"-o",
		exePath,
	)
	compile.Dir = "../.."
	if out, err := compile.CombinedOutput(); err != nil {
		t.Errorf("compile runtime storage smoke: %v\n%s", err, out)
		return 1
	}
	run := exec.Command(exePath)
	run.Dir = "../.."
	if out, err := run.CombinedOutput(); err != nil {
		t.Errorf("run runtime storage smoke: %v\n%s", err, out)
		return 1
	}
	return 0
}

// runRuntimeStorageCountingSmoke checks storage cleanup without host wrappers.
func runRuntimeStorageCountingSmoke(t *testing.T, storagePath string) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("runtime storage counting smoke requires clang: %v", err)
		return 1
	}
	tempDir := t.TempDir()
	harnessPath := filepath.Join(tempDir, "runtime_storage_counting_main.c")
	exePath := filepath.Join(tempDir, "runtime-storage-counting-smoke")
	harness := []byte(runtimeStorageCountingHarnessSource)
	if err := os.WriteFile(harnessPath, harness, 0o644); err != nil {
		t.Errorf("write runtime storage counting harness: %v", err)
		return 1
	}
	compile := exec.Command(clang, "-Wno-override-module", storagePath, harnessPath, "-o", exePath)
	compile.Dir = "../.."
	if out, err := compile.CombinedOutput(); err != nil {
		t.Errorf("compile runtime storage counting smoke: %v\n%s", err, out)
		return 1
	}
	run := exec.Command(exePath)
	run.Dir = "../.."
	if out, err := run.CombinedOutput(); err != nil {
		t.Errorf("run runtime storage counting smoke: %v\n%s", err, out)
		return 1
	}
	return 0
}

// countHostCapabilityValidationFailures validates the non-Go host boundary artifact.
func countHostCapabilityValidationFailures(t *testing.T, llContent string, metaContent string) int {
	t.Helper()
	if failures := countHostCapabilityLLFailures(t, llContent); failures > 0 {
		return failures
	}
	failures := countHostCapabilityMetadataFailures(t, metaContent)
	failures += countHostCapabilityLinkSmokeFailures(t)
	return failures
}

// countHostCapabilityLLFailures checks host capability wrappers and smoke calls.
func countHostCapabilityLLFailures(t *testing.T, llContent string) int {
	t.Helper()
	requiredLL := []string{
		"; kizu selfhost host capabilities ll v0\n",
		"declare ptr @kizu_host_page_allocator()\n",
		"declare ptr @kizu_host_io_blocking()\n",
		"declare ptr @kizu_host_alloc(ptr, i64)\n",
		"declare void @kizu_host_free(ptr, ptr)\n",
		"declare void @kizu_host_fs_exists",
		"declare void @kizu_host_fs_metadata",
		"declare void @kizu_host_fs_read_dir",
		"declare void @kizu_host_fs_read_file",
		"declare void @kizu_host_fs_write_file",
		"declare void @kizu_host_fs_create_dir",
		"declare void @kizu_host_fs_rename",
		"declare void @kizu_host_io_write_stdout",
		"declare void @kizu_host_io_write_stderr",
		"declare i64 @kizu_host_process_arg_count()\n",
		"declare void @kizu_host_process_arg",
		"declare void @kizu_host_process_env",
		"declare i64 @kizu_host_process_exit_code",
		"declare void @kizu_host_process_exit(i64) noreturn\n",
		"define %kizu.owned @kizu_rt_mem_page_allocator()",
		"define %kizu.owned @kizu_rt_io_blocking()",
		"define ptr @kizu_rt_alloc",
		"define void @kizu_rt_free",
		"define %kizu.error.bool @kizu_rt_fs_exists",
		"define %kizu.error.metadata @kizu_rt_fs_metadata",
		"define %kizu.error.owned @kizu_rt_fs_read_dir",
		"define %kizu.error.slice.u8 @kizu_rt_fs_read_file",
		"define %kizu.error.void @kizu_rt_fs_write_file",
		"define %kizu.error.void @kizu_rt_fs_create_dir",
		"define %kizu.error.void @kizu_rt_fs_rename",
		"define %kizu.error.void @kizu_rt_io_write_stdout",
		"define %kizu.error.void @kizu_rt_io_write_stderr",
		"define i64 @kizu_rt_process_arg_count()",
		"define %kizu.error.slice.u8 @kizu_rt_process_arg",
		"define %kizu.error.slice.u8 @kizu_rt_process_env",
		"define i64 @kizu_rt_process_exit_code",
		"define void @kizu_rt_process_exit",
		"define i64 @kizu_selfhost__host_capability_smoke()",
	}
	for _, fragment := range requiredLL {
		if !strings.Contains(llContent, fragment) {
			t.Errorf("host capability artifact missing %q:\n%s", fragment, llContent)
			return 1
		}
	}
	forbiddenMarkers := []string{"std.builtin", "stdprim", "internal/interp", "internal global"}
	for _, forbidden := range forbiddenMarkers {
		if strings.Contains(llContent, forbidden) {
			t.Errorf("host capability artifact contains Go fallback marker %q", forbidden)
			return 1
		}
	}
	return 0
}

// countHostCapabilityMetadataFailures checks host boundary metadata and guards.
func countHostCapabilityMetadataFailures(t *testing.T, metaContent string) int {
	t.Helper()
	requiredMeta := []string{
		"kizu-host-capabilities-v0\n",
		"abi selfhost-abi-v0\n",
		"ir target/selfhost/selfhost.ir\n",
		"manifest target/selfhost/selfhost.ir.manifest\n",
		"linked-llvm target/selfhost/selfhost.ll\n",
		"output target/selfhost/selfhost.host.ll\n",
		"host-implementation selfhost/runtime/selfhost.hosted.c\n",
		"host-smoke reads selfhost/kizu.toml\n",
		"host-smoke reads selfhost/src\n",
		"host-smoke writes target/selfhost/host-smoke.status\n",
		"host-capabilities selfhost-host-v0\n",
		"allocator-boundary explicit\n",
		"io-boundary explicit\n",
		"filesystem-boundary explicit\n",
		"process-boundary explicit\n",
		"stdout-boundary explicit\n",
		"stderr-boundary explicit\n",
		"exit-boundary explicit\n",
		"external @kizu_host_page_allocator\n",
		"external @kizu_host_io_blocking\n",
		"external @kizu_host_alloc\n",
		"external @kizu_host_free\n",
		"external @kizu_host_fs_exists\n",
		"external @kizu_host_fs_metadata\n",
		"external @kizu_host_fs_read_dir\n",
		"external @kizu_host_fs_read_file\n",
		"external @kizu_host_fs_write_file\n",
		"external @kizu_host_fs_create_dir\n",
		"external @kizu_host_fs_rename\n",
		"external @kizu_host_io_write_stdout\n",
		"external @kizu_host_io_write_stderr\n",
		"external @kizu_host_process_arg_count\n",
		"external @kizu_host_process_arg\n",
		"external @kizu_host_process_env\n",
		"external @kizu_host_process_exit_code\n",
		"external @kizu_host_process_exit\n",
		"external @kizu_host_trap\n",
		"go-stdprim-host none\n",
		"interpreter-host none\n",
		"linker-process deferred issue-459\n",
	}
	for _, fragment := range requiredMeta {
		if !strings.Contains(metaContent, fragment) {
			t.Errorf("host capability metadata missing %q:\n%s", fragment, metaContent)
			return 1
		}
	}
	return 0
}

// countHostCapabilityLinkSmokeFailures links and runs the hosted no-Go boundary smoke.
func countHostCapabilityLinkSmokeFailures(t *testing.T) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("host capability smoke requires clang: %v", err)
		return 1
	}
	tempDir := t.TempDir()
	harnessPath := filepath.Join(tempDir, "host_smoke_main.c")
	exePath := filepath.Join(tempDir, "host-smoke")
	if err := os.WriteFile(harnessPath, []byte(hostCapabilityHarnessSource), 0o644); err != nil {
		t.Errorf("write host capability harness: %v", err)
		return 1
	}
	if err := os.Remove("../../target/selfhost/host-smoke.status"); err != nil &&
		!os.IsNotExist(err) {
		t.Errorf("remove old host smoke target: %v", err)
		return 1
	}
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		"target/selfhost/selfhost.host.ll",
		"selfhost/runtime/selfhost.hosted.c",
		harnessPath,
		"-o",
		exePath,
	)
	compile.Dir = "../.."
	if out, err := compile.CombinedOutput(); err != nil {
		t.Errorf("compile host capability smoke: %v\n%s", err, out)
		return 1
	}
	return countHostCapabilitySmokeRunFailures(t, exePath)
}

// countHostCapabilitySmokeRunFailures runs the linked host capability smoke binary.
func countHostCapabilitySmokeRunFailures(t *testing.T, exePath string) int {
	t.Helper()
	run := exec.Command(exePath, "check", "selfhost")
	run.Dir = "../.."
	run.Env = append(os.Environ(), "KIZU_HOST_SMOKE=ok")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Errorf("run host capability smoke: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
		return 1
	}
	if stdout.String() != "selfhost-host:ok\n" {
		t.Errorf("host capability stdout mismatch: %q", stdout.String())
		return 1
	}
	if stderr.String() != "selfhost-host:diag\n" {
		t.Errorf("host capability stderr mismatch: %q", stderr.String())
		return 1
	}
	status, err := os.ReadFile("../../target/selfhost/host-smoke.status")
	if err != nil {
		t.Errorf("read host capability smoke output: %v", err)
		return 1
	}
	if string(status) != "selfhost-host:ok\n" {
		t.Errorf("host capability file output mismatch: %q", string(status))
		return 1
	}
	return 0
}

// countHostedCompilerCLISmokeFailures links and runs the generated CLI artifact.
func countHostedCompilerCLISmokeFailures(t *testing.T) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("hosted compiler CLI smoke requires clang: %v", err)
		return 1
	}
	tempDir := t.TempDir()
	harnessPath := filepath.Join(tempDir, "hosted_cli_main.c")
	exePath := filepath.Join(tempDir, "selfhost-cli")
	if err := os.WriteFile(harnessPath, []byte(hostedCompilerCLIHarnessSource), 0o644); err != nil {
		t.Errorf("write hosted compiler CLI harness: %v", err)
		return 1
	}
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.host.ll",
		"selfhost/runtime/selfhost.hosted.c",
		harnessPath,
		"-o",
		exePath,
	)
	compile.Dir = "../.."
	if out, err := compile.CombinedOutput(); err != nil {
		t.Errorf("compile hosted compiler CLI smoke: %v\n%s", err, out)
		return 1
	}
	failures := countHostedCompilerCLICheckFailures(t, exePath)
	failures += countHostedCompilerCLIStageFailures(t, exePath)
	failures += countHostedCompilerCLIUnsupportedFailures(t, exePath)
	return failures
}

// countHostedCompilerCLICheckFailures runs `check selfhost` through the artifact.
func countHostedCompilerCLICheckFailures(t *testing.T, exePath string) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "check", "selfhost")
	if code != 0 {
		t.Errorf("hosted compiler check exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "check: ok\n" {
		t.Errorf("hosted compiler check stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler check stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIStageFailures runs `stage selfhost` through the artifact.
func countHostedCompilerCLIStageFailures(t *testing.T, exePath string) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "stage", "selfhost")
	if code != 0 {
		t.Errorf("hosted compiler stage exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	want := strings.Join([]string{
		"stage: ok",
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.ll.meta",
		"target/selfhost/selfhost.storage.ll",
		"target/selfhost/selfhost.storage.ll.meta",
		"target/selfhost/selfhost.host.ll",
		"target/selfhost/selfhost.host.ll.meta",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("hosted compiler stage stdout mismatch:\nwant:\n%s\ngot:\n%s", want, stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler stage stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIUnsupportedFailures checks unsupported commands fail early.
func countHostedCompilerCLIUnsupportedFailures(t *testing.T, exePath string) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "bad", "selfhost")
	if code != 64 {
		t.Errorf("hosted compiler unsupported exit=%d\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler unsupported stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "unsupported selfhost command\n" {
		t.Errorf("hosted compiler unsupported stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// runHostedCompilerCLI captures stdout, stderr, and exit code for the CLI artifact.
func runHostedCompilerCLI(t *testing.T, exePath string, args ...string) (string, string, int) {
	t.Helper()
	run := exec.Command(exePath, args...)
	run.Dir = "../.."
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err := run.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Errorf("run hosted compiler CLI: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
		return stdout.String(), stderr.String(), -1
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

const hostCapabilityHarnessSource = `
#include <stdint.h>

void kizu_host_init(int argc, char **argv);
int64_t kizu_selfhost__host_capability_smoke(void);

int main(int argc, char **argv) {
    kizu_host_init(argc, argv);
    return kizu_selfhost__host_capability_smoke() == 0 ? 0 : 1;
}
`

const runtimeStorageHarnessSource = `
#include <stdint.h>

void kizu_host_init(int argc, char **argv);
int64_t kizu_selfhost__runtime_storage_smoke(void);

int main(int argc, char **argv) {
    kizu_host_init(argc, argv);
    return kizu_selfhost__runtime_storage_smoke() == 0 ? 0 : 1;
}
`

const runtimeStorageCountingHarnessSource = `
#include <stdint.h>
#include <stdlib.h>

static int64_t kizu_allocs;
static int64_t kizu_frees;

void *kizu_rt_alloc(void *allocator, int64_t bytes) {
    (void)allocator;
    kizu_allocs++;
    return calloc(1, bytes <= 0 ? 1 : (size_t)bytes);
}

void kizu_rt_free(void *allocator, void *value) {
    (void)allocator;
    if (value != NULL) {
        kizu_frees++;
    }
    free(value);
}

int64_t kizu_selfhost__runtime_storage_smoke(void);

int main(void) {
    if (kizu_selfhost__runtime_storage_smoke() != 0) {
        return 1;
    }
    if (kizu_allocs != kizu_frees) {
        return 2;
    }
    return 0;
}
`

const hostedCompilerCLIHarnessSource = `
#include <stdint.h>

void kizu_host_init(int argc, char **argv);
int64_t kizu_selfhost__cli_main(void);

int main(int argc, char **argv) {
    kizu_host_init(argc, argv);
    return (int)kizu_selfhost__cli_main();
}
`

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
