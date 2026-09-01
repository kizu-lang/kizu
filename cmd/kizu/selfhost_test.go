package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/cimport"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	kizufmt "github.com/kizu-lang/kizu/internal/fmt"
	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// TestSelfhostFrontend checks the user-facing compiler boundaries not expressed
// by the language conformance cases. Detailed parser, checker, IR and LLVM
// behavior belongs to their shared corpora; this test keeps one file or package
// crossing each CLI path and compares it byte for byte with the shipping path.
func TestSelfhostFrontend(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the selfhost compiler")
	}
	selfhost := sharedSelfhost(t)
	file := "../../examples/hello.kizu"
	pkg := "../../tests/behavior"
	// A program the checkers accept and the backend does not, so the case
	// fails only if `check` reaches the backend on both sides.
	backendReject := "../../examples/negative/union_recursive_payload.kizu"
	commands := []struct {
		name string
		want cliOutput
		args []string
	}{
		{"parse-file", goParseOutput(file), []string{"parse", file}},
		{"check-package", goCheckOutput(pkg), []string{"check", pkg}},
		{"check-backend-reject", goCheckOutput(backendReject),
			[]string{"check", backendReject}},
		{"ir-package", goIrOutput(pkg, false), []string{"ir", pkg}},
		{"ir-opt-package", goIrOutput(pkg, true), []string{"ir", "--opt", pkg}},
		{"llvm-package", goEmitLLVMOutput(pkg, false), []string{"build", "--emit-llvm", pkg}},
		{"llvm-opt-package", goEmitLLVMOutput(pkg, true), []string{"build", "--emit-llvm", "--opt", pkg}},
		{"wasm-file", goWASMOutput(file, false),
			[]string{"build", "--target", "wasm32-wasi", file}},
		{"wasm-browser-file", goBrowserWASMOutput(file, false),
			[]string{"build", "--target", "wasm32-browser", file}},
		{"wasm-package-manifest", goWASMOutput(pkg+"/kizu.toml", false),
			[]string{"build", "--target", "wasm32-wasi", pkg + "/kizu.toml"}},
		{"wasm-browser-package", goBrowserWASMOutput(pkg, false),
			[]string{"build", "--target", "wasm32-browser", pkg}},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			compareSelfhostArgs(t, selfhost, command.want, command.args...)
		})
	}
	runSelfhostFmtCases(t, selfhost)
	runSelfhostWASMCases(t, selfhost)
	runSelfhostArgumentCases(t, selfhost)
	t.Run("version", func(t *testing.T) {
		compareSelfhostVersion(t, selfhost)
	})
	t.Run("installed-tree", func(t *testing.T) {
		compareSelfhostInstalledTree(t, selfhost, file)
	})
	for _, header := range cimportRepresentativeHeaders() {
		t.Run("import-c-header/"+header.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), header.name+".h")
			writeTestFile(t, path, []byte(header.source))
			compareSelfhostArgs(t, selfhost,
				goImportCHeaderOutput(path), "import-c-header", path)
		})
	}
	t.Run("init", func(t *testing.T) {
		compareSelfhostInit(t, selfhost)
	})
	t.Run("build-native", func(t *testing.T) {
		compareNativeBuild(t, selfhost, file, false)
	})
	t.Run("build-native-opt", func(t *testing.T) {
		compareNativeBuild(t, selfhost, file, true)
	})
}

// runSelfhostFmtCases compares the formatter across the CLI boundary: each
// layout fixture, a source that does not parse, and both spellings of the
// in-place flag.
func runSelfhostFmtCases(t *testing.T, selfhost string) {
	t.Helper()
	for _, fixture := range fmtRepresentativeFixtures() {
		t.Run("fmt/"+fixture.name, func(t *testing.T) {
			path := writeTempKizuSource(t, fixture.name+".kizu", fixture.source)
			compareSelfhostFmt(t, selfhost, path)
		})
	}
	t.Run("fmt/parse-error", func(t *testing.T) {
		compareSelfhostFmt(t, selfhost, "../../examples/negative/unclosed_block.kizu")
	})
	t.Run("fmt-write", func(t *testing.T) {
		compareSelfhostFmtWrite(t, selfhost, "--write", fmtMoveMarkerFixture())
	})
	t.Run("fmt-w", func(t *testing.T) {
		compareSelfhostFmtWrite(t, selfhost, "-w", "fn main(){return;}\n")
	})
}

// cliOutput is what a CLI command wrote and how it ended.
type cliOutput struct {
	stdout string
	stderr string
	failed bool
}

