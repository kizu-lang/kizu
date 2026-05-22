package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
)

type selfhostCLIFrontendCase struct {
	name      string
	args      []string
	wantOut   string
	wantErr   string
	wantFiles []selfhostCLIArtifactExpectation
}

type selfhostCLIArtifactExpectation struct {
	path     string
	contains []string
	rejects  []string
}

type selfhostCLIFrontendFixtures struct {
	source              string
	runSource           string
	movedSource         string
	unknownSource       string
	unknownStd          string
	undefinedVariable   string
	undefinedMatch      string
	aritySource         string
	duplicate           string
	typeArity           string
	unknownType         string
	argumentMismatch    string
	assignmentMismatch  string
	immutableAssignment string
	invalidAssignment   string
	returnMismatch      string
	returnMatchMismatch string
	returningIf         string
	missingReturn       string
	ifMissingReturn     string
	matchMissing        string
	invalidSource       string
	missingAssign       string
	topLevelStmt        string
	invalidToken        string
	invalidExpr         string
	invalidFnName       string
	invalidParam        string
	invalidTypeParam    string
	invalidReturn       string
	missingFnBody       string
	invalidImport       string
	invalidStruct       string
	invalidField        string
	missingColon        string
	missingType         string
	expectOK            string
	expectFail          string
	missingExpr         string
	movedValue          string
}

// TestSelfhostCLIGate executes the minimum Kizu-owned selfhost CLI contract.
func TestSelfhostCLIGate(t *testing.T) {
	requireSelfhostGate(t)
	if failures := countSelfhostCLIGateFailures(t); failures > 0 {
		t.Fatalf("selfhost CLI gate failures=%d", failures)
	}
}

// TestSelfhostCLIFileFrontendGate runs file commands through Kizu frontend code.
func TestSelfhostCLIFileFrontendGate(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()

	fixtures := writeSelfhostCLIFrontendFixtures(t)

	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatalf("load selfhost package: %v", err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("check selfhost package: %v", err)
	}

	for _, tt := range selfhostCLIFrontendCases(fixtures) {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runSelfhostCLIFileFrontendCase(program, tt.args)
			if err != nil {
				t.Fatalf("run selfhost cli gate: %v\nstdout:\n%sstderr:\n%s", err, stdout, stderr)
			}
			if stdout != tt.wantOut || stderr != tt.wantErr {
				t.Fatalf(
					"output mismatch\nwant stdout:\n%swant stderr:\n%sgot stdout:\n%sgot stderr:\n%s",
					tt.wantOut,
					tt.wantErr,
					stdout,
					stderr,
				)
			}
			checkSelfhostCLIArtifactExpectations(t, tt.wantFiles)
		})
	}
}

// checkSelfhostCLIArtifactExpectations validates emitted source-driven artifacts.
func checkSelfhostCLIArtifactExpectations(
	t *testing.T,
	expectations []selfhostCLIArtifactExpectation,
) {
	t.Helper()
	for _, item := range expectations {
		content := readSelfhostCLIArtifact(t, item.path)
		for _, fragment := range item.contains {
			if !strings.Contains(content, fragment) {
				t.Fatalf("artifact %s missing %q:\n%s", item.path, fragment, content)
			}
		}
		for _, fragment := range item.rejects {
			if strings.Contains(content, fragment) {
				t.Fatalf("artifact %s kept rejected %q:\n%s", item.path, fragment, content)
			}
		}
	}
}

// readSelfhostCLIArtifact reads one emitted selfhost CLI artifact.
func readSelfhostCLIArtifact(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %s: %v", path, err)
	}
	if len(bytes) == 0 {
		t.Fatalf("artifact %s is empty", path)
	}
	return string(bytes)
}

// selfhostCLIFrontendCases returns source-driven CLI frontend cases.
func selfhostCLIFrontendCases(fixtures selfhostCLIFrontendFixtures) []selfhostCLIFrontendCase {
	cases := selfhostCLIFrontendHappyCases(fixtures)
	cases = append(cases, selfhostCLIFrontendParseFailureCases(fixtures)...)
	cases = append(cases, selfhostCLIFrontendCheckSemanticFailureCases(fixtures)...)
	cases = append(cases, selfhostCLIFrontendCheckParseFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendHappyCases returns successful source-driven frontend cases.
func selfhostCLIFrontendHappyCases(fixtures selfhostCLIFrontendFixtures) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name: "parse_temp_source",
			args: []string{"parse", fixtures.source},
			wantOut: "enum Flag { Yes, No }\nstruct Name { value: []u8 }\n" +
				"fn choose(flag: Flag) -> bool { return match flag { " +
				"Yes => true, No => false }; }\n" +
				"fn main(values: std::array::Array <Name>) { let count = values.len(); " +
				"print(count); values.deinit(); }\nexit-code\n0\n",
		},
		{
			name:    "check_temp_source",
			args:    []string{"check", fixtures.source},
			wantOut: "check: ok\nexit-code\n0\n",
		},
		{
			name:    "check_temp_returning_if",
			args:    []string{"check", fixtures.returningIf},
			wantOut: "check: ok\nexit-code\n0\n",
		},
		{
			name:    "run_temp_source",
			args:    []string{"run", fixtures.runSource},
			wantOut: "exit-code\n0\n",
			wantFiles: selfhostRunArtifactExpectations(
				fixtures.runSource,
				"selfhost/tests/cli/run_hello.kizu",
			),
		},
		{
			name:    "test_temp_expect_ok",
			args:    []string{"test", fixtures.expectOK},
			wantOut: "exit-code\n0\n",
			wantFiles: selfhostTestArtifactExpectations(
				fixtures.expectOK,
				"test_expect_ok",
				"selfhost/tests/cli/test_expect_ok.kizu",
			),
		},
		{
			name:    "test_temp_expect_failure",
			args:    []string{"test", fixtures.expectFail},
			wantOut: "exit-code\n0\n",
			wantFiles: selfhostTestArtifactExpectations(
				fixtures.expectFail,
				"test_expect_failure",
				"selfhost/tests/cli/test_expect_failure.kizu",
			),
		},
	}
}

