package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestX509VerifiesWhatGoIssues has Go's crypto/x509 issue fresh chains
// -- a root, an intermediate, a leaf naming a host -- and a Kizu program
// verify them, then the same chain against an unrelated root, at a time
// before the leaf's validity, and for a host the leaf does not name.
// One chain is P-256 throughout; one has P-384 keys on the root and the
// intermediate, signing with SHA-384; one has RSA keys there, signing
// with SHA-256, SHA-384 and SHA-512; all under a P-256 leaf. The DER
// reader and the checks are Kizu source; what another implementation
// writes has to read back and verify the way it meant.
func TestX509VerifiesWhatGoIssues(t *testing.T) {
	notBefore := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	other, _ := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "Other Root"},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil, 256)

	var program strings.Builder
	program.WriteString(x509ProgramHead)
	within := notBefore.AddDate(5, 0, 0).Unix()
	var want []string
	for _, chain := range []testChain{
		{256, 256, x509.ECDSAWithSHA256, x509.ECDSAWithSHA256},
		{384, 384, x509.ECDSAWithSHA384, x509.ECDSAWithSHA384},
		{2048, 3072, x509.SHA384WithRSA, x509.SHA512WithRSA},
	} {
		root, intermediate, leaf := issueTestChain(t, notBefore, notAfter, chain)
		cases := []struct {
			roots   []byte
			host    string
			now     int64
			verdict string
		}{
			{root, "example.net", within, "verified"},
			{root, "api.example.net", within, "verified"},
			{root, "a.b.example.net", within, "name mismatch"},
			{root, "example.net", notBefore.AddDate(0, 6, 0).Unix(), "expired"},
			{other, "example.net", within, "unknown issuer"},
		}
		for _, c := range cases {
			fmt.Fprintf(&program, "    try check(allocator, %q, %q, %q, %q, %d);\n",
				hex.EncodeToString(leaf), hex.EncodeToString(intermediate),
				hex.EncodeToString(c.roots), c.host, c.now)
			want = append(want, c.verdict)
		}
	}
	program.WriteString("    return;\n}\n")
	path := filepath.Join(t.TempDir(), "x509_oracle.kizu")
	if err := os.WriteFile(path, []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := kizuCommand("run", path).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != strings.Join(want, "\n") {
		t.Fatalf("Kizu judged Go's chain as:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

// A testChain says what keys and signatures a chain is issued with: the
// key size of the root and the intermediate, as `issueCertificate`
// reads it, and the algorithm each of the intermediate and the leaf is
// signed with.
type testChain struct {
	rootBits, intermediateBits           int
	intermediateAlgorithm, leafAlgorithm x509.SignatureAlgorithm
}

// issueTestChain issues a root, an intermediate under it, and a leaf
// for example.net under that, as `chain` says, valid from a year after
// notBefore.
func issueTestChain(
	t *testing.T,
	notBefore, notAfter time.Time,
	chain testChain,
) ([]byte, []byte, []byte) {
	t.Helper()
	root, rootKey := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test Root", Organization: []string{"Test"}},
		NotBefore:    notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil, chain.rootBits)
	intermediate, intermediateKey := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Intermediate", Organization: []string{"Test"}},
		NotBefore:    notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage:           x509.KeyUsageCertSign,
		SignatureAlgorithm: chain.intermediateAlgorithm,
	}, root, rootKey, chain.intermediateBits)
	leaf, _ := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "example.net"},
		NotBefore: notBefore.AddDate(1, 0, 0), NotAfter: notAfter, BasicConstraintsValid: true,
		KeyUsage:           x509.KeyUsageDigitalSignature,
		ExtKeyUsage:        []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:           []string{"example.net", "*.example.net"},
		SignatureAlgorithm: chain.leafAlgorithm,
	}, intermediate, intermediateKey, 256)
	return root, intermediate, leaf
}

