package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
)

// runRenderCase is one end-to-end tape-render case: a kizu source that lowers onto
// the run codegen IR v1 tape, renders to LLVM, links, runs, and prints wantStdout.
type runRenderCase struct {
	name                     string
	source                   string
	wantStdout               string
	wantExit                 int
	wantLLVMFragments        []string
	wantLLVMOrderedFragments []string
}

// TestSelfhostRunRenderGate drives the tape renderer end to end: it lowers a kizu
// source onto the run codegen IR v1 tape (selfhost::ir::codegen::lower_code_module),
// renders the run artifact LLVM (selfhost::ir::code_render::render_tape_module)
// through the interpreter, links the module with the hosted runtime, runs it, and
// asserts the program output. It proves the renderer produces a correct executable
// before the hosted run CLI is switched onto the tape (#1255 slice 4).
func TestSelfhostRunRenderGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_RUN_RENDER") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_RUN_RENDER=1 to run the selfhost run render gate")
	}
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skipf("clang is required for the run render gate: %v", err)
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()
	_, program, err := loadPackageProgram("selfhost")
	if err != nil {
		t.Fatalf("load selfhost: %v", err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("check selfhost: %v", err)
	}
	if err := os.MkdirAll("target/selfhost", 0o755); err != nil {
		t.Fatalf("prepare target dir: %v", err)
	}
	cases := append(runRenderCases(), runRenderLoopCases()...)
	cases = append(cases, runRenderBoolCases()...)
	cases = append(cases, runRenderReturnCases()...)
	cases = append(cases, runRenderNestedCallCases()...)
	cases = append(cases, runRenderShadowingCases()...)
	cases = append(cases, runRenderErrorUnionI64Cases()...)
	cases = append(cases, runRenderExpectCases()...)
	cases = append(cases, runRenderModPrintBoolCases()...)
	cases = append(cases, runRenderLabeledLoopCases()...)
	cases = append(cases, runRenderMultilineStringCases()...)
	cases = append(cases, runRenderSliceParamCases()...)
	cases = append(cases, runRenderIoWriteCases()...)
	cases = append(cases, runRenderStringBuilderCases()...)
	cases = append(cases, runRenderOwnedArrayCases()...)
	cases = append(cases, runRenderCleanupCases()...)
	cases = append(cases, runRenderCleanupControlFlowCases()...)
	cases = append(cases, runRenderPathJoinCleanCases()...)
	cases = append(cases, runRenderStructCases()...)
	cases = append(cases, runRenderEnumMatchCases()...)
	cases = append(cases, runRenderVarStructCases()...)
	cases = append(cases, runRenderUnionPayloadCases()...)
	cases = append(cases, runRenderUnionAbiCases()...)
	cases = append(cases, runRenderValueExprCases()...)
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			runOneRenderCase(t, program, clang, item)
		})
	}
}

// runRenderCases is the corpus the tape renderer covers in this slice.
func runRenderCases() []runRenderCase {
	return []runRenderCase{
		{
			name:       "const_string_print",
			source:     "fn main() {\n    print(\"hello, kizu\");\n}\n",
			wantStdout: "hello, kizu\n",
		},
		{
			name:       "i64_arithmetic_print",
			source:     "fn main() {\n    print(1 + 2);\n}\n",
			wantStdout: "3\n",
		},
		{
			name: "user_function_call",
			source: "fn add(a: i64, b: i64) -> i64 {\n    return a + b;\n}\n" +
				"\nfn main() {\n    print(add(1, 2));\n}\n",
			wantStdout: "3\n",
		},
		{
			name: "user_function_nested_arith",
			source: "fn mul(a: i64, b: i64) -> i64 {\n    return a * b;\n}\n" +
				"\nfn main() {\n    print(mul(4, 5));\n}\n",
			wantStdout: "20\n",
		},
		{
			name: "user_function_prints",
			source: "fn announce(value: i64) -> i64 {\n    print(value);\n    return value;\n}\n" +
				"\nfn main() {\n    print(announce(7));\n}\n",
			wantStdout: "7\n7\n",
		},
		{
			name: "if_else_true_branch",
			source: "fn main() {\n    let age = 20;\n    if age >= 20 {\n" +
				"        print(\"adult\");\n    } else {\n        print(\"minor\");\n    }\n}\n",
			wantStdout: "adult\n",
		},
		{
			name: "if_else_false_branch",
			source: "fn main() {\n    let age = 15;\n    if age >= 20 {\n" +
				"        print(\"adult\");\n    } else {\n        print(\"minor\");\n    }\n}\n",
			wantStdout: "minor\n",
		},
		{
			name: "if_no_else",
			source: "fn main() {\n    let n = 3;\n    if n == 3 {\n        print(\"three\");\n    }\n" +
				"    print(\"done\");\n}\n",
			wantStdout: "three\ndone\n",
		},
	}
}

