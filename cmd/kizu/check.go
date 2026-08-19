package main

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/llvm"
)

// checkFile runs every static gate `run` and `build` would pass the target
// through, by lowering it exactly as they do and discarding the module.
// `check: ok` is a promise the backend accepts the program, not just the
// checkers, so a lowering gap surfaces here instead of waiting for the first
// `run`, and the promise cannot drift from what `run` actually does.
//
// The dry-run reaches the LLVM text: emitting is where backend bugs live that
// the IR verifier cannot see (#1622), and the emitter verifies its own output,
// so the promise covers everything short of the link.
func checkFile(path string) error {
	module, err := lowerRunTarget(path)
	if err != nil {
		return err
	}
	if _, err := llvm.Emit(module); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}
