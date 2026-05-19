package main

import "testing"

// TestSelfhostPackageSkeletonChecks keeps the source-owned selfhost layout valid.
func TestSelfhostPackageSkeletonChecks(t *testing.T) {
	runKizuOK(t, "check", "selfhost")
}
