// Command gen-selfhost-version writes the version module the selfhost
// compiler prints for `kizu version`.
//
// Run it from the repository root before checking, testing, or building
// `compiler/`:
//
//	go run ./scripts/gen-selfhost-version
//
// The line names the revision, so it changes with every commit and the
// generated file is not checked in. It goes away with the port.
package main

import (
	"fmt"
	"os"

	"github.com/kizu-lang/kizu/internal/selfhost"
)

// main writes the module and reports what stopped it.
func main() {
	if err := selfhost.WriteVersionSource("."); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}
