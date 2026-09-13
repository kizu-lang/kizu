package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestX509VerifiesWhatGoIssues has Go's crypto/x509 issue a fresh chain
// -- a root, an intermediate, a leaf naming a host -- and a Kizu program
// verify it, then the same chain against an unrelated root, at a time
// before the leaf's validity, and for a host the leaf does not name.
// The DER reader and the checks are Kizu source; what another
// implementation writes has to read back and verify the way it meant.
func TestX509VerifiesWhatGoIssues(t *testing.T) {
	notBefore := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	root, rootKey := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test Root", Organization: []string{"Test"}},
		NotBefore:    notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil)
	intermediate, intermediateKey := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Intermediate", Organization: []string{"Test"}},
		NotBefore:    notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign,
	}, root, rootKey)
	leaf, _ := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "example.net"},
		NotBefore: notBefore.AddDate(1, 0, 0), NotAfter: notAfter, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"example.net", "*.example.net"},
	}, intermediate, intermediateKey)
	other, _ := issueCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "Other Root"},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil)

	var program strings.Builder
	program.WriteString(x509ProgramHead)
	within := notBefore.AddDate(5, 0, 0).Unix()
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
	var want []string
	for _, c := range cases {
		fmt.Fprintf(&program, "    try check(allocator, %q, %q, %q, %q, %d);\n",
			hex.EncodeToString(leaf), hex.EncodeToString(intermediate), hex.EncodeToString(c.roots),
			c.host, c.now)
		want = append(want, c.verdict)
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

// issueCertificate signs template under the parent's key, or under its
// own when there is no parent, and returns the DER and the new key.
func issueCertificate(
	t *testing.T,
	template *x509.Certificate,
	parentDER []byte,
	parentKey *ecdsa.PrivateKey,
) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
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
