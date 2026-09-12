package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/conformance"
)

// TestSha256ExampleAgreesOutsideKizu computes, with Go's crypto, the digest
// and the tag that examples/crypto_sha256.kizu promises to print.
// TestConformance holds the example to that promise, so the hex is what
// std::crypto writes; an implementation Kizu did not write has to give the
// same bytes for the same message, or a digest is not a name for it.
func TestSha256ExampleAgreesOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/crypto_sha256.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 3 {
		t.Fatalf("%s promises %d lines, want the digest, the tag, and the check", path, len(lines))
	}
	message := []byte("The quick brown fox jumps over the lazy dog")
	digest := sha256.Sum256(message)
	if got := hex.EncodeToString(digest[:]); got != lines[0] {
		t.Fatalf("Go's SHA-256 of the message is %s, the example promises %s", got, lines[0])
	}
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write(message)
	if got := hex.EncodeToString(mac.Sum(nil)); got != lines[1] {
		t.Fatalf("Go's HMAC-SHA-256 of the message is %s, the example promises %s", got, lines[1])
	}
}
