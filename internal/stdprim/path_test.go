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
}
