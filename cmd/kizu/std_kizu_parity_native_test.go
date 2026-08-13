package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/native"
)

// buildNativeParityHarness compiles a generated parity harness to a native binary.
func buildNativeParityHarness(t *testing.T, dir string, name string, source string) string {
	t.Helper()
	path := filepath.Join(dir, name+".kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	module, err := lowerFile(path, false)
	if err != nil {
		t.Fatalf("lower %s harness: %v", name, err)
	}
	llvmIR, err := llvm.Emit(module)
	if err != nil {
		t.Fatalf("emit %s harness: %v", name, err)
	}
	binary := filepath.Join(dir, name)
	err = native.Build(native.Options{
		ErrorSets: stdErrorSets(),
		LLVMIR:    llvmIR, Output: binary,
		LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang",
	})
	if err != nil {
		t.Fatalf("link %s harness: %v", name, err)
	}
	return binary
}

// runNativeParityHarness executes a parity harness binary and returns its stdout.
func runNativeParityHarness(t *testing.T, binary string) string {
	t.Helper()
	command := exec.Command(binary)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("harness failed: %v\nstderr:\n%s\nstdout tail:\n%s",
			err, stderr.String(), tailForLog(string(output)))
	}
	return string(output)
}

// writeParityCaseFile stores one embedded case source for harness file reads.
func writeParityCaseFile(t *testing.T, dir string, index int, source string) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("case_%d.kizu", index))
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// tailForLog keeps failure logs readable when harness output is large.
func tailForLog(out string) string {
	const keep = 2000
	if len(out) <= keep {
		return out
	}
	return "..." + out[len(out)-keep:]
}