// selfhostRunArtifactExpectations returns run artifact content checks.
func selfhostRunArtifactExpectations(
	sourcePath string,
	rejectedSourcePath string,
) []selfhostCLIArtifactExpectation {
	return []selfhostCLIArtifactExpectation{
		{
			path: filepath.Join("target", "selfhost", "run", "run_hello.ll"),
			contains: []string{
				`source_filename = "` + sourcePath + `"`,
			},
			rejects: []string{rejectedSourcePath},
		},
		{
			path: filepath.Join("target", "selfhost", "run", "run_hello.ll.meta"),
			contains: []string{
				"source " + sourcePath + "\n",
			},
			rejects: []string{rejectedSourcePath},
		},
	}
}

// selfhostTestArtifactExpectations returns test artifact content checks.
func selfhostTestArtifactExpectations(
	sourcePath string,
	stem string,
	rejectedSourcePath string,
) []selfhostCLIArtifactExpectation {
	return []selfhostCLIArtifactExpectation{
		{
			path: filepath.Join("target", "selfhost", "test", stem+".ll"),
			contains: []string{
				`source_filename = "` + sourcePath + `"`,
			},
			rejects: []string{rejectedSourcePath},
		},
		{
			path: filepath.Join("target", "selfhost", "test", stem+".ll.meta"),
			contains: []string{
				"source " + sourcePath + "\n",
			},
			rejects: []string{rejectedSourcePath},
		},
	}
}

// selfhostCLIFrontendParseFailureCases returns parse diagnostic frontend cases.
func selfhostCLIFrontendParseFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	cases := selfhostCLIFrontendParseGeneralFailureCases(fixtures)
	cases = append(cases, selfhostCLIFrontendParseAggregateFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendParseGeneralFailureCases returns common parse failures.
func selfhostCLIFrontendParseGeneralFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	cases := selfhostCLIFrontendParseSyntaxFailureCases(fixtures)
	cases = append(cases, selfhostCLIFrontendParseFunctionFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendParseSyntaxFailureCases returns statement/token failures.
func selfhostCLIFrontendParseSyntaxFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "parse_invalid",
			args:    []string{"parse", fixtures.missingExpr},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected expression, got ; at 1:25\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_missing_brace",
			args:    []string{"parse", fixtures.invalidSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected right brace at 3:1\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_missing_assign",
			args:    []string{"parse", fixtures.missingAssign},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected assign, got ; at 2:14\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_top_level_statement",
			args:    []string{"parse", fixtures.topLevelStmt},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected declaration, got let at 1:1\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_token",
			args:    []string{"parse", fixtures.invalidToken},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected declaration, got ILLEGAL at 1:1\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_expr_token",
			args:    []string{"parse", fixtures.invalidExpr},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected expression, got ILLEGAL at 2:11\nerror: parse failed\n",
		},
	}
}

// selfhostCLIFrontendParseFunctionFailureCases returns function header failures.
func selfhostCLIFrontendParseFunctionFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "parse_temp_invalid_fn_name",
			args:    []string{"parse", fixtures.invalidFnName},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected function name, got { at 1:4\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_fn_param",
			args:    []string{"parse", fixtures.invalidParam},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected colon, got ) at 1:14\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_fn_type_param",
			args:    []string{"parse", fixtures.invalidTypeParam},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type parameter, got > at 1:9\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_fn_return_type",
			args:    []string{"parse", fixtures.invalidReturn},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type name, got { at 1:14\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_missing_fn_body",
			args:    []string{"parse", fixtures.missingFnBody},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected left brace, got ; at 1:11\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_import",
			args:    []string{"parse", fixtures.invalidImport},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got ; at 1:8\nerror: parse failed\n",
		},
	}
}

// selfhostCLIFrontendParseAggregateFailureCases returns aggregate body failures.
func selfhostCLIFrontendParseAggregateFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "parse_temp_invalid_struct",
			args:    []string{"parse", fixtures.invalidStruct},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got { at 1:8\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_invalid_struct_field",
			args:    []string{"parse", fixtures.invalidField},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got : at 1:15\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_missing_struct_field_colon",
			args:    []string{"parse", fixtures.missingColon},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected colon, got } at 1:21\nerror: parse failed\n",
		},
		{
			name:    "parse_temp_missing_struct_field_type",
			args:    []string{"parse", fixtures.missingType},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type name, got } at 1:22\nerror: parse failed\n",
		},
	}
}

