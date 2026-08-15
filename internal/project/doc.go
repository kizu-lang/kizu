// Package project reads a module graph and hands the rest of the compiler one
// program.
//
// modules.go parses kizu.toml and resolves source files into module paths.
// Reading those modules is then three jobs, one per file: load.go turns them
// into declarations and collects what each exports, qualify.go rewrites every
// name to its module path, and resolve.go answers what a written name refers to.
package project
