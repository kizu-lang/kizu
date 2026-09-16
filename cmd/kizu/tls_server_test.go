package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTlsServerTalksToGoClient runs a Kizu TLS server over each kind of
// key -- P-256, P-384, RSA -- and has Go's crypto/tls connect to it,
// verify its self-signed certificate as a root, and echo a line; a
// client that trusts another root refuses the certificate, and the
// server reports the failure.
func TestTlsServerTalksToGoClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tls_server.kizu")
	if err := os.WriteFile(path, []byte(tlsServerProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bits := range []int{256, 384, 2048} {
		t.Run(fmt.Sprint(bits), func(t *testing.T) { echoThroughKizuServer(t, path, bits) })
	}
	t.Run("Kizu client", func(t *testing.T) {
		// Both ends Kizu: the client program of TestTlsClientTalksToGoServer
		// against this server.
		der, key := selfSignedServer(t, "localhost", 256)
		pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		client := filepath.Join(t.TempDir(), "tls_client.kizu")
		if err := os.WriteFile(client, []byte(tlsClientProgram), 0o644); err != nil {
			t.Fatal(err)
		}
		server, port := startKizuServer(t, path, der, pkcs8)
		out, _ := kizuCommand("run", client, "--",
			fmt.Sprint(port), hex.EncodeToString(der), "localhost").CombinedOutput()
		served := server.wait()
		if got := strings.TrimSpace(string(out)); !strings.Contains(got, "HELLO OVER TLS\nclosed") {
			t.Fatalf("client printed:\n%s\nserver printed:\n%s", got, served)
		}
		if !strings.Contains(served, "served localhost with AES-128-GCM\nhello over tls\nclosed") {
			t.Fatalf("server printed:\n%s", served)
		}
	})
	t.Run("wrong root", func(t *testing.T) {
		der, key := selfSignedServer(t, "localhost", 256)
		other, _ := selfSignedServer(t, "localhost", 256)
		pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		server, port := startKizuServer(t, path, der, pkcs8)
		roots := x509.NewCertPool()
		roots.AddCert(mustParse(t, other))
		_, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{
			RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13,
		})
		if err == nil {
			t.Fatal("Go accepted a certificate under the wrong root")
		}
		// Go sends its alert and closes at once; whether the server reads
		// the alert or meets the reset first is the kernel's timing.
		out := server.wait()
		if !strings.Contains(out, "Alert") && !strings.Contains(out, "ConnectionReset") {
			t.Fatalf("server printed:\n%s\nwant the client's alert or the reset after it", out)
		}
	})
}

// echoThroughKizuServer starts the server with a key of `bits`, connects
// Go's client trusting the server's certificate, and checks the echo.
func echoThroughKizuServer(t *testing.T, path string, bits int) {
	t.Helper()
	der, key := selfSignedServer(t, "localhost", bits)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	server, port := startKizuServer(t, path, der, pkcs8)
	roots := x509.NewCertPool()
	roots.AddCert(mustParse(t, der))
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{
		RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13,
	})
	if err != nil {
		t.Fatalf("Go could not connect: %v\n%s", err, server.output())
	}
	if _, err := conn.Write([]byte("hello over tls")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 64)
	n, err := conn.Read(reply)
	if err != nil {
		t.Fatalf("Go could not read: %v\n%s", err, server.output())
	}
	conn.Close()
	out := server.wait()
	if got := string(reply[:n]); got != "HELLO OVER TLS" {
		t.Fatalf("Go read %q, want the line in upper case\n%s", got, out)
	}
	if !strings.Contains(out, "served localhost with AES-128-GCM\nhello over tls\nclosed") {
		t.Fatalf("server printed:\n%s", out)
	}
}

// A kizuServer is a Kizu TLS server process started by `startKizuServer`.
type kizuServer struct {
	cmd    *exec.Cmd
	stdout *bufio.Reader
	lines  strings.Builder
}

// startKizuServer runs the server program with the certificate, the
// PKCS #8 key, and any further arguments, reads the port it prints, and
// returns the process.
func startKizuServer(
	t *testing.T, path string, der, pkcs8 []byte, more ...string,
) (*kizuServer, int) {
	t.Helper()
	args := []string{"run", path, "--", hex.EncodeToString(der), hex.EncodeToString(pkcs8)}
	args = append(args, more...)
	cmd := kizuCommand(args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	server := &kizuServer{cmd: cmd, stdout: bufio.NewReader(pipe)}
	line, err := server.stdout.ReadString('\n')
	if err != nil {
		t.Fatalf("the server printed no port: %v\n%s", err, server.output())
	}
	port, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("the server printed %q, want a port", line)
	}
	return server, port
}

