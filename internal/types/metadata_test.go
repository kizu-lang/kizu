package types

import "testing"

// TestCheckerMetadataStartsEmpty pins the one-check registry boundary.
func TestCheckerMetadataStartsEmpty(t *testing.T) {
	metadata := newCheckerMetadata()
	if len(metadata.functions) != 0 || len(metadata.structs) != 0 ||
		len(metadata.enums) != 0 || len(metadata.errorSets) != 0 ||
		len(metadata.unions) != 0 || len(metadata.contracts) != 0 ||
		len(metadata.impls) != 0 || len(metadata.declaredTypes) != 0 {
		t.Fatal("new checker metadata is not empty")
	}
}
