package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// TestSelfhostCompiledUnionDispatchUsesUnionFacts guards the generic non-bool
// match lowering used by ConstructorFacts: union scrutinees resolve tags from
// union-variant facts and the renderer dispatches on the aggregate tag field.
func TestSelfhostCompiledUnionDispatchUsesUnionFacts(t *testing.T) {
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	const entry = "selfhost::backend::compiled_mir_llvm::union_dispatch_renderer_gate"
	if err := interp.New(&out).RunEntry(program, entry); err != nil {
		t.Fatalf("union dispatch renderer gate failed: %v\n%s", err, out.String())
	}
	unionAndRest := strings.Split(out.String(), "void-dispatch-llvm\\n")
	if len(unionAndRest) != 2 {
		t.Fatalf("renderer gate void sections = %d, want 2\n%s", len(unionAndRest), out.String())
	}
	unionLLVM := strings.TrimPrefix(unionAndRest[0], "union-dispatch-llvm\\n")
	if !strings.Contains(unionLLVM, "extractvalue %test.union %value, 0") ||
		!strings.Contains(unionLLVM, "icmp eq i64 %dispatch0_tag0, 3") ||
		!strings.Contains(unionLLVM, "%dispatch.payload.test = load %test.payload") ||
		!strings.Contains(unionLLVM, "extractvalue %test.payload %dispatch.payload.test, 0") {
		t.Fatalf("union dispatch does not branch on aggregate tag\n%s", unionLLVM)
	}
	voidAndEnum := strings.Split(unionAndRest[1], "enum-dispatch-llvm\\n")
	if len(voidAndEnum) != 2 {
		t.Fatalf("renderer gate enum sections = %d, want 2\n%s", len(voidAndEnum), out.String())
	}
	voidLLVM := voidAndEnum[0]
	if !strings.Contains(voidLLVM, "%dispatch.payload.void = load %test.payload") ||
		strings.Count(voidLLVM, "ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }") != 2 {
		t.Fatalf("union dispatch bare-return arms do not return canonical !void success\n%s", voidLLVM)
	}
	assertFallthroughDispatchLLVM(t, voidAndEnum[1])
}

