package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfhostPackageSkeletonChecks keeps the source-owned selfhost layout valid.
func TestSelfhostPackageSkeletonChecks(t *testing.T) {
	runReferenceCheckOK(t, "selfhost")
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
		if strings.Contains(string(bytes), "pub fn first_duplicate_declaration(") {
			t.Fatalf("%s keeps duplicate declaration path/text wrapper", filepath.Clean(path))
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
		"pub fn parse_validated_file(",
		"validation_ok: bool",
		"if !validation_ok {",
		"var tokens = try lexer::tokenize(allocator, source);",
		"defer tokens.deinit();",
		"let validation_ok = try validation::validate_tokens_ok(allocator, source, &tokens);",
		"let validation_result = try validation::validate_tokens(allocator, source, &tokens);",
		"if !validation_ok {",
		"if !validation_result.ok {",
		"let token_check = require_checked_tokens(&tokens);",
		"try token_check;",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost parser checked path missing %q", fragment)
		}
	}
	if strings.Contains(content, "pub fn parse_diagnostic_file(") {
		t.Fatal("selfhost parser keeps AST-discarding diagnostic parse wrapper")
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

// TestSelfhostFunctionCallDiagnosticsUseASTEntry keeps diagnostics off source-text wrappers.
func TestSelfhostFunctionCallDiagnosticsUseASTEntry(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/function_calls.kizu")
	if err != nil {
		t.Fatalf("read selfhost function calls: %v", err)
	}
	content := string(bytes)
	if strings.Contains(content, "std::mem::equal_bytes(file.text, source_text)") {
		t.Fatal("function call diagnostics select files by source bytes")
	}
	if strings.Contains(content, "target_path") ||
		strings.Contains(content, "pub fn first_function_call_error(") {
		t.Fatal("function call diagnostics keep path-based target wrapper")
	}
	required := []string{
		"pub fn first_package_function_call_error_ast_node(",
		"collect_other_package_function_arities_for_modules_from_ast(",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("function call diagnostics missing AST entry fragment %q", fragment)
		}
	}
	if !strings.Contains(content, "return first_qualified_segment_end(name) < std::mem::len(name);") {
		t.Fatal("function call diagnostics do not reuse qualified segment scanning")
	}
	if strings.Contains(content, "fn bytes_contains(") {
		t.Fatal("function call diagnostics keep local byte-contains helper")
	}
}

// TestSelfhostTypeReferenceDiagnosticsAvoidDeadByteContains keeps type refs lean.
func TestSelfhostTypeReferenceDiagnosticsAvoidDeadByteContains(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/types/type_refs.kizu")
	if strings.Contains(content, "fn bytes_contains(") {
		t.Fatal("type reference diagnostics keep unused byte-contains helper")
	}
}

// TestSelfhostTypeDeclarationsUseParsedAST keeps declaration collection on the parsed tree.
func TestSelfhostTypeDeclarationsUseParsedAST(t *testing.T) {
	typeRefs := readSelfhostFile(t, "../../selfhost/src/types/type_refs.kizu")
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	requiredTypeRefs := []string{
		"pub fn collect_declared_types_from_ast(",
		"Program(program) => return collect_declared_types_in_range(",
		"StructDecl(struct_decl) => return collect_declared_type_from_ast(",
		"EnumDecl(enum_decl) => return collect_declared_type_from_ast(",
		"UnionDecl(union_decl) => return collect_declared_type_from_ast(",
		"pub fn collect_type_parameters_from_ast(",
		"fn ast_node_text(",
	}
	for _, fragment := range requiredTypeRefs {
		if !strings.Contains(typeRefs, fragment) {
			t.Fatalf("selfhost type declaration AST collection missing %q", fragment)
		}
	}
	forbiddenTypeRefs := []string{
		"pub fn collect_declared_types(",
		"pub fn collect_type_parameters(",
		"fn collect_type_parameters_at(",
		"fn type_decl_arity(",
	}
	for _, fragment := range forbiddenTypeRefs {
		if strings.Contains(typeRefs, fragment) {
			t.Fatalf("selfhost type declaration collection keeps token path %q", fragment)
		}
	}
	requiredChecker := []string{
		"parser::parse_checked_file(allocator, file.path, file.text)",
		"type_refs::collect_declared_types_from_ast(",
		"type_refs::collect_type_parameters_from_ast(",
	}
	for _, fragment := range requiredChecker {
		if !strings.Contains(checker, fragment) {
			t.Fatalf("selfhost checker does not use AST type declaration collection %q", fragment)
		}
	}
	forbiddenChecker := []string{
		"type_refs::collect_declared_types(allocator, file, tokens",
		"type_refs::collect_type_parameters(file, tokens",
	}
	for _, fragment := range forbiddenChecker {
		if strings.Contains(checker, fragment) {
			t.Fatalf("selfhost checker keeps token type declaration collection %q", fragment)
		}
	}
}

