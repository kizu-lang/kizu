// Package manifest parses the minimal kizu.toml shape the compiler accepts.
//
// It stands apart from package resolution so that anything needing to read a
// manifest -- including std loading, which package resolution depends on -- can
// do so without depending on the checker.
package manifest