// runRenderLoopCases is the control-flow corpus (while/for + var + break/continue)
// the tape renderer covers in this slice, kept separate so each case list stays
// within the per-function length budget.
func runRenderLoopCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "while_counter",
			source: "fn main() {\n    var i = 0;\n    while i < 3 {\n        print(i);\n" +
				"        i = i + 1;\n    }\n    print(\"done\");\n}\n",
			wantStdout: "0\n1\n2\ndone\n",
		},
		{
			name: "while_break_continue",
			source: "fn main() {\n    var i = 0;\n    while i < 7 {\n        i = i + 1;\n" +
				"        if i == 3 {\n            continue;\n        }\n" +
				"        if i == 6 {\n            break;\n        }\n        print(i);\n    }\n}\n",
			wantStdout: "1\n2\n4\n5\n",
		},
		{
			name:       "for_range",
			source:     "fn main() {\n    for 0..4 |n| {\n        print(n);\n    }\n}\n",
			wantStdout: "0\n1\n2\n3\n",
		},
		{
			name: "for_range_break_continue",
			source: "fn main() {\n    for 2..7 |n| {\n        if n == 3 {\n" +
				"            continue;\n        }\n" +
				"        if n == 6 {\n            break;\n        }\n        print(n);\n    }\n}\n",
			wantStdout: "2\n4\n5\n",
		},
	}
}

// runRenderBoolCases is the bool corpus (bool literals, short-circuit and/or,
// logical not, bool parameters) the tape renderer covers in this slice.
func runRenderBoolCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "while_true_break",
			source: "fn main() {\n    var i = 0;\n    while true {\n        print(i);\n" +
				"        i = i + 1;\n        if i == 2 {\n            break;\n        }\n    }\n}\n",
			wantStdout: "0\n1\n",
		},
		{
			name: "if_bool_literal_false",
			source: "fn main() {\n    if false {\n        print(\"not executed\");\n    }\n" +
				"    print(\"done\");\n}\n",
			wantStdout: "done\n",
		},
		{
			name: "logical_and_or",
			source: "fn main() {\n    let age = 30;\n    let admin = false;\n" +
				"    if age >= 20 and age < 130 {\n        print(\"and-ok\");\n    }\n" +
				"    if (age < 20 and admin) or age == 30 {\n        print(\"or-ok\");\n    }\n" +
				"    if admin or age < 20 {\n        print(\"unexpected\");\n    }\n}\n",
			wantStdout: "and-ok\nor-ok\n",
		},
		{
			name: "logical_not",
			source: "fn main() {\n    let admin = false;\n    if !admin {\n" +
				"        print(\"not-ok\");\n    }\n    if !(1 == 2) {\n        print(\"ne-ok\");\n    }\n}\n",
			wantStdout: "not-ok\nne-ok\n",
		},
		{
			// The right operand divides by the guarded zero, so this output is only
			// reachable when 'and' really short-circuits.
			name: "short_circuit_division_guard",
			source: "fn main() {\n    let x = 0;\n    if x != 0 and 100 / x > 0 {\n" +
				"        print(\"unexpected\");\n    } else {\n        print(\"guarded\");\n    }\n}\n",
			wantStdout: "guarded\n",
		},
		{
			name: "bool_parameter_call",
			source: "fn pick(flag: bool) -> i64 {\n    var result = 2;\n    if flag {\n" +
				"        result = 1;\n    }\n    return result;\n}\n" +
				"\nfn main() {\n    print(pick(true));\n    print(pick(false));\n}\n",
			wantStdout: "1\n2\n",
		},
	}
}

// runRenderReturnCases is the early-return corpus (returns inside nested blocks,
// statement-position void/i64 user calls) the tape renderer covers in this slice.
func runRenderReturnCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "early_return_in_if_i64",
			source: "fn pick(flag: bool) -> i64 {\n    if flag {\n        return 1;\n    }\n" +
				"    return 2;\n}\n\nfn main() {\n    print(pick(true));\n    print(pick(false));\n}\n",
			wantStdout: "1\n2\n",
		},
		{
			name: "void_call_statement_early_return",
			source: "fn done(ok: bool) -> void {\n    if ok {\n        print(\"done\");\n" +
				"        return;\n    }\n\n    print(\"not done\");\n}\n\nfn main() {\n    done(true);\n}\n",
			wantStdout: "done\n",
		},
		{
			name: "main_early_return",
			source: "fn main() {\n    print(\"a\");\n    if 1 == 1 {\n        return;\n    }\n" +
				"    print(\"b\");\n}\n",
			wantStdout: "a\n",
		},
		{
			name: "early_return_in_while",
			source: "fn first_over(limit: i64) -> i64 {\n    var i = 0;\n    while true {\n" +
				"        if i > limit {\n            return i;\n        }\n        i = i + 1;\n    }\n" +
				"    return 0;\n}\n\nfn main() {\n    print(first_over(3));\n}\n",
			wantStdout: "4\n",
		},
	}
}

// runRenderNestedCallCases is the nested-call-argument corpus (calls inside call
// arguments at arbitrary depth) the tape renderer covers in this slice.
func runRenderNestedCallCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "nested_call_argument",
			source: "fn add(a: i64, b: i64) -> i64 {\n    return a + b;\n}\n\n" +
				"fn mul(a: i64, b: i64) -> i64 {\n    return a * b;\n}\n\n" +
				"fn main() {\n    print(add(mul(2, 3), 4));\n}\n",
			wantStdout: "10\n",
		},
		{
			name: "deeply_nested_call_arguments",
			source: "fn add(a: i64, b: i64) -> i64 {\n    return a + b;\n}\n\n" +
				"fn main() {\n    print(add(add(add(1, 2), add(3, 4)), add(5, 6)));\n}\n",
			wantStdout: "21\n",
		},
		{
			name: "nested_call_in_binary",
			source: "fn double(n: i64) -> i64 {\n    return n * 2;\n}\n\n" +
				"fn main() {\n    let v = double(3) + double(4);\n    print(v);\n}\n",
			wantStdout: "14\n",
		},
	}
}

