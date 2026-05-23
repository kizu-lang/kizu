package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfhostPackageSkeletonChecks keeps the source-owned selfhost layout valid.
func TestSelfhostPackageSkeletonChecks(t *testing.T) {
	runKizuOK(t, "check", "selfhost")
}

// TestSelfhostCheckPhasesUseParserFacade keeps parse ownership in selfhost::parser.
func TestSelfhostCheckPhasesUseParserFacade(t *testing.T) {
	paths := []string{
		"../../selfhost/src/resolver.kizu",
		"../../selfhost/src/types/checker.kizu",
		"../../selfhost/src/ownership/checker.kizu",
	}
	for _, path := range paths {
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Clean(path), err)
		}
		if strings.Contains(string(bytes), "std::kizu::parser::parse_program") {
			t.Fatalf("%s bypasses selfhost::parser facade", filepath.Clean(path))
		}
	}
}

// TestSelfhostParserFacadeValidatesCheckedFiles fixes the checked CLI parse path.
func TestSelfhostParserFacadeValidatesCheckedFiles(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/parser.kizu")
	if err != nil {
		t.Fatalf("read selfhost parser: %v", err)
	}
	content := string(bytes)
	required := []string{
		"pub fn parse_file(",
		"pub fn parse_checked_file(",
		"pub fn parse_diagnostic_file(",
		"var tokens = try lexer::tokenize(allocator, source);",
		"defer tokens.deinit();",
		"let validation_ok = try validation::validate_tokens_ok(allocator, source, &tokens);",
		"let validation_result = try validation::validate_tokens(allocator, source, &tokens);",
		"if !validation_ok {",
		"if !validation_result.ok {",
		"let token_check = require_checked_tokens(&tokens);",
		"try token_check;",
		"var parsed = try parse_program_file(allocator, path, source);",
		"parsed.deinit();",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost parser checked path missing %q", fragment)
		}
	}
	if strings.Contains(content, "return try validation::validate_source(allocator, source);") {
		t.Fatal("selfhost parser diagnostic path retokenizes validation failures")
	}
}

// TestSelfhostParserFacadeRequiresExplicitPath keeps parse entry points file-owned.
func TestSelfhostParserFacadeRequiresExplicitPath(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/parser.kizu")
	if err != nil {
		t.Fatalf("read selfhost parser: %v", err)
	}
	content := string(bytes)
	forbidden := []string{
		"pub fn parse_tokens(",
		"pub fn parse_source(",
		"\"<memory>\"",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost parser keeps pathless parse entry %q", fragment)
		}
	}
}

// TestSelfhostParserSummaryUsesParsedAST rejects caller-supplied AST summaries.
func TestSelfhostParserSummaryUsesParsedAST(t *testing.T) {
	parserBytes, err := os.ReadFile("../../selfhost/src/parser.kizu")
	if err != nil {
		t.Fatalf("read selfhost parser: %v", err)
	}
	astBytes, err := os.ReadFile("../../selfhost/src/ast.kizu")
	if err != nil {
		t.Fatalf("read selfhost ast: %v", err)
	}
	oracleBytes, err := os.ReadFile("../../selfhost/src/parser_oracle.kizu")
	if err != nil {
		t.Fatalf("read selfhost parser oracle: %v", err)
	}
	parserContent := string(parserBytes)
	astContent := string(astBytes)
	oracleContent := string(oracleBytes)
	forbidden := []string{
		"declarations: i64",
		"parser::summarize(result, token_count, 2)",
	}
	for _, fragment := range forbidden {
		if strings.Contains(parserContent+oracleContent, fragment) {
			t.Fatalf("selfhost parser summary keeps caller-provided %q", fragment)
		}
	}
	required := []string{
		"summarize_parse_result(result, tokens, 0)",
		"fn node_count(",
		"Program(program) => program.declarations.len",
	}
	for _, fragment := range required {
		if !strings.Contains(parserContent+astContent, fragment) {
			t.Fatalf("selfhost parser summary missing %q", fragment)
		}
	}
}

