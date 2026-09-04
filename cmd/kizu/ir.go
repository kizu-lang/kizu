package main

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// irCommand parses options and dumps typed SSA IR.
func irCommand(args []string) error {
	path, opt, err := parseOptFileArgs(args)
	if err != nil {
		return err
	}
	module, err := lowerTarget(path, opt)
	if err != nil {
		return err
	}
	// The dump shows what a build would keep: std::fmt is loaded for every
	// program (SPEC §14.1), and its unreached bodies are not the program's.
	ir.KeepTargetReachableFunctions(module, "", "main")
	_, _ = fmt.Println(ir.Dump(module))
	return nil
}
