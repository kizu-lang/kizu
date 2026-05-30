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
		"declare %kizu.error.i64 @kizu_rt_process_spawn_wait8",
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
		"%test_executable = call %kizu.selfhost.executable " +
			"@kizu_selfhost__cli_test_executable",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_parse_test_expect_value",
		"%test_ok_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%test_failure_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
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
	fragments := []string{
		"%kizu.selfhost.executable = type { i64, %kizu.slice.u8 }",
		"%kizu.selfhost.codegen.binding = type { i1, %kizu.slice.u8, %kizu.slice.u8, i64 }",
		"%kizu.selfhost.codegen.payload = type { i1, %kizu.slice.u8, i64 }",
		"%kizu.selfhost.codegen.run_ast = type { i1, i64, %kizu.slice.u8",
		// !RunAst error union backs the run-codegen AST traversal lowering cluster compiled
		// into stage2 (tracker 961, scope 4 prerequisite): { ok, RunAst value, diagnostic }.
		"%kizu.error.run_ast = type { i1, %kizu.selfhost.codegen.run_ast, %kizu.slice.u8 }",
		"%kizu.selfhost.codegen.value = type { i64, %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.selfhost.codegen.instruction = type { i64, i64, %kizu.slice.u8",
		"%kizu.selfhost.codegen.block = type { %kizu.slice.u8, i64 }",
		"%kizu.selfhost.codegen.function = type { %kizu.slice.u8, i64 }",
		"%kizu.selfhost.codegen.program = type { i64, %kizu.slice.u8",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_test_executable",
		"define %kizu.selfhost.codegen.value @kizu_selfhost__ir_codegen_const_string_value",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_const_string_instruction",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_call_instruction",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_return_void_instruction",
		"@.kizu.cli.codegen_metadata_line = private unnamed_addr constant",
		"define %kizu.slice.u8 @kizu_selfhost__ir_codegen_metadata_line()",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_main_print_program",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__cli_frontend_lower_checked_run_ast",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_codegen_lower_run_ast",
		"define %kizu.selfhost.codegen.binding @kizu_selfhost__cli_frontend_parse_let_binding",
		"define %kizu.selfhost.codegen.payload @kizu_selfhost__cli_frontend_parse_print_statement",
		"define i1 @kizu_selfhost__ir_codegen_program_supported",
	}
	fragments = append(fragments, requiredLLVMRunCodegenLoweringFragments()...)
	fragments = append(fragments, requiredLLVMAstAccessorFragments()...)
	fragments = append(fragments, requiredLLVMReturnErrorFragments()...)
	fragments = append(fragments, requiredLLVMArrayGetFragments()...)
	fragments = append(fragments, requiredLLVMChildAtFragments()...)
	fragments = append(fragments, requiredLLVMUnionAbiFragments()...)
	fragments = append(fragments, requiredLLVMArenaGetFragments()...)
	fragments = append(fragments, requiredLLVMMatchUnionFragments()...)
	fragments = append(fragments, requiredLLVMNodeCountTypeFragments()...)
	fragments = append(fragments, requiredLLVMNodeCountLoweringFragments()...)
	fragments = append(fragments, requiredLLVMTextAccessorFragments()...)
	fragments = append(fragments, requiredLLVMStringLiteralSpanFragments()...)
	fragments = append(fragments, requiredLLVMPrintPayloadFragments()...)
	fragments = append(fragments, requiredLLVMLowerPrintCallFragments()...)
	return append(fragments, []string{
		"define %kizu.error.slice.u8 @kizu_selfhost__ir_codegen_stdout_payload",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_codegen_payload_llvm_c_string",
		"define i64 @kizu_selfhost__cli_codegen_write_llvm_escape",
		"define %kizu.error.void @kizu_selfhost__cli_hosted_write_stdout_ll",
		"%payload = call %kizu.error.slice.u8 @kizu_selfhost__ir_codegen_stdout_payload",
		"%from_codegen_lowering = extractvalue %kizu.selfhost.codegen.program %program, 15",
		"%shape_ok = and i1 %shape_12, %from_codegen_lowering",
		"%escape_next_write = call i64 @kizu_selfhost__cli_codegen_write_llvm_escape",
		"define i1 @kizu_selfhost__cli_emit_run_codegen_artifact",
		"%run_emitted = call i1 @kizu_selfhost__cli_emit_run_codegen_artifact",
		"%run_link_result = call %kizu.error.i64 @kizu_rt_process_spawn_wait8",
		"%run_artifact_result = call %kizu.error.i64 @kizu_rt_process_spawn_wait8",
	}...)
}

