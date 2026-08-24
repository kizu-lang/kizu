package buildcache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// TestSharedSHA256Vectors keeps the vector corpus in compiler/tests/sha256
// equal to what crypto/sha256 computes. The selfhost sha256 module digests
// the same file, so one fixture gates both implementations: a digest either
// side gets wrong cannot agree with what this test keeps honest.
func TestSharedSHA256Vectors(t *testing.T) {
	data, err := os.ReadFile("../../compiler/tests/sha256/vectors.txt")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	checked := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		digest, inputHex, found := strings.Cut(line, ":")
		if !found {
			t.Fatalf("vector line without separator: %q", line)
		}
		input, err := hex.DecodeString(inputHex)
		if err != nil {
			t.Fatalf("vector input %q: %v", inputHex, err)
		}
		sum := sha256.Sum256(input)
		if got := hex.EncodeToString(sum[:]); got != digest {
			t.Errorf("digest for input %q is %s, file says %s", inputHex, got, digest)
		}
		checked++
	}
	if checked < 15 {
		t.Errorf("checked %d vectors, the corpus should hold at least 15", checked)
	}
}