// runRenderShadowingCases is the block-scope corpus (inner-scope shadowing, the
// outer binding resolving again after the block, loop-scoped induction names)
// the tape renderer covers in this slice.
func runRenderShadowingCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "block_shadowing",
			source: "fn main() {\n    let m = 1;\n    if 1 == 1 {\n        let m = 2;\n" +
				"        print(m);\n    }\n    print(m);\n}\n",
			wantStdout: "2\n1\n",
		},
		{
			name: "var_block_shadowing",
			source: "fn main() {\n    var x = 1;\n    if 1 == 1 {\n        var x = 10;\n" +
				"        x = x + 1;\n        print(x);\n    }\n    print(x);\n}\n",
			wantStdout: "11\n1\n",
		},
		{
			name: "for_induction_scope",
			source: "fn main() {\n    let n = 5;\n    for 0..2 |n| {\n        print(n);\n    }\n" +
				"    print(n);\n}\n",
			wantStdout: "0\n1\n5\n",
		},
		{
			name: "let_after_loop_reuses_induction_name",
			source: "fn main() {\n    for 0..2 |i| {\n        print(i);\n    }\n" +
				"    let i = 9;\n    print(i);\n}\n",
			wantStdout: "0\n1\n9\n",
		},
	}
}

// runRenderErrorUnionI64Cases is the error-union-i64 corpus ('-> !i64' functions,
// value-yielding try in let position, discarded try statements, propagation
// through !void and !i64 callers, and main discarding its return value).
func runRenderErrorUnionI64Cases() []runRenderCase {
	return []runRenderCase{
		{
			name: "try_i64_let_value",
			source: "fn parse() -> !i64 {\n    return 1;\n}\n\n" +
				"fn main() -> !i64 {\n    let value = try parse();\n    print(value);\n" +
				"    return value + 1;\n}\n",
			wantStdout: "1\n",
		},
		{
			name: "try_i64_statement_discard",
			source: "fn parse() -> !i64 {\n    return 1;\n}\n\n" +
				"fn main() -> !void {\n    try parse();\n    print(\"reached\");\n    return;\n}\n",
			wantStdout: "reached\n",
		},
		{
			// the propagate blocks render (and must compile) even on the success
			// path; the failing-path exit code is covered by the parity manifest.
			name: "try_i64_through_err_void_and_err_i64",
			source: "fn base() -> !i64 {\n    return 7;\n}\n\n" +
				"fn doubled() -> !i64 {\n    let v = try base();\n    return v * 2;\n}\n\n" +
				"fn step() -> !void {\n    let v = try doubled();\n    print(v);\n    return;\n}\n\n" +
				"fn main() -> !void {\n    try step();\n    print(\"done\");\n    return;\n}\n",
			wantStdout: "14\ndone\n",
		},
		{
			name:       "main_return_value_exits_zero",
			source:     "fn main() -> i64 {\n    print(\"v\");\n    return 5;\n}\n",
			wantStdout: "v\n",
		},
		{
			name: "typed_error_i64_success",
			source: "union ConfigError {\n    NotFound([]u8),\n    InvalidPort(i64),\n}\n\n" +
				"fn read_port(ok: bool) -> ConfigError!i64 {\n" +
				"    if ok {\n        return 8080;\n    }\n" +
				"    return ConfigError::NotFound(\"config.kizu\");\n}\n\n" +
				"fn main() -> ConfigError!void {\n" +
				"    let port = try read_port(true);\n    print(port);\n    return;\n}\n",
			wantStdout: "8080\n",
			wantLLVMFragments: []string{
				"%kizu.error.union.i64 = type { i1, i64, %kizu.run.union }",
				"define %kizu.error.union.i64 @kizu_run_user_read_port",
				"call %kizu.error.union.i64 @kizu_run_user_read_port",
				"insertvalue %kizu.error.union.i64 { i1 false",
			},
		},
		{
			name: "typed_error_i64_failure_exits",
			source: "union ConfigError {\n    NotFound([]u8),\n    InvalidPort(i64),\n}\n\n" +
				"fn read_port(ok: bool) -> ConfigError!i64 {\n" +
				"    if ok {\n        return 8080;\n    }\n" +
				"    return ConfigError::NotFound(\"config.kizu\");\n}\n\n" +
				"fn step() -> ConfigError!void {\n" +
				"    let port = try read_port(false);\n    print(port);\n    return;\n}\n\n" +
				"fn main() -> ConfigError!void {\n" +
				"    try step();\n    print(\"unreachable\");\n    return;\n}\n",
			wantExit: 1,
			wantLLVMOrderedFragments: []string{
				"define %kizu.error.union.void @kizu_run_user_step",
				"call %kizu.error.union.i64 @kizu_run_user_read_port",
				"extractvalue %kizu.error.union.i64 %tc",
				"insertvalue %kizu.error.union.void { i1 false",
				"define i64 @kizu_run_main()",
				"call %kizu.error.union.void @kizu_run_user_step",
			},
		},
	}
}

