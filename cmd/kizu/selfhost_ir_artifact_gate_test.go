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
	fragments = append(fragments, requiredSelfhostIRSelectedSignatureFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedHelperBodyFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyParsingFragments()...)
	fragments = append(fragments, requiredSelfhostIRSelectedBodyLoweringFragments()...)
	fragments = append(fragments, requiredSelfhostIRHostedArtifactPathFragments()...)
	fragments = append(fragments, requiredSelfhostIRHostedLoweringFragments()...)
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

// requiredSelfhostIRSelectedSignatureFragments returns selected executable signatures.
func requiredSelfhostIRSelectedSignatureFragments() []string {
	return []string{
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
		"body-call selfhost::cli::execute::run_file_cli ",
		"body-call selfhost::backend::hosted::emit_run_executable_artifact ",
		"selected-function-body-end selfhost::backend::executable::lower_run_executable_ast ",
	}
}

// requiredSelfhostIRSelectedHelperBodyFragments returns checked helper body IR facts.
func requiredSelfhostIRSelectedHelperBodyFragments() []string {
	return []string{
		"selected-helper-body-end selfhost::backend::executable::parse_run_program_ast ",
		"selected-helper-body-end selfhost::backend::executable::parse_test_program_ast ",
	}
}

// requiredSelfhostIRSelectedBodyParsingFragments returns checked AST parser facts.
func requiredSelfhostIRSelectedBodyParsingFragments() []string {
	return []string{
		"selected-body-parsing-token syntax-fn fn\n",
		"selected-body-parsing-token syntax-test test\n",
		"selected-body-parsing-token syntax-return return\n",
		"selected-body-parsing-token syntax-void void\n",
		"selected-body-parsing-token value-main main\n",
		"selected-body-parsing-token run-print-callee print\n",
		"selected-body-parsing-token expect-callee-root std\n",
		"selected-body-parsing-token expect-callee-module testing\n",
		"selected-body-parsing-token expect-callee-function expect\n",
		"selected-body-parsing-token literal-true true\n",
		"selected-body-parsing-token literal-false false\n",
	}
}

// requiredSelfhostIRSelectedBodyLoweringFragments returns the selected body lowering gate fact.
func requiredSelfhostIRSelectedBodyLoweringFragments() []string {
	return []string{
		"selected-function-body-end selfhost::backend::executable::lower_run_executable_ast ",
		"selected-function-body-end selfhost::backend::executable::lower_test_executable_ast ",
	}
}

// requiredSelfhostIRHostedArtifactPathFragments returns selected hosted
// artifact path facts derived from backend::hosted writer bodies.
func requiredSelfhostIRHostedArtifactPathFragments() []string {
	return []string{
		"hosted-artifact-dir selfhost::backend::hosted::" +
			"emit_run_executable_artifact target/selfhost/run\n",
		"hosted-artifact-ll-prefix selfhost::backend::hosted::" +
			"emit_run_executable_artifact target/selfhost/run/\n",
		"hosted-artifact-ll-suffix selfhost::backend::hosted::" +
			"emit_run_executable_artifact .ll\n",
		"hosted-artifact-metadata-prefix selfhost::backend::hosted::" +
			"emit_run_executable_artifact target/selfhost/run/\n",
		"hosted-artifact-metadata-suffix selfhost::backend::hosted::" +
			"emit_run_executable_artifact .ll.meta\n",
		"hosted-artifact-writer selfhost::backend::hosted::" +
			"emit_run_executable_artifact write_run_artifact\n",
		"hosted-artifact-metadata-title selfhost::backend::hosted::" +
			"write_run_metadata kizu-run-artifact-v0\n",
		"hosted-artifact-metadata-issue selfhost::backend::hosted::" +
			"write_run_metadata issue\\20#569\n",
		"hosted-artifact-dir selfhost::backend::hosted::" +
			"emit_test_executable_artifact target/selfhost/test\n",
		"hosted-artifact-ll-prefix selfhost::backend::hosted::" +
			"emit_test_executable_artifact target/selfhost/test/\n",
		"hosted-artifact-ll-suffix selfhost::backend::hosted::" +
			"emit_test_executable_artifact .ll\n",
		"hosted-artifact-metadata-prefix selfhost::backend::hosted::" +
			"emit_test_executable_artifact target/selfhost/test/\n",
		"hosted-artifact-metadata-suffix selfhost::backend::hosted::" +
			"emit_test_executable_artifact .ll.meta\n",
		"hosted-artifact-writer selfhost::backend::hosted::" +
			"emit_test_executable_artifact write_test_artifact\n",
		"hosted-artifact-metadata-title selfhost::backend::hosted::" +
			"write_test_metadata kizu-test-artifact-v0\n",
		"hosted-artifact-metadata-issue selfhost::backend::hosted::" +
			"write_test_metadata issue\\20#570\n",
		"hosted-artifact-metadata-source-prefix selfhost::backend::hosted::" +
			"append_common_metadata source\\20\n",
		"hosted-artifact-metadata-output-prefix selfhost::backend::hosted::" +
			"append_common_metadata output\\20\n",
		"hosted-artifact-metadata-abi-line selfhost::backend::hosted::" +
			"append_common_metadata abi\\20selfhost-abi-v0\n",
		"hosted-artifact-metadata-entry-prefix selfhost::backend::hosted::" +
			"append_common_metadata entry\\20@\n",
		"hosted-artifact-metadata-runtime-line selfhost::backend::hosted::" +
			"append_common_metadata runtime\\20target/selfhost/selfhost.host.ll\n",
		"hosted-artifact-metadata-lowering-line selfhost::backend::hosted::" +
			"append_common_metadata executable_lowering\\20selfhost::backend::" +
			"executable\\20checked-ast\n",
		"hosted-artifact-metadata-fallback-line selfhost::backend::hosted::" +
			"append_common_metadata go.cmd-kizu-fallback\\20none\n",
		"hosted-artifact-metadata-mode-line selfhost::backend::hosted::" +
			"append_common_metadata artifact_mode\\20hosted-artifact\n",
		"hosted-artifact-metadata-discovery-line selfhost::backend::hosted::" +
			"write_test_metadata discovery\\20none\n",
	}
}

