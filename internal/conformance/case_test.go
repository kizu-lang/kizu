package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlockPathFindsTestOnlyPackage checks package promises can live with tests.
func TestBlockPathFindsTestOnlyPackage(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "tests", "behavior", "src")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "main_test.kizu"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := blockPath(root, "tests/behavior")
	if err != nil {
		t.Fatalf("block path failed: %v", err)
	}
	if got != "tests/behavior/src/main_test.kizu" {
		t.Fatalf("got %q, want test-only package entry", got)
	}
}

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
		`// env: KIZU_TEST_ENV=env-ok`,
		`// dir: examples/fixtures`,
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
	if strings.Join(entry.Env, " ") != "KIZU_TEST_ENV=env-ok" {
		t.Fatalf("got env %q", entry.Env)
	}
	if strings.Join(entry.Dirs, " ") != "examples/fixtures" {
		t.Fatalf("got dirs %q", entry.Dirs)
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
		{"malformed env", []string{"run", "features: fn", "env: NO_VALUE", "output:"},
			"must be `NAME=value`"},
		{"duplicate env", []string{
			"run", "features: fn", "env: NAME=one", "env: NAME=two", "output:",
		}, "repeats `NAME`"},
		{"empty dir", []string{"run", "features: fn", "dir:", "output:"},
			"must name a repository-relative directory"},
		{"absolute dir", []string{"run", "features: fn", "dir: /tmp", "output:"},
			"must name a repository-relative directory"},
		{"escaping dir", []string{"run", "features: fn", "dir: ../fixtures", "output:"},
			"must name a repository-relative directory"},
		{"mapped dir", []string{"run", "features: fn", "dir: host::guest", "output:"},
			"must name a repository-relative directory"},
		{"duplicate dir", []string{
			"run", "features: fn", "dir: fixtures", "dir: fixtures", "output:",
		}, "repeats `fixtures`"},
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
