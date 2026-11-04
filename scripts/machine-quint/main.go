// Command machine-quint writes std::machine tables as Quint modules.
//
// Run it from the repository root, with the program that prints the tables on
// stdin and the modules to write as arguments:
//
//	just spec-table
//
// Each file takes one document from stdin, in order, and is named for its module.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/machinelean"
	"github.com/kizu-lang/kizu/internal/machinequint"
)

// main reports a failure on stderr and in the exit status.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "machine-quint:", err)
		os.Exit(1)
	}
}

// run writes one module per file from the tables on stdin.
func run(paths []string) error {
	tables, err := machinelean.ReadTables(os.Stdin)
	if err != nil {
		return err
	}
	if len(tables) != len(paths) {
		return fmt.Errorf("%d files for %d tables", len(paths), len(tables))
	}
	for index, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		text, err := machinequint.Render(name, tables[index])
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}
