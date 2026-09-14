package main

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// TestHkdfExampleAgreesOutsideKizu computes, with Go's HMAC, the key and
// the derived bytes that examples/crypto_hkdf.kizu promises to print, the
// way RFC 5869 spells them: extract is one HMAC, and each expand block is
// the HMAC of the block before it, the info, and the block number.
func TestHkdfExampleAgreesOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/crypto_hkdf.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 3 {
		t.Fatalf("%s promises %d lines, want the key, the derived bytes, and the check", path, len(lines))
	}
	extract := hmac.New(sha256.New, []byte("salt"))
	extract.Write([]byte("a shared secret"))
	key := extract.Sum(nil)
	if got := hex.EncodeToString(key); got != lines[0] {
		t.Fatalf("Go's HKDF-Extract gives %s, the example promises %s", got, lines[0])
	}
	expand := hmac.New(sha256.New, key)
	expand.Write([]byte("kizu example key"))
	expand.Write([]byte{1})
	if got := hex.EncodeToString(expand.Sum(nil)[:16]); got != lines[1] {
		t.Fatalf("Go's HKDF-Expand gives %s, the example promises %s", got, lines[1])
	}
}

// TestAesGcmExampleAgreesOutsideKizu seals the message of
// examples/crypto_aes_gcm.kizu with Go's AES-GCM and compares the bytes
// the example promises to print. The bitsliced AES and the carry-less
// GHASH are Kizu source; an implementation Kizu did not write has to
// arrive at the same ciphertext and tag.
func TestAesGcmExampleAgreesOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/crypto_aes_gcm.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 3 {
		t.Fatalf("%s promises %d lines, want the sealed hex, the text, and the refusal", path, len(lines))
	}
	block, err := aes.NewCipher([]byte("an example key!!"))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nil, []byte("twelve bytes"), []byte("attack at dawn"), []byte("record 1"))
	if got := hex.EncodeToString(sealed); got != lines[0] {
		t.Fatalf("Go's AES-GCM seals to %s, the example promises %s", got, lines[0])
	}
}

// TestX25519ExampleAgreesOutsideKizu derives, with Go's crypto/ecdh, the
// public keys and the shared secret that examples/crypto_x25519.kizu
// promises to print. The ladder and the field arithmetic are Kizu
// source; an implementation Kizu did not write has to arrive at the
// same 32 bytes from the same private keys.
func TestX25519ExampleAgreesOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/crypto_x25519.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 5 {
		t.Fatalf("%s promises %d lines, want two public keys, the secret, and two verdicts",
			path, len(lines))
	}
	curve := ecdh.X25519()
	alice, err := curve.NewPrivateKey([]byte("alice's private key, 32 bytes..."))
	if err != nil {
		t.Fatal(err)
	}
	bob, err := curve.NewPrivateKey([]byte("bob's private key, also 32 bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(alice.PublicKey().Bytes()); got != lines[0] {
		t.Fatalf("Go derives Alice's public key %s, the example promises %s", got, lines[0])
	}
	if got := hex.EncodeToString(bob.PublicKey().Bytes()); got != lines[1] {
		t.Fatalf("Go derives Bob's public key %s, the example promises %s", got, lines[1])
	}
	shared, err := alice.ECDH(bob.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(shared); got != lines[2] {
		t.Fatalf("Go agrees on %s, the example promises %s", got, lines[2])
	}
}

// TestEcdsaP256VerifiesWhatGoSigns signs digests with fresh P-256 keys
// from Go's crypto/ecdsa and has a Kizu program verify each signature,
// then the same signature over another digest and with one byte of s
// changed. The field and point arithmetic are Kizu source; a signature
// an implementation Kizu did not write made has to pass, and a changed
// one has to fail.
func TestEcdsaP256VerifiesWhatGoSigns(t *testing.T) {
	var program strings.Builder
	program.WriteString(ecdsaProgramHead)
	var want []string
	for i := 0; i < 6; i++ {
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		public, err := private.PublicKey.ECDH()
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("message %d", i)))
		r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		signature := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
		other := sha256.Sum256([]byte(fmt.Sprintf("message %d changed", i)))
		changed := append([]byte(nil), signature...)
		changed[40] ^= 1
		key := hex.EncodeToString(public.Bytes())
		for _, c := range []struct {
			digest, signature []byte
			verdict           string
		}{
			{digest[:], signature, "valid"},
			{other[:], signature, "invalid"},
			{digest[:], changed, "invalid"},
		} {
			fmt.Fprintf(&program, "    try check(allocator, %q, %q, %q);\n",
				key, hex.EncodeToString(c.digest), hex.EncodeToString(c.signature))
			want = append(want, c.verdict)
		}
	}
	program.WriteString("    return;\n}\n")
	path := filepath.Join(t.TempDir(), "ecdsa_oracle.kizu")
	if err := os.WriteFile(path, []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := kizuCommand("run", path).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != strings.Join(want, "\n") {
		t.Fatalf("Kizu verified Go's signatures as:\n%s\nwant:\n%s",
			got, strings.Join(want, "\n"))
	}
}