// generateKey makes a P-256 key for `keyBits` 256, a P-384 key for 384,
// and an RSA key of that many bits otherwise.
func generateKey(keyBits int) (crypto.Signer, error) {
	switch keyBits {
	case 256:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case 384:
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	default:
		return rsa.GenerateKey(rand.Reader, keyBits)
	}
}

// issueCertificate signs template under the parent's key, or under its
// own when there is no parent, and returns the DER and the new key:
// P-256 for `keyBits` 256, P-384 for 384, and RSA of that many bits
// otherwise.
func issueCertificate(
	t *testing.T,
	template *x509.Certificate,
	parentDER []byte,
	parentKey crypto.Signer,
	keyBits int,
) ([]byte, crypto.Signer) {
	t.Helper()
	key, err := generateKey(keyBits)
	if err != nil {
		t.Fatal(err)
	}
	parent := template
	signer := key
	if parentDER != nil {
		parent, err = x509.ParseCertificate(parentDER)
		if err != nil {
			t.Fatal(err)
		}
		signer = parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, key.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// x509ProgramHead is the program TestX509VerifiesWhatGoIssues completes
// with one `check` per case.
const x509ProgramHead = `import std::array;
import std::crypto::x509;
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
    leaf_hex: []u8,
    intermediate_hex: []u8,
    root_hex: []u8,
    host: []u8,
    now: i64
) -> mem::Error!void {
    var chain = array::new<string::String>(allocator);
    defer chain.deinit(allocator);
    try chain.append(allocator, try bytes_of_hex(allocator, leaf_hex));
    try chain.append(allocator, try bytes_of_hex(allocator, intermediate_hex));
    var roots = array::new<string::String>(allocator);
    defer roots.deinit(allocator);
    try roots.append(allocator, try bytes_of_hex(allocator, root_hex));
    if x509::verify_chain(&chain, &roots, host, now) {
        print("verified");
    } else |err| {
        print(match err {
            Malformed => "malformed",
            Unsupported => "unsupported",
            Expired => "expired",
            NameMismatch => "name mismatch",
            BadSignature => "bad signature",
            UnknownIssuer => "unknown issuer",
        });
    }
    return;
}

fn main() -> !void {
    let allocator = mem::page_allocator();
`

// TestX509SignsWhatGoVerifies hands Kizu private keys Go generated, in
// each shape a key file takes -- PKCS #8, SEC 1, PKCS #1, and one of
// them as PEM -- and has it sign a digest the way a CertificateVerify
// is signed; Go then verifies each signature under the public key.
func TestX509SignsWhatGoVerifies(t *testing.T) {
	var program strings.Builder
	program.WriteString(x509SignProgramHead)
	var checks []signatureCheck
	for n, c := range []struct {
		bits int
		hash crypto.Hash
	}{{256, crypto.SHA256}, {384, crypto.SHA384}, {2048, crypto.SHA256}, {2048, crypto.SHA384}} {
		signer, err := generateKey(c.bits)
		if err != nil {
			t.Fatal(err)
		}
		digest := digestOf(c.hash, fmt.Sprintf("message %d", c.bits))
		for i, shape := range keyShapes(t, signer) {
			name := fmt.Sprintf("key_%d_%d", n, i)
			if shape.pem {
				// A Kizu literal carries no escapes, so PEM's lines go
				// in as a multi-line literal.
				fmt.Fprintf(&program, "    let %s =\n", name)
				for _, line := range strings.Split(strings.TrimSpace(shape.text), "\n") {
					fmt.Fprintf(&program, "        \\\\%s\n", line)
				}
				program.WriteString("    ;\n")
			} else {
				fmt.Fprintf(&program, "    let %s = %q;\n", name, shape.text)
			}
			fmt.Fprintf(&program, "    try sign(allocator, %s, %t, crypto::Hash::%s, %q);\n",
				name, shape.pem, hashName(c.hash), hex.EncodeToString(digest))
			public := signer.Public()
			hash := c.hash
			checks = append(checks, func(signature []byte) error {
				switch key := public.(type) {
				case *ecdsa.PublicKey:
					if !ecdsa.VerifyASN1(key, digest, signature) {
						return fmt.Errorf("Go rejects the ECDSA signature")
					}
					return nil
				case *rsa.PublicKey:
					return rsa.VerifyPSS(key, hash, digest, signature,
						&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
				}
				return fmt.Errorf("unexpected key type %T", public)
			})
		}
	}
	program.WriteString("    return;\n}\n")
	path := filepath.Join(t.TempDir(), "x509_sign_oracle.kizu")
	if err := os.WriteFile(path, []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := kizuCommand("run", path).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(checks) {
		t.Fatalf("Kizu printed %d signatures, want %d:\n%s", len(lines), len(checks), out)
	}
	for i, line := range lines {
		signature, err := hex.DecodeString(line)
		if err != nil {
			t.Fatalf("signature %d is not hex: %v", i, err)
		}
		if err := checks[i](signature); err != nil {
			t.Errorf("signature %d: %v", i, err)
		}
	}
}

// A keyShape is one encoding of a private key: DER as hex, or PEM text.
type keyShape struct {
	text string
	pem  bool
}

// keyShapes encodes a key every way a key file might: PKCS #8, the
// algorithm's own bare form, and PKCS #8 wrapped in PEM.
func keyShapes(t *testing.T, signer crypto.Signer) []keyShape {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		t.Fatal(err)
	}
	var bare []byte
	switch key := signer.(type) {
	case *ecdsa.PrivateKey:
		if bare, err = x509.MarshalECPrivateKey(key); err != nil {
			t.Fatal(err)
		}
	case *rsa.PrivateKey:
		bare = x509.MarshalPKCS1PrivateKey(key)
	}
	text := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	return []keyShape{
		{hex.EncodeToString(pkcs8), false},
		{hex.EncodeToString(bare), false},
		{string(text), true},
	}
}

// hashName is the std::crypto::Hash member for a Go hash.
func hashName(hash crypto.Hash) string {
	switch hash {
	case crypto.SHA384:
		return "Sha384"
	case crypto.SHA512:
		return "Sha512"
	}
	return "Sha256"
}

// x509SignProgramHead is the program TestX509SignsWhatGoVerifies
// completes with one `sign` per key shape; each parses the key, signs
// the digest, and prints the signature as hex. The salt is the digest
// of the digest, since the test only needs some salt as long as the hash.
const x509SignProgramHead = `import std::crypto;
import std::crypto::x509;
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

fn print_hex(allocator: Allocator, bytes: []u8) -> mem::Error!void {
    var out = string::new(allocator);
    defer out.deinit(allocator);
    let digits = "0123456789abcdef";
    for 0..mem::len(bytes) |index| {
        try out.append_byte(allocator, digits[cast<i64>(bytes[index] >> 4)]);
        try out.append_byte(allocator, digits[cast<i64>(bytes[index] & 15)]);
    }
    let text = out.as_bytes();
    print(text);
    return;
}

fn sign(
    allocator: Allocator,
    key_text: []u8,
    pem: bool,
    hash: crypto::Hash,
    digest_hex: []u8
) -> x509::Failure!void {
    let der = if pem {
        try x509::decode_pem(allocator, key_text, "PRIVATE KEY")
    } else {
        try bytes_of_hex(allocator, key_text)
    };
    defer der.deinit(allocator);
    let digest = try bytes_of_hex(allocator, digest_hex);
    defer digest.deinit(allocator);
    let der_bytes = der.as_bytes();
    let key = try x509::parse_private_key(der_bytes);
    let digest_bytes = digest.as_bytes();
    var salt = [64]u8{};
    let salt_view = salt.as_mut_bytes();
    crypto::digest_of(hash, digest_bytes, salt_view);
    let salt_bytes = salt_view[0..crypto::digest_length(hash)];
    let signature = try x509::sign(allocator, &key, hash, digest_bytes, salt_bytes);
    defer signature.deinit(allocator);
    let made = signature.as_bytes();
    try print_hex(allocator, made);
    return;
}

fn main() -> !void {
    let allocator = mem::page_allocator();
`