// runRenderExpectCases is the std::testing::expect corpus (passing expectations
// over comparisons and short-circuit logic; the failing trap path is covered by
// the parity manifest).
func runRenderExpectCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "expect_passing_conditions",
			source: "fn main() -> !void {\n    let age = 30;\n    let admin = false;\n" +
				"    std::testing::expect(age >= 20 and age < 130);\n" +
				"    std::testing::expect(age < 20 or age >= 30);\n" +
				"    std::testing::expect(!admin);\n    print(\"ok\");\n    return;\n}\n",
			wantStdout: "ok\n",
		},
	}
}

// runRenderModPrintBoolCases is the remainder-operator and print(bool) corpus.
func runRenderModPrintBoolCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "mod_operator",
			source: "fn main() {\n    print(7 % 3);\n    print(10 % 2);\n" +
				"    if 9 % 4 == 1 {\n        print(\"mod-ok\");\n    }\n}\n",
			wantStdout: "1\n0\nmod-ok\n",
		},
		{
			name: "print_bool_values",
			source: "fn main() {\n    let t = 1 == 1;\n    print(t);\n    print(false);\n" +
				"    print(2 < 1 or true);\n}\n",
			wantStdout: "true\nfalse\ntrue\n",
		},
	}
}

// runRenderLabeledLoopCases is the labeled-loop corpus (break/continue naming an
// outer while/for target).
func runRenderLabeledLoopCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "labeled_while_break",
			source: "fn main() -> void {\n    var i = 0;\n    outer: while i < 3 {\n" +
				"        var j = 0;\n        while j < 3 {\n            if i == 1 {\n" +
				"                break :outer;\n            }\n            print(i * 10 + j);\n" +
				"            j = j + 1;\n        }\n        i = i + 1;\n    }\n}\n",
			wantStdout: "0\n1\n2\n",
		},
		{
			name: "labeled_for_continue",
			source: "fn main() {\n    rows: for 0..3 |i| {\n        for 0..3 |j| {\n" +
				"            if j == 1 {\n                continue :rows;\n            }\n" +
				"            print(i * 10 + j);\n        }\n    }\n}\n",
			wantStdout: "0\n10\n20\n",
		},
	}
}

// runRenderMultilineStringCases is the multiline string literal corpus ('\\'
// lines joined with newlines, including an empty line and an embedded quote).
func runRenderMultilineStringCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "multiline_string_let_print",
			source: "fn main() {\n    let help =\n        \\\\Usage: kizu <command>\n" +
				"        \\\\\n        \\\\Commands:\n        \\\\  build    Build the project\n" +
				"        \\\\  run      Run the project\n    ;\n    print(help);\n}\n",
			wantStdout: "Usage: kizu <command>\n\nCommands:\n" +
				"  build    Build the project\n  run      Run the project\n",
		},
		{
			name:       "multiline_string_quote_payload",
			source:     "fn main() {\n    print(\n        \\\\say \"hi\" twice\n    );\n}\n",
			wantStdout: "say \"hi\" twice\n",
		},
	}
}

// runRenderSliceParamCases is the []u8-parameter corpus (string literal and local
// string variable arguments, and a slice parameter passed through to another call).
func runRenderSliceParamCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "slice_param_literal_and_local",
			source: "fn echo(message: []u8) -> void {\n    print(message);\n}\n\n" +
				"fn main() {\n    echo(\"hello\");\n    let greeting = \"good morning\";\n" +
				"    echo(greeting);\n}\n",
			wantStdout: "hello\ngood morning\n",
		},
		{
			name: "slice_param_passthrough",
			source: "fn echo(message: []u8) -> void {\n    print(message);\n}\n\n" +
				"fn shout(message: []u8) -> void {\n    echo(message);\n    echo(message);\n}\n\n" +
				"fn main() {\n    shout(\"twice\");\n}\n",
			wantStdout: "twice\ntwice\n",
		},
	}
}

// runRenderIoWriteCases is the std::io::write_stdout/write_stderr corpus (string
// literal payloads and a stderr write between stdout writes; stderr content is
// covered by the parity manifest).
func runRenderIoWriteCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "io_write_stdout_literals",
			source: "fn main() -> !void {\n    let io = std::io::blocking();\n" +
				"    try std::io::write_stdout(io, \"alpha \");\n" +
				"    try std::io::write_stderr(io, \"to stderr\");\n" +
				"    try std::io::write_stdout(io, \"beta\");\n    return;\n}\n",
			wantStdout: "alpha beta",
		},
	}
}

