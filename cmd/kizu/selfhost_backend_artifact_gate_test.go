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
	if failures := countWithIsolatedSelfhostTarget(
		t,
		func() int { return countSelfhostBackendArtifactGateFailures(t) },
	); failures > 0 {
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
		"arena-storage ast-node-inline-two-slot\n",
		"arena-get returns-stored-payload\n",
		"arena-deinit releases-inline-payload-storage\n",
		"arena-payload-constraints ast-node-copy-scalar-view\n",
		"arena-allocator-boundary explicit\n",
		"arena-handle-provenance checked\n",
		"arena-invalid-handle-diagnostic invalid arena handle\n",
		"abi-record summary-i64-slice\n",
		"abi-error-union summary-success-failure\n",
		"abi-call direct-record-roundtrip\n",
		"deferred tagged-union-payload issue-495\n",
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
	for _, fragment := range requiredLLVMFragments() {
		if !strings.Contains(llContent, fragment) {
			t.Errorf("LLVM artifact missing %q:\n%s", fragment, llContent)
			return 1
		}
	}
	for _, fragment := range []string{
		"selfhost/tests/cli/run_hello.kizu",
		"selfhost/tests/cli/test_expect_ok.kizu",
		"selfhost/tests/cli/test_expect_failure.kizu",
	} {
		if strings.Contains(llContent, fragment) {
			t.Errorf("LLVM artifact keeps fixed CLI fixture path %q:\n%s", fragment, llContent)
			return 1
		}
	}
	for _, fragment := range forbiddenLLVMFragments() {
		if strings.Contains(llContent, fragment) {
			t.Errorf("LLVM artifact keeps source-shape gate %q:\n%s", fragment, llContent)
			return 1
		}
	}
	return countLLVMMetadataValidationFailures(t, metaContent)
}

// requiredLLVMFragments returns mandatory hosted compiler LLVM fragments.
func requiredLLVMFragments() []string {
	fragments := requiredLLVMRuntimeFragments()
	fragments = append(fragments, requiredLLVMCLIFragments()...)
	fragments = append(fragments, requiredLLVMExecutableFragments()...)
	return fragments
}

// requiredLLVMRuntimeFragments returns mandatory hosted runtime declarations.
func requiredLLVMRuntimeFragments() []string {
	return []string{
		"; kizu selfhost bootstrap ll v0\n",
		"source_filename = \"target/selfhost/selfhost.ir\"\n",
		"%kizu.slice.u8 = type { ptr, i64 }\n",
		"%kizu.owned = type { ptr }\n",
		"%kizu.handle = type { ptr, i64 }\n",
		"%kizu.error.bool = type { i1, i1, %kizu.slice.u8 }\n",
		"%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }\n",
		"declare %kizu.owned @kizu_rt_mem_page_allocator()\n",
		"declare ptr @kizu_rt_alloc(ptr, i64)\n",
		"declare void @kizu_rt_free(ptr, ptr)\n",
		"declare void @llvm.memcpy.p0.p0.i64",
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
	}
}

// requiredLLVMCLIFragments returns mandatory hosted CLI helper fragments.
func requiredLLVMCLIFragments() []string {
	fragments := requiredLLVMCLIRunTestFragments()
	return append(fragments, requiredLLVMCLICheckFragments()...)
}