// TestSelfhostFunctionCallDiagnosticsUseSourcePath keeps file selection path-owned.
func TestSelfhostFunctionCallDiagnosticsUseSourcePath(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/function_calls.kizu")
	if err != nil {
		t.Fatalf("read selfhost function calls: %v", err)
	}
	content := string(bytes)
	if strings.Contains(content, "std::mem::equal_bytes(file.text, source_text)") {
		t.Fatal("function call diagnostics select files by source bytes")
	}
	if !strings.Contains(content, "std::mem::equal_bytes(file.path, target_path)") {
		t.Fatal("function call diagnostics do not select files by source path")
	}
}

// TestSelfhostPackageCallDiagnosticsBorrowAST keeps target parsing single-pass.
func TestSelfhostPackageCallDiagnosticsBorrowAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/function_calls.kizu")
	if err != nil {
		t.Fatalf("read selfhost function calls: %v", err)
	}
	content := string(bytes)
	required := []string{
		"collect_referenced_package_call_modules(",
		"collect_other_package_function_arities_for_modules_from_ast(",
		"PackageModuleRef",
		"ast: &std::kizu::ast::Ast,",
		"ast_ref_node_text(",
		"&result.ast",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost package call diagnostics missing %q", fragment)
		}
	}
	forbidden := []string{
		"source_function_call_error(",
		"source_local_function_call_error(",
		"function_call_error_at(",
		"local_function_call_error_at(",
		"FunctionCallCandidate",
		"collect_package_function_call_candidates(",
		"first_function_call_error_in_candidates(",
		"collect_other_package_function_arities_from_ast(",
		"has_package_function_call(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost package call diagnostics keep removed path %q", fragment)
		}
	}
	body := selfhostKizuFunctionBody(t, content, "pub fn first_function_call_error(")
	parseCall := "parser::parse_checked_file(allocator, file.path, file.text)"
	if count := strings.Count(body, parseCall); count != 1 {
		t.Fatalf("first_function_call_error parses target %d times, want 1", count)
	}
	if !strings.Contains(body, "collect_function_arities_from_ast(") {
		t.Fatal("first_function_call_error does not collect target arity from the parsed target AST")
	}
}

// TestSelfhostTypeLocalsUseParsedAST rejects raw body text scans for function locals.
func TestSelfhostTypeLocalsUseParsedAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	content := string(bytes)
	forbidden := []string{
		"let body_text = ast_node_text(file, ast, body)",
		"local_initializer_end(",
		"infer_initializer_text_type(",
		"infer_expression_text_type(",
		"namespace_value_type_text(",
		"expression_callee_text(",
		"cast_expression_text_type(",
		"field_text_type(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost type local collection keeps raw text scan %q", fragment)
		}
	}
	required := []string{
		"try collect_param_local(file, ast, child, local_types, local_mutability);",
		"collect_statement_locals_from_node(",
		"collect_let_statement_local(",
		"let type_name = try infer_expression_type(",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost type local collection missing %q", fragment)
		}
	}
}

// TestSelfhostArgumentTypesUseParsedParams rejects param-source re-lexing.
func TestSelfhostArgumentTypesUseParsedParams(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	content := string(bytes)
	forbidden := []string{
		"collect_local_function_param_sources(",
		"function_param_type_at(",
		"function_param_range_type(",
		"let param_source = try function_param_sources.get(callee_text)",
		"append_function_param_key(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost argument type check keeps param text scan %q", fragment)
		}
	}
	required := []string{
		"collect_local_function_param_types(",
		"collect_function_param_type(",
		"collect_function_param_node_type(",
		"std::array::Array<FunctionParamType>",
		"lookup_function_param_type(",
		"ast_return_type_text(file, ast, type_node)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost argument type check missing %q", fragment)
		}
	}
}

