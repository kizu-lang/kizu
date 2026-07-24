package main

import (
	"strings"
	"testing"
)

func TestSelfhostReachableComponentTypeFacts(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::ir::package_type_facts_gates::component_type_facts_gate",
	)
	if err != nil {
		t.Fatalf("component type facts gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "generic component type facts", out, []string{
		"type-llvm app::model::Child %kizu.app.model.child",
		"struct-field app::model::Child 0 value i64",
		"type-llvm app::model::Model %kizu.app.model.model",
		"struct-field app::model::Model 0 child Child",
		"struct-field app::model::Model 1 items std::array::Array<Model>",
		"struct-field app::model::Model 2 bytes std::channel::Channel<[]u8>",
		"struct-field app::model::Model 3 models std::channel::Channel<std::array::Array<Model>>",
		"struct-field app::model::Model 4 by_name std::map::Map<[]u8,Model>",
		"declared-type-parameter app::model::Tokens 0 V",
		"declared-type-parameter app::model::Tokens 1 _V",
		"struct-field app::model::Tokens 0 plain std::map::Map<[]u8,V>",
		"struct-field app::model::Tokens 1 underscored std::map::Map<[]u8,_V>",
		"enum-type app::model::State i64",
		"enum-variant app::model::State Ready 0",
		"enum-variant app::model::State Done 1",
		"union-type app::model::Event %kizu.app.model.event",
		"union-variant app::model::Event Empty 0 void",
		"union-variant app::model::Event Model 1 Model",
		"union-variant app::model::Event Bytes 2 []u8",
		"declared-type-parameter app::model::GenericEvent 0 K",
		"declared-type-parameter app::model::GenericEvent 1 V",
		"union-type app::model::GenericEvent %kizu.app.model.generic_event",
		"union-variant app::model::GenericEvent Empty 0 void",
		"union-variant app::model::GenericEvent Entry 1 std::map::Map<K,V>",
	})
	lines := strings.Split(out, "\n")
	enumType := "enum-type app::model::State i64"
	firstVariant := "enum-variant app::model::State Ready 0"
	headerIndex := -1
	for index, line := range lines {
		if line == enumType {
			if headerIndex >= 0 {
				t.Fatalf("enum ABI header emitted more than once: %q", enumType)
			}
			headerIndex = index
		}
	}
	if headerIndex < 0 {
		t.Fatalf("exact enum i64 ABI line missing:\n%s", out)
	}
	if headerIndex+1 >= len(lines) {
		t.Fatalf("enum ABI header has no following variant fact:\n%s", out)
	}
	if lines[headerIndex+1] != firstVariant {
		t.Fatalf(
			"enum header and first variant are not separate adjacent facts\nheader=%q\nnext=%q",
			enumType, lines[headerIndex+1],
		)
	}
}

func TestSelfhostEnumTypeFactUsesExplicitNewlineByte(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/package_type_facts.kizu")
	body := selfhostKizuFunctionBody(t, source, "fn append_enum_name(")
	requireSourceFragments(t, "enum fact line termination", body, []string{
		`out.append_bytes(" i64")`,
		"out.append_byte(cast<u8>(10))",
	})
	if strings.Contains(body, `append_bytes(" i64\n")`) {
		t.Fatal("enum fact emitter relies on an undecoded backslash-newline escape")
	}
}

func TestSelfhostExternalABIRepresentationHasSingleOwner(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::ir::package_type_facts_gates::external_abi_owner_gate",
	)
	if err != nil {
		t.Fatalf("external ABI owner gate failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "abi-repr std::string::String %kizu.owned") {
		t.Fatalf("external ABI fact missing:\n%s", out)
	}
	for _, fact := range []string{
		"abi-repr ptr ptr",
		"nullable-abi-repr ptr ptr",
		"abi-repr []u8 %kizu.slice.u8",
	} {
		if !strings.Contains(out, fact) {
			t.Fatalf("generic intrinsic ABI fact %q missing:\n%s", fact, out)
		}
	}
	if strings.Contains(out, "type-llvm std::string::String") {
		t.Fatalf("external ABI identity retained a competing nominal owner:\n%s", out)
	}
}