// TestSelfhostFunctionSignaturesUseParsedAST keeps the first type pass on the parsed tree.
func TestSelfhostFunctionSignaturesUseParsedAST(t *testing.T) {
	functionCalls := readSelfhostFile(t, "../../selfhost/src/types/function_calls.kizu")
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	requiredFunctionCalls := []string{
		"pub fn collect_function_signatures_from_ast(",
		"Program(program) => return collect_function_signatures_in_range_from_ast(",
		"FnDecl(fn_decl) => return collect_function_signature_from_ast(",
		"fn ast_return_type(",
	}
	for _, fragment := range requiredFunctionCalls {
		if !strings.Contains(functionCalls, fragment) {
			t.Fatalf("selfhost function signature AST collection missing %q", fragment)
		}
	}
	forbiddenFunctionCalls := []string{
		"fn collect_package_function_signatures(",
		"pub fn collect_function_signatures(",
		"fn function_param_open(",
		"fn function_param_source(",
		"fn function_return_type(",
		"fn function_public_at(",
		"fn ast_param_source(",
	}
	for _, fragment := range forbiddenFunctionCalls {
		if strings.Contains(functionCalls, fragment) {
			t.Fatalf("selfhost function signature collection keeps token path %q", fragment)
		}
	}
	if !strings.Contains(checker, "function_calls::collect_function_signatures_from_ast(") {
		t.Fatal("selfhost checker does not use AST function signature collection")
	}
	if strings.Contains(checker, "function_calls::collect_function_signatures(") {
		t.Fatal("selfhost checker keeps token function signature collection")
	}
}

// TestSelfhostFirstTypeReferenceDiagnosticUsesParsedAST keeps the AST check entry parsed.
func TestSelfhostFirstTypeReferenceDiagnosticUsesParsedAST(t *testing.T) {
	typeRefs := readSelfhostFile(t, "../../selfhost/src/types/type_ref_ast.kizu")
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	requiredTypeRefs := []string{
		"pub fn first_type_error_in_ast(",
		"fn first_type_error_in_type_node_ast(",
		"fn first_type_error_in_type_text(",
		"fn observed_type_arity_text(",
	}
	for _, fragment := range requiredTypeRefs {
		if !strings.Contains(typeRefs, fragment) {
			t.Fatalf("selfhost type reference AST diagnostic missing %q", fragment)
		}
	}
	forbiddenTypeRefs := []string{
		"pub fn first_type_error_in_file(",
		"fn first_type_error_in_range(",
		"fn first_type_error_in_arguments(",
		"fn type_error_at(",
	}
	for _, fragment := range forbiddenTypeRefs {
		if strings.Contains(typeRefs, fragment) {
			t.Fatalf("selfhost type reference diagnostics keep token path %q", fragment)
		}
	}
	preASTBody := selfhostKizuFunctionBody(
		t,
		checker,
		"pub fn first_pre_move_check_diagnostic_ast_node(",
	) + selfhostKizuFunctionBody(
		t,
		checker,
		"pub fn first_pre_move_check_diagnostic_ast_node_with_types(",
	)
	if !strings.Contains(preASTBody, "type_ref_ast::first_type_error_in_ast(") {
		t.Fatal("pre-move AST diagnostic entry does not use AST type reference diagnostics")
	}
	forbiddenChecker := []string{
		"lexer::tokenize(allocator, file.text)",
		"type_refs::first_type_error_in_file(",
	}
	for _, fragment := range forbiddenChecker {
		if strings.Contains(preASTBody, fragment) {
			t.Fatalf("pre-move AST diagnostic entry keeps token type reference path %q", fragment)
		}
	}
}