// compareSelfhostArgs runs the selfhost compiler with args and compares what
// it prints with the Go output.
func compareSelfhostArgs(t *testing.T, selfhost string, want cliOutput, args ...string) {
	t.Helper()
	cmd := exec.Command(selfhost, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	got := cliOutput{
		stdout: stdout.String(),
		stderr: stderr.String(),
		failed: runErr != nil,
	}
	if got != want {
		t.Errorf("selfhost %s differs\n--- want (failed=%v)\nstdout:\n%sstderr:\n%s"+
			"--- got (failed=%v)\nstdout:\n%sstderr:\n%s",
			strings.Join(args, " "), want.failed, want.stdout, want.stderr,
			got.failed, got.stdout, got.stderr)
	}
}

// compareSelfhostFmt compares the selfhost command with the same Go stages the
// shipping fmt command calls: parse diagnostics, move markers and formatting.
func compareSelfhostFmt(t *testing.T, selfhost string, target string) {
	t.Helper()
	want := goFmtOutput(target)
	got := runNativeCLI(t, selfhost, "fmt", target)
	if got != want {
		t.Errorf("selfhost fmt %s differs\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			target, want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
	}
}

// goFmtOutput renders the shipping fmt path without starting one Go process
// per corpus file. The existing frontend comparisons use the same pattern.
func goFmtOutput(file string) nativeCLIResult {
	source, err := os.ReadFile(file)
	if err != nil {
		return failedCLIResult(err)
	}
	if _, diagnostics, err := parsePath(file); err != nil {
		return failedCLIResult(err)
	} else if len(diagnostics) > 0 {
		var stderr strings.Builder
		for _, diagnostic := range diagnostics {
			stderr.WriteString(diagnostic.CLIError())
			stderr.WriteByte('\n')
		}
		stderr.WriteString("error: parse failed\n")
		return nativeCLIResult{
			output: cliOutput{stderr: stderr.String(), failed: true},
			code:   1,
		}
	}
	marked, err := insertMoveMarkers(file, string(source))
	if err != nil {
		return failedCLIResult(err)
	}
	return nativeCLIResult{output: cliOutput{stdout: kizufmt.Format(marked)}}
}

// runSelfhostWASMCases compares the wasm target on one program per backend
// behavior and on each distinct argument error it reports.
func runSelfhostWASMCases(t *testing.T, selfhost string) {
	t.Helper()
	packagePath := "../../examples/modules/compiler_phases"
	t.Run("wasm/package", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(packagePath, false),
			"build", "--target", "wasm32-wasi", packagePath)
		compareSelfhostArgs(t, selfhost, goWASMOutput(packagePath, true),
			"build", "--target", "wasm32-wasi", "--opt", packagePath)
		compareSelfhostWASMBinary(t, selfhost, packagePath)
	})
	runSelfhostTargetAdapterWASICase(t, selfhost)
	// A loop in a called function repeats the block names of the caller's loop,
	// so this crosses phi copies that have to stay inside one function.
	loops := "../../examples/loop_in_called_function.kizu"
	t.Run("wasm/cross_function_loops", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(loops, false),
			"build", "--target", "wasm32-wasi", loops)
	})
	for _, example := range selfhostWASMExamples() {
		name := strings.TrimSuffix(filepath.Base(example), ".kizu")
		t.Run("wasm/"+name, func(t *testing.T) {
			compareSelfhostArgs(t, selfhost, goWASMOutput(example, false),
				"build", "--target", "wasm32-wasi", example)
		})
	}
	tagged := "../../examples/optional_error_flow.kizu"
	t.Run("wasm/optional_error_flow_opt", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(tagged, true),
			"build", "--target", "wasm32-wasi", "--opt", tagged)
	})
	arena := "../../examples/arena_at_mut.kizu"
	t.Run("wasm/arena_at_mut_opt", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(arena, true),
			"build", "--target", "wasm32-wasi", "--opt", arena)
	})
	joinTrim := "../../examples/std_string_join_trim.kizu"
	t.Run("wasm/std_string_join_trim_opt", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(joinTrim, true),
			"build", "--target", "wasm32-wasi", "--opt", joinTrim)
	})
	runSelfhostWASMContainerOptCases(t, selfhost)
	for _, example := range []string{"hello.kizu", "function_pointer.kizu", "map_keys.kizu"} {
		t.Run("wasm-binary/"+example, func(t *testing.T) {
			compareSelfhostWASMBinary(t, selfhost, "../../examples/"+example)
		})
	}
	for _, fixture := range wasmRepresentativeFixtures() {
		t.Run("wasm/"+fixture.name, func(t *testing.T) {
			path := writeTempKizuSource(t, fixture.name+".kizu", fixture.source)
			args := []string{"build", "--target", "wasm32-wasi"}
			if fixture.opt {
				args = append(args, "--opt")
			}
			args = append(args, path)
			compareSelfhostArgs(t, selfhost, goWASMOutput(path, fixture.opt), args...)
		})
	}
	for _, example := range []string{
		"io_evented.kizu",
		"coro_suspend.kizu",
		"net_round_trip.kizu",
	} {
		t.Run("wasm-reject-host/"+example, func(t *testing.T) {
			path := "../../examples/" + example
			compareSelfhostArgs(t, selfhost, goWASMOutput(path, false),
				"build", "--target", "wasm32-wasi", path)
		})
	}
	runSelfhostBrowserWASMCases(t, selfhost)
}

// runSelfhostTargetAdapterWASICase compares target selection in WAT and binary output.
func runSelfhostTargetAdapterWASICase(t *testing.T, selfhost string) {
	t.Helper()
	path := "../../examples/modules/target_adapters"
	t.Run("wasm/target-adapters", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(path, false),
			"build", "--target", "wasm32-wasi", path)
		compareSelfhostArgs(t, selfhost, goWASMOutput(path, true),
			"build", "--target", "wasm32-wasi", "--opt", path)
		compareSelfhostWASMBinary(t, selfhost, path)
	})
}

// runSelfhostBrowserWASMCases compares the browser target's portable output,
// target refusals, direct binary renderer, and ExitStatus boundary.
func runSelfhostBrowserWASMCases(t *testing.T, selfhost string) {
	t.Helper()
	packagePath := "../../examples/modules/compiler_phases"
	t.Run("wasm-browser/package", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(packagePath, false),
			"build", "--target", "wasm32-browser", packagePath)
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(packagePath, true),
			"build", "--target", "wasm32-browser", "--opt", packagePath)
		compareSelfhostWASMBinaryTarget(t, selfhost, "wasm32-browser", packagePath)
		compareSelfhostBrowserESM(t, selfhost, packagePath, false)
		compareSelfhostBrowserESM(t, selfhost, packagePath, true)
	})
	t.Run("wasm-browser/esm-missing-runtime", func(t *testing.T) {
		compareSelfhostBrowserESMMissingRuntime(t, selfhost)
	})
	runSelfhostTargetAdapterBrowserCase(t, selfhost)
	t.Run("wasm-browser/explicit-host-interface", func(t *testing.T) {
		path := "../../tests/browser/host_interface.kizu"
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, false),
			"build", "--target", "wasm32-browser", path)
		compareSelfhostWASMBinaryTarget(t, selfhost, "wasm32-browser", path)
	})
	for _, example := range []string{
		"hello.kizu",
		"aggregate_calls.kizu",
		"arena.kizu",
		"error_union_try.kizu",
		"std_io_stderr.kizu",
	} {
		t.Run("wasm-browser/"+example, func(t *testing.T) {
			path := "../../examples/" + example
			compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, false),
				"build", "--target", "wasm32-browser", path)
		})
	}
	for _, example := range []string{
		"fs_read.kizu",
		"std_process.kizu",
		"net_round_trip.kizu",
	} {
		t.Run("wasm-browser-reject/"+example, func(t *testing.T) {
			path := "../../examples/" + example
			compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, false),
				"build", "--target", "wasm32-browser", path)
		})
	}
	for _, example := range []string{
		"hello.kizu",
		"aggregate_calls.kizu",
		"arena.kizu",
		"error_union_try.kizu",
		"error_set_undeclared.kizu",
		"std_io_stderr.kizu",
	} {
		t.Run("wasm-browser-binary/"+example, func(t *testing.T) {
			compareSelfhostWASMBinaryTarget(
				t, selfhost, "wasm32-browser", "../../examples/"+example,
			)
		})
	}
	t.Run("wasm-browser/exit-status", func(t *testing.T) {
		path := writeTempKizuSource(t, "browser_exit_status.kizu", `import std::process;

fn main() -> !process::ExitStatus {
    return process::ExitStatus::Specific(7);
}
`)
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, false),
			"build", "--target", "wasm32-browser", path)
		compareSelfhostWASMBinaryTarget(t, selfhost, "wasm32-browser", path)
	})
}