func TestSelfhostExternalABIContractIsUnifiedAndFailClosed(t *testing.T) {
	contractSource := readSelfhostFile(
		t, "../../selfhost/src/ir/intrinsic_type_contract.kizu",
	)
	requireSourceFragments(t, "unified external ABI contract", contractSource, []string{
		"pub struct ExternalAbiContract",
		"pub identity: []u8",
		"pub fixed_abi: fixed_abi_contract::FixedAbi",
		"pub generic_arity: i64",
		"pub dependence: ExternalAbiDependence",
		"pub nullable: bool",
		"pub owned_family: []u8",
		"pub fn external_abi_contract_at(index: i64)",
	})
	for _, removed := range []string{
		"external_abi_identity_at",
		"external_abi_representation_at",
		"external_abi_is_nullable_at",
		"external_abi_owned_family_at",
	} {
		if strings.Contains(contractSource, removed) {
			t.Fatalf("parallel external ABI lookup remains: %q", removed)
		}
	}
	factsSource := readSelfhostFile(t, "../../selfhost/src/ir/package_type_facts.kizu")
	emitter := selfhostKizuFunctionBody(
		t, factsSource, "pub fn append_core_abi_representation_facts(",
	)
	requireSourceFragments(t, "external ABI record consumer", emitter, []string{
		"external_abi_contract_at(index)",
		"entry.identity",
		"entry.fixed_abi.llvm_name",
		"entry.owned_family",
		"entry.nullable",
		"entry.dependence",
	})

	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::ir::package_type_facts_gates::external_abi_contract_gate",
	)
	if err != nil {
		t.Fatalf("external ABI contract gate failed: %v\n%s", err, out)
	}
	if out != "ok\n" {
		t.Fatalf("external ABI contract gate output = %q, want ok", out)
	}

	out, err = runSelfhostAbiParamsGate(
		t,
		"selfhost::ir::package_type_facts_gates::"+
			"external_abi_contract_out_of_range_gate",
	)
	if err == nil {
		t.Fatalf("out-of-range external ABI contract was accepted\n%s", out)
	}
	if !strings.Contains(err.Error(), "external ABI index out of range") {
		t.Fatalf("out-of-range external ABI contract error mismatch: %v", err)
	}
}

func TestSelfhostPackageTypeCatalogIncludesTypeOnlyComponents(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::ir::package_type_facts_gates::package_component_type_facts_gate",
	)
	if err != nil {
		t.Fatalf("package component type facts gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "package-wide type facts", out, []string{
		"type-llvm selfhost::backend::data::RunArtifact %kizu.selfhost.backend.data.run_artifact",
		"struct-field selfhost::backend::data::RunArtifact 0 bytes i64",
		"struct-field selfhost::backend::data::RunArtifact 1 metadata_bytes i64",
		"enum-type selfhost::backend::data::OutputStream i64",
		"enum-variant selfhost::backend::data::OutputStream None 0",
		"enum-variant selfhost::backend::data::OutputStream Stdout 1",
	})
}

func TestSelfhostPackageQueueComponentAccessor(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::package_dependency_edge_gate::queue_target_component_gate",
	)
	if err != nil {
		t.Fatalf("queue component gate failed: %v\n%s", err, out)
	}
	if out != "queue-component\n0\n" {
		t.Fatalf("queue component gate output mismatch: %q", out)
	}
}

func TestSelfhostNumericCollectorHasNoNamedTypeSeeds(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	body := selfhostKizuFunctionBody(t, source, "fn append_numeric_package_closure(")
	for _, forbidden := range []string{
		`"ConstructorFacts"`, `"TypeRecord"`,
		`append_qualified_struct_type_facts(`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("numeric closure retained named type seed %s", forbidden)
		}
	}
}

