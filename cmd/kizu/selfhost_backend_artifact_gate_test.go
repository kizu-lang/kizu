package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// TestSelfhostBackendArtifactGate executes the stage0-native LLVM artifact smoke.
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

// TestSelfhostBackendArtifactGateRecipes keeps backend artifact timing explicit.
func TestSelfhostBackendArtifactGateRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	gate := justRecipe(content, "selfhost-backend-artifact-gate")
	requireRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_GATES=1 go test")
	requireRecipeFragment(t, gate, "TestSelfhostBackendArtifactGate$")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")
	requireNoRecipeFragment(t, gate, "KIZU_RUN_SELFHOST_ORACLE=1")

	integration := justRecipe(content, "selfhost-integration-gates")
	requireNoRecipeFragment(t, integration, "BackendArtifactGate")
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
		"array-pop-or-panic removes-last-element-view\n",
		"array-set in-place-element-overwrite\n",
		"array-deinit releases-element-buffer\n",
		"array-invalid-element-diagnostic invalid array element\n",
		"array-oob-diagnostic array index out of bounds\n",
		"string-storage byte-buffer\n",
		"string-as-bytes returns-stored-bytes\n",
		"string-deinit releases-byte-buffer\n",
		"string-invalid-slice-diagnostic invalid slice\n",
		"map-storage string-key-generic-byte-buffer\n",
		"map-value-layout constructor-sized\n",
		"map-growth geometric-entry-buffer\n",
		"map-get returns-stored-value-view\n",
		"map-deinit releases-key-value-entry-buffers\n",
		"map-key-ownership copies-key-bytes\n",
		"map-missing-key-diagnostic Map.get key not found\n",
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
	required = append(required, backendArtifactContractInventory...)
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("backend artifact gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return countSelfhostBackendArtifactFileFailures(t)
}

// appendSelfhostBackendArtifactReport writes the legacy gate contract from files.
func appendSelfhostBackendArtifactReport(t *testing.T, out *strings.Builder) int {
	t.Helper()
	files := []struct {
		pathLabel  string
		bytesLabel string
		path       string
	}{
		{"llvm-artifact-path", "llvm-artifact-bytes", "target/selfhost/selfhost.ll"},
		{"llvm-metadata-path", "llvm-metadata-bytes", "target/selfhost/selfhost.ll.meta"},
		{"runtime-storage-path", "runtime-storage-bytes", "target/selfhost/selfhost.storage.ll"},
		{
			"runtime-storage-metadata-path",
			"runtime-storage-metadata-bytes",
			"target/selfhost/selfhost.storage.ll.meta",
		},
		{"host-capability-path", "host-capability-bytes", "target/selfhost/selfhost.host.ll"},
		{
			"host-capability-metadata-path",
			"host-capability-metadata-bytes",
			"target/selfhost/selfhost.host.ll.meta",
		},
	}
	failures := 0
	for _, file := range files {
		size, err := fileSize(file.path)
		if err != nil {
			t.Errorf("read staged backend artifact %s: %v", file.path, err)
			failures++
			continue
		}
		fmt.Fprintf(out, "%s\n%s\n", file.pathLabel, file.path)
		fmt.Fprintf(out, "%s\n%d\n", file.bytesLabel, size)
	}
	return failures
}

// countSelfhostBackendArtifactFileFailures validates deterministic LLVM artifacts.
func countSelfhostBackendArtifactFileFailures(t *testing.T) int {
	t.Helper()
	irBytes, err := os.ReadFile("../../target/selfhost/selfhost.ir")
	if err != nil {
		t.Errorf("read full selfhost IR artifact: %v", err)
		return 1
	}
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
	failures := countEveryEmittedBodyCallClassificationFailures(t, string(irBytes))
	failures += countTextualLLVMValidationFailures(t, string(llBytes), string(metaBytes))
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
	fragments = append(fragments, requiredLLVMMemStartsWithFragments()...)
	fragments = append(fragments, requiredLLVMSourceAbsoluteNameFragments()...)
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
		"%kizu.selfhost.types.constructor_facts.constructor_facts = type { " +
			"%kizu.owned, %kizu.owned, %kizu.owned, %kizu.owned, %kizu.owned }\n",
		"%kizu.selfhost.types.primitive_type.type_record = type { i64, i64, i64, i1 }\n",
		"declare %kizu.owned @kizu_rt_mem_page_allocator()\n",
		"declare ptr @kizu_rt_alloc(ptr, i64)\n",
		"declare void @kizu_rt_free(ptr, ptr)\n",
		"declare void @llvm.memcpy.p0.p0.i64",
		"declare void @llvm.trap()\n",
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
		"define %kizu.slice.u8 @kizu_selfhost__backend_hosted_artifact_stem(",
		"define %kizu.error.owned @kizu_selfhost__backend_hosted_artifact_path(",
		"define i64 @kizu_selfhost__parse_buffer_append",
		"define i64 @kizu_selfhost__parse_next_semantic_token_index",
		"define %kizu.error.owned @kizu_selfhost__parser_format_format_source(",
		"call %kizu.error.owned @kizu_selfhost__parser_format_format_source(",
		"define i1 @kizu_selfhost__parse_format_write",
		"define i1 @kizu_selfhost__format_source_write",
		"define i1 @kizu_selfhost__format_source_file_write",
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
		"%format_newline = call %kizu.error.void @kizu_rt_io_write_stdout",
		"%is_fmt = call i1 @kizu_selfhost__slice_equal",
		"dispatch_fmt:",
		"%fmt_format_ok = call i1 @kizu_selfhost__format_source_write",
		"dispatch_fmt_write_arg:",
		"dispatch_fmt_write:",
		"%fmt_write_format_ok = call i1 @kizu_selfhost__format_source_file_write",
		"%run_parsed = call %kizu.kizu.ast.parse_result " +
			"@kizu_selfhost__cli_parse_validated_ast",
		"%run_render = call %kizu.error.owned " +
			"@kizu_selfhost__cli_execute_render_checked_run_artifact",
		"%test_parsed = call %kizu.kizu.ast.parse_result " +
			"@kizu_selfhost__cli_parse_validated_ast",
		"%test_lowered = call %kizu.error.kizu.selfhost.backend.data.executable " +
			"@kizu_selfhost__backend_executable_lower_test_executable",
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
		"define %kizu.error.bool @kizu_selfhost__cli_check_fast_diagnostics_parsed_file",
		"%check_semantic_result = call %kizu.error.bool " +
			"@kizu_selfhost__cli_check_fast_diagnostics_parsed_file",
		"%run_check_semantic_result = call %kizu.error.bool " +
			"@kizu_selfhost__cli_check_fast_diagnostics_parsed_file",
		"%test_check_semantic_result = call %kizu.error.bool " +
			"@kizu_selfhost__cli_check_fast_diagnostics_parsed_file",
		"define i64 @kizu_selfhost__cli_main() {\n",
		"define i64 @kizu_selfhost__smoke() {\n",
	}
}

// requiredLLVMExecutableFragments returns mandatory hosted executable fragments.
func requiredLLVMExecutableFragments() []string {
	fragments := []string{
		"%kizu.selfhost.backend.data.executable = type { i64, %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.selfhost.ir.codegen.code_eval = type { i1, i64, i64 }",
		"%kizu.error.kizu.selfhost.ir.codegen.code_eval = type { i1, " +
			"%kizu.selfhost.ir.codegen.code_eval, %kizu.slice.u8 }",
		"define %kizu.error.kizu.selfhost.backend.data.executable " +
			"@kizu_selfhost__backend_executable_lower_test_executable",
		// #1255 slice4 PR-1: const_string_value / const_string_instruction /
		// call_instruction / return_void_instruction / lowered_main_print_program were
		// severed (closure-excluded). Only the live metadata_line constant + define
		// remain; their absence is locked in by forbiddenSeveredShapeEmissionFragments.
		"@.kizu.cli.codegen_metadata_line = private unnamed_addr constant",
		"define %kizu.slice.u8 @kizu_selfhost__ir_codegen_metadata_line()",
		"define %kizu.kizu.ast.parse_result @kizu_selfhost__cli_parse_validated_ast",
		// issue 1157: cli_parse_validated_ast is now a thin glue that calls the compiled
		// std::kizu::parser::parse_program instead of the retired hand-written tokenize/parse loop.
		"%parsed_eu = call %kizu.error.kizu.kizu.ast.parse_result @kizu_kizu__parser_parse_program(",
		"call void @kizu_rt_process_exit(i64 1)",
		"define %kizu.error.owned @kizu_selfhost__cli_execute_render_checked_run_artifact",
		"define %kizu.error.owned @kizu_selfhost__ir_code_render_render_run_artifact",
	}
	fragments = append(fragments, requiredLLVMRunCodegenLoweringFragments()...)
	fragments = append(fragments, requiredLLVMAstAccessorFragments()...)
	fragments = append(fragments, requiredLLVMAstChildAssemblyFragments()...)
	fragments = append(fragments, requiredLLVMArrayGetFragments()...)
	fragments = append(fragments, requiredLLVMChildAtFragments()...)
	fragments = append(fragments, requiredLLVMUnionAbiFragments()...)
	fragments = append(fragments, requiredLLVMArenaGetFragments()...)
	fragments = append(fragments, requiredLLVMMatchUnionFragments()...)
	fragments = append(fragments, requiredLLVMNodeCountTypeFragments()...)
	fragments = append(fragments, requiredLLVMNodeCountLoweringFragments()...)
	fragments = append(fragments, requiredLLVMTextAccessorFragments()...)
	fragments = append(fragments, requiredLLVMStringLiteralSpanFragments()...)
	fragments = append(fragments, requiredLLVMLexerClassifierFragments()...)
	fragments = append(fragments, requiredLLVMLexerAdvanceFragments()...)
	fragments = append(fragments, requiredLLVMLexerTokenFragments()...)
	fragments = append(fragments, requiredLLVMTokenizerFragments()...)
	fragments = append(fragments, requiredLLVMParserPredicateFragments()...)
	fragments = append(fragments, requiredLLVMSourcePredicateFragments()...)
	fragments = append(fragments, requiredLLVMSourceModulePathFragments()...)
	fragments = append(fragments, requiredLLVMSourcePackagePrefixFragments()...)
	fragments = append(fragments, requiredLLVMSourceLoaderFragments()...)
	return append(fragments, []string{
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_codegen_payload_llvm_c_string",
		"define i64 @kizu_selfhost__cli_codegen_write_llvm_escape",
		"%escape_next_write = call i64 @kizu_selfhost__cli_codegen_write_llvm_escape",
		"define i1 @kizu_selfhost__cli_emit_run_codegen_artifact",
		"%run_emitted = call i1 @kizu_selfhost__cli_emit_run_codegen_artifact",
		"%run_link_result = call %kizu.error.i64 @kizu_rt_process_spawn_wait8",
		"%run_artifact_result = call %kizu.error.i64 @kizu_rt_process_spawn_wait8",
	}...)
}

// requiredLLVMSourcePredicateFragments locks SourceKind predicates compiled through
// mini MIR enum tag comparisons. The SourceKind discriminants come from source.kizu
// enum facts rather than hardcoded codegen branches.
func requiredLLVMSourcePredicateFragments() []string {
	return []string{
		"define i1 @kizu_selfhost__source_is_source_code",
		"%t2 = icmp ne i64 %kind, 0",
		"ret i1 %t2",
		"define i1 @kizu_selfhost__source_is_frontend_source",
		"%t2 = icmp eq i64 %kind, 1",
		"br i1 %t2, label %if0_then, label %if0_cont",
		"if0_then:\n  ret i1 true",
		"%t5 = icmp eq i64 %kind, 3",
		"ret i1 %t5",
	}
}

// requiredLLVMSourceModulePathFragments locks source::module_path compiled through
// mini MIR field access plus the generic checked slice-expression return.
func requiredLLVMSourceModulePathFragments() []string {
	return []string{
		"@.kizu.compiled.kizu_selfhost__source_module_path.s0 = " +
			"private unnamed_addr constant [8 x i8] c\"manifest\"",
		"define %kizu.slice.u8 @kizu_selfhost__source_module_path",
		"%t0 = extractvalue %kizu.selfhost.source.source_file %file, 1",
		"%t2 = icmp eq i64 %t0, 0",
		"%t4 = extractvalue %kizu.selfhost.source.source_file %file, 5",
		"%t5 = extractvalue %kizu.selfhost.source.source_file %file, 3",
		"%t6 = extractvalue %kizu.selfhost.source.source_file %file, 4",
		"%t7_srclen = extractvalue %kizu.slice.u8 %t4, 1",
		"%t7_endbefore = icmp slt i64 %t6, %t5",
		"%t7_bad = or i1 %t7_badlower, %t7_endhigh",
		"br i1 %t7_bad, label %t7_slice_oob, label %t7_slice_ok",
		"%t7_gep = getelementptr i8, ptr %t7_baseptr, i64 %t5",
		"%t7_len = sub i64 %t6, %t5",
		"ret %kizu.slice.u8 %t7",
	}
}