// TestSelfhostSemanticDiagnosticsCollectReturnsFromParsedAST rejects return-type token scans.
func TestSelfhostSemanticDiagnosticsCollectReturnsFromParsedAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	content := string(bytes)
	required := []string{
		"collect_function_returns_from_ast(",
		"FnDecl(fn_decl) => return collect_function_return(",
		"ast_return_type_text(file, ast, return_type)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost semantic diagnostics missing %q", fragment)
		}
	}
	preBody := selfhostKizuFunctionBody(t, content, "pub fn first_pre_move_check_diagnostic(")
	postBody := selfhostKizuFunctionBody(t, content, "pub fn first_post_move_check_diagnostic(")
	if !strings.Contains(preBody, "parser::parse_checked_file(allocator, path, text)") {
		t.Fatal("pre-move diagnostic pass does not parse checked AST")
	}
	if !strings.Contains(postBody, "parser::parse_checked_file(allocator, path, text)") {
		t.Fatal("post-move diagnostic pass does not parse checked AST")
	}
	if !strings.Contains(preBody+postBody, "collect_function_returns_from_ast(") {
		t.Fatal("shared diagnostic passes do not collect function returns from AST")
	}
	oldEntries := []string{
		"pub fn first_argument_type_mismatch(",
		"pub fn first_assignment_type_mismatch(",
		"pub fn first_immutable_assignment(",
		"pub fn first_undefined_variable(",
		"pub fn first_return_type_mismatch(",
		"pub fn first_match_diagnostic(",
		"pub fn first_type_reference_error(",
		"pub fn first_user_function_call_error(",
	}
	for _, fragment := range oldEntries {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost types keeps per-diagnostic public entry %q", fragment)
		}
	}
}

// TestSelfhostCheckEntryRunsPackageCallDiagnostics uses the package source path directly.
func TestSelfhostCheckEntryRunsPackageCallDiagnostics(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/cli/check.kizu")
	if err != nil {
		t.Fatalf("read selfhost cli check: %v", err)
	}
	content := string(bytes)
	if strings.Contains(content, "source_has_qualified_name") {
		t.Fatal("check entry gates package call diagnostics on raw source content")
	}
	if strings.Contains(content, "source_has_package_function_call(") {
		t.Fatal("check entry keeps a separate package-call prefilter")
	}
	required := []string{
		"var files = try source::load_file_sources(allocator, io, path, file_text)",
		"types::first_function_call_error(allocator, files, path)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("check entry package call diagnostic missing %q", fragment)
		}
	}
	call := "write_package_function_call_diagnostic(allocator, io, path, file_text)"
	if !strings.Contains(content, call) {
		t.Fatal("check entry does not run package call diagnostics")
	}
}

// TestSelfhostCheckEntrySharesDiagnosticPasses keeps per-file checks grouped by phase.
func TestSelfhostCheckEntrySharesDiagnosticPasses(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/cli/check.kizu")
	if err != nil {
		t.Fatalf("read selfhost cli check: %v", err)
	}
	content := string(bytes)
	body := selfhostKizuFunctionBody(t, content, "pub fn fast_diagnostics(")
	required := []string{
		"types::first_pre_move_check_diagnostic(allocator, path, file_text)",
		"types::first_post_move_check_diagnostic(allocator, path, file_text)",
		"ownership::first_use_after_move_name(allocator, path, file_text)",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("fast_diagnostics missing shared phase %q", fragment)
		}
	}
	forbidden := []string{
		"write_type_reference_diagnostic(",
		"write_match_diagnostic(",
		"write_return_type_diagnostic(",
		"types::first_user_function_call_error(",
		"write_undefined_variable_diagnostic(",
		"write_argument_type_diagnostic(",
		"write_immutable_assignment_diagnostic(",
		"write_invalid_assignment_target_diagnostic(",
		"write_assignment_type_diagnostic(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(body, fragment) {
			t.Fatalf("fast_diagnostics keeps per-diagnostic call %q", fragment)
		}
	}
	oldWrappers := []string{
		"fn write_return_type_diagnostic(",
		"fn write_argument_type_diagnostic(",
		"fn write_undefined_variable_diagnostic(",
		"fn write_immutable_assignment_diagnostic(",
		"fn write_invalid_assignment_target_diagnostic(",
		"fn write_assignment_type_diagnostic(",
		"fn write_type_reference_diagnostic(",
		"fn write_match_diagnostic(",
	}
	for _, fragment := range oldWrappers {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost cli check keeps unused diagnostic wrapper %q", fragment)
		}
	}
}