// selfhostCLIFrontendCheckSemanticFailureCases returns check semantic diagnostics.
func selfhostCLIFrontendCheckSemanticFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	cases := selfhostCLIFrontendCheckOnlySemanticFailureCases(fixtures)
	cases = append(cases, selfhostCLIFrontendRunSemanticFailureCases(fixtures)...)
	cases = append(cases, selfhostCLIFrontendTestSemanticFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendCheckOnlySemanticFailureCases returns check-only failures.
func selfhostCLIFrontendCheckOnlySemanticFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	cases := []selfhostCLIFrontendCase{
		{
			name:    "check_temp_moved_value",
			args:    []string{"check", fixtures.movedValue},
			wantOut: "exit-code\n1\n",
			wantErr: "error: move error: moved value `name` was used\n",
		},
		{
			name:    "check_temp_moved_record",
			args:    []string{"check", fixtures.movedSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: move error: moved value `person` was used\n",
		},
		{
			name:    "check_temp_unknown_call",
			args:    []string{"check", fixtures.unknownSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `missing_symbol`\n",
		},
		{
			name:    "check_temp_unknown_std_call",
			args:    []string{"check", fixtures.unknownStd},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `std::testing::missing`\n",
		},
		{
			name:    "check_temp_undefined_variable",
			args:    []string{"check", fixtures.undefinedVariable},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: undefined variable `name`\n",
		},
		{
			name:    "check_temp_undefined_match_arm_variable",
			args:    []string{"check", fixtures.undefinedMatch},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: undefined variable `name`\n",
		},
		{
			name:    "check_temp_arity_mismatch",
			args:    []string{"check", fixtures.aritySource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: function `takes_one` expects 1 argument, got 2\n",
		},
		{
			name:    "check_temp_duplicate_declaration",
			args:    []string{"check", fixtures.duplicate},
			wantOut: "exit-code\n1\n",
			wantErr: "error: duplicate symbol `main`\n",
		},
		{
			name:    "check_temp_type_arity_mismatch",
			args:    []string{"check", fixtures.typeArity},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type `std::map::Map` expects 2 type arguments, got 1\n",
		},
		{
			name:    "check_temp_unknown_type",
			args:    []string{"check", fixtures.unknownType},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown type `Missing`\n",
		},
	}
	cases = append(cases, selfhostCLIFrontendTypeSemanticFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendTypeSemanticFailureCases returns type-check failures.
func selfhostCLIFrontendTypeSemanticFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "check_temp_argument_type_mismatch",
			args:    []string{"check", fixtures.argumentMismatch},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: argument type mismatch\n",
		},
		{
			name:    "check_temp_assignment_type_mismatch",
			args:    []string{"check", fixtures.assignmentMismatch},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: assignment type mismatch\n",
		},
		{
			name:    "check_temp_immutable_assignment",
			args:    []string{"check", fixtures.immutableAssignment},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: cannot assign to immutable binding `count`\n",
		},
		{
			name:    "check_temp_invalid_assignment_target",
			args:    []string{"check", fixtures.invalidAssignment},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: invalid assignment target `1`\n",
		},
		{
			name:    "check_temp_return_type_mismatch",
			args:    []string{"check", fixtures.returnMismatch},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: return type mismatch\n",
		},
		{
			name:    "check_temp_return_match_type_mismatch",
			args:    []string{"check", fixtures.returnMatchMismatch},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: return type mismatch\n",
		},
		{
			name:    "check_temp_missing_return",
			args:    []string{"check", fixtures.missingReturn},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: function `bad` must return i64\n",
		},
		{
			name:    "check_temp_if_missing_return",
			args:    []string{"check", fixtures.ifMissingReturn},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: function `bad` must return i64\n",
		},
		{
			name:    "check_temp_match_non_exhaustive",
			args:    []string{"check", fixtures.matchMissing},
			wantOut: "exit-code\n1\n",
			wantErr: "error: type error: match on `Color` is not exhaustive\n",
		},
	}
}

// selfhostCLIFrontendRunSemanticFailureCases returns run frontend failures.
func selfhostCLIFrontendRunSemanticFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "run_temp_moved_value",
			args:    []string{"run", fixtures.movedValue},
			wantOut: "exit-code\n1\n",
			wantErr: "error: move error: moved value `name` was used\n",
		},
		{
			name:    "run_temp_unknown_call",
			args:    []string{"run", fixtures.unknownSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `missing_symbol`\n",
		},
		{
			name:    "run_temp_unknown_std_call",
			args:    []string{"run", fixtures.unknownStd},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `std::testing::missing`\n",
		},
	}
}

