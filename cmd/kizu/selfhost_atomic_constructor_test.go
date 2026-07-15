package main

import (
	"strings"
	"testing"
)

type atomicConstructorSources struct {
	check, execute, cliRun, codegen, facts, renderer string
}

// loadAtomicConstructorSources reads the source boundaries shared by atomic constructor tests.
func loadAtomicConstructorSources(t *testing.T) atomicConstructorSources {
	t.Helper()
	return atomicConstructorSources{
		check:    readSelfhostFile(t, "../../selfhost/src/cli/check.kizu"),
		execute:  readSelfhostFile(t, "../../selfhost/src/cli/execute.kizu"),
		cliRun:   readSelfhostFile(t, "../../selfhost/src/backend/cli_run_llvm.kizu"),
		codegen:  readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu"),
		facts:    readSelfhostFile(t, "../../selfhost/src/types/constructor_facts.kizu"),
		renderer: readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_llvm.kizu"),
	}
}

// TestSelfhostAtomicConstructorHandoffHasNoSpellingFallbackOrEscapeABI pins the
// checked constructor identity handoff without spelling fallback or escaped ABI.
func TestSelfhostAtomicConstructorHandoffHasNoSpellingFallbackOrEscapeABI(t *testing.T) {
	sources := loadAtomicConstructorSources(t)
	assertAtomicCheckedHandoff(t, sources)
	assertAtomicScratchABI(t, sources)
	assertAtomicScratchHasNoNestedReceivers(t, sources)
	assertAtomicSelectionHasNoFallback(t, sources)
	assertConstructorFactsEntryContract(t, sources)
	assertConstructorIdentityMatching(t, sources)
	assertConstructorImportResolution(t, sources)
	assertConstructorResolutionLimits(t, sources)
	assertErrorUnionFailureLayout(t, sources.renderer)
}

// assertAtomicCheckedHandoff verifies facts are produced after the complete static check.
func assertAtomicCheckedHandoff(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	checkedBody := selfhostKizuFunctionBody(t, sources.check, "pub fn checked_ast_node(")
	fastCheck := strings.Index(checkedBody, "fast_diagnostics_ast_node")
	fullCheck := strings.Index(checkedBody, "full_static_diagnostics_loaded")
	collect := strings.Index(checkedBody, "constructor_facts::collect_checked")
	if fastCheck < 0 || fullCheck <= fastCheck || collect <= fullCheck {
		t.Fatal("checked_ast_node does not produce constructor identities after successful full check")
	}
	for _, forbidden := range []string{
		"FastDiagnosticContext", "fast_diagnostics_ast_node_with_context", "&facts.type_kinds",
	} {
		if strings.Contains(checkedBody, forbidden) {
			t.Fatalf("checked_ast_node duplicates fast diagnostic context %q", forbidden)
		}
	}
	runBody := selfhostKizuFunctionBody(t, sources.execute, "pub fn run_file_cli(")
	requireSourceFragments(t, "run identity handoff", runBody, []string{
		"constructor_facts::init(allocator)",
		"&var constructor_identities",
		"&constructor_identities",
		"atomic_constructor_id",
		"bool_type_id",
	})
}

// assertAtomicScratchABI verifies the four borrowed fact arrays reach codegen scratch state.
func assertAtomicScratchABI(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	lowerModule := selfhostKizuFunctionBody(t, sources.codegen, "pub fn lower_code_module(")
	scratchInit := selfhostKizuFunctionBody(t, sources.codegen, "fn scratch_init(")
	scratchSignature := "fn scratch_init(\n" +
		"    args_scratch: &var std::array::Array<i64>,\n" +
		"    node_starts: &std::array::Array<i64>,\n" +
		"    node_ends: &std::array::Array<i64>,\n" +
		"    constructor_ids: &std::array::Array<i64>,\n" +
		"    type_arg0_ids: &std::array::Array<i64>,"
	if !strings.Contains(sources.codegen, scratchSignature) {
		t.Fatal("constructor scratch does not take four borrowed array parameters")
	}
	requireSourceFragments(t, "constructor scratch normalization", scratchInit, []string{
		"node_starts.len()",
		"node_ends.len()",
		"constructor_ids.len()",
		"type_arg0_ids.len()",
		"node_starts.get(index)",
		"node_ends.get(index)",
		"let node_start = try node_starts.get(index)",
		"let node_end = try node_ends.get(index)",
		"args_scratch.append(node_start)",
		"args_scratch.append(node_end)",
		"constructor_ids.get(index)",
		"type_arg0_ids.get(index)",
		"var resolved_kind = 0 - 1",
	})
	requireSourceFragments(t, "constructor scratch borrowed fields", lowerModule, []string{
		"&constructor_identities.node_starts",
		"&constructor_identities.node_ends",
		"&constructor_identities.constructor_ids",
		"&constructor_identities.type_arg0_ids",
	})
}

