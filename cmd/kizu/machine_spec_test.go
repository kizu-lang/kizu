package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

// machineGenerators are the Kizu programs that write the std::machine tables of
// std::tls into the specifications, with the files each one writes.
var machineGenerators = []struct {
	program string
	args    []string
}{
	{"scripts/machine-lean/main.kizu", []string{"spec/Tls.lean", "client", "server"}},
	{"scripts/machine-quint/main.kizu", []string{"spec/tls_client.qnt", "spec/tls_server.qnt"}},
}

// TestTlsSpecTablesComeFromStdTls checks the generated blocks of spec/Tls.lean
// and the generated Quint modules are what std::tls does now. Neither Lean nor
// Quint is needed here: a change the proofs would have to answer for shows as
// a stale block, and `just spec-table` brings it up to date for `lean --run`
// to judge. The generators compare with -check, so the text this test holds
// the files to is the text `just spec-table` writes.
func TestTlsSpecTablesComeFromStdTls(t *testing.T) {
	root := filepath.Join("..", "..")
	command := kizuCommand("run", filepath.Join("spec", "tls_machine.kizu"))
	command.Dir = root
	var tables, stderr bytes.Buffer
	command.Stdout = &tables
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run spec/tls_machine.kizu: %v\n%s", err, stderr.String())
	}
	for _, generator := range machineGenerators {
		args := append([]string{"run", generator.program, "--", "-check"}, generator.args...)
		check := kizuCommand(args...)
		check.Dir = root
		check.Stdin = bytes.NewReader(tables.Bytes())
		if out, err := check.CombinedOutput(); err != nil {
			t.Errorf("%s -check: %v\n%s", generator.program, err, out)
		}
	}
}

// TestMachineGeneratorsPassTheirTests runs the test blocks of the generators:
// how they read the tables, and each way a table and a file can disagree.
func TestMachineGeneratorsPassTheirTests(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, generator := range machineGenerators {
		test := kizuCommand("test", generator.program)
		test.Dir = root
		if out, err := test.CombinedOutput(); err != nil {
			t.Fatalf("kizu test %s: %v\n%s", generator.program, err, out)
		}
	}
}
