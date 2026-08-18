package main

import (
	"fmt"
)

// checkFile runs every static gate `run` and `build` would pass the target
// through, by lowering it exactly as they do and discarding the module.
// `check: ok` is a promise the backend accepts the program, not just the
// checkers, so a lowering gap surfaces here instead of waiting for the first
// `run`, and the promise cannot drift from what `run` actually does.
func checkFile(path string) error {
	if _, err := lowerRunTarget(path); err != nil {
		return err
	}
	_, _ = fmt.Println("check: ok")
	return nil
}