// requiredLLVMRunCodegenLoweringFragments returns the tracker-961 run-codegen
// lowering members compiled into stage2.
func requiredLLVMRunCodegenLoweringFragments() []string {
	return []string{
		// tracker 961: first AST-traversal lowering member compiled into stage2.
		// This lands the run-AST local-binding model (LocalTable / LocalBinding)
		// plus the duplicate-name membership check used inside lower_run_ast_block.
		"%kizu.selfhost.codegen.local_binding = type { %kizu.slice.u8, i64, i64 }",
		"%kizu.selfhost.codegen.local_table = type { i64, " +
			"%kizu.selfhost.codegen.local_binding, %kizu.selfhost.codegen.local_binding }",
		"define i1 @kizu_selfhost__ir_codegen_local_table_contains",
		// tracker 961 follow-up: run-AST local-binding lookup helpers compiled into
		// stage2. empty_payload_span exercises the new Prefix(-Int) struct field
		// lowering (i64 -1 sentinels); local_payload_span exercises nested field
		// extraction plus an if-then struct-literal return with the empty_payload_span
		// fallback. #1021 already unblocked the nested-field call arguments.
		"%kizu.selfhost.codegen.payload_span = type { i64, i64 }",
		"define %kizu.selfhost.codegen.payload_span @kizu_selfhost__ir_codegen_empty_payload_span",
		"define %kizu.selfhost.codegen.local_binding @kizu_selfhost__ir_codegen_empty_local",
		"define %kizu.selfhost.codegen.payload_span @kizu_selfhost__ir_codegen_local_payload_span",
		// tracker 961 follow-up: empty_local_table compiled into stage2. It exercises
		// the new call-valued struct field initializer lowering, where first/second
		// are built by calling empty_local and feeding the result into the LocalTable
		// insertvalue chain.
		"define %kizu.selfhost.codegen.local_table @kizu_selfhost__ir_codegen_empty_local_table",
		"%vc0_1 = call %kizu.selfhost.codegen.local_binding " +
			"@kizu_selfhost__ir_codegen_empty_local",
		// tracker 961 follow-up: insert_local compiled into stage2. It exercises
		// nested struct-literal field initializers (first/second: LocalBinding { ... })
		// rendered in their own %v5000000+ insertvalue chains, alongside a call-valued
		// field (second: empty_local(text)) in the count == 0 then-block. Both the
		// count == 0 and count != 0 LocalTable shapes are preserved.
		"define %kizu.selfhost.codegen.local_table @kizu_selfhost__ir_codegen_insert_local",
		"%vc0_2 = call %kizu.selfhost.codegen.local_binding " +
			"@kizu_selfhost__ir_codegen_empty_local",
		"%v5000001_0 = insertvalue %kizu.selfhost.codegen.local_binding poison",
		// tracker 961 follow-up: is_payload_supported compiled into stage2 through the
		// restricted bounded-counter while lowering. The loop emits a single loop-carried
		// phi for the induction variable (%index), a comparison condition that branches to
		// the loop body/exit, an index-plus-one latch increment, and an ADR-0053 checked
		// payload index load (trap-guarded getelementptr) rather than an unchecked GEP.
		// The three sequential early-return byte-range checks reject bytes outside
		// 32..126 and the quote byte.
		"define i1 @kizu_selfhost__ir_codegen_is_payload_supported",
		"%index = phi i64 [ 0, %loop4_preheader ], [ %index_next, %loop4_latch ]",
		", label %loop4_body, label %loop4_exit",
		"%index_next = add i64 %index, 1",
		"br i1 %t7_bad, label %t7_idx_oob, label %t7_idx_ok",
		"%t7_gep = getelementptr i8, ptr %t7_ptr, i64 %t6",
		// tracker 961 foundation: empty_int_env is the first stage2 function that
		// takes a real std::kizu::ast value type (ChildRange) as a parameter and
		// forwards it whole into a struct field, without touching the AstNode/AstData
		// union. The ChildRange LLVM type and its embedding IntEnv type are emitted,
		// and the decls parameter is inserted as an aggregate into field index 1.
		"%kizu.kizu.ast.child_range = type { i64, i64 }",
		"%kizu.selfhost.codegen.int_env = type { i64, %kizu.kizu.ast.child_range, " +
			"%kizu.slice.u8, i64, %kizu.slice.u8, i64, %kizu.slice.u8, i64 }",
		"define %kizu.selfhost.codegen.int_env @kizu_selfhost__ir_codegen_empty_int_env",
		"%v0_1 = insertvalue %kizu.selfhost.codegen.int_env %v0_0, " +
			"%kizu.kizu.ast.child_range %decls, 1",
	}
}

// requiredLLVMAstAccessorFragments returns the tracker-961 read-only AST accessor
// members compiled into stage2. std::kizu::ast::Ast.len is the first one: the Ast
// container value lowers with its nodes (Arena) and children (Array) fields as
// %kizu.owned heap handles; the compiled define reads the children Array (struct
// field index 1) and returns its length via the array_len builtin, which reads the
// len field (index 2) out of the %kizu.rt.array record the handle wraps. No
// AstNode/AstData union, Arena.get, array_get, or error-union return is touched.
func requiredLLVMAstAccessorFragments() []string {
	return []string{
		"%kizu.kizu.ast.source_file = type { %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.kizu.ast.ast = type { %kizu.owned, %kizu.owned, " +
			"%kizu.kizu.ast.source_file }",
		"%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }",
		"define i64 @kizu_kizu__ast_ast_len",
		"%arg0_0_ex = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%result = call i64 @kizu_rt_array_len(%kizu.owned %arg0_0_ex)",
		"define i64 @kizu_rt_array_len(%kizu.owned %array)",
		"%len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2",
	}
}

