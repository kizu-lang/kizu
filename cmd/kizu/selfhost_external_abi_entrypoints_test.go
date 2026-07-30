package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

func TestSelfhostExternalABIEntrypointsOwnCompiledPackageRoots(t *testing.T) {
	manifest := readSelfhostFile(t, "../../selfhost/src/ir/external_abi_entrypoints.kizu")
	executable := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	validation := readSelfhostFile(t, "../../selfhost/src/backend/external_abi_validation.kizu")
	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	externalRoots := selfhostKizuFunctionBody(
		t, executable, "fn append_external_abi_roots(",
	)

	entries := []string{
		`"selfhost", "cli/check", "fast_diagnostics_parsed_file"`,
		`"selfhost", "cli/execute", "render_checked_run_artifact"`,
		`"selfhost", "backend/executable", "lower_test_executable"`,
		`"std", "kizu/parser", "parse_program"`,
		`"selfhost", "parser/format", "format_source"`,
		`"selfhost", "backend/hosted", "artifact_path"`,
		`"selfhost", "ir/codegen", "metadata_line"`,
		// The manifest doubles as the emission closure's root set. These two are roots for
		// that reason rather than because the handwritten boundary calls them: package_cli
		// pulls in source::loader, and summarize_parse_result is the narrowest root that
		// reaches ast.kizu's node_count family. The count assertion below is what keeps the
		// set closed, so a new root has to be added here deliberately.
		`"selfhost", "cli/check", "package_cli"`,
		`"selfhost", "ast", "summarize_parse_result"`,
	}
	for _, entry := range entries {
		if count := strings.Count(manifest, entry); count != 1 {
			t.Fatalf("external ABI manifest entry %q count = %d, want one", entry, count)
		}
	}
	for _, signature := range []string{
		`"fast_diagnostics_parsed_file", "!bool",`,
		`"Allocator;Io;[]u8;[]u8;std::kizu::ast::ParseResult"`,
		`"render_checked_run_artifact", "!std::string::String",`,
		`"Allocator;[]u8;[]u8;std::kizu::ast::ParseResult"`,
		`"lower_test_executable", "!data::Executable",`,
		`"[]u8;std::kizu::ast::Ast;std::kizu::ast::NodeId"`,
		`"parse_program", "!std::kizu::ast::ParseResult",`,
		`"Allocator;std::kizu::ast::SourceFile"`,
		`"format_source", "!std::string::String",`,
		`"Allocator;[]u8"`,
		`"artifact_path", "!std::string::String",`,
		`"Allocator;[]u8;[]u8;[]u8"`,
		`"metadata_line", "[]u8", ""`,
		`"package_cli", "!i64",`,
		`"Allocator;Io;[]u8"`,
		`"summarize_parse_result", "!AstSummary",`,
		`"&std::kizu::ast::ParseResult;i64;i64"`,
	} {
		if !strings.Contains(manifest, signature) {
			t.Fatalf("external ABI manifest lacks exact semantic signature %q", signature)
		}
	}
	if count := strings.Count(manifest, "try append("); count != len(entries) {
		t.Fatalf("external ABI manifest root count = %d, want %d", count, len(entries))
	}
	for _, fragment := range []string{
		"pub return_type: []u8",
		"pub param_types: []u8",
		"entrypoints: std::array::Array<ExternalAbiEntrypoint>",
		"manifest.entrypoints.append(ExternalAbiEntrypoint {",
		`"!bool",`,
		`"Allocator;Io;[]u8;[]u8;std::kizu::ast::ParseResult"`,
		"pub fn param_count(",
		"pub fn param_type_at(",
		"pub fn append_qualified_name(",
		"entrypoint.package_name",
		"entrypoint.module_path",
		"entrypoint.function_name",
	} {
		if !strings.Contains(manifest, fragment) {
			t.Fatalf("external ABI manifest lacks canonical qualified-name builder %q", fragment)
		}
	}
	for _, fragment := range []string{
		"external_abi_entrypoints::collect(allocator)",
		"let manifest_entries = &entrypoints.entrypoints",
		"manifest_entries.at(entrypoint_index)",
		"package_exact_lookup::resolve_call(",
		`out.append_bytes("external-abi-entrypoint ")`,
		"external_abi_entrypoints::append_qualified_name(out, &entrypoint)",
		"package_dependency_graph::queue_append(pending, catalog, target)",
	} {
		if !strings.Contains(externalRoots, fragment) {
			t.Fatalf("numeric package closure missing manifest ownership %q", fragment)
		}
	}
	for _, fragment := range []string{
		"external_abi_validation::require_manifest_roots(bytes)",
	} {
		if !strings.Contains(llvm, fragment) {
			t.Fatalf("LLVM backend does not consume manifest roots: missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"external_abi_entrypoints::collect(allocator)",
		"external_abi_entrypoints::append_qualified_name(&var qualified, entrypoint)",
		`fact_prefix_line_count(bytes, "external-abi-entrypoint ")`,
		`"function-signature-return "`,
		`"function-signature-param "`,
		"entrypoint.return_type",
		"external_abi_entrypoints::param_count(entrypoint)",
		"external_abi_entrypoints::param_type_at(",
		`"runtime"`,
		`"external ABI entrypoint param type mismatch"`,
		`"duplicate external ABI entrypoint param signature"`,
	} {
		if !strings.Contains(validation, fragment) {
			t.Fatalf("external ABI validator missing fail-closed manifest check %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"require_executable_function_signatures",
		`"selfhost::cli::execute::run_file_cli"`,
		"require_function_signature_param_count",
		"require_function_signature_return",
		"require_function_signature_param(",
		"append_executable_function_signature_metadata",
	} {
		if strings.Contains(llvm, forbidden) || strings.Contains(validation, forbidden) {
			t.Fatalf("external ABI validation retains manual root path %q", forbidden)
		}
	}
	for _, fragment := range []string{
		"append_component_reachable_compiled_functions",
		"append_codegen_reachable_compiled_functions",
		"append_code_render_reachable_compiled_functions",
		"append_kizu_parser_reachable_compiled_functions",
		"append_kizu_lexer_reachable_compiled_functions",
		"append_lexer_reachable_compiled_functions",
		"append_format_reachable_compiled_functions",
		"append_ast_reachable_compiled_functions",
		"append_loader_reachable_compiled_functions",
	} {
		if strings.Contains(cli, fragment) {
			t.Fatalf("backend retains duplicate manual compiled closure %q", fragment)
		}
	}
	consumerSources := readSelfhostFile(t, "../../selfhost/src/backend/cli_check_gate_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_run_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_test_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_ast_boundary_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_parse_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	for _, consumer := range []string{
		"@kizu_selfhost__cli_check_fast_diagnostics_parsed_file(",
		"@kizu_selfhost__cli_execute_render_checked_run_artifact(",
		"@kizu_selfhost__backend_executable_lower_test_executable(",
		"@kizu_kizu__parser_parse_program(",
		"@kizu_selfhost__parser_format_format_source(",
		"@kizu_selfhost__backend_hosted_artifact_path(",
		`"selfhost::ir::codegen::metadata_line"`,
	} {
		if !strings.Contains(consumerSources, consumer) {
			t.Fatalf("external ABI manifest has no backend consumer %q", consumer)
		}
	}

	cliBoundary := cli +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_ast_boundary_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_check_gate_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_run_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_test_llvm.kizu")
	for _, forbidden := range []string{
		"cli_test_ast_llvm",
		"%kizu.kizu.ast.ast_data",
		"cli_unsupported_parse_result",
		"cli_ast_new",
		"cli_add_leaf_node",
		"cli_lower_test_parse_result",
		"%kizu.kizu.ast.source_file",
		"%kizu.kizu.ast.parse_result",
		"%kizu.kizu.ast.ast",
		"%kizu.kizu.ast.node_id",
		" poison, %kizu.slice.u8 %path, 0",
		" %file0, %kizu.slice.u8 %source, 1",
		" %test_parsed, 0",
		" %test_parsed, 1",
		" %test_executable, 0",
		" %test_executable, 2",
	} {
		if strings.Contains(cliBoundary, forbidden) {
			t.Fatalf("static CLI retains obsolete AST lowering path %q", forbidden)
		}
	}
	for _, fragment := range []string{
		"executable_error_abi",
		"%test_lowered_ok = extractvalue ",
		"@kizu_selfhost__backend_executable_lower_test_executable(",
		"%test_executable_payload = extractvalue ",
		"std::fmt::append_i64(out, executable_payload_index)",
		"compiled_fact_lookup::lookup_struct_field_exact_indexed(",
		`"std::kizu::ast::SourceFile", "path"`,
		`"std::kizu::ast::ParseResult", "root"`,
		`"selfhost::backend::data::Executable", "payload"`,
		"call void @kizu_rt_process_exit(i64 1)",
	} {
		if !strings.Contains(cliBoundary, fragment) {
			t.Fatalf("compiled CLI AST boundary missing %q", fragment)
		}
	}
}

func TestSelfhostExternalABIManifestValidationFailsClosed(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var nameOut bytes.Buffer
	if err := interp.New(&nameOut).RunEntry(
		program,
		"selfhost::ir::external_abi_entrypoints::qualified_name_gate",
	); err != nil {
		t.Fatalf("external ABI qualified-name gate failed: %v", err)
	}
	if got := nameOut.String(); got != "selfhost::cli::check::fast_diagnostics_parsed_file\n" {
		t.Fatalf("external ABI qualified name = %q", got)
	}

	var validOut bytes.Buffer
	if err := interp.New(&validOut).RunEntry(
		program,
		"selfhost::backend::external_abi_validation::valid_manifest_roots_gate",
	); err != nil {
		t.Fatalf("valid external ABI manifest rejected: %v\n%s", err, validOut.String())
	}
	if got := validOut.String(); got != "external-abi-roots-ok\n" {
		t.Fatalf("valid external ABI gate output = %q", got)
	}

	cases := []struct {
		entry string
		want  string
	}{
		{"missing_manifest_root_gate", "missing external ABI entrypoint root"},
		{"duplicate_manifest_root_gate", "duplicate external ABI entrypoint root"},
		{"missing_manifest_return_gate", "missing external ABI entrypoint return signature"},
		{"duplicate_manifest_return_gate", "duplicate external ABI entrypoint return signature"},
		{"wrong_manifest_return_type_gate", "external ABI entrypoint return type mismatch"},
		{"missing_manifest_param_gate", "external ABI entrypoint param arity mismatch"},
		{"extra_manifest_param_gate", "external ABI entrypoint param arity mismatch"},
		{"duplicate_manifest_params_gate", "duplicate external ABI entrypoint param signature"},
		{"wrong_manifest_param_type_gate", "external ABI entrypoint param type mismatch"},
		{"wrong_manifest_param_mode_gate", "external ABI entrypoint param mode mismatch"},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		err := interp.New(&out).RunEntry(
			program,
			"selfhost::backend::external_abi_validation::"+tc.entry,
		)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("external ABI gate %s error = %v, want %q", tc.entry, err, tc.want)
		}
	}
}