// TestSelfhostTypeReferenceSummaryUsesParsedAST keeps package summaries AST-owned.
func TestSelfhostTypeReferenceSummaryUsesParsedAST(t *testing.T) {
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	typeRefs := readSelfhostFile(t, "../../selfhost/src/types/type_refs.kizu")
	typeRefAST := readSelfhostFile(t, "../../selfhost/src/types/type_ref_ast.kizu")
	scan := readSelfhostFile(t, "../../selfhost/src/types/type_ref_scan_ast.kizu")
	required := []string{
		"type_ref_scan_ast::check_file_type_references_from_ast(",
		"pub fn check_file_type_references_from_ast(",
		"type_ref_ast::type_error_for_name_text_with_imports(",
		"pub fn type_error_for_name_text(",
	}
	content := checker + typeRefs + typeRefAST + scan
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost type reference AST scan missing %q", fragment)
		}
	}
	forbidden := []string{
		"type_refs::check_file_type_references(",
		"pub fn check_file_type_references(",
		"std::array::Array<std::kizu::lexer::Token>",
		"fn scan_type_range(",
		"fn scan_type_arguments(",
		"fn qualified_path_end(",
	}
	typeSummaryContent := checker + typeRefs + scan
	for _, fragment := range forbidden {
		if strings.Contains(typeSummaryContent, fragment) {
			t.Fatalf("selfhost type reference scan keeps token path %q", fragment)
		}
	}
	checkSourcesBody := selfhostKizuFunctionBody(t, checker, "pub fn check_sources(")
	if strings.Contains(checkSourcesBody, "lexer::tokenize(allocator, file.text)") {
		t.Fatal("check_sources still tokenizes source for type reference summary")
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
		"collect_import_aliases_from_ast(",
		"collect_referenced_package_call_modules_from_ast(",
		"collect_other_package_function_arities_for_modules_from_ast(",
		"pub fn first_package_function_call_error_ast_node(",
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
		"fn collect_referenced_package_call_modules(",
		"collect_other_package_function_arities_from_ast(",
		"has_package_function_call(",
		"pub fn first_function_call_error(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("selfhost package call diagnostics keep removed path %q", fragment)
		}
	}
	astBody := selfhostKizuFunctionBody(
		t,
		content,
		"pub fn first_package_function_call_error_ast_node(",
	)
	if !strings.Contains(astBody, "collect_function_arities_from_ast(") {
		t.Fatal("package call diagnostics do not collect target arity from the parsed target AST")
	}
	if strings.Contains(astBody, "lexer::tokenize(allocator, file.text)") ||
		strings.Contains(astBody, "target_tokens") {
		t.Fatal("package call diagnostics re-tokenize the target source in the AST entry")
	}
	localBody := selfhostKizuFunctionBody(
		t,
		content,
		"pub fn first_function_call_error_ast_node(",
	)
	if strings.Contains(localBody, "lexer::tokenize(allocator, file.text)") {
		t.Fatal("local function call diagnostics re-tokenize the parsed target AST")
	}
}

// TestSelfhostFunctionReferenceScanUsesParsedAST keeps package summaries off token walking.
func TestSelfhostFunctionReferenceScanUsesParsedAST(t *testing.T) {
	checker := readSelfhostFile(t, "../../selfhost/src/types/checker.kizu")
	functionCalls := readSelfhostFile(t, "../../selfhost/src/types/function_calls.kizu")
	scan := readSelfhostFile(t, "../../selfhost/src/types/function_call_scan_ast.kizu")
	required := []string{
		"function_call_scan_ast::check_file_function_references_from_ast(",
		"fn check_file_body_ast_node(",
		"pub fn check_file_function_references_from_ast(",
		"package_modules::collect_import_aliases_from_ast(",
		"function_calls::function_call_name_text(",
		"pub fn function_call_name_text(",
	}
	content := checker + functionCalls + scan
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost function reference AST scan missing %q", fragment)
		}
	}
	functionScanContent := functionCalls + scan
	forbidden := []string{
		"pub fn check_file_function_references(",
		"std::array::Array<std::kizu::lexer::Token>",
		"fn function_param_close(",
		"fn count_function_params(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(functionScanContent, fragment) {
			t.Fatalf("selfhost function reference scan keeps token or reparse path %q", fragment)
		}
	}
	checkerForbidden := []string{
		"function_calls::check_file_function_references(",
		"fn check_file_body_ast(",
	}
	for _, fragment := range checkerForbidden {
		if strings.Contains(checker, fragment) {
			t.Fatalf("selfhost checker keeps token or reparse path %q", fragment)
		}
	}
	body := selfhostKizuFunctionBody(t, checker, "fn check_file_body_ast_node(")
	if strings.Contains(body, "parser::parse_checked_file(") {
		t.Fatal("body checker reparses a file after the package checker already parsed it")
	}
}

// TestSelfhostTypeLocalsUseParsedAST rejects raw body text scans for function locals.
func TestSelfhostTypeLocalsUseParsedAST(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/types/local_facts.kizu")
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
		"let type_name = try expression_infer::infer_expression_type(",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost type local collection missing %q", fragment)
		}
	}
}