// requiredLLVMReturnErrorFragments returns the tracker-961 `return error("...")`
// lowering members compiled into stage2. stdout_payload's error branch is the
// first compiled error-union failure return: instead of the prior `unreachable`
// placeholder it materializes the "unsupported codegen stdout payload" message
// global, builds the %kizu.error.slice.u8 failure value (i1 false ok flag at
// field 0, success value left as zeroinitializer, the message %kizu.slice.u8 at
// the failure index 2) and returns it. The success branch still wraps the
// payload slice as the field-1 success value.
func requiredLLVMReturnErrorFragments() []string {
	return []string{
		"@.kizu.compiled.kizu_selfhost__ir_codegen_stdout_payload.s0 = " +
			"private unnamed_addr constant [34 x i8] c\"unsupported codegen stdout payload\"",
		"%t3 = insertvalue %kizu.slice.u8 %t3_base, i64 34, 1",
		"%errfail3_flag = insertvalue %kizu.error.slice.u8 zeroinitializer, i1 false, 0",
		"%errfail3 = insertvalue %kizu.error.slice.u8 %errfail3_flag, %kizu.slice.u8 %t3, 2",
		"  ret %kizu.error.slice.u8 %errfail3",
		"%retwrap_val = insertvalue %kizu.error.slice.u8 %retwrap_ok, %kizu.slice.u8 %t4, 1",
	}
}

// requiredLLVMArrayGetFragments returns the tracker-961 checked Array<T>.get
// accessor compiled into stage2. std::kizu::diagnostic::Diagnostic.related_get reads
// the related Array<RelatedSpan> field (struct field index 2) off the Diagnostic value
// receiver, calls the self-contained @kizu_rt_array_at runtime helper (the same checked
// element accessor std arrays use, returning a borrowed %kizu.error.slice.u8 view), and
// branches on the ok flag: the success path loads the RelatedSpan element by value and
// wraps it as the error-union success value (field 1); the failure path forwards the
// runtime message as a real error-union failure return (field 2), never `unreachable`.
func requiredLLVMArrayGetFragments() []string {
	return []string{
		"%kizu.kizu.diagnostic.file_span = type { i64, %kizu.slice.u8, i64, i64, i64, i64 }",
		"%kizu.kizu.diagnostic.related_span = type { %kizu.kizu.diagnostic.file_span, " +
			"%kizu.slice.u8 }",
		"%kizu.kizu.diagnostic.diagnostic = type { %kizu.kizu.diagnostic.file_span, " +
			"%kizu.slice.u8, %kizu.owned }",
		"%kizu.error.related_span = type { i1, %kizu.kizu.diagnostic.related_span, " +
			"%kizu.slice.u8 }",
		"define %kizu.error.related_span @kizu_kizu__diagnostic_diagnostic_related_get",
		"%array = extractvalue %kizu.kizu.diagnostic.diagnostic %self, 2",
		"%view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index)",
		"%view_ok = extractvalue %kizu.error.slice.u8 %view, 0",
		"br i1 %view_ok, label %array_get_ok, label %array_get_fail",
		"%elem = load %kizu.kizu.diagnostic.related_span, ptr %elem_ptr",
		"%ok_value = insertvalue %kizu.error.related_span %ok_flag, " +
			"%kizu.kizu.diagnostic.related_span %elem, 1",
		"  ret %kizu.error.related_span %ok_value",
		"%fail_value = insertvalue %kizu.error.related_span %fail_flag, " +
			"%kizu.slice.u8 %fail_msg, 2",
		"  ret %kizu.error.related_span %fail_value",
		"define %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index)",
	}
}

// requiredLLVMChildAtFragments returns the tracker-961 std::kizu::ast::Ast.child_at
// accessor compiled into stage2. It composes the checked Array<NodeId>.get accessor
// with a bounds-check `if index < 0 or index >= range.len { return error(...); }`:
// the `or` short-circuit branches to the shared error block, which materializes the
// "child index out of bounds" message and returns a real %kizu.error.node_id failure
// value (field 2), never `unreachable`. The continuation computes range.start + index
// (extractvalue of the ChildRange + add), reads the children Array (field 1) off the
// Ast value, calls the checked @kizu_rt_array_at, and on success loads the NodeId by
// value and wraps it as the error-union success value (field 1).
func requiredLLVMChildAtFragments() []string {
	return []string{
		"%kizu.kizu.ast.node_id = type { i64 }",
		"%kizu.error.node_id = type { i1, %kizu.kizu.ast.node_id, %kizu.slice.u8 }",
		"define %kizu.error.node_id @kizu_kizu__ast_ast_child_at",
		"%kizu.kizu.ast.child_range %range",
		"br i1 %t2, label %if0_then, label %if0_rhs",
		"%t5 = icmp sge i64 %t3, %t4",
		"%t4 = extractvalue %kizu.kizu.ast.child_range %range, 1",
		"br i1 %t5, label %if0_then, label %if0_cont",
		"%errfail6 = insertvalue %kizu.error.node_id %errfail6_flag, %kizu.slice.u8 %t6, 2",
		"  ret %kizu.error.node_id %errfail6",
		"%t7 = extractvalue %kizu.kizu.ast.child_range %range, 0",
		"%t9 = add i64 %t7, %t8",
		"%array = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %t9)",
		"%elem = load %kizu.kizu.ast.node_id, ptr %elem_ptr",
		"%ok_value = insertvalue %kizu.error.node_id %ok_flag, %kizu.kizu.ast.node_id %elem, 1",
		"  ret %kizu.error.node_id %ok_value",
		"  ret %kizu.error.node_id %fail_value",
	}
}

