package main

import (
	"fmt"
	"os"
	"strings"

	kizufmt "github.com/kizu-lang/kizu/internal/fmt"
)

// fmtCommand prints or rewrites a Kizu source file in canonical form.
//
// usage: kizu fmt [--write] <file>
//
//	--write: rewrite the file in-place.
func fmtCommand(args []string) error {
	write := false
	if len(args) == 2 && isFmtWriteFlag(args[0]) {
		write = true
		args = args[1:]
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		usage()
		return fmt.Errorf("fmt takes an optional --write and one target")
	}
	source, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	// The formatter is token based and will happily lay out source that does not
	// parse, so validate first: rewriting a file whose syntax is broken would
	// bake the breakage into a "formatted" result.
	if _, errs, err := parsePath(args[0]); err != nil {
		return err
	} else if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	formatted := kizufmt.Format(string(source))
	if !write {
		_, _ = fmt.Print(formatted)
		return nil
	}
	return os.WriteFile(args[0], []byte(formatted), 0o644)
}

// isFmtWriteFlag reports whether an argument selects in-place formatting.
func isFmtWriteFlag(arg string) bool {
	return arg == "--write" || arg == "-w"
}
