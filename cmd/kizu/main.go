package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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

// dispatchTest runs a program's tests through the Go-owned path, or with -o
// builds the test executable without running it.
func dispatchTest(args []string) error {
	options, err := parseTestArgs(args)
	if err != nil {
		usage()
		return err
	}
	if options.Output != "" {
		return buildTestFile(options)
	}
	return testFile(options.Path, options.Args, options.Seed)
}

// testArgs is what one `kizu test` command line asks for.
type testArgs struct {
	Path   string
	Args   []string
	Seed   *int64
	Triple string
	Output string
}

// parseTestArgs reads the flags before the target, then the target and the
// program args after `--`. A test executable built with -o is not run here,
// so it takes no program args, and one built for another machine cannot be
// run here, so --triple needs -o to say where it goes.
func parseTestArgs(args []string) (testArgs, error) {
	var parsed testArgs
	for len(args) > 0 && (args[0] == "--seed" || args[0] == "--triple" || args[0] == "-o") {
		if len(args) < 2 {
			return testArgs{}, fmt.Errorf("missing value after %s", args[0])
		}
		switch args[0] {
		case "--seed":
			value, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return testArgs{}, fmt.Errorf("test error: --seed expects an integer, got `%s`", args[1])
			}
			parsed.Seed = &value
		case "--triple":
			parsed.Triple = args[1]
		default:
			parsed.Output = args[1]
		}
		args = args[2:]
	}
	parsed.Path, parsed.Args = splitProgramArgs(args)
	switch {
	case parsed.Path == "":
		return testArgs{}, fmt.Errorf("missing source file")
	case parsed.Triple != "" && parsed.Output == "":
		return testArgs{}, fmt.Errorf("test error: --triple builds a test executable " +
			"to run elsewhere; name it with -o")
	case parsed.Output != "" && len(parsed.Args) > 0:
		return testArgs{}, fmt.Errorf("test error: -o builds the test executable " +
			"without running it, so it takes no program args")
	}
	return parsed, nil
}

const wasmWATUsage = "usage: kizu build --target wasm32-wasi " +
	"[--emit wat] [--opt] [-o file] <file|package>"

const wasmBinaryUsage = "usage: kizu build --target wasm32-wasi " +
	"--emit wasm -o file [--opt] <file|package>"

const browserWATUsage = "usage: kizu build --target wasm32-browser " +
	"[--emit wat] [--opt] [-o file] <file|package>"

const browserBinaryUsage = "usage: kizu build --target wasm32-browser " +
	"--emit wasm -o file [--opt] <file|package>"

const browserESMUsage = "usage: kizu build --target wasm32-browser " +
	"--emit esm -o directory [--opt] <file|package>"

// usage prints the supported command line shape.
func usage() {
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu <parse|run|check|test> <file> [-- args...]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu test [--seed N] <file|package> [-- args...]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu test [--seed N] [--triple T] -o file <file|package>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu init [path]")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu fmt [--write] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu ir [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --emit-llvm [--opt] <file>")
	_, _ = fmt.Fprintln(os.Stderr, "usage: kizu build --target native [native-options] <file>")
	_, _ = fmt.Fprintln(os.Stderr, wasmWATUsage)
	_, _ = fmt.Fprintln(os.Stderr, wasmBinaryUsage)
	_, _ = fmt.Fprintln(os.Stderr, browserWATUsage)
	_, _ = fmt.Fprintln(os.Stderr, browserBinaryUsage)
	_, _ = fmt.Fprintln(os.Stderr, browserESMUsage)
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
