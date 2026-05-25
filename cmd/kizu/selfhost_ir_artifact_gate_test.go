package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostIRArtifactGate executes the selfhost IR artifact emission smoke.
func TestSelfhostIRArtifactGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countWithIsolatedSelfhostTarget(
		t,
		func() int { return countSelfhostIRArtifactGateFailures(t) },
	); failures > 0 {
		t.Fatalf("selfhost IR artifact gate failures=%d", failures)
	}
}

// countSelfhostIRArtifactGateFailures returns failures for artifact summary logging.
func countSelfhostIRArtifactGateFailures(t *testing.T) int {
	t.Helper()
	out, err := runSelfhostIRArtifactGate(t)
	if err != nil {
		t.Errorf("IR artifact gate failed: %v\n%s", err, out)
		return 1
	}
	required := []string{
		"ir-artifact-path\ntarget/selfhost/selfhost.ir\n",
		"ir-manifest-path\ntarget/selfhost/selfhost.ir.manifest\n",
		"backend-artifact-bytes\n",
	}
	for _, fragment := range required {
		if !strings.Contains(out, fragment) {
			t.Errorf("IR artifact gate output missing %q\ngot:\n%s", fragment, out)
			return 1
		}
	}
	return countSelfhostIRArtifactFileFailures(t)
}

// countSelfhostIRArtifactFileFailures checks emitted files contain deterministic headers.
func countSelfhostIRArtifactFileFailures(t *testing.T) int {
	t.Helper()
	irBytes, err := os.ReadFile("../../target/selfhost/selfhost.ir")
	if err != nil {
		t.Errorf("read IR artifact: %v", err)
		return 1
	}
	manifestBytes, err := os.ReadFile("../../target/selfhost/selfhost.ir.manifest")
	if err != nil {
		t.Errorf("read IR manifest: %v", err)
		return 1
	}
	irContent := string(irBytes)
	if !strings.Contains(irContent, "kizu-ir-v0\npackage selfhost\n") {
		t.Errorf("IR artifact missing deterministic header:\n%s", irBytes)
		return 1
	}
	for _, fragment := range requiredSelfhostIRContractFragments() {
		if !strings.Contains(irContent, fragment) {
			t.Errorf("IR artifact missing contract fragment %q:\n%s", fragment, irBytes)
			return 1
		}
	}
	if !strings.Contains(string(manifestBytes), "kizu-ir-shape-v0\n") {
		t.Errorf("IR manifest missing deterministic header:\n%s", manifestBytes)
		return 1
	}
	if !strings.Contains(string(manifestBytes), "external std::fs::write_file\n") {
		t.Errorf("IR manifest missing fs write capability:\n%s", manifestBytes)
		return 1
	}
	return 0
}

// requiredSelfhostIRContractFragments returns facts the hosted backend requires.
func requiredSelfhostIRContractFragments() []string {
	fragments := []string{
		"ir-contract selfhost-checked-package-v1\n",
		"module selfhost\n",
		"entry selfhost::cli_main\n",
		"checked-entry selfhost::cli_main\n",
		"hosted-entry @kizu_selfhost__cli_main\n",
		"hosted-smoke @kizu_selfhost__smoke\n",
		"executable-contract-source data selfhost::backend::data\n",
		"executable-contract-source lowering selfhost::backend::executable\n",
	}
	fragments = append(fragments, requiredSelfhostIRSelectedFunctionFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedSignatureFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedHelperBodyFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyParsingFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyLoweringFragments()...)
	return append(fragments, []string{
		"frontend-executable-lowering checked-ast-selected-body-ir\n",
		"hosted-executable-abi executable-result-layout-v1\n",
		"executable-ast-layout kind:i64 payload:[]u8\n",
		"executable-layout kind:i64 stdout_payload:[]u8\n",
		"executable-ast-kind Unsupported 0\n",
		"executable-ast-kind RunPrintCall 1\n",
		"executable-ast-kind RunReturnVoid 2\n",
		"executable-ast-kind TestExpectTrue 3\n",
		"executable-ast-kind TestExpectFalse 4\n",
		"executable-kind Unsupported 0\n",
		"executable-kind RunPrintString 1\n",
		"executable-kind RunReturnVoid 2\n",
		"executable-kind TestExpectOk 3\n",
		"executable-kind TestExpectFailure 4\n",
		"checked-nodes ",
		"checked-resources ",
		"checked-borrows ",
		"checked-diagnostics 0\n",
	}...)
}

