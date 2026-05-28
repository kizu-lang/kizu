package interp

import (
	"bytes"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

// interpHotPathSource exercises the interpreter hot paths that dominate the
// selfhost backend gate: recursive calls, identifier resolution, arithmetic
// and logical binary operators, locals, and loop control. It is a fast proxy
// for the ~350s gate so optimizations can be iterated with `go test -bench`
// and confirmed afterwards with TestSelfhostBackendArtifactGate.
const interpHotPathSource = `fn fib(n: i64) -> i64 {
    if n < 2 { return n; }
    return fib(n - 1) + fib(n - 2);
}
fn classify(x: i64) -> i64 {
    if x > 10 and x < 1000 { return 1; }
    if x < 0 or x > 100000 { return 2; }
    return 0;
}
fn mix(a: i64, b: i64, c: i64) -> i64 {
    let d = a + b;
    let e = b + c;
    let f = d + e;
    let g = d * e;
    let h = f + g;
    return a + b + c + d + e + f + g + h;
}
fn main() {
    var acc = 0;
    var i = 0;
    while i < 4000 {
        let x = i * 3;
        let y = i + 7;
        let m = mix(x, y, i);
        let c = classify(m);
        acc = acc + m + c + fib(12);
        i = i + 1;
    }
    print(acc);
}`

// BenchmarkInterpHotPath measures interpreter throughput on a representative
// compute workload. Parsing happens once so the timed loop is pure evaluation.
func BenchmarkInterpHotPath(b *testing.B) {
	p := parser.New(lexer.New(interpHotPathSource))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		b.Fatalf("parse: %s", errs[0])
	}
	var out bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out.Reset()
		if err := New(&out).Run(program); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}
