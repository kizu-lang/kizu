package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/kizu-lang/kizu/internal/ir"
)

// runFile builds a source file or package into a native executable and runs it.
//
// `run` and `build` are one path: both lower the program the same way and hand
// the same IR to the same linker, and differ only in where the executable goes.
// A program cannot behave one way under run and another way under build.
func runFile(path string, args []string) error {
	exe, err := buildRunExecutable(path)
	if err != nil {
		return err
	}
	return executeBuilt(exe, args)
}

// buildRunExecutable lowers a run target and links it into an executable.
func buildRunExecutable(path string) (string, error) {
	module, err := lowerRunTarget(path)
	if err != nil {
		return "", err
	}
	return linkModule(module)
}

// lowerRunTarget lowers either a package root or a single source file.
func lowerRunTarget(path string) (*ir.Module, error) {
	if isPackageRoot(path) {
		return lowerPackage(path, false)
	}
	return lowerFile(path, false)
}

// executeBuilt runs a linked executable and forwards its exit status.
func executeBuilt(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exitStatus{code: childExitCode(exit)}
		}
		return err
	}
	return nil
}

// childExitCode reports a child's exit the way the wait status spells it: a
// signal death is 128 plus the signal, the convention the runtime's own
// process primitives use, rather than the -1 the Go wrapper reports.
func childExitCode(exit *exec.ExitError) int {
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exit.ExitCode()
}