// selfhostCLIFrontendTestSemanticFailureCases returns test frontend failures.
func selfhostCLIFrontendTestSemanticFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "test_temp_unknown_call",
			args:    []string{"test", fixtures.unknownSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `missing_symbol`\n",
		},
		{
			name:    "test_temp_unknown_std_call",
			args:    []string{"test", fixtures.unknownStd},
			wantOut: "exit-code\n1\n",
			wantErr: "error: unknown function `std::testing::missing`\n",
		},
	}
}

// selfhostCLIFrontendCheckParseFailureCases returns check parse diagnostics.
func selfhostCLIFrontendCheckParseFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	cases := selfhostCLIFrontendCheckGeneralParseFailureCases(fixtures)
	cases = append(cases, selfhostCLIFrontendCheckAggregateParseFailureCases(fixtures)...)
	return cases
}

// selfhostCLIFrontendCheckGeneralParseFailureCases returns common check parse failures.
func selfhostCLIFrontendCheckGeneralParseFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "check_temp_missing_brace",
			args:    []string{"check", fixtures.invalidSource},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected right brace at 3:1\nerror: parse failed\n",
		},
		{
			name:    "check_temp_missing_assign",
			args:    []string{"check", fixtures.missingAssign},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected assign, got ; at 2:14\nerror: parse failed\n",
		},
		{
			name:    "check_temp_top_level_statement",
			args:    []string{"check", fixtures.topLevelStmt},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected declaration, got let at 1:1\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_token",
			args:    []string{"check", fixtures.invalidToken},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected declaration, got ILLEGAL at 1:1\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_expr_token",
			args:    []string{"check", fixtures.invalidExpr},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected expression, got ILLEGAL at 2:11\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_fn_name",
			args:    []string{"check", fixtures.invalidFnName},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected function name, got { at 1:4\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_fn_param",
			args:    []string{"check", fixtures.invalidParam},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected colon, got ) at 1:14\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_fn_type_param",
			args:    []string{"check", fixtures.invalidTypeParam},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type parameter, got > at 1:9\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_fn_return_type",
			args:    []string{"check", fixtures.invalidReturn},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type name, got { at 1:14\nerror: parse failed\n",
		},
		{
			name:    "check_temp_missing_fn_body",
			args:    []string{"check", fixtures.missingFnBody},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected left brace, got ; at 1:11\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_import",
			args:    []string{"check", fixtures.invalidImport},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got ; at 1:8\nerror: parse failed\n",
		},
	}
}

// selfhostCLIFrontendCheckAggregateParseFailureCases returns aggregate parse failures.
func selfhostCLIFrontendCheckAggregateParseFailureCases(
	fixtures selfhostCLIFrontendFixtures,
) []selfhostCLIFrontendCase {
	return []selfhostCLIFrontendCase{
		{
			name:    "check_temp_invalid_struct",
			args:    []string{"check", fixtures.invalidStruct},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got { at 1:8\nerror: parse failed\n",
		},
		{
			name:    "check_temp_invalid_struct_field",
			args:    []string{"check", fixtures.invalidField},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected identifier, got : at 1:15\nerror: parse failed\n",
		},
		{
			name:    "check_temp_missing_struct_field_colon",
			args:    []string{"check", fixtures.missingColon},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected colon, got } at 1:21\nerror: parse failed\n",
		},
		{
			name:    "check_temp_missing_struct_field_type",
			args:    []string{"check", fixtures.missingType},
			wantOut: "exit-code\n1\n",
			wantErr: "error: expected type name, got } at 1:22\nerror: parse failed\n",
		},
	}
}

