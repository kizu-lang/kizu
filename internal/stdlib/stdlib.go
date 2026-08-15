// Package stdlib says where the standard library is and what it is called.
//
// Reading it is somebody else's job: std is a package with a manifest and a
// source tree, so `internal/project` loads it the same way it loads a program's
// own package. What is left here is the one thing that cannot come from a
// manifest -- which directory on this machine the library tree is.
package stdlib

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Root is the namespace the standard library lives under. A user package may
// not be named this, so a path starting here always means std.
const Root = "std"

// LibDirEnv names the environment variable that overrides where the library
// tree is. A caller that already knows the path passes it with SetLibDir; this
// is for the ones that do not run the CLI, and for a development shell.
const LibDirEnv = "KIZU_LIB_DIR"

// libDir is the library tree this process reads std from, once decided.
var libDir struct {
	sync.Once
	path string
	err  error
}

// SetLibDir points this process at a library tree, overriding what it would
// otherwise find. It has to be called before anything reads std.
func SetLibDir(path string) {
	libDir.Do(func() {})
	libDir.path, libDir.err = path, nil
}

// LibDir returns the library tree std is read from. There is one rule: the
// caller says where it is, or it sits next to the running binary. Nothing is
// searched for, and the current directory is never consulted -- a program has
// to mean the same thing whatever directory it is compiled from.
func LibDir() (string, error) {
	libDir.Do(func() { libDir.path, libDir.err = resolveLibDir() })
	return libDir.path, libDir.err
}

// resolveLibDir decides the library tree from the environment or the binary.
func resolveLibDir() (string, error) {
	if fromEnv := os.Getenv(LibDirEnv); fromEnv != "" {
		return fromEnv, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		resolved = executable
	}
	// <prefix>/bin/kizu -> <prefix>/lib/kizu
	return filepath.Join(filepath.Dir(filepath.Dir(resolved)), "lib", "kizu"), nil
}

// FindLibFile returns the path of one file inside the library tree.
func FindLibFile(name string) (string, error) {
	dir, err := LibDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf(
			"open %s: no such file or directory"+
				"\nhelp: the library tree is `%s`;"+
				" set %s or pass --lib-dir to point somewhere else",
			path, dir, LibDirEnv,
		)
	}
	return path, nil
}