// requiredLLVMCLIRunTestFragments returns hosted run/test CLI fragments.
func requiredLLVMCLIRunTestFragments() []string {
	return []string{
		"define i1 @kizu_selfhost__slice_equal",
		"define i1 @kizu_selfhost__slice_starts_with_dash",
		"define %kizu.error.void @kizu_selfhost__write_concat3",
		"define %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"define %kizu.error.void @kizu_selfhost__write_concat5",
		"define %kizu.error.void @kizu_selfhost__write_concat9",
		"define %kizu.error.slice.u8 @kizu_selfhost__i64_decimal",
		"define %kizu.error.slice.u8 @kizu_selfhost__artifact_path",
		"define i64 @kizu_selfhost__parse_buffer_append",
		"define i64 @kizu_selfhost__parse_next_semantic_token_index",
		"define %kizu.error.slice.u8 @kizu_selfhost__parse_format_alloc",
		"define i1 @kizu_selfhost__parse_format_write",
		"define i1 @kizu_selfhost__parse_format_file_write",
		"define i64 @kizu_selfhost__parse_skip_comment_or_self",
		"define i64 @kizu_selfhost__parse_missing_expr_index",
		"define i64 @kizu_selfhost__parse_missing_assign_index",
		"@.kizu.cli.parse_expected_expr_prefix",
		"@.kizu.cli.parse_expected_assign_prefix",
		"@.kizu.cli.fmt",
		"@.kizu.cli.fmt_write",
		"@.kizu.cli.fmt_write_short",
		"%argc_is_three = icmp eq i64 %argc, 3",
		"%parse_format_ok = call i1 @kizu_selfhost__parse_format_write",
		"%is_fmt = call i1 @kizu_selfhost__slice_equal",
		"dispatch_fmt:",
		"%fmt_format_ok = call i1 @kizu_selfhost__parse_format_write",
		"dispatch_fmt_write_arg:",
		"dispatch_fmt_write:",
		"%fmt_write_format_ok = call i1 @kizu_selfhost__parse_format_file_write",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_parse_run_print_payload",
		"define i1 @kizu_selfhost__cli_parse_run_return_void_ok",
		"define i1 @kizu_selfhost__cli_is_supported_run_print_payload",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_run_payload_llvm_c_string",
		"%run_executable = call %kizu.selfhost.executable " +
			"@kizu_selfhost__cli_run_executable",
		"%test_executable = call %kizu.selfhost.executable " +
			"@kizu_selfhost__cli_test_executable",
		"%run_print_payload_llvm_result = call %kizu.error.slice.u8 " +
			"@kizu_selfhost__cli_run_payload_llvm_c_string",
		"%run_print_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%run_return_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"define i64 @kizu_selfhost__cli_parse_test_expect_value",
		"%test_ok_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%test_failure_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%run_print_ll_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"%run_print_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"%run_return_ll_write = call %kizu.error.void @kizu_selfhost__write_concat3",
		"%run_return_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"%test_ok_ll_write = call %kizu.error.void @kizu_selfhost__write_concat3",
		"%test_ok_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"%test_failure_ll_write = call %kizu.error.void @kizu_selfhost__write_concat3",
		"%test_failure_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
	}
}

// requiredLLVMCLICheckFragments returns hosted check diagnostic fragments.
func requiredLLVMCLICheckFragments() []string {
	return []string{
		"%parse_missing_index = call i64 @kizu_selfhost__parse_missing_expr_index",
		"%parse_missing_assign_index = call i64 @kizu_selfhost__parse_missing_assign_index",
		"%check_missing_index = call i64 @kizu_selfhost__parse_missing_expr_index",
		"%check_parse_ok = call i1 @kizu_selfhost__parse_write_missing_expr",
		"%check_missing_assign = call i64 @kizu_selfhost__parse_missing_assign_index",
		"%check_assign_ok = call i1 @kizu_selfhost__parse_write_missing_assign",
		"define %kizu.slice.u8 @kizu_selfhost__cli_moved_value_name",
		"@.kizu.cli.move_prefix",
		"@.kizu.cli.move_suffix",
		"%check_moved_name = call %kizu.slice.u8 @kizu_selfhost__cli_moved_value_name",
		"br i1 %check_has_moved_name, label %check_file_move_error, label %check_file_ok",
		"%run_check_moved_name = call %kizu.slice.u8 @kizu_selfhost__cli_moved_value_name",
		"br i1 %run_check_has_moved_name, label %run_check_move_error, label %run_match_source",
		"%test_check_moved_name = call %kizu.slice.u8 @kizu_selfhost__cli_moved_value_name",
		"br i1 %test_check_has_moved_name, label %test_check_move_error, label %test_match_source",
		"define i64 @kizu_selfhost__cli_main() {\n",
		"define i64 @kizu_selfhost__smoke() {\n",
	}
}

// requiredLLVMExecutableFragments returns mandatory hosted executable fragments.
func requiredLLVMExecutableFragments() []string {
	return []string{
		"%kizu.selfhost.executable.ast = type { i64, %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.selfhost.executable = type { i64, %kizu.slice.u8 }",
		"define %kizu.selfhost.executable.ast " +
			"@kizu_selfhost__cli_parse_run_executable_ast",
		"define %kizu.selfhost.executable.ast " +
			"@kizu_selfhost__cli_parse_test_executable_ast",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_lower_run_executable_ast",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_lower_test_executable_ast",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_run_executable",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_test_executable",
	}
}

