package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

func TestSelfhostCanonicalFactsStrictValidation(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{
		"valid_gate",
		"declared_fixed_abi_gate",
		"borrow_var_spelling_gate",
		"nested_generic_spelling_gate",
		"declared_generic_runtime_abi_gate",
		"runtime_abi_direct_resolution_gate",
		"error_result_receiver_child_gate",
		"print_runtime_abi_gate",
		"direction_neutral_scalar_codec_gate",
		"aggregate_scalar_storage_rejected_gate",
	} {
		var out bytes.Buffer
		if err := interp.New(&out).RunEntry(
			program, "selfhost::backend::compiled_canonical_facts::"+entry,
		); err != nil {
			t.Fatalf("canonical facts gate %s failed: %v\n%s", entry, err, out.String())
		}
	}
	for _, tc := range []struct {
		entry string
		want  string
	}{
		{"missing_type_gate", "exact one-to-one coverage"},
		{"phantom_type_gate", "exact one-to-one coverage"},
		{"duplicate_classification_gate", "duplicate or conflicting"},
		{"numeric_overflow_gate", "numeric token overflow"},
		{"typed_error_spelling_gate", "cannot erase typed error"},
		{"declared_generic_arity_mismatch_gate", "declared type argument arity mismatch"},
		{"mismatched_error_result_receiver_child_gate", "runtime result receiver child selector mismatch"},
		{"unsupported_print_runtime_abi_gate", "print type unsupported"},
	} {
		var out bytes.Buffer
		err := interp.New(&out).RunEntry(
			program, "selfhost::backend::compiled_canonical_facts::"+tc.entry,
		)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("canonical facts gate %s error = %v, want %q", tc.entry, err, tc.want)
		}
	}
}

func TestSelfhostRuntimeStorageCodec(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_mir_llvm_call::storage_codec_gate",
	); err != nil {
		t.Fatalf("runtime storage codec gate failed: %v\n%s", err, out.String())
	}
	for _, fragment := range []string{
		"%arg0_0_storage = zext i1 %flag to i64",
		"%arg0_1_storage = zext i1 true to i64",
		"%arg0_2_storage = sext i32 -7 to i64",
		"%arg0_3_call = call i1 @predicate()",
		"%arg0_3_storage = zext i1 %arg0_3_call to i64",
		"%arg0_4_byte_slot = alloca i1",
		"store i1 %flag, ptr %arg0_4_byte_slot",
		"%arg0_5_byte_slot = alloca %kizu.slice.u8",
		"store %kizu.slice.u8 %text, ptr %arg0_5_byte_slot",
		"%arg0_6_byte_slot = alloca %app.pair",
		"store %app.pair %pair, ptr %arg0_6_byte_slot",
		"ptrtoint (ptr getelementptr (%app.pair, ptr null, i32 1) to i64)",
		"%kizu.slice.u8 %arg0_4_storage, %kizu.slice.u8 %arg0_5_storage, %kizu.slice.u8 %arg0_6_storage",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("scalar storage codec output missing %q\n%s", fragment, out.String())
		}
	}
}

func TestSelfhostRuntimeResultStorageCodec(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_mir_llvm::byte_view_result_renderer_gate",
	); err != nil {
		t.Fatalf("runtime result storage codec gate failed: %v\n%s", err, out.String())
	}
	for _, fragment := range []string{
		"call %kizu.error.slice.u8 @kizu_rt_map_get",
		"_nonnull = icmp ne ptr",
		"ptrtoint (ptr getelementptr (i1, ptr null, i32 1) to i64)",
		"ptrtoint (ptr getelementptr (%kizu.slice.u8, ptr null, i32 1) to i64)",
		"ptrtoint (ptr getelementptr (%app.pair, ptr null, i32 1) to i64)",
		"= load i1, ptr",
		"= load %kizu.slice.u8, ptr",
		"= load %app.pair, ptr",
		"_storage_msg = extractvalue %kizu.error.slice.u8",
		"_semantic_failure = insertvalue",
		"= phi %kizu.error.app.pair",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("runtime result storage codec output missing %q\n%s", fragment, out.String())
		}
	}
	if strings.Contains(out.String(), "kizu_rt_map_get_i64") {
		t.Fatalf("runtime result storage codec emitted legacy specialized symbol\n%s", out.String())
	}

	var popOut bytes.Buffer
	if err := interp.New(&popOut).RunEntry(
		program, "selfhost::backend::compiled_mir_llvm::array_pop_result_renderer_gate",
	); err != nil {
		t.Fatalf("array pop result storage codec gate failed: %v\n%s", err, popOut.String())
	}
	for _, fragment := range []string{
		"call %kizu.slice.u8 @kizu_rt_array_pop_or_panic",
		"extractvalue %app.owner %owner, 2",
		"ptrtoint (ptr getelementptr (i1, ptr null, i32 1) to i64)",
		"ptrtoint (ptr getelementptr (%app.pair, ptr null, i32 1) to i64)",
		"ptrtoint (ptr getelementptr (%app.noncopy, ptr null, i32 1) to i64)",
		"_size_ok = icmp eq i64",
		"= load i1, ptr",
		"= load %app.pair, ptr",
		"= load %app.noncopy, ptr",
		"call void @llvm.trap()",
	} {
		if !strings.Contains(popOut.String(), fragment) {
			t.Fatalf("array pop result storage output missing %q\n%s", fragment, popOut.String())
		}
	}
	if strings.Contains(popOut.String(), "kizu_rt_array_pop_or_panic_") {
		t.Fatalf("array pop emitted a type-specialized runtime symbol\n%s", popOut.String())
	}

	var fieldOut bytes.Buffer
	if err := interp.New(&fieldOut).RunEntry(
		program, "selfhost::backend::compiled_mir_lower::method_field_receiver_gate",
	); err != nil {
		t.Fatalf("method field receiver gate failed: %v\n%s", err, fieldOut.String())
	}
}