// assertAtomicScratchHasNoNestedReceivers rejects unsupported nested Array receivers.
func assertAtomicScratchHasNoNestedReceivers(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	scratchInit := selfhostKizuFunctionBody(t, sources.codegen, "fn scratch_init(")
	forbidSourceFragments(t, "constructor scratch nested receiver", scratchInit, []string{
		"let node_starts = &constructor_identities.node_starts",
		"let node_ends = &constructor_identities.node_ends",
		"let constructor_ids = &constructor_identities.constructor_ids",
		"let type_arg0_ids = &constructor_identities.type_arg0_ids",
		"constructor_identities.node_starts.len()",
		"constructor_identities.node_ends.len()",
		"constructor_identities.constructor_ids.len()",
		"constructor_identities.type_arg0_ids.len()",
		"constructor_identities.node_starts.get(index)",
		"constructor_identities.node_ends.get(index)",
		"constructor_identities.constructor_ids.get(index)",
		"constructor_identities.type_arg0_ids.get(index)",
		"args_scratch.append(try node_starts.get(index))",
		"args_scratch.append(try node_ends.get(index))",
		"var resolved_kind = -1",
	})
}

// assertAtomicSelectionHasNoFallback rejects spelling and return-ABI escape paths.
func assertAtomicSelectionHasNoFallback(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	producer := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_known_identity_id(")
	lookup := selfhostKizuFunctionBody(t, sources.codegen, "fn scratch_constructor_kind(")
	for _, forbidden := range []string{"equal_bytes", "starts_with", "std::atomic::Atomic"} {
		if strings.Contains(lookup, forbidden) {
			t.Fatalf("codegen lookup contains spelling fallback %q", forbidden)
		}
	}
	if strings.Contains(producer, "starts_with") {
		t.Fatal("checked identity producer contains prefix spelling fallback")
	}
	returnKind := selfhostKizuFunctionBody(t, sources.codegen, "fn code_return_kind(")
	if strings.Contains(returnKind, "code_kind_atomic_bool") ||
		strings.Contains(returnKind, "Atomic") {
		t.Fatal("Atomic escaped into function return ABI support")
	}
}

// assertConstructorFactsEntryContract verifies the checked producer and hosted helper boundary.
func assertConstructorFactsEntryContract(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	collectBody := selfhostKizuFunctionBody(t, sources.facts, "pub fn collect_checked(")
	collectSignature := "pub fn collect_checked(\n" +
		"    text: []u8,\n" +
		"    ast: std::kizu::ast::Ast,\n" +
		"    root: std::kizu::ast::NodeId,\n" +
		"    facts: &var ConstructorFacts\n" +
		") -> !void"
	if !strings.Contains(sources.facts, collectSignature) {
		t.Fatal("checked constructor fact producer regained allocator or checker-map parameters")
	}
	if !strings.Contains(collectBody, "collect_node(text, ast, root, root, facts)") {
		t.Fatal("checked constructor fact collection does not directly traverse the checked root")
	}
	hostedHelper := selfhostKizuFunctionBody(t, sources.execute, "pub fn render_checked_run_artifact(")
	requireSourceFragments(t, "hosted constructor-facts helper", hostedHelper, []string{
		"constructor_facts::init(allocator)",
		"constructor_facts::collect_checked(",
		"constructor_facts::constructor_atomic()",
		"constructor_facts::type_bool()",
		"code_render::render_run_artifact(",
		"cannot yet lower ConstructorFacts aggregate deinit",
	})
	forbidSourceFragments(t, "static run constructor facts", sources.cliRun, []string{
		"@kizu_selfhost__ir_code_render_render_run_artifact",
		"constructor_facts::",
		"@kizu_selfhost__types_constructor_facts_init",
	})
	if !strings.Contains(sources.cliRun, "@kizu_selfhost__cli_execute_render_checked_run_artifact") {
		t.Fatal("static run LLVM does not cross the single four-argument compiled helper boundary")
	}
}

// assertConstructorIdentityMatching verifies exact known-name matching without identity maps.
func assertConstructorIdentityMatching(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	producer := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_known_identity_id(")
	appendResolved := selfhostKizuFunctionBody(t, sources.facts, "fn append_resolved(")
	requireSourceFragments(t, "known constructor identity", appendResolved, []string{
		`"std::atomic::Atomic"`,
		`constructor_atomic()`,
		`"bool"`,
		`type_bool()`,
	})
	forbidSourceFragments(t, "constructor identity allocation", sources.facts, []string{
		"IdentityMaps",
		"package_modules",
		"kinds.contains",
		"import_alias_starts = std::map::Map",
		"import_alias_ends = std::map::Map",
		"constructor_ids = std::map::Map",
		"type_ids = std::map::Map",
		"append_import_resolved_type_reference_name",
		"std::array::Array<u8>",
	})
	requireSourceFragments(t, "checked identity structural match", producer, []string{
		"std::mem::equal_bytes(name, known_name)",
		"first_qualified_segment_end(name)",
		"resolved_import_known_identity_id(",
		"let name_start = node.span.start",
		"let name_end = node.span.end",
		"text[name_start..name_end]",
	})
}

// assertConstructorImportResolution verifies structural root-import alias matching.
func assertConstructorImportResolution(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	producer := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_known_identity_id(")
	importRoot := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_known_identity_id(")
	importRange := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_range_identity_id(")
	importPath := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_path_identity_id(")
	requireSourceFragments(t, "constructor root import scan", importRoot, []string{
		"Program(program)",
		"program.declarations",
	})
	requireSourceFragments(t, "constructor first-import-wins scan", importRange, []string{
		"while index < declarations.len",
		"if result != no_alias_match() { return result; }",
	})
	requireSourceFragments(t, "constructor import structural match", importPath, []string{
		"let alias_start = last_node.span.start",
		"let alias_end = last_node.span.end",
		"text[alias_start..alias_end]",
		"std::mem::equal_bytes(name[0..first_end], alias)",
		"let module_start = first_node.span.start",
		"let module_end = last_node.span.end",
		"text[module_start..module_end]",
		"let name_end = std::mem::len(name)",
		"name[first_end..name_end]",
		"module_len + std::mem::len(suffix) != std::mem::len(known_name)",
		"std::mem::equal_bytes(module_name, known_name[0..module_len])",
		"let known_name_end = std::mem::len(known_name)",
		"std::mem::equal_bytes(suffix, known_name[module_len..known_name_end])",
	})
	forbidSourceFragments(t, "constructor identity slice bounds", producer+importPath, []string{
		"text[node.span.start..node.span.end]",
		"text[last_node.span.start..last_node.span.end]",
		"text[first_node.span.start..last_node.span.end]",
		"name[first_end..std::mem::len(name)]",
		"known_name[module_len..std::mem::len(known_name)]",
	})
}

// assertConstructorResolutionLimits rejects local allocation and arity-based selection.
func assertConstructorResolutionLimits(t *testing.T, sources atomicConstructorSources) {
	t.Helper()
	collectBody := selfhostKizuFunctionBody(t, sources.facts, "pub fn collect_checked(")
	producer := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_known_identity_id(")
	lookup := selfhostKizuFunctionBody(t, sources.codegen, "fn scratch_constructor_kind(")
	importRoot := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_known_identity_id(")
	importRange := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_range_identity_id(")
	importPath := selfhostKizuFunctionBody(t, sources.facts, "fn resolved_import_path_identity_id(")
	identityResolution := collectBody + producer + importRoot + importRange + importPath
	forbidSourceFragments(t, "constructor identity local allocation", identityResolution, []string{
		"std::array::Array", "std::map::Map", "std::string::String", "package_modules", "kinds.contains",
	})
	if strings.Contains(producer, "arities") || strings.Contains(lookup, "arity") {
		t.Fatal("constructor selection regressed to generic arity classification")
	}
}

// assertErrorUnionFailureLayout verifies message indices follow each error-union ABI.
func assertErrorUnionFailureLayout(t *testing.T, renderer string) {
	t.Helper()
	failureValue := selfhostKizuFunctionBody(t, renderer, "fn append_error_union_failure_value(")
	if !strings.Contains(failureValue, "error_union_msg_index_text(return_type)") {
		t.Fatal("generic error failure renderer does not derive the message field index")
	}
	if strings.Contains(failureValue, `append_bytes(", 2")`) {
		t.Fatal("generic error failure renderer keeps a fixed value-union message index")
	}
	messageIndex := selfhostKizuFunctionBody(t, renderer, "fn error_union_msg_index_text(")
	requireSourceFragments(t, "error union message index layout", messageIndex, []string{
		`equal_bytes(error_union, "%kizu.error.void")`,
		`return "1"`,
		`return "2"`,
	})
}

// TestSelfhostAtomicBoolTapeAndLLVMContract pins the Atomic<bool> tape shape and
// its byte-sized sequentially-consistent LLVM representation.
func TestSelfhostAtomicBoolTapeAndLLVMContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertAtomicBoolTapeContract(t, codegen)
	assertAtomicBoolLLVMContract(t, render)
}

// assertAtomicBoolTapeContract verifies identity routing and the fixed tape record shape.
func assertAtomicBoolTapeContract(t *testing.T, codegen string) {
	t.Helper()
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_atomic_bool_new(")
	requireSourceFragments(t, "Atomic<bool> lowering", lower, []string{
		"args.len != 1",
		"init_kind != code_kind_bool()",
		"code.append(code_op_atomic_bool_new())",
		"kinds.append(code_kind_atomic_bool())",
	})

	runtimeConstructors := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_constructor(")
	identityLookup := strings.Index(runtimeConstructors, "scratch_constructor_kind")
	legacyArena := strings.Index(runtimeConstructors, "lower_code_arena_new")
	if identityLookup < 0 || legacyArena <= identityLookup {
		t.Fatal("resolved constructor fact is not selected before spelling-based legacy constructors")
	}
	atomicGuard := strings.Index(runtimeConstructors, "if resolved_kind == atomic_kind")
	atomicReturn := strings.Index(runtimeConstructors, "return try lower_code_atomic_bool_new")
	if atomicGuard < 0 || atomicReturn <= atomicGuard || atomicReturn >= legacyArena {
		t.Fatal("known Atomic identity can fall through after an unsupported shape")
	}
}

// assertAtomicBoolLLVMContract verifies byte storage and sequentially-consistent rendering.
func assertAtomicBoolLLVMContract(t *testing.T, render string) {
	t.Helper()
	allocas := selfhostKizuFunctionBody(t, render, "fn render_var_allocas(")
	requireSourceFragments(t, "atomic entry alloca scan", allocas, []string{
		"code_op_atomic_bool_new()",
		"render_one_atomic_bool_alloca(out, atomic_slot)",
	})
	atomicAlloca := selfhostKizuFunctionBody(t, render, "fn render_one_atomic_bool_alloca(")
	if !strings.Contains(atomicAlloca, `" = alloca i8, align 1"`) {
		t.Fatal("atomic storage is not a byte-sized aligned entry alloca")
	}
	atomicWriter := selfhostKizuFunctionBody(t, render, "fn render_atomic_bool_new(")
	requireSourceFragments(t, "atomic LLVM writer", atomicWriter, []string{
		`" = getelementptr i8, ptr %atomic"`,
		`" = zext i1 %v"`,
		`"  store atomic i8 %ab"`,
		`" seq_cst, align 1"`,
	})
	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	if !strings.Contains(recordEnd, "code_op_atomic_bool_new()") ||
		!strings.Contains(recordEnd, "return index + 3") {
		t.Fatal("ATOMIC_BOOL_NEW is not a fixed three-slot tape record")
	}
	valueType := selfhostKizuFunctionBody(t, render, "fn render_value_type(")
	if !strings.Contains(valueType, "is_atomic_bool") ||
		!strings.Contains(valueType, `try w(out, "ptr")`) ||
		!strings.Contains(valueType, "!is_atomic_bool") {
		t.Fatal("Atomic<bool> tape kind does not remain a ptr value")
	}
}

// forbidSourceFragments reports source fragments that must remain absent.
func forbidSourceFragments(t *testing.T, label, source string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(source, fragment) {
			t.Errorf("%s keeps %q", label, fragment)
		}
	}
}
