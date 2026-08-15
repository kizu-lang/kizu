// Package conformance reads the case each Kizu example declares about itself.
//
// A case is the run of comment lines a program ends with. It comes last so
// that reading the file starts with the program:
//
//	fn main() {
//	    print("hello, kizu");
//	}
//
//	// run
//	// features: fn print void
//	// output:
//	// hello, kizu
//
// The first line is the directive: `run`, `check`, or `test` for a program that
// has to succeed, and `run-fails`, `check-fails`, `test-fails`, or `parse-fails`
// for one whose failing is the point. Anything after the directive word is
// passed to the CLI after the path.
//
// The rest is `key: value` lines. `features:` lists the tags the README backend
// table groups by and is required. `pending:` names a gap, and a case that
// carries one is checked for still failing, so the line cannot outlive it.
// `error:` is the substring a `-fails` case must print. `output:` is what a
// program prints, and the lines after it are that output, so it goes last.
package conformance