// requiredLLVMUnionAbiFragments returns the tracker-961 AstNode/AstData tagged-union
// value-type ABI compiled into stage2. std::kizu::ast::Ast.add_node constructs an
// AstNode (a Span plus the AstData union value forwarded whole) and appends it to the
// nodes Arena. AstData is the #991 inline layout { i64 tag, [96 x i8] payload storage }
// where 96 is the largest variant payload capacity; the 44 variant payload structs are
// not modelled because add_node forwards the union value without inspecting it. The
// element is marshalled through stack storage as a %kizu.slice.u8 (ptr + sizeof via the
// getelementptr-null idiom) and appended through the heap-backed @kizu_rt_arena_add,
// which returns a %kizu.handle whose index field becomes the NodeId.
func requiredLLVMUnionAbiFragments() []string {
	return []string{
		"%kizu.kizu.ast.span = type { i64, i64 }",
		"%kizu.kizu.ast.ast_data = type { i64, [96 x i8] }",
		"%kizu.kizu.ast.ast_node = type { %kizu.kizu.ast.span, %kizu.kizu.ast.ast_data }",
		"define %kizu.kizu.ast.node_id @kizu_kizu__ast_ast_add_node",
		"%v0_0 = insertvalue %kizu.kizu.ast.ast_node poison, %kizu.kizu.ast.span %span_value, 0",
		"%v0_1 = insertvalue %kizu.kizu.ast.ast_node %v0_0, %kizu.kizu.ast.ast_data %data, 1",
		"store %kizu.kizu.ast.ast_node %v0_1, ptr %raw_slot, align 8",
		"%raw_slice = insertvalue %kizu.slice.u8 %raw_slice_base, i64 ptrtoint (ptr getelementptr (" +
			"%kizu.kizu.ast.ast_node, ptr null, i32 1) to i64), 1",
		"%raw_arena = extractvalue %kizu.kizu.ast.ast %self, 0",
		"%raw_handle = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %raw_arena, " +
			"%kizu.slice.u8 %raw_slice)",
		"%raw = extractvalue %kizu.handle %raw_handle, 1",
		"  ret %kizu.kizu.ast.node_id %v2_0",
		"define %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %value)",
	}
}

// requiredLLVMArenaGetFragments returns the tracker-961 std::kizu::ast::Ast.get
// accessor compiled into stage2. It reads an AstNode back out of the nodes Arena by
// value (self.nodes.get(id.raw)), reusing the AstNode/AstData union value-type ABI: it
// computes the index from the NodeId.raw field, reads the Arena handle off the Ast
// value, calls the checked self-contained @kizu_rt_arena_get, and on success loads the
// AstNode element by value and returns it. An out-of-bounds index traps via
// @kizu_rt_trap (Arena.get panics rather than returning an error union, so the trap is
// the genuine failure semantics, not a stubbed-out branch).
func requiredLLVMArenaGetFragments() []string {
	return []string{
		"define %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get",
		"%index = extractvalue %kizu.kizu.ast.node_id %id, 0",
		"%arena = extractvalue %kizu.kizu.ast.ast %self, 0",
		"%view = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, i64 %index)",
		"br i1 %view_ok, label %arena_get_ok, label %arena_get_fail",
		"%elem = load %kizu.kizu.ast.ast_node, ptr %elem_ptr",
		"  ret %kizu.kizu.ast.ast_node %elem",
		"call void @kizu_rt_trap(%kizu.slice.u8 %fail_msg)",
		"define %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, i64 %index)",
	}
}

