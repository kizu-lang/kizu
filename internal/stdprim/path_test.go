package stdprim

import "testing"

// TestPathPrimitives checks std path primitives stay host-independent.
func TestPathPrimitives(t *testing.T) {
	if got := PathJoin("examples", "hello.kizu"); got != "examples/hello.kizu" {
		t.Fatalf("PathJoin got %q", got)
	}
	if got := PathClean("examples/./fixtures/../hello.kizu"); got != "examples/hello.kizu" {
		t.Fatalf("PathClean got %q", got)
	}
	if got := PathBase("examples/hello.kizu"); got != "hello.kizu" {
		t.Fatalf("PathBase got %q", got)
	}
	if got := PathDir("examples/hello.kizu"); got != "examples" {
		t.Fatalf("PathDir got %q", got)
	}
	if got := PathExt("examples/hello.kizu"); got != ".kizu" {
		t.Fatalf("PathExt got %q", got)
	}
}