// writeSelfhostCLIFrontendFixtures writes source-driven frontend gate inputs.
func writeSelfhostCLIFrontendFixtures(t *testing.T) selfhostCLIFrontendFixtures {
	t.Helper()

	tempSource, tempRunSource := writeSelfhostCLIHappyFrontendFixtures(t)
	tempMovedSource, tempMovedValue, tempUnknownSource, tempUnknownStd, tempAritySource,
		tempDuplicate, tempTypeArity, tempUnknownType, tempUndefinedVariable, tempUndefinedMatch :=
		writeSelfhostCLISemanticFrontendFixtures(t)
	tempArgumentMismatch := writeSelfhostCLIArgumentFrontendFixtures(t)
	tempAssignmentMismatch, tempImmutableAssignment, tempInvalidAssignment :=
		writeSelfhostCLIAssignmentFrontendFixtures(t)
	tempReturnMismatch, tempReturnMatchMismatch, tempReturningIf,
		tempMissingReturn, tempIfMissingReturn :=
		writeSelfhostCLIReturnFrontendFixtures(t)
	tempMatchMissing := writeSelfhostCLIMatchFrontendFixtures(t)

	tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr,
		tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam, tempInvalidReturn,
		tempMissingFnBody, tempInvalidImport, tempInvalidStruct, tempInvalidField, tempMissingColon,
		tempMissingType :=
		writeSelfhostCLIInvalidFrontendFixtures(t)
	tempExpectOK, tempExpectFail := writeSelfhostCLIExpectFrontendFixtures(t)

	return selfhostCLIFrontendFixtures{
		source:              tempSource,
		runSource:           tempRunSource,
		movedSource:         tempMovedSource,
		unknownSource:       tempUnknownSource,
		unknownStd:          tempUnknownStd,
		undefinedVariable:   tempUndefinedVariable,
		undefinedMatch:      tempUndefinedMatch,
		aritySource:         tempAritySource,
		duplicate:           tempDuplicate,
		typeArity:           tempTypeArity,
		unknownType:         tempUnknownType,
		argumentMismatch:    tempArgumentMismatch,
		assignmentMismatch:  tempAssignmentMismatch,
		immutableAssignment: tempImmutableAssignment,
		invalidAssignment:   tempInvalidAssignment,
		returnMismatch:      tempReturnMismatch,
		returnMatchMismatch: tempReturnMatchMismatch,
		returningIf:         tempReturningIf,
		missingReturn:       tempMissingReturn,
		ifMissingReturn:     tempIfMissingReturn,
		matchMissing:        tempMatchMissing,
		invalidSource:       tempInvalidSource,
		missingAssign:       tempMissingAssign,
		topLevelStmt:        tempTopLevelStmt,
		invalidToken:        tempInvalidToken,
		invalidExpr:         tempInvalidExpr,
		invalidFnName:       tempInvalidFnName,
		invalidParam:        tempInvalidParam,
		invalidTypeParam:    tempInvalidTypeParam,
		invalidReturn:       tempInvalidReturn,
		missingFnBody:       tempMissingFnBody,
		invalidImport:       tempInvalidImport,
		invalidStruct:       tempInvalidStruct,
		invalidField:        tempInvalidField,
		missingColon:        tempMissingColon,
		missingType:         tempMissingType,
		expectOK:            tempExpectOK,
		expectFail:          tempExpectFail,
		missingExpr:         tempMissingExpr,
		movedValue:          tempMovedValue,
	}
}

// writeSelfhostCLIHappyFrontendFixtures writes successful frontend inputs.
func writeSelfhostCLIHappyFrontendFixtures(t *testing.T) (string, string) {
	t.Helper()

	tempSource := writeTempKizuSource(
		t,
		"frontend_source.kizu",
		`enum Flag{Yes,No}
struct Name{value:[]u8,}
fn choose(flag:Flag)->bool{return match flag{Yes=>true,No=>false};}
fn main(values:std::array::Array<Name>){let count=values.len();print(count);values.deinit();}
`,
	)

	const runSource = `fn main() {
    print("hello, kizu");
}
`
	tempRunSource := writeTempKizuSource(t, "frontend_run_hello.kizu", runSource)
	return tempSource, tempRunSource
}

// writeSelfhostCLISemanticFrontendFixtures writes semantic-check inputs.
func writeSelfhostCLISemanticFrontendFixtures(
	t *testing.T,
) (string, string, string, string, string, string, string, string, string, string) {
	t.Helper()

	tempMovedSource, tempMovedValue := writeSelfhostCLIMoveFrontendFixtures(t)
	tempUnknownSource, tempUnknownStd, tempAritySource, tempDuplicate, tempTypeArity,
		tempUnknownType := writeSelfhostCLISymbolFrontendFixtures(t)
	tempUndefinedVariable, tempUndefinedMatch := writeSelfhostCLIUndefinedVariableFrontendFixtures(t)

	return tempMovedSource, tempMovedValue, tempUnknownSource, tempUnknownStd, tempAritySource,
		tempDuplicate, tempTypeArity, tempUnknownType, tempUndefinedVariable, tempUndefinedMatch
}

// writeSelfhostCLIUndefinedVariableFrontendFixtures writes variable semantic inputs.
func writeSelfhostCLIUndefinedVariableFrontendFixtures(t *testing.T) (string, string) {
	t.Helper()

	const undefinedVariable = `fn main() {
    print(name);
}
`
	tempUndefinedVariable := writeTempKizuSource(
		t,
		"frontend_undefined_variable.kizu",
		undefinedVariable,
	)

	const undefinedMatch = `enum Color {
    Red,
    Green,
}

fn main() {
    let color = Color::Red;
    match color {
        Red => print(name);,
        Green => print("green");,
    }
}
`
	tempUndefinedMatch := writeTempKizuSource(
		t,
		"frontend_undefined_match_variable.kizu",
		undefinedMatch,
	)

	return tempUndefinedVariable, tempUndefinedMatch
}

// writeSelfhostCLIArgumentFrontendFixtures writes argument-check semantic inputs.
func writeSelfhostCLIArgumentFrontendFixtures(t *testing.T) string {
	t.Helper()

	const argumentMismatch = `fn takes_i64(value: i64) {
    print(value);
}

fn main() {
    takes_i64(true);
}
`
	return writeTempKizuSource(t, "frontend_argument_type_mismatch.kizu", argumentMismatch)
}

