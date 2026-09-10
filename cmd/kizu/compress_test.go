package main

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/conformance"
)

// TestGzipExampleReadsOutsideKizu hands the member that
// examples/compress_gzip.kizu promises to print to Go's gzip reader.
// TestConformance holds the example to that promise, so the hex is what
// std::compress writes; a reader Kizu did not write has to open it too, or
// the writer and its own reader merely agree with each other.
func TestGzipExampleReadsOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/compress_gzip.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 3 {
		t.Fatalf("%s promises %d lines, want the sizes, the hex, and the check", path, len(lines))
	}
	member, err := hex.DecodeString(lines[1])
	if err != nil {
		t.Fatalf("%s: the second line is not hex: %v", path, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(member))
	if err != nil {
		t.Fatalf("Go's gzip reader refused the member: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Go's gzip reader stopped inside the member: %v", err)
	}
	want := strings.Repeat("a stream of blocks, a stream of bytes\n", 3)
	if string(got) != want {
		t.Fatalf("the member read as %q, want %q", got, want)
	}
}