// compareSelfhostBrowserESMMissingRuntime checks an incomplete installation
// fails before creating an output directory and names the missing host asset.
func compareSelfhostBrowserESMMissingRuntime(t *testing.T, selfhost string) {
	t.Helper()
	directory := t.TempDir()
	libDir := filepath.Join(directory, "lib", "kizu")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(t, "../../lib/kizu/std", filepath.Join(libDir, "std"))
	seedDir := filepath.Join(directory, "seed")
	selfhostDir := filepath.Join(directory, "selfhost")
	command := func(output string) []string {
		return []string{
			"--lib-dir", libDir,
			"build", "--target", "wasm32-browser", "--emit", "esm",
			"-o", output, "../../examples/hello.kizu",
		}
	}
	seed := runNativeCLI(t, kizuBinaryPath, command(seedDir)...)
	shipping := runNativeCLI(t, selfhost, command(selfhostDir)...)
	if seed.code == 0 || shipping != seed {
		t.Fatalf("missing browser runtime CLI differs\nseed: %#v\nshipping: %#v", seed, shipping)
	}
	if !strings.Contains(seed.output.stderr, "browser/app.mjs: no such file or directory") ||
		!strings.Contains(seed.output.stderr, "set KIZU_LIB_DIR or pass --lib-dir") {
		t.Fatalf("missing browser runtime diagnostic is not actionable:\n%s", seed.output.stderr)
	}
	for _, output := range []string{seedDir, selfhostDir} {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("incomplete installation left output %s: %v", output, err)
		}
	}
}

// compareSelfhostBrowserESM checks both compilers write the same relocatable
// JavaScript host module and browser WebAssembly binary.
func compareSelfhostBrowserESM(t *testing.T, selfhost string, source string, opt bool) {
	t.Helper()
	directory := t.TempDir()
	seedDir := filepath.Join(directory, "seed")
	selfhostDir := filepath.Join(directory, "selfhost")
	seedArgs := []string{
		"build", "--target", "wasm32-browser", "--emit", "esm", "-o", seedDir,
	}
	shippingArgs := []string{
		"build", "--target", "wasm32-browser", "--emit", "esm", "-o", selfhostDir,
	}
	if opt {
		seedArgs = append(seedArgs, "--opt")
		shippingArgs = append(shippingArgs, "--opt")
	}
	seed := runNativeCLI(t, kizuBinaryPath, append(seedArgs, source)...)
	shipping := runNativeCLI(t, selfhost, append(shippingArgs, source)...)
	if seed.code != 0 || shipping.code != 0 || seed.output != shipping.output {
		t.Fatalf("browser esm CLI differs\nseed: %#v\nshipping: %#v", seed, shipping)
	}
	for _, name := range []string{browserESMModule, browserESMWASM} {
		seedBytes, err := os.ReadFile(filepath.Join(seedDir, name))
		if err != nil {
			t.Fatal(err)
		}
		shippingBytes, err := os.ReadFile(filepath.Join(selfhostDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(seedBytes, shippingBytes) {
			t.Fatalf("%s differs: seed=%d bytes shipping=%d bytes",
				name, len(seedBytes), len(shippingBytes))
		}
	}
}

// runSelfhostTargetAdapterBrowserCase compares browser target selection in WAT and binary output.
func runSelfhostTargetAdapterBrowserCase(t *testing.T, selfhost string) {
	t.Helper()
	path := "../../examples/modules/target_adapters"
	t.Run("wasm-browser/target-adapters", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, false),
			"build", "--target", "wasm32-browser", path)
		compareSelfhostArgs(t, selfhost, goBrowserWASMOutput(path, true),
			"build", "--target", "wasm32-browser", "--opt", path)
		compareSelfhostWASMBinaryTarget(t, selfhost, "wasm32-browser", path)
	})
}

// compareSelfhostWASMBinary checks the seed and shipping compiler write the
// exact same binary artifact without sending bytes to stdout.
func compareSelfhostWASMBinary(t *testing.T, selfhost string, source string) {
	compareSelfhostWASMBinaryTarget(t, selfhost, "wasm32-wasi", source)
}

