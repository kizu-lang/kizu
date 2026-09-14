package main

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTlsClientTalksToGoServer completes a TLS 1.3 handshake between a
// Kizu program using std::tls and Go's crypto/tls server, exchanges
// application data, and closes. The handshake, the key schedule, the
// record layer and the certificate check are Kizu source; a server Kizu
// did not write has to accept the ClientHello, verify the client's
// Finished, and read what it sends. The same server is then refused for
// a host its certificate does not name, and under a root that did not
// issue it; servers with a P-384 key and with an RSA key, which signs
// its CertificateVerify with RSA-PSS, are talked to as well.
func TestTlsClientTalksToGoServer(t *testing.T) {
	der, key := selfSignedServer(t, "localhost", 256)
	other, _ := selfSignedServer(t, "other.test", 256)
	p384DER, p384Key := selfSignedServer(t, "localhost", 384)
	rsaDER, rsaKey := selfSignedServer(t, "localhost", 2048)
	path := filepath.Join(t.TempDir(), "tls_client.kizu")
	if err := os.WriteFile(path, []byte(tlsClientProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		cert []byte
		key  crypto.PrivateKey
		root []byte
		host string
		want string
	}{
		{"echo", der, key, der, "localhost", "connected with AES-128-GCM\nHELLO OVER TLS\nclosed"},
		{"wrong host", der, key, der, "example.test", "NameMismatch"},
		{"wrong root", der, key, other, "localhost", "UnknownIssuer"},
		{"echo over P-384", p384DER, p384Key, p384DER, "localhost",
			"connected with AES-128-GCM\nHELLO OVER TLS\nclosed"},
		{"echo over RSA", rsaDER, rsaKey, rsaDER, "localhost",
			"connected with AES-128-GCM\nHELLO OVER TLS\nclosed"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			port, done := serveOnce(t, tt.cert, tt.key)
			out, _ := kizuCommand("run", path, "--",
				fmt.Sprint(port), hex.EncodeToString(tt.root), tt.host).CombinedOutput()
			<-done
			if got := strings.TrimSpace(string(out)); !strings.Contains(got, tt.want) {
				t.Fatalf("client printed:\n%s\nwant it to contain %q", got, tt.want)
			}
		})
	}
}

// selfSignedServer makes a certificate for `host`, self-signed and
// marked as a CA so it can be its own root, with a key of `keyBits` as
// `generateKey` reads it.
func selfSignedServer(t *testing.T, host string, keyBits int) ([]byte, crypto.Signer) {
	t.Helper()
	key, err := generateKey(keyBits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		DNSNames:              []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// serveOnce listens on a free port and, in the background, accepts one
// connection, completes the TLS handshake, and echoes what it reads in
// upper case. Whatever happens, `done` is closed when the server is.
func serveOnce(t *testing.T, der []byte, key crypto.PrivateKey) (int, chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS13,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer listener.Close()
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		conn := tls.Server(raw, config)
		defer conn.Close()
		if err := conn.Handshake(); err != nil {
			return
		}
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte(strings.ToUpper(string(buf[:n]))))
	}()
	return listener.Addr().(*net.TCPAddr).Port, done
}

// tlsClientProgram connects to 127.0.0.1 on the port in its first
// argument as the host in its third, trusting the root the second spells
// in hex, sends a line, prints the reply, and closes.
const tlsClientProgram = `import std::array;
import std::io;
import std::mem;
import std::process;
import std::string;
import std::tls;

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

fn main() -> !void {
    let io = io::blocking();
    let allocator = mem::page_allocator();
    let port = try process::arg(0);
    let root_hex = try process::arg(1);
    let host = try process::arg(2);
    var address = string::new(allocator);
    defer address.deinit(allocator);
    try address.append_bytes(allocator, "127.0.0.1:");
    try address.append_bytes(allocator, port);
    var roots = array::new<string::String>(allocator);
    defer roots.deinit(allocator);
    try roots.append(allocator, try bytes_of_hex(allocator, root_hex));
    let now = process::unix_millis() / 1000;
    let address_bytes = address.as_bytes();
    var client = try tls::connect(io, allocator, address_bytes, host, &roots, now);
    defer client.deinit(allocator);
    if client.cipher_suite() == tls::CipherSuite::Aes128GcmSha256 {
        print("connected with AES-128-GCM");
    } else {
        print("connected with ChaCha20-Poly1305");
    }
    try client.write(io, allocator, "hello over tls");
    var reply = string::new(allocator);
    defer reply.deinit(allocator);
    while true {
        let got = try client.read_into(io, allocator, &var reply, 4096);
        if got == 0 {
            break;
        }
    }
    let reply_bytes = reply.as_bytes();
    print(reply_bytes);
    try client.close(io, allocator);
    print("closed");
    return;
}
`
