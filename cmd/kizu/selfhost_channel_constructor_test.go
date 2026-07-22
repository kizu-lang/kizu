package main

import (
	"strings"
	"testing"
)

// TestSelfhostChannelValueABIContract pins one generic Channel tape/lowering
// family for both i64 and []u8 payloads.
func TestSelfhostChannelValueABIContract(t *testing.T) {
	facts := readSelfhostFile(t, "../../selfhost/src/types/constructor_facts.kizu")
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")

	typeResolver := selfhostKizuFunctionBody(t, facts, "fn resolved_type_identity_id(")
	requireSourceFragments(t, "Channel payload identities", typeResolver, []string{
		`"i64"`, "type_i64()", `"[]u8"`, "type_slice()",
	})
	scratch := selfhostKizuFunctionBody(t, codegen, "fn scratch_init(")
	requireSourceFragments(t, "generic Channel kind selection", scratch, []string{
		"let payload_kind = code_type_identity_value_kind(",
		"constructor_id == channel_constructor_id and payload_kind >= 0",
		"resolved_kind = code_kind_channel(payload_kind)",
	})
	forbidSourceFragments(t, "Channel codegen identity spelling", scratch, []string{
		`"Channel"`, `"std::channel"`, "source_path", "fixture", "fallback",
	})

	kind := selfhostKizuFunctionBody(t, codegen, "pub fn code_kind_channel(")
	requireSourceFragments(t, "encoded Channel payload kind", kind, []string{
		"code_kind_value_abi_size(payload_kind)",
		"code_kind_channel_base() + payload_kind",
	})
	dispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_constructor(")
	channelAt := strings.Index(dispatch, "if code_kind_is_channel(resolved_kind)")
	arenaAt := strings.Index(dispatch, "lower_code_arena_new")
	if channelAt < 0 || arenaAt <= channelAt {
		t.Fatal("known Channel payload kind can fall through to legacy constructor probes")
	}
	lower := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_new(")
	requireSourceFragments(t, "generic Channel constructor tape", lower, []string{
		"let payload_kind = code_channel_payload_kind(channel_kind)",
		"code.append(code_op_channel_new())",
		"code.append(next_value)",
		"code.append(payload_kind)",
		"kinds.append(channel_kind)",
	})

	all := codegen + render
	forbidSourceFragments(t, "per-payload Channel implementation", all, []string{
		"CHANNEL_I64", "CHANNEL_SLICE", "code_op_channel_i64", "code_op_channel_slice",
		"render_channel_i64", "render_channel_slice", "channel_i64.node", "channel_slice.node",
	})
}

// TestSelfhostStorageValueABISharedWithArray pins one compiler-owned storage
// ABI source for Array elements and Channel payloads.
func TestSelfhostStorageValueABISharedWithArray(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")

	size := selfhostKizuFunctionBody(t, codegen, "pub fn code_kind_value_abi_size(")
	requireSourceFragments(t, "shared value ABI sizes", size, []string{
		"kind == code_kind_bool()", "return 1",
		"kind == code_kind_i64()", "return 8",
		"kind == code_kind_slice()", "return 16",
		"return 0 - 1",
	})
	forbidSourceFragments(t, "borrowed view storage ABI", size, []string{
		"code_kind_borrowed_slice",
	})
	arraySize := selfhostKizuFunctionBody(t, codegen, "fn code_array_element_size(")
	requireSourceFragments(t, "Array shared value ABI size", arraySize, []string{
		"code_array_element_kind", "code_kind_value_abi_size(element_kind)",
	})

	typeWriter := selfhostKizuFunctionBody(t, render, "fn render_value_abi_type(")
	requireSourceFragments(t, "shared value ABI LLVM types", typeWriter, []string{
		"code_kind_bool()", `"i8"`, "code_kind_i64()", `"i64"`,
		"code_kind_slice()", `"%kizu.slice.u8"`,
	})
	store := selfhostKizuFunctionBody(t, render, "fn render_value_abi_store(")
	requireSourceFragments(t, "shared value ABI store", store, []string{
		`"_byte = zext i1 %v"`, "render_value_abi_type(out, kind)",
	})
	load := selfhostKizuFunctionBody(t, render, "fn render_value_abi_load(")
	requireSourceFragments(t, "shared value ABI load", load, []string{
		`"_byte = load i8, ptr %"`, `" = icmp ne i8 %"`,
		"render_value_abi_type(out, kind)",
	})

	arrayAppend := selfhostKizuFunctionBody(t, render, "fn render_array_append_slot(")
	arrayGet := selfhostKizuFunctionBody(t, render, "fn render_array_get_load(")
	arraySet := selfhostKizuFunctionBody(t, render, "fn render_array_set_store(")
	arrayPop := selfhostKizuFunctionBody(t, render, "fn render_array_pop_load(")
	arrayOperations := arrayAppend + arrayGet + arraySet + arrayPop
	requireSourceFragments(t, "Array shared value ABI", arrayOperations, []string{
		"render_value_abi_alloca_store(", "render_value_abi_load(", "render_value_abi_store(",
	})
	channelSend := selfhostKizuFunctionBody(t, render, "fn render_channel_send(")
	channelRecv := selfhostKizuFunctionBody(t, render, "fn render_channel_recv(")
	requireSourceFragments(t, "Channel shared value ABI", channelSend+channelRecv, []string{
		"render_value_abi_type(out, payload_kind)",
		"render_value_abi_store(out, \"ch\"",
		"render_value_abi_load(out, \"cr\"",
	})
	forbidSourceFragments(t, "duplicate Array size table", render, []string{
		"fn array_element_size(",
	})
}

// TestSelfhostChannelGenericOperationsContract preserves local-only FIFO,
// close, empty-recv, typed payload matching, and fixed generic tape records.
func TestSelfhostChannelGenericOperationsContract(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	render := readSelfhostFile(t, "../../selfhost/src/ir/code_render.kizu")
	assertSelfhostChannelCodegenOperations(t, codegen)
	assertSelfhostChannelSendRecvLowering(t, codegen)
	assertSelfhostChannelRenderOperations(t, render)
}

// assertSelfhostChannelCodegenOperations pins generic Channel lowering.
func assertSelfhostChannelCodegenOperations(t *testing.T, codegen string) {
	statementDispatch := selfhostKizuFunctionBody(
		t, codegen, "fn lower_code_runtime_field_statement_call(",
	)
	requireSourceFragments(t, "generic Channel statement dispatch", statementDispatch, []string{
		`std::mem::equal_bytes(method, "send")`,
		"code_local_channel_receiver_value(",
		"code_kind_is_channel(receiver_kind)",
		"lower_code_channel_send_statement(",
		`std::mem::equal_bytes(method, "close")`,
		"lower_code_channel_close_statement(",
	})
	sendStart := strings.Index(statementDispatch, `std::mem::equal_bytes(method, "send")`)
	closeStart := strings.Index(statementDispatch, `std::mem::equal_bytes(method, "close")`)
	storeStart := strings.Index(statementDispatch, `std::mem::equal_bytes(method, "store")`)
	if sendStart < 0 || closeStart <= sendStart || storeStart <= closeStart {
		t.Fatal("Channel statement method arms are not ordered")
	}
	assertChannelReceiverEvaluatedOnce(
		t, "send", statementDispatch[sendStart:closeStart], "args.len != 1",
	)
	assertChannelReceiverEvaluatedOnce(
		t, "close", statementDispatch[closeStart:storeStart], "args.len != 0",
	)
	probe := selfhostKizuFunctionBody(t, codegen, "fn code_local_channel_receiver_value(")
	requireSourceFragments(t, "side-effect-free Channel receiver probe", probe, []string{
		"Var(var_node)", "code_local_channel_var_value(",
		"_ => code_no_local_channel_receiver()",
	})
	forbidSourceFragments(t, "Channel receiver probe effects", probe, []string{
		"lower_code_expr", "code.append", "kinds.append",
	})
	varProbe := selfhostKizuFunctionBody(t, codegen, "fn code_local_channel_var_value(")
	requireSourceFragments(t, "local Channel kind probe", varProbe, []string{
		"code_local_lookup(", "bound < 0 or bound >= kinds.len()",
		"code_kind_is_channel(kind)", "return bound",
	})
}

// assertSelfhostChannelSendRecvLowering pins payload and recv fallback rules.
func assertSelfhostChannelSendRecvLowering(t *testing.T, codegen string) {
	send := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_send_statement(")
	requireSourceFragments(t, "generic Channel send tape", send, []string{
		"direct_kind == code_kind_borrowed_slice()",
		"let payload_eval = try lower_code_arg_slice(",
		"let payload_kind = code_channel_payload_kind(receiver_kind)",
		"value_kind != payload_kind",
		"code.append(code_op_channel_send())",
		"code.append(payload_eval.value_id)",
		"code.append(payload_kind)",
	})
	recv := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_recv(")
	requireSourceFragments(t, "generic Channel recv tape", recv, []string{
		"let payload_kind = code_channel_payload_kind(receiver_kind)",
		"code.append(code_op_channel_recv())",
		"code.append(payload_kind)",
		"kinds.append(payload_kind)",
	})
	stringView := selfhostKizuFunctionBody(t, codegen, "fn lower_code_string_as_bytes_expr(")
	requireSourceFragments(t, "String view provenance kind", stringView, []string{
		"code_kind_borrowed_slice()", "kinds.append(kind_slice)",
	})
	call := selfhostKizuFunctionBody(t, codegen, "fn lower_code_call(")
	unionAt := strings.Index(call, "let union_tag = try code_callee_union_tag(")
	recvAt := strings.Index(call, "let channel_recv = try lower_code_channel_recv_probe(")
	userMethodAt := strings.Index(call, "let method_kind = try code_method_call_return_kind(")
	if unionAt < 0 || recvAt <= unionAt || userMethodAt <= recvAt {
		t.Fatal("Channel recv probe does not preserve user-method fallback ordering")
	}
	recvProbe := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_recv_probe(")
	forbidSourceFragments(t, "Channel recv structural probe effects", recvProbe, []string{
		"lower_code_expr", "code.append", "kinds.append",
	})
	recvField := selfhostKizuFunctionBody(t, codegen, "fn lower_code_channel_recv_field(")
	probeAt := strings.Index(recvField, "let probed_receiver = try code_local_channel_receiver_value(")
	evalAt := strings.Index(recvField, "let receiver_eval = try lower_code_expr(")
	receiverEvalCount := strings.Count(recvField, "let receiver_eval = try lower_code_expr(")
	if probeAt < 0 || evalAt <= probeAt || receiverEvalCount != 1 {
		t.Fatal("Channel recv does not guard then evaluate its receiver exactly once")
	}
	exprDispatch := selfhostKizuFunctionBody(t, codegen, "fn lower_code_runtime_field_expr_method(")
	forbidSourceFragments(t, "statement-only Channel methods", exprDispatch, []string{
		"lower_code_channel_send_statement", "lower_code_channel_close_statement",
	})
}

// assertSelfhostChannelRenderOperations pins generic Channel queue rendering.
func assertSelfhostChannelRenderOperations(t *testing.T, render string) {
	preamble := selfhostKizuFunctionBody(t, render, "fn render_preamble(")
	requireSourceFragments(t, "generic Channel queue header", preamble, []string{
		`"%kizu.run.channel = type { ptr, ptr, i1 }"`,
		"render_channel_empty_global(out)",
	})
	forbidSourceFragments(t, "per-payload Channel node declarations", preamble, []string{
		"channel.i64", "channel.slice", ".node = type",
	})
	sendWriter := selfhostKizuFunctionBody(t, render, "fn render_channel_send(")
	requireSourceFragments(t, "generic Channel FIFO send", sendWriter, []string{
		`"_node = alloca { "`, `", ptr }"`,
		`"_empty_queue, label %ch"`, `"_nonempty_queue"`,
		`"_tail_next_field = getelementptr inbounds { "`, `"_linked:"`,
	})
	forbidSourceFragments(t, "Channel send hidden storage", sendWriter, []string{
		"page_allocator", "kizu_rt_alloc", "malloc", "calloc", "source_path", "fixture",
	})
	closeWriter := selfhostKizuFunctionBody(t, render, "fn render_channel_close(")
	requireSourceFragments(t, "generic Channel close", closeWriter, []string{
		`"_closed_field = getelementptr inbounds %kizu.run.channel, ptr %v"`,
		`", i32 0, i32 2"`, `"  store i1 true, ptr %ch"`,
	})
	recvWriter := selfhostKizuFunctionBody(t, render, "fn render_channel_recv(")
	requireSourceFragments(t, "generic Channel FIFO recv", recvWriter, []string{
		`"_has_value = icmp ne ptr %cr"`,
		`@.kizu.run.channel_empty`, `call void @kizu_rt_trap`,
		`"_next = load ptr, ptr %cr"`, `"_now_empty = icmp eq ptr %cr"`,
		`"  store ptr null, ptr %cr"`, `"_done:"`,
	})
	forbidSourceFragments(t, "Channel recv closed-state mutation", recvWriter, []string{
		"_closed", "i32 0, i32 2", "store i1",
	})
	forbidSourceFragments(t, "Channel close FIFO mutation", closeWriter, []string{
		"_head", "_tail", "load", "icmp", "br i1", "alloca",
	})
	emptyMessage := selfhostKizuFunctionBody(t, render, "fn channel_empty_message(")
	if !strings.Contains(emptyMessage, `"error: runtime error: channel is empty"`) {
		t.Fatal("Channel empty diagnostic text changed")
	}
	emptyGlobal := selfhostKizuFunctionBody(t, render, "fn render_channel_empty_global(")
	requireSourceFragments(t, "Channel empty diagnostic global", emptyGlobal, []string{
		`@.kizu.run.channel_empty`, "channel_empty_message()", `\0A`,
	})

	records := selfhostKizuFunctionBody(t, render, "fn tape_record_end_core_scalar(")
	requireSourceFragments(t, "generic Channel tape widths", records, []string{
		"code_op_channel_new()", "return index + 3",
		"code_op_channel_send()", "return index + 4",
		"code_op_channel_close()", "return index + 2",
		"code_op_channel_recv()", "return index + 4",
	})
}

// assertChannelReceiverEvaluatedOnce pins arity and receiver evaluation order.
func assertChannelReceiverEvaluatedOnce(t *testing.T, method, arm, arityGuard string) {
	t.Helper()
	probe := "let probed_receiver = try code_local_channel_receiver_value("
	eval := "let receiver_eval = try lower_code_expr("
	guardAt := strings.Index(arm, arityGuard)
	probeAt := strings.Index(arm, probe)
	evalAt := strings.Index(arm, eval)
	if guardAt < 0 || probeAt <= guardAt || evalAt <= probeAt {
		t.Fatalf("Channel.%s does not validate arity, probe, then evaluate", method)
	}
	if strings.Count(arm, eval) != 1 {
		t.Fatalf("Channel.%s receiver is not evaluated exactly once", method)
	}
	forbidSourceFragments(t, "Channel."+method+" hidden dispatch", arm, []string{
		`"Channel"`, `"std::channel"`, "source_path", "fixture", "fallback",
	})
}

// TestSelfhostChannelEscapeUnsupported keeps Channel values out of params,
// returns, aggregates, containers, and owned/drop-resource paths.
func TestSelfhostChannelEscapeUnsupported(t *testing.T) {
	codegen := readSelfhostFile(t, "../../selfhost/src/ir/codegen.kizu")
	for _, name := range []string{
		"fn code_return_kind(",
		"fn code_param_kind(",
		"fn code_array_element_kind(",
		"fn code_struct_decl_field_kind(",
		"fn code_struct_param_field_kind(",
		"fn code_kind_is_owned_resource(",
	} {
		body := selfhostKizuFunctionBody(t, codegen, name)
		hasChannelKind := strings.Contains(body, "code_kind_is_channel") ||
			strings.Contains(body, "code_kind_channel(")
		if hasChannelKind {
			t.Fatalf("Channel escaped through %s", name)
		}
	}
}