func TestSelfhostFunctionTypeParametersUseCatalogFacts(t *testing.T) {
	emitter := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	body := selfhostKizuFunctionBody(t, emitter, "fn append_function_type_parameter_facts(")
	requireSourceFragments(t, "function type parameter fact emitter", body, []string{
		`out.append_bytes("function-type-parameter ")`,
		"package_catalog::function_type_parameter_len(",
		"package_catalog::function_type_parameter_name(",
	})

	declarations := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_declarations.kizu")
	formalCheck := selfhostKizuFunctionBody(t, declarations, "fn unresolved_function_formal_type(")
	requireSourceFragments(t, "function-scoped declaration traversal", formalCheck, []string{
		"compiled_type_resolver::is_function_type_parameter_indexed(",
		"function_name",
	})
	for _, forbidden := range []string{`"T"`, `"K"`, `"V"`} {
		if strings.Contains(formalCheck, forbidden) {
			t.Fatalf("function formal traversal hardcodes type parameter %s", forbidden)
		}
	}
}

func TestSelfhostAstDataUnionLayoutIsFactDerived(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	for _, legacy := range []string{
		"append_ast_function_facts",
		`append_abi_repr_fact(out, "std::kizu::ast::AstData"`,
		`append_type_llvm_fact(out, "std::kizu::ast::AstData"`,
	} {
		if strings.Contains(source, legacy) {
			t.Fatalf("frontend retains AST-specific ABI producer %q", legacy)
		}
	}

	llvm := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	if strings.Contains(llvm, `"%kizu.kizu.ast.ast_data = type`) {
		t.Fatal("AstData retained a static LLVM declaration instead of fact-derived layout")
	}
	declarations := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_declarations.kizu")
	if !strings.Contains(declarations, "compiled_type_layout::union_payload_capacity(") {
		t.Fatal("reachable declaration registry does not derive union payload capacity")
	}
}

func TestSelfhostMatchUnionConsumerCarriesResolvedABI(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	resolver := selfhostKizuFunctionBody(t, lower, "fn resolve_node_field_fetch_abi(")
	requireSourceFragments(t, "match union ABI resolver", resolver, []string{
		"compiled_type_resolver::resolve_call_indexed(",
		"compiled_fact_lookup::lookup_struct_field_exact_indexed(",
		"get_function.return_type.canonical_identity",
	})
	matchResolver := selfhostKizuFunctionBody(t, lower, "fn resolve_match_union_abi(")
	if !strings.Contains(matchResolver, "compiled_type_resolver::exact_union_type_indexed(") {
		t.Fatal("match union resolver does not validate the exact union owner")
	}

	mir := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir.kizu")
	for _, field := range []string{
		"pub get_module: []u8", "pub get_name: []u8",
		"pub node_type: []u8", "pub union_type: []u8",
	} {
		if strings.Count(mir, field) < 2 {
			t.Fatalf("both match MIR consumers must carry %q", field)
		}
	}

	renderer := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_llvm.kizu")
	for _, fn := range []string{"fn append_match_union_block(", "fn append_multi_match_traversal("} {
		body := selfhostKizuFunctionBody(t, renderer, fn)
		requireSourceFragments(t, fn, body, []string{
			".node_type", ".union_type", ".get_module", ".get_name",
		})
		for _, forbidden := range []string{
			"std::kizu::ast::AstData", "%kizu.kizu.ast.ast_data",
			"@kizu_kizu__ast_ast_get",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s retained hardcoded union consumer ABI %q", fn, forbidden)
			}
		}
	}
}

