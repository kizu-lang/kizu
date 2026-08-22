// Package project reads packages and hands the rest of the compiler one program.
//
// modules.go parses kizu.toml and resolves source files into module paths, and
// answers which of them a package keeps to itself. std.go finds the standard
// library and reads the part of it a program imports: std is a package with a
// manifest and a source tree, so it comes through here rather than through a
// loader of its own.
//
// Reading a directory module is then three jobs: load.go parses each file and
// merges declarations while retaining file-local imports, qualify.go rewrites
// every name to its module path, and resolve.go answers what a written name
// refers to.
package project
