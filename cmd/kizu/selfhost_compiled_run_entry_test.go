package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfhostCompiledRunEntryOwnsFactDrivenWrapper(t *testing.T) {
	emitter := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_run_entry_llvm.kizu",
	)
	contract := readSelfhostFile(
		t, "../../selfhost/src/abi/program_entry_contract.kizu",
	)
	errorABI := readSelfhostFile(
		t, "../../selfhost/src/backend/compiled_error_abi.kizu",
	)
	fixedABI := readSelfhostFile(
		t, "../../selfhost/src/abi/fixed_abi_contract.kizu",
	)
	hosted := readSelfhostFile(t, "../../selfhost/runtime/selfhost.hosted.c")
	cli := readSelfhostFile(t, "../../selfhost/src/backend/cli_llvm.kizu")

	body := selfhostKizuFunctionBody(t, emitter, "pub fn append_run_entry(")
	for _, fragment := range []string{
		"exact_program_entry(index, facts)",
		"require_definition_owner(index, facts, &program)",
		"require_zero_runtime_params(index, facts, program.canonical_name)",
		"compiled_type_resolver::resolve_function_indexed(",
		"compiled_signature::append_function_llvm_symbol(",
		"program_entry_contract::run_entry_symbol()",
		"append_error_result_branch(out, llvm_return, success_abi)",
		"append_c_main(out)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("compiled run entry owner missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		`"main"`,
		`"kizu_run_main"`,
		`"kizu_host_init"`,
		`"kizu_main_error_message"`,
		`"kizu_rt_process_exit_code"`,
		"ir_index::build(",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("compiled run entry owner contains forbidden source or ABI hardcode %q", forbidden)
		}
	}
	for _, fragment := range []string{
		"compiled_error_abi::success_flag_field_index()",
		"compiled_error_abi::message_field_index(success_abi)",
		"fixed_abi_contract::slice_pointer_field_index()",
		"fixed_abi_contract::slice_length_field_index()",
	} {
		if !strings.Contains(emitter, fragment) {
			t.Errorf("compiled run entry layout is not contract-owned: missing %q", fragment)
		}
	}
	for _, symbol := range []string{
		"kizu_run_main",
		"main",
		"kizu_host_init",
		"kizu_main_error_message",
		"kizu_rt_process_exit_code",
	} {
		if !strings.Contains(contract, `"`+symbol+`"`) {
			t.Errorf("program entry contract missing symbol %q", symbol)
		}
	}
	for _, fragment := range []string{
		"pub fn success_flag_field_index()",
		"pub fn message_field_index(payload_abi: []u8)",
	} {
		if !strings.Contains(errorABI, fragment) {
			t.Errorf("compiled error ABI missing layout owner %q", fragment)
		}
	}
	for _, fragment := range []string{
		"pub fn slice_pointer_field_index()",
		"pub fn slice_length_field_index()",
	} {
		if !strings.Contains(fixedABI, fragment) {
			t.Errorf("fixed ABI contract missing slice layout owner %q", fragment)
		}
	}
	if count := strings.Count(
		hosted, "void kizu_main_error_message(",
	); count != 1 {
		t.Fatalf("hosted main error boundary definition count = %d, want 1", count)
	}
	if strings.Contains(cli, "compiled_run_entry_llvm") {
		t.Fatal("CLI switched to compiled run entry before the dedicated integration slice")
	}
}

func TestSelfhostCompiledRunEntryLLVMShapes(t *testing.T) {
	cases := []struct {
		name        string
		entry       string
		declaration string
		types       string
		want        []string
	}{
		{
			name:        "void",
			entry:       "void_gate",
			declaration: "declare void @kizu_app__entry_start()\n",
			want: []string{
				"call void @kizu_app__entry_start()",
				"call i64 @kizu_rt_process_exit_code(i64 0)",
			},
		},
		{
			name:        "i64",
			entry:       "i64_gate",
			declaration: "declare i64 @kizu_app__entry_start()\n",
			want: []string{
				"%program_result = call i64 @kizu_app__entry_start()",
				"call i64 @kizu_rt_process_exit_code(i64 0)",
			},
		},
		{
			name:        "error_void",
			entry:       "error_void_gate",
			types:       "%kizu.slice.u8 = type { ptr, i64 }\n%kizu.error.void = type { i1, %kizu.slice.u8 }\n",
			declaration: "declare %kizu.error.void @kizu_app__entry_start()\n",
			want: []string{
				"extractvalue %kizu.error.void %program_result, 1",
				"call void @kizu_main_error_message(ptr %program_message_ptr, i64 %program_message_len)",
				"call i64 @kizu_rt_process_exit_code(i64 1)",
			},
		},
		{
			name:        "error_i64",
			entry:       "error_i64_gate",
			types:       "%kizu.slice.u8 = type { ptr, i64 }\n%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }\n",
			declaration: "declare %kizu.error.i64 @kizu_app__entry_start()\n",
			want: []string{
				"extractvalue %kizu.error.i64 %program_result, 2",
				"call void @kizu_main_error_message(ptr %program_message_ptr, i64 %program_message_len)",
				"call i64 @kizu_rt_process_exit_code(i64 1)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runSelfhostAbiParamsGate(
				t,
				"selfhost::backend::compiled_run_entry_llvm::"+tc.entry,
			)
			if err != nil {
				t.Fatalf("compiled run entry gate failed: %v\n%s", err, out)
			}
			for _, common := range []string{
				"declare void @kizu_host_init(i32, ptr)",
				"declare void @kizu_main_error_message(ptr, i64)",
				"declare i64 @kizu_rt_process_exit_code(i64)",
				"define i64 @kizu_run_main()",
				"define i32 @main(i32 %argc, ptr %argv)",
				"call void @kizu_host_init(i32 %argc, ptr %argv)",
			} {
				if !strings.Contains(out, common) {
					t.Errorf("compiled run entry output missing %q\n%s", common, out)
				}
			}
			for _, fragment := range tc.want {
				if !strings.Contains(out, fragment) {
					t.Errorf("compiled run entry output missing %q\n%s", fragment, out)
				}
			}
			verifyCompiledRunEntryLLVM(
				t, tc.types+tc.declaration+out,
			)
		})
	}
}

func TestSelfhostCompiledRunEntryRejectsInvalidFacts(t *testing.T) {
	cases := []struct {
		entry string
		want  string
	}{
		{"duplicate_program_entry_gate", "exactly one program-entry fact required"},
		{"numeric_owner_mismatch_gate", "canonical name owner mismatch"},
		{"unsupported_return_gate", "unsupported return ABI"},
		{"missing_signature_gate", "function signature not found"},
		{"duplicate_signature_gate", "duplicate exact fact"},
		{"runtime_param_gate", "program entry parameters unsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			out, err := runSelfhostAbiParamsGate(
				t,
				"selfhost::backend::compiled_run_entry_llvm::"+tc.entry,
			)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compiled run entry error = %v, want %q\n%s", err, tc.want, out)
			}
		})
	}
}

func verifyCompiledRunEntryLLVM(t *testing.T, llvm string) {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is required for the compiled run entry LLVM verifier")
	}
	dir := t.TempDir()
	llPath := filepath.Join(dir, "run-entry.ll")
	objPath := filepath.Join(dir, "run-entry.o")
	if err := os.WriteFile(llPath, []byte(llvm), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(
		clang,
		"-Wno-override-module",
		"-x", "ir",
		"-c", llPath,
		"-o", objPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled run entry LLVM verification failed: %v\n%s\n%s", err, out, llvm)
	}
}
