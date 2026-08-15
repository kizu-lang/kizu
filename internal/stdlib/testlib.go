package stdlib

import (
	"path/filepath"
	"runtime"
)

// RepoLibDir returns the library tree of the repository this package was built
// from. Tests use it to point at std the way a user does -- through the one
// override -- rather than by giving the compiler a second way to find std.
//
// It is only correct when the Go sources are present, which is true when tests
// run and false for an installed binary. Nothing outside a test may call it.
func RepoLibDir() string {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	// internal/stdlib/testlib.go -> <repo>/lib/kizu
	repo := filepath.Dir(filepath.Dir(filepath.Dir(sourceFile)))
	return filepath.Join(repo, "lib", "kizu")
}