func TestSelfhostImportedMatchUnionABIUsesCalleeAndFieldOwners(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_lower::gate_imported_match_union_abi",
	)
	if err != nil {
		t.Fatalf("imported match union ABI gate failed: %v\n%s", err, out)
	}
	want := "lib::defs::\nfetch\n%test.node\nlib::defs::Event\n%test.event\n0\n"
	if out != want {
		t.Fatalf("imported match union ABI mismatch\nwant:\n%sgot:\n%s", want, out)
	}
}

func TestSelfhostResolvedMatchUnionMIRRendersOnlyCarriedABI(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::resolved_match_union_renderer_gate",
	)
	if err != nil {
		t.Fatalf("resolved match union renderer gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "resolved match union LLVM", out, []string{
		"call %test.node @kizu_lib__defs_fetch(%test.tree %tree, i64 %id)",
		"extractvalue %test.node %match_node, 0",
		"extractvalue %test.event %match_data, 0",
		"alloca %test.event, align 8",
		"getelementptr %test.event, ptr %match_payload_slot",
	})
	for _, forbidden := range []string{
		"kizu_kizu__ast_ast_get", "%kizu.kizu.ast.ast_node", "%kizu.kizu.ast.ast_data",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("resolved match renderer emitted hardcoded AST ABI %q\n%s", forbidden, out)
		}
	}
}

func TestSelfhostImportedNodeSpanABIUsesCalleeAndFieldOwners(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_lower::gate_imported_node_span_abi",
	)
	if err != nil {
		t.Fatalf("imported node span ABI gate failed: %v\n%s", err, out)
	}
	want := "lib::defs::\nfetch\n%test.tree\n%test.index\n%test.node\n%test.range\n3\n2\n5\n"
	if out != want {
		t.Fatalf("imported node span ABI mismatch\nwant:\n%sgot:\n%s", want, out)
	}
}

func TestSelfhostNodeSpanMIRRenderersUseOnlyCarriedABI(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::resolved_node_span_renderer_gate",
	)
	if err != nil {
		t.Fatalf("resolved node span renderer gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "resolved node span LLVM", out, []string{
		"call %test.node @kizu_lib__defs_fetch(%test.tree %tree, %test.index %id)",
		"extractvalue %test.node %ant_node, 3",
		"extractvalue %test.range %ant_span, 2",
		"extractvalue %test.range %ant_span, 5",
		"extractvalue %test.node %sls_node, 3",
		"%pps_ast_node = call %test.node @kizu_lib__defs_fetch(",
		"%test.index %pps_node_id",
	})
	for _, forbidden := range []string{
		"kizu_kizu__ast_ast_get", "%kizu.kizu.ast.ast_node", "%kizu.kizu.ast.span",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("node span renderer emitted hardcoded AST ABI %q\n%s", forbidden, out)
		}
	}
}

func TestSelfhostCountRangeMIRRendererUsesOnlyCarriedABI(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::resolved_count_range_renderer_gate",
	)
	if err != nil {
		t.Fatalf("resolved count range renderer gate failed: %v\n%s", err, out)
	}
	requireSourceFragments(t, "resolved count range LLVM", out, []string{
		"extractvalue %test.range %range, 4",
		"call %test.error.child @kizu_lib__defs_child(",
		"%test.range %range, i64 %index",
		"call %test.error.i64 @kizu_app__count(%test.tree %tree, %test.child %child)",
	})
	for _, forbidden := range []string{
		"kizu_kizu__ast_ast_child_at", "%kizu.kizu.ast.child_range", "%kizu.kizu.ast.node_id",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("count range renderer emitted hardcoded AST ABI %q\n%s", forbidden, out)
		}
	}
}

