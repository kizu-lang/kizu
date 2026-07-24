package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostTypeDeclarationRegistryOutput = "%kizu.app.alpha.child = type { i64 }\n" +
	"%kizu.app.alpha.model = type { %kizu.app.alpha.child, %kizu.owned }\n" +
	"%kizu.app.beta.child = type { i1 }\n" +
	"%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }\n" +
	"%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }\n" +
	"%kizu.error.void = type { i1, %kizu.slice.u8 }\n" +
	"%kizu.selfhost.ir.codegen.payload_span = type { i64, i64 }\n" +
	"%kizu.error.kizu.selfhost.ir.codegen.payload_span = type { i1, " +
	"%kizu.selfhost.ir.codegen.payload_span, %kizu.slice.u8 }\n" +
	"%kizu.selfhost.source.source_file = type { i64, i64, %kizu.slice.u8, " +
	"i64, i64, %kizu.slice.u8, %kizu.slice.u8 }\n" +
	"%kizu.selfhost.types.constructor_facts.constructor_facts = type { " +
	"%kizu.owned, %kizu.owned, %kizu.owned, %kizu.owned, %kizu.owned }\n" +
	"%kizu.selfhost.types.primitive_type.type_record = type { i64, i64, i64, i1 }\n" +
	"%kizu.kizu.ast.source_file = type { %kizu.slice.u8, %kizu.slice.u8 }\n" +
	"%kizu.kizu.ast.child_range = type { i64, i64 }\n" +
	"%kizu.kizu.ast.program_node = type { %kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.token_id = type { i64 }\n" +
	"%kizu.kizu.ast.int_node = type { %kizu.kizu.ast.token_id }\n" +
	"%kizu.kizu.ast.string_node = type { %kizu.kizu.ast.token_id }\n" +
	"%kizu.kizu.ast.symbol_id = type { i64 }\n" +
	"%kizu.kizu.ast.type_name_node = type { %kizu.kizu.ast.symbol_id }\n" +
	"%kizu.kizu.ast.span = type { i64, i64 }\n" +
	"%kizu.kizu.ast.var_node = type { %kizu.kizu.ast.symbol_id, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.bool_node = type { i1 }\n" +
	"%kizu.kizu.ast.node_id = type { %kizu.handle }\n" +
	"%kizu.kizu.ast.prefix_node = type { i64, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.binary_node = type { i64, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.field_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, i1 }\n" +
	"%kizu.kizu.ast.deref_expr_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.call_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.type_apply_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.cast_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.index_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, i1 }\n" +
	"%kizu.kizu.ast.struct_literal_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.struct_field_init_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.arena_new_expr_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.try_expr_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.comptime_expr_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.block_node = type { %kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.if_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.let_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.assign_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.return_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.defer_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.err_defer_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.expr_stmt_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.while_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.for_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.break_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.continue_node = type { %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.import_decl_node = type { %kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.param_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.field_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.struct_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range, %kizu.kizu.ast.child_range, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.enum_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.union_decl_node = type { i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range, %kizu.kizu.ast.child_range, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.impl_decl_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.union_variant_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.ast.match_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.child_range }\n" +
	"%kizu.kizu.ast.match_arm_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.unsafe_node = type { %kizu.kizu.ast.child_range, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.comptime_if_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.fn_decl_node = type { i1, i1, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.child_range, " +
	"%kizu.kizu.ast.child_range, %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.ast.node_id, %kizu.kizu.ast.node_id, %kizu.kizu.ast.span }\n" +
	"%kizu.kizu.lexer.position = type { i64, i64 }\n" +
	"%kizu.kizu.lexer.token = type { i64, i64, i64, i64, i64, i64, i64 }\n" +
	"%kizu.kizu.parser.parse_node = type { %kizu.kizu.ast.node_id, " +
	"%kizu.kizu.lexer.token }\n" +
	"%kizu.kizu.parser.parse_range = type { %kizu.kizu.ast.child_range, " +
	"%kizu.kizu.lexer.token }\n" +
	"%kizu.kizu.ast.ast = type { %kizu.owned, %kizu.owned, " +
	"%kizu.kizu.ast.source_file }\n" +
	"%kizu.kizu.ast.parse_result = type { %kizu.kizu.ast.ast, " +
	"%kizu.kizu.ast.node_id }\n" +
	"%kizu.kizu.ast.ast_data = type { i64, [136 x i8] }\n" +
	"%kizu.kizu.ast.ast_node = type { %kizu.kizu.ast.span, " +
	"%kizu.kizu.ast.ast_data }\n" +
	"%kizu.kizu.diagnostic.file_span = type { i64, %kizu.slice.u8, " +
	"i64, i64, i64, i64 }\n" +
	"%kizu.kizu.diagnostic.related_span = type { " +
	"%kizu.kizu.diagnostic.file_span, %kizu.slice.u8 }\n" +
	"%kizu.kizu.diagnostic.diagnostic = type { " +
	"%kizu.kizu.diagnostic.file_span, %kizu.slice.u8, %kizu.owned }\n" +
	"\n"