// requiredLLVMSourceAbsoluteNameFragments locks source::is_absolute_name_for_file compiled through
// short-circuit OR return lowering. The two string literal call arguments must receive distinct
// globals so the std:: and selfhost:: prefix checks do not collide before the package-prefix helper
// call.
func requiredLLVMSourceAbsoluteNameFragments() []string {
	return []string{
		"@.kizu.compiled.kizu_selfhost__source_is_absolute_name_for_file.s0 = " +
			"private unnamed_addr constant [5 x i8] c\"std::\"",
		"@.kizu.compiled.kizu_selfhost__source_is_absolute_name_for_file.s1 = " +
			"private unnamed_addr constant [10 x i8] c\"selfhost::\"",
		"define i1 @kizu_selfhost__source_is_absolute_name_for_file",
		"%arg1000000_1_ptr = getelementptr [5 x i8], ptr " +
			"@.kizu.compiled.kizu_selfhost__source_is_absolute_name_for_file.s0",
		"%t0 = call i1 @kizu_std__mem_starts_with(" +
			"%kizu.slice.u8 %name, %kizu.slice.u8 %arg1000000_1_slice)",
		"br i1 %t0, label %if0_then, label %if0_cont",
		"if0_then:\n  ret i1 true",
		"%arg1000001_1_ptr = getelementptr [10 x i8], ptr " +
			"@.kizu.compiled.kizu_selfhost__source_is_absolute_name_for_file.s1",
		"%t1 = call i1 @kizu_std__mem_starts_with(" +
			"%kizu.slice.u8 %name, %kizu.slice.u8 %arg1000001_1_slice)",
		"br i1 %t1, label %if1_then, label %if1_cont",
		"if1_then:\n  ret i1 true",
		"%t2 = call i1 @kizu_selfhost__source_starts_with_package_prefix(" +
			"%kizu.selfhost.source.source_file %file, %kizu.slice.u8 %name)",
		"ret i1 %t2",
	}
}

// requiredLLVMSourcePackagePrefixFragments locks source::starts_with_package_prefix
// compiled through mini MIR field lets, stdlib slice calls, checked slice call
// arguments, checked byte indexes, and the final boolean conjunction.
func requiredLLVMSourcePackagePrefixFragments() []string {
	return []string{
		"define i1 @kizu_selfhost__source_starts_with_package_prefix",
		"%t0 = extractvalue %kizu.selfhost.source.source_file %file, 2",
		"%package_len = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %package_name)",
		"%t3 = icmp slt i64 %package_len, 1",
		"%name_len = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %name)",
		"%t6 = icmp slt i64 %name_len, %package_len",
		"%arg1000007_0_send = add i64 %package_len, 0",
		"%t7 = call i1 @kizu_selfhost__slice_equal(" +
			"%kizu.slice.u8 %arg1000007_0_slice, %kizu.slice.u8 %package_name)",
		"%t8 = xor i1 %t7, true",
		"%t11 = icmp eq i64 %name_len, %package_len",
		"%t16 = icmp sle i64 %name_len, %t15",
		"%t18_gep = getelementptr i8, ptr %t18_ptr, i64 %package_len",
		"%t20 = icmp eq i8 %t18, 58",
		"%t23 = add i64 %package_len, 1",
		"%t24_gep = getelementptr i8, ptr %t24_ptr, i64 %t23",
		"%t26 = icmp eq i8 %t24, 58",
		"%t27 = and i1 %t20, %t26",
		"ret i1 %t27",
	}
}

// requiredLLVMSourceLoaderFragments locks source-loader helpers compiled through
// mini MIR LetExpr/IndexExpr support. Var reads are copy-propagated so comparisons,
// checked index operands, and offset arithmetic use their source SSA names directly;
// literal operands are consumed without throwaway const temps.
func requiredLLVMSourceLoaderFragments() []string {
	return []string{
		"define i64 @kizu_selfhost__source_loader_package_module_end",
		"%end = call i64 @kizu_selfhost__source_loader_module_end_for(%kizu.slice.u8 %path)",
		"define i64 @kizu_selfhost__source_loader_module_end_for",
		"@.kizu.compiled.kizu_selfhost__source_loader_module_end_for.s0 = " +
			"private unnamed_addr constant [5 x i8] c\".kizu\"",
		"define i1 @kizu_selfhost__source_loader_is_manifest_root_source",
		"%t2 = icmp slt i64 %path_len, %module_root_len",
		"%t5 = sub i64 %path_len, %module_root_len",
		"%t8 = icmp sgt i64 %start, 0",
		"%t11 = sub i64 %start, 1",
		"%t14 = icmp ne i8 %t12, 47",
		"%arg5_0_sstart = add i64 %start, 0",
		"%arg5_0_send = add i64 %path_len, 0",
		"define i64 @kizu_selfhost__source_loader_package_module_start",
		"%t2 = icmp slt i64 %source_root_len, 0",
		"%t6 = icmp sle i64 %t4, %source_root_len",
		"%t9_gep = getelementptr i8, ptr %t9_ptr, i64 %source_root_len",
		"%t11 = icmp ne i8 %t9, 47",
		"%t15 = add i64 %source_root_len, 1",
		"%kizu.selfhost.source.source_file = type { i64, i64, %kizu.slice.u8, " +
			"i64, i64, %kizu.slice.u8, %kizu.slice.u8 }",
		"define %kizu.selfhost.source.source_file @kizu_selfhost__source_loader_source_file",
		"%v0_0 = insertvalue %kizu.selfhost.source.source_file poison, i64 %id, 0",
		"%v0_1 = insertvalue %kizu.selfhost.source.source_file %v0_0, i64 %kind, 1",
		"%v0_6 = insertvalue %kizu.selfhost.source.source_file %v0_5, %kizu.slice.u8 %text, 6",
		"ret %kizu.selfhost.source.source_file %v0_6",
	}
}

// requiredLLVMRunCodegenLoweringFragments returns run-artifact helper members
// still needed by the tape-backed renderer compiled into stage2.
func requiredLLVMRunCodegenLoweringFragments() []string {
	return []string{
		// empty_payload_span and string_literal_span are still used by the current
		// code-render path for tape-backed run artifacts.
		"%kizu.selfhost.ir.codegen.payload_span = type { i64, i64 }",
		"define %kizu.selfhost.ir.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_empty_payload_span",
		"define i1 @kizu_selfhost__ir_codegen_is_payload_supported",
		"%index = phi i64 [ 0, %loop4_preheader ], [ %index_next, %loop4_latch ]",
		"%t5 = icmp slt i64 %index, %t4",
		", label %loop4_body, label %loop4_exit",
		"%index_next = add i64 %index, 1",
		"br i1 %t7_bad, label %t7_idx_oob, label %t7_idx_ok",
		"%t7_gep = getelementptr i8, ptr %t7_ptr, i64 %index",
		"%kizu.kizu.ast.child_range = type { i64, i64 }",
		"define %kizu.error.i64 @kizu_selfhost__ir_codegen_parse_int_literal",
		"call %kizu.error.i64 @kizu_selfhost__ir_codegen_parse_int_literal",
	}
}

// requiredLLVMAstAccessorFragments returns the tracker-961 read-only AST accessor
// members compiled into stage2. std::kizu::ast::Ast.begin_children carries the shape:
// the Ast container value lowers with its nodes (Arena) and children (Array) fields as
// %kizu.owned heap handles; the compiled define reads the children Array (struct
// field index 1) and returns its length via the array_len builtin, which reads the
// len field (index 2) out of the %kizu.rt.array record the handle wraps. No
// AstNode/AstData union, Arena.get, array_get, or error-union return is touched.
// Ast.len has the identical body ('return self.children.len();') and used to anchor this
// list, but nothing calls it any more, so reachability keeps it out of the artifact.
func requiredLLVMAstAccessorFragments() []string {
	return []string{
		"%kizu.kizu.ast.source_file = type { %kizu.slice.u8, %kizu.slice.u8 }",
		"%kizu.kizu.ast.ast = type { %kizu.owned, %kizu.owned, " +
			"%kizu.kizu.ast.source_file }",
		"%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }",
		"define i64 @kizu_kizu__ast_ast_begin_children",
		"%arg0_0_ex = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%result = call i64 @kizu_rt_array_len(%kizu.owned %arg0_0_ex)",
		"define i64 @kizu_rt_array_len(%kizu.owned %array)",
		"%len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2",
	}
}

// requiredLLVMAstChildAssemblyFragments locks the issue-1157 NEW-A child-array
// mutator body primitives compiled into stage2: a field-receiver Array.append
// and ChildRange { len: self.children.len() - start } without backend name-special-cases.
// The append lowers through the shared runtime-method path, so the receiver is a
// call-arg field extraction and the element is marshalled into the %kizu.slice.u8
// byte view the builtin contract declares for the element parameter.
func requiredLLVMAstChildAssemblyFragments() []string {
	return []string{
		"define i64 @kizu_kizu__ast_ast_begin_children",
		"define %kizu.error.void @kizu_kizu__ast_ast_add_child",
		"%arg0_0_ex = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%arg0_1_byte_slot = alloca %kizu.kizu.ast.node_id",
		"store %kizu.kizu.ast.node_id %child, ptr %arg0_1_byte_slot",
		"%arg0_1_storage = insertvalue %kizu.slice.u8 %arg0_1_byte_base, " +
			"i64 ptrtoint (ptr getelementptr (%kizu.kizu.ast.node_id, ptr null, i32 1) to i64), 1",
		"%voidtry0_call = call %kizu.error.void @kizu_rt_array_append(" +
			"%kizu.owned %arg0_0_ex, %kizu.slice.u8 %arg0_1_storage)",
		"define %kizu.kizu.ast.child_range @kizu_kizu__ast_ast_finish_children",
		"%fc0_1_receiver = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%fc0_1_left = call i64 @kizu_rt_array_len(%kizu.owned %fc0_1_receiver)",
		"%fc0_1_value = sub i64 %fc0_1_left, %start",
		"%v0_1 = insertvalue %kizu.kizu.ast.child_range %v0_0, i64 %fc0_1_value, 1",
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
		"%kizu.error.kizu.kizu.diagnostic.related_span = type { i1, " +
			"%kizu.kizu.diagnostic.related_span, " +
			"%kizu.slice.u8 }",
		"define %kizu.error.kizu.kizu.diagnostic.related_span " +
			"@kizu_kizu__diagnostic_diagnostic_related_get",
		"%array = extractvalue %kizu.kizu.diagnostic.diagnostic %self, 2",
		"%view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index)",
		"%view_ok = extractvalue %kizu.error.slice.u8 %view, 0",
		"br i1 %view_ok, label %array_get_ok, label %array_get_fail",
		"%elem = load %kizu.kizu.diagnostic.related_span, ptr %elem_ptr",
		"%ok_value = insertvalue %kizu.error.kizu.kizu.diagnostic.related_span %ok_flag, " +
			"%kizu.kizu.diagnostic.related_span %elem, 1",
		"  ret %kizu.error.kizu.kizu.diagnostic.related_span %ok_value",
		"%fail_value = insertvalue %kizu.error.kizu.kizu.diagnostic.related_span %fail_flag, " +
			"%kizu.slice.u8 %fail_msg, 2",
		"  ret %kizu.error.kizu.kizu.diagnostic.related_span %fail_value",
		"define %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index)",
	}
}

// requiredLLVMChildAtFragments returns the tracker-961 std::kizu::ast::Ast.child_at
// accessor compiled into stage2. It composes the checked Array<NodeId>.get accessor
// with a bounds-check `if index < 0 or index >= range.len { return error(...); }`:
// the `or` short-circuit branches to the shared error block, which materializes the
// "child index out of bounds" message and returns a real canonical NodeId error-union failure
// value (field 2), never `unreachable`. The continuation computes range.start + index
// (extractvalue of the ChildRange + add), reads the children Array (field 1) off the
// Ast value, calls the checked @kizu_rt_array_at, and on success loads the NodeId by
// value and wraps it as the error-union success value (field 1).
func requiredLLVMChildAtFragments() []string {
	return []string{
		"%kizu.kizu.ast.node_id = type { %kizu.handle }",
		"%kizu.error.kizu.kizu.ast.node_id = type { i1, %kizu.kizu.ast.node_id, %kizu.slice.u8 }",
		"define %kizu.error.kizu.kizu.ast.node_id @kizu_kizu__ast_ast_child_at",
		"%kizu.kizu.ast.child_range %range",
		"%t2 = icmp slt i64 %index, 0",
		"br i1 %t2, label %if0_then, label %if0_rhs",
		"%t5 = icmp sge i64 %index, %t4",
		"%t4 = extractvalue %kizu.kizu.ast.child_range %range, 1",
		"br i1 %t5, label %if0_then, label %if0_cont",
		"%errfail6 = insertvalue %kizu.error.kizu.kizu.ast.node_id %errfail6_flag, %kizu.slice.u8 %t6, 2",
		"  ret %kizu.error.kizu.kizu.ast.node_id %errfail6",
		"%t7 = extractvalue %kizu.kizu.ast.child_range %range, 0",
		"%t9 = add i64 %t7, %index",
		"%array = extractvalue %kizu.kizu.ast.ast %self, 1",
		"%view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %t9)",
		"%elem = load %kizu.kizu.ast.node_id, ptr %elem_ptr",
		"%ok_value = insertvalue %kizu.error.kizu.kizu.ast.node_id %ok_flag, %kizu.kizu.ast.node_id %elem, 1",
		"  ret %kizu.error.kizu.kizu.ast.node_id %ok_value",
		"  ret %kizu.error.kizu.kizu.ast.node_id %fail_value",
	}
}

