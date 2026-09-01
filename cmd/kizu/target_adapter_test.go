package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const targetAdapterOutput = "16\n1517\n"

// TestTargetSelectedAdapters runs one package through native filesystem I/O,
// WASI filesystem I/O, and an explicit browser host input. Every target hands
// the bytes to the same portable core and must produce the same result.
func TestTargetSelectedAdapters(t *testing.T) {
	packagePath := "examples/modules/target_adapters"
	runTargetAdapterOptimizations(t, "native", func(t *testing.T, opt bool) {
		runNativeTargetAdapter(t, packagePath, opt)
	})

	wasmtime, wasmtimeErr := exec.LookPath("wasmtime")
	if wasmtimeErr == nil {
		runTargetAdapterOptimizations(t, "wasi", func(t *testing.T, opt bool) {
			runWASITargetAdapter(t, wasmtime, packagePath, opt)
		})
	} else {
		t.Log("wasmtime is not installed; skipping WASI adapter execution")
	}

	node, nodeErr := exec.LookPath("node")
	if nodeErr == nil {
		runTargetAdapterOptimizations(t, "browser", func(t *testing.T, opt bool) {
			runBrowserTargetAdapter(t, node, packagePath, opt)
		})
	} else {
		t.Log("node is not installed; skipping browser adapter execution")
	}
}

// runTargetAdapterOptimizations checks default and optimized builds for one target.
func runTargetAdapterOptimizations(
	t *testing.T,
	target string,
	run func(*testing.T, bool),
) {
	t.Helper()
	for _, opt := range []bool{false, true} {
		name := "default"
		if opt {
			name = "optimized"
		}
		t.Run(target+"/"+name, func(t *testing.T) { run(t, opt) })
	}
}

// runNativeTargetAdapter builds and executes the filesystem adapter.
func runNativeTargetAdapter(t *testing.T, packagePath string, opt bool) {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), "target-adapters")
	args := []string{"build", "--target", "native"}
	if opt {
		args = append(args, "--opt")
	}
	args = append(args, "-o", artifact, packagePath)
	build := kizuCommand(args...)
	build.Dir = repoRoot
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("native adapter build failed: %v\n%s", err, output)
	}
	if got, want := string(output), artifact+"\n"; got != want {
		t.Fatalf("build output = %q, want %q", got, want)
	}
	run := exec.Command(artifact)
	run.Dir = repoRoot
	output, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("native adapter failed: %v\n%s", err, output)
	}
	expectTargetAdapterOutput(t, output)
}

// buildTargetAdapterWASM builds one WebAssembly target and returns its artifact.
func buildTargetAdapterWASM(t *testing.T, target string, packagePath string, opt bool) string {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), "target-adapters.wasm")
	args := []string{"build", "--target", target}
	if opt {
		args = append(args, "--opt")
	}
	args = append(args, "--emit", "wasm", "-o", artifact, packagePath)
	build := kizuCommand(args...)
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%s adapter build failed: %v\n%s", target, err, output)
	} else if len(output) != 0 {
		t.Fatalf("%s adapter build wrote to terminal: %q", target, output)
	}
	return artifact
}

// runWASITargetAdapter executes the filesystem adapter under Wasmtime.
func runWASITargetAdapter(t *testing.T, wasmtime string, packagePath string, opt bool) {
	t.Helper()
	artifact := buildTargetAdapterWASM(t, "wasm32-wasi", packagePath, opt)
	run := exec.Command(wasmtime, "run", "--dir", ".", artifact)
	run.Dir = repoRoot
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("WASI adapter failed: %v\n%s", err, output)
	}
	expectTargetAdapterOutput(t, output)
}

// runBrowserTargetAdapter executes the explicit host-input adapter under Node.
func runBrowserTargetAdapter(t *testing.T, node string, packagePath string, opt bool) {
	t.Helper()
	artifact := buildTargetAdapterWASM(t, "wasm32-browser", packagePath, opt)
	run := exec.Command(node, "scripts/run-browser-target-adapters.mjs", artifact)
	run.Dir = repoRoot
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("browser adapter failed: %v\n%s", err, output)
	}
	expectTargetAdapterOutput(t, output)
}

// expectTargetAdapterOutput checks the portable core's observable result.
func expectTargetAdapterOutput(t *testing.T, output []byte) {
	t.Helper()
	if got := string(output); got != targetAdapterOutput {
		t.Fatalf("got %q, want %q", got, targetAdapterOutput)
	}
}