// writeSelfhostCLIAssignmentFrontendFixtures writes assignment semantic inputs.
func writeSelfhostCLIAssignmentFrontendFixtures(t *testing.T) (string, string, string) {
	t.Helper()

	const assignmentMismatch = `fn main() {
    var count = 1;
    count = true;
}
`
	tempAssignmentMismatch := writeTempKizuSource(
		t,
		"frontend_assignment_type_mismatch.kizu",
		assignmentMismatch,
	)

	const immutableAssignment = `fn main() {
    let count = 1;
    count = 2;
}
`
	tempImmutableAssignment := writeTempKizuSource(
		t,
		"frontend_immutable_assignment.kizu",
		immutableAssignment,
	)

	const invalidAssignment = `fn main() {
    1 = 2;
}
`
	tempInvalidAssignment := writeTempKizuSource(
		t,
		"frontend_invalid_assignment_target.kizu",
		invalidAssignment,
	)

	return tempAssignmentMismatch, tempImmutableAssignment, tempInvalidAssignment
}

// writeSelfhostCLIReturnFrontendFixtures writes return-check semantic inputs.
func writeSelfhostCLIReturnFrontendFixtures(t *testing.T) (string, string, string, string, string) {
	t.Helper()

	const returnMismatch = `fn bad() -> i64 {
    return true;
}
`
	tempReturnMismatch := writeTempKizuSource(
		t,
		"frontend_return_type_mismatch.kizu",
		returnMismatch,
	)

	const returnMatchMismatch = `enum Flag {
    Yes,
    No,
}

fn choose(flag: Flag) -> i64 {
    return match flag {
        Yes => true,
        No => false,
    };
}
`
	tempReturnMatchMismatch := writeTempKizuSource(
		t,
		"frontend_return_match_type_mismatch.kizu",
		returnMatchMismatch,
	)

	const returningIf = `fn choose(ok: bool) -> i64 {
    if ok {
        return 1;
    } else {
        return 2;
    }
}
`
	tempReturningIf := writeTempKizuSource(t, "frontend_returning_if.kizu", returningIf)

	const missingReturn = `fn bad() -> i64 {
    1;
}
`
	tempMissingReturn := writeTempKizuSource(t, "frontend_missing_return.kizu", missingReturn)

	const ifMissingReturn = `fn bad(ok: bool) -> i64 {
    if ok {
        return 1;
    }
}
`
	tempIfMissingReturn := writeTempKizuSource(
		t,
		"frontend_if_missing_return.kizu",
		ifMissingReturn,
	)

	return tempReturnMismatch, tempReturnMatchMismatch, tempReturningIf,
		tempMissingReturn, tempIfMissingReturn
}

// writeSelfhostCLIMatchFrontendFixtures writes match-check semantic inputs.
func writeSelfhostCLIMatchFrontendFixtures(t *testing.T) string {
	t.Helper()

	const matchMissing = `enum Color {
    Red,
    Green,
}

fn choose(color: Color) -> bool {
    return match color {
        Red => true,
    };
}
`
	return writeTempKizuSource(t, "frontend_match_non_exhaustive.kizu", matchMissing)
}

// writeSelfhostCLIMoveFrontendFixtures writes move-check semantic inputs.
func writeSelfhostCLIMoveFrontendFixtures(t *testing.T) (string, string) {
	t.Helper()

	const movedSource = `struct User {
    value: []u8,
}

fn take(user: User) {
    print(user.value);
}

fn main() {
    let person = User { value: "alice" };
    take(person);
    print(person.value);
}
`
	tempMovedSource := writeTempKizuSource(t, "frontend_moved_record.kizu", movedSource)

	const movedValueSource = `struct Name {
    value: []u8,
}

fn take(name: Name) {
    print(name.value);
}

fn main() {
    let name = Name { value: "alice" };
    take(name);
    print(name.value);
}
`
	tempMovedValue := writeTempKizuSource(t, "frontend_moved_value.kizu", movedValueSource)

	return tempMovedSource, tempMovedValue
}

// writeSelfhostCLISymbolFrontendFixtures writes symbol and type-check inputs.
func writeSelfhostCLISymbolFrontendFixtures(
	t *testing.T,
) (string, string, string, string, string, string) {
	t.Helper()

	const unknownSource = `fn main() {
    missing_symbol();
}
`
	tempUnknownSource := writeTempKizuSource(t, "frontend_unknown_call.kizu", unknownSource)

	const unknownStdSource = `fn main() {
    std::testing::missing();
}
`
	tempUnknownStd := writeTempKizuSource(t, "frontend_unknown_std_call.kizu", unknownStdSource)

	const aritySource = `fn takes_one(value: i64) {
    print(value);
}

fn main() {
    takes_one(1, 2);
}
	`
	tempAritySource := writeTempKizuSource(t, "frontend_arity_mismatch.kizu", aritySource)

	const duplicateSource = `fn main() {
    print("one");
}

fn main() {
    print("two");
}
`
	tempDuplicate := writeTempKizuSource(t, "frontend_duplicate_declaration.kizu", duplicateSource)

	const typeAritySource = `fn main(values: std::map::Map<i64>) {
    print("bad");
}
	`
	tempTypeArity := writeTempKizuSource(t, "frontend_type_arity_mismatch.kizu", typeAritySource)

	const unknownTypeSource = `fn main(value: Missing) {
    print("bad");
}
`
	tempUnknownType := writeTempKizuSource(t, "frontend_unknown_type.kizu", unknownTypeSource)

	return tempUnknownSource, tempUnknownStd, tempAritySource, tempDuplicate, tempTypeArity,
		tempUnknownType
}

