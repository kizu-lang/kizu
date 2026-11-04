package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kizu-lang/kizu/internal/machinelean"
	"github.com/kizu-lang/kizu/internal/machinequint"
)

// TestTlsSpecTablesComeFromStdTls checks the generated blocks of spec/Tls.lean
// and the generated Quint modules are what std::tls does now. Neither Lean nor
// Quint is needed here: a change the proofs would
// have to answer for shows as a stale block, and `just spec-table` brings it up
// to date for `lean --run` to judge.
func TestTlsSpecTablesComeFromStdTls(t *testing.T) {
	root := filepath.Join("..", "..")
	command := kizuCommand("run", filepath.Join("spec", "tls_machine.kizu"))
	command.Dir = root
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run spec/tls_machine.kizu: %v\n%s", err, stderr.String())
	}
	tables, err := machinelean.ReadTables(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "spec", "Tls.lean")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := machinelean.Update(string(source), []string{"client", "server"}, tables)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(source) {
		t.Fatal("spec/Tls.lean is out of step with std::tls; run `just spec-table` and `just spec-trace`")
	}
	for index, name := range []string{"tls_client", "tls_server"} {
		want, err := machinequint.Render(name, tables[index])
		if err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(filepath.Join(root, "spec", name+".qnt"))
		if err != nil {
			t.Fatal(err)
		}
		if want != string(written) {
			t.Fatalf("spec/%s.qnt is out of step with std::tls; "+
				"run `just spec-table`", name)
		}
	}
}