// requiredSelfhostIRSelectedFunctionFragments returns selected executable path facts.
func requiredSelfhostIRSelectedFunctionFragments() []string {
	return []string{
		"executable-selected-functions checked-ast-path-v1\n",
		"selected-function selfhost::cli::execute::run_file_cli checked-run-artifact\n",
		"selected-function selfhost::cli::execute::test_file_cli checked-test-artifact\n",
		"selected-function selfhost::backend::executable::" +
			"lower_run_executable checked-run-wrapper\n",
		"selected-function selfhost::backend::executable::" +
			"parse_run_executable_ast checked-run-ast\n",
		"selected-function selfhost::backend::executable::" +
			"lower_run_executable_ast checked-run-executable\n",
		"selected-function selfhost::backend::executable::" +
			"lower_test_executable checked-test-wrapper\n",
		"selected-function selfhost::backend::executable::" +
			"parse_test_executable_ast checked-test-ast\n",
		"selected-function selfhost::backend::executable::" +
			"lower_test_executable_ast checked-test-executable\n",
		"selected-function selfhost::backend::hosted::" +
			"emit_run_executable_artifact hosted-run-writer\n",
		"selected-function selfhost::backend::hosted::" +
			"emit_test_executable_artifact hosted-test-writer\n",
	}
}

// requiredSelfhostIRSelectedSignatureFragments returns selected executable signatures.
func requiredSelfhostIRSelectedSignatureFragments() []string {
	return []string{
		"executable-selected-signatures checked-ast-signature-v1\n",
		"selected-signature selfhost::cli::execute::run_file_cli checked-run-artifact\n",
		"selected-signature selfhost::cli::execute::test_file_cli checked-test-artifact\n",
		"selected-signature selfhost::backend::executable::" +
			"lower_run_executable checked-run-wrapper\n",
		"selected-signature selfhost::backend::executable::" +
			"parse_run_executable_ast checked-run-ast\n",
		"selected-signature selfhost::backend::executable::" +
			"lower_run_executable_ast checked-run-executable\n",
		"selected-signature selfhost::backend::executable::" +
			"parse_run_program_ast checked-run-ast-helper\n",
		"selected-signature selfhost::backend::executable::" +
			"parse_run_print_call_ast checked-run-ast-helper\n",
		"selected-signature selfhost::backend::executable::" +
			"parse_expect_call_ast checked-test-ast-helper\n",
		"selected-signature-param-count selfhost::cli::execute::run_file_cli 3\n",
		"selected-signature-return selfhost::cli::execute::run_file_cli !i64\n",
		"selected-signature-param selfhost::cli::execute::run_file_cli#0 " +
			"allocator:runtime:Allocator\n",
		"selected-signature-return selfhost::backend::executable::" +
			"lower_run_executable !data::Executable\n",
		"selected-signature-param selfhost::backend::executable::" +
			"lower_run_executable#1 ast:runtime:std::kizu::ast::Ast\n",
		"selected-signature-return selfhost::backend::executable::" +
			"lower_run_executable_ast data::Executable\n",
		"selected-signature-return selfhost::backend::executable::" +
			"parse_run_program_ast !data::ExecutableAst\n",
		"selected-signature-param selfhost::backend::executable::" +
			"parse_run_print_call_ast#3 args:runtime:std::kizu::ast::ChildRange\n",
		"selected-signature-param selfhost::backend::executable::" +
			"parse_expect_call_ast#3 args:runtime:std::kizu::ast::ChildRange\n",
		"selected-signature-return selfhost::backend::hosted::" +
			"emit_run_executable_artifact !data::RunArtifact\n",
		"selected-signature-param selfhost::backend::hosted::" +
			"emit_run_executable_artifact#3 executable:runtime:data::Executable\n",
	}
}

