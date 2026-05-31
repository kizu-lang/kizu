package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// compareRunProgramResult checks hosted run output against checked-in goldens.
func compareRunProgramResult(
	t *testing.T,
	item runParityCase,
	result runParityResult,
	expectedOut string,
	expectedErr string,
) int {
	t.Helper()
	if result.program.code != item.exitCode ||
		result.program.stdout != expectedOut ||
		result.program.stderr != expectedErr {
		t.Errorf("run parity %s program mismatch", item.name)
		return 1
	}
	return 0
}

// linkRunParityExecutableWithHost links one emitted run artifact with a host runtime.
func linkRunParityExecutableWithHost(
	clang string,
	llPath string,
	hostLLPath string,
	exePath string,
) error {
	compile := exec.Command(
		clang,
		"-Wno-override-module",
		"-fno-integrated-as",
		llPath,
		hostLLPath,
		"selfhost/runtime/selfhost.hosted.c",
		"-o",
		exePath,
	)
	if out, err := compile.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// runRunParityExecutable captures stdout, stderr, and exit code for run output.
func runRunParityExecutable(t *testing.T, exePath string) bootstrapCommandResult {
	t.Helper()
	start := time.Now()
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Errorf("resolve %s: %v", exePath, err)
	}
	run := exec.Command(absExe)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	return bootstrapCommandResult{
		name:    filepath.Base(exePath),
		command: absExe,
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// fileSize returns the size of a regular file.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	return info.Size(), nil
}
