package main

import (
	"errors"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
)

// TestEveryExampleLowersToVerifiedIR runs the corpus through the pipeline `kizu
// run` uses. ir.Lower ends in ir.Verify, so this reports a rule broken by a
// shape only one example has, in that example's name, rather than leaving it
// for whichever backend reads the module first.
//
// The conformance manifest already lowers the examples it runs or builds.
// This adds the ones it only checks, which lower without any backend reading
// them.
func TestEveryExampleLowersToVerifiedIR(t *testing.T) {
	for _, path := range kizuSourcePaths(t, "../../examples") {
		// The negative examples are supposed to fail, and the two pending
		// features fail in lowering. Only a Verify failure is this test's
		// question, which is what the sentinel separates.
		if _, err := lowerFile(path, false); errors.Is(err, ir.ErrVerify) {
			t.Errorf("%s: %v", path, err)
		}
	}
}