func TestSelfhostRemainingSourceDefinedABILiteralsAreResolved(t *testing.T) {
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	for _, fn := range []string{
		"pub fn lower_token_kind_predicate_function(",
		"pub fn lower_first_token_function(",
		"pub fn lower_next_token_function(",
	} {
		body := selfhostKizuFunctionBody(t, lower, fn)
		for _, forbidden := range []string{
			"%kizu.kizu.lexer.token", "%kizu.kizu.lexer.position",
			"kizu_kizu__lexer_position", "kizu_kizu__lexer_advance_position",
			"kizu_kizu__lexer_token_at",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s retained source-defined ABI literal %q", fn, forbidden)
			}
		}
	}

	nextRenderer := selfhostKizuFunctionBody(t,
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_llvm.kizu"),
		"fn append_multi_next_token(",
	)
	for _, field := range []string{
		"stmt.start_field_index", "stmt.end_field_index",
		"stmt.line_field_index", "stmt.column_field_index",
		"stmt.position_module", "stmt.advance_position_module", "stmt.token_at_module",
	} {
		if !strings.Contains(nextRenderer, field) {
			t.Fatalf("next-token renderer does not consume resolved field %q", field)
		}
	}
	for _, forbidden := range []string{"append_ll_line(out, \", 1\")", "append_ll_line(out, \", 2\")", "append_ll_line(out, \", 3\")", "append_ll_line(out, \", 4\")"} {
		if strings.Contains(nextRenderer, forbidden) {
			t.Fatalf("next-token renderer retained fixed source field index %q", forbidden)
		}
	}

	commentEmitter := selfhostKizuFunctionBody(t,
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_struct_cf.kizu"),
		"fn cf_emit_comment_state_success(",
	)
	if strings.Contains(commentEmitter, "%kizu.selfhost.parser.format.comment_format_state") {
		t.Fatal("comment-state emitter retained a source-defined LLVM type literal")
	}
	for _, field := range []string{"state_type", "last_index", "at_line_start_index", "after_comment_index"} {
		if !strings.Contains(commentEmitter, field) {
			t.Fatalf("comment-state emitter does not consume resolved field %q", field)
		}
	}
}

func TestSelfhostCustomLexerAndCommentStateABIAreCarried(t *testing.T) {
	kind, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_lower::gate_custom_token_kind_field_abi",
	)
	if err != nil {
		t.Fatalf("custom token-kind ABI gate failed: %v\n%s", err, kind)
	}
	wantKind := "%test.lexeme\nlib::lexer::\nCategory\n6\n4\n"
	if kind != wantKind {
		t.Fatalf("custom token-kind ABI mismatch\nwant:\n%sgot:\n%s", wantKind, kind)
	}

	comment, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_struct_cf::gate_custom_comment_state_abi",
	)
	if err != nil {
		t.Fatalf("custom comment-state ABI gate failed: %v\n%s", err, comment)
	}
	wantComment := "true\nlib::format::State\n%test.comment.state\n2\n0\n1\n"
	if comment != wantComment {
		t.Fatalf("custom comment-state ABI mismatch\nwant:\n%sgot:\n%s", wantComment, comment)
	}

	lexer, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::resolved_lexer_wrapper_renderer_gate",
	)
	if err != nil {
		t.Fatalf("custom lexer wrapper ABI gate failed: %v\n%s", err, lexer)
	}
	requireSourceFragments(t, "custom lexer wrapper ABI", lexer, []string{
		"call %test.position @kizu_lib__geo_make_position(i16 1, i16 1)",
		"call %test.token @kizu_lib__scan_read_token(%test.bytes %bytes, i32 0, %test.position %ft_pos)",
		"extractvalue %test.token %previous, 5",
		"extractvalue %test.token %previous, 2",
		"extractvalue %test.token %previous, 7",
		"extractvalue %test.token %previous, 4",
		"call %test.position @kizu_lib__geo_advance(%test.bytes %bytes, i32 %nt_pstart, i32 %nt_pend, %test.position %nt_pos)",
	})
	for _, forbidden := range []string{
		"%kizu.kizu.lexer.token", "%kizu.kizu.lexer.position",
		"kizu_kizu__lexer_position", "kizu_kizu__lexer_token_at",
	} {
		if strings.Contains(lexer, forbidden) {
			t.Fatalf("custom lexer wrapper emitted fixed ABI %q\n%s", forbidden, lexer)
		}
	}
}

