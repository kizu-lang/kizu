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
	sections := strings.Split(out.String(), "enum-dispatch-llvm")
	if len(sections) != 2 {
		t.Fatalf("renderer gate sections = %d, want 2\n%s", len(sections), out.String())
	}
	unionLLVM := strings.TrimPrefix(sections[0], "union-dispatch-llvm\\n")
	if !strings.Contains(unionLLVM, "extractvalue %test.union %value, 0") ||
		!strings.Contains(unionLLVM, "icmp eq i64 %dispatch0_tag0, 3") {
		t.Fatalf("union dispatch does not branch on aggregate tag\n%s", unionLLVM)
	}
	if strings.Contains(sections[1], "extractvalue") {
		t.Fatalf("plain enum dispatch unexpectedly extracts an aggregate tag\n%s", sections[1])
	}
}
