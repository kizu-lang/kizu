package main

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/version"
)

// versionCommand prints what this binary is.
func versionCommand(args []string) error {
	if len(args) != 0 {
		usage()
		return fmt.Errorf("version takes no arguments")
	}
	_, _ = fmt.Println(version.String())
	return nil
}