// requiredSelfhostIRHostedLoweringFragments returns selected hosted artifact
// behavior facts derived from backend::hosted lowering bodies.
func requiredSelfhostIRHostedLoweringFragments() []string {
	return []string{
		"hosted-lowering-case-kind selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 RunPrintString\n",
		"hosted-lowering-case-comment-llvm selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 kizu\\20run\\20artifact\\20ll\\20v0\n",
		"hosted-lowering-case-entry selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 kizu_run_main\n",
		"hosted-lowering-case-global selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 kizu.run.stdout\n",
		"hosted-lowering-case-stream selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 Stdout\n",
		"hosted-lowering-case-newline selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 true\n",
		"hosted-lowering-case-exit selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 0\n",
		"hosted-lowering-case-payload selfhost::backend::hosted::" +
			"lower_run_hosted_executable 0 executable-field\n",
		"hosted-lowering-case-kind selfhost::backend::hosted::" +
			"lower_run_hosted_executable 1 RunReturnVoid\n",
		"hosted-lowering-case-comment-llvm selfhost::backend::hosted::" +
			"lower_run_hosted_executable 1 kizu\\20run\\20artifact\\20ll\\20v0\n",
		"hosted-lowering-case-global selfhost::backend::hosted::" +
			"lower_run_hosted_executable 1 none\n",
		"hosted-lowering-case-stream selfhost::backend::hosted::" +
			"lower_run_hosted_executable 1 None\n",
		"hosted-lowering-case-payload selfhost::backend::hosted::" +
			"lower_run_hosted_executable 1 empty-source-slice\n",
		"hosted-lowering-case-kind selfhost::backend::hosted::" +
			"lower_test_hosted_executable 0 TestExpectOk\n",
		"hosted-lowering-case-comment-llvm selfhost::backend::hosted::" +
			"lower_test_hosted_executable 0 kizu\\20test\\20artifact\\20ll\\20v0\n",
		"hosted-lowering-case-global selfhost::backend::hosted::" +
			"lower_test_hosted_executable 0 kizu.test.ok\n",
		"hosted-lowering-case-stream selfhost::backend::hosted::" +
			"lower_test_hosted_executable 0 Stdout\n",
		"hosted-lowering-case-payload-llvm selfhost::backend::hosted::" +
			"lower_test_hosted_executable 0 test:\\20ok\n",
		"hosted-lowering-case-kind selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 TestExpectFailure\n",
		"hosted-lowering-case-comment-llvm selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 kizu\\20test\\20artifact\\20ll\\20v0\n",
		"hosted-lowering-case-global selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 kizu.test.failure\n",
		"hosted-lowering-case-stream selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 Stderr\n",
		"hosted-lowering-case-exit selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 1\n",
		"hosted-lowering-case-payload-llvm selfhost::backend::hosted::" +
			"lower_test_hosted_executable 1 error:\\20runtime\\20error:" +
			"\\20expected\\20condition\\20to\\20be\\20true\n",
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