// runRenderStringBuilderCases covers the bounded std::string::String and std::fmt
// helper shape used by examples/std_fmt.kizu.
func runRenderStringBuilderCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "std_fmt_string_builder",
			source: "fn main() -> !void {\n    let allocator = std::mem::page_allocator();\n" +
				"    var text = std::string::String(allocator);\n\n" +
				"    try std::fmt::append_i64(text, 0);\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_i64(text, (0 - 9223372036854775807) - 1);\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_i64(text, 42);\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_bool(text, true);\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_bool(text, false);\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_bytes_literal(text, \"token\");\n" +
				"    try text.append_byte(cast<u8>(32));\n" +
				"    try std::fmt::append_bytes_literal(text, \"a\\b\");\n\n" +
				"    let bytes = text.as_bytes();\n    print(bytes);\n    return;\n}\n",
			wantStdout: "0 -9223372036854775808 42 true false \"token\" \"a\\\\b\"\n",
		},
		{
			name: "std_string_mut_borrow_param",
			source: "fn append_suffix(text: &var std::string::String) -> !void {\n" +
				"    try text.append_bytes(\" suffix\");\n" +
				"    text.clear();\n" +
				"    try text.append_bytes(\"mutated\");\n" +
				"    return;\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    var text = std::string::String(allocator);\n" +
				"    try text.append_bytes(\"prefix\");\n" +
				"    try append_suffix(text);\n" +
				"    let bytes = text.as_bytes();\n" +
				"    print(bytes);\n" +
				"    return;\n}\n",
			wantStdout: "mutated\n",
			wantLLVMFragments: []string{
				"define %kizu.error.void @kizu_run_user_append_suffix(%kizu.owned",
				"call %kizu.error.void @kizu_rt_string_append_bytes(%kizu.owned",
				"_len_field = getelementptr inbounds %kizu.rt.string",
			},
		},
	}
}

// runRenderOwnedArrayCases covers runtime-owned Array handles returned through
// the shared '%kizu.error.owned' ABI.
func runRenderOwnedArrayCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "errdefer_returns_owned_array_i64",
			source: "fn make_values(allocator: Allocator) -> !std::array::Array<i64> {\n" +
				"    let values = std::array::Array<i64>(allocator);\n" +
				"    errdefer values.deinit();\n" +
				"\n" +
				"    try values.append(1);\n" +
				"    try values.append(2);\n" +
				"    return values;\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    let values = try make_values(allocator);\n" +
				"    defer values.deinit();\n" +
				"\n" +
				"    std::testing::expect_equal<i64>(2, values.len());\n" +
				"    std::testing::expect_equal<i64>(1, values.get_or_panic(0));\n" +
				"    std::testing::expect_equal<i64>(2, values.get_or_panic(1));\n" +
				"    print(\"ok\");\n" +
				"    return;\n}\n",
			wantStdout: "ok\n",
			wantLLVMFragments: []string{
				"_fail:\n  call void @kizu_rt_array_deinit(%kizu.owned %v",
			},
		},
		{
			name: "errdefer_return_error_deinits_array_i64",
			source: "fn fail_values(allocator: Allocator) -> !void {\n" +
				"    let values = std::array::Array<i64>(allocator);\n" +
				"    errdefer values.deinit();\n" +
				"    return error(\"boom\");\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    try fail_values(allocator);\n" +
				"    print(\"unreachable\");\n" +
				"    return;\n}\n",
			wantExit: 1,
			wantLLVMFragments: []string{
				"call void @kizu_rt_array_deinit(%kizu.owned %v",
				"ret %kizu.error.void { i1 false",
			},
		},
		{
			name: "std_array_single_field_struct_borrow",
			source: "struct Token {\n    text: []u8,\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    var tokens = std::array::Array<Token>(allocator);\n" +
				"    try tokens.append(Token { text: \"let\" });\n" +
				"    let first = try tokens.at(0);\n" +
				"    print(first.text);\n" +
				"    let slot = try tokens.at_mut(0);\n" +
				"    slot.* = Token { text: \"var\" };\n" +
				"    let updated = try tokens.at(0);\n" +
				"    print(updated.text);\n" +
				"    tokens.deinit();\n" +
				"    return;\n}\n",
			wantStdout: "let\nvar\n",
			wantLLVMFragments: []string{
				"call %kizu.error.slice.u8 @kizu_rt_array_at",
				" = extractvalue %kizu.slice.u8 %am",
				"store %kizu.slice.u8 %v",
			},
		},
	}
}

// runRenderCleanupCases covers SPEC cleanup stack behavior across error exits:
// normal defer runs on error paths, errdefer is error-only, and both share LIFO.
func runRenderCleanupCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "defer_try_error_deinits_array_i64",
			source: "fn failer() -> !void {\n" +
				"    return error(\"boom\");\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    let values = std::array::Array<i64>(allocator);\n" +
				"    defer values.deinit();\n" +
				"    try failer();\n" +
				"    print(\"unreachable\");\n" +
				"    return;\n}\n",
			wantExit: 1,
			wantLLVMFragments: []string{
				"_fail:\n  call void @kizu_rt_array_deinit(%kizu.owned %v",
			},
		},
		{
			name: "defer_errdefer_return_error_lifo_order",
			source: "fn fail_mixed(allocator: Allocator) -> !void {\n" +
				"    let first = std::array::Array<i64>(allocator);\n" +
				"    defer first.deinit();\n" +
				"    let second = std::array::Array<i64>(allocator);\n" +
				"    errdefer second.deinit();\n" +
				"    let third = std::array::Array<i64>(allocator);\n" +
				"    defer third.deinit();\n" +
				"    return error(\"boom\");\n}\n\n" +
				"fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    try fail_mixed(allocator);\n" +
				"    print(\"unreachable\");\n" +
				"    return;\n}\n",
			wantExit: 1,
			wantLLVMOrderedFragments: []string{
				"define %kizu.error.void @kizu_run_user_fail_mixed",
				"call void @kizu_rt_array_deinit(%kizu.owned %v3)",
				"call void @kizu_rt_array_deinit(%kizu.owned %v2)",
				"call void @kizu_rt_array_deinit(%kizu.owned %v1)",
				"ret %kizu.error.void { i1 false",
			},
		},
	}
}