func TestSelfhostCountRangeCarriesDerivedChildErrorABI(t *testing.T) {
	mir := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir.kizu")
	if !strings.Contains(mir, "pub child_error_union: []u8") {
		t.Fatal("count-range MIR does not carry the child call's exact error ABI")
	}
	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	body := selfhostKizuFunctionBody(t, lower, "pub fn lower_count_range_function(")
	if !strings.Contains(body, "compiled_mir_types::lower_call_info_for_instance_indexed(") ||
		!strings.Contains(body, "compiled_mir_types::call_info_error_success_llvm(") ||
		!strings.Contains(body, "child_info.return_llvm_type") {
		t.Fatal("count-range lowering does not derive the child error ABI from canonical facts")
	}
	if strings.Contains(body, "%kizu.error.node_id") ||
		strings.Contains(body, "%kizu.error.kizu.kizu.ast.node_id") {
		t.Fatal("count-range lowering retained a handwritten NodeId error ABI name")
	}
}

func TestSelfhostSelectedComponentsUseCanonicalTypeCatalog(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	appendFacts := selfhostKizuFunctionBody(t, source, "fn append_facts_from_parsed(")
	if !strings.Contains(appendFacts, "append_package_component_type_facts(") {
		t.Fatal("append_facts does not build the package-wide type catalog")
	}
	numeric := selfhostKizuFunctionBody(t, source, "fn append_numeric_package_closure(")
	if strings.Contains(numeric, "append_component_type_facts(") ||
		strings.Contains(numeric, "emitted_type_components") {
		t.Fatal("numeric function closure still owns component type discovery")
	}
	for _, forbidden := range []string{
		"append_loader_function_facts(", "append_diagnostic_function_facts(",
		"append_codegen_function_facts", "append_selected_helper_body(",
		"stdlib-return-generic ",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy selected/type-specific producer remains %s", forbidden)
		}
	}
}

func TestSelfhostLegacyAstTypeFactHelpersAreDeleted(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	for _, deadHelper := range []string{
		"append_ast_leaf_payload_struct_type_facts",
		"append_ast_struct_type_fact",
		"emit_ast_node_type_llvm_fact",
		"emit_ast_node_struct_field_facts",
		"emit_ast_node_struct_fields",
		"emit_ast_node_struct_field",
		"find_ast_node_struct",
		"find_ast_node_struct_in_program",
		"ast_node_struct_name_matches",
		"struct_name_is_variant_node",
	} {
		if strings.Contains(source, deadHelper) {
			t.Fatalf("legacy AST type-fact helper remains: %s", deadHelper)
		}
	}
}

func TestSelfhostCliEmittersCarryDerivedNominalErrorABI(t *testing.T) {
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")
	body := selfhostKizuFunctionBody(t, cli, "pub fn append_functions(")
	if strings.Count(body, "compiled_type_lower::error_union_inner_to_llvm_indexed(") != 2 {
		t.Fatal("CLI lowering does not derive ParseResult and Executable error ABIs from canonical facts")
	}
	for _, fragment := range []string{
		"cli_ast_boundary_llvm::append_functions(",
		"source_file_abi",
		"parse_result_abi",
		"ast_abi",
		"node_id_abi",
		"compiled_type_lower::kizu_type_to_llvm_indexed(",
		"executable_error_abi",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("CLI lowering does not thread derived error ABI with %q", fragment)
		}
	}

	combined := cli +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_ast_boundary_llvm.kizu") +
		readSelfhostFile(t, "../../selfhost/src/backend/cli_test_llvm.kizu")
	for _, forbidden := range []string{
		"%kizu.error.node_id",
		"%kizu.error.parse_result",
		"%kizu.error.kizu.kizu.ast.node_id",
		"%kizu.error.kizu.kizu.ast.parse_result",
		"append_loop_then_block_ok_function",
		"append_loop_print_call_ok_function",
		"append_payload_span_child_fail_return",
		"append_i64_child_fail_return",
		"append_run_ast_child_fail_return",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("CLI emitter retained handwritten or dead nominal error ABI path %q", forbidden)
		}
	}
}

