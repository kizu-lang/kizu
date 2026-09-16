package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHttpClientFetchesOverTls has Go's net/http serve one answer over TLS
// and a Kizu program fetch it with std::http: the URL says `https`, the
// program hands over the root the server's certificate is, and the status
// and body come back through std::tls underneath. The same fetch under a
// root that did not issue the certificate, for a host the certificate does
// not name, and with no roots at all is refused, each by its own name.
func TestHttpClientFetchesOverTls(t *testing.T) {
	der, key := selfSignedServer(t, "localhost", 256)
	other, _ := selfSignedServer(t, "other.test", 256)
	greet := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "hello over tls from %s", r.URL.Path)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(greet))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS13,
	}
	server.StartTLS()
	defer server.Close()
	port := server.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]
	path := filepath.Join(t.TempDir(), "http_tls.kizu")
	if err := os.WriteFile(path, []byte(httpTlsProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		root []byte
		url  string
		want string
	}{
		{"fetch", der, "https://localhost:" + port + "/greeting", "200\nhello over tls from /greeting"},
		{"wrong root", other, "https://localhost:" + port + "/", "UnknownIssuer"},
		{"wrong host", der, "https://127.0.0.1:" + port + "/", "NameMismatch"},
		{"no roots", nil, "https://localhost:" + port + "/", "NoTrustedRoots"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := kizuCommand("run", path, "--", hex.EncodeToString(tt.root), tt.url).CombinedOutput()
			if got := strings.TrimSpace(string(out)); got != tt.want {
				t.Fatalf("client printed:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// httpTlsProgram fetches the URL in its second argument, trusting the root
// the first spells in hex (none when empty), and prints the status and body
// or the name of the failure.
const httpTlsProgram = `import std::array;
import std::http;
import std::io;
import std::mem;
import std::process;
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

fn failure_name(failure: http::Failure) -> []u8 {
    return match failure {
        NoTrustedRoots => "NoTrustedRoots",
        UnknownIssuer => "UnknownIssuer",
        NameMismatch => "NameMismatch",
        Expired => "Expired",
        _ => "other",
    };
}

fn fetch(io: Io, allocator: Allocator, root_hex: []u8, url: []u8) -> http::Failure!bool {
    var roots = array::new<string::String>(allocator);
    defer roots.deinit(allocator);
    if mem::len(root_hex) > 0 {
        let root = try bytes_of_hex(allocator, root_hex);
        try roots.append(allocator, move root);
    }
    var headers = http::headers_new(allocator);
    defer headers.deinit(allocator);
    var response = try http::fetch_with(
        io, allocator, "GET", url, &headers, "", &roots, http::default_limits(), 1 << 20);
    defer response.deinit(allocator);
    print(response.status);
    let body = response.body.as_bytes();
    print(body);
    return true;
}

fn main() -> !void {
    let io = io::blocking();
    let allocator = mem::page_allocator();
    let root_hex = try process::arg(0);
    let url = try process::arg(1);
    if fetch(io, allocator, root_hex, url) |done| {
        let _ = done;
    } else |err| {
        print(failure_name(err));
    }
    return;
}
`

// TestHttpServerServesOverTls runs a Kizu https server -- once answering
// with the blocking `accept`, once with the `first` / `next` loop -- and
// has Go's net/http fetch from it over TLS 1.3, trusting the server's
// self-signed certificate as a root: two requests down one kept-alive
// connection and one more down a fresh one. A client trusting another
// root is refused during the handshake.
func TestHttpServerServesOverTls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "https_server.kizu")
	if err := os.WriteFile(path, []byte(httpsServerProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"accept", "loop"} {
		t.Run(mode, func(t *testing.T) {
			der, key := selfSignedServer(t, "localhost", 256)
			other, _ := selfSignedServer(t, "localhost", 256)
			pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			server, port := startKizuServer(t, path, der, pkcs8, mode, "3")
			client := httpsClient(t, der)
			for _, route := range []string{"/one", "/two"} {
				if got := fetchText(t, client, port, route); got != "hello over https from "+route {
					t.Fatalf("Go read %q for %s\n%s", got, route, server.output())
				}
			}
			client.CloseIdleConnections()
			if got := fetchText(t, client, port, "/three"); got != "hello over https from /three" {
				t.Fatalf("Go read %q for a fresh connection\n%s", got, server.output())
			}
			stranger := httpsClient(t, other)
			if _, err := stranger.Get(fmt.Sprintf("https://localhost:%d/", port)); err == nil {
				t.Fatalf("Go accepted the certificate under the wrong root\n%s", server.output())
			}
			out := server.wait()
			if !strings.Contains(out, "served /one\nserved /two\nserved /three\n") {
				t.Fatalf("server printed:\n%s", out)
			}
		})
	}
}

// httpsClient is an HTTP client that trusts `root` and speaks TLS 1.3.
func httpsClient(t *testing.T, root []byte) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(mustParse(t, root))
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
	}}
}

// fetchText GETs a route from the server on `port` and returns the body.
func fetchText(t *testing.T, client *http.Client, port int, route string) string {
	t.Helper()
	response, err := client.Get(fmt.Sprintf("https://localhost:%d%s", port, route))
	if err != nil {
		t.Fatalf("Go could not fetch %s: %v", route, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("Go got status %d for %s", response.StatusCode, route)
	}
	return string(body)
}

// httpsServerProgram listens for https on a free port with the certificate
// and PKCS #8 key its first two arguments spell in hex, prints the port
// unbuffered, and answers as many requests as its fourth argument says,
// each with a line naming the route -- with the blocking `accept` when
// its third argument is `accept`, and with `first` / `next` when it is
// `loop`. A connection that fails its handshake is not one of them.
const httpsServerProgram = `import std::array;
import std::fmt;
import std::http;
import std::io;
import std::mem;
import std::process;
import std::string;

error Failure = http::Failure or process::Error or io::Error;

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

fn append_target(
    allocator: Allocator,
    exchange: &http::Exchange,
    out: &var string::String
) -> mem::Error!void {
    let target = exchange.request.target.as_bytes();
    try out.append_bytes(allocator, target);
    return;
}

fn answer(io: Io, allocator: Allocator, exchange: &var http::Exchange) -> Failure!void {
    var text = string::new(allocator);
    defer text.deinit(allocator);
    try text.append_bytes(allocator, "hello over https from ");
    try append_target(allocator, exchange, &var text);
    var line = string::new(allocator);
    defer line.deinit(allocator);
    try line.append_bytes(allocator, "served ");
    try append_target(allocator, exchange, &var line);
    let text_bytes = text.as_bytes();
    try exchange.respond_text(io, allocator, 200, "text/plain", text_bytes);
    let line_bytes = line.as_bytes();
    print(line_bytes);
    return;
}

fn serve_accepting(
    io: Io,
    allocator: Allocator,
    server: &var http::Server,
    count: i64
) -> Failure!void {
    var served = 0;
    while served < count {
        if server.accept(io, allocator, 1 << 16) |taken| {
            var exchange = move taken;
            defer exchange.deinit(allocator);
            try answer(io, allocator, &var exchange);
            served = served + 1;
        } else |err| {
            let _ = err;
        }
    }
    return;
}

fn serve_looping(
    io: Io,
    allocator: Allocator,
    server: &var http::Server,
    count: i64
) -> Failure!void {
    var current = try server.first(io, allocator, 1 << 16);
    var served = 1;
    while served < count {
        current = try turn(io, allocator, server, move current);
        served = served + 1;
    }
    try last(io, allocator, move current);
    return;
}

// turn answers the exchange it owns and hands it back for the next one;
// owning it here is what lets an errdefer cover the answering.
fn turn(
    io: Io,
    allocator: Allocator,
    server: &var http::Server,
    taken: http::Exchange
) -> Failure!http::Exchange {
    var current = move taken;
    errdefer current.deinit(allocator);
    try answer(io, allocator, &var current);
    let following = try server.next(io, allocator, move current, 1 << 16);
    return move following;
}

fn last(io: Io, allocator: Allocator, taken: http::Exchange) -> Failure!void {
    var current = move taken;
    defer current.deinit(allocator);
    try answer(io, allocator, &var current);
    return;
}

fn main() -> Failure!void {
    let io = io::blocking();
    let allocator = mem::page_allocator();
    let der = try bytes_of_hex(allocator, try process::arg(0));
    defer der.deinit(allocator);
    let key = try bytes_of_hex(allocator, try process::arg(1));
    defer key.deinit(allocator);
    let mode = try process::arg(2);
    let count_text = try process::arg(3);
    let count = cast<i64>(count_text[0]) - 48;
    var chain = array::new<string::String>(allocator);
    defer chain.deinit(allocator);
    let der_bytes = der.as_bytes();
    let copy = try string::from_bytes(allocator, der_bytes);
    try chain.append(allocator, move copy);
    let key_bytes = key.as_bytes();
    var server = try http::listen_secure(
        io, allocator, "127.0.0.1:0", &chain, key_bytes, http::default_limits());
    defer server.deinit(allocator);
    var port = string::new(allocator);
    defer port.deinit(allocator);
    try fmt::append_i64(allocator, &var port, try server.local_port());
    let port_bytes = port.as_bytes();
    try io::write_stdout_line(io, &port_bytes);
    if mem::equal_bytes(mode, "accept") {
        try serve_accepting(io, allocator, &var server, count);
    } else {
        try serve_looping(io, allocator, &var server, count);
    }
    return;
}
`