// requiredLLVMUnionAbiFragments returns the tracker-961 AstNode/AstData tagged-union
// value-type ABI compiled into stage2. std::kizu::ast::Ast.add_node constructs an
// AstNode (a Span plus the AstData union value forwarded whole) and appends it to the
// nodes Arena. AstData is the fact-derived inline layout { i64 tag, [136 x i8] payload storage }
// where 136 is the largest variant payload capacity; the 44 variant payload structs are
// not modelled because add_node forwards the union value without inspecting it. The
// element is marshalled through stack storage as a %kizu.slice.u8 (ptr + sizeof via the
// getelementptr-null idiom) and appended through the heap-backed @kizu_rt_arena_add,
// which returns a %kizu.handle that becomes the opaque NodeId payload without
// discarding its arena provenance.
func requiredLLVMUnionAbiFragments() []string {
	return []string{
		"%kizu.kizu.ast.span = type { i64, i64 }",
		"%kizu.kizu.ast.ast_data = type { i64, [136 x i8] }",
		"%kizu.kizu.ast.ast_node = type { %kizu.kizu.ast.span, %kizu.kizu.ast.ast_data }",
		"%kizu.kizu.ast.parse_result = type { %kizu.kizu.ast.ast, %kizu.kizu.ast.node_id }",
		"define %kizu.kizu.ast.node_id @kizu_kizu__ast_ast_add_node",
		"%v0_0 = insertvalue %kizu.kizu.ast.ast_node poison, %kizu.kizu.ast.span %span_value, 0",
		"%v0_1 = insertvalue %kizu.kizu.ast.ast_node %v0_0, %kizu.kizu.ast.ast_data %data, 1",
		"store %kizu.kizu.ast.ast_node %v0_1, ptr %raw_slot, align 8",
		"%raw_slice = insertvalue %kizu.slice.u8 %raw_slice_base, i64 ptrtoint (ptr getelementptr (" +
			"%kizu.kizu.ast.ast_node, ptr null, i32 1) to i64), 1",
		"%raw_arena = extractvalue %kizu.kizu.ast.ast %self, 0",
		"%raw = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %raw_arena, " +
			"%kizu.slice.u8 %raw_slice)",
		"%v2_0 = insertvalue %kizu.kizu.ast.node_id poison, %kizu.handle %raw, 0",
		"  ret %kizu.kizu.ast.node_id %v2_0",
		"define %kizu.kizu.ast.parse_result @kizu_kizu__ast_parse_result",
		"%v0_0 = insertvalue %kizu.kizu.ast.parse_result poison, %kizu.kizu.ast.ast %ast, 0",
		"%v0_1 = insertvalue %kizu.kizu.ast.parse_result %v0_0, %kizu.kizu.ast.node_id %root, 1",
		"  ret %kizu.kizu.ast.parse_result %v0_1",
		"define %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %value)",
	}
}