// compareSelfhostWASMBinaryTarget checks one target's seed and shipping
// compiler write the exact same binary artifact.
func compareSelfhostWASMBinaryTarget(
	t *testing.T,
	selfhost string,
	target string,
	source string,
) {
	t.Helper()
	directory := t.TempDir()
	seedPath := filepath.Join(directory, "seed.wasm")
	selfhostPath := filepath.Join(directory, "selfhost.wasm")
	seed := runNativeCLI(t, kizuBinaryPath,
		"build", "--target", target, "--emit", "wasm", "-o", seedPath, source)
	shipping := runNativeCLI(t, selfhost,
		"build", "--target", target, "--emit", "wasm", "-o", selfhostPath, source)
	if seed.code != 0 || shipping.code != 0 || seed.output != shipping.output {
		t.Fatalf("binary CLI differs\nseed: %#v\nshipping: %#v", seed, shipping)
	}
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	shippingBytes, err := os.ReadFile(selfhostPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(seedBytes, shippingBytes) {
		t.Fatalf("binary artifacts differ: seed=%d bytes shipping=%d bytes",
			len(seedBytes), len(shippingBytes))
	}
}

// selfhostWASMExamples lists observable programs whose seed and shipping WAT
// must remain identical.
func selfhostWASMExamples() []string {
	return []string{
		"../../examples/aggregate_calls.kizu",
		"../../examples/union.kizu",
		"../../examples/enum.kizu",
		"../../examples/mutable_borrow_nested_field.kizu",
		"../../examples/optional_error_flow.kizu",
		"../../examples/slice_checked_access.kizu",
		"../../examples/std_array.kizu",
		"../../examples/std_array_append_bytes.kizu",
		"../../examples/std_array_token_list.kizu",
		"../../examples/fixed_buffer.kizu",
		"../../examples/bytes_iter.kizu",
		"../../examples/std_string_join_trim.kizu",
		"../../examples/user_allocator_refusal.kizu",
		"../../examples/std_mem_box_take.kizu",
		"../../examples/std_mem_box_ast.kizu",
		"../../examples/std_mem_box_cleanup.kizu",
		"../../examples/arena.kizu",
		"../../examples/arena_at_mut.kizu",
		"../../examples/arena_owner_elements.kizu",
		"../../examples/arena_add_recovers_from_a_full_allocator.kizu",
		"../../examples/std_map.kizu",
		"../../examples/std_map_integer_key.kizu",
		"../../examples/std_map_symbol_table.kizu",
		"../../examples/std_map_owner_value.kizu",
		"../../examples/map_insert_recovers_from_a_full_allocator.kizu",
		"../../examples/negative/std_map_owner_value_overwrite.kizu",
		"../../examples/std_json_decode_map.kizu",
		"../../examples/std_path_edges.kizu",
		"../../examples/main_exit_status.kizu",
		"../../examples/std_process.kizu",
		"../../examples/std_process_spawn.kizu",
		"../../examples/std_process_spawn_unreachable.kizu",
		"../../examples/std_io_process.kizu",
		"../../examples/std_io_stderr.kizu",
		"../../examples/fs_read.kizu",
		"../../examples/fs_read_dir.kizu",
		"../../examples/fs_real_path.kizu",
		"../../examples/std_fs_path.kizu",
		"../../examples/negative/std_process_arg_bounds.kizu",
		"../../examples/negative/std_io_failing_write.kizu",
		"../../examples/negative/fs_read_limit_exceeded.kizu",
		"../../examples/negative/fs_read_missing.kizu",
		"../../examples/negative/fs_failing_io.kizu",
		"../../examples/negative/fs_real_path_loop.kizu",
		"../../examples/negative/fs_real_path_missing.kizu",
		"../../examples/negative/fs_real_path_not_directory.kizu",
		"../../examples/negative/slice_syntax_index_out_of_bounds.kizu",
		"../../examples/negative/slice_syntax_range_out_of_bounds.kizu",
		"../../examples/negative/std_array_get_or_panic_bounds.kizu",
		"../../examples/negative/arena_handle_from_another_instance.kizu",
		"../../examples/negative/std_testing_run_fail.kizu",
		"../../examples/negative/std_testing_run_expect_equal.kizu",
		"../../examples/negative/std_testing_run_expect_equal_bool.kizu",
		"../../examples/negative/std_testing_run_expect_equal_bytes.kizu",
	}
}

// runSelfhostWASMContainerOptCases compares optimized owner-container lowering.
func runSelfhostWASMContainerOptCases(t *testing.T, selfhost string) {
	t.Helper()
	mapInteger := "../../examples/std_map_integer_key.kizu"
	t.Run("wasm/std_map_integer_key_opt", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(mapInteger, true),
			"build", "--target", "wasm32-wasi", "--opt", mapInteger)
	})
	jsonBox := "../../examples/std_json_decode_map.kizu"
	t.Run("wasm/std_json_decode_map_opt", func(t *testing.T) {
		compareSelfhostArgs(t, selfhost, goWASMOutput(jsonBox, true),
			"build", "--target", "wasm32-wasi", "--opt", jsonBox)
	})
}

// compareSelfhostInstalledTree checks both binaries find their library tree
// the way a released one has to: laid out as <prefix>/bin/kizu next to
// <prefix>/lib/kizu, run from an unrelated directory, with no environment
// pointing at anything. A symlinked bin is included because that is how an
// install is usually put on PATH.
func compareSelfhostInstalledTree(t *testing.T, selfhost string, file string) {
	t.Helper()
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	for _, dir := range []string{filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(t, "../../lib/kizu", filepath.Join(prefix, "lib", "kizu"))
	source, err := filepath.Abs(file)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "onpath")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, binary := range []struct {
		name string
		from string
	}{{"kizu", kizuBinaryPath}, {"selfhost", selfhost}} {
		installed := filepath.Join(prefix, "bin", binary.name)
		copyExecutable(t, binary.from, installed)
		link := filepath.Join(linked, binary.name)
		if err := os.Symlink(installed, link); err != nil {
			t.Fatal(err)
		}
		for _, entry := range []struct {
			name string
			path string
		}{{"installed", installed}, {"symlink", link}} {
			got := runNativeCLIAtEnv(t, elsewhere,
				[]string{stdlib.LibDirEnv + "="}, entry.path, "check", source)
			if got.code != 0 || got.output.stdout != "check: ok\n" {
				t.Errorf("%s from an installed tree failed (code=%d)\nstdout:\n%sstderr:\n%s",
					entry.path, got.code, got.output.stdout, got.output.stderr)
			}
			bundle := filepath.Join(root, "bundles", binary.name, entry.name)
			got = runNativeCLIAtEnv(t, elsewhere,
				[]string{stdlib.LibDirEnv + "="}, entry.path,
				"build", "--target", "wasm32-browser", "--emit", "esm",
				"-o", bundle, source)
			if got.code != 0 || got.output != (cliOutput{}) {
				t.Errorf("%s browser esm from an installed tree failed (code=%d)\nstdout:\n%sstderr:\n%s",
					entry.path, got.code, got.output.stdout, got.output.stderr)
				continue
			}
			for _, name := range []string{browserESMModule, browserESMWASM} {
				if _, err := os.Stat(filepath.Join(bundle, name)); err != nil {
					t.Errorf("%s: %v", entry.path, err)
				}
			}
		}
	}
}

// copyTree copies one directory recursively, following the shape a release
// tarball carries.
func copyTree(t *testing.T, from string, to string) {
	t.Helper()
	if out, err := exec.Command("cp", "-R", from, to).CombinedOutput(); err != nil {
		t.Fatalf("copy %s: %v\n%s", from, err, out)
	}
}

