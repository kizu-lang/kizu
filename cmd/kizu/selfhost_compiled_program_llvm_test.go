package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfhostGenericIdentityInstancesVerifyWithClang(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_program_llvm::generic_identity_instances_gate",
	)
	if err != nil {
		t.Fatalf("generic identity instance gate failed: %v\n%s", err, out)
	}
	for _, symbol := range []string{
		"define i64 @kizu_instance_0_0_",
		"define i1 @kizu_instance_0_0_",
		"define i64 @kizu_app__generic_use_i64",
		"define i1 @kizu_app__generic_use_bool",
	} {
		if strings.Count(out, symbol) != 1 {
			t.Fatalf("generic identity specialization %q missing or duplicate\n%s", symbol, out)
		}
	}
	for _, call := range []string{
		"call i64 @kizu_instance_0_0_",
		"call i1 @kizu_instance_0_0_",
	} {
		if strings.Count(out, call) != 1 {
			t.Fatalf("production caller did not target specialization %q\n%s", call, out)
		}
	}
	module := out + `
define i32 @main() {
entry:
  %i = call i64 @kizu_app__generic_use_i64(i64 42)
  %b = call i1 @kizu_app__generic_use_bool(i1 true)
  %iok = icmp eq i64 %i, 42
  %ok = and i1 %iok, %b
  %code = select i1 %ok, i32 0, i32 1
  ret i32 %code
}
`
	exe := filepath.Join(t.TempDir(), "generic-identity")
	clang := exec.Command("clang", "-x", "ir", "-o", exe, "-")
	clang.Stdin = strings.NewReader(module)
	if verifyOut, verifyErr := clang.CombinedOutput(); verifyErr != nil {
		t.Fatalf("clang rejected generic instances: %v\n%s\n%s", verifyErr, verifyOut, module)
	}
	if runOut, runErr := exec.Command(exe).CombinedOutput(); runErr != nil {
		t.Fatalf("generic identity executable failed: %v\n%s", runErr, runOut)
	}
}

func TestSelfhostCompiledProgramLLVMOwnsReachableFunctionEmission(t *testing.T) {
	programLLVM := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_program_llvm.kizu",
	)
	cliLLVM := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	consumer := selfhostKizuFunctionBody(
		t, programLLVM, "pub fn append_reachable_functions(",
	)
	for _, fragment := range []string{
		`let prefix = "package-dependency ";`,
		`let name_prefix = "package-definition-name ";`,
		"emit_numeric_package_definition(",
	} {
		if !strings.Contains(consumer, fragment) {
			t.Errorf("compiled program consumer missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"ir_index::build(",
		"compiled_canonical_facts::parse_and_validate(",
		"run_file_cli",
		"format_source",
	} {
		if strings.Contains(consumer, forbidden) {
			t.Errorf("compiled program consumer contains forbidden ownership or source policy %q", forbidden)
		}
	}
	for _, input := range []string{
		"lookup_index: &ir_index::IrIndex",
		"canonical_facts: &compiled_canonical_facts::CanonicalFactTable",
	} {
		if !strings.Contains(programLLVM, input) {
			t.Errorf("compiled program API missing caller-owned input %q", input)
		}
	}

	emitter := selfhostKizuFunctionBody(
		t, programLLVM, "fn emit_numeric_package_definition(",
	)
	if !strings.Contains(
		emitter, "compiled_llvm::append_compiled_function_auto_indexed(",
	) {
		t.Fatal("compiled program emitter does not use the caller-owned index and canonical facts")
	}

	for _, fragment := range []string{
		"import selfhost::backend::compiled_program_llvm;",
		"compiled_program_llvm::append_reachable_functions(",
		"out, lookup_index, &canonical_facts, ir_bytes",
	} {
		if !strings.Contains(cliLLVM, fragment) {
			t.Errorf("cli_llvm direct compiled program call missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"pub fn append_reachable_functions(",
		"pub fn consume_package_dependencies(",
		"fn emit_numeric_package_definition(",
		`let prefix = "package-dependency ";`,
	} {
		if strings.Contains(cliLLVM, forbidden) {
			t.Errorf("cli_llvm retained compiled program implementation %q", forbidden)
		}
	}
}