// TestSelfhostArgumentTypesUseParsedParams rejects param-source re-lexing.
func TestSelfhostArgumentTypesUseParsedParams(t *testing.T) {
	checkerBytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	paramBytes, err := os.ReadFile("../../selfhost/src/types/function_params.kizu")
	if err != nil {
		t.Fatalf("read selfhost function params: %v", err)
	}
	content := string(checkerBytes) + string(paramBytes)
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
		"ast_text::ast_return_type_text(file, ast, type_node)",
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
		"ast_text::ast_return_type_text(file, ast, return_type)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("selfhost semantic diagnostics missing %q", fragment)
		}
	}
	preASTBody := selfhostKizuFunctionBody(
		t,
		content,
		"pub fn first_pre_move_check_diagnostic_ast_node(",
	)
	postASTBody := selfhostKizuFunctionBody(
		t,
		content,
		"pub fn first_post_move_check_diagnostic_ast_node(",
	)
	if !strings.Contains(preASTBody+postASTBody, "collect_function_returns_from_ast(") {
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
		"pub fn first_pre_move_check_diagnostic(",
		"pub fn first_post_move_check_diagnostic(",
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
		"files: &std::array::Array<source::SourceFile>",
		"types::first_package_function_call_error_ast_node(",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("check entry package call diagnostic missing %q", fragment)
		}
	}
	astBody := selfhostKizuFunctionBody(t, content, "pub fn fast_diagnostics_ast_node(")
	coreBody := selfhostFastDiagnosticsCoreBody(t, content)
	callFragments := []string{
		"write_package_function_call_diagnostic(",
		"files,",
		"file,",
		"&ast,",
		"root",
	}
	for _, fragment := range callFragments {
		if !strings.Contains(astBody+coreBody, fragment) {
			t.Fatalf("check entry package call diagnostic missing call fragment %q", fragment)
		}
	}
	packageCall := "types::first_package_function_call_error_ast_node("
	if count := strings.Count(content, packageCall); count != 1 {
		t.Fatalf("check entry runs package call diagnostics %d times, want 1", count)
	}
	diagnosticBody := selfhostKizuFunctionBody(
		t,
		content,
		"fn write_package_function_call_diagnostic(",
	)
	if strings.Contains(diagnosticBody, "loader::load_file_sources(") {
		t.Fatal("check entry package call diagnostic reloads the source table")
	}
	fileCliBody := selfhostKizuFunctionBody(t, content, "pub fn file_cli(")
	if count := strings.Count(fileCliBody, "loader::load_file_sources("); count != 1 {
		t.Fatalf("check file_cli loads source table %d times, want 1", count)
	}
	if strings.Contains(content, "types::first_function_call_error(allocator, files, path)") {
		t.Fatal("check entry package call diagnostics reparse the target file")
	}
}

// selfhostFastDiagnosticsCoreBody extracts the shared parsed-AST diagnostic core.
func selfhostFastDiagnosticsCoreBody(t *testing.T, content string) string {
	t.Helper()
	return selfhostKizuFunctionBody(t, content, "fn fast_diagnostics_ast_node_with_context(")
}

// TestSelfhostCheckEntrySharesDiagnosticPasses keeps per-file checks grouped by phase.
func TestSelfhostCheckEntrySharesDiagnosticPasses(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/cli/check.kizu")
	if err != nil {
		t.Fatalf("read selfhost cli check: %v", err)
	}
	content := string(bytes)
	wrapperBody := selfhostKizuFunctionBody(t, content, "pub fn fast_diagnostics(")
	assertSelfhostFastDiagnosticsWrapper(t, wrapperBody)
	astBody := selfhostKizuFunctionBody(t, content, "pub fn fast_diagnostics_ast_node(")
	coreBody := selfhostFastDiagnosticsCoreBody(t, content)
	assertSelfhostFastDiagnosticsASTNode(t, wrapperBody, astBody+coreBody)
	assertSelfhostCheckEntryDropsOldDiagnosticWrappers(t, content)
}

// assertSelfhostFastDiagnosticsWrapper checks the wrapper validates once before AST parsing.
func assertSelfhostFastDiagnosticsWrapper(t *testing.T, wrapperBody string) {
	t.Helper()
	wrapperRequired := []string{
		"parser::validate_diagnostic_file(allocator, path, file_text)",
		"let validation_ok = parsed_validation.ok",
		"var files = try loader::load_file_sources(allocator, io, path, file_text)",
		"let parsed = try parser::parse_validated_file(",
		"validation_ok",
		"return try fast_diagnostics_ast_node(allocator, io, files, file, parsed.ast, parsed.root)",
	}
	for _, fragment := range wrapperRequired {
		if !strings.Contains(wrapperBody, fragment) {
			t.Fatalf("fast_diagnostics missing wrapper phase %q", fragment)
		}
	}
	if strings.Contains(wrapperBody, "parser::parse_checked_file(") {
		t.Fatal("fast_diagnostics revalidates an already validated source")
	}
	if count := strings.Count(wrapperBody, "parser::parse_validated_file("); count != 1 {
		t.Fatalf("fast_diagnostics parses validated source %d times, want 1", count)
	}
}

// assertSelfhostFastDiagnosticsASTNode checks per-file diagnostics use parsed AST phases.
func assertSelfhostFastDiagnosticsASTNode(t *testing.T, wrapperBody string, astBody string) {
	t.Helper()
	required := []string{
		"resolver::first_duplicate_declaration_ast_node(",
		"types::first_pre_move_check_diagnostic_ast_node_with_types(",
		"types::first_post_move_check_diagnostic_ast_node(",
		"ownership::first_use_after_move_name_ast_node_with_borrow_params(",
	}
	for _, fragment := range required {
		if !strings.Contains(astBody, fragment) {
			t.Fatalf("fast_diagnostics missing shared phase %q", fragment)
		}
	}
	forbidden := []string{
		"parser::parse_diagnostic_file(",
		"resolver::first_duplicate_declaration(allocator, path, file_text)",
		"types::first_pre_move_check_diagnostic(allocator, path, file_text)",
		"types::first_post_move_check_diagnostic(allocator, path, file_text)",
		"ownership::first_use_after_move_name(allocator, path, file_text)",
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
		if strings.Contains(wrapperBody+astBody, fragment) {
			t.Fatalf("fast_diagnostics keeps per-diagnostic call %q", fragment)
		}
	}
}