func TestSelfhostErrorAbiFacts(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_error_abi::gate",
	)
	if err != nil {
		t.Fatalf("error ABI facts gate failed: %v\n%s", err, out)
	}
	const want = "error-llvm void %kizu.error.void\n" +
		"error-llvm i1 %kizu.error.bool\n" +
		"error-llvm i8 %kizu.error.u8\n" +
		"error-llvm i16 %kizu.error.i16\n" +
		"error-llvm i32 %kizu.error.i32\n" +
		"error-llvm i64 %kizu.error.i64\n" +
		"error-llvm float %kizu.error.f32\n" +
		"error-llvm double %kizu.error.f64\n" +
		"error-llvm ptr %kizu.error.ptr\n" +
		"error-llvm %kizu.slice.u8 %kizu.error.slice.u8\n" +
		"error-llvm %kizu.owned %kizu.error.owned\n" +
		"error-llvm %kizu.handle %kizu.error.kizu.handle\n" +
		"error-llvm %kizu.app.alpha.model %kizu.error.kizu.app.alpha.model\n" +
		"error-llvm %kizu.app.beta.model %kizu.error.kizu.app.beta.model\n\n"
	if out != want {
		t.Fatalf("error ABI facts output mismatch\nwant:\n%sgot:\n%s", want, out)
	}
}

func TestSelfhostErrorAbiDerivationBorrowsFactCatalog(t *testing.T) {
	errorABI := readSelfhostFile(t, "../../selfhost/src/backend/compiled_error_abi.kizu")
	if !strings.Contains(errorABI,
		"pub fn derive_facts(source: &std::string::String) -> !std::string::String") {
		t.Fatal("error ABI derivation does not borrow its source fact catalog")
	}
	derive := selfhostKizuFunctionBody(t, errorABI, "pub fn derive_facts(")
	if strings.Contains(derive, "source.deinit()") {
		t.Fatal("borrowed error ABI derivation still destroys its source fact catalog")
	}
	gate := selfhostKizuFunctionBody(t, errorABI, "pub fn gate(")
	for _, fragment := range []string{
		"defer source_facts.deinit()",
		"derive_facts(&source_facts)",
		"if source_facts.len() != source_len",
	} {
		if !strings.Contains(gate, fragment) {
			t.Fatalf("error ABI behavior gate does not pin borrowed source lifetime with %q", fragment)
		}
	}

	executable := readSelfhostFile(t, "../../selfhost/src/ir/executable_functions.kizu")
	production := selfhostKizuFunctionBody(t, executable, "fn append_facts_from_parsed(")
	if !strings.Contains(production, "compiled_error_abi::derive_facts(out)") {
		t.Fatal("production error ABI derivation does not borrow the existing fact catalog")
	}
	for _, forbidden := range []string{"copy_fact_bytes", "error_abi_source"} {
		if strings.Contains(executable, forbidden) {
			t.Fatalf("production fact path retains full-catalog copy helper %q", forbidden)
		}
	}
}

