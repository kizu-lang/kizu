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
	name       string
	source     string
	wantStdout string
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
	for _, item := range runRenderCases() {
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
	exePath := filepath.Join("target/selfhost", item.name+".render")
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		"-fno-integrated-as",
		llPath,
		"selfhost/runtime/selfhost.host.ll",
		"selfhost/runtime/selfhost.hosted.c",
		"-o",
		exePath,
	)
	if linkOut, err := compile.CombinedOutput(); err != nil {
		llText, _ := os.ReadFile(llPath)
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
	if exitCode(runErr) != 0 {
		t.Fatalf("rendered program exit=%d stderr=%q", exitCode(runErr), stderr.String())
	}
	if stdout.String() != item.wantStdout {
		t.Fatalf("rendered program stdout=%q want %q", stdout.String(), item.wantStdout)
	}
}