// assertSelfhostCheckEntryDropsOldDiagnosticWrappers rejects removed per-diagnostic writers.
func assertSelfhostCheckEntryDropsOldDiagnosticWrappers(t *testing.T, content string) {
	t.Helper()
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

// TestSelfhostRunTestReuseCheckedAST keeps run/test on one parsed frontend path.
func TestSelfhostRunTestReuseCheckedAST(t *testing.T) {
	main := readSelfhostFile(t, "../../selfhost/src/main.kizu")
	execute := readSelfhostFile(t, "../../selfhost/src/cli/execute.kizu")
	executable := readSelfhostFile(t, "../../selfhost/src/cli/executable.kizu")
	check := readSelfhostFile(t, "../../selfhost/src/cli/check.kizu")
	if !strings.Contains(check, "pub fn fast_diagnostics_ast_node(") {
		t.Fatal("check module does not expose parsed-AST fast diagnostics")
	}
	for _, fragment := range []string{
		"execute::run_file_cli(",
		"execute::test_file_cli(",
	} {
		if !strings.Contains(main, fragment) {
			t.Fatalf("main dispatch missing execute module call %q", fragment)
		}
	}
	for _, signature := range []string{
		"pub fn run_file_cli(",
		"pub fn test_file_cli(",
	} {
		body := selfhostKizuFunctionBody(t, execute, signature)
		required := []string{
			"parser::validate_diagnostic_file(allocator, path, file_text)",
			"let validation_ok = parsed_validation.ok",
			"var files = try loader::load_file_sources(allocator, io, path, file_text)",
			"let file = try files.at(0)",
			"let parsed = try parser::parse_validated_file(",
			"validation_ok",
			"check::fast_diagnostics_ast_node(",
		}
		for _, fragment := range required {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s missing shared frontend fragment %q", signature, fragment)
			}
		}
		if strings.Contains(body, "check::fast_diagnostics(allocator, io, path, file_text)") {
			t.Fatalf("%s reparses source through check::fast_diagnostics", signature)
		}
		if strings.Contains(body, "parser::parse_checked_file(") {
			t.Fatalf("%s revalidates an already validated source", signature)
		}
		if count := strings.Count(body, "parser::parse_validated_file("); count != 1 {
			t.Fatalf("%s parses validated source %d times, want 1", signature, count)
		}
	}
	for _, fragment := range []string{
		"executable::lower_run_executable(",
		"executable::lower_test_executable(",
		"backend::emit_run_executable_artifact(",
		"backend::emit_test_executable_artifact(",
	} {
		if !strings.Contains(execute, fragment) {
			t.Fatalf("execute module missing bounded executable path %q", fragment)
		}
	}
	assertSelfhostExecutableLoweringSplit(t, execute, executable)
}