// assertFallthroughDispatchLLVM checks the mixed terminal/effect arm CFG.
func assertFallthroughDispatchLLVM(t *testing.T, output string) {
	t.Helper()
	enumAndFallthrough := strings.Split(output, "fallthrough-dispatch-llvm\\n")
	if len(enumAndFallthrough) != 2 {
		t.Fatalf(
			"renderer gate fallthrough sections = %d, want 2\n%s",
			len(enumAndFallthrough),
			output,
		)
	}
	if strings.Contains(enumAndFallthrough[0], "extractvalue") {
		t.Fatalf("plain enum dispatch unexpectedly extracts an aggregate tag\n%s", enumAndFallthrough[0])
	}
	if strings.Contains(enumAndFallthrough[0], "dispatcharg") {
		t.Fatalf(
			"non-effect dispatch unexpectedly uses the effect call-arg namespace\n%s",
			enumAndFallthrough[0],
		)
	}
	fallthroughAndBoundary := strings.Split(enumAndFallthrough[1], "enum-dispatch-boundary-llvm\\n")
	if len(fallthroughAndBoundary) != 2 {
		t.Fatalf(
			"renderer gate boundary sections = %d, want 2\n%s",
			len(fallthroughAndBoundary),
			enumAndFallthrough[1],
		)
	}
	fallthroughLLVM := fallthroughAndBoundary[0]
	boundaryLLVM := fallthroughAndBoundary[1]
	const canonicalSuccess = "ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }"
	const firstNestedCall = "%dispatcharg0_0_0_0_call = call i64 @kizu_testnested(" +
		"%kizu.slice.u8 %dispatcharg0_0_1_0_slice)"
	const firstEffectCall = "%voidtry7_call = call %kizu.error.void @kizu_testeffect(" +
		"i64 %dispatcharg0_0_0_0_call)"
	const terminalCall = "%enumdispatchcall0_2 = call %kizu.error.void @kizu_testterminal(" +
		"i64 %dispatcharg0_2_0_0_call)"
	firstTerminalThenJoin := "  ret %kizu.error.void %enumdispatchcall0_2\n" +
		"dispatch0_cont0:\n  %dispatch1_is_0"
	secondTerminalThenJoin := "dispatch1_arm_1:\n  " + canonicalSuccess +
		"\ndispatch1_cont0:\n  %arg1002000_0_ptr"
	const postMatchCall = "%ret_val_2 = call %kizu.error.void @kizu_testpost_match(" +
		"i64 %arg2_0_call)"
	if !strings.Contains(fallthroughLLVM, firstNestedCall) ||
		!strings.Contains(fallthroughLLVM, firstEffectCall) ||
		!strings.Contains(fallthroughLLVM, terminalCall) ||
		!strings.Contains(fallthroughLLVM, postMatchCall) ||
		!strings.Contains(fallthroughLLVM, "try7_cont:\n  br label %dispatch0_cont0") ||
		!strings.Contains(fallthroughLLVM, "try9_cont:\n  br label %dispatch0_cont0") ||
		!strings.Contains(fallthroughLLVM, "try10_cont:\n  br label %dispatch1_cont0") ||
		!strings.Contains(fallthroughLLVM, firstTerminalThenJoin) ||
		!strings.Contains(fallthroughLLVM, secondTerminalThenJoin) {
		t.Fatalf(
			"mixed terminal/fallthrough dispatch does not join after its effect arm\n%s",
			fallthroughLLVM,
		)
	}
	assertUniqueLLVMDefinitions(t, fallthroughLLVM, "dispatch call-arg", []string{
		"%dispatcharg0_0_1_0_ptr =",
		"%dispatcharg0_0_0_0_call =",
		"%dispatcharg0_1_1_0_ptr =",
		"%dispatcharg0_1_0_0_call =",
		"%dispatcharg0_2_1_0_ptr =",
		"%dispatcharg0_2_0_0_call =",
		"%dispatcharg1_0_1_0_ptr =",
		"%dispatcharg1_0_0_0_call =",
	})
	assertUniqueLLVMDefinitions(t, fallthroughLLVM, "post-match normal call-arg", []string{
		"%arg1002000_0_ptr =",
		"%arg2_0_call =",
	})
	assertEnumDispatchBoundaryLLVM(t, boundaryLLVM)
}

// assertEnumDispatchBoundaryLLVM guards tuple scopes beyond the old 1000-arm numeric window.
func assertEnumDispatchBoundaryLLVM(t *testing.T, boundaryLLVM string) {
	t.Helper()
	assertUniqueLLVMDefinitions(t, boundaryLLVM, "enum dispatch boundary", []string{
		"%dispatcharg0_1000_1_0_ptr =",
		"%dispatcharg0_1000_0_0_call =",
		"%enumdispatchcall0_1000 =",
		"%enumdispatchcall0_1000_wrap0 =",
		"%enumdispatchcall0_1000_wrap1 =",
		"%dispatcharg1_0_1_0_ptr =",
		"%dispatcharg1_0_0_0_call =",
		"%enumdispatchcall1_0 =",
		"%enumdispatchcall1_0_wrap0 =",
		"%enumdispatchcall1_0_wrap1 =",
		"%enumdispatchret0_1000_ok =",
		"%enumdispatchret0_1000_val =",
		"%enumdispatchret1_0_ok =",
		"%enumdispatchret1_0_val =",
	})
	if strings.Contains(boundaryLLVM, "%arg1000_") ||
		strings.Contains(boundaryLLVM, "%dispatchcall1000") ||
		strings.Contains(boundaryLLVM, "%dispatchret1000") {
		t.Fatalf(
			"enum terminal return still uses a numeric dispatch namespace\n%s",
			boundaryLLVM,
		)
	}
}

// assertUniqueLLVMDefinitions requires every expected SSA definition to occur exactly once.
func assertUniqueLLVMDefinitions(t *testing.T, llvm, label string, definitions []string) {
	t.Helper()
	for _, definition := range definitions {
		if strings.Count(llvm, definition) != 1 {
			t.Fatalf("%s definition %q is not unique\n%s", label, definition, llvm)
		}
	}
}
