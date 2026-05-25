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
	fragments = append(fragments, requiredSelfhostIRSelectedBodyFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyParsingFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyLoweringFragments()...)
	return append(fragments, []string{
		"frontend-executable-lowering checked-ast-bounded\n",
		"hosted-executable-ast executable-ast-rules-v1\n",
		"executable-ast-rule MainScan LeadingFunctions\n",
		"executable-ast-rule RunPrintCall MainPrintString\n",
		"executable-ast-rule RunReturnVoid MainReturnVoid\n",
		"executable-ast-rule TestExpectTrue MainExpectTrue\n",
		"executable-ast-rule TestExpectFalse MainExpectFalse\n",
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
		"body-call selfhost::cli::execute::run_file_cli check::checked_ast_node 6\n",
		"body-call selfhost::cli::execute::run_file_cli backend::lower_run_executable 3\n",
		"body-call selfhost::cli::execute::test_file_cli backend::lower_test_executable 3\n",
		"body-call selfhost::backend::executable::lower_run_executable " +
			"parse_run_executable_ast 3\n",
		"body-call selfhost::backend::executable::lower_test_executable " +
			"parse_test_executable_ast 3\n",
		"body-struct-literal selfhost::backend::executable::" +
			"lower_run_executable_ast data::Executable\n",
		"body-struct-literal selfhost::backend::executable::" +
			"lower_test_executable_ast data::Executable\n",
		"body-call selfhost::backend::hosted::emit_run_executable_artifact " +
			"write_run_artifact 6\n",
		"body-call selfhost::backend::hosted::emit_test_executable_artifact " +
			"write_test_artifact 6\n",
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
		"selected-body-parser-rule MainScan LeadingFunctions\n",
		"selected-body-parser-rule RunPrintCall MainPrintString\n",
		"selected-body-parser-rule RunReturnVoid MainReturnVoid\n",
		"selected-body-parser-rule TestExpectTrue MainExpectTrue\n",
		"selected-body-parser-rule TestExpectFalse MainExpectFalse\n",
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
		"selected-run-body-lowering-rule RunPrintCall RunPrintString\n",
		"selected-run-body-lowering-rule RunReturnVoid RunReturnVoid\n",
		"selected-test-body-lowering-rule TestExpectTrue TestExpectOk\n",
		"selected-test-body-lowering-rule TestExpectFalse TestExpectFailure\n",
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
