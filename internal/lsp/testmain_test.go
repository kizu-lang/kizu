package lsp

import (
	"os"
	"testing"

	"github.com/kizu-lang/kizu/internal/stdlib"
)

// TestMain points std at the repository's library tree, the same way the other
// packages do: through the one override a user would use.
func TestMain(m *testing.M) {
	stdlib.SetLibDir(stdlib.RepoLibDir())
	os.Exit(m.Run())
}
