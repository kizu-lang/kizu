package main

import (
	"strings"
	"testing"
)

// TestVersionCommandNamesTheBinary checks the binary answers `kizu version`
// with a line that starts by naming itself, whatever it was built from.
func TestVersionCommandNamesTheBinary(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		out, err := kizuCommand(arg).CombinedOutput()
		if err != nil {
			t.Fatalf("kizu %s: %v\n%s", arg, err, out)
		}
		if !strings.HasPrefix(string(out), "kizu ") {
			t.Fatalf("kizu %s output %q does not name the binary", arg, out)
		}
	}
}

// TestVersionCommandCanRunWithoutTarget checks main accepts the version
// command shape.
func TestVersionCommandCanRunWithoutTarget(t *testing.T) {
	if !commandAllowsNoTarget("version") {
		t.Fatal("version should be accepted without a path argument")
	}
	if !commandAllowsNoTarget("--version") {
		t.Fatal("--version should be accepted without a path argument")
	}
}

// TestVersionCommandRejectsArguments keeps the command shape closed: a target
// after `version` is a mistake, not something to ignore.
func TestVersionCommandRejectsArguments(t *testing.T) {
	if err := versionCommand([]string{"extra"}); err == nil {
		t.Fatal("version with an argument should fail")
	}
}
