package main

import (
	"strings"
	"testing"
)

// TestSelfhostAtomicBoolTapeAndLLVMContract pins the Atomic<bool> tape shape and
// its byte-sized sequentially-consistent LLVM representation.
func TestSelfhostAtomicBoolTapeAndLLVMContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertAtomicBoolTapeContract(t, codegen)
	assertAtomicBoolLoadLowering(t, codegen)
	assertAtomicBoolStoreLowering(t, codegen)
	assertAtomicBoolEscapeUnsupported(t, codegen)
	assertAtomicBoolLLVMContract(t, render)
	assertAtomicBoolLoadLLVMContract(t, render)
	assertAtomicBoolStoreLLVMContract(t, render)
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

// assertAtomicBoolLoadLowering verifies load is gated by receiver kind and zero arguments.
func assertAtomicBoolLoadLowering(t *testing.T, codegen string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_expr_method(")
	atomicGuard := strings.Index(dispatch, "if receiver_kind == code_kind_atomic_bool()")
	atomicCall := strings.Index(dispatch, "lower_code_atomic_bool_expr_method(")
	if atomicGuard < 0 || atomicCall <= atomicGuard {
		t.Fatal("Atomic<bool>.load dispatch is not gated by the evaluated receiver kind")
	}
	if strings.Count(dispatch, "lower_code_atomic_bool_expr_method(") != 1 {
		t.Fatal("Atomic method lowering escaped its receiver-kind dispatch arm")
	}
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_atomic_bool_expr_method(")
	requireSourceFragments(t, "Atomic<bool>.load lowering", lower, []string{
		`std::mem::equal_bytes(method, "load")`,
		"args.len != 0",
		"code.append(code_op_atomic_bool_load())",
		"code.append(receiver_value)",
		"kinds.append(code_kind_bool())",
	})
	forbidSourceFragments(t, "Atomic<bool>.load spelling dispatch", lower, []string{
		`"Atomic"`, `"std::atomic"`, `"store"`, "source_path", "code_op_atomic_bool_store",
	})
}

// assertAtomicBoolStoreLowering verifies the typed store arm lowers one bool
// argument once and returns the established void-call success shape.
func assertAtomicBoolStoreLowering(t *testing.T, codegen string) {
	t.Helper()
	statement := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_statement_call(")
	requireSourceFragments(t, "Atomic<bool>.store typed statement routing", statement, []string{
		`std::mem::equal_bytes(method, "store")`,
		"args.len != 1",
		"lower_code_expr(text, ast, decls, receiver",
		"receiver_kind != code_kind_atomic_bool()",
		"lower_code_atomic_bool_store_statement(",
	})
	storeGuard := strings.Index(statement, `std::mem::equal_bytes(method, "store")`)
	shapeCheck := -1
	receiverProbe := -1
	if storeGuard >= 0 {
		storeArm := statement[storeGuard:]
		shapeCheck = strings.Index(storeArm, "args.len != 1")
		receiverProbe = strings.Index(storeArm, "lower_code_expr(text, ast, decls, receiver")
	}
	if storeGuard < 0 || shapeCheck < 0 || receiverProbe <= shapeCheck {
		t.Fatal("Atomic<bool>.store receiver is evaluated before its exact shape is known")
	}
	if strings.Contains(statement, "code_field_method_return_kind") ||
		strings.Contains(statement, "user_method_kind") {
		t.Fatal("Atomic<bool>.store is incorrectly gated by a global same-name impl lookup")
	}
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_atomic_bool_store_statement(")
	requireSourceFragments(t, "Atomic<bool>.store lowering", lower, []string{
		"ast.child_at(args, 0)",
		"value_kind != code_kind_bool()",
		"code.append(code_op_atomic_bool_store())",
		"code.append(receiver_value)",
		"code.append(value_eval.value_id)",
		"code_eval_value(0, value_eval.next)",
	})
	if strings.Count(lower, "lower_code_expr(") != 1 {
		t.Fatal("Atomic<bool>.store argument is not lowered exactly once")
	}
	exprLower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_atomic_bool_expr_method(")
	forbidSourceFragments(t, "Atomic<bool>.store value-position lowering", exprLower, []string{
		`"store"`, "code_op_atomic_bool_store", "lower_code_atomic_bool_store_statement",
	})
	forbidSourceFragments(t, "Atomic<bool>.store hidden dispatch", lower, []string{
		`"Atomic"`, `"std::atomic"`, "source_path", "fixture", "fallback",
	})
}

// assertAtomicBoolEscapeUnsupported keeps ABI/container escape out of this slice.
func assertAtomicBoolEscapeUnsupported(t *testing.T, codegen string) {
	t.Helper()
	for _, name := range []string{
		"fn code_return_kind(",
		"fn code_param_kind(",
		"fn code_array_element_kind(",
		"fn code_struct_decl_field_kind(",
		"fn code_struct_param_field_kind(",
	} {
		body := selfhostKizuFunctionBody(t, codegen, name)
		if strings.Contains(body, "code_kind_atomic_bool") {
			t.Fatalf("Atomic<bool> escaped through %s", name)
		}
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
	if strings.Contains(allocas, "code_op_atomic_bool_store()") {
		t.Fatal("Atomic<bool>.store allocates new storage")
	}
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

// assertAtomicBoolStoreLLVMContract verifies the fixed tape walk and seq_cst
// byte store, including the required i1-to-i8 widening.
func assertAtomicBoolStoreLLVMContract(t *testing.T, render string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Atomic<bool>.store render dispatch", dispatch, []string{
		"code_op_atomic_bool_store()",
		"render_atomic_bool_store(out, code, index)",
	})
	storeWriter := selfhostKizuFunctionBody(t, render, "fn render_atomic_bool_store(")
	requireSourceFragments(t, "Atomic<bool>.store LLVM", storeWriter, []string{
		`" = zext i1 %v"`,
		`" to i8"`,
		`"  store atomic i8 %abs"`,
		`", ptr %v"`,
		`" seq_cst, align 1"`,
	})
	if strings.Contains(storeWriter, "alloca") {
		t.Fatal("Atomic<bool>.store writer allocates new storage")
	}
	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	storeRecord := strings.Index(recordEnd, "code_op_atomic_bool_store()")
	if storeRecord < 0 || !strings.Contains(recordEnd[storeRecord:], "return index + 3") {
		t.Fatal("ATOMIC_BOOL_STORE is not a fixed three-slot tape record")
	}
}

// assertAtomicBoolLoadLLVMContract verifies the three-slot tape walk and seq_cst byte load.
func assertAtomicBoolLoadLLVMContract(t *testing.T, render string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Atomic<bool>.load render dispatch", dispatch, []string{
		"code_op_atomic_bool_load()",
		"render_atomic_bool_load(out, code, index)",
	})
	loadWriter := selfhostKizuFunctionBody(t, render, "fn render_atomic_bool_load(")
	requireSourceFragments(t, "Atomic<bool>.load LLVM", loadWriter, []string{
		`" = load atomic i8, ptr %v"`,
		`" seq_cst, align 1"`,
		`" = icmp ne i8 %abl"`,
		`", 0"`,
	})
	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	loadRecord := strings.Index(recordEnd, "code_op_atomic_bool_load()")
	if loadRecord < 0 || !strings.Contains(recordEnd[loadRecord:], "return index + 3") {
		t.Fatal("ATOMIC_BOOL_LOAD is not a fixed three-slot tape record")
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