// forbiddenLLVMFragments returns source-shape gates removed from the hosted CLI.
func forbiddenLLVMFragments() []string {
	return []string{
		"@.kizu.cli.main_fn_pattern",
		"@.kizu.cli.run_hello_pattern",
		"define i1 @kizu_selfhost__cli_run_prints_hello",
		"@.kizu.cli.run_hello_ll_suffix",
		"@.kizu.cli.test_expect_true_pattern",
		"@.kizu.cli.test_expect_false_pattern",
		"define %kizu.slice.u8 @kizu_selfhost__moved_value_name",
		"%parse_small",
		"%corpus_small",
		"%main_fn_found",
		"label %check_file_shape",
		"%run_hello_found = call i1 @kizu_selfhost__slice_contains",
		"%test_ok_found = call i1 @kizu_selfhost__slice_contains",
		"%test_failure_found = call i1 @kizu_selfhost__slice_contains",
		"target/selfhost/run/runtime.kizu",
		"target/selfhost/run/run_hello.ll",
		"target/selfhost/run/run_hello.ll.meta",
		"target/selfhost/test/expectoksrc.kizu",
		"target/selfhost/test/expectfailureabc.kizu",
		"target/selfhost/test/test_expect_ok.ll",
		"target/selfhost/test/test_expect_ok.ll.meta",
		"target/selfhost/test/test_expect_failure.ll",
		"target/selfhost/test/test_expect_failure.ll.meta",
	}
}

// countLLVMMetadataValidationFailures validates artifact metadata for stage comparison.
func countLLVMMetadataValidationFailures(t *testing.T, metaContent string) int {
	t.Helper()
	for _, fragment := range requiredLLVMMetadataFragments() {
		if !strings.Contains(metaContent, fragment) {
			t.Errorf("LLVM artifact metadata missing %q:\n%s", fragment, metaContent)
			return 1
		}
	}
	return countForbiddenCLIMetadataFailures(t, metaContent)
}

// requiredLLVMMetadataFragments returns mandatory backend artifact metadata.
func requiredLLVMMetadataFragments() []string {
	fragments := []string{
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
		"abi-record summary-i64-slice\n",
		"abi-error-union summary-success-failure\n",
		"abi-call direct-record-roundtrip\n",
		"go-stdprim-storage none\n",
		"go-stdprim-host none\n",
		"linker-process deferred issue-459\n",
		"backend-input ir-contract selfhost-checked-package-v1\n",
		"backend-input checked-entry selfhost::cli_main\n",
		"backend-input hosted-entry @kizu_selfhost__cli_main\n",
		"backend-input hosted-smoke @kizu_selfhost__smoke\n",
		"backend-input executable-contract-source data selfhost::backend::data\n",
		"backend-input executable-contract-source lowering selfhost::backend::executable\n",
	}
	fragments = append(fragments, requiredLLVMMetadataSelectedSignatureFragments()...)
	fragments = append(fragments, requiredLLVMMetadataSelectedBodyFragments()...)
	fragments = append(fragments, requiredLLVMMetadataSelectedHelperBodyFragments()...)
	fragments = append(fragments, requiredLLVMMetadataSelectedBodyParsingFragments()...)
	fragments = append(fragments, requiredLLVMMetadataSelectedBodyLoweringFragments()...)
	fragments = append(fragments, requiredLLVMMetadataExecutableABIFragments()...)
	fragments = append(fragments, []string{
		"entry @kizu_selfhost__cli_main\n",
		"cli-command check selfhost\n",
		"cli-command stage selfhost\n",
		"cli-command check file source\n",
		"cli-command parse file source\n",
		"cli-command fmt file source\n",
		"cli-command run file executable top-level-print-or-return\n",
		"cli-command test file executable top-level-expect\n",
		"cli-parity-manifest selfhost/tests/cli/parse-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/check-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/run-parity.tsv\n",
		"cli-parity-manifest selfhost/tests/cli/test-parity.tsv\n",
		"cli-hosted-smoke no-go\n",
		"validation go test ./cmd/kizu -run TestSelfhostBackendArtifactGate\n",
	}...)
	fragments = append(fragments, requiredLLVMMetadataExternalFragments()...)
	return append(fragments, []string{
		"unsupported-policy blocker\n",
		"deferred tagged-union-payload issue-495\n",
	}...)
}