// TestSelfhostTypeCheckSkipsStdDiagnosticPass keeps std as declarations-only for user checks.
func TestSelfhostTypeCheckSkipsStdDiagnosticPass(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	content := string(bytes)
	body := selfhostKizuFunctionBody(t, content, "pub fn check_sources(")
	if !strings.Contains(body, "if source::is_frontend_source(file.kind) {") {
		t.Fatal("type checker second pass does not limit diagnostics to frontend sources")
	}
	if count := strings.Count(body, "if source::is_source_code(file.kind) {"); count != 1 {
		t.Fatalf("type checker has %d source-code passes, want declarations-only pass", count)
	}
}

// TestSelfhostPackageAritySelectionUsesModulePaths rejects hardcoded std module IDs.
func TestSelfhostPackageAritySelectionUsesModulePaths(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/function_calls.kizu")
	if err != nil {
		t.Fatalf("read selfhost function calls: %v", err)
	}
	content := string(bytes)
	forbidden := []string{
		"std_package_module_id(",
		"std_package_module_matches_file(",
		"module == 1000",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("package arity selection keeps hardcoded module path %q", fragment)
		}
	}
	required := []string{
		"struct PackageModuleRef",
		"package_module_matches_path(",
		"source::module_path(file)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("package arity selection missing %q", fragment)
		}
	}
}

// TestSelfhostFunctionCallsResolveImportAliases keeps qualified calls import-owned.
func TestSelfhostFunctionCallsResolveImportAliases(t *testing.T) {
	functionCalls := readSelfhostFile(t, "../../selfhost/src/types/function_calls.kizu")
	checkCLI := readSelfhostFile(t, "../../selfhost/src/cli/check.kizu")
	required := []string{
		"var import_alias_starts = std::map::Map<[]u8, i64>(allocator)",
		"var import_alias_ends = std::map::Map<[]u8, i64>(allocator)",
		"collect_import_aliases(",
		"first_qualified_segment_end(name)",
		"if import_alias_starts.contains(alias) {",
		"file.text[module_start..module_end]",
		"try output::stderr_newline(allocator, io);",
	}
	content := functionCalls + checkCLI
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost import alias call resolution missing %q", fragment)
		}
	}
	if strings.Contains(checkCLI, "var newline = std::string::String(allocator)") {
		t.Fatal("missing-return diagnostic keeps manual newline after alias resolution")
	}
}

// TestSelfhostTypeCheckerNormalizesBorrowProvenance keeps borrowed returns type-owned.
func TestSelfhostTypeCheckerNormalizesBorrowProvenance(t *testing.T) {
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	required := []string{
		"fn value_type_text(type_name: []u8) -> []u8",
		"let marker = \" borrows \";",
		"let expected_type = value_type_text(expected);",
		"let actual_type = value_type_text(actual);",
		"std::mem::equal_bytes(value_type_text(type_name), \"[]u8\")",
	}
	for _, fragment := range required {
		if !strings.Contains(checker, fragment) {
			t.Fatalf("selfhost type checker borrow provenance normalization missing %q", fragment)
		}
	}
	if strings.Contains(checker, "debug-") {
		t.Fatal("selfhost type checker keeps temporary debug output")
	}
	body := selfhostKizuFunctionBody(t, checker, "fn known_type_mismatch(")
	if !strings.Contains(body, "return !std::mem::equal_bytes(expected_type, actual_type);") {
		t.Fatal("selfhost type mismatch comparison does not use normalized type text")
	}
}