// TestSelfhostTypeDeclarationRegistryGate executes the independent Kizu-owned
// declaration registry over exact fake package facts. The output pins nested
// module-local field resolution, nominal/root dedupe, opaque recursive handles,
// ABI-keyed error-union sharing, qualified nominal separation, and !void.
func TestSelfhostTypeDeclarationRegistryGate(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_declaration_gates::gate",
	)
	if err != nil {
		t.Fatalf("type declaration registry gate failed: %v\n%s", err, out.String())
	}
	if out.String() != selfhostTypeDeclarationRegistryOutput {
		t.Fatalf(
			"type declaration registry output mismatch\nwant:\n%sgot:\n%s",
			selfhostTypeDeclarationRegistryOutput, out.String(),
		)
	}
}

func TestSelfhostTypeDeclarationRegistryUsesExactNominalIdentity(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program,
		"selfhost::backend::compiled_type_declaration_gates::gate_non_nominal_shared_abi",
	)
	if err != nil {
		t.Fatalf("exact nominal identity gate failed: %v\n%s", err, out.String())
	}
	if out.String() != "exact-nonnominal\n" {
		t.Fatalf("exact nominal identity output mismatch: %q", out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryRejectsInvalidFacts(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	cases := []struct {
		entry string
		want  string
	}{
		{"gate_direct_cycle", "by-value type cycle"},
		{"gate_mutual_cycle", "by-value type cycle"},
		{"gate_duplicate_field", "duplicate struct field index"},
		{"gate_field_gap", "struct field index gap"},
		{"gate_field_name_conflict", "duplicate struct field name"},
		{"gate_negative_field_index", "invalid struct field index"},
		{"gate_nondecimal_field_index", "invalid struct field index"},
		{"gate_field_index_overflow", "struct field index overflow"},
		{"gate_empty_field_name", "empty struct field name"},
		{"gate_empty_field_type", "empty struct field type"},
		{"gate_nominal_collision", "qualified nominal ABI collision"},
		{"gate_exact_nominal_abi_mismatch", "canonical type kind ABI mismatch"},
		{"gate_duplicate_canonical_type_kind", "duplicate canonical type kind"},
		{"gate_duplicate_generic_constructor_kind", "duplicate canonical type kind"},
		{"gate_duplicate_type_fact", "compiled type resolver: duplicate exact fact"},
		{"gate_conflicting_error_fact", "compiled error ABI: conflicting payload fact"},
		{"gate_error_name_collision", "error union name collision"},
		{"gate_duplicate_union_variant_name", "duplicate variant name"},
		{"gate_duplicate_union_variant_tag", "duplicate variant tag"},
		{"gate_negative_union_variant_tag", "invalid index"},
		{"gate_nondecimal_union_variant_tag", "invalid index"},
		{"gate_nonzero_union_variant_start", "variant tags must be ordered from zero"},
		{"gate_union_variant_tag_gap", "variant tags must be ordered from zero"},
		{"gate_union_variant_tag_order_mismatch", "variant tags must be ordered from zero"},
		{"gate_union_variant_tag_overflow", "index overflow"},
		{"gate_unknown_abi_repr", "unknown abi-repr"},
		{"gate_reserved_fixed_abi_name", "reserved fixed ABI name"},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			var out bytes.Buffer
			err := interp.New(&out).RunEntry(
				program,
				"selfhost::backend::compiled_type_declaration_gates::"+tc.entry,
			)
			if err == nil {
				t.Fatalf("invalid declaration facts accepted\n%s", out.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("declaration registry error mismatch: %v", err)
			}
		})
	}
}

func TestSelfhostTypeDeclarationRegistryRecursiveUnionLayout(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_declaration_gates::gate_recursive_union_layout",
	)
	if err != nil {
		t.Fatalf("recursive union layout gate failed: %v\n%s", err, out.String())
	}
	want := "%kizu.app.inner = type { i64, [8 x i8] }\n" +
		"%kizu.app.wide = type { %kizu.slice.u8, i64 }\n" +
		"%kizu.app.outer = type { i64, [24 x i8] }\n\n"
	if out.String() != want {
		t.Fatalf("recursive union layout output mismatch\nwant:\n%sgot:\n%s", want, out.String())
	}
}

