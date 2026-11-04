// Command machine-lean writes std::machine tables into the generated blocks of a
// Lean specification.
//
// Run it from the repository root with the program that prints the tables:
//
//	kizu run spec/tls_machine.kizu | go run ./scripts/machine-lean spec/Tls.lean client server
//
// The names give the blocks the documents on stdin belong to, in order.
package main

import (
	"fmt"
	"os"

	"github.com/kizu-lang/kizu/internal/machinelean"
)

// main reports a failure on stderr and in the exit status.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "machine-lean:", err)
		os.Exit(1)
	}
}

// run rewrites the file named first from the tables on stdin.
func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: machine-lean <spec.lean> <block>... < tables.json")
	}
	tables, err := machinelean.ReadTables(os.Stdin)
	if err != nil {
		return err
	}
	source, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	updated, err := machinelean.Update(string(source), args[1:], tables)
	if err != nil {
		return err
	}
	return os.WriteFile(args[0], []byte(updated), 0o644)
}