func TestSelfhostFixedABIContractOwnsScalarClassification(t *testing.T) {
	contract := readSelfhostFile(t, "../../selfhost/src/abi/fixed_abi_contract.kizu")
	requireSourceFragments(t, "fixed float ABI contract", contract, []string{
		"Float32,",
		"Float64,",
		`equal_bytes(spelling, "f32")`,
		`equal_bytes(spelling, "f64")`,
		`equal_bytes(llvm_name, "float")`,
		`equal_bytes(llvm_name, "double")`,
		`kind: kind, llvm_name: "float", llvm_body: "", size: 4, alignment: 4`,
		`kind: kind, llvm_name: "double", llvm_body: "", size: 8, alignment: 8`,
	})

	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	for _, signature := range []string{
		"fn resolve_node_span_fetch_abi(",
		"pub fn lower_count_range_function(",
	} {
		body := selfhostKizuFunctionBody(t, lower, signature)
		if !strings.Contains(body, "fixed_abi_contract::from_llvm(") {
			t.Fatalf("%s does not classify fixed scalar ABI through the contract", signature)
		}
		for _, forbidden := range []string{`equal_bytes(start_type.abi, "i64")`, `equal_bytes(end_type.abi, "i64")`, `equal_bytes(range_len_type.abi, "i64")`, `child_types.get(2), "i64"`} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s retains raw scalar ABI classification %q", signature, forbidden)
			}
		}
	}

	comment := selfhostKizuFunctionBody(t,
		readSelfhostFile(t, "../../selfhost/src/backend/compiled_struct_cf.kizu"),
		"pub fn comment_state_abi_indexed(",
	)
	for _, fragment := range []string{
		"fixed_abi_contract::from_llvm(last_type.abi)",
		"fixed_abi_contract::FixedAbiKind::Integer8",
		"fixed_abi_contract::FixedAbiKind::Bool",
	} {
		if !strings.Contains(comment, fragment) {
			t.Fatalf("comment state ABI classification missing %q", fragment)
		}
	}
	for _, forbidden := range []string{`equal_bytes(last_type.abi, "i8")`, `equal_bytes(at_line_start_type.abi, "i1")`, `equal_bytes(after_comment_type.abi, "i1")`} {
		if strings.Contains(comment, forbidden) {
			t.Fatalf("comment state retains raw scalar ABI classification %q", forbidden)
		}
	}

	errorABI := readSelfhostFile(t, "../../selfhost/src/backend/compiled_error_abi.kizu")
	knownName := selfhostKizuFunctionBody(t, errorABI, "fn known_name(")
	if !strings.Contains(knownName, "fixed_abi_contract::from_llvm(payload_abi)") {
		t.Fatal("error ABI naming does not classify payloads through the fixed contract")
	}
	for _, fixedScalar := range []string{`"void"`, `"i1"`, `"i8"`, `"i16"`, `"i32"`, `"i64"`, `"float"`, `"double"`, `"ptr"`, `"%kizu.slice.u8"`, `"%kizu.owned"`, `"%kizu.handle"`} {
		if strings.Contains(knownName, "equal_bytes(payload_abi, "+fixedScalar+")") {
			t.Fatalf("error ABI naming retains duplicate raw scalar classification %s", fixedScalar)
		}
	}
	for _, fragment := range []string{
		"append_fixed_seed_fact(&var out, fixed_abi_contract::FixedAbiKind::Float32)",
		"append_fixed_seed_fact(&var out, fixed_abi_contract::FixedAbiKind::Float64)",
		"fixed_abi_contract::from_llvm(bytes[start..end])",
	} {
		if !strings.Contains(errorABI, fragment) {
			t.Fatalf("error ABI fixed seed ownership missing %q", fragment)
		}
	}
}

func TestSelfhostCompiledTypeLowerHasNoTypeAllowlist(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/backend/compiled_type_lower.kizu")
	for _, forbidden := range []string{
		"error_union_type_llvm_direct_or_empty",
		"error_union_enum_abi_or_empty",
		"non_error_type_llvm_direct_or_empty",
		`"String"`, `"NodeId"`, `"Token"`, `"ParseNode"`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("compiled type lowering retained handwritten type branch %s", forbidden)
		}
	}
	if !strings.Contains(source, "compiled_error_abi::lookup_indexed(") {
		t.Fatal("compiled type lowering does not use exact error ABI facts")
	}
}
