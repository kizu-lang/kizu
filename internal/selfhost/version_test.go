package selfhost_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/selfhost"
)

// TestWriteVersionSourceInLinkedWorktree keeps the selfhost line equal to the
// Go binary's where Go stamps no VCS state: `go build` in a linked worktree,
// whose `.git` is a file, names itself `kizu devel`, and so must the module
// written there. The main checkout of the same repository still names its
// revision.
func TestWriteVersionSourceInLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to make a worktree")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main")
	linked := filepath.Join(dir, "linked")
	runGit(t, dir, "init", "-q", main)
	runGit(t, main, "-c", "user.name=kizu", "-c", "user.email=kizu@example.invalid",
		"commit", "-q", "--allow-empty", "-m", "root")
	runGit(t, main, "worktree", "add", "-q", "--detach", linked)

	if got := writtenVersionLine(t, linked); got != `"kizu devel"` {
		t.Fatalf("linked worktree wrote %s, want \"kizu devel\"", got)
	}
	if got := writtenVersionLine(t, main); !strings.HasPrefix(got, `"kizu devel (`) {
		t.Fatalf("main checkout wrote %s, want the revision", got)
	}
}

// runGit runs one git command in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = selfhost.RepositoryEnv()
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writtenVersionLine writes the version module under root and returns the
// literal it returns.
func writtenVersionLine(t *testing.T, root string) string {
	t.Helper()
	if err := selfhost.WriteVersionSource(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(selfhost.VersionSourcePath)))
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(string(data), "    return ")
	line, _, _ := strings.Cut(after, ";")
	return line
}