// requiredLLVMMatchUnionFragments returns the tracker-961 selfhost::ast::declaration_count
// accessor compiled into stage2: the first 'match node.data' over the AstNode/AstData
// tagged union. It binds an AstNode by value via the checked @kizu_kizu__ast_ast_get call,
// extracts the AstData union (AstNode field 1) and its i64 tag (field 0), and dispatches on
// the Program discriminant: the active arm reads the ProgramNode payload out of the inline
// union storage (alloca + store + gep field 1 + load) and returns program.declarations.len
// (a two-level extractvalue), while the default arm returns the literal 0 (a real value,
// not 'unreachable'). The %kizu.kizu.ast.program_node payload struct is the only modelled
// AstData variant payload.
func requiredLLVMMatchUnionFragments() []string {
	return []string{
		"%kizu.kizu.ast.program_node = type { %kizu.kizu.ast.child_range }",
		"define i64 @kizu_selfhost__ast_declaration_count",
		"%match_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %root)",
		"%match_data = extractvalue %kizu.kizu.ast.ast_node %match_node, 1",
		"%match_tag = extractvalue %kizu.kizu.ast.ast_data %match_data, 0",
		"%match_is_variant = icmp eq i64 %match_tag, 0",
		"br i1 %match_is_variant, label %match_arm_variant, label %match_arm_default",
		"%match_payload_ptr = getelementptr %kizu.kizu.ast.ast_data, " +
			"ptr %match_payload_slot, i32 0, i32 1",
		"%match_payload = load %kizu.kizu.ast.program_node, ptr %match_payload_ptr, align 8",
		"%match_field0 = extractvalue %kizu.kizu.ast.program_node %match_payload, 0",
		"%match_value = extractvalue %kizu.kizu.ast.child_range %match_field0, 1",
		"  ret i64 %match_value",
		"match_arm_default:",
		"  ret i64 0",
	}
}

// requiredLLVMNodeCountTypeFragments returns the tracker-961 type/facts foundation for the
// node_count AST-traversal cluster: the AstData variant payload structs that node_count and
// the count_* helpers read by value, modelled in stage2 as %kizu.kizu.ast.*_node value types
// (bool -> i1, an enum tag -> i64, NodeId / ChildRange / Span -> their value types). The five
// leaf variants and the payload-less Empty are not modelled here. This is the type-definition
// foundation; the cluster's lowering lands in a later PR.
func requiredLLVMNodeCountTypeFragments() []string {
	return []string{
		"%kizu.kizu.ast.prefix_node = type { i64, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.binary_node = type { i64, %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.field_expr_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, i1 }",
		"%kizu.kizu.ast.deref_expr_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.call_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.type_apply_expr_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.cast_expr_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.index_expr_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, i1 }",
		"%kizu.kizu.ast.struct_literal_expr_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.struct_field_init_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.arena_new_expr_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.try_expr_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.comptime_expr_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.block_node = type { %kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.if_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.let_node = type { i1, %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.assign_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.return_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.defer_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.expr_stmt_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.while_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.for_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.break_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.continue_node = type { %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.import_decl_node = type { %kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.param_node = type { i1, %kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.field_node = type { i1, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.span }",
		"%kizu.kizu.ast.struct_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.child_range, %kizu.kizu.ast.child_range, " +
			"%kizu.kizu.ast.span }",
		"%kizu.kizu.ast.enum_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.child_range, %kizu.kizu.ast.span }",
		"%kizu.kizu.ast.union_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.child_range, %kizu.kizu.ast.child_range, " +
			"%kizu.kizu.ast.span }",
		"%kizu.kizu.ast.impl_decl_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.union_variant_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.span }",
		"%kizu.kizu.ast.match_node = type { %kizu.kizu.ast.node_id, %kizu.kizu.ast.child_range }",
		"%kizu.kizu.ast.match_arm_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.unsafe_node = type { %kizu.kizu.ast.child_range, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.comptime_if_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }",
		"%kizu.kizu.ast.fn_decl_node = type { i1, i1, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.child_range, " +
			"%kizu.kizu.ast.child_range, %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, %kizu.kizu.ast.span }",
	}
}