// TestSelfhostFrontendResponsibilitiesStaySplit keeps frontend boundaries split.
func TestSelfhostFrontendResponsibilitiesStaySplit(t *testing.T) {
	assertSelfhostSplitFiles(t)
	assertSelfhostRootOmitsResponsibilities(t)
}

var selfhostSplitFileExpectations = map[string][]string{
	"../../selfhost/src/types/model.kizu": {
		"pub enum TypeKind",
		"pub struct CheckDiagnosticSummary",
	},
	"../../selfhost/src/types/diagnostic.kizu": {
		"pub fn unknown_type(",
		"pub fn non_exhaustive_match(",
	},
	"../../selfhost/src/types/symbols.kizu": {
		"pub fn declare_type(",
		"pub fn lookup_kind(",
	},
	"../../selfhost/src/types/checker.kizu": {
		"pub fn check_sources(",
		"pub fn first_pre_move_check_diagnostic(",
	},
	"../../selfhost/src/types/function_calls.kizu": {
		"pub fn first_function_call_error(",
		"pub fn check_file_function_references(",
	},
	"../../selfhost/src/types/body_scan.kizu": {
		"pub fn scan_body_ast_node(",
		"fn scan_body_child_range(",
	},
	"../../selfhost/src/types/type_refs.kizu": {
		"pub fn check_file_type_references(",
		"pub fn first_type_error_in_file(",
	},
	"../../selfhost/src/ownership/data.kizu": {
		"pub enum ResourceKind",
		"pub struct CheckedPackage",
	},
	"../../selfhost/src/ownership/state.kizu": {
		"pub fn declare_resource(",
		"pub fn move_value(",
	},
	"../../selfhost/src/ownership/diagnostic.kizu": {
		"pub fn use_after_move(",
		"pub fn deinit_while_borrowed(",
	},
	"../../selfhost/src/ownership/checker.kizu": {
		"pub fn check_package(",
		"fn check_ownership_ast_node(",
	},
	"../../selfhost/src/ownership/kind.kizu": {
		"pub fn initializer_resource_kind(",
		"pub fn ast_node_text(",
	},
	"../../selfhost/src/ownership/move_diagnostic.kizu": {
		"pub fn first_use_after_move_name(",
		"fn first_use_after_move_ast_node(",
	},
	"../../selfhost/src/backend/runtime.kizu": {
		"pub fn load_storage_module(",
		"pub fn render_host_metadata(",
	},
	"../../selfhost/src/backend/hosted.kizu": {
		"pub fn emit_run_hello_artifact(",
		"fn render_test_expect_ok_llvm(",
	},
	"../../selfhost/src/backend/llvm.kizu": {
		"pub fn emit_llvm_artifact(",
		"fn render_llvm_module(",
	},
	"../../selfhost/src/backend/cli_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_functions(",
	},
	"../../selfhost/src/backend/cli_parse_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_cli_parse_blocks(",
		"pub fn append_cli_fmt_blocks(",
		"fn append_parse_format_write_function(",
	},
	"../../selfhost/src/backend/cli_parse_diag_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_functions(",
		"fn append_parse_missing_assign_index_function(",
	},
	"../../selfhost/src/cli/check.kizu": {
		"pub fn file_cli(",
		"pub fn fast_diagnostics(",
	},
}

// assertSelfhostSplitFiles checks the expected responsibility modules exist.
func assertSelfhostSplitFiles(t *testing.T) {
	t.Helper()
	for path, required := range selfhostSplitFileExpectations {
		content := readSelfhostFile(t, path)
		for _, fragment := range required {
			if !strings.Contains(content, fragment) {
				t.Fatalf("%s missing split responsibility %q", filepath.Clean(path), fragment)
			}
		}
	}
}

