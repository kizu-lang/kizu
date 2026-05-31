package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
)

const selfhostAbiParamsGateOutput = "abi-params-spec\n" +
	"i8 byte;i64 count;i64 offset;%kizu.slice.u8 source;" +
	"%kizu.kizu.lexer.position pos;%kizu.kizu.lexer.token tok;i8 ref\n" +
	"abi-params-count\n" +
	"7\n"

// TestSelfhostAbiParamsGate executes the compiled_abi_params behavior gate that
// derives a compiled params_spec from fake function-signature-param facts
// (Agent B, tracker 1069).
func TestSelfhostAbiParamsGate(t *testing.T) {
	out, err := runSelfhostAbiParamsGate(t, "selfhost::backend::compiled_abi_params::gate")
	if err != nil {
		t.Fatalf("abi params gate failed: %v\n%s", err, out)
	}
	if out != selfhostAbiParamsGateOutput {
		t.Fatalf("abi params gate output mismatch\nwant:\n%sgot:\n%s", selfhostAbiParamsGateOutput, out)
	}
}

// TestSelfhostAbiParamsGateUnsupportedType confirms an unsupported parameter
// type is an explicit error rather than a silently-resolved guess.
func TestSelfhostAbiParamsGateUnsupportedType(t *testing.T) {
	entry := "selfhost::backend::compiled_abi_params::" +
		"gate_unsupported_type"
	out, err := runSelfhostAbiParamsGate(t, entry)
	if err == nil {
		t.Fatalf("abi params gate accepted unsupported type, want error\n%s", out)
	}
	if !strings.Contains(err.Error(), "abi mapper: unsupported parameter type") {
		t.Fatalf("abi params gate error mismatch: %v", err)
	}
}

// runSelfhostAbiParamsGate loads the selfhost package and runs the given entry.
func runSelfhostAbiParamsGate(t *testing.T, entry string) (string, error) {
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
