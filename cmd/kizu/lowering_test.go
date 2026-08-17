package main

import (
	"errors"
	"fmt"
	"strings"
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
		// The negative examples are supposed to fail, and the pending
		// features fail in lowering. Only a Verify failure is this test's
		// question, which is what the sentinel separates.
		if _, err := lowerFile(path, false); errors.Is(err, ir.ErrVerify) {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// TestLoweringIsDeterministic lowers the corpus more than once and compares
// what came out. Lowering walks the names in scope to decide where phi nodes
// go, and each phi takes the next SSA number as it is made, so a walk in a
// different order is a different module. Reading a Go map back gives a
// different order every run, which is what this used to be: the same source
// lowered to a different module each time, and with a cold build cache to
// different LLVM.
func TestLoweringIsDeterministic(t *testing.T) {
	for _, path := range kizuSourcePaths(t, "../../examples") {
		module, err := lowerFile(path, false)
		if err != nil {
			// A negative example, which never reaches a module.
			continue
		}
		first := ir.Dump(module)
		for pass := 0; pass < 2; pass++ {
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