// ecdsaProgramHead is the program TestEcdsaP256VerifiesWhatGoSigns
// completes with one `check` per signature.
const ecdsaProgramHead = `import std::crypto;
import std::mem;
import std::string;

fn bytes_of_hex(allocator: Allocator, hex: []u8) -> mem::Error!string::String {
    var out = string::new(allocator);
    errdefer out.deinit(allocator);
    var at = 0;
    while at + 1 < mem::len(hex) {
        let high = hex_value(hex[at]);
        let low = hex_value(hex[at + 1]);
        try out.append_byte(allocator, cast<u8>(high * 16 + low));
        at = at + 2;
    }
    return move out;
}

fn hex_value(byte: u8) -> i64 {
    let value = cast<i64>(byte);
    if value >= 97 {
        return value - 87;
    }
    return value - 48;
}

fn check(
    allocator: Allocator,
    key_hex: []u8,
    digest_hex: []u8,
    signature_hex: []u8
) -> mem::Error!void {
    let key = try bytes_of_hex(allocator, key_hex);
    defer key.deinit(allocator);
    let digest = try bytes_of_hex(allocator, digest_hex);
    defer digest.deinit(allocator);
    let signature = try bytes_of_hex(allocator, signature_hex);
    defer signature.deinit(allocator);
    let key_bytes = key.as_bytes();
    let digest_bytes = digest.as_bytes();
    let signature_bytes = signature.as_bytes();
    if crypto::ecdsa_p256_verify(key_bytes, digest_bytes, signature_bytes) {
        print("valid");
    } else {
        print("invalid");
    }
    return;
}

fn main() -> !void {
    let allocator = mem::page_allocator();
`

// TestSha512ExampleAgreesOutsideKizu computes, with Go's crypto, the two
// digests examples/crypto_sha512.kizu promises to print.
func TestSha512ExampleAgreesOutsideKizu(t *testing.T) {
	cases, err := conformance.Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const path = "examples/crypto_sha512.kizu"
	var promised string
	for _, tt := range cases {
		if tt.Path == path && tt.Stdout != nil {
			promised = *tt.Stdout
		}
	}
	lines := strings.Split(strings.TrimSpace(promised), "\n")
	if len(lines) != 2 {
		t.Fatalf("%s promises %d lines, want the SHA-512 and the SHA-384 digest", path, len(lines))
	}
	message := []byte("The quick brown fox jumps over the lazy dog")
	long := sha512.Sum512(message)
	if got := hex.EncodeToString(long[:]); got != lines[0] {
		t.Fatalf("Go's SHA-512 is %s, the example promises %s", got, lines[0])
	}
	short := sha512.Sum384(message)
	if got := hex.EncodeToString(short[:]); got != lines[1] {
		t.Fatalf("Go's SHA-384 is %s, the example promises %s", got, lines[1])
	}
}

