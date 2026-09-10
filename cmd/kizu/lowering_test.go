package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
)

// TestEveryExampleLowersToTheSameVerifiedIR runs the corpus through the
// pipeline `kizu run` uses, twice. ir.Lower ends in ir.Verify, so a rule
// broken by a shape only one example has is reported in that example's name
// rather than left for whichever backend reads the module first. The second
// lowering has to come out the same: lowering walks the names in scope to
// decide where phi nodes go, and each phi takes the next SSA number as it is
// made, so a walk in a different order is a different module. Reading a Go
// map back gives a different order every run, which is what this used to be:
// the same source lowered to a different module each time, and with a cold
// build cache to different LLVM.
//
// The conformance manifest already lowers the examples it runs or builds.
// This adds the ones it only checks, which lower without any backend reading
// them, and the comparison, which nothing else makes.
func TestEveryExampleLowersToTheSameVerifiedIR(t *testing.T) {
	for _, path := range kizuSourcePaths(t, "../../examples") {
		module, err := lowerFile(path, false)
		if errors.Is(err, ir.ErrVerify) {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if err != nil {
			// A negative example, which never reaches a module, or a
			// pending feature, which fails in lowering.
			continue
		}
		first := ir.Dump(module)
		again, err := lowerFile(path, false)
		if err != nil {
			t.Fatalf("%s: lowered once but not again: %v", path, err)
		}
		if second := ir.Dump(again); second != first {
			t.Errorf("%s: lowering is not deterministic\n%s",
				path, firstDifference(first, second))
		}
	}
}

// firstDifference reports the first line two dumps disagree on, so a failure
// names the instruction instead of printing two whole modules.
func firstDifference(first string, second string) string {
	firstLines := strings.Split(first, "\n")
	secondLines := strings.Split(second, "\n")
	for index := range firstLines {
		if index >= len(secondLines) {
			return fmt.Sprintf("line %d: second dump ends early", index+1)
		}
		if firstLines[index] != secondLines[index] {
			return fmt.Sprintf("line %d:\n  first:  %s\n  second: %s",
				index+1, firstLines[index], secondLines[index])
		}
	}
	return fmt.Sprintf("line %d: first dump ends early", len(firstLines)+1)
}
