package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/stdlib"
)

// main dispatches the kizu command line interface.
func main() {
	args := takeLibDir(os.Args[1:])
	if len(args) < 1 || (len(args) < 2 && !commandAllowsNoTarget(args[0])) {
		usage()
		os.Exit(2)
	}
	if err := dispatch(args[0], args[1:]); err != nil {
		var status exitStatus
		if errors.As(err, &status) {
			os.Exit(status.code)
		}
		printError(err)
		os.Exit(1)
	}
}

// takeLibDir reads the leading `--lib-dir PATH` and returns the rest. It comes
// before the command because it says where the compiler's own library tree is,
// which every command reads and no command owns.
func takeLibDir(args []string) []string {
	if len(args) < 2 || args[0] != "--lib-dir" {
		return args
	}
	stdlib.SetLibDir(args[1])
	return args[2:]
}

// exitStatus exits without printing an extra Go diagnostic.
type exitStatus struct {
	code int
}

// Error renders the process exit status for tests and wrapping.
func (s exitStatus) Error() string {
	return fmt.Sprintf("exit status %d", s.code)
}

// printError writes a stable top-level CLI error prefix.
func printError(err error) {
	var structured *diag.Diagnostic
	if errors.As(err, &structured) {
		_, _ = fmt.Fprintln(os.Stderr, structured.CLIError())
		return
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "error:") {
		_, _ = fmt.Fprintln(os.Stderr, msg)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "error: "+msg)
}

// printParserDiagnostics writes parser diagnostics using the shared CLI severity prefix.
func printParserDiagnostics(diags []parser.Diagnostic) {
	for _, diag := range diags {
		_, _ = fmt.Fprintln(os.Stderr, diag.CLIError())
	}
}

// dispatch runs one CLI command.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "parse":
		if len(args) != 1 {
			usage()
			return fmt.Errorf("parse takes one target")
		}
		return parseFile(args[0])
	case "run":
		return dispatchRun(args)
	case "check":
		if len(args) != 1 {
			usage()
			return fmt.Errorf("check takes one target")
		}
		return checkFile(args[0])
	case "test":
		return dispatchTest(args)
	case "fmt":
		return fmtCommand(args)
	case "init":
		return initCommand(args)
	case "ir":
		return irCommand(args)
	case "build":
		return buildFile(args)
	case "cache":
		return cacheCommand(args)
	case "import-c-header":
		return importCHeaderFile(args[0])
	case "version", "--version":
		return versionCommand(args)
	default:
		usage()
		return fmt.Errorf("unknown command `%s`", cmd)
	}
}

// dispatchRun runs a program through the Go-owned path.
func dispatchRun(args []string) error {
	path, programArgs := splitProgramArgs(args)
	return runFile(path, programArgs)
}

// dispatchTest runs a program's tests through the Go-owned path.
func dispatchTest(args []string) error {
	path, programArgs := splitProgramArgs(args)
	return testFile(path, programArgs)
}

// usage prints the supported command line shape.
func usage() {
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu <parse|run|check|test> <file> [-- args...]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu init [path]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu fmt [--write] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu ir [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --emit-llvm [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target native [native-options] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target wasm32-wasi [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu cache <status|prune>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu import-c-header <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu version")
}

// splitProgramArgs separates the source path from optional Kizu process args.
func splitProgramArgs(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) >= 2 && args[1] == "--" {
		return args[0], args[2:]
	}
	return args[0], nil
}