// assertSelfhostExecutableLoweringSplit keeps CLI I/O separate from AST lowering.
func assertSelfhostExecutableLoweringSplit(t *testing.T, execute string, executable string) {
	t.Helper()
	for _, fragment := range []string{
		"pub fn lower_run_executable(",
		"pub fn lower_test_executable(",
		"fn lower_run_print_call(",
		"fn lower_expect_call(",
	} {
		if !strings.Contains(executable, fragment) {
			t.Fatalf("executable module missing AST executable lowering %q", fragment)
		}
	}
	for _, fragment := range []string{
		"fn lower_run_executable(",
		"fn lower_test_executable(",
	} {
		if strings.Contains(execute, fragment) {
			t.Fatalf("execute module still owns executable lowering %q", fragment)
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

// TestSelfhostTypeCheckReusesParsedFrontendAST keeps check_sources on one parse per source.
func TestSelfhostTypeCheckReusesParsedFrontendAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/checker.kizu")
	if err != nil {
		t.Fatalf("read selfhost types: %v", err)
	}
	body := selfhostKizuFunctionBody(t, string(bytes), "pub fn check_sources(")
	required := []string{
		"var parsed_frontend = std::array::Array<std::kizu::ast::ParseResult>(allocator)",
		"var parsed_frontend_files = std::array::Array<i64>(allocator)",
		"try parsed_frontend.append(result)",
		"try parsed_frontend_files.append(index)",
		"let result = try parsed_frontend.pop()",
		"let file_index = try parsed_frontend_files.pop()",
		"result.deinit()",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("type checker does not cache frontend ASTs with %q", fragment)
		}
	}
	if count := strings.Count(body, "parser::parse_checked_file("); count != 1 {
		t.Fatalf("type checker parses source in %d syntactic sites, want 1", count)
	}
}

// TestSelfhostPackageAritySelectionUsesModulePaths rejects hardcoded std module IDs.
func TestSelfhostPackageAritySelectionUsesModulePaths(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/types/function_calls.kizu")
	if err != nil {
		t.Fatalf("read selfhost function calls: %v", err)
	}
	moduleBytes, err := os.ReadFile("../../selfhost/src/types/package_modules.kizu")
	if err != nil {
		t.Fatalf("read selfhost package modules: %v", err)
	}
	content := string(bytes) + string(moduleBytes)
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
		"pub struct PackageModuleRef",
		"package_modules::list_contains_file(",
		"fn module_matches_path(",
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
	functionScan := readSelfhostFile(t, "../../selfhost/src/types/function_call_scan_ast.kizu")
	checkCLI := readSelfhostFile(t, "../../selfhost/src/cli/check.kizu")
	required := []string{
		"var import_alias_starts = std::map::Map<[]u8, i64>(allocator)",
		"var import_alias_ends = std::map::Map<[]u8, i64>(allocator)",
		"package_modules::collect_import_aliases_from_ast(",
		"first_qualified_segment_end(name)",
		"if import_alias_starts.contains(alias) {",
		"file.text[module_start..module_end]",
		"try output::stderr_newline(allocator, io);",
	}
	content := functionCalls + functionScan + checkCLI
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
	expressionInfer := readSelfhostFile(t, "../../selfhost/src/types/expression_infer.kizu")
	required := []string{
		"fn value_type_text(type_name: []u8) -> []u8",
		"let marker = \" borrows \";",
		"let expected_type = value_type_text(expected);",
		"let actual_type = value_type_text(actual);",
		"pub fn type_text_has_error_marker(type_name: []u8) -> bool",
		"std::mem::equal_bytes(value_type_text(type_name), \"[]u8\")",
	}
	for _, fragment := range required {
		if !strings.Contains(expressionInfer, fragment) {
			t.Fatalf("selfhost type checker borrow provenance normalization missing %q", fragment)
		}
	}
	if strings.Contains(expressionInfer, "debug-") {
		t.Fatal("selfhost type checker keeps temporary debug output")
	}
	if strings.Contains(expressionInfer, "fn bytes_contains(") ||
		strings.Contains(expressionInfer, "bytes_contains(") {
		t.Fatal("selfhost type checker keeps generic byte contains helper")
	}
	body := selfhostKizuFunctionBody(t, expressionInfer, "pub fn known_type_mismatch(")
	if !strings.Contains(body, "type_text_has_error_marker(actual_type)") {
		t.Fatal("selfhost type mismatch comparison does not use closed error-marker predicate")
	}
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
		"pub fn first_pre_move_check_diagnostic_ast_node(",
	},
	"../../selfhost/src/types/ast_text.kizu": {
		"pub fn ast_node_text(",
		"pub fn ast_return_type_text(",
		"pub fn is_qualified_name(",
	},
	"../../selfhost/src/types/expression_infer.kizu": {
		"pub fn infer_expression_type(",
		"pub fn known_type_mismatch(",
		"fn field_expression_type(",
	},
	"../../selfhost/src/types/local_facts.kizu": {
		"pub fn collect_function_statement_locals(",
		"pub fn function_statement_return_type(",
		"fn collect_let_statement_local(",
	},
	"../../selfhost/src/types/function_params.kizu": {
		"pub struct FunctionParamType",
		"pub fn collect_local_function_param_types(",
		"pub fn lookup_function_param_type(",
	},
	"../../selfhost/src/types/return_diagnostics.kizu": {
		"pub fn first_return_type_mismatch_node(",
		"pub fn first_missing_return_node(",
		"fn statement_always_returns(",
	},
	"../../selfhost/src/types/function_calls.kizu": {
		"pub fn first_package_function_call_error_ast_node(",
		"pub fn function_call_name_text(",
	},
	"../../selfhost/src/types/function_call_scan_ast.kizu": {
		"pub fn check_file_function_references_from_ast(",
		"fn scan_function_reference_ast_node(",
	},
	"../../selfhost/src/types/package_modules.kizu": {
		"pub struct PackageModuleRef",
		"pub fn collect_referenced_package_call_modules_from_ast(",
	},
	"../../selfhost/src/types/body_scan.kizu": {
		"pub fn scan_body_ast_node(",
		"fn scan_body_child_range(",
	},
	"../../selfhost/src/types/type_refs.kizu": {
		"pub fn collect_declared_types_from_ast(",
		"pub fn collect_type_parameters_from_ast(",
	},
	"../../selfhost/src/types/type_ref_ast.kizu": {
		"pub fn first_type_error_in_ast(",
		"fn first_type_error_in_type_node_ast(",
	},
	"../../selfhost/src/types/type_ref_scan_ast.kizu": {
		"pub fn check_file_type_references_from_ast(",
		"fn scan_type_reference_ast_node(",
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
		"pub fn first_use_after_move_name_ast_node(",
		"fn first_use_after_move_ast_node(",
	},
	"../../selfhost/src/backend/runtime.kizu": {
		"pub fn load_storage_module(",
		"pub fn render_host_metadata(",
	},
	"../../selfhost/src/backend/hosted.kizu": {
		"pub fn emit_run_executable_artifact(",
		"fn render_run_return_llvm(",
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
	"../../selfhost/src/backend/cli_artifact_path_llvm.kizu": {
		"pub fn append_functions(",
		"fn append_write_concat5_function(",
		"fn append_write_concat9_function(",
		"fn append_i64_decimal_function(",
		"fn append_artifact_path_function(",
	},
	"../../selfhost/src/backend/cli_run_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_cli_run_blocks(",
		"fn append_run_print_emit_block(",
		"fn append_run_return_emit_block(",
	},
	"../../selfhost/src/backend/cli_test_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_cli_test_blocks(",
		"fn append_test_ok_emit_block(",
	},
	"../../selfhost/src/backend/cli_match_llvm.kizu": {
		"pub fn append_functions(",
		"fn append_cli_run_print_payload_function(",
		"fn append_cli_run_return_ok_function(",
		"fn append_cli_test_expect_value_function(",
		"fn append_cli_moved_value_name_function(",
	},
	"../../selfhost/src/backend/cli_parse_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_cli_parse_blocks(",
		"pub fn append_cli_fmt_blocks(",
		"fn append_parse_format_write_function(",
	},
	"../../selfhost/src/backend/cli_parse_comment_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_functions(",
		"fn append_parse_has_unsupported_line_comment_function(",
	},
	"../../selfhost/src/backend/cli_parse_diag_llvm.kizu": {
		"pub fn append_globals(",
		"pub fn append_functions(",
		"fn append_parse_missing_assign_index_function(",
	},
	"../../selfhost/src/parser/validation_tokens.kizu": {
		"pub fn token_text(",
		"pub fn skip_top_level_declaration(",
		"pub fn skip_balanced_parens(",
	},
	"../../selfhost/src/cli/check.kizu": {
		"pub fn file_cli(",
		"pub fn fast_diagnostics(",
		"pub fn fast_diagnostics_ast_node(",
	},
	"../../selfhost/src/cli/execute.kizu": {
		"pub fn run_file_cli(",
		"pub fn test_file_cli(",
		"executable::lower_run_executable(",
		"executable::lower_test_executable(",
	},
	"../../selfhost/src/cli/executable.kizu": {
		"pub fn lower_run_executable(",
		"pub fn lower_test_executable(",
		"fn lower_run_print_call(",
		"fn lower_expect_call(",
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
	parserValidation := readSelfhostFile(t, "../../selfhost/src/parser/validation.kizu")
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
			"fn render_run_print_llvm(",
			"fn render_test_expect_ok_llvm(",
		},
		"parser/validation.kizu": {
			"fn token_text(",
			"fn skip_top_level_declaration(",
			"fn skip_balanced_parens(",
		},
	}
	contents := map[string]string{
		"types.kizu":             rootTypes,
		"ownership.kizu":         rootOwnership,
		"main.kizu":              rootMain,
		"backend.kizu":           rootBackend,
		"parser/validation.kizu": parserValidation,
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
	if strings.Contains(content, "parser::parse_diagnostic_file(") {
		t.Fatal("CLI parse path uses AST-discarding parser wrapper")
	}
	body := selfhostKizuFunctionBody(t, content, "fn validate_file_cli_parse(")
	required := []string{
		"parser::validate_diagnostic_file(allocator, path, file_text)",
		"let validation_ok = parsed_validation.ok",
		"if !validation_ok {",
		"diagnostics::parse_validation_error(allocator, io, parsed_validation)",
		"let parsed = try parser::parse_validated_file(",
		"validation_ok",
		"parsed.deinit();",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("CLI parse path missing %q", fragment)
		}
	}
}