// TestRsaVerifiesWhatGoSigns signs digests with keys Go generates, under
// PKCS #1 v1.5 and PSS over each of the three hashes, and has std::crypto
// verify them: the valid ones, one over another message, and one with a
// bit changed. The verdicts must be Go's.
func TestRsaVerifiesWhatGoSigns(t *testing.T) {
	var program strings.Builder
	program.WriteString(rsaProgramHead)
	var want []string
	hashes := []struct {
		name string
		hash crypto.Hash
	}{{"Sha256", crypto.SHA256}, {"Sha384", crypto.SHA384}, {"Sha512", crypto.SHA512}}
	for i, bits := range []int{2048, 3072} {
		private, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			t.Fatal(err)
		}
		modulus := hex.EncodeToString(private.N.Bytes())
		for _, h := range hashes {
			digest := digestOf(h.hash, fmt.Sprintf("message %d", i))
			other := digestOf(h.hash, fmt.Sprintf("message %d changed", i))
			pkcs1, err := rsa.SignPKCS1v15(rand.Reader, private, h.hash, digest)
			if err != nil {
				t.Fatal(err)
			}
			pss, err := rsa.SignPSS(rand.Reader, private, h.hash, digest,
				&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
			if err != nil {
				t.Fatal(err)
			}
			changed := append([]byte(nil), pss...)
			changed[len(changed)/2] ^= 1
			for _, c := range []struct {
				scheme    string
				digest    []byte
				signature []byte
				verdict   string
			}{
				{"pkcs1", digest, pkcs1, "valid"},
				{"pss", digest, pss, "valid"},
				{"pkcs1", other, pkcs1, "invalid"},
				{"pss", digest, changed, "invalid"},
				{"pss", digest, pkcs1, "invalid"},
			} {
				fmt.Fprintf(&program, "    try check(allocator, crypto::Hash::%s, %t, %q, %q, %q);\n",
					h.name, c.scheme == "pss", modulus,
					hex.EncodeToString(c.digest), hex.EncodeToString(c.signature))
				want = append(want, c.verdict)
			}
		}
	}
	program.WriteString("    return;\n}\n")
	path := filepath.Join(t.TempDir(), "rsa_oracle.kizu")
	if err := os.WriteFile(path, []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := kizuCommand("run", path).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != strings.Join(want, "\n") {
		t.Fatalf("Kizu verified Go's RSA signatures as:\n%s\nwant:\n%s",
			got, strings.Join(want, "\n"))
	}
}

// digestOf hashes a message under one of the hashes RSA signatures use.
func digestOf(hash crypto.Hash, message string) []byte {
	h := hash.New()
	h.Write([]byte(message))
	return h.Sum(nil)
}

// rsaProgramHead is the program TestRsaVerifiesWhatGoSigns completes with
// one `check` per signature; the exponent is the 65537 Go's keys have.
const rsaProgramHead = `import std::crypto;
import std::mem;
import std::string;

fn bytes_of_hex(allocator: Allocator, hex: []u8) -> mem::Error!string::String {
    var out = string::new(allocator);
    errdefer out.deinit(allocator);
    var at = 0;
    while at + 1 < mem::len(hex) {
        let high = hex_value(hex[at]);
        let low = hex_value(hex[at + 1]);
        try out.append_byte(allocator, cast<u8>(high * 16 + low));
        at = at + 2;
    }
    return move out;
}

fn hex_value(byte: u8) -> i64 {
    let value = cast<i64>(byte);
    if value >= 97 {
        return value - 87;
    }
    return value - 48;
}

fn check(
    allocator: Allocator,
    hash: crypto::Hash,
    pss: bool,
    modulus_hex: []u8,
    digest_hex: []u8,
    signature_hex: []u8
) -> mem::Error!void {
    let modulus = try bytes_of_hex(allocator, modulus_hex);
    defer modulus.deinit(allocator);
    let exponent = try bytes_of_hex(allocator, "010001");
    defer exponent.deinit(allocator);
    let digest = try bytes_of_hex(allocator, digest_hex);
    defer digest.deinit(allocator);
    let signature = try bytes_of_hex(allocator, signature_hex);
    defer signature.deinit(allocator);
    let n = modulus.as_bytes();
    let e = exponent.as_bytes();
    let digest_bytes = digest.as_bytes();
    let signature_bytes = signature.as_bytes();
    let valid = if pss {
        crypto::rsa_pss_verify(hash, n, e, digest_bytes, signature_bytes)
    } else {
        crypto::rsa_pkcs1_verify(hash, n, e, digest_bytes, signature_bytes)
    };
    if valid {
        print("valid");
    } else {
        print("invalid");
    }
    return;
}

fn main() -> !void {
    let allocator = mem::page_allocator();
`