// assertSelfhostRootOmitsResponsibilities keeps split code out of root modules.
func assertSelfhostRootOmitsResponsibilities(t *testing.T) {
	t.Helper()
	rootTypes := readSelfhostFile(t, "../../selfhost/src/types.kizu")
	rootOwnership := readSelfhostFile(t, "../../selfhost/src/ownership.kizu")
	rootMain := readSelfhostFile(t, "../../selfhost/src/main.kizu")
	rootBackend := readSelfhostFile(t, "../../selfhost/src/backend.kizu")
	forbidden := map[string][]string{
		"types.kizu": {
			"pub enum TypeKind",
			"pub fn unknown_type(",
			"pub fn declare_type(",
			"fn first_function_call_error_ast_node(",
		},
		"ownership.kizu": {
			"pub enum ResourceKind",
			"pub fn declare_resource(",
			"pub fn use_after_move(",
			"fn first_use_after_move_ast_node(",
			"fn check_ownership_ast_node(",
			"fn resource_kind_for_text(",
		},
		"main.kizu": {
			"pub fn fast_diagnostics(",
			"fn write_check_diagnostic(",
		},
		"backend.kizu": {
			"fn render_llvm_module(",
			"fn append_cli_globals(",
			"fn render_run_hello_llvm(",
			"fn render_test_expect_ok_llvm(",
		},
	}
	contents := map[string]string{
		"types.kizu":     rootTypes,
		"ownership.kizu": rootOwnership,
		"main.kizu":      rootMain,
		"backend.kizu":   rootBackend,
	}
	for name, fragments := range forbidden {
		for _, fragment := range fragments {
			if strings.Contains(contents[name], fragment) {
				t.Fatalf("%s keeps split responsibility %q", name, fragment)
			}
		}
	}
}

// readSelfhostFile reads a selfhost source file for structural assertions.
func readSelfhostFile(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Clean(path), err)
	}
	return string(bytes)
}

// selfhostKizuFunctionBody extracts a simple Kizu function body for structural checks.
func selfhostKizuFunctionBody(t *testing.T, content string, signature string) string {
	t.Helper()
	start := strings.Index(content, signature)
	if start < 0 {
		t.Fatalf("missing Kizu function %q", signature)
	}
	open := strings.Index(content[start:], "{")
	if open < 0 {
		t.Fatalf("missing body for Kizu function %q", signature)
	}
	bodyStart := start + open
	depth := 0
	for index := bodyStart; index < len(content); index++ {
		switch content[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[bodyStart : index+1]
			}
		}
	}
	t.Fatalf("unterminated body for Kizu function %q", signature)
	return ""
}

// TestSelfhostCLIParseUsesParserDiagnosticFacade keeps validation owned by parser.
func TestSelfhostCLIParseUsesParserDiagnosticFacade(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/main.kizu")
	if err != nil {
		t.Fatalf("read selfhost main: %v", err)
	}
	content := string(bytes)
	if strings.Contains(content, "validation::validate_source(allocator, file_text)") {
		t.Fatal("CLI entry validates source outside parser facade")
	}
	required := []string{
		"parser::parse_diagnostic_file(allocator, path, file_text)",
		"if !parsed_validation.ok {",
		"return try write_parse_failure(allocator, io, parsed_validation);",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("CLI parse path missing %q", fragment)
		}
	}
}

// TestSelfhostMoveDiagnosticsUseSourcePath keeps move diagnostics file-owned.
func TestSelfhostMoveDiagnosticsUseSourcePath(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/ownership/move_diagnostic.kizu")
	if err != nil {
		t.Fatalf("read selfhost move diagnostic: %v", err)
	}
	content := string(bytes)
	required := []string{
		"pub fn first_use_after_move_name(",
		"path: []u8,",
		"kind: source::SourceKind::User,",
		"path: path,",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("move diagnostics missing %q", fragment)
		}
	}
}
