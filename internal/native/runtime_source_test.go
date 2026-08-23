package native

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateRuntimeSource = flag.Bool("update", false,
	"rewrite the selfhost runtime_source.kizu from runtime/runtime.c")

// runtimeSourceKizuPath locates the generated selfhost copy of the runtime
// source, relative to this package.
const runtimeSourceKizuPath = "../../compiler/src/internal/native/runtime_source.kizu"

// runtimeSourceKizuHeader is everything in the generated file before the
// multi-line literal rows.
const runtimeSourceKizuHeader = `// Code generated from internal/native/runtime/runtime.c by
// ` + "`go test ./internal/native -run TestRuntimeSourceKizu -update`" + `. DO NOT EDIT.

/// Returns the C runtime source the native backend compiles and links into
/// every executable, byte-identical to the Go ` + "`runtimeSource`" + `.
pub fn runtime_source() -> []u8 {
    return
`

// runtimeSourceKizuFooter closes the literal and the function.
const runtimeSourceKizuFooter = `    ;
}
`

// renderRuntimeSourceKizu writes the runtime C source as one Kizu multi-line
// literal: each line of the source becomes one `\\` row, and joining the rows
// with newlines is exactly the source again.
func renderRuntimeSourceKizu(source string) string {
	var out strings.Builder
	out.WriteString(runtimeSourceKizuHeader)
	for _, line := range strings.Split(source, "\n") {
		out.WriteString("        \\\\")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteString(runtimeSourceKizuFooter)
	return out.String()
}

// TestRuntimeSourceKizu gates the selfhost copy of the runtime C source: the
// bytes the Kizu literal spells must be the bytes the Go compiler embeds.
// This is a data identity gate, not a structure pin: the runtime source is
// one constant carried by both compilers, and drifting copies would link
// programs against two different runtimes. With -update it rewrites the
// generated file from runtime/runtime.c.
func TestRuntimeSourceKizu(t *testing.T) {
	want := renderRuntimeSourceKizu(runtimeSource)
	if *updateRuntimeSource {
		if err := os.MkdirAll(filepath.Dir(runtimeSourceKizuPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(runtimeSourceKizuPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(runtimeSourceKizuPath)
	if err != nil {
		t.Fatalf("%v (regenerate with go test ./internal/native -run TestRuntimeSourceKizu -update)", err)
	}
	if string(got) != want {
		t.Fatalf("%s does not match runtime/runtime.c; regenerate with "+
			"go test ./internal/native -run TestRuntimeSourceKizu -update", runtimeSourceKizuPath)
	}
}