// requiredLLVMNodeCountLoweringFragments returns the tracker-961 node_count recursive
// AST-traversal cluster compiled into stage2: node_count (the 44-arm match-over-AstData
// traversal), count_range (the two-phi accumulator loop calling Ast.child_at + node_count),
// and the count_* helpers (let-try / return-try arithmetic). The whole mutually-recursive
// cluster is defined so selfhost.ll links with no undefined symbol; every arm returns a real
// value (no 'unreachable'). These fragments lock each lowered body shape.
func requiredLLVMNodeCountLoweringFragments() []string {
	return []string{
		// All eleven cluster defines are present (linkage invariant).
		"define %kizu.error.i64 @kizu_selfhost__ast_node_count(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_range(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_one(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_two(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_three(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_five(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_with_range(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_node_with_range(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_named_range(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_named_ranges(",
		"define %kizu.error.i64 @kizu_selfhost__ast_count_fn_decl_parts(",
		// node_count: bind the AstNode via Ast.get, extract the union tag, and dispatch over
		// the exhaustive icmp/br arm chain (Program tag 0 first, FnDecl tag 42, ...).
		"%match_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %node_id)",
		"%match_tag = extractvalue %kizu.kizu.ast.ast_data %match_data, 0",
		"%match_is_0 = icmp eq i64 %match_tag, 0",
		"br i1 %match_is_0, label %match_arm_0, label %match_check_1",
		// node_count Program arm: load the ProgramNode payload, forward declarations to
		// count_with_range, and return its (identically-typed) error union directly.
		"%match_arm_0_payload = load %kizu.kizu.ast.program_node, " +
			"ptr %match_arm_0_ptr, align 8",
		"%match_arm_0_a0 = extractvalue %kizu.kizu.ast.program_node %match_arm_0_payload, 0",
		"%match_arm_0_call = call %kizu.error.i64 @kizu_selfhost__ast_count_with_range(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.child_range %match_arm_0_a0)",
		"  ret %kizu.error.i64 %match_arm_0_call",
		// count_range: a two-phi accumulator loop over the range calling the checked
		// Ast.child_at and the recursive node_count, propagating either failure and returning
		// the accumulated count wrapped as the success.
		"%range_len = extractvalue %kizu.kizu.ast.child_range %range, 1",
		"%count = phi i64 [ 0, %entry ], [ %count_next, %count_nc_cont ]",
		"%index = phi i64 [ 0, %entry ], [ %index_next, %count_nc_cont ]",
		"%child_call = call %kizu.error.node_id @kizu_kizu__ast_ast_child_at(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.child_range %range, i64 %index)",
		"%nc_call = call %kizu.error.i64 @kizu_selfhost__ast_node_count(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %child)",
		"%count_next = add i64 %count, %nc",
		// count_two: let-try the recursive node_count, propagate failure, bind the success,
		// then return the arithmetic wrapped as the error-union success.
		"%first_count_call = call %kizu.error.i64 @kizu_selfhost__ast_node_count(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %first)",
		"%first_count = extractvalue %kizu.error.i64 %first_count_call, 1",
		// count_one: return '1 + try node_count(...)' as a single return-try-binary.
		"%rettry_call = call %kizu.error.i64 @kizu_selfhost__ast_node_count(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %first)",
		"%rettry_sum = add i64 1, %rettry_success",
	}
}

// requiredLLVMTextAccessorFragments returns the tracker-961 span-based text slicing accessor
// chain compiled into stage2: the run-codegen AST traversal binds source text by reading a
// node's span (ast_node_text), trims the slice (std::mem::trim_ascii), and classifies bytes
// (std::mem::is_ascii_space). The three defines are emitted together so selfhost.ll links with
// no undefined symbol; these fragments lock each lowered body shape.
func requiredLLVMTextAccessorFragments() []string {
	return []string{
		// All three chain defines are present (linkage invariant).
		"define i1 @kizu_std__mem_is_ascii_space(",
		"define %kizu.slice.u8 @kizu_std__mem_trim_ascii(",
		"define %kizu.slice.u8 @kizu_selfhost__ir_codegen_ast_node_text(",
		// trim_ascii: a front loop advancing %start past leading ASCII spaces and a back loop
		// retreating %end past trailing ones, returning the trimmed two-bound slice.
		"%trim_bytes_len = extractvalue %kizu.slice.u8 %bytes, 1",
		"%trim_start = phi i64 [ 0, %entry ], [ %trim_start_next, %trim_front_inc ]",
		"%trim_front_cond = icmp slt i64 %trim_start, %trim_bytes_len",
		"%trim_front_space = call i1 @kizu_std__mem_is_ascii_space(i8 %trim_front_byte)",
		"%trim_end = phi i64 [ %trim_bytes_len, %trim_front_head ], " +
			"[ %trim_bytes_len, %trim_front_body ], [ %trim_end_next, %trim_back_inc ]",
		"%trim_back_cond = icmp sgt i64 %trim_end, %trim_start",
		"%trim_back_space = call i1 @kizu_std__mem_is_ascii_space(i8 %trim_back_byte)",
		"%trim_slice_len = sub i64 %trim_end, %trim_start",
		"  ret %kizu.slice.u8 %trim_s1",
		// ast_node_text: bind the AstNode via Ast.get, extract its Span (field 0) start/end,
		// slice the text two-bound, and forward the slice to trim_ascii.
		"%ant_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %ast, %kizu.kizu.ast.node_id %node_id)",
		"%ant_span = extractvalue %kizu.kizu.ast.ast_node %ant_node, 0",
		"%ant_start = extractvalue %kizu.kizu.ast.span %ant_span, 0",
		"%ant_end = extractvalue %kizu.kizu.ast.span %ant_span, 1",
		"%ant_slice_len = sub i64 %ant_end, %ant_start",
		"%ant_trimmed = call %kizu.slice.u8 @kizu_std__mem_trim_ascii(%kizu.slice.u8 %ant_s1)",
		"  ret %kizu.slice.u8 %ant_trimmed",
	}
}