// copyExecutable copies one binary and keeps it runnable.
func copyExecutable(t *testing.T, from string, to string) {
	t.Helper()
	content, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, content, 0o755); err != nil {
		t.Fatal(err)
	}
}

// compareSelfhostVersion checks the selfhost binary names itself the way the
// Go one does. The line carries the revision each was built from, so it is
// compared byte for byte only when this run linked the selfhost compiler:
// a binary restored from the CI cache truthfully names an older revision.
func compareSelfhostVersion(t *testing.T, selfhost string) {
	t.Helper()
	for _, arg := range []string{"version", "--version"} {
		got := runNativeCLI(t, selfhost, arg)
		if got.code != 0 || got.output.stderr != "" {
			t.Fatalf("selfhost %s failed (code=%d)\nstderr:\n%s", arg, got.code, got.output.stderr)
		}
		if !strings.HasPrefix(got.output.stdout, "kizu ") {
			t.Errorf("selfhost %s output %q does not name the binary", arg, got.output.stdout)
		}
		if selfhostBuiltHere {
			want := runNativeCLI(t, kizuBinaryPath, arg)
			if got != want {
				t.Errorf("selfhost %s differs\n--- want\n%s--- got\n%s",
					arg, want.output.stdout, got.output.stdout)
			}
		}
	}
	// The refusal does not depend on what either binary was built from.
	compareSelfhostCLI(t, selfhost, "version", "EXTRA")
}

// runSelfhostArgumentCases compares one command line per distinct argument
// error the CLI reports, and per distinct failure of reading the target.
func runSelfhostArgumentCases(t *testing.T, selfhost string) {
	t.Helper()
	for _, arguments := range cliRepresentativeArguments() {
		t.Run("arguments/"+arguments.name, func(t *testing.T) {
			compareSelfhostCLI(t, selfhost, arguments.args...)
		})
	}
}

// goWASMOutput renders what `kizu build --target wasm32-wasi` prints for one
// source file or package, optimized when opt is set.
func goWASMOutput(path string, opt bool) cliOutput {
	module, err := lowerTargetForTarget(path, opt, stdtarget.WasmWASI)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	text, err := wasm.Emit(module)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: text + "\n"}
}

// goBrowserWASMOutput renders a source file or package through the browser target.
func goBrowserWASMOutput(path string, opt bool) cliOutput {
	module, err := lowerTargetForTarget(path, opt, stdtarget.WasmBrowser)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	ir.KeepTargetReachableFunctions(module, "browser", "main")
	lowered, err := wasm.LowerTarget(module, wasm.TargetBrowser)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: lowered.WAT() + "\n"}
}

// wasmFixture is one program the wasm backend is compared on.
type wasmFixture struct {
	name   string
	source string
	opt    bool
}

// wasmRepresentativeFixtures is one program per wasm backend behavior: what it
// emits, then each rejection message the target subset draws.
func wasmRepresentativeFixtures() []wasmFixture {
	return append(wasmEmitFixtures(), wasmRejectionFixtures()...)
}

// wasmEmitFixtures is one program per emitted shape: calls with i64 parameters
// and a return, the phi copies and dispatch arms of a loop with a bool print,
// what the optimizer folds away, the bytes a data segment escapes, a tagged
// field, and branch-local returns. The plain data segment and string print are
// the hello file the command table already crosses.
func wasmEmitFixtures() []wasmFixture {
	return []wasmFixture{
		{name: "functions", source: `fn add(a: i64, b: i64) -> i64 {
    return a + b;
}

fn main() {
    print(add(1, 2));
}
`},
		{name: "control_flow", source: `fn main() {
    var i = 0;
    while i < 3 {
        print(i);
        i = i + 1;
    }
    print(true);
}
`},
		{name: "opt", opt: true, source: `fn unused(x: i64) -> i64 {
    return x * 2;
}

fn main() {
    let a = 2 + 3;
    if a > 4 {
        print(a);
    } else {
        print(0);
    }
}
`},
		// A Kizu literal carries the bytes between the quotes verbatim, so the
		// tab, the backslash and the two multi-byte characters below reach the
		// data segment as themselves, and the multi-line literal joins with a
		// newline. Together they cross every escape the WAT writer draws.
		{name: "data_escapes", source: "fn main() {\n" +
			"    print(\"tab\there\");\n" +
			"    print(\"back\\slash and tilde ~\");\n" +
			"    print(\"héllo ✓\");\n" +
			"    print(\n" +
			"        \\\\first line\n" +
			"        \\\\second line\n" +
			"    );\n" +
			"}\n"},
		{name: "tagged_field", source: `struct Holder {
    value: ?i64,
}

fn main() {
    let holder = Holder { value: null };
}
`},
		{name: "branch_returns", source: `fn pick(x: i64) -> i64 {
    if x > 0 {
        return 1;
    } else {
        return 2;
    }
}

fn main() {
    print(pick(1));
}
`},
	}
}

// wasmRejectionFixtures keeps one program per rejection message the backend
// reports for scalar types it does not lower.
func wasmRejectionFixtures() []wasmFixture {
	return []wasmFixture{
		{name: "reject_instruction", source: `import std;

fn main() {
    let s = "abcd";
    print(std::mem::len(s));
}
`},
		{name: "reject_print_enum", source: `enum Color {
    Red,
    Blue,
}

fn main() {
    let c = Color::Red;
    print(c);
}
`},
		{name: "reject_print_type", source: `fn main() {
    let x = cast<u32>(5);
    print(x);
}
`},
	}
}

// cliArguments is one command line and the name its subtest runs under.
type cliArguments struct {
	name string
	args []string
}

