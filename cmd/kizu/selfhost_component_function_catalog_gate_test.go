package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostComponentFunctionCatalogGateOutput = `component-catalog-functions
2
component-catalog-add-index
0
component-catalog-zero-index
1
`

// TestSelfhostComponentFunctionCatalogGate runs the selfhost component function catalog gate.
func TestSelfhostComponentFunctionCatalogGate(t *testing.T) {
	out, err := runSelfhostComponentFunctionCatalogGate(t)
	if err != nil {
		t.Fatalf("component function catalog gate failed: %v\n%s", err, out)
	}
	if out != selfhostComponentFunctionCatalogGateOutput {
		t.Fatalf(
			"component function catalog gate output mismatch\nwant:\n%sgot:\n%s",
			selfhostComponentFunctionCatalogGateOutput,
			out,
		)
	}
}

// runSelfhostComponentFunctionCatalogGate invokes the gate through the selfhost interpreter.
func runSelfhostComponentFunctionCatalogGate(t *testing.T) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, "selfhost::ir::component_function_catalog::gate")
	return out.String(), err
}