func TestSelfhostScalarABIAndUnionLayoutShareWidths(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_declaration_gates::gate_scalar_abi_layout",
	)
	if err != nil {
		t.Fatalf("scalar ABI/layout gate failed: %v\n%s", err, out.String())
	}
	if out.String() != "1\n2\n4\n8\n4\n8\n" {
		t.Fatalf("scalar ABI/layout mismatch: %q", out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryTraversesGenericArguments(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_declaration_gates::gate_reachable_generic_arguments",
	)
	if err != nil {
		t.Fatalf("reachable generic arguments gate failed: %v\n%s", err, out.String())
	}
	if out.String() != "%kizu.app.model = type { i64 }\n\n" {
		t.Fatalf("reachable generic arguments output mismatch: %q", out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryScopesFunctionTypeParameters(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program,
		"selfhost::backend::compiled_type_declaration_gates::gate_reachable_function_type_parameters",
	)
	if err != nil {
		t.Fatalf("reachable function type parameters gate failed: %v\n%s", err, out.String())
	}
	if out.String() != "%kizu.app.model = type { i64 }\n\n" {
		t.Fatalf("reachable function type parameters output mismatch: %q", out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryDoesNotLeakFunctionTypeParameterScope(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program,
		"selfhost::backend::compiled_type_declaration_gates::gate_function_type_parameter_scope",
	)
	if err == nil {
		t.Fatalf("plain function type was mistaken for another function's formal\n%s", out.String())
	}
	for _, want := range []string{
		"compiled type resolver: type facts not found",
		"module=app::plain::",
		"type=T",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("function type parameter scope error missing %q: %v", want, err)
		}
	}
}

func TestSelfhostTypeDeclarationRegistryRejectsMalformedFunctionTypeParameters(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	cases := []struct {
		entry string
		want  string
	}{
		{"gate_function_type_parameter_gap", "must be ordered from zero"},
		{"gate_function_type_parameter_duplicate_name", "duplicate function type parameter name"},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		err := interp.New(&out).RunEntry(
			program,
			"selfhost::backend::compiled_type_declaration_gates::"+tc.entry,
		)
		if err == nil {
			t.Fatalf("malformed function type parameters accepted for %s\n%s", tc.entry, out.String())
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("malformed function type parameter error mismatch for %s: %v", tc.entry, err)
		}
	}
}

func TestSelfhostTypeDeclarationRegistryUsesUnionDeclarationModule(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_type_declaration_gates::gate_imported_union_payload",
	)
	if err != nil {
		t.Fatalf("imported union payload gate failed: %v\n%s", err, out.String())
	}
	want := "%kizu.app.defs.model = type { i64 }\n" +
		"%kizu.app.defs.event = type { i64, [8 x i8] }\n\n"
	if out.String() != want {
		t.Fatalf("imported union payload output mismatch\nwant:\n%sgot:\n%s", want, out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryUsesImportedStructDeclarationModule(t *testing.T) {
	program := loadSelfhostTypeDeclarationRegistryProgram(t)
	var out bytes.Buffer
	err := interp.New(&out).RunEntry(
		program,
		"selfhost::backend::compiled_type_declaration_gates::gate_imported_struct_field_context",
	)
	if err != nil {
		t.Fatalf("imported struct field gate failed: %v\n%s", err, out.String())
	}
	want := "%kizu.app.defs.payload = type { i64 }\n" +
		"%kizu.app.defs.envelope = type { %kizu.app.defs.payload }\n\n"
	if out.String() != want {
		t.Fatalf("imported struct field output mismatch\nwant:\n%sgot:\n%s", want, out.String())
	}
}

func TestSelfhostTypeDeclarationRegistryProductionHook(t *testing.T) {
	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	if !strings.Contains(
		llvm,
		"compiled_type_declarations::append_reachable_fact_declarations_indexed(",
	) {
		t.Fatal("production LLVM renderer does not call the reachable declaration registry")
	}
	for _, removed := range []string{
		`"%kizu.selfhost.parser.format.comment_format_state = type`,
		`"%kizu.error.comment_format_state = type`,
		`"%kizu.kizu.ast.node_id = type`,
		`"%kizu.error.node_id = type`,
		`"%kizu.error.parse_result = type`,
		`"%kizu.error.parse_node = type`,
		`"%kizu.error.parse_range = type`,
		`"%kizu.error.token = type`,
		`"%kizu.error.prefix_op = type`,
		`"%kizu.error.binary_op = type`,
		`"%kizu.selfhost.codegen.run_ast = type`,
		`"%kizu.error.run_ast = type`,
		`"%kizu.selfhost.codegen.value = type`,
		`"%kizu.selfhost.codegen.instruction = type`,
		`"%kizu.selfhost.codegen.block = type`,
		`"%kizu.selfhost.codegen.function = type`,
		`"%kizu.selfhost.codegen.program = type`,
		`"%kizu.selfhost.codegen.local_binding = type`,
		`"%kizu.selfhost.codegen.local_table = type`,
		`"%kizu.selfhost.codegen.code_eval = type`,
		`"%kizu.error.code_eval = type`,
		`"%kizu.selfhost.cli.parse_node_result = type`,
		`"%kizu.error.related_span = type`,
		`"%kizu.selfhost.codegen.payload_span = type`,
		`"%kizu.error.payload_span = type`,
		`"%kizu.selfhost.source.source_file = type`,
		`"%kizu.selfhost.types.constructor_facts.constructor_facts = type`,
		`"%kizu.selfhost.types.primitive_type.type_record = type`,
		`"%kizu.kizu.diagnostic.file_span = type`,
		`"%kizu.kizu.diagnostic.related_span = type`,
		`"%kizu.kizu.diagnostic.diagnostic = type`,
		`"%kizu.kizu.ast.source_file = type`,
		`"%kizu.kizu.ast.span = type`,
		`"%kizu.kizu.ast.token_id = type`,
		`"%kizu.kizu.ast.symbol_id = type`,
		`"%kizu.kizu.ast.child_range = type`,
		`"%kizu.kizu.ast.program_node = type`,
		`"%kizu.kizu.ast.int_node = type`,
		`"%kizu.kizu.ast.string_node = type`,
		`"%kizu.kizu.ast.type_name_node = type`,
		`"%kizu.kizu.ast.var_node = type`,
		`"%kizu.kizu.ast.bool_node = type`,
		`"%kizu.kizu.ast.prefix_node = type`,
		`"%kizu.kizu.ast.binary_node = type`,
		`"%kizu.kizu.ast.field_expr_node = type`,
		`"%kizu.kizu.ast.deref_expr_node = type`,
		`"%kizu.kizu.ast.call_node = type`,
		`"%kizu.kizu.ast.type_apply_expr_node = type`,
		`"%kizu.kizu.ast.cast_expr_node = type`,
		`"%kizu.kizu.ast.index_expr_node = type`,
		`"%kizu.kizu.ast.struct_literal_expr_node = type`,
		`"%kizu.kizu.ast.struct_field_init_node = type`,
		`"%kizu.kizu.ast.arena_new_expr_node = type`,
		`"%kizu.kizu.ast.try_expr_node = type`,
		`"%kizu.kizu.ast.comptime_expr_node = type`,
		`"%kizu.kizu.ast.block_node = type`,
		`"%kizu.kizu.ast.if_node = type`,
		`"%kizu.kizu.ast.let_node = type`,
		`"%kizu.kizu.ast.assign_node = type`,
		`"%kizu.kizu.ast.return_node = type`,
		`"%kizu.kizu.ast.defer_node = type`,
		`"%kizu.kizu.ast.err_defer_node = type`,
		`"%kizu.kizu.ast.expr_stmt_node = type`,
		`"%kizu.kizu.ast.while_node = type`,
		`"%kizu.kizu.ast.for_node = type`,
		`"%kizu.kizu.ast.break_node = type`,
		`"%kizu.kizu.ast.continue_node = type`,
		`"%kizu.kizu.ast.import_decl_node = type`,
		`"%kizu.kizu.ast.param_node = type`,
		`"%kizu.kizu.ast.field_node = type`,
		`"%kizu.kizu.ast.struct_decl_node = type`,
		`"%kizu.kizu.ast.enum_decl_node = type`,
		`"%kizu.kizu.ast.union_decl_node = type`,
		`"%kizu.kizu.ast.impl_decl_node = type`,
		`"%kizu.kizu.ast.union_variant_node = type`,
		`"%kizu.kizu.ast.match_node = type`,
		`"%kizu.kizu.ast.match_arm_node = type`,
		`"%kizu.kizu.ast.unsafe_node = type`,
		`"%kizu.kizu.ast.comptime_if_node = type`,
		`"%kizu.kizu.ast.fn_decl_node = type`,
		`"%kizu.kizu.lexer.position = type`,
		`"%kizu.kizu.lexer.token = type`,
		`"%kizu.kizu.parser.parse_node = type`,
		`"%kizu.kizu.parser.parse_range = type`,
		`"%kizu.kizu.ast.ast = type`,
		`"%kizu.kizu.ast.parse_result = type`,
		`"%kizu.kizu.ast.ast_node = type`,
	} {
		if strings.Contains(llvm, removed) {
			t.Fatalf("production LLVM retained superseded manual declaration %s", removed)
		}
	}
	cliLLVM := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	for _, removed := range []string{
		"fn append_run_ast_success_return(",
		"fn append_run_ast_error_union_fail_return(",
		"fn append_unsupported_run_ast_success_return(",
		"fn append_payload_span_success_return(",
		"fn append_empty_payload_span_success_return(",
	} {
		if strings.Contains(cliLLVM, removed) {
			t.Fatalf("production LLVM renderer retained dead RunAst helper %s", removed)
		}
	}
	compiledMIRLLVM := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_llvm.kizu")
	payloadSpanRenderer := selfhostKizuFunctionBody(
		t, compiledMIRLLVM, "fn append_multi_string_literal_span(",
	)
	if !strings.Contains(payloadSpanRenderer, "let return_type = function.return_type.llvm_name;") {
		t.Fatal("PayloadSpan renderer does not consume the fact-derived function return ABI")
	}
	for _, hardcoded := range []string{
		"%kizu.selfhost.codegen.payload_span",
		"%kizu.selfhost.ir.codegen.payload_span",
	} {
		if strings.Contains(payloadSpanRenderer, hardcoded) {
			t.Fatalf("PayloadSpan renderer retained hardcoded ABI %s", hardcoded)
		}
	}

	signatures := readSelfhostFile(t, "../../selfhost/src/ir/function_signature.kizu")
	for _, required := range []string{
		`if !comptime_param {`,
		`try out.append_bytes("reachable-type ");`,
	} {
		if !strings.Contains(signatures, required) {
			t.Fatalf("runtime signature roots missing %s", required)
		}
	}
	body := readSelfhostFile(t, "../../selfhost/src/ir/executable_body.kizu")
	for _, required := range []string{
		`StructLiteralExpr(struct_literal) => return append_body_reachable_type_fact(`,
		`TypeName(type_name) => return append_body_reachable_type_fact(`,
		`try out.append_bytes("reachable-type ");`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("body type roots missing %s", required)
		}
	}
	owners := readSelfhostFile(t, "../../selfhost/src/ir/package_dependency_graph.kizu")
	if !strings.Contains(owners, `try out.append_bytes("function-owner-module ");`) {
		t.Fatal("reachable roots are not linked to exact function owner modules")
	}
}

func loadSelfhostTypeDeclarationRegistryProgram(t *testing.T) *ast.Program {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	return program
}