// cliRepresentativeArguments is one command line per distinct message the CLI
// prints for a command line it will not run, and per distinct reason reading
// the target can fail. The file the good cases name is the hello example, so a
// failure here is the argument handling and not the program.
func cliRepresentativeArguments() []cliArguments {
	file := "../../examples/hello.kizu"
	pkg := "../../tests/behavior"
	return []cliArguments{
		{"unknown_command", []string{"frobnicate", file}},
		{"parse_extra_target", []string{"parse", file, "EXTRA"}},
		{"check_extra_target", []string{"check", file, "EXTRA"}},
		{"ir_lone_opt", []string{"ir", "--opt"}},
		{"ir_extra_target", []string{"ir", file, "EXTRA"}},
		{"build_unknown_subcommand", []string{"build", "junk"}},
		{"build_missing_subcommand", []string{"build", "--target"}},
		{"build_unknown_target", []string{"build", "--target", "wasm64"}},
		{"wasm_missing_file", []string{"build", "--target", "wasm32-wasi"}},
		{"wasm_extra_file", []string{"build", "--target", "wasm32-wasi", "a.kizu", "b.kizu"}},
		{"wasm_duplicate_opt", []string{"build", "--target", "wasm32-wasi", "--opt", "--opt", file}},
		{"wasm_emit_missing_format", []string{"build", "--target", "wasm32-wasi", file, "--emit"}},
		{"wasm_invalid_emit", []string{"build", "--target", "wasm32-wasi", "--emit", "object", file}},
		{"wasm_esm_invalid_target", []string{
			"build", "--target", "wasm32-wasi", "--emit", "esm", "-o", "unused", file,
		}},
		{"wasm_duplicate_emit", []string{
			"build", "--target", "wasm32-wasi", "--emit", "wat", "--emit", "wasm", file,
		}},
		{"wasm_output_missing_path", []string{"build", "--target", "wasm32-wasi", file, "-o"}},
		{"wasm_duplicate_output", []string{
			"build", "--target", "wasm32-wasi", "-o", "a.wat", "-o", "b.wat", file,
		}},
		{"wasm_unknown_option", []string{"build", "--target", "wasm32-wasi", "--wat", file}},
		{"wasm_binary_missing_output", []string{
			"build", "--target", "wasm32-wasi", "--emit", "wasm", file,
		}},
		{"wasm_browser_missing_file", []string{"build", "--target", "wasm32-browser"}},
		{"wasm_browser_extra_file", []string{
			"build", "--target", "wasm32-browser", "a.kizu", "b.kizu",
		}},
		{"wasm_browser_binary_missing_output", []string{
			"build", "--target", "wasm32-browser", "--emit", "wasm", file,
		}},
		{"wasm_browser_emit_missing_format", []string{
			"build", "--target", "wasm32-browser", file, "--emit",
		}},
		{"wasm_browser_esm_missing_output", []string{
			"build", "--target", "wasm32-browser", "--emit", "esm", file,
		}},
		{"target_not_found", []string{"parse", "../../examples/missing.kizu"}},
		{"target_is_directory", []string{"parse", pkg}},
		{"target_under_file", []string{"parse", file + "/inner.kizu"}},
	}
}

// compareSelfhostCLI runs one command line through both compilers and compares
// what each printed and the exact status it exited with. The usage lines an
// argument error prints belong to the CLI, not to a package the test can call.
func compareSelfhostCLI(t *testing.T, selfhost string, args ...string) {
	t.Helper()
	want := runNativeCLI(t, kizuBinaryPath, args...)
	got := runNativeCLI(t, selfhost, args...)
	if got != want {
		t.Errorf("selfhost %s differs\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			strings.Join(args, " "), want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
	}
}

// goImportCHeaderOutput renders the shipping import-c-header path without
// starting one Go process per header, the pattern goFmtOutput uses.
func goImportCHeaderOutput(file string) cliOutput {
	source, err := os.ReadFile(file)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	declarations, err := cimport.Import(string(source))
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: declarations + "\n"}
}

// cHeaderFixture is one C header and the name its subtest runs under.
type cHeaderFixture struct {
	name   string
	source string
}

// cimportRepresentativeHeaders is one header per import-c-header behavior: the
// shapes the importer accepts on one side, and every feature it rejects on the
// other. One rejection ends the whole header, so each needs its own file.
func cimportRepresentativeHeaders() []cHeaderFixture {
	return []cHeaderFixture{
		{
			name: "prototypes",
			source: `int puts(const char *s);
void write_byte(unsigned char *p, unsigned char value);
size_t len(const uint8_t *data, size_t n);
int read_i32(const int32_t *);
void nada();
double zeta(void);
`,
		},
		{
			// The U+00A0 separates a type from a name: Go splits fields on
			// unicode.IsSpace, so an ASCII-only importer would reject this.
			name: "comments_and_spacing",
			source: "/* block\n   comment */\nint   a ( void ) ;   // trailing comment\n" +
				"/* mid */ int\u00a0b (  int   x ) ; // another\n",
		},
		{
			name: "pointers_and_qualifiers",
			source: `void deep(const char **p, char *const q, volatile int *r);
void keep(int *restrict r, unsigned long long z);
float scale(double *out, intptr_t offset);
`,
		},
		{
			name:   "only_comments",
			source: "// nothing but comments\n/* and a block */\n",
		},
		{name: "preprocessor", source: "#define X 1\n"},
		{name: "typedef", source: "typedef int my_int;\n"},
		{name: "struct", source: "struct point { int x; };\n"},
		{name: "enum", source: "enum color { RED };\n"},
		{name: "variadic", source: "int printf(const char *fmt, ...);\n"},
		{name: "function_pointer", source: "void qsort_cb(int (*cmp)(int, int));\n"},
		{name: "array", source: "void fill(char buf[8]);\n"},
		{name: "unknown_type", source: "widget make_widget(void);\n"},
		{name: "empty_parameter", source: "int pair(,);\n"},
		{name: "not_a_prototype", source: "int counter;\n"},
	}
}

// failedCLIResult renders one Go error through the shipping top-level prefix.
func failedCLIResult(err error) nativeCLIResult {
	return nativeCLIResult{
		output: cliOutput{stderr: cliErrorLine(err), failed: true},
		code:   1,
	}
}

