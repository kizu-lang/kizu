package main

import (
	"fmt"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ir"
)

// testFile runs Kizu test blocks and reports a minimal test result. A seed
// given on the command line is what std::testing::seed answers, so a failed
// randomized run replays (SPEC §14.5).
func testFile(path string, args []string, seed *int64) error {
	module, err := lowerTestTarget(path)
	if err != nil {
		return err
	}
	if err := addTestMain(module, seed); err != nil {
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

// lowerTestTarget includes package-only test files while loose files remain explicit.
func lowerTestTarget(path string) (*ir.Module, error) {
	if isPackageRoot(path) {
		return lowerTestPackage(path, false)
	}
	return lowerFile(path, false)
}

// addTestMain builds an entry that runs every lowered test block in order,
// after handing the runtime the seed the command line chose, if any. It
// writes IR by hand, so it ends by verifying what it produced, the same way
// `internal/ir` verifies what it returns.
func addTestMain(module *ir.Module, seed *int64) error {
	names := ir.TestFunctionNames(module)
	if len(names) == 0 {
		return fmt.Errorf("test error: no tests found")
	}
	// Loose-file lowering may already contain a native main; tests replace it.
	kept := module.Functions[:0]
	for _, fn := range module.Functions {
		if fn.Name != "main" {
			kept = append(kept, fn)
		}
	}
	module.Functions = kept
	// A test body returns `!void`, so the entry tries each result: the first
	// failing test returns its message instead of letting the run report ok.
	instrs := make([]*ir.Instr, 0, 2*len(names)+3)
	if seed != nil {
		value := ir.Value{Name: "%seed", Type: "i64"}
		instrs = append(instrs,
			&ir.Instr{Result: value, Op: "const", Immediate: strconv.FormatInt(*seed, 10)},
			&ir.Instr{
				Result: ir.Value{Name: "%seed.set", Type: "void"},
				Op:     "call.std::internal::builtin::test_seed_set",
				Args:   []ir.Value{value},
			})
	}
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
