package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuildTargetBrowserRunsPortablePrograms crosses the JavaScript host ABI
// with representative calls, aggregates, allocation, and checked errors.
func TestBuildTargetBrowserRunsPortablePrograms(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for browser host adapter execution")
	}
	cases := []struct {
		file string
		want string
	}{
		{"functions.kizu", "3\n"},
		{"aggregate_calls.kizu", "4\n3\n"},
		{"arena.kizu", "alice\n"},
		{"error_union_try.kizu", "1\n2\n"},
		{"modules/compiler_phases", "7\n"},
	}
	for _, test := range cases {
		t.Run(test.file, func(t *testing.T) {
			artifact := filepath.Join(t.TempDir(), "program.wasm")
			build := kizuCommand(
				"build", "--target", "wasm32-browser", "--emit", "wasm",
				"-o", artifact, filepath.Join("../../examples", test.file),
			)
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("browser build failed: %v\n%s", err, output)
			} else if len(output) != 0 {
				t.Fatalf("browser build wrote to terminal: %q", output)
			}
			run := exec.Command(node, "../../scripts/run-browser-wasm.mjs", artifact)
			output, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("browser host adapter failed: %v\n%s", err, output)
			}
			if got := string(output); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

// TestBuildTargetBrowserReturnsMainFailure keeps a checked Kizu error as an
// explicit host status while its diagnostic crosses the stderr stream.
func TestBuildTargetBrowserReturnsMainFailure(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for browser host adapter execution")
	}
	artifact := filepath.Join(t.TempDir(), "program.wasm")
	build := kizuCommand(
		"build", "--target", "wasm32-browser", "--emit", "wasm", "-o", artifact,
		"../../examples/error_set_undeclared.kizu",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("browser build failed: %v\n%s", err, output)
	}
	run := exec.Command(node, "../../scripts/run-browser-wasm.mjs", artifact)
	output, err := run.CombinedOutput()
	if err == nil {
		t.Fatalf("failed main returned success:\n%s", output)
	}
	if exit := run.ProcessState.ExitCode(); exit != 1 {
		t.Fatalf("got status %d, want 1: %s", exit, output)
	}
	if got, want := string(output), "runtime error: NetError::Timeout\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildTargetBrowserMapsExitStatus checks the non-process browser entry
// returns the same portable status mapping used by hosted targets.
func TestBuildTargetBrowserMapsExitStatus(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for browser host adapter execution")
	}
	cases := []struct {
		name   string
		status string
		want   int
	}{
		{"success", "Success", 0},
		{"failure", "Failure", 1},
		{"specific", "Specific(7)", 7},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "status.kizu")
			program := "import std::process;\n\n" +
				"fn main() -> !process::ExitStatus {\n" +
				"    return process::ExitStatus::" + test.status + ";\n" +
				"}\n"
			if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(directory, "status.wasm")
			build := kizuCommand(
				"build", "--target", "wasm32-browser", "--emit", "wasm",
				"-o", artifact, source,
			)
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("browser build failed: %v\n%s", err, output)
			}
			run := exec.Command(node, "../../scripts/run-browser-wasm.mjs", artifact)
			output, runErr := run.CombinedOutput()
			got := 0
			if runErr != nil {
				got = run.ProcessState.ExitCode()
			}
			if got != test.want || len(output) != 0 {
				t.Fatalf("got status %d output %q, want status %d", got, output, test.want)
			}
		})
	}
}

// TestBuildTargetBrowserRunsExplicitHostInterface crosses source-declared host
// imports, bounded guest-memory copy, a later JavaScript callback, and narrow
// integer adaptation through the public adapter rather than inspecting WAT.
func TestBuildTargetBrowserRunsExplicitHostInterface(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for browser host adapter execution")
	}
	artifact := filepath.Join(t.TempDir(), "host-interface.wasm")
	build := kizuCommand(
		"build", "--target", "wasm32-browser", "--emit", "wasm",
		"-o", artifact, "../../tests/browser/host_interface.kizu",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("browser host interface build failed: %v\n%s", err, output)
	} else if len(output) != 0 {
		t.Fatalf("browser host interface build wrote to terminal: %q", output)
	}
	run := exec.Command(node, "../../scripts/run-browser-host.mjs", artifact)
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("explicit browser host interface failed: %v\n%s", err, output)
	}
	if got, want := string(output),
		"Kizu browser host ready\nBROWSER CALLBACK\n-1/255\nbool=1\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildTargetBrowserRejectsMissingCapabilities checks browser absence at
// build time instead of producing a module whose imports cannot be attached.
func TestBuildTargetBrowserRejectsMissingCapabilities(t *testing.T) {
	stdin := filepath.Join(t.TempDir(), "stdin.kizu")
	stdinSource := `import std::io;
import std::mem;

fn main() -> !void {
    let handle = io::blocking();
    let allocator = mem::page_allocator();
    var text = try io::read_stdin(handle, allocator, mem::Limit::Bytes(16));
    defer text.deinit(allocator);
    return;
}
`
	if err := os.WriteFile(stdin, []byte(stdinSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		file string
		api  string
	}{
		{"filesystem", "../../examples/fs_read.kizu", "std::fs"},
		{"process", "../../examples/std_process.kizu", "std::process::arg_count"},
		{"stdin", stdin, "std::io::read_stdin"},
		{"evented-io", "../../examples/io_evented.kizu", "evented std::io"},
		{"coroutine", "../../examples/coro_suspend.kizu", "std::coro"},
		{"socket", "../../examples/net_round_trip.kizu", "std::net"},
		{"extern-c", "../../examples/user_allocator.kizu", "extern C"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			command := kizuCommand("build", "--target", "wasm32-browser", test.file)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("command succeeded:\n%s", output)
			}
			want := "error: wasm error: target wasm32-browser does not support " + test.api + "\n"
			if got := string(output); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}