const runRenderCleanupMarkerPrelude = "struct Marker {\n" +
	"    label: []u8,\n" +
	"}\n\n" +
	"impl Marker {\n" +
	"    fn deinit(self: Marker) -> void {\n" +
	"        print(self.label);\n" +
	"    }\n" +
	"}\n\n"

// runRenderCleanupControlFlowCases covers normal defer cleanup on non-local
// control-flow exits: return, break, continue, and labeled break.
func runRenderCleanupControlFlowCases() []runRenderCase {
	cases := append(runRenderCleanupLoopExitCases(), runRenderCleanupBranchExitCases()...)
	return cases
}

// runRenderCleanupLoopExitCases covers break/continue paths inside a loop body.
func runRenderCleanupLoopExitCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "defer_runs_before_break",
			source: runRenderCleanupMarkerPrelude +
				"fn main() {\n" +
				"    while true {\n" +
				"        let marker = Marker { label: \"cleanup\" };\n" +
				"        defer marker.deinit();\n" +
				"        print(\"before\");\n" +
				"        break;\n" +
				"    }\n" +
				"    print(\"after\");\n" +
				"}\n",
			wantStdout: "before\ncleanup\nafter\n",
		},
		{
			name: "defer_runs_before_continue",
			source: runRenderCleanupMarkerPrelude +
				"fn main() {\n" +
				"    var i = 0;\n" +
				"    while i < 2 {\n" +
				"        i = i + 1;\n" +
				"        let marker = Marker { label: \"cleanup\" };\n" +
				"        defer marker.deinit();\n" +
				"        print(i);\n" +
				"        continue;\n" +
				"    }\n" +
				"    print(\"done\");\n" +
				"}\n",
			wantStdout: "1\ncleanup\n2\ncleanup\ndone\n",
		},
	}
}

// runRenderCleanupBranchExitCases covers branch exits that must preserve or run defers.
func runRenderCleanupBranchExitCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "defer_after_non_taken_return_branch",
			source: runRenderCleanupMarkerPrelude +
				"fn maybe(flag: bool) {\n" +
				"    let marker = Marker { label: \"cleanup\" };\n" +
				"    defer marker.deinit();\n" +
				"    if flag {\n" +
				"        return;\n" +
				"    }\n" +
				"    print(\"body\");\n" +
				"}\n\n" +
				"fn main() {\n" +
				"    maybe(false);\n" +
				"}\n",
			wantStdout: "body\ncleanup\n",
		},
		{
			name: "outer_defer_not_run_before_break",
			source: runRenderCleanupMarkerPrelude +
				"fn main() {\n" +
				"    let outer = Marker { label: \"outer\" };\n" +
				"    defer outer.deinit();\n" +
				"    while true {\n" +
				"        let inner = Marker { label: \"inner\" };\n" +
				"        defer inner.deinit();\n" +
				"        print(\"before\");\n" +
				"        break;\n" +
				"    }\n" +
				"    print(\"after\");\n" +
				"}\n",
			wantStdout: "before\ninner\nafter\nouter\n",
		},
		{
			name: "labeled_break_runs_exited_defers_lifo",
			source: runRenderCleanupMarkerPrelude +
				"fn main() {\n" +
				"    outer_loop: while true {\n" +
				"        let outer = Marker { label: \"outer\" };\n" +
				"        defer outer.deinit();\n" +
				"        while true {\n" +
				"            let inner = Marker { label: \"inner\" };\n" +
				"            defer inner.deinit();\n" +
				"            print(\"before\");\n" +
				"            break :outer_loop;\n" +
				"        }\n" +
				"    }\n" +
				"    print(\"after\");\n" +
				"}\n",
			wantStdout: "before\ninner\nouter\nafter\n",
		},
	}
}

// runRenderPathJoinCleanCases covers owned std::path::join/clean lowering.
func runRenderPathJoinCleanCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "std_path_join_clean",
			source: "fn main() -> !void {\n" +
				"    let allocator = std::mem::page_allocator();\n" +
				"    var source = try std::path::join(allocator, \"examples\", \"fixtures/config.txt\");\n" +
				"    let source_bytes = source.as_bytes();\n" +
				"    print(source_bytes);\n" +
				"    var absolute = try std::path::join(allocator, \"/a\", \"../b/\");\n" +
				"    let absolute_bytes = absolute.as_bytes();\n" +
				"    print(absolute_bytes);\n" +
				"    var cleaned = try std::path::clean(allocator, \"a//./b/../c/\");\n" +
				"    let cleaned_bytes = cleaned.as_bytes();\n" +
				"    print(cleaned_bytes);\n" +
				"    var parent = try std::path::clean(allocator, \"../a/..\");\n" +
				"    let parent_bytes = parent.as_bytes();\n" +
				"    print(parent_bytes);\n" +
				"    source.deinit();\n" +
				"    absolute.deinit();\n" +
				"    cleaned.deinit();\n" +
				"    parent.deinit();\n" +
				"    return;\n}\n",
			wantStdout: "examples/fixtures/config.txt\n/b\na/c\n..\n",
			wantLLVMFragments: []string{
				"call %kizu.error.owned @kizu_run_path_join",
				"call %kizu.error.owned @kizu_run_path_clean",
			},
		},
	}
}

