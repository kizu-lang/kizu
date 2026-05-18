package native

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Options describes one native link request.
type Options struct {
	LLVMIR  string
	Output  string
	Triple  string
	LibC    string
	Runtime string
	Emit    string
}

// Build writes transient inputs and links them into a native executable.
func Build(options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "kizu-native-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	irPath, runtimePath, err := writeInputs(tmp, options.LLVMIR)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0o755); err != nil {
		return err
	}
	return runClang(irPath, runtimePath, options)
}

// validateOptions rejects native build modes that do not have a concrete backend yet.
func validateOptions(options Options) error {
	if options.Output == "" {
		return fmt.Errorf("native error: output path is required")
	}
	if options.LibC != "on" {
		return fmt.Errorf("native error: --libc %s is not implemented yet", options.LibC)
	}
	if options.Runtime != "hosted" {
		return fmt.Errorf("native error: --runtime %s is not implemented yet", options.Runtime)
	}
	if options.Emit != "exe" {
		return fmt.Errorf("native error: --emit %s is not implemented yet", options.Emit)
	}
	return nil
}

// writeInputs writes LLVM IR and the minimal Kizu runtime shim.
func writeInputs(dir string, llvmIR string) (string, string, error) {
	irPath := filepath.Join(dir, "main.ll")
	runtimePath := filepath.Join(dir, "runtime.c")
	if err := os.WriteFile(irPath, []byte(llvmIR), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(runtimePath, []byte(runtimeSource), 0o644); err != nil {
		return "", "", err
	}
	return irPath, runtimePath, nil
}

// runClang invokes the host C/LLVM toolchain with explicit inputs.
func runClang(irPath string, runtimePath string, options Options) error {
	args := []string{}
	if options.Triple != "" {
		args = append(args, "-target", options.Triple)
	}
	args = append(args, irPath, runtimePath, "-o", options.Output)
	cmd := exec.Command("clang", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("native error: clang failed: %w\n%s", err, out)
	}
	return nil
}

const runtimeSource = `
#include <stdint.h>
#include <stdio.h>

void kizu_print_string(const unsigned char *s, int64_t len) {
    fwrite(s, 1, (size_t)len, stdout);
    fputc('\n', stdout);
}

void kizu_print_int(int64_t v) {
    printf("%lld\n", (long long)v);
}

void kizu_print_bool(_Bool v) {
    fputs(v ? "true\n" : "false\n", stdout);
}
`
