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

// TestSelfhostChannelI64SendContract pins statement-only typed lowering and a
// dynamically stack-allocated FIFO node for every send execution.
func TestSelfhostChannelI64SendContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertChannelI64SendLowering(t, codegen)
	assertChannelI64SendFIFO(t, render)
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
	if !strings.Contains(preamble, `"%kizu.run.channel.i64 = type { ptr, ptr, i1 }"`) {
		t.Fatal("Channel<i64> runtime queue layout is not declared")
	}
	if !strings.Contains(preamble, `"%kizu.run.channel.i64.node = type { i64, ptr }"`) {
		t.Fatal("Channel<i64> runtime FIFO node layout is not declared")
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
		`"  store %kizu.run.channel.i64 { ptr null, ptr null, i1 false }, ptr %channel"`,
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

// assertChannelI64SendLowering verifies statement-position send validates its
// arity before evaluating the receiver, evaluates receiver and argument once,
// and commits only the exact Channel<i64>/i64 numeric pair to the tape.
func assertChannelI64SendLowering(t *testing.T, codegen string) {
	t.Helper()
	assertChannelI64SendDispatch(t, codegen)
	assertChannelI64SendReceiverProbe(t, codegen)
	assertChannelI64SendTapeLowering(t, codegen)

	exprDispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_expr_method(")
	forbidSourceFragments(t, "Channel<i64>.send expression lowering", exprDispatch, []string{
		"lower_code_channel_i64_send_statement", "code_op_channel_i64_send",
	})
}

// assertChannelI64SendDispatch pins exact statement-only method selection,
// arity-first validation, and single receiver evaluation.
func assertChannelI64SendDispatch(t *testing.T, codegen string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_statement_call(")
	sendStart := strings.Index(dispatch, `std::mem::equal_bytes(method, "send")`)
	storeStart := strings.Index(dispatch, `std::mem::equal_bytes(method, "store")`)
	if sendStart < 0 || storeStart <= sendStart {
		t.Fatal("Channel<i64>.send is not an exact runtime statement method arm")
	}
	sendArm := dispatch[sendStart:storeStart]
	receiverEval := "let receiver_eval = try lower_code_expr("
	receiverProbe := "let probed_receiver = try code_local_channel_i64_receiver_value("
	requireSourceFragments(t, "Channel<i64>.send typed statement dispatch", sendArm, []string{
		"args.len != 1",
		receiverProbe,
		"if probed_receiver < 0",
		receiverEval,
		"let receiver_kind = try kinds.get(receiver_eval.value_id)",
		"receiver_kind != code_kind_channel_i64()",
		"lower_code_channel_i64_send_statement(",
	})
	if strings.Index(sendArm, "args.len != 1") > strings.Index(sendArm, receiverEval) {
		t.Fatal("Channel<i64>.send evaluates its receiver before validating arity")
	}
	if strings.Index(sendArm, receiverProbe) > strings.Index(sendArm, receiverEval) {
		t.Fatal("Channel<i64>.send lowers its receiver before the side-effect-free local-kind guard")
	}
	if strings.Count(sendArm, receiverEval) != 1 {
		t.Fatal("Channel<i64>.send receiver is not evaluated exactly once")
	}
	forbidSourceFragments(t, "Channel<i64>.send dispatch fallback", sendArm, []string{
		`"Channel"`, `"std::channel"`, "source_path", "fixture", "fallback",
	})
}

// assertChannelI64SendReceiverProbe pins the side-effect-free local Var and
// exact Channel<i64> kind guard used before ordinary receiver lowering.
func assertChannelI64SendReceiverProbe(t *testing.T, codegen string) {
	t.Helper()
	probe := selfhostKizuFunctionBody(t, codegen, "fn code_local_channel_i64_receiver_value(")
	requireSourceFragments(t, "Channel<i64>.send local receiver shape", probe, []string{
		"Var(var_node)",
		"code_local_channel_i64_var_value(",
		"_ => code_no_local_channel_i64_receiver()",
	})
	forbidSourceFragments(t, "Channel<i64>.send receiver probe effects", probe, []string{
		"lower_code_expr", "code.append", "kinds.append",
	})
	noReceiver := selfhostKizuFunctionBody(t, codegen, "fn code_no_local_channel_i64_receiver(")
	if !strings.Contains(noReceiver, "return 0 - 1") {
		t.Fatal("Channel<i64>.send local receiver sentinel is not negative")
	}
	varProbe := selfhostKizuFunctionBody(t, codegen, "fn code_local_channel_i64_var_value(")
	requireSourceFragments(t, "Channel<i64>.send local receiver kind", varProbe, []string{
		"code_local_lookup(",
		"bound < 0 or bound >= kinds.len()",
		"let kind = try kinds.get(bound)",
		"kind != code_kind_channel_i64()",
		"return bound",
	})
	forbidSourceFragments(t, "Channel<i64>.send local receiver lookup effects", varProbe, []string{
		"lower_code_expr", "code.append", "kinds.append",
	})
}

// assertChannelI64SendTapeLowering pins one i64 argument evaluation and the
// fixed receiver/value tape payload without hidden storage or result kinds.
func assertChannelI64SendTapeLowering(t *testing.T, codegen string) {
	t.Helper()
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_send_statement(")
	requireSourceFragments(t, "Channel<i64>.send tape lowering", lower, []string{
		"let value = try ast.child_at(args, 0)",
		"let value_eval = try lower_code_expr(",
		"let value_kind = try kinds.get(value_eval.value_id)",
		"value_kind != code_kind_i64()",
		"code.append(code_op_channel_i64_send())",
		"code.append(receiver_value)",
		"code.append(value_eval.value_id)",
		"code_eval_value(0, value_eval.next)",
	})
	if strings.Count(lower, "let value_eval = try lower_code_expr(") != 1 {
		t.Fatal("Channel<i64>.send argument is not evaluated exactly once")
	}
	forbidSourceFragments(t, "Channel<i64>.send hidden lowering", lower, []string{
		"kinds.append", "page_allocator", "kizu_rt_", "malloc", "calloc",
		"source_path", "fixture", "fallback", "closed",
	})
}

// assertChannelI64SendFIFO verifies send allocates its node at the instruction,
// links both empty and non-empty FIFO cases, and never acquires heap storage.
func assertChannelI64SendFIFO(t *testing.T, render string) {
	t.Helper()
	allocas := selfhostKizuFunctionBody(t, render, "fn render_var_allocas(")
	forbidSourceFragments(t, "Channel<i64>.send entry hoist", allocas, []string{
		"code_op_channel_i64_send", "channel_i64.node",
	})

	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Channel<i64>.send render dispatch", dispatch, []string{
		"code_op_channel_i64_send()",
		"render_channel_i64_send(out, code, index)",
	})

	writer := selfhostKizuFunctionBody(t, render, "fn render_channel_i64_send(")
	requireSourceFragments(t, "Channel<i64>.send dynamic FIFO", writer, []string{
		`"_node = alloca %kizu.run.channel.i64.node"`,
		`"_value_field = getelementptr inbounds %kizu.run.channel.i64.node, ptr %ch"`,
		`"  store i64 %v"`,
		`"_next_field = getelementptr inbounds %kizu.run.channel.i64.node, ptr %ch"`,
		`"  store ptr null, ptr %ch"`,
		`"_head_field = getelementptr inbounds %kizu.run.channel.i64, ptr %v"`,
		`"_tail_field = getelementptr inbounds %kizu.run.channel.i64, ptr %v"`,
		`"_empty = icmp eq ptr %ch"`,
		`"_empty_queue, label %ch"`,
		`"_nonempty_queue"`,
		`"_tail_next_field = getelementptr inbounds %kizu.run.channel.i64.node, ptr %ch"`,
		`"_linked:"`,
	})
	forbidSourceFragments(t, "Channel<i64>.send forbidden storage", writer, []string{
		"page_allocator", "kizu_rt_", "malloc", "calloc", "capacity", "closed",
		"render_one_channel_i64_alloca",
	})

	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	sendStart := strings.Index(recordEnd, "code_op_channel_i64_send()")
	if sendStart < 0 {
		t.Fatal("CHANNEL_I64_SEND tape record is missing")
	}
	sendRecord := recordEnd[sendStart:]
	if next := strings.Index(sendRecord, "let not_op"); next >= 0 {
		sendRecord = sendRecord[:next]
	}
	if !strings.Contains(sendRecord, "return index + 3") {
		t.Fatal("CHANNEL_I64_SEND is not a fixed three-slot tape record")
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
