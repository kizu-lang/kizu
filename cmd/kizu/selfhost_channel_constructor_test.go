package main

import (
	"strings"
	"testing"
)

// TestSelfhostChannelI64ConstructorContract pins the checked numeric identity,
// typed tape, and complete function-local queue initialization.
func TestSelfhostChannelI64ConstructorContract(t *testing.T) {
	facts := readSelfhostFile(t, "../../selfhost/src/types/constructor_facts.kizu")
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertChannelI64CheckedIdentity(t, facts, codegen)
	assertChannelI64ConstructorLowering(t, codegen)
	assertChannelI64FunctionLocalStorage(t, render)
	assertChannelI64EscapeUnsupported(t, codegen)
}

// assertChannelI64CheckedIdentity verifies exact checker-side identities become
// one numeric Channel<i64> kind without codegen spelling or arity fallback.
func assertChannelI64CheckedIdentity(t *testing.T, facts, codegen string) {
	t.Helper()
	appendResolved := selfhostKizuFunctionBody(t, facts, "fn append_resolved(")
	requireSourceFragments(t, "generic constructor fact selection", appendResolved, []string{
		"args.len != 1",
		"resolved_constructor_identity_id(text, ast, root, callee)",
		"resolved_type_identity_id(text, ast, root, arg)",
	})
	constructorResolver := selfhostKizuFunctionBody(t, facts, "fn resolved_constructor_identity_id(")
	requireSourceFragments(t, "Channel constructor exact identity", constructorResolver, []string{
		`"std::channel::Channel"`,
		"constructor_channel()",
	})
	typeResolver := selfhostKizuFunctionBody(t, facts, "fn resolved_type_identity_id(")
	requireSourceFragments(t, "i64 exact type identity", typeResolver, []string{
		`"i64"`,
		"type_i64()",
	})
	forbidSourceFragments(t, "Channel slice identity", typeResolver, []string{
		`"[]u8"`, "type_slice",
	})
	scratch := selfhostKizuFunctionBody(t, codegen, "fn scratch_init(")
	requireSourceFragments(t, "numeric Channel<i64> selection", scratch, []string{
		"constructor_id == channel_constructor_id",
		"type_arg0_id == i64_type_id",
		"resolved_kind = code_kind_channel_i64()",
	})
	forbidSourceFragments(t, "Channel codegen identity", scratch, []string{
		`"Channel"`, `"std::channel"`, "source_path", "arity", "args.len",
	})
}

// assertChannelI64ConstructorLowering verifies the exact zero-argument typed
// constructor commits before legacy spelling-based constructor probes.
func assertChannelI64ConstructorLowering(t *testing.T, codegen string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_constructor(")
	channelGuard := strings.Index(dispatch, "if resolved_kind == channel_i64_kind")
	channelCall := strings.Index(dispatch, "lower_code_channel_i64_new(args, code, kinds, next_value)")
	legacyArena := strings.Index(dispatch, "lower_code_arena_new")
	if channelGuard < 0 || channelCall <= channelGuard || legacyArena <= channelCall {
		t.Fatal("known Channel<i64> identity can fall through to legacy constructor probes")
	}
	forbidSourceFragments(t, "Channel constructor dispatch spelling", dispatch, []string{
		`"Channel"`, `"std::channel"`, "source_path",
	})
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_new(")
	requireSourceFragments(t, "Channel<i64> constructor lowering", lower, []string{
		"args.len != 0",
		"code.append(code_op_channel_i64_new())",
		"code.append(next_value)",
		"kinds.append(code_kind_channel_i64())",
		"code_eval_value(next_value, next_value + 1)",
	})
	forbidSourceFragments(t, "Channel constructor hidden path", lower, []string{
		`"Channel"`, `"std::channel"`, "source_path", "fixture", "fallback",
		"code_op_channel_send", "code_op_channel_recv", "code_op_channel_close",
	})
}

// assertChannelI64FunctionLocalStorage verifies the renderer owns and fully
// initializes the reusable queue state without an implicit allocator.
func assertChannelI64FunctionLocalStorage(t *testing.T, render string) {
	t.Helper()
	preamble := selfhostKizuFunctionBody(t, render, "fn render_preamble(")
	if !strings.Contains(preamble, `"%kizu.run.channel.i64 = type { ptr, i64, i64, i64, i1 }"`) {
		t.Fatal("Channel<i64> runtime queue layout is not declared")
	}
	allocas := selfhostKizuFunctionBody(t, render, "fn render_var_allocas(")
	requireSourceFragments(t, "Channel<i64> entry storage scan", allocas, []string{
		"code_op_channel_i64_new()",
		"render_one_channel_i64_alloca(out, channel_slot)",
	})
	channelAlloca := selfhostKizuFunctionBody(t, render, "fn render_one_channel_i64_alloca(")
	if !strings.Contains(channelAlloca, `" = alloca %kizu.run.channel.i64"`) {
		t.Fatal("Channel<i64> state is not function-local storage")
	}
	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Channel<i64> render dispatch", dispatch, []string{
		"code_op_channel_i64_new()",
		"render_channel_i64_new(out, code, index)",
	})
	writer := selfhostKizuFunctionBody(t, render, "fn render_channel_i64_new(")
	requireSourceFragments(t, "Channel<i64> initialized queue", writer, []string{
		`"  store %kizu.run.channel.i64 { ptr null, i64 0, i64 0, i64 0, i1 false }, ptr %channel"`,
		`" = getelementptr %kizu.run.channel.i64, ptr %channel"`,
	})
	forbidSourceFragments(t, "Channel<i64> implicit allocation", writer, []string{
		"page_allocator", "kizu_rt_alloc", "kizu_rt_channel", "malloc", "calloc",
	})
	valueType := selfhostKizuFunctionBody(t, render, "fn render_value_type(")
	requireSourceFragments(t, "Channel<i64> ptr value kind", valueType, []string{
		"kind == codegen::code_kind_channel_i64()",
		"if is_channel_i64",
		`try w(out, "ptr")`,
		"!is_channel_i64",
	})
	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	channelRecord := strings.Index(recordEnd, "code_op_channel_i64_new()")
	if channelRecord < 0 || !strings.Contains(recordEnd[channelRecord:], "return index + 2") {
		t.Fatal("CHANNEL_I64_NEW is not a fixed two-slot tape record")
	}
}

// assertChannelI64EscapeUnsupported keeps the constructor result local to one
// function and out of params, returns, aggregates, containers, and threads.
func assertChannelI64EscapeUnsupported(t *testing.T, codegen string) {
	t.Helper()
	for _, name := range []string{
		"fn code_return_kind(",
		"fn code_param_kind(",
		"fn code_array_element_kind(",
		"fn code_struct_decl_field_kind(",
		"fn code_struct_param_field_kind(",
		"fn code_kind_is_owned_resource(",
	} {
		body := selfhostKizuFunctionBody(t, codegen, name)
		if strings.Contains(body, "code_kind_channel_i64") {
			t.Fatalf("Channel<i64> escaped through %s", name)
		}
	}
}