// requiredLLVMMetadataSelectedSignatureFragments returns executable signature inputs.
func requiredLLVMMetadataSelectedSignatureFragments() []string {
	return []string{
		"backend-input function-signature-return selfhost::cli::execute::" +
			"run_file_cli !i64\n",
		"backend-input function-signature-param selfhost::cli::execute::" +
			"run_file_cli 0 allocator:runtime:Allocator\n",
		"backend-input function-signature-return selfhost::backend::executable::" +
			"lower_run_executable !data::Executable\n",
		"backend-input function-signature-param selfhost::backend::executable::" +
			"lower_run_executable 1 ast:runtime:std::kizu::ast::Ast\n",
		"backend-input function-signature-return selfhost::backend::executable::" +
			"lower_run_executable_ast data::Executable\n",
		"backend-input function-signature-param selfhost::backend::executable::" +
			"parse_expect_call_ast 3 args:runtime:std::kizu::ast::ChildRange\n",
		"backend-input function-signature-return selfhost::backend::hosted::" +
			"emit_run_executable_artifact !data::RunArtifact\n",
		"backend-input function-signature-param selfhost::backend::hosted::" +
			"emit_run_executable_artifact 3 executable:runtime:data::Executable\n",
	}
}

// requiredLLVMMetadataSelectedBodyFragments returns executable body IR facts.
func requiredLLVMMetadataSelectedBodyFragments() []string {
	return nil
}

// requiredLLVMMetadataSelectedHelperBodyFragments returns executable helper body inputs.
func requiredLLVMMetadataSelectedHelperBodyFragments() []string {
	return nil
}

// requiredLLVMMetadataSelectedBodyParsingFragments returns body parser inputs.
func requiredLLVMMetadataSelectedBodyParsingFragments() []string {
	return nil
}

// requiredLLVMMetadataSelectedBodyLoweringFragments returns body lowering inputs.
func requiredLLVMMetadataSelectedBodyLoweringFragments() []string {
	return nil
}

// requiredLLVMMetadataExternalFragments returns runtime symbols the artifact declares.
func requiredLLVMMetadataExternalFragments() []string {
	return []string{
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
		"external @kizu_rt_alloc\n",
		"external @kizu_rt_free\n",
		"external @llvm.memcpy.p0.p0.i64\n",
	}
}

// requiredLLVMMetadataExecutableABIFragments returns executable layout metadata facts.
func requiredLLVMMetadataExecutableABIFragments() []string {
	return []string{
		"backend-input hosted-executable-abi executable-result-layout-v1\n",
		"backend-input executable-ast-layout kind:i64 callee:[]u8 payload:[]u8\n",
		"backend-input executable-layout kind:i64 stdout_payload:[]u8\n",
		"backend-input executable-ast-kind Unsupported 0\n",
		"backend-input executable-ast-kind ExprStmt 1\n",
		"backend-input executable-ast-kind Call 2\n",
		"backend-input executable-ast-kind StringLiteral 3\n",
		"backend-input executable-ast-kind BoolLiteral 4\n",
		"backend-input executable-ast-kind RunReturnVoid 5\n",
		"backend-input executable-kind Unsupported 0\n",
		"backend-input executable-kind RunPrintString 1\n",
		"backend-input executable-kind RunReturnVoid 2\n",
		"backend-input executable-kind Call 3\n",
	}
}