// writeSelfhostCLIInvalidFrontendFixtures writes invalid frontend inputs.
func writeSelfhostCLIInvalidFrontendFixtures(
	t *testing.T,
) (
	string, string, string, string, string,
	string, string, string, string, string,
	string, string, string, string, string, string,
) {
	t.Helper()

	tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr, tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam,
		tempInvalidReturn, tempMissingFnBody, tempInvalidImport :=
		writeSelfhostCLIInvalidGeneralFrontendFixtures(t)
	tempInvalidStruct, tempInvalidField, tempMissingColon, tempMissingType :=
		writeSelfhostCLIInvalidAggregateFrontendFixtures(t)

	return tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr, tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam,
		tempInvalidReturn, tempMissingFnBody, tempInvalidImport, tempInvalidStruct, tempInvalidField,
		tempMissingColon, tempMissingType
}

// writeSelfhostCLIInvalidGeneralFrontendFixtures writes common parse failures.
func writeSelfhostCLIInvalidGeneralFrontendFixtures(
	t *testing.T,
) (
	string, string, string, string, string,
	string, string, string, string, string, string, string,
) {
	t.Helper()

	tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr := writeSelfhostCLIInvalidSyntaxFrontendFixtures(t)
	tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam, tempInvalidReturn, tempMissingFnBody,
		tempInvalidImport := writeSelfhostCLIInvalidFunctionFrontendFixtures(t)

	return tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr, tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam,
		tempInvalidReturn, tempMissingFnBody, tempInvalidImport
}

// writeSelfhostCLIInvalidSyntaxFrontendFixtures writes syntax parse failures.
func writeSelfhostCLIInvalidSyntaxFrontendFixtures(
	t *testing.T,
) (string, string, string, string, string, string) {
	t.Helper()

	const missingExprSource = `fn main() { let value = ; }
`
	tempMissingExpr := writeTempKizuSource(t, "frontend_missing_expr.kizu", missingExprSource)

	const invalidSource = `fn main() {
    print("unterminated");
`
	tempInvalidSource := writeTempKizuSource(t, "frontend_missing_brace.kizu", invalidSource)

	const missingAssignSource = `fn main() {
    let value;
}
`
	tempMissingAssign := writeTempKizuSource(t, "frontend_missing_assign.kizu", missingAssignSource)

	const topLevelStmtSource = `let value = 1;
`
	tempTopLevelStmt := writeTempKizuSource(t, "frontend_top_level_stmt.kizu", topLevelStmtSource)

	const invalidTokenSource = `@
`
	tempInvalidToken := writeTempKizuSource(t, "frontend_invalid_token.kizu", invalidTokenSource)

	const invalidExprSource = `fn main() {
    print(@);
}
`
	tempInvalidExpr := writeTempKizuSource(t, "frontend_invalid_expr_token.kizu", invalidExprSource)

	return tempMissingExpr, tempInvalidSource, tempMissingAssign, tempTopLevelStmt, tempInvalidToken,
		tempInvalidExpr
}

// writeSelfhostCLIInvalidFunctionFrontendFixtures writes function parse failures.
func writeSelfhostCLIInvalidFunctionFrontendFixtures(
	t *testing.T,
) (string, string, string, string, string, string) {
	t.Helper()

	const invalidFnNameSource = `fn {}
`
	tempInvalidFnName := writeTempKizuSource(t, "frontend_invalid_fn_name.kizu", invalidFnNameSource)

	const invalidParamSource = `fn main(value) {
    return;
}
`
	tempInvalidParam := writeTempKizuSource(t, "frontend_invalid_fn_param.kizu", invalidParamSource)

	const invalidTypeParamSource = `fn main<>() {
    return;
}
`
	tempInvalidTypeParam := writeTempKizuSource(
		t,
		"frontend_invalid_fn_type_param.kizu",
		invalidTypeParamSource,
	)

	const invalidReturnSource = `fn main() -> {
    return;
}
`
	tempInvalidReturn := writeTempKizuSource(
		t,
		"frontend_invalid_fn_return_type.kizu",
		invalidReturnSource,
	)

	const missingFnBodySource = `fn main() ;
`
	tempMissingFnBody := writeTempKizuSource(t, "frontend_missing_fn_body.kizu", missingFnBodySource)

	const invalidImportSource = `import ;
`
	tempInvalidImport := writeTempKizuSource(t, "frontend_invalid_import.kizu", invalidImportSource)

	return tempInvalidFnName, tempInvalidParam, tempInvalidTypeParam, tempInvalidReturn,
		tempMissingFnBody, tempInvalidImport
}