// runRenderStructCases is the struct-scalarization corpus (let-bound struct
// literals, scalar and slice field reads, nested struct fields, and a field
// read as a user-call argument).
func runRenderStructCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "struct_field_reads",
			source: "struct User {\n    name: []u8,\n    age: i64,\n}\n\n" +
				"fn main() {\n    let user = User {\n        name: \"alice\",\n        age: 30,\n    };\n\n" +
				"    print(user.name);\n    print(user.age);\n}\n",
			wantStdout: "alice\n30\n",
		},
		{
			name: "nested_struct_field_call_arg",
			source: "struct Inner {\n    value: i64,\n}\n\n" +
				"struct Outer {\n    inner: Inner,\n    label: []u8,\n}\n\n" +
				"fn double(n: i64) -> i64 {\n    return n * 2;\n}\n\n" +
				"fn main() {\n    let outer = Outer {\n        inner: Inner { value: 21 },\n" +
				"        label: \"deep\",\n    };\n" +
				"    print(double(outer.inner.value));\n    print(outer.label);\n}\n",
			wantStdout: "42\ndeep\n",
		},
	}
}

// runRenderEnumMatchCases is the enum corpus (qualified variant prints, tag
// equality, match dispatch over variants, and a wildcard arm).
func runRenderEnumMatchCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "enum_print_and_equality",
			source: "enum Color {\n    Red,\n    Green,\n    Blue,\n}\n\n" +
				"fn main() {\n    let color = Color::Green;\n    print(color);\n" +
				"    print(color == Color::Green);\n    print(color == Color::Red);\n}\n",
			wantStdout: "Color::Green\ntrue\nfalse\n",
		},
		{
			name: "enum_match_dispatch",
			source: "enum Color {\n    Red,\n    Green,\n    Blue,\n}\n\n" +
				"fn main() {\n    let color = Color::Blue;\n    match color {\n" +
				"        Red => print(\"red\"),\n        Green => print(\"green\"),\n" +
				"        Blue => print(\"blue\"),\n    }\n    print(\"done\");\n}\n",
			wantStdout: "blue\ndone\n",
		},
		{
			name: "enum_match_wildcard",
			source: "enum Mode {\n    Fast,\n    Slow,\n}\n\n" +
				"fn main() {\n    let mode = Mode::Slow;\n    match mode {\n" +
				"        Fast => print(\"fast\"),\n        _ => print(\"other\"),\n    }\n}\n",
			wantStdout: "other\n",
		},
	}
}

// runRenderVarStructCases is the var-struct corpus (scalarized field slots:
// string and i64 field assignment, and a field read feeding its own update).
func runRenderVarStructCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "var_struct_field_assignment",
			source: "struct User {\n    name: []u8,\n    age: i64,\n}\n\n" +
				"fn main() -> void {\n    var user = User { name: \"alice\", age: 30 };\n" +
				"    user.name = \"bob\";\n    user.age = user.age + 1;\n" +
				"    print(user.name);\n    print(user.age);\n}\n",
			wantStdout: "bob\n31\n",
		},
	}
}

// runRenderUnionPayloadCases is the union corpus (payload constructors, payload
// bindings in match arms incl. a slice payload, and a payload-less variant).
func runRenderUnionPayloadCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "union_payload_match",
			source: "union Shape {\n    Point,\n    Circle(i64),\n    Label([]u8),\n}\n\n" +
				"fn main() {\n    let circle = Shape::Circle(10);\n    match circle {\n" +
				"        Point => print(\"point\"),\n        Circle(radius) => print(radius),\n" +
				"        Label(text) => print(text),\n    }\n" +
				"    let label = Shape::Label(\"name\");\n    match label {\n" +
				"        Point => print(\"point\"),\n        Circle(radius) => print(radius),\n" +
				"        Label(text) => print(text),\n    }\n" +
				"    let point = Shape::Point;\n    match point {\n" +
				"        Point => print(\"point\"),\n        _ => print(\"other\"),\n    }\n}\n",
			wantStdout: "10\nname\npoint\n",
		},
	}
}

// runRenderUnionAbiCases is the runtime union ABI corpus: a union value
// crossing a function boundary (the examples/union.kizu shape) and a runtime
// match on the union-typed parameter with i64/slice payload bindings.
func runRenderUnionAbiCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "union_return_pick",
			source: "union Shape {\n    Point,\n    Circle(i64),\n    Label([]u8),\n}\n\n" +
				"fn pick(kind: i64) -> Shape {\n    if kind == 1 {\n" +
				"        return Shape::Circle(7);\n    }\n    if kind == 2 {\n" +
				"        return Shape::Label(\"ring\");\n    }\n    return Shape::Point;\n}\n\n" +
				"fn describe(shape: &Shape) -> void {\n    match shape {\n" +
				"        Point => print(\"point\"),\n        Circle(radius) => print(radius),\n" +
				"        Label(text) => print(text),\n    }\n}\n\n" +
				"fn main() {\n    let circle = pick(1);\n    let label = pick(2);\n" +
				"    describe(circle);\n    describe(label);\n    match pick(0) {\n" +
				"        Point => print(\"point\"),\n        _ => print(\"other\"),\n    }\n}\n",
			wantStdout: "7\nring\npoint\n",
		},
		{
			name: "union_param_describe",
			source: "union Shape {\n    Point,\n    Circle(i64),\n    Label([]u8),\n}\n\n" +
				"fn describe(shape: &Shape) -> void {\n    match shape {\n" +
				"        Point => print(\"point\"),\n        Circle(radius) => print(radius),\n" +
				"        Label(text) => print(text),\n    }\n}\n\n" +
				"fn main() {\n    let circle = Shape::Circle(10);\n" +
				"    let label = Shape::Label(\"name\");\n    describe(circle);\n" +
				"    describe(label);\n    describe(Shape::Point);\n}\n",
			wantStdout: "10\nname\npoint\n",
		},
	}
}

