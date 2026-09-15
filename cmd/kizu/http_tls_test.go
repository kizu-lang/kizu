package main

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
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
