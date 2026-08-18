package main

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// testFile runs Kizu test blocks and reports a minimal test result.
func testFile(path string, args []string) error {
	module, err := lowerRunTarget(path)
	if err != nil {
		return err
	}
	if err := addTestMain(module); err != nil {
		return err
	}
	exe, err := linkModule(module)
	if err != nil {
		return err
	}
	if err := executeBuilt(exe, args); err != nil {
		return err
	}
	_, _ = fmt.Println("test: ok")
	return nil
}

// addTestMain builds an entry that runs every lowered test block in order. It
// writes IR by hand, so it ends by verifying what it produced, the same way
// `internal/ir` verifies what it returns.
func addTestMain(module *ir.Module) error {
	names := ir.TestFunctionNames(module)
	if len(names) == 0 {
		return fmt.Errorf("test error: no tests found")
	}
	// A package lowering supplies its own entry; a test run replaces it.
	kept := module.Functions[:0]
	for _, fn := range module.Functions {
		if fn.Name != "main" {
			kept = append(kept, fn)
		}
	}
	module.Functions = kept
	// A test body returns `!void`, so the entry tries each result: the first
	// failing test returns its message instead of letting the run report ok.
	instrs := make([]*ir.Instr, 0, 2*len(names)+1)
	for i, name := range names {
		called := ir.Value{Name: fmt.Sprintf("%%test.%d", i), Type: "!void"}
		instrs = append(instrs,
			&ir.Instr{Result: called, Op: "call." + name},
			&ir.Instr{
				Result: ir.Value{Name: fmt.Sprintf("%%try.%d", i), Type: "void"},
				Op:     "error.try",
				Args:   []ir.Value{called},
			})
	}
	ok := ir.Value{Name: "%test.ok", Type: "!void"}
	instrs = append(instrs, &ir.Instr{Result: ok, Op: "error.ok"})
	module.Functions = append(module.Functions, &ir.Function{
		Name:   "main",
		Return: "!void",
		Blocks: []*ir.Block{{
			Name:       "entry",
			Instrs:     instrs,
			Terminator: ir.Terminator{Op: "return", Value: ok},
		}},
	})
	return ir.Verify(module)
}