// compareSelfhostFmtWrite checks both accepted in-place flags and the bytes
// each compiler leaves in the file.
func compareSelfhostFmtWrite(t *testing.T, selfhost string, flag string, source string) {
	t.Helper()
	root := t.TempDir()
	goFile := filepath.Join(root, "go", "unformatted.kizu")
	selfFile := filepath.Join(root, "selfhost", "unformatted.kizu")
	writeTestFile(t, goFile, []byte(source))
	writeTestFile(t, selfFile, []byte(source))

	want := runNativeCLI(t, kizuBinaryPath, "fmt", flag, goFile)
	got := runNativeCLI(t, selfhost, "fmt", flag, selfFile)
	if got != want {
		t.Errorf("selfhost fmt %s differs\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			flag, want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
	}
	compareFileBytes(t, goFile, selfFile)
}

// compareSelfhostInit checks the created package, success output and both
// existing-file rejection paths in isolated directory trees.
func compareSelfhostInit(t *testing.T, selfhost string) {
	t.Helper()
	root := t.TempDir()
	goTarget := filepath.Join(root, "go", "my-app")
	selfTarget := filepath.Join(root, "selfhost", "my-app")

	compareInitRun(t, selfhost, goTarget, selfTarget)
	compareFileBytes(t,
		filepath.Join(goTarget, "kizu.toml"),
		filepath.Join(selfTarget, "kizu.toml"))
	compareFileBytes(t,
		filepath.Join(goTarget, "src", "main.kizu"),
		filepath.Join(selfTarget, "src", "main.kizu"))

	// A second run rejects kizu.toml before touching either generated file.
	compareInitRun(t, selfhost, goTarget, selfTarget)

	goMainTarget := filepath.Join(root, "go", "main-existing")
	selfMainTarget := filepath.Join(root, "selfhost", "main-existing")
	existing := []byte("keep me\n")
	writeTestFile(t, filepath.Join(goMainTarget, "src", "main.kizu"), existing)
	writeTestFile(t, filepath.Join(selfMainTarget, "src", "main.kizu"), existing)
	compareInitRun(t, selfhost, goMainTarget, selfMainTarget)
	compareFileBytes(t,
		filepath.Join(goMainTarget, "src", "main.kizu"),
		filepath.Join(selfMainTarget, "src", "main.kizu"))
	for _, target := range []string{goMainTarget, selfMainTarget} {
		if _, err := os.Stat(filepath.Join(target, "kizu.toml")); !os.IsNotExist(err) {
			t.Errorf("%s kizu.toml stat error = %v, want not exist", target, err)
		}
	}

	goCurrent := filepath.Join(root, "go", "current-project")
	selfCurrent := filepath.Join(root, "selfhost", "current-project")
	for _, target := range []string{goCurrent, selfCurrent} {
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := normalizeInitResult(
		runNativeCLIAt(t, goCurrent, kizuBinaryPath, "init"), goCurrent)
	got := normalizeInitResult(
		runNativeCLIAt(t, selfCurrent, selfhost, "init"), selfCurrent)
	if got != want {
		t.Errorf("selfhost init current directory differs\n"+
			"--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
	}
	compareFileBytes(t,
		filepath.Join(goCurrent, "kizu.toml"),
		filepath.Join(selfCurrent, "kizu.toml"))
	compareFileBytes(t,
		filepath.Join(goCurrent, "src", "main.kizu"),
		filepath.Join(selfCurrent, "src", "main.kizu"))
}

// compareInitRun normalizes only the deliberately different absolute target
// roots; everything else, including stdout/stderr and exact exit status, must
// match byte for byte.
func compareInitRun(t *testing.T, selfhost string, goTarget string, selfTarget string) {
	t.Helper()
	want := normalizeInitResult(runNativeCLI(t, kizuBinaryPath, "init", goTarget), goTarget)
	got := normalizeInitResult(runNativeCLI(t, selfhost, "init", selfTarget), selfTarget)
	if got != want {
		t.Errorf("selfhost init differs\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
	}
}

// normalizeInitResult replaces the one path that must differ between the two
// isolated init targets.
func normalizeInitResult(result nativeCLIResult, target string) nativeCLIResult {
	result.output.stdout = strings.ReplaceAll(result.output.stdout, target, "<target>")
	result.output.stderr = strings.ReplaceAll(result.output.stderr, target, "<target>")
	return result
}

// writeTestFile creates parent directories and writes one isolated fixture.
func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// compareFileBytes checks two generated files without normalizing their
// contents.
func compareFileBytes(t *testing.T, left string, right string) {
	t.Helper()
	leftBytes, err := os.ReadFile(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := os.ReadFile(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		t.Errorf("generated files differ\n--- %s\n%s--- %s\n%s",
			left, leftBytes, right, rightBytes)
	}
}

// goParseOutput renders what `kizu parse` prints for a file.
func goParseOutput(file string) cliOutput {
	program, diags, err := parsePath(file)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	if len(diags) > 0 {
		var stderr strings.Builder
		for _, d := range diags {
			stderr.WriteString(d.CLIError())
			stderr.WriteByte('\n')
		}
		stderr.WriteString("error: parse failed\n")
		return cliOutput{stderr: stderr.String(), failed: true}
	}
	return cliOutput{stdout: program.String() + "\n"}
}

// goCheckOutput renders what `kizu check` prints for a target by running the
// path checkFile runs, backend included. Rebuilding the pipeline here from the
// checkers alone once let the promise be weaker than the command's, and both
// sides agreed on the weaker one.
func goCheckOutput(target string) cliOutput {
	module, err := lowerRunTarget(target)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	if _, err := llvm.Emit(module); err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: "check: ok\n"}
}

// goIrOutput renders what `kizu ir` prints for a target: the dump of the
// lowered module, optimized when opt is set. A package lowers the way build
// lowers it, with the package main exposed as the entrypoint.
func goIrOutput(target string, opt bool) cliOutput {
	module, err := lowerTarget(target, opt)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: ir.Dump(module) + "\n"}
}

// goEmitLLVMOutput renders what `kizu build --emit-llvm` prints for a
// target: the LLVM IR emitted from the lowered module, optimized when opt is
// set.
func goEmitLLVMOutput(target string, opt bool) cliOutput {
	module, err := lowerTarget(target, opt)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	ir.KeepTargetReachableFunctions(module, "", "main")
	text, err := llvm.Emit(module)
	if err != nil {
		return cliOutput{stderr: cliErrorLine(err), failed: true}
	}
	return cliOutput{stdout: text + "\n"}
}

// cliErrorLine renders an error the way printError writes it.
func cliErrorLine(err error) string {
	var structured *diag.Diagnostic
	if errors.As(err, &structured) {
		return structured.CLIError() + "\n"
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "error:") {
		return msg + "\n"
	}
	return "error: " + msg + "\n"
}

// nativeCLIResult is one command run: what it printed, whether it failed,
// and the exact status it exited with.
type nativeCLIResult struct {
	output cliOutput
	code   int
}

// runNativeCLI runs one binary with its own TMPDIR, so the selfhost
// temporary build directories of parallel cases cannot collide.
func runNativeCLI(t *testing.T, name string, args ...string) nativeCLIResult {
	t.Helper()
	return runNativeCLIAt(t, "", name, args...)
}

// runNativeCLIAt runs one binary from dir and pins PWD to the same absolute
// path, which is how the selfhost fsutil replacement observes filepath.Abs.
func runNativeCLIAt(t *testing.T, dir string, name string, args ...string) nativeCLIResult {
	t.Helper()
	return runNativeCLIAtEnv(t, dir, nil, name, args...)
}

// runNativeCLIAtEnv runs one binary with explicit environment overrides.
func runNativeCLIAtEnv(
	t *testing.T,
	dir string,
	extraEnv []string,
	name string,
	args ...string,
) nativeCLIResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	env := append(os.Environ(), "TMPDIR="+t.TempDir())
	env = append(env, extraEnv...)
	if dir != "" {
		withoutPWD := env[:0]
		for _, item := range env {
			if !strings.HasPrefix(item, "PWD=") {
				withoutPWD = append(withoutPWD, item)
			}
		}
		env = append(withoutPWD, "PWD="+dir)
	}
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %s: %v", name, runErr)
		}
	}
	return nativeCLIResult{
		output: cliOutput{stdout: stdout.String(), stderr: stderr.String(), failed: runErr != nil},
		code:   code,
	}
}