// requiredLLVMStringLiteralSpanFragments returns the tracker-961 scope-4 prerequisite
// string_literal_span AST traversal accessor compiled into stage2: it binds an AstNode via
// Ast.get, reads the Span start/end, rejects spans shorter than the two surrounding quotes,
// checks the leading/trailing double-quote bytes, forwards the inner payload slice to
// is_payload_supported, and returns the trimmed PayloadSpan or the shared empty_payload_span()
// sentinel. Its callees are already compiled so selfhost.ll links. These fragments lock the
// lowered body shape.
func requiredLLVMStringLiteralSpanFragments() []string {
	return []string{
		"define %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_string_literal_span(",
		"%sls_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %ast, %kizu.kizu.ast.node_id %node_id)",
		"%sls_span = extractvalue %kizu.kizu.ast.ast_node %sls_node, 0",
		"%sls_length = sub i64 %sls_end, %sls_start",
		"%sls_too_short = icmp slt i64 %sls_length, 2",
		"  br i1 %sls_too_short, label %sls_empty, label %sls_check_quotes",
		"%sls_first_q = icmp eq i8 %sls_first, 34",
		"%sls_last_q = icmp eq i8 %sls_last, 34",
		"%sls_both_q = and i1 %sls_first_q, %sls_last_q",
		"%sls_supported = call i1 @kizu_selfhost__ir_codegen_is_payload_supported(" +
			"%kizu.slice.u8 %sls_p1)",
		"%sls_r1 = insertvalue %kizu.selfhost.codegen.payload_span %sls_r0, " +
			"i64 %sls_payload_end, 1",
		"%sls_empty_call = call %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_empty_payload_span()",
	}
}

// requiredLLVMPrintPayloadFragments returns the tracker-961 scope-4 prerequisite print_payload
// AST traversal accessor compiled into stage2: it binds the print argument node via Ast.get,
// reads the AstData tag (AstNode field 1, tag field 0), and dispatches by variant - String
// (tag 2) returns string_literal_span, Var (tag 4) returns local_payload_span(locals,
// ast_node_text(...)), and every other variant returns the shared empty_payload_span() sentinel.
// Its callees are already compiled so selfhost.ll links. These fragments lock the lowered shape.
func requiredLLVMPrintPayloadFragments() []string {
	return []string{
		"define %kizu.selfhost.codegen.payload_span @kizu_selfhost__ir_codegen_print_payload(",
		"%pp_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %ast, %kizu.kizu.ast.node_id %arg)",
		"%pp_data = extractvalue %kizu.kizu.ast.ast_node %pp_node, 1",
		"%pp_tag = extractvalue %kizu.kizu.ast.ast_data %pp_data, 0",
		"%pp_is_string = icmp eq i64 %pp_tag, 2",
		"  br i1 %pp_is_string, label %pp_string, label %pp_check_var",
		"%pp_string_result = call %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_string_literal_span(%kizu.slice.u8 %text, " +
			"%kizu.kizu.ast.ast %ast, %kizu.kizu.ast.node_id %arg)",
		"%pp_is_var = icmp eq i64 %pp_tag, 4",
		"%pp_name = call %kizu.slice.u8 @kizu_selfhost__ir_codegen_ast_node_text(" +
			"%kizu.slice.u8 %text, %kizu.kizu.ast.ast %ast, %kizu.kizu.ast.node_id %arg)",
		"%pp_var_result = call %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_local_payload_span(" +
			"%kizu.selfhost.codegen.local_table %locals, %kizu.slice.u8 %pp_name)",
		"%pp_empty = call %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_empty_payload_span()",
	}
}

