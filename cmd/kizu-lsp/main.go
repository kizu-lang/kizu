// Command kizu-lsp starts the Kizu language server over stdio.
package main

import (
	"fmt"
	"os"

	"github.com/kizu-lang/kizu/internal/lsp"
)

// main runs the stdio language server process.
func main() {
	if err := lsp.Run(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "kizu-lsp: "+err.Error())
		os.Exit(1)
	}
}