// clangNoise matches the toolchain lines the Go CLI captures and discards
// on success, which the selfhost CLI lets through because std::process
// inherits the child's streams.
var clangNoise = regexp.MustCompile(
	`^(warning: overriding the module target triple .*|\d+ warnings? generated\.)$`)

// selfhostNativeStderr drops what only the selfhost path prints on stderr:
// the inherited toolchain noise the Go CLI captures and discards.
func selfhostNativeStderr(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if clangNoise.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 || (len(kept) == 1 && kept[0] == "") {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}

// isClangFailure reports whether a CLI run failed inside the C/LLVM
// toolchain. Those cases are excluded from the comparison: the Go error
// carries clang's captured output, which the selfhost CLI cannot capture.
func isClangFailure(out cliOutput) bool {
	return out.failed && strings.Contains(out.stderr, " failed: exit status ")
}

// compareNativeBuild builds one target with both compilers, compares the
// command output, then runs both artifacts and compares what they print
// and their exact exit status, and compares the build metadata with every
// absolute path normalized.
func compareNativeBuild(t *testing.T, selfhost string, target string, opt bool) {
	t.Helper()
	if goCheckOutput(target).failed {
		t.Skip("target does not check")
	}
	goOut := filepath.Join(t.TempDir(), "program")
	selfOut := filepath.Join(t.TempDir(), "program")
	args := []string{"build", "--target", "native"}
	if opt {
		args = append(args, "--opt")
	}
	wantArgs := append(append([]string{}, args...), "-o", goOut, target)
	want := runNativeCLI(t, kizuBinaryPath, wantArgs...)
	if isClangFailure(want.output) {
		t.Skip("clang failure output cannot be captured by the selfhost CLI")
	}
	gotArgs := append(append([]string{}, args...), "-o", selfOut, target)
	got := runNativeCLI(t, selfhost, gotArgs...)
	got.output.stderr = selfhostNativeStderr(got.output.stderr)
	want.output.stdout = strings.ReplaceAll(want.output.stdout, goOut, "OUTPUT")
	got.output.stdout = strings.ReplaceAll(got.output.stdout, selfOut, "OUTPUT")
	if got.output != want.output || got.code != want.code {
		t.Errorf("selfhost build %s differs\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			target, want.code, want.output.stdout, want.output.stderr,
			got.code, got.output.stdout, got.output.stderr)
		return
	}
	if want.output.failed {
		return
	}
	wantRun := runNativeCLI(t, goOut)
	gotRun := runNativeCLI(t, selfOut)
	if gotRun.output != wantRun.output || gotRun.code != wantRun.code {
		t.Errorf("built executables for %s differ\n--- want (code=%d)\nstdout:\n%sstderr:\n%s"+
			"--- got (code=%d)\nstdout:\n%sstderr:\n%s",
			target, wantRun.code, wantRun.output.stdout, wantRun.output.stderr,
			gotRun.code, gotRun.output.stdout, gotRun.output.stderr)
	}
	assertNativeMetadata(t, goOut+".kizu-build.json", goOut, opt)
	wantMetadata := normalizedMetadata(t, goOut+".kizu-build.json")
	gotMetadata := normalizedMetadata(t, selfOut+".kizu-build.json")
	if gotMetadata != wantMetadata {
		t.Errorf("metadata for %s differs\n--- want\n%s\n--- got\n%s", target, wantMetadata, gotMetadata)
	}
}

// assertNativeMetadata checks the artifact built above records the requested
// mode and the command that produced it, without paying for another link.
func assertNativeMetadata(t *testing.T, path string, output string, opt bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Target  string   `json:"target"`
		LibC    string   `json:"libc"`
		Runtime string   `json:"runtime"`
		Emit    string   `json:"emit"`
		Linker  string   `json:"linker"`
		OptMode string   `json:"optimization_mode"`
		Output  string   `json:"output"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantMode := "debug"
	wantFlag := "-O0"
	if opt {
		wantMode = "opt"
		wantFlag = "-O3"
	}
	if got.Target != "native" || got.LibC != "on" || got.Runtime != "hosted" ||
		got.Emit != "exe" || got.Linker != "clang" || got.Output != output ||
		got.OptMode != wantMode {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if len(got.Command) == 0 || got.Command[0] != "clang" ||
		!slices.Contains(got.Command, wantFlag) {
		t.Fatalf("metadata command %v missing clang or %s", got.Command, wantFlag)
	}
}

// metadataAbsPath matches a JSON string holding an absolute path: the
// output the two builds are asked for and the temporary paths the IR and
// runtime object are written to differ between the two compilers by design.
var metadataAbsPath = regexp.MustCompile(`"/[^"]*"`)

// normalizedMetadata reads one build metadata file with every absolute
// path replaced, leaving the shape and the explicit build inputs.
func normalizedMetadata(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return metadataAbsPath.ReplaceAllString(string(data), `"PATH"`)
}