// runRenderValueExprCases is the value-position if/match corpus (bool, string,
// and i64 results; a wildcard arm; a union payload binding in an arm value).
func runRenderValueExprCases() []runRenderCase {
	return []runRenderCase{
		{
			name: "value_if_expressions",
			source: "fn main() {\n    let age = 20;\n    let adult = if age >= 18 {\n" +
				"        true\n    } else {\n        false\n    };\n    print(adult);\n" +
				"    let label = if age >= 20 {\n        \"adult\"\n    } else {\n" +
				"        \"minor\"\n    };\n    print(label);\n" +
				"    let bonus = if age == 20 {\n        let base = 5;\n        base * 2\n" +
				"    } else {\n        0\n    };\n    print(bonus);\n}\n",
			wantStdout: "true\nadult\n10\n",
		},
		{
			name: "value_match_expressions",
			source: "enum Color {\n    Red,\n    Green,\n    Blue,\n}\n\n" +
				"fn main() {\n    let color = Color::Blue;\n    let name = match color {\n" +
				"        Red => \"red\",\n        _ => \"other\",\n    };\n    print(name);\n" +
				"    let rank = match color {\n        Red => 1,\n        Green => 2,\n" +
				"        Blue => 3,\n    };\n" +
				"    print(rank);\n}\n",
			wantStdout: "other\n3\n",
		},
		{
			name: "value_match_union_payload",
			source: "union Shape {\n    Point,\n    Circle(i64),\n}\n\n" +
				"fn main() {\n    let circle = Shape::Circle(7);\n    let area = match circle {\n" +
				"        Point => 0,\n        Circle(radius) => radius * radius,\n    };\n" +
				"    print(area);\n}\n",
			wantStdout: "49\n",
		},
	}
}

// runOneRenderCase writes the source, renders the tape module via the interpreter,
// links it, runs it, and asserts the output.
func runOneRenderCase(t *testing.T, program *ast.Program, clang string, item runRenderCase) {
	t.Helper()
	if err := os.WriteFile(
		"target/selfhost/run_render_input.kizu",
		[]byte(item.source),
		0o644,
	); err != nil {
		t.Fatalf("write render input: %v", err)
	}
	var out bytes.Buffer
	const entry = "selfhost::backend::run_render_gate::run_render_gate"
	if err := interp.New(&out).RunEntry(program, entry); err != nil {
		t.Fatalf("render gate failed: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("run-render-ok")) {
		t.Fatalf("render gate did not report success:\n%s", out.String())
	}
	llPath := "target/selfhost/run_render.ll"
	llText, err := os.ReadFile(llPath)
	if err != nil {
		t.Fatalf("read rendered module: %v", err)
	}
	assertRunRenderLLVM(t, llText, item)
	exePath := filepath.Join("target/selfhost", item.name+".render")
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		llPath,
		"selfhost/runtime/selfhost.storage.ll",
		"selfhost/runtime/selfhost.host.ll",
		"selfhost/runtime/selfhost.hosted.c",
		"-o",
		exePath,
	)
	if linkOut, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("link rendered module: %v\n%s\n--- module ---\n%s", err, linkOut, llText)
	}
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Fatalf("resolve exe: %v", err)
	}
	run := exec.Command(absExe)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	runErr := run.Run()
	if exitCode(runErr) != item.wantExit {
		t.Fatalf(
			"rendered program exit=%d want %d stderr=%q",
			exitCode(runErr),
			item.wantExit,
			stderr.String(),
		)
	}
	if stdout.String() != item.wantStdout {
		t.Fatalf("rendered program stdout=%q want %q", stdout.String(), item.wantStdout)
	}
}

// assertRunRenderLLVM checks unordered and ordered LLVM fragments requested by a
// render case before the module is linked and executed.
func assertRunRenderLLVM(t *testing.T, llText []byte, item runRenderCase) {
	t.Helper()
	for _, fragment := range item.wantLLVMFragments {
		if !bytes.Contains(llText, []byte(fragment)) {
			t.Fatalf("rendered module missing fragment %q\n--- module ---\n%s", fragment, llText)
		}
	}
	if len(item.wantLLVMOrderedFragments) > 0 {
		offset := 0
		for _, fragment := range item.wantLLVMOrderedFragments {
			found := bytes.Index(llText[offset:], []byte(fragment))
			if found < 0 {
				t.Fatalf(
					"rendered module missing ordered fragment %q after byte %d\n--- module ---\n%s",
					fragment,
					offset,
					llText,
				)
			}
			offset += found + len(fragment)
		}
	}
}
