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

// TestSelfhostChannelI64CloseContract pins statement-only typed close lowering
// and the idempotent closed-flag store without disturbing queued nodes.
func TestSelfhostChannelI64CloseContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertChannelI64CloseLowering(t, codegen)
	assertChannelI64CloseLLVM(t, render)
}

// TestSelfhostChannelI64RecvContract pins the dedicated typed expression probe,
// three-slot i64 result, empty trap, and FIFO dequeue semantics.
func TestSelfhostChannelI64RecvContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertChannelI64RecvLowering(t, codegen)
	assertChannelI64RecvLLVM(t, render)
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
	closeStart := strings.Index(dispatch, `std::mem::equal_bytes(method, "close")`)
	if sendStart < 0 || closeStart <= sendStart {
		t.Fatal("Channel<i64>.send is not an exact runtime statement method arm")
	}
	sendArm := dispatch[sendStart:closeStart]
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

// assertChannelI64CloseLowering verifies close is a zero-argument statement op
// over the same guarded local Channel<i64> receiver and has no expression form.
func assertChannelI64CloseLowering(t *testing.T, codegen string) {
	t.Helper()
	assertChannelI64CloseDispatch(t, codegen)
	assertChannelI64CloseTapeLowering(t, codegen)

	exprDispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_expr_method(")
	forbidSourceFragments(t, "Channel<i64>.close expression lowering", exprDispatch, []string{
		"lower_code_channel_i64_close_statement", "code_op_channel_i64_close",
	})
}

// assertChannelI64CloseDispatch pins arity-first validation, the side-effect-free
// local-kind probe, and exactly one ordinary receiver evaluation.
func assertChannelI64CloseDispatch(t *testing.T, codegen string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_statement_call(")
	closeStart := strings.Index(dispatch, `std::mem::equal_bytes(method, "close")`)
	storeStart := strings.Index(dispatch, `std::mem::equal_bytes(method, "store")`)
	if closeStart < 0 || storeStart <= closeStart {
		t.Fatal("Channel<i64>.close is not an exact runtime statement method arm")
	}
	closeArm := dispatch[closeStart:storeStart]
	receiverEval := "let receiver_eval = try lower_code_expr("
	receiverProbe := "let probed_receiver = try code_local_channel_i64_receiver_value("
	requireSourceFragments(t, "Channel<i64>.close typed statement dispatch", closeArm, []string{
		"args.len != 0",
		receiverProbe,
		"if probed_receiver < 0",
		receiverEval,
		"let receiver_kind = try kinds.get(receiver_eval.value_id)",
		"receiver_kind != code_kind_channel_i64()",
		"lower_code_channel_i64_close_statement(",
	})
	if strings.Index(closeArm, "args.len != 0") > strings.Index(closeArm, receiverProbe) {
		t.Fatal("Channel<i64>.close probes its receiver before validating arity")
	}
	if strings.Index(closeArm, receiverProbe) > strings.Index(closeArm, receiverEval) {
		t.Fatal("Channel<i64>.close lowers its receiver before the local-kind guard")
	}
	if strings.Count(closeArm, receiverEval) != 1 {
		t.Fatal("Channel<i64>.close receiver is not evaluated exactly once")
	}
	forbidSourceFragments(t, "Channel<i64>.close dispatch fallback", closeArm, []string{
		`"Channel"`, `"std::channel"`, "source_path", "fixture", "fallback",
	})
}

// assertChannelI64CloseTapeLowering pins the two-slot void tape record without
// creating a result kind or consulting other channel operations.
func assertChannelI64CloseTapeLowering(t *testing.T, codegen string) {
	t.Helper()
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_close_statement(")
	requireSourceFragments(t, "Channel<i64>.close tape lowering", lower, []string{
		"code.append(code_op_channel_i64_close())",
		"code.append(receiver_value)",
		"code_eval_value(0, next_value)",
	})
	forbidSourceFragments(t, "Channel<i64>.close hidden lowering", lower, []string{
		"kinds.append", "code_op_channel_i64_send", "page_allocator", "kizu_rt_",
		"source_path", "fixture", "fallback",
	})
}

// assertChannelI64RecvLowering verifies the dedicated probe precedes name-only
// method dispatch while preserving side-effect-free misses for user recv methods.
func assertChannelI64RecvLowering(t *testing.T, codegen string) {
	t.Helper()
	assertChannelI64RecvProbeOrder(t, codegen)
	assertChannelI64RecvFieldProbe(t, codegen)
	assertChannelI64RecvTapeLowering(t, codegen)
}

// assertChannelI64RecvProbeOrder pins union-before-runtime-before-user-method
// ordering so unrelated recv methods still resolve without partial tape.
func assertChannelI64RecvProbeOrder(t *testing.T, codegen string) {
	t.Helper()
	call := selfhostKizuFunctionBody(t, codegen, "fn lower_code_call(")
	unionProbe := strings.Index(call, "let union_tag = try code_callee_union_tag(")
	recvProbe := strings.Index(call, "let channel_recv = try lower_code_channel_i64_recv_probe(")
	methodProbe := strings.Index(call, "let method_kind = try code_method_call_return_kind(")
	if unionProbe < 0 || recvProbe <= unionProbe || methodProbe <= recvProbe {
		t.Fatal("Channel<i64>.recv probe is not between union and user-method resolution")
	}
	requireSourceFragments(t, "Channel<i64>.recv probe result", call[recvProbe:methodProbe], []string{
		"if channel_recv.ok",
		"return channel_recv",
	})
	probe := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_recv_probe(")
	requireSourceFragments(t, "Channel<i64>.recv structural probe", probe, []string{
		"FieldExpr(field_expr)",
		"lower_code_channel_i64_recv_field(",
		"_ => code_eval_unsupported(next_value)",
	})
	forbidSourceFragments(t, "Channel<i64>.recv structural probe effects", probe, []string{
		"lower_code_expr", "code.append", "kinds.append",
	})
}

// assertChannelI64RecvFieldProbe pins exact recv/arity/local-kind checks before
// one ordinary receiver evaluation and the final typed tape commit.
func assertChannelI64RecvFieldProbe(t *testing.T, codegen string) {
	t.Helper()
	field := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_recv_field(")
	receiverEval := "let receiver_eval = try lower_code_expr("
	receiverProbe := "let probed_receiver = try code_local_channel_i64_receiver_value("
	requireSourceFragments(t, "Channel<i64>.recv typed field probe", field, []string{
		"if namespace",
		`std::mem::equal_bytes(method, "recv")`,
		"args.len != 0",
		receiverProbe,
		"if probed_receiver < 0",
		receiverEval,
		"let receiver_kind = try kinds.get(receiver_eval.value_id)",
		"receiver_kind != code_kind_channel_i64()",
		"lower_code_channel_i64_recv(receiver_eval.value_id, code, kinds, receiver_eval.next)",
	})
	probeAt := strings.Index(field, receiverProbe)
	lowerAt := strings.Index(field, receiverEval)
	if probeAt < 0 || lowerAt <= probeAt {
		t.Fatal("Channel<i64>.recv lowers its receiver before the local-kind probe")
	}
	if strings.Count(field, receiverEval) != 1 {
		t.Fatal("Channel<i64>.recv receiver is not evaluated exactly once")
	}
	forbidSourceFragments(t, "Channel<i64>.recv field fallback", field[:lowerAt], []string{
		"code.append", "kinds.append", "source_path", "fixture", "fallback",
	})
}

// assertChannelI64RecvTapeLowering pins dst=next_value, the receiver payload,
// and one i64 result kind in a fixed three-slot record.
func assertChannelI64RecvTapeLowering(t *testing.T, codegen string) {
	t.Helper()
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_i64_recv(")
	requireSourceFragments(t, "Channel<i64>.recv tape lowering", lower, []string{
		"code.append(code_op_channel_i64_recv())",
		"code.append(next_value)",
		"code.append(receiver_value)",
		"kinds.append(code_kind_i64())",
		"code_eval_value(next_value, next_value + 1)",
	})
	forbidSourceFragments(t, "Channel<i64>.recv hidden lowering", lower, []string{
		"source_path", "fixture", "fallback", "page_allocator", "kizu_rt_",
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

// assertChannelI64CloseLLVM verifies close only stores true to field index 2,
// leaves FIFO storage untouched, and remains a fixed two-slot tape record.
func assertChannelI64CloseLLVM(t *testing.T, render string) {
	t.Helper()
	allocas := selfhostKizuFunctionBody(t, render, "fn render_var_allocas(")
	forbidSourceFragments(t, "Channel<i64>.close entry storage", allocas, []string{
		"code_op_channel_i64_close",
	})
	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Channel<i64>.close render dispatch", dispatch, []string{
		"code_op_channel_i64_close()",
		"render_channel_i64_close(out, code, index)",
	})

	writer := selfhostKizuFunctionBody(t, render, "fn render_channel_i64_close(")
	requireSourceFragments(t, "Channel<i64>.close flag store", writer, []string{
		`"_closed_field = getelementptr inbounds %kizu.run.channel.i64, ptr %v"`,
		`", i32 0, i32 2"`,
		`"  store i1 true, ptr %ch"`,
	})
	forbidSourceFragments(t, "Channel<i64>.close forbidden mutation", writer, []string{
		"_head", "_tail", "channel.i64.node", "alloca", "load", "icmp", "br i1",
		"page_allocator", "kizu_rt_", "malloc", "calloc", "source_path", "fixture",
	})

	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	closeStart := strings.Index(recordEnd, "code_op_channel_i64_close()")
	if closeStart < 0 {
		t.Fatal("CHANNEL_I64_CLOSE tape record is missing")
	}
	closeRecord := recordEnd[closeStart:]
	if next := strings.Index(closeRecord, "let not_op"); next >= 0 {
		closeRecord = closeRecord[:next]
	}
	if !strings.Contains(closeRecord, "return index + 2") {
		t.Fatal("CHANNEL_I64_CLOSE is not a fixed two-slot tape record")
	}
}

// assertChannelI64RecvLLVM verifies the immutable empty diagnostic, FIFO pop,
// tail reset, untouched closed flag, and fixed tape record.
func assertChannelI64RecvLLVM(t *testing.T, render string) {
	t.Helper()
	assertChannelI64RecvGlobal(t, render)
	assertChannelI64RecvDequeue(t, render)
	assertChannelI64RecvRecord(t, render)
}

// assertChannelI64RecvGlobal pins the exact newline-terminated empty-channel
// runtime error as an immutable LLVM global consumed by the trap path.
func assertChannelI64RecvGlobal(t *testing.T, render string) {
	t.Helper()
	message := selfhostKizuFunctionBody(t, render, "fn channel_i64_empty_message(")
	if !strings.Contains(message, `"error: runtime error: channel is empty"`) {
		t.Fatal("Channel<i64>.recv empty diagnostic text changed")
	}
	global := selfhostKizuFunctionBody(t, render, "fn render_channel_i64_empty_global(")
	requireSourceFragments(t, "Channel<i64>.recv empty diagnostic global", global, []string{
		`"@.kizu.run.channel_i64_empty = private unnamed_addr constant ["`,
		"channel_i64_empty_message()",
		`"\0A"`,
	})
	preamble := selfhostKizuFunctionBody(t, render, "fn render_preamble(")
	if !strings.Contains(preamble, "render_channel_i64_empty_global(out)") {
		t.Fatal("Channel<i64>.recv empty diagnostic global is not emitted")
	}
}

// assertChannelI64RecvDequeue pins the null trap and FIFO head/tail mutation
// while forbidding closed-state changes and hidden storage paths.
func assertChannelI64RecvDequeue(t *testing.T, render string) {
	t.Helper()
	dispatch := selfhostKizuFunctionBody(t, render, "fn render_one_instruction_core_scalar(")
	requireSourceFragments(t, "Channel<i64>.recv render dispatch", dispatch, []string{
		"code_op_channel_i64_recv()",
		"render_channel_i64_recv(out, code, index)",
	})
	writer := selfhostKizuFunctionBody(t, render, "fn render_channel_i64_recv(")
	requireSourceFragments(t, "Channel<i64>.recv FIFO dequeue", writer, []string{
		`"_head_field = getelementptr inbounds %kizu.run.channel.i64, ptr %v"`,
		`"_head = load ptr, ptr %cr"`,
		`"_has_value = icmp ne ptr %cr"`,
		`"_msg_base = insertvalue %kizu.slice.u8 poison, ptr @.kizu.run.channel_i64_empty, 0"`,
		`"  call void @kizu_rt_trap(%kizu.slice.u8 %cr"`,
		`"_value_field = getelementptr inbounds %kizu.run.channel.i64.node, ptr %cr"`,
		`" = load i64, ptr %cr"`,
		`"_next_field = getelementptr inbounds %kizu.run.channel.i64.node, ptr %cr"`,
		`"_next = load ptr, ptr %cr"`,
		`try w(out, "  store ptr %cr")`,
		`try w(out, "_next, ptr %cr")`,
		`try wl(out, "_head_field")`,
		`"_now_empty = icmp eq ptr %cr"`,
		`"_tail_field = getelementptr inbounds %kizu.run.channel.i64, ptr %v"`,
		`"  store ptr null, ptr %cr"`,
		`"_done:"`,
	})
	forbidSourceFragments(t, "Channel<i64>.recv forbidden state", writer, []string{
		"_closed", "i32 0, i32 2", "store i1", "alloca", "kizu_rt_alloc",
		"kizu_rt_free", "kizu_rt_channel", "page_allocator", "source_path", "fixture",
	})
}

// assertChannelI64RecvRecord pins the renderer's fixed three-slot tape walk.
func assertChannelI64RecvRecord(t *testing.T, render string) {
	t.Helper()
	recordEnd := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	recvStart := strings.Index(recordEnd, "code_op_channel_i64_recv()")
	if recvStart < 0 {
		t.Fatal("CHANNEL_I64_RECV tape record is missing")
	}
	recvRecord := recordEnd[recvStart:]
	if next := strings.Index(recvRecord, "let not_op"); next >= 0 {
		recvRecord = recvRecord[:next]
	}
	if !strings.Contains(recvRecord, "return index + 3") {
		t.Fatal("CHANNEL_I64_RECV is not a fixed three-slot tape record")
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