// requiredLLVMArenaGetFragments returns the tracker-961 std::kizu::ast::Ast.get
// accessor compiled into stage2. It reads an AstNode back out of the nodes Arena by
// value (self.nodes.get(id.raw)), reusing the AstNode/AstData union value-type ABI: it
// extracts the opaque handle from the NodeId.raw field, reads the Arena owner off the
// Ast value, calls the checked self-contained @kizu_rt_arena_get, and on success loads
// the AstNode element by value and returns it. A foreign-arena or out-of-bounds handle traps via
// @kizu_rt_trap (Arena.get panics rather than returning an error union, so the trap is
// the genuine failure semantics, not a stubbed-out branch).
func requiredLLVMArenaGetFragments() []string {
	return []string{
		"define %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get",
		"%handle = extractvalue %kizu.kizu.ast.node_id %id, 0",
		"%arena = extractvalue %kizu.kizu.ast.ast %self, 0",
		"%view = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %handle)",
		"br i1 %view_ok, label %arena_get_ok, label %arena_get_fail",
		"%elem = load %kizu.kizu.ast.ast_node, ptr %elem_ptr",
		"  ret %kizu.kizu.ast.ast_node %elem",
		"call void @kizu_rt_trap(%kizu.slice.u8 %fail_msg)",
		"@.kizu.rt.arena_invalid_handle = private unnamed_addr constant [20 x i8] c\"invalid arena handle\"",
		"define %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %handle)",
		"%handle_arena = extractvalue %kizu.handle %handle, 0",
		"%same_arena = icmp eq ptr %raw, %handle_arena",
		"%index = extractvalue %kizu.handle %handle, 1",
		"%provenance_ok = and i1 %same_arena, %index_nonnegative",
		"%bounds_ok = and i1 %provenance_ok, %index_in_bounds",
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
		"%kizu.kizu.ast.token_id = type { i64 }",
		"%kizu.kizu.ast.symbol_id = type { i64 }",
		"%kizu.kizu.ast.int_node = type { %kizu.kizu.ast.token_id }",
		"%kizu.kizu.ast.string_node = type { %kizu.kizu.ast.token_id }",
		"%kizu.kizu.ast.type_name_node = type { %kizu.kizu.ast.symbol_id }",
		"%kizu.kizu.ast.var_node = type { %kizu.kizu.ast.symbol_id, %kizu.kizu.ast.span }",
		"%kizu.kizu.ast.bool_node = type { i1 }",
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
		"%kizu.kizu.ast.err_defer_node = type { %kizu.kizu.ast.node_id }",
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
// AST-traversal cluster compiled into stage2: node_count (the 45-arm match-over-AstData
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
		// the exhaustive icmp/br arm chain (Program tag 0 first, FnDecl tag 43, ...).
		"%match_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.node_id %node_id)",
		"%match_tag = extractvalue %kizu.kizu.ast.ast_data %match_data, 0",
		"%match_is_0 = icmp eq i64 %match_tag, 0",
		"br i1 %match_is_0, label %match_arm_0, label %match_check_1",
		// node_count Program arm: load the ProgramNode payload, forward declarations to
		// count_with_range, and return its (identically-typed) error union directly.
		"%match_arm_0_payload = load %kizu.kizu.ast.program_node, " +
			"ptr %match_arm_0_ptr, align 8",
		"%match_arm_0_a1 = extractvalue %kizu.kizu.ast.program_node %match_arm_0_payload, 0",
		"%match_arm_0_call = call %kizu.error.i64 @kizu_selfhost__ast_count_with_range(" +
			"%kizu.kizu.ast.ast %tree, %kizu.kizu.ast.child_range %match_arm_0_a1)",
		"  ret %kizu.error.i64 %match_arm_0_call",
		// count_range: a two-phi accumulator loop over the range calling the checked
		// Ast.child_at and the recursive node_count, propagating either failure and returning
		// the accumulated count wrapped as the success.
		"%range_len = extractvalue %kizu.kizu.ast.child_range %range, 1",
		"%count = phi i64 [ 0, %entry ], [ %count_next, %count_nc_cont ]",
		"%index = phi i64 [ 0, %entry ], [ %index_next, %count_nc_cont ]",
		"%child_call = call %kizu.error.kizu.kizu.ast.node_id @kizu_kizu__ast_ast_child_at(" +
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

// requiredLLVMMemStartsWithFragments locks std::mem::starts_with compiled into stage2 as a
// selfhost-owned checked-index slice-prefix predicate for resolved source-call users.
func requiredLLVMMemStartsWithFragments() []string {
	return []string{
		"define i1 @kizu_std__mem_starts_with(",
		"%prefix_len = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %prefix)",
		"%t1 = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %bytes)",
		"%t2 = icmp sgt i64 %prefix_len, %t1",
		"%index = phi i64 [ 0, %loop3_preheader ], [ %index_next, %loop3_latch ]",
		"%t5 = icmp slt i64 %index, %prefix_len",
		"%t7_bad = or i1 %t7_neg, %t7_high",
		"br i1 %t7_bad, label %t7_idx_oob, label %t7_idx_ok",
		"%t9_bad = or i1 %t9_neg, %t9_high",
		"br i1 %t9_bad, label %t9_idx_oob, label %t9_idx_ok",
		"%t10 = icmp ne i8 %t7, %t9",
		"%index_next = add i64 %index, 1",
		"loop3_exit:\n  ret i1 true",
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
		"define %kizu.selfhost.ir.codegen.payload_span " +
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
		"%sls_r1 = insertvalue %kizu.selfhost.ir.codegen.payload_span %sls_r0, " +
			"i64 %sls_payload_end, 1",
		"%sls_empty_call = call %kizu.selfhost.ir.codegen.payload_span " +
			"@kizu_selfhost__ir_codegen_empty_payload_span()",
	}
}

// requiredLLVMLexerClassifierFragments returns the tracker-961 scope-4 prerequisite
// std::kizu::lexer leaf byte classifiers compiled into stage2: is_alpha / is_digit / is_space
// (range/or-chain byte predicates over literal i8 bounds) and is_word (which or-combines is_alpha /
// is_digit). They are the bottom of the lexer compile chain the eventual scanner removal needs
// (source -> Ast requires the compiled tokenizer). is_word's i8 call arguments are resolved from
// the exact source signature rather than a default slice ABI. These fragments lock the shapes.
func requiredLLVMLexerClassifierFragments() []string {
	fragments := []string{
		"define i1 @kizu_kizu__lexer_is_alpha(",
		"define i1 @kizu_kizu__lexer_is_digit(",
		"define i1 @kizu_kizu__lexer_is_space(",
		"define i1 @kizu_kizu__lexer_is_word(",
		"%t2 = icmp sge i8 %byte, 65",
		"%t5 = icmp sle i8 %byte, 90",
		"%t17 = icmp eq i8 %byte, 95",
		"%t0 = call i1 @kizu_kizu__lexer_is_alpha(i8 %byte)",
		"%t1 = call i1 @kizu_kizu__lexer_is_digit(i8 %byte)",
		// position(line, column) builds the fact-derived Position cursor struct.
		"%kizu.kizu.lexer.position = type { i64, i64 }",
		"define %kizu.kizu.lexer.position @kizu_kizu__lexer_position(",
		"insertvalue %kizu.kizu.lexer.position %v0_0, i64 %column, 1",
		// Token is the fact-derived tokenizer output record.
		"%kizu.kizu.lexer.token = type { i64, i64, i64, i64, i64, i64, i64 }",
	}
	return append(fragments, requiredLLVMSelfhostLexerFragments()...)
}

// requiredLLVMSelfhostLexerFragments locks the selfhost lexer byte-class helper chain compiled
// through seed closure. is_identifier_continue reaches is_identifier_start and is_digit; the
// start helper then reaches is_alpha.
func requiredLLVMSelfhostLexerFragments() []string {
	return []string{
		"define i1 @kizu_selfhost__lexer_is_identifier_continue(",
		"%t0 = call i1 @kizu_selfhost__lexer_is_identifier_start(i8 %byte)",
		"%t1 = call i1 @kizu_selfhost__lexer_is_digit(i8 %byte)",
		"define i1 @kizu_selfhost__lexer_is_identifier_start(",
		"%t0 = call i1 @kizu_selfhost__lexer_is_alpha(i8 %byte)",
		"define i1 @kizu_selfhost__lexer_is_digit(",
		"define i1 @kizu_selfhost__lexer_is_alpha(",
		"define %kizu.slice.u8 @kizu_selfhost__lexer_invalid_token_display()",
		"@.kizu.compiled.kizu_selfhost__lexer_invalid_token_display = " +
			"private unnamed_addr constant [7 x i8] c\"ILLEGAL\"",
	}
}

// requiredLLVMLexerAdvanceFragments pins advance_byte(byte, initial): the lexer cursor
// step that calls position(...) in both the newline and same-line arms, exercising the
// int-literal and field-arith call args added for the run-codegen frontend (tracker 961,
// scope 4 prerequisite).
func requiredLLVMLexerAdvanceFragments() []string {
	return []string{
		"define %kizu.kizu.lexer.position @kizu_kizu__lexer_advance_byte(",
		// position(initial.line + 1, 1): field-arith arg (extract line, add 1) + int literal.
		"%arg0_0_ex = extractvalue %kizu.kizu.lexer.position %initial, 0",
		"%arg0_0_arith = add i64 %arg0_0_ex, 1",
		"call %kizu.kizu.lexer.position @kizu_kizu__lexer_position(i64 %arg0_0_arith, i64 1)",
		// position(initial.line, initial.column + 1): plain field arg + field-arith arg.
		"%arg1_1_ex = extractvalue %kizu.kizu.lexer.position %initial, 1",
		"%arg1_1_arith = add i64 %arg1_1_ex, 1",
		// advance_position(source, start, end, initial): a two-phi loop threading the i64 index
		// and the Position struct current, folding advance_byte across source[start..end].
		"define %kizu.kizu.lexer.position @kizu_kizu__lexer_advance_position(",
		"%ap_index = phi i64 [ %start, %entry ], [ %ap_index_next, %ap_loop_body ]",
		"%ap_current = phi %kizu.kizu.lexer.position [ %initial, %entry ], " +
			"[ %ap_next, %ap_loop_body ]",
		"%ap_next = call %kizu.kizu.lexer.position @kizu_kizu__lexer_advance_byte(" +
			"i8 %ap_byte, %kizu.kizu.lexer.position %ap_current)",
		"ret %kizu.kizu.lexer.position %ap_current",
	}
}

// requiredLLVMLexerTokenFragments pins the leaf Token constructors token / attach_doc: token
// reads current.line / current.column off the Position param into a Token struct literal, and
// attach_doc rebuilds the raw Token reading every field off it (tracker 961, scope 4
// prerequisite). Both lower through the struct-return path.
func requiredLLVMLexerTokenFragments() []string {
	return []string{
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_token(",
		// token reads current.line (field 0) and current.column (field 1) off the Position param.
		"extractvalue %kizu.kizu.lexer.position %current, 0",
		"extractvalue %kizu.kizu.lexer.position %current, 1",
		"ret %kizu.kizu.lexer.token ",
		// attach_doc rebuilds the raw Token, reading raw.kind (field 0) off the Token param.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_attach_doc(",
		"extractvalue %kizu.kizu.lexer.token %raw, 0",
		// number_token scans a digit run: an i64 end phi advancing while source[end] is a digit,
		// then building the Number Token off token(...).
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_number_token(",
		"%nt_end = phi i64 [ %start, %entry ], [ %nt_next, %nt_body ]",
		"%nt_isd = call i1 @kizu_kizu__lexer_is_digit(i8 %nt_byte)",
		"i64 %start, i64 %nt_end, %kizu.kizu.lexer.position %current)",
		// string_token handles both quoted strings and `\\` multiline strings inside the selected
		// helper body, so raw_token_at does not need a separate multiline helper body fact.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_string_token(",
		"%st_multiline_pair = and i1 %st_first_bs, %st_second_bs",
		"%st_ml_end = phi i64 [ %st_ml_after_slashes, %st_ml_head ], " +
			"[ %st_ml_line_next, %st_ml_line_body ]",
		"%st_probe_second_bs = icmp eq i8 %st_probe_second_byte, 92",
		"%st_end = phi i64 [ %st_start_next, %entry ], " +
			"[ %st_start_next, %st_check_multiline_pair ], [ %st_next, %st_body ]",
		"%st_notq = icmp ne i8 %st_byte, 34",
		"%st_endp1 = add i64 %st_end, 1",
		// skip_line_comment threads a two-phi loop (i64 end + Position current) past a comment line,
		// folding advance_byte until newline (byte 10), then building the Eof Token.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_skip_line_comment(",
		"%slc_current = phi %kizu.kizu.lexer.position [ %initial, %entry ], " +
			"[ %slc_advanced, %slc_body ]",
		"%slc_notnl = icmp ne i8 %slc_byte, 10",
		"%slc_advanced = call %kizu.kizu.lexer.position @kizu_kizu__lexer_advance_byte(" +
			"i8 %slc_byte, %kizu.kizu.lexer.position %slc_current)",
		// is_doc_comment_start: a bounded branch chain testing three slash bytes (47) plus a
		// fourth-slash demotion, returning i1.
		"define i1 @kizu_kizu__lexer_is_doc_comment_start(",
		"%dc_is0 = icmp eq i8 %dc_b0, 47",
		"%dc_ne = icmp ne i8 %dc_b3v, 47",
		// word_token: an is_word scan loop, an empty-run single-byte Ident, a keyword table
		// compared via equal_bytes against private string constants, and a multi-byte Ident.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_word_token(",
		"%wt_isw = call i1 @kizu_kizu__lexer_is_word(i8 %wt_byte)",
		"@.kizu.compiled.kizu_kizu__lexer_word_token.kw0 = " +
			"private unnamed_addr constant [2 x i8] c\"fn\"",
		"%wt_kw0_eq = call i1 @kizu_selfhost__slice_equal(" +
			"%kizu.slice.u8 %wt_text, %kizu.slice.u8 %wt_kw0_slice)",
		"br i1 %wt_kw0_eq, label %wt_kw0_ret, label %wt_kw1_check",
	}
}

// requiredLLVMTokenizerFragments pins the tokenizer driver functions: the byte classifier
// raw_token_at, the integration loop token_at, the entry points first_token / next_token, and the
// Array<Token> builder tokenize (tracker 961, scope 4 prerequisite).
func requiredLLVMTokenizerFragments() []string {
	return []string{
		// raw_token_at: an Eof guard, an operator dispatch table over the byte at start, then the
		// quote / multiline-string / digit / word fallbacks to string_token / number_token /
		// word_token.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_raw_token_at(",
		"%rt_eof = icmp sge i64 %start, %rt_length",
		"%rt_byte = load i8, ptr %rt_bptr",
		"%op0_is = icmp eq i8 %rt_byte, 123",
		"%rt_isbs = icmp eq i8 %rt_byte, 92",
		"%rt_is_multiline = icmp eq i8 %rt_next_byte, 92",
		"%rt_isd = call i1 @kizu_kizu__lexer_is_digit(i8 %rt_byte)",
		"%rt_wt = call %kizu.kizu.lexer.token @kizu_kizu__lexer_word_token(" +
			"%kizu.slice.u8 %source, i64 %start, %kizu.kizu.lexer.position %current)",
		// token_at: an outer loop (start / current / has_doc / doc_start / doc_end phis) with an
		// inner whitespace loop, comment skipping, and the final attach_doc(raw_token_at(...)).
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_token_at(",
		"%ta_has_doc = phi i1 [ false, %entry ], [ %ta_doc_comment, %ta_comment ]",
		"%ta_ws_isspace = call i1 @kizu_kizu__lexer_is_space(i8 %ta_ws_byte)",
		"%ta_skipped = call %kizu.kizu.lexer.token @kizu_kizu__lexer_skip_line_comment(",
		"%ta_tok = call %kizu.kizu.lexer.token @kizu_kizu__lexer_attach_doc(",
		// first_token / next_token: tokenizer entry points wrapping token_at / advance_position.
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_first_token(",
		"%ft_pos = call %kizu.kizu.lexer.position @kizu_kizu__lexer_position(i64 1, i64 1)",
		"define %kizu.kizu.lexer.token @kizu_kizu__lexer_next_token(",
		"%nt_next = call %kizu.kizu.lexer.position @kizu_kizu__lexer_advance_position(",
		// tokenize: build a dynamic Token array via the runtime helpers and fold first/next_token
		// into it, returning the array as the error-union-owned success. It lowers through the
		// generic ArrayNew + ValueWhile path (issue 1165): the element width comes from the element
		// type (ptrtoint), the carried Token threads through a value phi, and the seed/iteration
		// appends propagate failure as the error-union-owned failure.
		"define %kizu.error.owned @kizu_kizu__lexer_tokenize(",
		"%tokens = call %kizu.owned @kizu_rt_array_new(%kizu.owned %allocator, " +
			"i64 ptrtoint (ptr getelementptr (%kizu.kizu.lexer.token, ptr null, i32 1) to i64))",
		"%current = phi %kizu.kizu.lexer.token [ %current_init, %entry ], " +
			"[ %current_next, %vw0_after ]",
		"%vw0_seed_app = call %kizu.error.void @kizu_rt_array_append(",
		"%vw0_ok1 = insertvalue %kizu.error.owned %vw0_ok0, %kizu.owned %tokens, 1",
	}
}

// requiredLLVMParserPredicateFragments pins std::kizu::parser TokenKind leaves: predicates that
// extract the TokenKind field off the Token param and compare it to one or more matched variant
// discriminants, returning i1 (tracker 961, scope 4 prerequisite).
// requiredLLVMFormatHelperFragments locks the first selfhost::parser::format
// read-only helpers compiled into stage2 (issue 1165 / issue 1162). The four
// TokenKind predicates lower through the shared token-kind predicate MIR (an
// extractvalue of the Token kind field compared against the lexer enum
// discriminant), and token_text lowers through the generic single-statement
// expression path to a bounds-checked source[start..end] slice. next_token_text_equals
// reads tokens.len()/tokens.get(...) on a value-receiver Array<Token> param, and
// index_after_import scans the token array through the generic bounded counter loop with
// a parameter-seeded induction variable (var index = import_index) and i64 early returns.
// These are the first formatter component members on the compiled path; their presence
// here pins that the catalog-driven format closure keeps emitting them as real compiled
// functions.
func requiredLLVMFormatHelperFragments() []string {
	fragments := []string{
		"define i1 @kizu_selfhost__parser_format_is_import_token(",
		"define i1 @kizu_selfhost__parser_format_is_ident_token(",
		"define i1 @kizu_selfhost__parser_format_is_double_colon_token(",
		"define i1 @kizu_selfhost__parser_format_is_semicolon_token(",
		"define %kizu.slice.u8 @kizu_selfhost__parser_format_token_text(",
		"define %kizu.error.bool @kizu_selfhost__parser_format_next_token_text_equals(",
		"define %kizu.error.i64 @kizu_selfhost__parser_format_index_after_import(",
		// Pin the parameter-seeded induction phi: the loop counter seeds from the
		// %import_index parameter SSA value on the preheader edge, not a literal, so a
		// regression back to a literal seed is caught here (issue 1165).
		"%index = phi i64 [ %import_index, %loop1_preheader ]",
		// Pin the i64 error-union early-return wrap: 'return index + 1;' inside the loop
		// wraps the i64 into %kizu.error.i64 with an if-index-suffixed SSA name rather than
		// returning a raw i64 as the error union (issue 1165).
		"%if1002_retexpr_val = insertvalue %kizu.error.i64 %if1002_retexpr_ok, i64 %t7, 1",
		// index_after_leading_imports is the first scan-while whose loop latch is a
		// loop-carried try-call: 'var index = 0; while index < tokens.len() { let token =
		// try tokens.get(index); if !is_import_token(token) { return index; } index = try
		// index_after_import(tokens, index); } return index;' (issue 1165).
		"define %kizu.error.i64 @kizu_selfhost__parser_format_index_after_leading_imports(",
		// Pin the generic loop-carried latch: the phi seeds from the literal 0 on the
		// preheader edge and takes its latch operand %index_next from the try-call
		// continuation block %try1001_cont (the real predecessor), not %loop1_latch, so a
		// regression to a constant-step latch or a wrong phi predecessor is caught.
		"%index = phi i64 [ 0, %loop1_preheader ], [ %index_next, %try1001_cont ]",
		// Pin that the latch update is the index_after_import try-call (resolved through the
		// catalog/BFS, not a literal step) producing the loop-carried %kizu.error.i64.
		"%index_next_call = call %kizu.error.i64 " +
			"@kizu_selfhost__parser_format_index_after_import(%kizu.owned %tokens, i64 %index)",
		// Pin that the phi's latch operand is the try-call success value (field 1), so the
		// loop-carried counter advances by the callee result rather than a raw step.
		"%index_next = extractvalue %kizu.error.i64 %index_next_call, 1",
		// Pin the latch failure propagation: a try-call failure rewraps the callee message
		// into this function's own %kizu.error.i64 and returns it, never a raw i64 or an
		// 'unreachable', so a broken error-union propagation is caught (issue 1165).
		"%index_next_fail_val = insertvalue %kizu.error.i64 %index_next_fail_flag, " +
			"%kizu.slice.u8 %index_next_msg, 2",
		// Pin the 'return index;' early exit wrap on the !is_import_token branch: the i64
		// wraps into %kizu.error.i64 rather than returning a raw i64 as the error union.
		"%if1002_retexpr_val = insertvalue %kizu.error.i64 %if1002_retexpr_ok, i64 %index, 1",
		// format_source is the compiled formatter driver used by the hosted fmt command. Pin the
		// tokenizer call and owned String return shape so the artifact cannot silently fall back to
		// the old parse_format_alloc emitter.
		"define %kizu.error.owned @kizu_selfhost__parser_format_format_source(",
		"%format_tokens_call = call %kizu.error.owned @kizu_kizu__lexer_tokenize(",
		"%index = phi i64 [ %void.then.alias.13, %loop13_preheader ], [ %index_next, %loop13_latch ]",
		"%t24 = icmp slt i64 %index, %t23",
		"%token_view = call %kizu.error.slice.u8 @kizu_rt_array_at(" +
			"%kizu.owned %format_tokens, i64 %index)",
		"%retwrap0_ok = insertvalue %kizu.error.owned zeroinitializer, i1 true, 0",
		"%retwrap0_val = insertvalue %kizu.error.owned %retwrap0_ok, %kizu.owned %out, 1",
	}
	fragments = append(fragments, requiredLLVMFormatImportSortFragments()...)
	fragments = append(fragments, requiredLLVMFormatSortFragments()...)
	fragments = append(fragments, requiredLLVMFormatLeadingImportFragments()...)
	fragments = append(fragments, requiredLLVMFormatImportDeclFragments()...)
	fragments = append(fragments, requiredLLVMFormatSortedImportsFragments()...)
	fragments = append(fragments, requiredLLVMFormatAppendIndentFragments()...)
	fragments = append(fragments, requiredLLVMFormatLineByteClassifierFragments()...)
	fragments = append(fragments, requiredLLVMFormatAfterLineBreakFragments()...)
	fragments = append(fragments, requiredLLVMFormatLineEndFragments()...)
	fragments = append(fragments, requiredLLVMFormatCommentPreserveFragments()...)
	return append(fragments, requiredLLVMFormatTrailingCommaFragments()...)
}

// requiredLLVMFormatTrailingCommaFragments locks the selfhost::parser::format is_trailing_comma
// helper compiled into stage2 (issue 1165 / 1162): the first compiled member with a try-call in
// sub-expression position. Its '!(try <call>)' guard and '!(try <call>)' return lower through the
// generic prefix-not-of-try-call path -- an if-not-try-call-return-bool statement and a
// return-not-try-call statement -- each namespaced under 'nottry<N>' by the render-time statement
// index. The renderer emits the call, propagates a failure as this function's own %kizu.error.bool
// (never 'unreachable'), negates the unwrapped i1 success with 'xor i1 %success, true', and either
// branches into the guard's bool return or wraps the negation as the error-union success. Pinning
// these fragments keeps the sub-expression try-call capability from regressing to a name-only
// emission or losing its failure propagation / negation.
func requiredLLVMFormatTrailingCommaFragments() []string {
	return []string{
		"define %kizu.error.bool @kizu_selfhost__parser_format_is_trailing_comma(",
		// The 'if !(try next_token_text_equals(source, tokens, index, "}")) { return false; }' guard:
		// the hoisted try-call, its failure propagation as this function's own %kizu.error.bool, the
		// negation of the unwrapped i1, and the branch into the bool-return then-block or the
		// fall-through join the following statements render into.
		"%nottry5_call = call %kizu.error.bool " +
			"@kizu_selfhost__parser_format_next_token_text_equals(",
		"%nottry5_fail_val = insertvalue %kizu.error.bool %nottry5_fail_flag, " +
			"%kizu.slice.u8 %nottry5_msg, 2",
		"%nottry5_neg = xor i1 %nottry5_success, true",
		"br i1 %nottry5_neg, label %nottry5_then, label %nottry5_join",
		"%nottry5_ret_val = insertvalue %kizu.error.bool %nottry5_ret_flag, i1 false, 1",
		// The 'return !(try is_match_arm_trailing_comma(source, tokens, index));' tail: the hoisted
		// try-call, its failure propagation, the negation of the unwrapped i1, and the wrap of the
		// negation as the error-union success.
		"%nottry6_call = call %kizu.error.bool " +
			"@kizu_selfhost__parser_format_is_match_arm_trailing_comma(",
		"%nottry6_fail_val = insertvalue %kizu.error.bool %nottry6_fail_flag, " +
			"%kizu.slice.u8 %nottry6_msg, 2",
		"%nottry6_neg = xor i1 %nottry6_success, true",
		"%nottry6_wrap_val = insertvalue %kizu.error.bool %nottry6_wrap_flag, i1 %nottry6_neg, 1",
	}
}

// requiredLLVMFormatCommentPreserveFragments locks the selfhost::parser::format
// append_preserved_line_comments comment-preservation driver compiled into stage2 through
// compiled_struct_cf (issue 1165 / 1162): the first compiled member returning a struct in an error
// union ('-> !CommentFormatState'). It threads the loop-carried scalar state current_last /
// at_line_start / after_comment through a forward 'while index < end' scan loop whose if/else
// comment arm scans a '// ...' full-line comment and mutates the '&var std::string::String'
// accumulator through the @kizu_rt_string_append_* ABI, and closes with a 'return
// CommentFormatState { ... }' success wrap. Dispatched by its error-union return type.
func requiredLLVMFormatCommentPreserveFragments() []string {
	return []string{
		// The CommentFormatState struct value + error-union ABI define over the String accumulator
		// (%kizu.owned), the source slice, the i64 start/end/depth cursors, the i8 last byte, and the
		// i1 at_line_start flag, so a regression in the struct-in-error-union return ABI is caught.
		"define %kizu.error.comment_format_state " +
			"@kizu_selfhost__parser_format_append_preserved_line_comments(",
		"  i8 %last,",
		"  i1 %at_line_start",
		// Pin the four loop-head carried phis (the i64 cursor plus the three scalar state fields), so
		// a regression in the carried-scalar phi seeding is caught.
		"%index = phi i64 [ %start, %entry ], [ %index_next, %cp_latch ]",
		"%current_last = phi i8 [ %last, %entry ], [ %current_last_next, %cp_latch ]",
		"%current_at_line_start = phi i1 [ %at_line_start, %entry ], " +
			"[ %current_at_line_start_next, %cp_latch ]",
		"%current_after_comment = phi i1 [ false, %entry ], [ %current_after_comment_next, %cp_latch ]",
		// Pin the short-circuit '//' detection: the guarded byte loads run only after the bounds and
		// first-slash checks pass, so a regression to an eager out-of-bounds load is caught.
		"%cp_g1 = icmp slt i64 %cp_ip1, %end",
		"%cp_g2v = icmp eq i8 %cp_c0, 47",
		"%cp_g3v = icmp eq i8 %cp_c1, 47",
		// Pin the out.len() guard of the blank-before insertion against the runtime string-length ABI.
		"%cp_olen = call i64 @kizu_rt_string_len(%kizu.owned %out)",
		// Pin the at_line_start merge after the blank-before insertion (true on the ran edge, the
		// carried value on the three skip edges).
		"%cp_als_b = phi i1 [ %current_at_line_start, %cp_cmt ], " +
			"[ %current_at_line_start, %cp_blank0 ], [ %current_at_line_start, %cp_len ], " +
			"[ true, %cp_in2_cont ]",
		// Pin the comment-slice materialization 'source[index..comment_end]' and its append against
		// the @kizu_rt_string_append_bytes ABI.
		"%cp_sub_len = sub i64 %cp_cend, %index",
		"%cp_fa_call = call %kizu.error.void @kizu_rt_string_append_bytes(" +
			"%kizu.owned %out, %kizu.slice.u8 %cp_sub_slice)",
		// Pin the latch phis: the else arm carries the scalars unchanged with index + 1, the comment
		// arm carries the constant (10, true, true) state with the new cursor.
		"%current_last_next = phi i8 [ %current_last, %cp_else ], [ 10, %cp_k_done ]",
		// Pin the CommentFormatState success wrap: the struct value (field 1) carrying the three
		// scalar state fields, wrapped with the ok flag (field 0) into the error union.
		"%cp_ret_full = insertvalue %kizu.error.comment_format_state %cp_ret_ok, " +
			"%kizu.selfhost.parser.format.comment_format_state %cp_ret_sv2, 1",
	}
}

// requiredLLVMFormatLineEndFragments locks the selfhost::parser::format line_end_excluding_break
// and line_end_including_break scalar helpers compiled into stage2 (issue 1165 / 1162): 'var index
// = start; let length = std::mem::len(source); while index < length and
// !is_line_break(source[index]) { index = index + 1; } return index;', with
// line_end_including_break adding a trailing 'if index < length { return after_line_break(source,
// index); } return index;'. They are the first compiled helpers with a genuine short-circuit 'and'
// while header: the second operand '!is_line_break(source[index])' loads source[index] only when
// the 'index < length' guard holds. The fragments pin that the header op0 branches into a guarded
// loopN_head_rhs block carrying the checked byte load + is_line_break call + prefix-not, whose true
// edge enters the loop body and whose false edge short-circuits to loopN_exit, so the byte load
// never runs past the slice. The single-element index argument is hoisted into a checked index-load
// temp (%t4) and passed by value, and line_end_including_break's tail folds a CRLF/LF into the line
// through a single-comparison if whose then-block is a 'return after_line_break(..)' ReturnCall.
func requiredLLVMFormatLineEndFragments() []string {
	return []string{
		// line_end_excluding_break: the i64-returning define over the source slice and the i64
		// start, the length local, and the parameter-seeded induction phi (the loop counter seeds
		// from %start, not a literal).
		"define i64 @kizu_selfhost__parser_format_line_end_excluding_break(",
		"  %kizu.slice.u8 %source",
		"  i64 %start",
		"%length = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %source)",
		"%index = phi i64 [ %start, %loop2_preheader ], [ %index_next, %loop2_latch ]",
		// Operand 0 in the loop head: the 'index < length' guard branching into the guarded
		// loop2_head_rhs block on success and short-circuiting to loop2_exit on failure. This pins
		// the short-circuit while header block layout: the byte load never runs unless the guard
		// holds.
		"%t2 = icmp slt i64 %index, %length",
		"br i1 %t2, label %loop2_head_rhs, label %loop2_exit",
		// Operand 1 in the guarded loop2_head_rhs block: the checked source[index] byte load (the
		// single-element index argument hoisted into temp %t4), the is_line_break call, and the
		// prefix-not xor, branching into the loop body on a non-break byte and to loop2_exit on a
		// break byte.
		"loop2_head_rhs:",
		"%t4_bad = or i1 %t4_neg, %t4_high",
		"%t4 = load i8, ptr %t4_gep",
		"%t5 = call i1 @kizu_selfhost__parser_format_is_line_break(i8 %t4)",
		"%t6 = xor i1 %t5, true",
		"br i1 %t6, label %loop2_body, label %loop2_exit",
		"%index_next = add i64 %index, 1",
		"loop2_exit:",
		// line_end_including_break: the same short-circuit while header plus the trailing 'if index
		// < length { return after_line_break(source, index); } return index;' that folds a CRLF/LF
		// into the line. The then-block is a 'return after_line_break(..)' ReturnCall returning a
		// plain i64 (no error-union wrap on this scalar helper); the fall-through returns index.
		"define i64 @kizu_selfhost__parser_format_line_end_including_break(",
		"%t9 = icmp slt i64 %index, %length",
		"br i1 %t9, label %if3_then, label %if3_cont",
		"if3_then:",
		"%t10 = call i64 @kizu_selfhost__parser_format_after_line_break(" +
			"%kizu.slice.u8 %source, i64 %index)",
		"  ret i64 %t10",
		"if3_cont:",
		"  ret i64 %index",
	}
}

// requiredLLVMFormatAfterLineBreakFragments locks the selfhost::parser::format after_line_break
// scalar helper compiled into stage2 (issue 1165 / 1162): 'let length = std::mem::len(source); if
// source[index] == cast<u8>(13) and index + 1 < length and source[index + 1] == cast<u8>(10) {
// return index + 2; } return index + 1;'. It is the first comment-preservation helper whose
// if-condition is a three-operand short-circuit 'and' chain, lowered through the generic
// multi-statement path's short-circuit if-chain (cond_insts + cond_rhs_insts + cond_extra_operands)
// rather than an eager 'and i1'. The fragments pin that each operand evaluates in its own guarded
// block so the trailing 'source[index + 1]' byte load only runs once both guards hold, never an
// out-of-bounds load past the slice.
func requiredLLVMFormatAfterLineBreakFragments() []string {
	return []string{
		// The i64-returning define over the source slice and the i64 index, so a regression in the
		// []u8 / i64 scalar ABI is caught.
		"define i64 @kizu_selfhost__parser_format_after_line_break(",
		"  %kizu.slice.u8 %source",
		"  i64 %index",
		// The length local read through the slice-len helper (std::mem::len(source)).
		"%length = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %source)",
		// Operand 0 in the unguarded entry block: the 'source[index]' byte load compared against
		// the CR immediate 13, branching into the first guarded block ifN_rhs on success.
		"%t3 = icmp eq i8 %t1, 13",
		"br i1 %t3, label %if1_rhs, label %if1_cont",
		// Operand 1 in the first guarded block ifN_rhs: the 'index + 1 < length' bounds guard,
		// branching into the second guarded block ifN_rhs2 rather than straight to ifN_then, which
		// pins that the chain carries a third operand.
		"if1_rhs:",
		"%t8 = icmp slt i64 %t6, %length",
		"br i1 %t8, label %if1_rhs2, label %if1_cont",
		// Operand 2 in the second guarded block ifN_rhs2: the guarded 'source[index + 1]' byte load
		// compared against the LF immediate 10. It only executes once both earlier operands hold, so
		// the trailing byte load never runs past the slice.
		"if1_rhs2:",
		"%t14 = icmp eq i8 %t12, 10",
		"br i1 %t14, label %if1_then, label %if1_cont",
		// The then block returns 'index + 2' and the fall-through cont block returns 'index + 1', as
		// plain i64 returns (no error-union wrap on this scalar helper).
		"if1_then:",
		"%t17 = add i64 %index, 2",
		"ret i64 %t17",
		"if1_cont:",
		"%t20 = add i64 %index, 1",
		"ret i64 %t20",
	}
}

// requiredLLVMFormatLineByteClassifierFragments locks the two leaf byte classifiers of the
// selfhost::parser::format comment-preservation cluster compiled into stage2 (issue 1165 / 1162):
// is_horizontal_space and is_line_break, each a 'return byte == cast<u8>(<a>) or byte ==
// cast<u8>(<b>);' single-return short-circuit `or` predicate over a u8 parameter, lowered through
// the generic short-circuit-or-return path (a u8 icmp, an early 'ret i1 true' on the left operand,
// and the right operand's icmp returned directly). Their presence here pins that the catalog-driven
// format closure keeps emitting them as real compiled functions over the i8 ABI.
func requiredLLVMFormatLineByteClassifierFragments() []string {
	return []string{
		// is_horizontal_space: 'byte == 32 (space) or byte == 9 (tab)'. Pin the bool-returning
		// define over the i8 byte parameter and the short-circuit-or lowering (left icmp, early
		// 'ret i1 true', right icmp returned directly), so a regression in the u8 predicate ABI or
		// the short-circuit-or shape is caught.
		"define i1 @kizu_selfhost__parser_format_is_horizontal_space(",
		"  i8 %byte",
		"%t2 = icmp eq i8 %byte, 32",
		"br i1 %t2, label %if0_then, label %if0_cont",
		"%t5 = icmp eq i8 %byte, 9",
		// is_line_break: 'byte == 10 (LF) or byte == 13 (CR)'. The '%t2 = icmp eq i8 %byte, 10'
		// fragment is unique to this classifier (is_horizontal_space tests 32), so it pins the
		// distinct byte immediates.
		"define i1 @kizu_selfhost__parser_format_is_line_break(",
		"%t2 = icmp eq i8 %byte, 10",
		"%t5 = icmp eq i8 %byte, 13",
	}
}

// requiredLLVMFormatAppendIndentFragments locks the selfhost::parser::format append_indent
// structured-control-flow emitter compiled into stage2 through compiled_struct_cf (issue 1165 /
// 1162): the indentation emitter that appends the four-space indentation string literal through
// the @kizu_rt_string_append_bytes ABI on each iteration of an i64 depth counter loop. It shares
// the (Let(Int), While, Return(Empty)) top-level shape with the import-cluster emitter, so it is
// dispatched ahead of it and disambiguated by its two-statement append/increment scan body.
func requiredLLVMFormatAppendIndentFragments() []string {
	return []string{
		// The void-returning define over the String accumulator (%kizu.owned) and the i64 depth
		// counter, so a regression in the &var String param ABI is caught.
		"define %kizu.error.void @kizu_selfhost__parser_format_append_indent(",
		"  i64 %depth",
		// Pin the four-space indentation literal emitted as a private module constant and
		// materialized into a %kizu.slice.u8 operand for the @kizu_rt_string_append_bytes ABI, so a
		// regression to a source-literal or fixed-slice form is caught.
		"@.kizu.compiled.kizu_selfhost__parser_format_append_indent.s0 = " +
			"private unnamed_addr constant [4 x i8] c\"    \"",
		"%ind_slice = insertvalue %kizu.slice.u8 %ind_base, i64 4, 1",
		"%ind_call = call %kizu.error.void @kizu_rt_string_append_bytes(" +
			"%kizu.owned %out, %kizu.slice.u8 %ind_slice)",
		// Pin the i64 counter loop-head phi seeded from the literal 0 on the entry edge and the
		// scan condition comparing the counter against the i64 depth parameter directly (no length
		// call), so a regression to a literal bound or a length-call header is caught.
		"%index = phi i64 [ 0, %entry ], [ %index_next, %scan_latch ]",
		"%scan_cond = icmp slt i64 %index, %depth",
		// Pin the constant-step latch advance and the loop-exit void success wrap.
		"%index_next = add i64 %index, 1",
		"%ret_ok = insertvalue %kizu.error.void zeroinitializer, i1 true, 0",
	}
}

// requiredLLVMFormatSortedImportsFragments locks the selfhost::parser::format
// append_sorted_imports structured-control-flow emitter compiled into stage2 through
// compiled_struct_cf (issue 1165 / 1162): the import-cluster emitter that scans the sorted
// indices array and, for each, appends a single import declaration via append_import_decl
// followed by a newline byte through the @kizu_rt_string_append_byte ABI.
func requiredLLVMFormatSortedImportsFragments() []string {
	return []string{
		// The void-returning define over the String accumulator (%kizu.owned), the source slice,
		// the tokens handle, and the indices handle (%kizu.owned), so a regression in the &var
		// String / &Array param ABI is caught.
		"define %kizu.error.void @kizu_selfhost__parser_format_append_sorted_imports(",
		"  %kizu.owned %indices",
		// Pin the scan loop head reading the indices length through the runtime builtin and the
		// i64 counter loop-head phi.
		"%scan_len = call i64 @kizu_rt_array_len(%kizu.owned %indices)",
		"%index = phi i64 [ 0, %entry ], [ %index_next, %scan_latch ]",
		// Pin the checked Array<i64>.get read of the next sorted import index against the element
		// accessor, with try-propagation of a failure as this function's own error union.
		"%import_index_view = call %kizu.error.slice.u8 @kizu_rt_array_at(" +
			"%kizu.owned %indices, i64 %index)",
		"%import_index = load i64, ptr %import_index_ptr",
		// Pin the try-call to the single-import emitter forwarding the accumulator / source /
		// tokens / import index, with its %kizu.error.void propagated as this function's own union.
		"%decl_call = call %kizu.error.void @kizu_selfhost__parser_format_append_import_decl(" +
			"%kizu.owned %out, %kizu.slice.u8 %source, %kizu.owned %tokens, i64 %import_index)",
		"br i1 %decl_ok, label %decl_cont, label %decl_fail",
		// Pin the newline append against the @kizu_rt_string_append_byte ABI with the i8 immediate
		// derived from the cast<u8>(10) literal (no hidden constant).
		"%nl_call = call %kizu.error.void @kizu_rt_string_append_byte(%kizu.owned %out, i8 10)",
		// Pin the latch counter advance and the loop-exit void success wrap.
		"%index_next = add i64 %index, 1",
		"%ret_ok = insertvalue %kizu.error.void zeroinitializer, i1 true, 0",
	}
}

// requiredLLVMFormatImportDeclFragments locks the selfhost::parser::format append_import_decl
// structured-control-flow emitter compiled into stage2 through compiled_struct_cf (issue 1165 /
// 1162): the first compiled helper that mutates a '&var std::string::String' accumulator. It
// appends the 'import ' prefix and ';' terminator as string literals through the
// @kizu_rt_string_append_bytes ABI, reads each token's text via token_text, scans the import's
// tokens through a loop carrying an early-return semicolon branch and a fall-through 'ident or
// double-colon' text append, and returns the %kizu.error.void success.
func requiredLLVMFormatImportDeclFragments() []string {
	return []string{
		// The void-returning define over the String accumulator (%kizu.owned), the source slice,
		// the tokens handle, and the i64 import index, so a regression in the &var String param ABI
		// is caught.
		"define %kizu.error.void @kizu_selfhost__parser_format_append_import_decl(",
		"  %kizu.owned %out",
		"  i64 %import_index",
		// Pin the 'import ' / ';' string-literal globals emitted as private module constants and
		// materialized into %kizu.slice.u8 operands for the @kizu_rt_string_append_bytes ABI, so a
		// regression to a source-literal or fixed-slice form is caught.
		"@.kizu.compiled.kizu_selfhost__parser_format_append_import_decl.s0 = " +
			"private unnamed_addr constant [7 x i8] c\"import \"",
		"@.kizu.compiled.kizu_selfhost__parser_format_append_import_decl.s1 = " +
			"private unnamed_addr constant [1 x i8] c\";\"",
		"%imp_slice = insertvalue %kizu.slice.u8 %imp_base, i64 7, 1",
		"%imp_call = call %kizu.error.void @kizu_rt_string_append_bytes(" +
			"%kizu.owned %out, %kizu.slice.u8 %imp_slice)",
		// Pin the 'var index = import_index + 1' preheader seed feeding the scan loop-head phi.
		"%index_init = add i64 %import_index, 1",
		"%index = phi i64 [ %index_init, %imp_cont ], [ %index_next, %scan_latch ]",
		// Pin the checked Array<Token>.get element read and the token_text slice read off the
		// source, both AST-derived (no token name special-cased).
		"%token_view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %tokens, i64 %index)",
		"%text = call %kizu.slice.u8 @kizu_selfhost__parser_format_token_text(" +
			"%kizu.slice.u8 %source, %kizu.kizu.lexer.token %token)",
		// Pin the semicolon predicate branch: a true predicate appends ';' and returns the void
		// success early; a false predicate falls through to the text guard.
		"%semi = call i1 @kizu_selfhost__parser_format_is_semicolon_token(" +
			"%kizu.kizu.lexer.token %token)",
		"br i1 %semi, label %semi_then, label %semi_skip",
		"%semi_call = call %kizu.error.void @kizu_rt_string_append_bytes(" +
			"%kizu.owned %out, %kizu.slice.u8 %semi_slice)",
		"%ret_semi = insertvalue %kizu.error.void zeroinitializer, i1 true, 0",
		// Pin the short-circuit 'ident or double-colon' text guard: either predicate routes to the
		// text append, otherwise the scan falls through to the latch.
		"%ident = call i1 @kizu_selfhost__parser_format_is_ident_token(%kizu.kizu.lexer.token %token)",
		"br i1 %ident, label %emit_text, label %check_colon",
		"%colon = call i1 @kizu_selfhost__parser_format_is_double_colon_token(" +
			"%kizu.kizu.lexer.token %token)",
		"br i1 %colon, label %emit_text, label %scan_latch",
		// Pin the token-text append against the runtime ABI and the fall-through to the latch.
		"%txt_call = call %kizu.error.void @kizu_rt_string_append_bytes(" +
			"%kizu.owned %out, %kizu.slice.u8 %text)",
		// Pin the latch counter advance and the loop-exit void success wrap.
		"%index_next = add i64 %index, 1",
		"%ret_ok = insertvalue %kizu.error.void zeroinitializer, i1 true, 0",
	}
}

// requiredLLVMFormatImportSortFragments locks the selfhost::parser::format import-sort
// comparator cluster compiled into stage2 (issue 1165 / 1162): compare_bytes (the i64 byte
// comparison whose loop header is a short-circuit `and` of two comparisons over pure length
// locals) and import_path_less (the first multi-counter helper: two lockstep token cursors
// with base-plus-offset inits advanced under a short-circuit `and` header, with a nested-call
// compare_bytes let and a prefix-not call return).
func requiredLLVMFormatImportSortFragments() []string {
	return []string{
		// compare_bytes is the first import-sort helper on the compiled path: a non-error
		// i64 byte comparison whose loop header is a short-circuit `and` of two comparisons
		// over pure length locals, with []u8 index loads in the body guards (issue 1165 /
		// 1162).
		"define i64 @kizu_selfhost__parser_format_compare_bytes(",
		// Pin that std::mem::len(left) / std::mem::len(right) lower to the slice_len builtin
		// bound to the named length locals the loop header reads.
		"%left_len = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %left)",
		"%right_len = call i64 @kizu_selfhost__slice_len(%kizu.slice.u8 %right)",
		// Pin the short-circuit `and` while header: the two `index < *_len` comparisons lower
		// to an eager `and i1` over the comparison results (their operands are side-effect
		// free), so a regression that rejects an `and` header or mis-lowers it is caught.
		"%t6 = and i1 %t2, %t5",
		// import_path_less is the first multi-counter import-sort helper on the compiled path:
		// two lockstep token cursors with base-plus-offset inits advanced under a short-circuit
		// `and` header, with a nested-call compare_bytes let and a prefix-not call return
		// (issue 1165 / 1162).
		"define %kizu.error.bool @kizu_selfhost__parser_format_import_path_less(",
		// Pin the base-plus-offset preheader seeds ('var left_index = left + 1; var right_index
		// = right + 1;'): each lockstep counter materializes %<var>_init in the preheader so the
		// loop-head phi seeds from it instead of a literal or a plain copy.
		"%right_index_init = add i64 %right, 1",
		"%left_index_init = add i64 %left, 1",
		// Pin the two lockstep loop-head phis seeded from the preheader inits and advanced from
		// the shared latch, so a regression to a single induction counter is caught.
		"%right_index = phi i64 [ %right_index_init, %loop2_preheader ], " +
			"[ %right_index_next, %loop2_latch ]",
		"%left_index = phi i64 [ %left_index_init, %loop2_preheader ], " +
			"[ %left_index_next, %loop2_latch ]",
		// Pin the two-counter constant-step latch: both cursors advance by one in the same
		// latch block.
		"%right_index_next = add i64 %right_index, 1",
		"%left_index_next = add i64 %left_index, 1",
		// Pin the prefix-not call return ('return !is_semicolon_token(right_token);'): the bool
		// call result is negated with 'xor i1 %call, true' before the error-union wrap.
		"%t11 = xor i1 %t10, true",
	}
}

// requiredLLVMFormatSortFragments locks the selfhost::parser::format sort_import_indices
// structured-control-flow helper compiled into stage2 through compiled_struct_cf (issue
// 1165 / 1162): the scan-shift insertion sort with an outer counter loop, a nested scan loop
// carrying a cursor and a boolean flag through loop-head phis, an if/else that re-merges into
// the loop, a try-call import_path_less condition, and Array<i64>.get/set element reads/writes
// through the element-size-generic @kizu_rt_array_at / @kizu_rt_array_set / @kizu_rt_array_len
// ABI on its '&var std::array::Array<i64>' parameter.
func requiredLLVMFormatSortFragments() []string {
	return []string{
		// The void-returning define over the source slice, the tokens handle, and the mutable
		// indices handle (all %kizu.owned), so a regression in the &var Array<i64> param ABI is
		// caught.
		"define %kizu.error.void @kizu_selfhost__parser_format_sort_import_indices(",
		"  %kizu.owned %indices",
		// Pin the outer counter loop head reading the array length through the generic runtime
		// builtin and the nested scan loop's two loop-carried phis (a cursor i64 and a scanning
		// i1), so a regression to flat single-induction lowering is caught.
		"%outer_len = call i64 @kizu_rt_array_len(%kizu.owned %indices)",
		"%cursor = phi i64 [ %index, %outer_body ], [ %cursor_next, %inner_latch ]",
		"%scanning = phi i1 [ true, %outer_body ], [ %scanning_next, %inner_latch ]",
		// Pin the short-circuit 'scanning and cursor > 0' inner header.
		"%inner_cond = and i1 %scanning, %cursor_gt0",
		// Pin the Array<i64>.get reads against the checked element accessor at cursor - 1 and
		// cursor, with try-propagation of a failure as this function's own error union.
		"%left_view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %indices, i64 %cursor_m1)",
		"%right_view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %indices, i64 %cursor)",
		"%left = load i64, ptr %left_ptr",
		// Pin the try-call import_path_less condition (the comparator error union is called with
		// the swapped right/left indices) and its error propagation + boolean branch.
		"%cmp_eu = call %kizu.error.bool @kizu_selfhost__parser_format_import_path_less(" +
			"%kizu.slice.u8 %source, %kizu.owned %tokens, i64 %right, i64 %left)",
		"br i1 %cmp_ok, label %cmp_cont, label %cmp_fail",
		"br i1 %cmp_val, label %if_then, label %if_else",
		// Pin the Array<i64>.set writes against @kizu_rt_array_set with the element marshalled
		// through an entry-block slot and the element size derived from the i64 element type
		// (no hidden i64 special case), so a regression in the write ABI is caught.
		"%set0_eslice = insertvalue %kizu.slice.u8 %set0_es0, " +
			"i64 ptrtoint (ptr getelementptr (i64, ptr null, i32 1) to i64), 1",
		"%set0_call = call %kizu.error.void @kizu_rt_array_set(%kizu.owned %indices, " +
			"i64 %cursor_m1, %kizu.slice.u8 %set0_eslice)",
		"%set1_call = call %kizu.error.void @kizu_rt_array_set(%kizu.owned %indices, " +
			"i64 %cursor, %kizu.slice.u8 %set1_eslice)",
		// Pin the if/else fall-through merge phis at the inner latch: the cursor decrements on
		// the swap path and is unchanged on the else path; the scanning flag is unchanged on the
		// swap path and stops (false) on the else path.
		"%cursor_next = phi i64 [ %cursor_dec, %set1_cont ], [ %cursor, %if_else ]",
		"%scanning_next = phi i1 [ %scanning, %set1_cont ], [ false, %if_else ]",
	}
}

// requiredLLVMFormatLeadingImportFragments locks the selfhost::parser::format
// leading_import_indices structured-control-flow collector compiled into stage2 through
// compiled_struct_cf (issue 1165 / 1162): it builds a runtime-owned Array<i64> through the
// element-size-generic @kizu_rt_array_new / @kizu_rt_array_append ABI, scans the token array
// through an if/else branch-merge loop (a loop-terminating 'index = tokens.len()' on one arm,
// an append-then-try-advance on the other re-merging at a loop-latch phi), sorts the collected
// indices in place via the sort helper, and returns the filled array as the
// '!std::array::Array<i64>' success (a %kizu.error.owned wrap).
func requiredLLVMFormatLeadingImportFragments() []string {
	return []string{
		// The %kizu.error.owned-returning define over the allocator, source slice, and tokens
		// handle, so a regression in the '!std::array::Array<i64>' return / Allocator param ABI
		// is caught.
		"define %kizu.error.owned @kizu_selfhost__parser_format_leading_import_indices(",
		// Pin the runtime-owned Array<i64> constructor: the element size flows from the i64
		// element type through getelementptr (no hidden constant), and the marshalling slot is
		// pre-allocated in the entry block.
		"%app_slot = alloca i64",
		"%indices = call %kizu.owned @kizu_rt_array_new(%kizu.owned %allocator, " +
			"i64 ptrtoint (ptr getelementptr (i64, ptr null, i32 1) to i64))",
		// Pin the outer scan loop head carrying the i64 counter through a loop-head phi and
		// testing it against the token length through the generic runtime builtin.
		"%index = phi i64 [ 0, %entry ], [ %index_next, %scan_latch ]",
		"%scan_len = call i64 @kizu_rt_array_len(%kizu.owned %tokens)",
		// Pin the checked Array<Token>.get element read against @kizu_rt_array_at with
		// try-propagation of a failure as this function's own %kizu.error.owned error union, and
		// the boolean predicate call selecting the branch (a true predicate takes the import
		// append arm; a false predicate terminates the scan).
		"%token_view = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %tokens, i64 %index)",
		"%token = load %kizu.kizu.lexer.token, ptr %token_ptr",
		"%pred = call i1 @kizu_selfhost__parser_format_is_import_token(" +
			"%kizu.kizu.lexer.token %token)",
		"br i1 %pred, label %lii_else, label %lii_then",
		// Pin the loop-terminating arm: 'index = tokens.len()' steps the counter to the length.
		"%then_len = call i64 @kizu_rt_array_len(%kizu.owned %tokens)",
		// Pin the Array<i64>.append against @kizu_rt_array_append with the element marshalled
		// through the entry-block slot and the element size derived from the i64 element type
		// (no hidden i64 special case), so a regression in the append ABI is caught.
		"%app_eslice = insertvalue %kizu.slice.u8 %app_es0, " +
			"i64 ptrtoint (ptr getelementptr (i64, ptr null, i32 1) to i64), 1",
		"%app_call = call %kizu.error.void @kizu_rt_array_append(%kizu.owned %indices, " +
			"%kizu.slice.u8 %app_eslice)",
		// Pin the try-call advance (index = try index_after_import(tokens, index)): the
		// %kizu.error.i64 union is unwrapped on the value field (1) on success and the message
		// field (2) on failure, with the failure rewrapped into this function's own error union.
		"%iai_call = call %kizu.error.i64 @kizu_selfhost__parser_format_index_after_import(" +
			"%kizu.owned %tokens, i64 %index)",
		"%iai_msg = extractvalue %kizu.error.i64 %iai_call, 2",
		"%iai_val = extractvalue %kizu.error.i64 %iai_call, 1",
		// Pin the if/else fall-through merge phi at the loop latch: the next counter is the token
		// length on the terminating arm and the advance result on the import arm.
		"%index_next = phi i64 [ %then_len, %lii_then ], [ %iai_val, %iai_cont ]",
		// Pin the in-place sort try-call (try sort_import_indices(source, tokens, &var indices))
		// with the mutable indices handle passed as %kizu.owned and its error propagation.
		"%sort_call = call %kizu.error.void @kizu_selfhost__parser_format_sort_import_indices(" +
			"%kizu.slice.u8 %source, %kizu.owned %tokens, %kizu.owned %indices)",
		// Pin the 'return indices;' success wrap: the filled array lands in the %kizu.owned value
		// field of the %kizu.error.owned success union, not a raw owned handle.
		"%ret_ok1 = insertvalue %kizu.error.owned %ret_ok0, %kizu.owned %indices, 1",
		"%ret_ok2 = insertvalue %kizu.error.owned %ret_ok1, %kizu.slice.u8 zeroinitializer, 2",
	}
}

// requiredLLVMParserPredicateFragments locks the std::kizu::parser TokenKind / byte / span
// predicate closure compiled into stage2, and chains the selfhost::parser::format read-only
// helper closure that lowers through the same token-kind predicate / generic expression paths.
func requiredLLVMParserPredicateFragments() []string {
	parser := []string{
		"%kizu.kizu.parser.parse_node = type { %kizu.kizu.ast.node_id, " +
			"%kizu.kizu.lexer.token }",
		"%kizu.kizu.parser.parse_range = type { %kizu.kizu.ast.child_range, " +
			"%kizu.kizu.lexer.token }",
		"define i1 @kizu_kizu__parser_is_double_colon(",
		"define i1 @kizu_kizu__parser_is_eof_token(",
		"define i1 @kizu_kizu__parser_is_left_brace_token(",
		"define i1 @kizu_kizu__parser_is_right_brace_token(",
		"define i1 @kizu_kizu__parser_is_block_close_token(",
		"define i1 @kizu_kizu__parser_is_ident_kind(",
		"define i1 @kizu_kizu__parser_is_postfix_start(",
		"define i1 @kizu_kizu__parser_is_call_close_token(",
		"define i1 @kizu_kizu__parser_is_pub_token(",
		"define i1 @kizu_kizu__parser_is_comptime_token(",
		"define i1 @kizu_kizu__parser_is_decl_item_separator(",
		"define i1 @kizu_kizu__parser_is_lt_token(",
		"define i1 @kizu_kizu__parser_is_gt_token(",
		"define i1 @kizu_kizu__parser_is_comma_token(",
		"define i1 @kizu_kizu__parser_is_arrow_token(",
		"define i1 @kizu_kizu__parser_is_left_paren_token(",
		"define i1 @kizu_kizu__parser_is_right_paren_token(",
		"%tkp_kind = extractvalue %kizu.kizu.lexer.token %token, 0",
		"%tkp_is = icmp eq i64 %tkp_kind, ",
		"%tkp_is_1 = or i1 %tkp_is_0, %tkp_cmp_1",
		"ret i1 %tkp_is",
		"define i1 @kizu_kizu__parser_is_single_token_byte(",
		"define i1 @kizu_kizu__parser_is_double_token_byte(",
		"define i1 @kizu_kizu__parser_is_double_colon_at(",
		"define i1 @kizu_kizu__parser_is_name_byte(",
		"define i1 @kizu_kizu__parser_is_upper_byte(",
		"define i1 @kizu_kizu__parser_is_namespace_path_span(",
		"define i1 @kizu_kizu__parser_is_struct_literal_type_span(",
		"define i1 @kizu_kizu__parser_is_type_apply_start(",
		"define i1 @kizu_kizu__parser_is_struct_literal_start(",
		"%nps_saw = phi i1",
		"%slt_count = phi i64",
		"%pps_next = extractvalue %kizu.kizu.parser.parse_node %left,",
		"%pps_guard = call i1 @kizu_kizu__parser_is_lt_token",
		"%pps_ast_node = call %kizu.kizu.ast.ast_node @kizu_kizu__ast_ast_get(",
		"%pps_result = call i1 @kizu_kizu__parser_is_namespace_path_span(",
		"%pps_result = call i1 @kizu_kizu__parser_is_struct_literal_type_span(",
		"_idx_oob:",
	}
	return append(parser, requiredLLVMFormatHelperFragments()...)
}

// forbiddenLLVMFragments returns source-shape gates removed from the hosted CLI.
func forbiddenLLVMFragments() []string {
	fragments := forbiddenLegacyCLISourceShapeFragments()
	fragments = append(fragments, forbiddenLegacyRunAstEmissionFragments()...)
	fragments = append(fragments, forbiddenSeveredShapeEmissionFragments()...)
	fragments = append(fragments, forbiddenObsoleteTypeDeclarationFragments()...)
	return append(fragments, forbiddenLegacyRunArtifactFragments()...)
}

// forbiddenObsoleteTypeDeclarationFragments keeps severed backend-only ABI records
// out of the hosted module. None is a current canonical declaration; CodeEval and
// RelatedSpan instead use their fact-derived names.
func forbiddenObsoleteTypeDeclarationFragments() []string {
	return []string{
		"%kizu.selfhost.codegen.run_ast = type",
		"%kizu.error.run_ast = type",
		"%kizu.selfhost.codegen.value = type",
		"%kizu.selfhost.codegen.instruction = type",
		"%kizu.selfhost.codegen.block = type",
		"%kizu.selfhost.codegen.function = type",
		"%kizu.selfhost.codegen.program = type",
		"%kizu.selfhost.codegen.local_binding = type",
		"%kizu.selfhost.codegen.local_table = type",
		"%kizu.selfhost.cli.parse_node_result = type",
		"%kizu.selfhost.codegen.code_eval = type",
		"%kizu.error.code_eval = type",
		"%kizu.error.related_span = type",
		"%kizu.selfhost.codegen.payload_span = type",
		"%kizu.error.payload_span = type",
	}
}

// forbiddenSeveredShapeEmissionFragments returns the hosted per-shape Program builder
// / consumer compiled defines severed by #1255 slice4 PR-1. Once the codegen compiled
// closure dropped the lowered_main_print_program / metadata_for_program /
// main_print_payload / unsupported_program roots and the *_program_supported
// auto-index registrations, these defines are no longer emitted into stage2.
func forbiddenSeveredShapeEmissionFragments() []string {
	return []string{
		"define %kizu.selfhost.codegen.value @kizu_selfhost__ir_codegen_const_string_value",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_const_string_instruction",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_call_instruction",
		"define %kizu.selfhost.codegen.instruction @kizu_selfhost__ir_codegen_return_void_instruction",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_build_main_print_program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_main_print_program",
		"define %kizu.selfhost.codegen.value @kizu_selfhost__ir_codegen_main_print_payload",
		"define %kizu.slice.u8 @kizu_selfhost__ir_codegen_metadata_for_program",
		"define i1 @kizu_selfhost__ir_codegen_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_i64_call_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_return_void_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_try_void_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_loop_i64_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_fs_read_program_supported",
		"define i1 @kizu_selfhost__ir_codegen_lowered_program_supported",
	}
}

// forbiddenLegacyCLISourceShapeFragments returns removed fixture-shape CLI gates.
func forbiddenLegacyCLISourceShapeFragments() []string {
	return []string{
		"@.kizu.cli.main_fn_pattern",
		"@.kizu.cli.run_hello_pattern",
		"define i1 @kizu_selfhost__cli_run_prints_hello",
		"@.kizu.cli.run_hello_ll_suffix",
		"@.kizu.cli.test_expect_true_pattern",
		"@.kizu.cli.test_expect_false_pattern",
		"define %kizu.slice.u8 @kizu_selfhost__moved_value_name",
		"define %kizu.slice.u8 @kizu_selfhost__cli_moved_value_name",
		"@.kizu.cli.move_prefix",
		"@.kizu.cli.undefined_prefix",
		"first_hosted_check_undefined_variable_start_ast_node",
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
	}
}

// forbiddenLegacyRunAstEmissionFragments returns removed RunAst lowering emission roots.
func forbiddenLegacyRunAstEmissionFragments() []string {
	return []string{
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_codegen_lower_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_i64_call_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_return_void_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_try_void_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_loop_i64_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_fs_read_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_print_run_ast",
		"define %kizu.selfhost.codegen.run_ast @kizu_selfhost__ir_codegen_unsupported_run_ast",
		"define i1 @kizu_selfhost__ir_codegen_run_ast_supported",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_i64_call_program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_return_void_program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_try_void_program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_loop_i64_program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__ir_codegen_lowered_fs_read_program",
		"define i1 @kizu_selfhost__ir_codegen_local_table_contains",
		"define %kizu.selfhost.codegen.local_binding @kizu_selfhost__ir_codegen_empty_local",
		"define %kizu.selfhost.codegen.payload_span @kizu_selfhost__ir_codegen_local_payload_span",
		"define %kizu.selfhost.codegen.local_table @kizu_selfhost__ir_codegen_empty_local_table",
		"define %kizu.selfhost.codegen.local_table @kizu_selfhost__ir_codegen_insert_local",
		"define %kizu.selfhost.codegen.payload_span @kizu_selfhost__ir_codegen_print_payload",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_print_call",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_print_statement",
		"define %kizu.selfhost.codegen.local_binding @kizu_selfhost__ir_codegen_lower_let_binding",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_run_ast_block",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_run_ast_function",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_run_ast_declarations",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_run_parse_result",
		"define %kizu.error.run_ast @kizu_selfhost__ir_codegen_lower_run_ast",
		"define %kizu.error.slice.u8 @kizu_selfhost__ir_codegen_stdout_payload",
	}
}

// forbiddenLegacyRunArtifactFragments returns removed static run artifact branches.
func forbiddenLegacyRunArtifactFragments() []string {
	return []string{
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_run_codegen_program",
		"define %kizu.error.slice.u8 @kizu_selfhost__cli_run_payload_llvm_c_string",
		"%run_codegen = call %kizu.selfhost.codegen.program",
		"define %kizu.selfhost.codegen.program @kizu_selfhost__cli_codegen_lower_run_checked_program",
		"@kizu_selfhost__cli_codegen_parse_checked_run_ast",
		"@kizu_selfhost__cli_codegen_parse_let_binding",
		"@kizu_selfhost__cli_codegen_parse_print_statement",
		"define %kizu.selfhost.codegen.payload @kizu_selfhost__cli_codegen_parse_print_payload",
		"@kizu_selfhost__cli_frontend_lower_checked_run_ast",
		"@kizu_selfhost__cli_frontend_parse_let_binding",
		"@kizu_selfhost__cli_frontend_parse_print_statement",
		"@kizu_selfhost__cli_parse_test_expect_value",
		"@kizu_selfhost__cli_test_executable",
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

// requiredLLVMMetadataSelectedSignatureFragments is empty because validation now
// consumes manifest-owned root signatures directly from IR. Re-emitting a selected
// subset in metadata would recreate the removed manual root list.
func requiredLLVMMetadataSelectedSignatureFragments() []string {
	return nil
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
	forbiddenMarkers := []string{
		"std.builtin", "stdprim", "internal/interp", "internal global",
		"@kizu_rt_map_get_i64", "@.kizu.rt.map_full",
	}
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
		"%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }\n",
		"%kizu.record.abi.summary = type { i64, %kizu.slice.u8 }\n",
		"%kizu.error.record.abi.summary = type { i1, %kizu.record.abi.summary, %kizu.slice.u8 }\n",
		"%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }\n",
		"%kizu.rt.string = type { ptr, ptr, i64, i64 }\n",
		"%kizu.rt.map = type { ptr, ptr, i64, i64, i64 }\n",
		"%kizu.rt.map.entry = type { ptr, i64, ptr }\n",
		"%kizu.rt.box = type { ptr, ptr, i64 }\n",
		"%kizu.rt.arena = type { ptr, i64, i64, [64 x i8] }\n",
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
		"@.kizu.rt.map_key_delta",
		"@.kizu.rt.map_value_update",
		"@.kizu.rt.map_key_not_found",
		"@.kizu.rt.invalid_map_value",
		"@.kizu.rt.invalid_slice",
		"@.kizu.rt.invalid_box",
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
		"define %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned %array)",
		// array_set is the checked in-place element overwrite backing the formatter
		// import-sort Array<i64>.set (issue 1165 slice 2).
		"define %kizu.error.void @kizu_rt_array_set(%kizu.owned %array, i64 %index,",
		"define %kizu.owned @kizu_rt_string_new",
		"define %kizu.error.void @kizu_rt_string_append_bytes",
		"define %kizu.error.void @kizu_rt_string_append_byte",
		"define %kizu.slice.u8 @kizu_rt_string_as_bytes",
		"define %kizu.owned @kizu_rt_map_new(%kizu.owned %allocator, i64 %value_size)",
		"define %kizu.error.void @kizu_rt_map_insert(",
		"define i1 @kizu_rt_map_reserve",
		"define i1 @kizu_rt_map_key_equal",
		"define i1 @kizu_rt_map_contains",
		"define i64 @kizu_rt_map_len",
		"define %kizu.error.slice.u8 @kizu_rt_map_get",
		"define void @kizu_rt_map_deinit",
		"define %kizu.error.owned @kizu_rt_box_new",
		"define %kizu.error.slice.u8 @kizu_rt_box_borrow",
		"define void @kizu_rt_box_deinit",
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
		"call %kizu.error.void @kizu_rt_array_set",
		"call %kizu.error.void @kizu_rt_string_append_bytes",
		"call %kizu.error.void @kizu_rt_string_append_byte",
		"call %kizu.slice.u8 @kizu_rt_string_as_bytes",
		"call i64 @kizu_selfhost__runtime_string_invalid_smoke",
		"call %kizu.error.void @kizu_rt_map_insert",
		"call i1 @kizu_rt_map_contains",
		"call %kizu.error.slice.u8 @kizu_rt_map_get",
		"call i64 @kizu_rt_map_len",
		"call %kizu.error.owned @kizu_rt_box_new",
		"call %kizu.error.slice.u8 @kizu_rt_box_borrow",
		"call void @kizu_rt_box_deinit",
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
		"array-pop-or-panic removes-last-element-view\n",
		"array-set in-place-element-overwrite\n",
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
		"map-storage string-key-generic-byte-buffer\n",
		"map-value-layout constructor-sized\n",
		"map-growth geometric-entry-buffer\n",
		"map-get returns-stored-value-view\n",
		"map-deinit releases-key-value-entry-buffers\n",
		"map-key-ownership copies-key-bytes\n",
		"map-missing-key-diagnostic Map.get key not found\n",
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
		"reachable box i64-owned-indirection\n",
		"box-storage single-payload-byte-buffer\n",
		"box-borrow returns-stored-payload\n",
		"box-deinit releases-payload-buffer\n",
		"box-invalid-diagnostic invalid box\n",
		"abi-record summary-i64-slice\n",
		"abi-error-union summary-success-failure\n",
		"abi-call direct-record-roundtrip\n",
		"deferred tagged-union-payload issue-495\n",
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

// hostedLinkStackArgs reserves a larger main-thread stack for linked selfhost
// compiler binaries on darwin: the checker recurses deeply over large packages
// and overflows the default 8MiB stack while checking the selfhost package.
func hostedLinkStackArgs() []string {
	if goruntime.GOOS == "darwin" {
		return []string{"-Wl,-stack_size,0x20000000"}
	}
	return nil
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
	compileArgs := append(
		[]string{
			"-Wno-override-module",
			"-fno-integrated-as",
		},
		hostedLinkStackArgs()...,
	)
	compileArgs = append(
		compileArgs,
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.host.ll",
		"selfhost/runtime/selfhost.hosted.c",
		harnessPath,
		"-o",
		exePath,
	)
	compile := exec.Command(clang, compileArgs...)
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
	failures += countHostedCompilerCLISharedUndefinedFailures(t, exePath, dir)

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

// countHostedCompilerCLISharedUndefinedFailures proves check/run/test enter the
// same parsed semantic facade before command-specific lowering.
func countHostedCompilerCLISharedUndefinedFailures(t *testing.T, exePath string, dir string) int {
	t.Helper()
	undefinedPath := filepath.Join(dir, "hosted_shared_undefined.kizu")
	undefinedSource := "fn main() {\n    print(name);\n}\n"
	if err := os.WriteFile(undefinedPath, []byte(undefinedSource), 0o644); err != nil {
		t.Errorf("write hosted shared undefined source: %v", err)
		return 1
	}
	failures := 0
	for _, command := range []string{"check", "run", "test"} {
		failures += countHostedCompilerCLIExpectedFailure(
			t,
			exePath,
			command,
			undefinedPath,
			"error: type error: undefined variable `name` at 2:11\n",
			command+" shared undefined variable",
		)
	}
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
	failures := countHostedCompilerCLICommentParseFailures(t, exePath, dir)
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
func countHostedCompilerCLICommentParseFailures(t *testing.T, exePath string, dir string) int {
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
	expected := "fn main() { print(\"from temp\"); }\n"
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
	expected := "import std::fmt;\n\nstruct Point {\n    x: i64, y: i64\n}\n"
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
	expected := "import std::fmt;\n\nstruct Point {\n    x: i64, y: i64\n}"
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
// The probe must stay outside the run tape's supported surface. It uses a test
// declaration, which the hosted parser/check path accepts but `run` codegen
// rejects generically because it has no runnable main function.
func countHostedCompilerCLIRunFailures(t *testing.T, exePath string) int {
	t.Helper()
	failures := countHostedCompilerCLIUnsupportedRunSourceFailures(
		t,
		exePath,
		"hosted_run_test_decl_unsupported.kizu",
		"hosted_run_test_decl_unsupported",
		"test \"expect ok\" {\n    std::testing::expect(true);\n}\n",
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
	if !strings.Contains(stderr, "unsupported run codegen program") {
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
