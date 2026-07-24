package main

import (
	"strings"
	"testing"
)

func TestSelfhostTraversalLiteralArgsCarryScalarTypes(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_lower::gate_traversal_typed_literal_args",
	)
	if err != nil {
		t.Fatalf("typed traversal literal lower gate failed: %v\n%s", err, out)
	}
	if out != "i64\n7\ni1\n1\ni1\n0\n" {
		t.Fatalf("typed traversal literal MIR mismatch: %q", out)
	}
}

func TestSelfhostTraversalLiteralArgsRenderTypedLLVM(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::traversal_typed_literal_renderer_gate",
	)
	if err != nil {
		t.Fatalf("typed traversal literal renderer gate failed: %v\n%s", err, out)
	}
	if !strings.Contains(
		out,
		"call i64 @kizu_app__visit(i64 7, i1 true, i1 false)",
	) {
		t.Fatalf("typed traversal literal call args missing from LLVM:\n%s", out)
	}
}

func TestSelfhostTraversalArgsUseCommonExpressionPath(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_lower::gate_traversal_common_args",
	)
	if err != nil {
		t.Fatalf("common traversal arg lower gate failed: %v\n%s", err, out)
	}
	want := "1\n14\n0\n1\n1\n0\n13\n20\n14\n13\n0\npayload\n0\n"
	if out != want {
		t.Fatalf("common traversal arg MIR mismatch:\n got %q\nwant %q", out, want)
	}
}

func TestSelfhostTraversalCommonArgsRenderLLVM(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(
		t, "selfhost::backend::compiled_mir_llvm::traversal_common_args_renderer_gate",
	)
	if err != nil {
		t.Fatalf("common traversal arg renderer gate failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		`@.kizu.compiled.traversal_common_args_renderer_gate.s0 = private unnamed_addr constant [4 x i8] c"void"`,
		"%t3 = extractvalue %test.payload %match_arm_0_payload, 1",
		"%t5 = call i64 @kizu_app__inner(i64 %seed)",
		"call i64 @kizu_app__visit(%test.tree %tree, %kizu.slice.u8 %dispatcharg0_0_0_1_slice, i64 %t5, i64 %t3)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("common traversal LLVM missing %q:\n%s", want, out)
		}
	}
}

func TestSelfhostTraversalArgModelUsesCommonMIR(t *testing.T) {
	mir := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir.kizu")
	for _, stale := range []string{
		"struct MirTraversalArg",
		"fn traversal_param_arg(",
		"fn traversal_typed_literal_arg(",
	} {
		if strings.Contains(mir, stale) {
			t.Fatalf("legacy traversal arg model remains: %s", stale)
		}
	}
	requireSourceFragments(t, "common traversal arg ownership", mir, []string{
		"arg_insts: std::array::Array<MirExprInst>",
		"call_args: std::array::Array<MirCallArg>",
		"payload_args: std::array::Array<MirTraversalPayloadArg>",
	})
}
