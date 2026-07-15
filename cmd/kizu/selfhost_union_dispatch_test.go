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
	if strings.Contains(voidAndEnum[1], "extractvalue") {
		t.Fatalf("plain enum dispatch unexpectedly extracts an aggregate tag\n%s", voidAndEnum[1])
	}
}
