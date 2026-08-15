package conformance

import (
	"strings"
	"testing"
)

// TestBlockReadsWhatTheProgramDeclares checks the trailing comment block is
// read as the case, output included.
func TestBlockReadsWhatTheProgramDeclares(t *testing.T) {
	source := strings.Join([]string{
		`fn main() {`,
		`    print("hello, kizu");`,
		`}`,
		``,
		`// run -- input.kizu`,
		`// features: fn print void`,
		`// output:`,
		`// hello, kizu`,
		`//`,
		`// bye`,
	}, "\n")
	lines, err := block(source)
	if err != nil {
		t.Fatalf("block failed: %v", err)
	}
	entry, err := parse("examples/hello.kizu", lines)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if entry.Command != "run" || entry.MustFail {
		t.Fatalf("got command %q mustFail %v", entry.Command, entry.MustFail)
	}
	if strings.Join(entry.Args, " ") != "-- input.kizu" {
		t.Fatalf("got args %q", entry.Args)
	}
	if strings.Join(entry.Features, " ") != "fn print void" {
		t.Fatalf("got features %q", entry.Features)
	}
	// A blank line and a line that looks like a key are both output: nothing
	// follows `output:`, so the program's own text cannot be read as a key.
	if entry.Stdout == nil {
		t.Fatal("run case declared no output")
	}
	if want := "hello, kizu\n\nbye\n"; *entry.Stdout != want {
		t.Fatalf("got stdout %q, want %q", *entry.Stdout, want)
	}
}

// TestBlockRejectsUnsayableCases checks a program that does not say enough, or
// says something its directive cannot mean, is an error rather than a case that
// quietly checks less than it looks like it does.
func TestBlockRejectsUnsayableCases(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"unknown directive", []string{"walk", "features: fn"}, "unknown directive"},
		{"no features", []string{"check"}, "`features:` must not be empty"},
		{"run without output", []string{"run", "features: fn"}, "must declare `output:`"},
		{"failure without error", []string{"run-fails", "features: fn"}, "must declare `error:`"},
		{"error on a passing case", []string{
			"run", "features: fn", "error: boom", "output:",
		}, "belongs to a `-fails` case"},
		{"output on a check case", []string{"check", "features: fn", "output:"}, "has no `output:`"},
		{"unknown key", []string{"check", "features: fn", "colour: red"}, "unknown key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse("examples/x.kizu", tt.lines)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
	if _, err := block("fn main() {}\n"); err == nil {
		t.Fatal("a program with no case block must be an error")
	}
}