// requiredLLVMLowerPrintCallFragments returns the tracker-961 scope-4 prerequisite
// lower_print_call AST traversal lowering compiled into stage2: the first compiled !RunAst
// error-union lowering. It compares the callee text against the "print" literal global, checks
// the arity, propagates the checked Ast.child_at failure into the !RunAst error, extracts the
// argument payload via print_payload, slices the source text, and wraps print_run_ast /
// unsupported_run_ast into the !RunAst success. These fragments lock the lowered body shape.
func requiredLLVMLowerPrintCallFragments() []string {
	return []string{
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_print_call(",
		"@.kizu.compiled.kizu_selfhost__ir_codegen_lower_print_call.s0 = " +
			"private unnamed_addr constant [5 x i8] c\"print\"",
		"%lpc_callee_text = call %kizu.slice.u8 @kizu_selfhost__ir_codegen_ast_node_text(",
		"%lpc_is_print = call i1 @kizu_selfhost__slice_equal(" +
			"%kizu.slice.u8 %lpc_callee_text, %kizu.slice.u8 %lpc_print_slice)",
		"%lpc_args_len = extractvalue %kizu.kizu.ast.child_range %args, 1",
		"%lpc_child = call %kizu.error.node_id @kizu_kizu__ast_ast_child_at(",
		"%lpc_fail2 = insertvalue %kizu.error.run_ast %lpc_fail1, %kizu.slice.u8 %lpc_child_err, 2",
		"%lpc_payload = call %kizu.selfhost.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_print_payload(",
		"%lpc_run = call %kizu.selfhost.codegen.run_ast " +
			"@kizu_selfhost__ir_codegen_print_run_ast(%kizu.slice.u8 %function_name, " +
			"i64 %statement_count, %kizu.slice.u8 %lpc_callee_text, %kizu.slice.u8 %lpc_payload_text)",
		"%lpc_ok1 = insertvalue %kizu.error.run_ast %lpc_ok0, " +
			"%kizu.selfhost.codegen.run_ast %lpc_run, 1",
		"%lpc_us = call %kizu.selfhost.codegen.run_ast " +
			"@kizu_selfhost__ir_codegen_unsupported_run_ast(%kizu.slice.u8 %lpc_empty)",
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
		"define i1 @kizu_selfhost__cli_parse_run_return_void_ok",
		"define %kizu.selfhost.executable @kizu_selfhost__cli_run_executable",
		"%run_executable = call %kizu.selfhost.executable " +
			"@kizu_selfhost__cli_run_executable",
		"%run_return_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%run_return_ll_write = call %kizu.error.void @kizu_selfhost__write_concat3",
		"%run_return_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_parse_run_print_payload",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_parse_run_local_print_payload",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_codegen_main_print_payload",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_codegen_local_print_payload",
		"define i1 @kizu_selfhost__cli_is_supported_run_print_payload",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_run_codegen_program",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_run_payload_llvm_c_string",
		"%run_codegen = call %kizu.selfhost.codegen.program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_codegen_lower_run_checked_program",
		"@kizu_selfhost__cli_codegen_parse_checked_run_ast",
		"@kizu_selfhost__cli_codegen_parse_let_binding",
		"@kizu_selfhost__cli_codegen_parse_print_statement",
		"define %kizu.selfhost.codegen.payload @kizu_selfhost__cli_codegen_parse_print_payload",
		"%run_print_mkdir = call %kizu.error.void @kizu_selfhost__ensure_artifact_dir",
		"%run_print_ll_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"%run_print_meta_write = call %kizu.error.void @kizu_selfhost__write_concat9",
		"run_ll_prefix",
		"run_ll_middle",
		"run_module_prefix",
		"run_module_len_middle",
		"run_module_payload_middle",
		"run_module_slice_middle",
		"run_module_suffix",
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
		"linker-process hosted-artifact-runner\n",
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
		"backend-input function-signature-return selfhost::backend::" +
			"lower_run_codegen_program !codegen::Program\n",
		"backend-input function-signature-param selfhost::backend::" +
			"lower_run_codegen_program 1 ast:runtime:std::kizu::ast::Ast\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"lower_run_program !Program\n",
		"backend-input function-signature-param selfhost::ir::codegen::" +
			"lower_run_program 1 ast:runtime:std::kizu::ast::Ast\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"lower_run_ast !RunAst\n",
		"backend-input function-signature-param selfhost::ir::codegen::" +
			"lower_run_ast 1 ast:runtime:std::kizu::ast::Ast\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"lower_run_ast_to_program Program\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"main_print_program Program\n",
		"backend-input function-signature-param selfhost::ir::codegen::" +
			"main_print_program 0 payload:runtime:[]u8\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"stdout_payload ![]u8\n",
		"backend-input function-signature-param selfhost::ir::codegen::" +
			"stdout_payload 0 program:runtime:&Program\n",
		"backend-input function-signature-return selfhost::ir::codegen::" +
			"metadata_line []u8\n",
		"backend-input function-signature-param selfhost::backend::executable::" +
			"lower_test_executable 1 ast:runtime:std::kizu::ast::Ast\n",
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
		"backend-input executable-layout kind:i64 callee:[]u8 payload:[]u8\n",
		"backend-input executable-kind Unsupported 0\n",
		"backend-input executable-kind RunReturnVoid 1\n",
		"backend-input executable-kind Call 2\n",
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
		"external @kizu_host_process_spawn_wait8\n",
		"external @kizu_host_trap\n",
		"go-stdprim-host none\n",
		"interpreter-host none\n",
		"linker-process hosted-artifact-runner\n",
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
	failures := countHostedCompilerCLIUnsupportedRunSourceFailures(
		t,
		exePath,
		"hosted_run_return.kizu",
		"hosted_run_return",
		"fn main(){return;}\n",
	)
	return failures
}

// countHostedCompilerCLIUnsupportedRunSourceFailures checks that hosted `run`
// sources unsupported by codegen do not fall back to static artifact emission.
func countHostedCompilerCLIUnsupportedRunSourceFailures(
	t *testing.T,
	exePath string,
	name string,
	stem string,
	source string,
) int {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Errorf("write hosted run smoke source: %v", err)
		return 1
	}
	llPath := filepath.Join("target", "selfhost", "run", stem+".ll")
	metaPath := filepath.Join("target", "selfhost", "run", stem+".ll.meta")
	_ = os.Remove(llPath)
	_ = os.Remove(metaPath)
	stdout, stderr, code := runHostedCompilerCLI(t, exePath, "run", sourcePath)
	if code != 64 {
		t.Errorf("hosted compiler run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		return 1
	}
	if stdout != "" {
		t.Errorf("hosted compiler run stdout mismatch: %q", stdout)
		return 1
	}
	if !strings.Contains(stderr, "usage: selfhost") {
		t.Errorf("hosted compiler run stderr mismatch: %q", stderr)
		return 1
	}
	failures := 0
	for _, path := range []string{llPath, metaPath} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("hosted compiler run emitted unsupported artifact %s", path)
			failures++
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