// TestSelfhostCheckManifestTargetUsesContent rejects manifest filename dispatch.
func TestSelfhostCheckManifestTargetUsesContent(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/main.kizu")
	if err != nil {
		t.Fatalf("read selfhost main: %v", err)
	}
	body := selfhostKizuFunctionBody(t, string(bytes), "fn check_file_or_package_cli(")
	forbidden := []string{
		"std::path::basename(target)",
		"\"kizu.toml\"",
	}
	for _, fragment := range forbidden {
		if strings.Contains(body, fragment) {
			t.Fatalf("check file/package dispatch keeps filename branch %q", fragment)
		}
	}
	required := []string{
		"let target_text = try std::fs::read_file(io, target)",
		"source::is_manifest_text(target_text)",
		"check::package_cli(allocator, io, std::path::dirname(target))",
		"check::file_cli(allocator, io, target)",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("check file/package dispatch missing %q", fragment)
		}
	}
}

// TestSelfhostPackageSourceLoaderUsesManifestPaths rejects fixed package roots.
func TestSelfhostPackageSourceLoaderUsesManifestPaths(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/source/loader.kizu")
	if err != nil {
		t.Fatalf("read selfhost source loader: %v", err)
	}
	body := selfhostKizuFunctionBody(t, string(bytes), "pub fn load_package_sources(")
	if strings.Contains(body, "std::path::join(allocator, root, \"src\")") {
		t.Fatal("package source loader hardcodes src instead of manifest paths")
	}
	if !strings.Contains(body, "append_manifest_package_source_dirs(") {
		t.Fatal("package source loader does not use manifest source paths")
	}
	forbidden := []string{
		"package_src_module_start",
		"path_src_segment_at",
	}
	for _, fragment := range forbidden {
		if strings.Contains(string(bytes), fragment) {
			t.Fatalf("package source loader keeps fixed src module helper %q", fragment)
		}
	}
}

