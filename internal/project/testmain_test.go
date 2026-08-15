package project

import (
	"os"
	"testing"

	"github.com/kizu-lang/kizu/internal/stdlib"
)

// TestMain points std at the repository's library tree. Tests use the same
// override a user would, so there is one rule for where std is rather than a
// second one that only holds while the Go sources are next to the binary.
func TestMain(m *testing.M) {
	stdlib.SetLibDir(stdlib.RepoLibDir())
	os.Exit(m.Run())
}
