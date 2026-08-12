package main

import (
	"strings"
	"testing"
)

// TestSelfhostHostedPrintRuntimeHasSingleOwner keeps the print symbols defined
// exactly once, by the hosted C runtime. Both runtimes are linked together, so a
// second definition in the storage IR is either a duplicate-symbol link failure
// or, worse, a silent pick between two implementations.
func TestSelfhostHostedPrintRuntimeHasSingleOwner(t *testing.T) {
	hosted := readSelfhostFile(t, "../../selfhost/runtime/selfhost.hosted.c")
	storage := readSelfhostFile(t, "../../selfhost/runtime/selfhost.storage.ll")
	for _, symbol := range []string{
		"kizu_print_string",
		"kizu_print_int",
		"kizu_print_bool",
	} {
		if count := strings.Count(hosted, "void "+symbol+"("); count != 1 {
			t.Fatalf("hosted runtime definition count for %s = %d, want 1", symbol, count)
		}
		if strings.Contains(storage, "@"+symbol+"(") {
			t.Fatalf("storage runtime must not own hosted print symbol %s", symbol)
		}
	}
}