// countForbiddenCLIMetadataFailures rejects stale fixed-path CLI metadata.
func countForbiddenCLIMetadataFailures(t *testing.T, metaContent string) int {
	t.Helper()
	for _, fragment := range []string{
		"cli-command check examples/hello.kizu\n",
		"cli-command check examples/negative/moved_value.kizu\n",
		"cli-command check source-shape",
		"cli-command parse selfhost/tests/cli/parse_ok_minimal.kizu\n",
		"cli-command parse single-line source",
		"cli-command parse source-shape print-call",
		"cli-command parse source-shape testing-expect-ok",
		"cli-command parse source-shape testing-expect-failure",
		"cli-command parse source-shape moved-value-declarations",
		"cli-command parse source-shape missing-expression",
		"cli-command parse source-shape minimal-main-return\n",
		"cli-command run source-shape",
		"cli-command test source-shape",
	} {
		if strings.Contains(metaContent, fragment) {
			t.Errorf("LLVM artifact metadata keeps fixed CLI path %q:\n%s", fragment, metaContent)
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
		"%kizu.record.abi.summary = type { i64, %kizu.slice.u8 }\n",
		"%kizu.error.record.abi.summary = type { i1, %kizu.record.abi.summary, %kizu.slice.u8 }\n",
		"%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }\n",
		"%kizu.rt.string = type { ptr, ptr, i64, i64 }\n",
		"%kizu.rt.map = type { ptr, i64, ptr, i64, i64, ptr, i64, i64 }\n",
		"%kizu.rt.arena = type { ptr, i64, i64, [48 x i8] }\n",
		"@.kizu.rt.arena_invalid_handle",
		"@.kizu.rt.abi_summary_name",
		"@.kizu.rt.abi_failure",
		"@.kizu.rt.arena_smoke",
		"@.kizu.rt.arena_smoke_second",
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
		"define %kizu.record.abi.summary @kizu_selfhost__abi_summary_make",
		"define %kizu.record.abi.summary @kizu_selfhost__abi_summary_passthrough",
		"define %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_success",
		"define %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_failure",
		"define i64 @kizu_selfhost__runtime_abi_roundtrip_smoke() {\n",
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
		"call %kizu.record.abi.summary @kizu_selfhost__abi_summary_passthrough",
		"call i64 @kizu_selfhost__runtime_abi_roundtrip_smoke",
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
		"arena-storage ast-node-inline-two-slot\n",
		"arena-get returns-stored-payload\n",
		"arena-deinit releases-inline-payload-storage\n",
		"arena-payload-constraints ast-node-copy-scalar-view\n",
		"arena-allocator-boundary explicit\n",
		"arena-handle-provenance checked\n",
		"arena-invalid-handle-diagnostic invalid arena handle\n",
		"abi-record summary-i64-slice\n",
		"abi-error-union summary-success-failure\n",
		"abi-call direct-record-roundtrip\n",
		"deferred tagged-union-payload issue-495\n",
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
	failures += countHostedCompilerCLIParseFailures(t, exePath)
	failures += countHostedCompilerCLIFmtFailures(t, exePath)
	failures += countHostedCompilerCLIFmtWriteFailures(t, exePath)
	failures += countHostedCompilerCLIRunFailures(t, exePath)
	failures += countHostedCompilerCLITestFailures(t, exePath)
	failures += countHostedCompilerCLIUnsupportedFailures(t, exePath)
	return failures
}

// countHostedCompilerCLICheckFailures runs check commands through the artifact.
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
	return countHostedCompilerCLIFileCheckFailures(t, exePath)
}

// countHostedCompilerCLIFileCheckFailures checks an arbitrary readable source.
func countHostedCompilerCLIFileCheckFailures(t *testing.T, exePath string) int {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "hosted_check_generic.kizu")
	source := "fn main() {\n    // let value = ;\n    print(\"checked from temp\");\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted check smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "check", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler file check exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "check: ok\n" {
		t.Errorf("hosted compiler file check stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler file check stderr mismatch: %q", stderr)
		return 1
	}
	failures := countHostedCompilerCLIRealSourceCheckFailures(t, exePath)

	failures += countHostedCompilerCLIMovedCheckFailures(t, exePath, dir)

	invalidPath := filepath.Join(dir, "hosted_check_invalid.kizu")
	invalidSource := "fn main() { let value = ; }\n"
	if err := os.WriteFile(invalidPath, []byte(invalidSource), 0o644); err != nil {
		t.Errorf("write hosted check invalid source: %v", err)
		return 1
	}
	expectedStderr := "error: expected expression, got ; at 1:25\nerror: parse failed\n"
	failures += countHostedCompilerCLIExpectedFailure(
		t,
		exePath,
		"check",
		invalidPath,
		expectedStderr,
		"invalid file check",
	)

	invalidAssignPath := filepath.Join(dir, "hosted_check_missing_assign.kizu")
	invalidAssignSource := "fn main() {\n    let value;\n}\n"
	if err := os.WriteFile(invalidAssignPath, []byte(invalidAssignSource), 0o644); err != nil {
		t.Errorf("write hosted check missing assign source: %v", err)
		return 1
	}
	expectedAssignStderr := "error: expected assign, got ; at 2:14\nerror: parse failed\n"
	failures += countHostedCompilerCLIExpectedFailure(
		t,
		exePath,
		"check",
		invalidAssignPath,
		expectedAssignStderr,
		"missing assign check",
	)
	return failures
}