// writeSelfhostCLIInvalidAggregateFrontendFixtures writes aggregate parse failures.
func writeSelfhostCLIInvalidAggregateFrontendFixtures(
	t *testing.T,
) (string, string, string, string) {
	t.Helper()

	const invalidStructSource = `struct {}
`
	tempInvalidStruct := writeTempKizuSource(t, "frontend_invalid_struct.kizu", invalidStructSource)

	const invalidFieldSource = `struct Name { : i64 }
`
	tempInvalidField := writeTempKizuSource(t, "frontend_invalid_field.kizu", invalidFieldSource)

	const missingColonSource = `struct Name { value }
`
	tempMissingColon := writeTempKizuSource(t, "frontend_missing_field_colon.kizu", missingColonSource)

	const missingTypeSource = `struct Name { value: }
`
	tempMissingType := writeTempKizuSource(t, "frontend_missing_field_type.kizu", missingTypeSource)

	return tempInvalidStruct, tempInvalidField, tempMissingColon, tempMissingType
}

// writeSelfhostCLIExpectFrontendFixtures writes std::testing frontend inputs.
func writeSelfhostCLIExpectFrontendFixtures(t *testing.T) (string, string) {
	t.Helper()

	const expectOKSource = `fn main() -> !void {
    std::testing::expect(true);
    return;
}
`
	tempExpectOK := writeTempKizuSource(t, "frontend_expect_ok.kizu", expectOKSource)

	const expectFailSource = `fn main() -> !void {
    std::testing::expect(false);
    return;
}
`
	tempExpectFail := writeTempKizuSource(t, "frontend_expect_failure.kizu", expectFailSource)

	return tempExpectOK, tempExpectFail
}

// writeTempKizuSource writes one temporary Kizu source fixture.
func writeTempKizuSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// runSelfhostCLIFileFrontendCase runs cli_gate while capturing stdout and stderr.
func runSelfhostCLIFileFrontendCase(program *ast.Program, args []string) (string, string, error) {
	if err := os.RemoveAll("target/selfhost/run"); err != nil {
		return "", "", err
	}
	if err := os.RemoveAll("target/selfhost/test"); err != nil {
		return "", "", err
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	oldStderr := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = oldStderr
		_ = reader.Close()
	}()

	var out bytes.Buffer
	runErr := interp.NewWithProcessArgs(&out, args).RunEntry(program, "selfhost::cli_gate")
	_ = writer.Close()
	stderrBytes, readErr := io.ReadAll(reader)
	if runErr != nil {
		return out.String(), string(stderrBytes), runErr
	}
	if readErr != nil {
		return out.String(), string(stderrBytes), readErr
	}
	return out.String(), string(stderrBytes), nil
}

// countSelfhostCLIGateFailures checks supported CLI commands and stage artifacts.
func countSelfhostCLIGateFailures(t *testing.T) int {
	t.Helper()
	failures := 0
	stageOut, err := runSelfhostCLIContractGate(t)
	if err != nil {
		t.Errorf("selfhost CLI contract failed: %v\n%s", err, stageOut)
		failures++
	} else {
		failures += countSelfhostCLICheckOutputFailures(t, stageOut)
		failures += countSelfhostCLIStageOutputFailures(t, stageOut)
		failures += countSelfhostCLIArtifactPresenceFailures(t)
	}
	return failures
}

// countSelfhostCLICheckOutputFailures validates user-visible check output.
func countSelfhostCLICheckOutputFailures(t *testing.T, out string) int {
	t.Helper()
	required := "check: ok\nexit-code\n0\n"
	if !strings.Contains(out, required) {
		t.Errorf("selfhost check CLI output missing %q:\n%s", required, out)
		return 1
	}
	return 0
}

// countSelfhostCLIStageOutputFailures validates user-visible stage output.
func countSelfhostCLIStageOutputFailures(t *testing.T, out string) int {
	t.Helper()
	required := strings.Join([]string{
		"stage: ok",
		"target/selfhost/selfhost.ll",
		"target/selfhost/selfhost.ll.meta",
		"target/selfhost/selfhost.storage.ll",
		"target/selfhost/selfhost.storage.ll.meta",
		"target/selfhost/selfhost.host.ll",
		"target/selfhost/selfhost.host.ll.meta",
		"exit-code",
		"0",
		"",
	}, "\n")
	if !strings.Contains(out, required) {
		t.Errorf("selfhost stage CLI output missing %q:\n%s", required, out)
		return 1
	}
	return 0
}

// countSelfhostCLIArtifactPresenceFailures checks the stage command wrote outputs.
func countSelfhostCLIArtifactPresenceFailures(t *testing.T) int {
	t.Helper()
	paths := []string{
		"../../target/selfhost/selfhost.ll",
		"../../target/selfhost/selfhost.ll.meta",
		"../../target/selfhost/selfhost.storage.ll",
		"../../target/selfhost/selfhost.storage.ll.meta",
		"../../target/selfhost/selfhost.host.ll",
		"../../target/selfhost/selfhost.host.ll.meta",
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("selfhost CLI artifact missing %s: %v", path, err)
			return 1
		}
		if info.Size() == 0 {
			t.Errorf("selfhost CLI artifact is empty: %s", path)
			return 1
		}
	}
	return 0
}

// runSelfhostCLIContractGate runs the one-pass selfhost CLI contract entry.
func runSelfhostCLIContractGate(t *testing.T) (string, error) {
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
	err = interp.New(&out).RunEntry(program, "selfhost::cli_contract_gate")
	return out.String(), err
}
