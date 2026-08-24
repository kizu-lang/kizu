package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/native"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/wasm"
)

// stdErrorSets returns the numbers the runtime names its failures with. The
// runtime refers to every std error set whatever a program uses, so a build
// that cannot read them cannot say what it failed at.
func stdErrorSets() (map[string]map[string]int, error) {
	return project.StdErrorSets()
}

// linkModule emits one lowered module and returns an executable for it. The
// executable is kept rather than thrown away, so a program that has not changed
// since it was last run is not linked a second time.
func linkModule(module *ir.Module) (string, error) {
	llvmIR, err := llvm.Emit(module)
	if err != nil {
		return "", err
	}
	errorSets, err := stdErrorSets()
	if err != nil {
		return "", err
	}
	return native.Executable(native.Options{
		LLVMIR: llvmIR, ErrorSets: errorSets,
		LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang",
	})
}

// buildFile dispatches build subcommands.
func buildFile(args []string) error {
	if len(args) < 2 {
		usage()
		return fmt.Errorf("invalid build command")
	}
	switch args[0] {
	case "--emit-llvm":
		path, opt, err := parseOptFileArgs(args[1:])
		if err != nil {
			return err
		}
		return emitLLVMFile(path, opt)
	case "--target":
		return emitTargetFile(args[1], args[2:])
	default:
		usage()
		return fmt.Errorf("invalid build command")
	}
}

// lowerTarget lowers a package root the way build lowers it and any other
// target as one file. Every command that shows or links a lowered module goes
// through here, so `ir` and `build` are asked the same question about the same
// program instead of one of them refusing a directory.
func lowerTarget(path string, opt bool) (*ir.Module, error) {
	if isPackageRoot(path) {
		return lowerPackage(path, opt)
	}
	return lowerFile(path, opt)
}

// emitLLVMFile lowers a checked source file or package to LLVM IR text.
// The text is emitted fresh every time: a text cache keyed by the source
// would have to key on the compiler itself to stay honest (ADR-0126).
func emitLLVMFile(path string, opt bool) error {
	module, err := lowerTarget(path, opt)
	if err != nil {
		return err
	}
	output, err := llvm.Emit(module)
	if err != nil {
		return err
	}
	_, _ = fmt.Println(output)
	return nil
}

// emitTargetFile lowers a checked source file to the requested target.
func emitTargetFile(target string, args []string) error {
	if target == "native" {
		return emitNativeFile(args)
	}
	if target != "wasm32-wasi" {
		usage()
		return fmt.Errorf("invalid build target `%s`", target)
	}
	path, opt, err := parseOptFileArgs(args)
	if err != nil {
		return err
	}
	return emitWASMFile(path, opt)
}

// emitWASMFile lowers a checked source file to WASI WebAssembly text,
// emitted fresh every time like emitLLVMFile (ADR-0126).
func emitWASMFile(path string, opt bool) error {
	module, err := lowerFile(path, opt)
	if err != nil {
		return err
	}
	output, err := wasm.Emit(module)
	if err != nil {
		return err
	}
	_, _ = fmt.Println(output)
	return nil
}

// emitNativeFile lowers and links a source file into a native executable.
func emitNativeFile(args []string) error {
	options, err := parseNativeBuildArgs(args)
	if err != nil {
		return err
	}
	// Native --opt controls clang's native optimization. The typed-SSA optimizer
	// remains scoped to `build --emit-llvm --opt` until it is package-scale safe.
	const nativeIROpt = false
	var module *ir.Module
	if isPackageRoot(options.Path) {
		module, err = lowerPackage(options.Path, nativeIROpt)
	} else {
		module, err = lowerFile(options.Path, nativeIROpt)
	}
	if err != nil {
		return err
	}
	if options.Opt {
		ir.KeepReachableFunctions(module, "main")
	}
	llvmIR, err := llvm.Emit(module)
	if err != nil {
		return err
	}
	errorSets, err := stdErrorSets()
	if err != nil {
		return err
	}
	if err := native.Build(native.Options{
		LLVMIR: llvmIR, Output: options.Output, ErrorSets: errorSets,
		Triple: options.Triple,
		CPU:    options.CPU, ABI: options.ABI, LibC: options.LibC,
		Runtime: options.Runtime, Emit: options.Emit, Linker: options.Linker,
		Opt: options.Opt,
	}); err != nil {
		return err
	}
	_, _ = fmt.Println(options.Output)
	return nil
}

// parseOptFileArgs parses an optional --opt flag followed by one file path.
// A lone `--opt` names the flag, not a file, so it is an argument error rather
// than a path the command tries to open.
func parseOptFileArgs(args []string) (string, bool, error) {
	if len(args) == 1 && args[0] != "--opt" {
		return args[0], false, nil
	}
	if len(args) == 2 && args[0] == "--opt" {
		return args[1], true, nil
	}
	usage()
	return "", false, fmt.Errorf("invalid command arguments")
}

// nativeBuildArgs stores Zig-style native build options accepted by the CLI.
type nativeBuildArgs struct {
	Path    string
	Output  string
	Triple  string
	CPU     string
	ABI     string
	LibC    string
	Runtime string
	Emit    string
	Linker  string
	Opt     bool
}

// parseNativeBuildArgs parses native build flags and derives defaults.
func parseNativeBuildArgs(args []string) (nativeBuildArgs, error) {
	options := nativeBuildArgs{LibC: "on", Runtime: "hosted", Emit: "exe", Linker: "clang"}
	for i := 0; i < len(args); i++ {
		var err error
		switch args[i] {
		case "--opt":
			options.Opt = true
		case "-o":
			i, options.Output, err = nextNativeArg(args, i, "-o")
		case "--triple":
			i, options.Triple, err = nextNativeArg(args, i, "--triple")
		case "--cpu":
			i, options.CPU, err = nextNativeArg(args, i, "--cpu")
		case "--abi":
			i, options.ABI, err = nextNativeArg(args, i, "--abi")
		case "--libc":
			i, options.LibC, err = nextNativeArg(args, i, "--libc")
		case "--runtime":
			i, options.Runtime, err = nextNativeArg(args, i, "--runtime")
		case "--emit":
			i, options.Emit, err = nextNativeArg(args, i, "--emit")
		case "--linker":
			i, options.Linker, err = nextNativeArg(args, i, "--linker")
		default:
			if options.Path != "" {
				usage()
				return nativeBuildArgs{}, fmt.Errorf("invalid command arguments")
			}
			options.Path = args[i]
		}
		if err != nil {
			usage()
			return nativeBuildArgs{}, err
		}
	}
	if options.Path == "" {
		usage()
		return nativeBuildArgs{}, fmt.Errorf("missing source file")
	}
	if options.Output == "" {
		options.Output = defaultNativeOutput(options.Path, options.Emit)
	}
	return options, nil
}

// nextNativeArg returns the value following a flag.
func nextNativeArg(args []string, index int, flag string) (int, string, error) {
	if index+1 >= len(args) {
		return index, "", fmt.Errorf("missing value after %s", flag)
	}
	return index + 1, args[index+1], nil
}

// defaultNativeOutput maps a source path to the default target/native artifact.
func defaultNativeOutput(path string, emit string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if emit == "obj" {
		name += ".o"
	}
	if emit == "llvm" {
		name += ".ll"
	}
	return filepath.Join("target", "native", name)
}