// countHostedCompilerCLIMovedCheckFailures checks token-spaced moved diagnostics.
func countHostedCompilerCLIMovedCheckFailures(t *testing.T, exePath string, dir string) int {
	t.Helper()
	movedPath := filepath.Join(dir, "hosted_check_moved_spacing.kizu")
	movedSource := strings.Join([]string{
		"struct Name { value: []u8 }",
		"fn take(name: Name) { print(name.value); }",
		"fn main() {",
		"    let name = Name { value: \"alice\" };",
		"    take ( name );",
		"    print ( name . value );",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(movedPath, []byte(movedSource), 0o644); err != nil {
		t.Errorf("write hosted check moved source: %v", err)
		return 1
	}
	return countHostedCompilerCLIExpectedFailure(
		t,
		exePath,
		"check",
		movedPath,
		"error: move error: moved value `name` was used at 6:13\n",
		"moved value spacing check",
	)
}

// countHostedCompilerCLIRealSourceCheckFailures checks a full selfhost source file.
func countHostedCompilerCLIRealSourceCheckFailures(t *testing.T, exePath string) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "check", "selfhost/src/main.kizu")
	if code != 0 {
		t.Errorf("hosted compiler real source check exit=%d\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
		return 1
	}
	if stdout != "check: ok\n" {
		t.Errorf("hosted compiler real source check stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler real source check stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIParseFailures runs generic parse source through the artifact.
func countHostedCompilerCLIParseFailures(t *testing.T, exePath string) int {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "hosted_parse_generic.kizu")
	source := "fn main() {\n    print(\"from temp\");\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted parse smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "parse", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler parse exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	expected := "fn main() { print(\"from temp\"); }\n"
	if stdout != expected {
		t.Errorf("hosted compiler parse stdout mismatch:\nwant:\n%s\ngot:\n%s", expected, stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler parse stderr mismatch: %q", stderr)
		return 1
	}
	failures := countHostedCompilerCLICommentParseFailures(t, exePath, dir, expected)
	failures += countHostedCompilerCLIBorrowParseFailures(t, exePath, dir)
	failures += countHostedCompilerCLIRealSourceParseFailures(t, exePath)

	invalidAssignPath := filepath.Join(dir, "hosted_parse_missing_assign.kizu")
	invalidAssignSource := "fn main() {\n    let value;\n}\n"
	if err := os.WriteFile(invalidAssignPath, []byte(invalidAssignSource), 0o644); err != nil {
		t.Errorf("write hosted parse missing assign source: %v", err)
		return 1
	}
	expectedStderr := "error: expected assign, got ; at 2:14\nerror: parse failed\n"
	failures += countHostedCompilerCLIExpectedFailure(
		t,
		exePath,
		"parse",
		invalidAssignPath,
		expectedStderr,
		"missing assign parse",
	)
	return failures
}

// countHostedCompilerCLIBorrowParseFailures checks explicit &var call arguments do
// not trip missing-assign diagnostics in the hosted parse path.
func countHostedCompilerCLIBorrowParseFailures(t *testing.T, exePath string, dir string) int {
	t.Helper()
	sourcePath := filepath.Join(dir, "hosted_parse_borrow_arg.kizu")
	source := "fn take(report: &var Report) {}\nfn main() {\n    take(&var report);\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted parse borrow source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "parse", sourcePath)
	if code != 0 {
		t.Errorf(
			"hosted compiler parse borrow arg exit=%d\nstdout:\n%s\nstderr:\n%s",
			code,
			stdout,
			stderr,
		)
		return 1
	}
	expected := "fn take(report: &var Report) { }\nfn main() { take(&var report); }\n"
	if stdout != expected {
		t.Errorf(
			"hosted compiler parse borrow arg stdout mismatch:\nwant:\n%s\ngot:\n%s",
			expected,
			stdout,
		)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler parse borrow arg stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLICommentParseFailures checks comments do not trigger parse scans.
func countHostedCompilerCLICommentParseFailures(
	t *testing.T,
	exePath string,
	dir string,
	expected string,
) int {
	t.Helper()
	commentPath := filepath.Join(dir, "hosted_parse_comment_binding.kizu")
	commentSource := "fn main() {\n" +
		"    // let value = ;\n" +
		"    // let value;\n" +
		"    print(\"from temp\");\n" +
		"}\n"
	if err := os.WriteFile(commentPath, []byte(commentSource), 0o644); err != nil {
		t.Errorf("write hosted parse comment binding source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "parse", commentPath)
	if code != 0 {
		t.Errorf(
			"hosted compiler parse comment binding exit=%d\nstdout:\n%s\nstderr:\n%s",
			code,
			stdout,
			stderr,
		)
		return 1
	}
	if stdout != expected {
		t.Errorf(
			"hosted compiler parse comment binding stdout mismatch:\nwant:\n%s\ngot:\n%s",
			expected,
			stdout,
		)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler parse comment binding stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIRealSourceParseFailures checks a full selfhost source file.
func countHostedCompilerCLIRealSourceParseFailures(t *testing.T, exePath string) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "parse", "selfhost/src/main.kizu")
	if code != 0 {
		t.Errorf("hosted compiler real source parse exit=%d\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
		return 1
	}
	for _, fragment := range []string{"parse_file_cli", "check::file_cli"} {
		if !strings.Contains(stdout, fragment) {
			t.Errorf("hosted compiler real source parse missing %q:\n%s", fragment, stdout)
			return 1
		}
	}
	if stderr != "" {
		t.Errorf("hosted compiler real source parse stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIExpectedFailure validates one expected CLI failure.
func countHostedCompilerCLIExpectedFailure(
	t *testing.T,
	exePath string,
	command string,
	sourcePath string,
	expectedStderr string,
	label string,
) int {
	t.Helper()
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, command, sourcePath)
	if code != 1 {
		t.Errorf(
			"hosted compiler %s exit=%d\nstdout:\n%s\nstderr:\n%s",
			label,
			code,
			stdout,
			stderr,
		)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler %s stdout mismatch: %q", label, stdout)
		return 1
	}
	if stderr != expectedStderr {
		t.Errorf(
			"hosted compiler %s stderr mismatch:\nwant:\n%s\ngot:\n%s",
			label,
			expectedStderr,
			stderr,
		)
		return 1
	}
	return 0
}

// countHostedCompilerCLIFmtFailures runs generic fmt source through the artifact.
func countHostedCompilerCLIFmtFailures(t *testing.T, exePath string) int {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), "hosted_fmt_generic.kizu")
	source := "import std::fmt; struct Point{x:i64,y:i64,}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted fmt smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "fmt", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler fmt exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	expected := "import std::fmt;\nstruct Point { x: i64, y: i64 }\n"
	if stdout != expected {
		t.Errorf("hosted compiler fmt stdout mismatch:\nwant:\n%s\ngot:\n%s", expected, stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler fmt stderr mismatch: %q", stderr)
		return 1
	}
	return 0
}

// countHostedCompilerCLIFmtWriteFailures checks the hosted artifact mutates fmt input.
func countHostedCompilerCLIFmtWriteFailures(t *testing.T, exePath string) int {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), "hosted_fmt_write_generic.kizu")
	source := "import std::fmt; struct Point{x:i64,y:i64,}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted fmt --write smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "fmt", "--write", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler fmt --write exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler fmt --write stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler fmt --write stderr mismatch: %q", stderr)
		return 1
	}
	formatted, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Errorf("read hosted fmt --write source: %v", err)
		return 1
	}
	expected := "import std::fmt;\nstruct Point { x: i64, y: i64 }\n"
	if string(formatted) != expected {
		t.Errorf(
			"hosted compiler fmt --write content mismatch:\nwant:\n%s\ngot:\n%s",
			expected,
			formatted,
		)
		return 1
	}
	return 0
}

// countHostedCompilerCLIRunFailures runs a non-fixture source through `run`.
func countHostedCompilerCLIRunFailures(t *testing.T, exePath string) int {
	t.Helper()
	failures := countHostedCompilerCLIRunSourceFailures(
		t,
		exePath,
		"hosted_run_generic.kizu",
		"hosted_run_generic",
		"fn main(){print ( \"from hosted\" );}\n",
		`c"from hosted\0A"`,
	)
	failures += countHostedCompilerCLIRunSourceFailures(
		t,
		exePath,
		"hosted_run_backslash.kizu",
		"hosted_run_backslash",
		"fn main(){print(\"path\\value\");}\n",
		`c"path\5Cvalue\0A"`,
	)
	failures += countHostedCompilerCLIRunSourceFailures(
		t,
		exePath,
		"hosted_run_return.kizu",
		"hosted_run_return",
		"fn main(){return;}\n",
		"define i64 @kizu_run_main()",
		"@.kizu.run.stdout",
	)
	return failures
}

// countHostedCompilerCLIRunSourceFailures checks one hosted `run` source.
func countHostedCompilerCLIRunSourceFailures(
	t *testing.T,
	exePath string,
	name string,
	stem string,
	source string,
	expectedLLFragment string,
	rejectedLLFragments ...string,
) int {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted run smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "run", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler run stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler run stderr mismatch: %q", stderr)
		return 1
	}
	llPath := filepath.Join("target", "selfhost", "run", stem+".ll")
	metaPath := filepath.Join("target", "selfhost", "run", stem+".ll.meta")
	failures := countHostedCompilerCLIArtifactSourceFailures(
		t,
		llPath,
		metaPath,
		sourcePath,
		"target/selfhost/run/runtime.kizu",
	)
	llContent, readFailures := readHostedCompilerCLIArtifact(t, llPath)
	failures += readFailures
	if readFailures == 0 && !strings.Contains(llContent, expectedLLFragment) {
		t.Errorf(
			"hosted compiler run artifact %s missing %q:\n%s",
			llPath,
			expectedLLFragment,
			llContent,
		)
		failures++
	}
	if readFailures == 0 {
		for _, rejected := range rejectedLLFragments {
			if strings.Contains(llContent, rejected) {
				t.Errorf(
					"hosted compiler run artifact %s kept rejected %q:\n%s",
					llPath,
					rejected,
					llContent,
				)
				failures++
			}
		}
	}
	return failures
}

// countHostedCompilerCLITestFailures runs non-fixture sources through `test`.
func countHostedCompilerCLITestFailures(t *testing.T, exePath string) int {
	t.Helper()
	failures := countHostedCompilerCLITestSourceFailures(
		t,
		exePath,
		"hosted_test_ok_generic.kizu",
		"hosted_test_ok_generic",
		"test \"ok\" { std :: testing :: expect ( true ); }\n",
		"target/selfhost/test/expectoksrc.kizu",
	)
	failures += countHostedCompilerCLITestSourceFailures(
		t,
		exePath,
		"hosted_test_failure_generic.kizu",
		"hosted_test_failure_generic",
		"test \"failure\" { std :: testing :: expect ( false ); }\n",
		"target/selfhost/test/expectfailureabc.kizu",
	)
	return failures
}

// countHostedCompilerCLITestSourceFailures checks one hosted `test` source.
func countHostedCompilerCLITestSourceFailures(
	t *testing.T,
	exePath string,
	name string,
	stem string,
	source string,
	rejectedSourcePath string,
) int {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted test smoke source: %v", err)
		return 1
	}
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "test", sourcePath)
	if code != 0 {
		t.Errorf("hosted compiler test exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler test stdout mismatch: %q", stdout)
		return 1
	}
	if stderr != "" {
		t.Errorf("hosted compiler test stderr mismatch: %q", stderr)
		return 1
	}
	return countHostedCompilerCLIArtifactSourceFailures(
		t,
		filepath.Join("target", "selfhost", "test", stem+".ll"),
		filepath.Join("target", "selfhost", "test", stem+".ll.meta"),
		sourcePath,
		rejectedSourcePath,
	)
}

// countHostedCompilerCLIArtifactSourceFailures checks artifact source paths.
func countHostedCompilerCLIArtifactSourceFailures(
	t *testing.T,
	llPath string,
	metaPath string,
	sourcePath string,
	rejectedSourcePath string,
) int {
	t.Helper()
	llContent, failures := readHostedCompilerCLIArtifact(t, llPath)
	if failures > 0 {
		return failures
	}
	metaContent, failures := readHostedCompilerCLIArtifact(t, metaPath)
	if failures > 0 {
		return failures
	}
	expectedLL := `source_filename = "` + sourcePath + `"`
	if !strings.Contains(llContent, expectedLL) {
		t.Errorf("hosted compiler artifact %s missing %q:\n%s", llPath, expectedLL, llContent)
		return 1
	}
	expectedMeta := "source " + sourcePath + "\n"
	if !strings.Contains(metaContent, expectedMeta) {
		t.Errorf("hosted compiler artifact %s missing %q:\n%s", metaPath, expectedMeta, metaContent)
		return 1
	}
	expectedOutput := "output " + filepath.ToSlash(llPath) + "\n"
	if !strings.Contains(metaContent, expectedOutput) {
		t.Errorf("hosted compiler artifact %s missing %q:\n%s", metaPath, expectedOutput, metaContent)
		return 1
	}
	expectedLowering := "executable_lowering selfhost::backend::executable checked-ast\n"
	if !strings.Contains(metaContent, expectedLowering) {
		t.Errorf("hosted compiler artifact %s missing %q:\n%s",
			metaPath, expectedLowering, metaContent)
		return 1
	}
	if strings.Contains(llContent, rejectedSourcePath) {
		t.Errorf("hosted compiler artifact %s kept rejected source %q:\n%s",
			llPath, rejectedSourcePath, llContent)
		return 1
	}
	if strings.Contains(metaContent, rejectedSourcePath) {
		t.Errorf("hosted compiler artifact %s kept rejected source %q:\n%s",
			metaPath, rejectedSourcePath, metaContent)
		return 1
	}
	return 0
}

// readHostedCompilerCLIArtifact reads a repo-root artifact emitted by the hosted CLI.
func readHostedCompilerCLIArtifact(t *testing.T, path string) (string, int) {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Errorf("read hosted compiler artifact %s: %v", path, err)
		return "", 1
	}
	if len(bytes) == 0 {
		t.Errorf("hosted compiler artifact %s is empty", path)
		return "", 1
	}
	return string(bytes), 0
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
