package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// TestWASMBinaryMatchesText runs representative portable programs through
// Wasmtime and compares the binary renderer with the existing WAT renderer.
func TestWASMBinaryMatchesText(t *testing.T) {
	wasmtime, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime is required for binary execution")
	}
	examples := []string{
		"hello.kizu",
		"functions.kizu",
		"aggregate_calls.kizu",
		"function_pointer.kizu",
		"error_union_try.kizu",
		"std_array.kizu",
		"arena.kizu",
		"map_keys.kizu",
	}
	for _, example := range examples {
		t.Run(example, func(t *testing.T) {
			module, err := lowerFile(filepath.Join("../../examples", example), false)
			if err != nil {
				t.Fatalf("lower failed: %v", err)
			}
			ir.KeepReachableFunctions(module, "main")
			lowered, err := wasm.Lower(module)
			if err != nil {
				t.Fatalf("lower wasm failed: %v", err)
			}
			binary, err := lowered.Binary()
			if err != nil {
				t.Fatalf("encode binary failed: %v", err)
			}
			directory := t.TempDir()
			watPath := filepath.Join(directory, "program.wat")
			wasmPath := filepath.Join(directory, "program.wasm")
			if err := os.WriteFile(watPath, []byte(lowered.WAT()), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(wasmPath, binary, 0o644); err != nil {
				t.Fatal(err)
			}
			watOutput, err := exec.Command(wasmtime, "run", watPath).CombinedOutput()
			if err != nil {
				t.Fatalf("WAT execution failed: %v\n%s", err, watOutput)
			}
			binaryOutput, err := exec.Command(wasmtime, "run", wasmPath).CombinedOutput()
			if err != nil {
				t.Fatalf("binary execution failed: %v\n%s", err, binaryOutput)
			}
			if !bytes.Equal(binaryOutput, watOutput) {
				t.Fatalf("binary output %q differs from WAT output %q", binaryOutput, watOutput)
			}
		})
	}
}