// wait reads the rest of what the server prints and waits for it to exit.
func (s *kizuServer) wait() string {
	rest, _ := s.stdout.ReadString(0)
	s.lines.WriteString(rest)
	_ = s.cmd.Wait()
	return s.lines.String()
}

// output is what the server has printed so far, for a failure message.
func (s *kizuServer) output() string {
	_ = s.cmd.Process.Kill()
	return s.wait()
}

// mustParse parses a DER certificate or fails the test.
func mustParse(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// tlsServerProgram listens on a free port, prints it, accepts one
// connection with the certificate and key its arguments spell in hex,
// prints the host the client asked for and the suite, echoes one line
// in upper case, and closes.
const tlsServerProgram = `import std::array;
import std::crypto::x509;
import std::fmt;
import std::io;
import std::mem;
import std::net;
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

fn upper(bytes: &var string::String) -> void {
    let view = bytes.as_mut_bytes();
    for 0..mem::len(view) |index| {
        if view[index] >= 97 and view[index] <= 122 {
            view[index] = view[index] - 32;
        }
    }
    return;
}

fn failure_name(failure: tls::Failure) -> []u8 {
    return match failure {
        Alert => "Alert",
        tls::Error::Closed => "Closed",
        UnexpectedMessage => "UnexpectedMessage",
        tls::Error::Unsupported => "Unsupported",
        tls::Error::BadSignature => "BadSignature",
        ConnectionReset => "ConnectionReset",
        net::Error::ReadFailed => "ReadFailed",
        WriteFailed => "WriteFailed",
        net::Error::Closed => "net Closed",
        _ => "other",
    };
}

fn serve(
    io: Io,
    allocator: Allocator,
    stream: net::TcpStream,
    chain: &array::Array<string::String>,
    key: &x509::PrivateKey
) -> tls::Failure!bool {
    var connection = try tls::accept(io, allocator, move stream, chain, key);
    defer connection.deinit(allocator);
    var line = string::new(allocator);
    defer line.deinit(allocator);
    try line.append_bytes(allocator, "served ");
    let name = connection.server_name();
    try line.append_bytes(allocator, name);
    if connection.cipher_suite() == tls::CipherSuite::Aes128GcmSha256 {
        try line.append_bytes(allocator, " with AES-128-GCM");
    } else {
        try line.append_bytes(allocator, " with ChaCha20-Poly1305");
    }
    let line_bytes = line.as_bytes();
    print(line_bytes);
    var request = string::new(allocator);
    defer request.deinit(allocator);
    let got = try connection.read_into(io, allocator, &var request, 4096);
    let _ = got;
    let request_bytes = request.as_bytes();
    print(request_bytes);
    upper(&var request);
    let reply = request.as_bytes();
    try connection.write(io, allocator, reply);
    try connection.close(io, allocator);
    print("closed");
    return true;
}

fn main() -> !void {
    let io = io::blocking();
    let allocator = mem::page_allocator();
    let der = try bytes_of_hex(allocator, try process::arg(0));
    defer der.deinit(allocator);
    let key_der = try bytes_of_hex(allocator, try process::arg(1));
    defer key_der.deinit(allocator);
    var chain = array::new<string::String>(allocator);
    defer chain.deinit(allocator);
    let der_bytes = der.as_bytes();
    let copy = try string::from_bytes(allocator, der_bytes);
    try chain.append(allocator, move copy);
    let key_bytes = key_der.as_bytes();
    let key = try x509::parse_private_key(key_bytes);
    var listener = try net::tcp_listen(io, "127.0.0.1:0");
    defer listener.deinit();
    // The port goes out unbuffered, so a test can read it before the
    // accept blocks.
    var port = string::new(allocator);
    defer port.deinit(allocator);
    try fmt::append_i64(allocator, &var port, try listener.local_port());
    let port_bytes = port.as_bytes();
    try io::write_stdout_line(io, &port_bytes);
    let stream = try listener.accept(io);
    if serve(io, allocator, move stream, &chain, &key) |done| {
        let _ = done;
    } else |err| {
        print(failure_name(err));
    }
    return;
}
`
