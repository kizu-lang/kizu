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
		"../../selfhost/src/types.kizu",
		"../../selfhost/src/ownership.kizu",
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
	bytes, err := os.ReadFile("../../selfhost/src/types.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
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
	bytes, err := os.ReadFile("../../selfhost/src/types.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	content := string(bytes)
	required := []string{
		"has_package_function_call(",
		"collect_other_package_function_arities_from_ast(",
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
	otherBody := selfhostKizuFunctionBody(
		t,
		content,
		"fn collect_other_package_function_arities_from_ast(",
	)
	if !strings.Contains(otherBody, "!std::mem::equal_bytes(file.path, target_path)") {
		t.Fatal("package arity collection does not skip the already parsed target file")
	}
}

// TestSelfhostTypeLocalsUseParsedAST rejects raw body text scans for function locals.
func TestSelfhostTypeLocalsUseParsedAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types.kizu")
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
	bytes, err := os.ReadFile("../../selfhost/src/types.kizu")
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
	bytes, err := os.ReadFile("../../selfhost/src/types.kizu")
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
	entries := []string{
		"first_argument_type_mismatch",
		"first_assignment_type_mismatch",
		"first_immutable_assignment",
		"first_undefined_variable",
		"first_return_type_mismatch",
		"first_match_diagnostic",
	}
	for _, name := range entries {
		body := selfhostKizuFunctionBody(t, content, "pub fn "+name+"(")
		forbidden := []string{
			"lexer::tokenize(allocator, text)",
			"collect_function_signatures(",
		}
		for _, fragment := range forbidden {
			if strings.Contains(body, fragment) {
				t.Fatalf("%s keeps signature token scan %q", name, fragment)
			}
		}
		if !strings.Contains(body, "parser::parse_checked_file(allocator, path, text)") {
			t.Fatalf("%s does not parse checked AST", name)
		}
		if !strings.Contains(body, "collect_function_returns_from_ast(") {
			t.Fatalf("%s does not collect function returns from AST", name)
		}
	}
}

// TestSelfhostCheckEntryRunsPackageCallDiagnostics rejects raw source prefilters.
func TestSelfhostCheckEntryRunsPackageCallDiagnostics(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/main.kizu")
	if err != nil {
		t.Fatalf("read selfhost main: %v", err)
	}
	content := string(bytes)
	if strings.Contains(content, "source_has_qualified_name") {
		t.Fatal("check entry gates package call diagnostics on raw source content")
	}
	call := "write_package_function_call_diagnostic(allocator, io, path, file_text)"
	if !strings.Contains(content, call) {
		t.Fatal("check entry does not run package call diagnostics")
	}
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
		"return try write_check_parse_failure(allocator, io, parsed_validation);",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("CLI parse path missing %q", fragment)
		}
	}
}

// TestSelfhostMoveDiagnosticsUseSourcePath keeps move diagnostics file-owned.
func TestSelfhostMoveDiagnosticsUseSourcePath(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/ownership.kizu")
	if err != nil {
		t.Fatalf("read selfhost ownership: %v", err)
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