// TestSelfhostPackageFastDiagnosticsReuseParsedAST rejects package call reparse loops.
func TestSelfhostPackageFastDiagnosticsReuseParsedAST(t *testing.T) {
	content := readSelfhostFile(t, "../../selfhost/src/cli/check.kizu")
	packageBody := selfhostKizuFunctionBody(t, content, "pub fn package_cli(")
	if !strings.Contains(packageBody, "package_fast_diagnostics(") {
		t.Fatal("package check CLI does not run parsed-AST fast diagnostics")
	}
	if strings.Contains(packageBody, "package_diagnostics_loaded(") {
		t.Fatal("package check CLI reruns full package diagnostics after fast diagnostics")
	}
	body := selfhostKizuFunctionBody(t, content, "fn package_fast_diagnostics(")
	required := []string{
		"parse_package_fast_diagnostic_sources(",
		"var facts = FastDiagnosticContext(allocator)",
		"fast_diagnostics_ast_node_with_context(",
		"write_cached_package_function_call_diagnostic(",
	}
	for _, fragment := range required {
		if !strings.Contains(body+content, fragment) {
			t.Fatalf("package fast diagnostics missing parsed AST reuse %q", fragment)
		}
	}
	forbidden := []string{
		"package_fast_diagnostic_file(",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("package fast diagnostics keeps per-file reparse helper %q", fragment)
		}
	}
}

// TestSelfhostMoveDiagnosticsUseParsedAST keeps move diagnostics on the parsed AST entry.
func TestSelfhostMoveDiagnosticsUseParsedAST(t *testing.T) {
	bytes, err := os.ReadFile("../../selfhost/src/ownership/move_diagnostic.kizu")
	if err != nil {
		t.Fatalf("read selfhost move diagnostic: %v", err)
	}
	content := string(bytes)
	required := []string{
		"pub fn first_use_after_move_name_ast_node(",
		"file: &source::SourceFile,",
		"fn initializer_is_record_literal(",
		"StructLiteralExpr(struct_literal) => true",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("move diagnostics missing %q", fragment)
		}
	}
	forbidden := []string{
		"pub fn first_use_after_move_name(",
		"parser::parse_checked_file(",
		"initializer_text_is_record_literal(",
		"fn bytes_contains(",
		"bytes_contains(text, \"{\")",
	}
	for _, fragment := range forbidden {
		if strings.Contains(content, fragment) {
			t.Fatalf("move diagnostics keep raw initializer scan %q", fragment)
		}
	}
}

// TestSelfhostOwnershipBorrowKindsUseTypeNodes rejects raw substring borrow fallback.
func TestSelfhostOwnershipBorrowKindsUseTypeNodes(t *testing.T) {
	kind := readSelfhostFile(t, "../../selfhost/src/ownership/kind.kizu")
	checker := readSelfhostFile(t, "../../selfhost/src/ownership/checker.kizu")
	required := []string{
		"pub fn resource_kind_for_type_node(",
		"resource_type_node_is_borrowed(file, ast, type_node)",
		"kind::resource_kind_for_type_node(file, ast, type_node)",
		"Param(param_node) => return resource_kind_for_type_node(",
	}
	for _, fragment := range required {
		if !strings.Contains(kind+checker, fragment) {
			t.Fatalf("ownership resource kind missing parsed type-node path %q", fragment)
		}
	}
	forbidden := []string{
		"fn resource_text_is_borrowed(",
		"bytes_contains(",
		"\": &\"",
		"\": []\"",
	}
	for _, fragment := range forbidden {
		if strings.Contains(kind, fragment) {
			t.Fatalf("ownership resource kind keeps raw borrowed-type fallback %q", fragment)
		}
	}
}