func TestSelfhostArrayPopOrPanicRuntimeIsGenericAndChecked(t *testing.T) {
	storage := readSelfhostFile(t, "../../selfhost/runtime/selfhost.storage.ll")
	start := strings.Index(
		storage,
		"define %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned %array)",
	)
	if start < 0 {
		t.Fatal("storage runtime is missing array_pop_or_panic")
	}
	endOffset := strings.Index(storage[start:], "\n}\n")
	if endOffset < 0 {
		t.Fatal("storage runtime array_pop_or_panic body is unterminated")
	}
	body := storage[start : start+endOffset]
	for _, fragment := range []string{
		"%raw_ok = icmp ne ptr %raw, null",
		"br i1 %raw_ok, label %inspect, label %invalid",
		"%data_ok = icmp ne ptr %data, null",
		"%len_ok = icmp sgt i64 %len, 0",
		"%size_ok = icmp sgt i64 %element_size, 0",
		"%next_len = sub i64 %len, 1",
		"store i64 %next_len, ptr %len_field",
		"ret %kizu.slice.u8 %slice",
		"call void @llvm.trap()",
		"unreachable",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("array_pop_or_panic runtime missing %q\n%s", fragment, body)
		}
	}

	backend := readSelfhostFile(t, "../../selfhost/src/backend/llvm.kizu")
	if !strings.Contains(
		backend,
		`"define %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned %array) {"`,
	) {
		t.Fatal("self-contained compiler module is missing array_pop_or_panic")
	}

	lower := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir_lower.kizu")
	receiver := selfhostKizuFunctionBody(t, lower, "fn resolve_method_receiver(")
	for _, fragment := range []string{
		`std::mem::equal_bytes(receiver_kind, "FieldExpr")`,
		"compiled_mir_types::resolve_field_expr_indexed_with_cache(",
		`std::mem::equal_bytes(field_root_kind, "Var")`,
	} {
		if !strings.Contains(receiver, fragment) {
			t.Fatalf("generic method field receiver missing %q\n%s", fragment, receiver)
		}
	}
	for _, forbidden := range []string{"ParseResult", "ParsedSourceFiles", "parsed"} {
		if strings.Contains(receiver, forbidden) {
			t.Fatalf("method field receiver contains type/name-specific branch %q", forbidden)
		}
	}
}

func TestSelfhostCompiledPrintRenderer(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_mir_llvm::print_renderer_gate",
	); err != nil {
		t.Fatalf("compiled print renderer gate failed: %v\n%s", err, out.String())
	}
	for _, fragment := range []string{
		"extractvalue %kizu.slice.u8",
		"call void @kizu_print_string(ptr",
		"sext i16 -7 to i64",
		"zext i8 250 to i64",
		"call void @kizu_print_int(i64 42)",
		"call void @kizu_print_bool(i1 true)",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("compiled print renderer output missing %q\n%s", fragment, out.String())
		}
	}
	var unsupported bytes.Buffer
	err = interp.New(&unsupported).RunEntry(
		program,
		"selfhost::backend::compiled_mir_llvm::print_renderer_unsupported_transport_gate",
	)
	if err == nil || !strings.Contains(err.Error(), "print runtime descriptor missing") {
		t.Fatalf("unsupported print transport error = %v, want descriptor failure", err)
	}
}

func TestSelfhostCompiledPrintStatementLowering(t *testing.T) {
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := interp.New(&out).RunEntry(
		program, "selfhost::backend::compiled_mir_lower::gate_print_statement_lowering",
	); err != nil {
		t.Fatalf("compiled print lowering gate failed: %v\n%s", err, out.String())
	}
	for _, fragment := range []string{
		"slice",
		"%kizu.slice.u8",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("compiled print lowering output missing %q\n%s", fragment, out.String())
		}
	}
}

func TestSelfhostHostedPrintRuntimeHasSingleOwner(t *testing.T) {
	hosted := readSelfhostFile(t, "../../selfhost/runtime/selfhost.hosted.c")
	storage := readSelfhostFile(t, "../../selfhost/runtime/selfhost.storage.ll")
	for _, symbol := range []string{
		"kizu_print_string",
		"kizu_print_int",
		"kizu_print_bool",
	} {
		if count := strings.Count(hosted, "void "+symbol+"("); count != 1 {
			t.Fatalf("hosted runtime definition count for %s = %d, want 1", symbol, count)
		}
		if strings.Contains(storage, "@"+symbol+"(") {
			t.Fatalf("storage runtime must not own hosted print symbol %s", symbol)
		}
	}
}
