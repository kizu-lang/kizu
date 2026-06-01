package main

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostLLVMSymbolGateOutput = "llvm-symbol\n" +
	"kizu_kizu__lexer_first_token\n" +
	"kizu_kizu__lexer_next_token\n" +
	"kizu_std__mem_starts_with\n" +
	"kizu_selfhost__source_module_path\n"

// TestSelfhostLLVMSymbolGate executes the compiled_signature behavior gate that
// derives LLVM symbols from fully qualified Kizu function names via
// append_function_llvm_symbol, which the std lexer compiled closure now relies
// on instead of a handwritten symbol table (tracker 1112). It pins the
// std::kizu::* re-export mangling alongside ordinary std and selfhost modules.
func TestSelfhostLLVMSymbolGate(t *testing.T) {
	entry := "selfhost::backend::compiled_signature::gate_llvm_symbol"
	out, err := runSelfhostLLVMSymbolGate(t, entry)
	if err != nil {
		t.Fatalf("llvm symbol gate failed: %v\n%s", err, out)
	}
	if out != selfhostLLVMSymbolGateOutput {
		t.Fatalf("llvm symbol gate output mismatch\nwant:\n%sgot:\n%s", selfhostLLVMSymbolGateOutput, out)
	}
}

// runSelfhostLLVMSymbolGate loads the selfhost package and runs the given entry.
func runSelfhostLLVMSymbolGate(t *testing.T, entry string) (string, error) {
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
