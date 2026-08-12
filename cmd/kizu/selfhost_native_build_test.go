package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type nativeSelfhostBuildConfig struct {
	name       string
	outputPath string
	cacheDir   string
}

// buildNativeSelfhost compiles the selfhost package through the Go native compiler.
func buildNativeSelfhost(
	t *testing.T,
	config nativeSelfhostBuildConfig,
) selfhostCommandResult {
	t.Helper()
	start := time.Now()
	build := exec.Command(
		"go",
		"run",
		"./cmd/kizu",
		"build",
		"--target",
		"native",
		"--opt",
		"--libc",
		"on",
		"--runtime",
		"hosted",
		"--emit",
		"exe",
		"--linker",
		"clang",
		"-o",
		config.outputPath,
		"selfhost",
	)
	build.Env = append(os.Environ(), "KIZU_CACHE_DIR="+config.cacheDir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	build.Stdout = &stdout
	build.Stderr = &stderr
	err := build.Run()
	return selfhostCommandResult{
		name:    "build " + config.name,
		command: strings.Join(build.Args, " "),
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// stripClangDriverNoise drops toolchain warning lines clang emits for wrapper flags
// it does not consume (nix wrappers pass framework/include flags to every job). They
// vary by machine and toolchain and say nothing about the artifact under test.
func stripClangDriverNoise(stderr string) string {
	lines := strings.Split(stderr, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "clang-") &&
			strings.Contains(line, ": warning: argument unused during compilation:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestSelfhostStage0NativeGate is the ADR-0081 required gate: the Go backend
// (stage0) compiles the selfhost package to a native executable, and that
// executable answers the frontend commands. Nothing else produces a selfhost
// binary, so a red here means selfhost cannot be built at all.
func TestSelfhostStage0NativeGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_NATIVE") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_NATIVE=1 to run the stage0 native gate")
	}
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	defer restore()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("stage0 native gate requires clang: %v", err)
	}

	runner := "target/selfhost/stage0-native/selfhost"
	build := buildNativeSelfhost(t, nativeSelfhostBuildConfig{
		name:       "stage0 native selfhost",
		outputPath: runner,
		cacheDir:   "target/selfhost/stage0-native-cache",
	})
	if build.code != 0 || build.stdout != runner+"\n" {
		t.Fatalf("stage0 build failed code=%d\nstdout=%q\nstderr=%s",
			build.code, build.stdout, stripClangDriverNoise(build.stderr))
	}

	for _, smoke := range []struct {
		args   []string
		stdout string
	}{
		{[]string{"check", "examples/hello.kizu"}, "check: ok\n"},
		{[]string{"run", "examples/hello.kizu"}, "hello, kizu\n"},
		{[]string{"fmt", "examples/hello.kizu"}, "fn main() {\n    print(\"hello, kizu\");\n}\n"},
	} {
		result := runSelfhostCommand(t, runner, smoke.args...)
		if result.code != 0 || result.stdout != smoke.stdout {
			t.Errorf("stage0 native %v: code=%d stdout=%q stderr=%q",
				smoke.args, result.code, result.stdout, result.stderr)
		}
	}
}
