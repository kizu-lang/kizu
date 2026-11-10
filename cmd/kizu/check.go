package main

import (
	"fmt"
)

// checkFile runs the target through the same lowering `run` and `build` use
// and discards the module. `check: ok` promises that the frontend checkers and
// the IR verifier accept the program; lowerRunTarget verifies the whole module
// before returning, so a lowering gap still surfaces here instead of waiting
// for the first `run`, and the promise cannot drift from what `run` lowers.
//
// The promise stops at the IR verifier. Emitting LLVM text and the emitter's
// own self-check are covered by `run`, `build`, `test` and conformance, which
// is where backend bugs the verifier cannot see (#1622) are caught; spending
// check time on text nobody reads does not buy a stronger edit-loop answer.
// For the same reason check does not prune to the entry closure: pruning only
// decided what the emitter would see, and the verifier has already run over
// every lowered function.
func checkFile(path string) error {
	if _, err := lowerRunTarget(path); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}
