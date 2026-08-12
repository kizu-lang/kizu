package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

// runSelfhostPackageGate loads the selfhost package, checks it, and runs one
// named gate entry through the interpreter. It is the shared runner for every
// gate that asserts on a selfhost entry point's printed output.
func runSelfhostPackageGate(t *testing.T, entry string) (string, error) {
	t.Helper()
	_, program, err := loadPackageProgram("../../selfhost")
	if err != nil {
		return "", err
	}
	if err := checkProgram(program); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = interp.New(&out).RunEntry(program, entry)
	return out.String(), err
}
