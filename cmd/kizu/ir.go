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
	module, err := lowerFile(path, opt)
	if err != nil {
		return err
	}
	_, _ = fmt.Println(ir.Dump(module))
	return nil
}