// requiredSelfhostIRSelectedBodyFragments returns checked AST body IR facts.
func requiredSelfhostIRSelectedBodyFragments() []string {
	return []string{
		"executable-selected-body-ir checked-ast-body-v1\n",
		"selected-function-body selfhost::cli::execute::run_file_cli checked-run-artifact\n",
		"selected-function-body selfhost::cli::execute::test_file_cli checked-test-artifact\n",
		"selected-function-body selfhost::backend::executable::" +
			"lower_run_executable checked-run-wrapper\n",
		"selected-function-body selfhost::backend::executable::" +
			"parse_run_executable_ast checked-run-ast\n",
		"selected-function-body selfhost::backend::executable::" +
			"lower_run_executable_ast checked-run-executable\n",
		"selected-function-body selfhost::backend::executable::" +
			"lower_test_executable checked-test-wrapper\n",
		"selected-function-body selfhost::backend::executable::" +
			"parse_test_executable_ast checked-test-ast\n",
		"selected-function-body selfhost::backend::executable::" +
			"lower_test_executable_ast checked-test-executable\n",
		"selected-function-body selfhost::backend::hosted::" +
			"emit_run_executable_artifact hosted-run-writer\n",
		"selected-function-body selfhost::backend::hosted::" +
			"emit_test_executable_artifact hosted-test-writer\n",
		"body-call selfhost::cli::execute::run_file_cli 82 check::checked_ast_node 6\n",
		"body-call selfhost::cli::execute::run_file_cli 109 backend::lower_run_executable 3\n",
		"body-call selfhost::cli::execute::test_file_cli 109 backend::lower_test_executable 3\n",
		"body-call selfhost::backend::executable::lower_run_executable " +
			"4 parse_run_executable_ast 3\n",
		"body-call selfhost::backend::executable::lower_test_executable " +
			"4 parse_test_executable_ast 3\n",
		"body-struct-literal selfhost::backend::executable::" +
			"lower_run_executable_ast 13 data::Executable\n",
		"body-struct-literal selfhost::backend::executable::" +
			"lower_test_executable_ast 13 data::Executable\n",
		"body-call selfhost::backend::hosted::emit_run_executable_artifact " +
			"28 write_run_artifact 6\n",
		"body-call selfhost::backend::hosted::emit_test_executable_artifact " +
			"28 write_test_artifact 6\n",
	}
}

// requiredSelfhostIRSelectedHelperBodyFragments returns checked helper body IR facts.
func requiredSelfhostIRSelectedHelperBodyFragments() []string {
	return []string{
		"executable-selected-helper-body-ir checked-ast-helper-body-v1\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_program_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_fn_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_block_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_return_stmt_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_print_stmt_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_run_print_call_ast checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"run_string_literal_payload checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"run_payload_from_literal checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"is_supported_run_print_payload checked-run-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_test_program_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_test_fn_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_test_block_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_test_expect_statement_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"is_void_return_type checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_expect_stmt_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"parse_expect_call_ast checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"is_empty_return checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"expect_bool_value checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"bool_value_as_i64 checked-test-ast-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"is_empty_node checked-executable-shared-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"unsupported_executable_ast checked-executable-shared-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"unsupported_executable checked-executable-shared-helper\n",
		"selected-helper-body selfhost::backend::executable::" +
			"ast_node_text checked-executable-shared-helper\n",
		"body-call selfhost::backend::executable::parse_run_program_ast " +
			"39 parse_run_fn_ast 4\n",
		"body-call selfhost::backend::executable::parse_test_program_ast " +
			"39 parse_test_fn_ast 5\n",
		"body-call selfhost::backend::executable::parse_run_print_call_ast " +
			"38 run_string_literal_payload 3\n",
		"body-call selfhost::backend::executable::parse_expect_call_ast " +
			"38 expect_bool_value 2\n",
	}
}

// requiredSelfhostIRSelectedBodyParsingFragments returns checked AST parser facts.
func requiredSelfhostIRSelectedBodyParsingFragments() []string {
	return []string{
		"executable-selected-body-parsing checked-ast-body-parsing-v1\n",
		"selected-body-parsing selfhost::backend::executable::" +
			"parse_run_executable_ast checked-run-ast\n",
		"selected-body-parsing selfhost::backend::executable::" +
			"parse_test_executable_ast checked-test-ast\n",
	}
}

// requiredSelfhostIRSelectedBodyLoweringFragments returns selected body lowering facts.
func requiredSelfhostIRSelectedBodyLoweringFragments() []string {
	return []string{
		"executable-selected-body-lowering checked-ast-body-lowering-v1\n",
		"selected-body-lowering selfhost::backend::executable::" +
			"lower_run_executable_ast checked-run-executable\n",
		"selected-body-lowering selfhost::backend::executable::" +
			"lower_test_executable_ast checked-test-executable\n",
		"selected-body-lowering-unsupported selfhost::backend::executable::" +
			"lower_run_executable_ast unsupported_executable\n",
		"selected-body-lowering-unsupported selfhost::backend::executable::" +
			"lower_test_executable_ast unsupported_executable\n",
	}
}

// runSelfhostIRArtifactGate loads the selfhost package and runs its artifact smoke.
func runSelfhostIRArtifactGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::artifact_gate")
	return out.String(), err
}
