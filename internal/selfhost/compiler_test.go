package selfhost_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kizu-lang/kizu/internal/selfhost"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/stdlib/stdlibtest"
)

// repoRoot is where the Kizu compiler's sources are read from and where the
// CLI is run.
const repoRoot = "../.."

// TestCompilerTestsPass runs the test blocks of the Kizu compiler under
// compiler/ -- what `kizu test compiler` and `just selfhost` run. They are
// Kizu sources, which `go test` never sees on its own: the seed compiles the
// compiler and its tests into one executable and runs it, and this is the
// one place in `go test ./...` that happens.
//
// It lives in its own package so that it runs beside cmd/kizu rather than
// behind it: both take a minute or more, and the packages of one `go test`
// run in parallel while the tests of one package do not.
func TestCompilerTestsPass(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the Kizu compiler's tests and runs them")
	}
	// The compiler reads its version from a module generated with it, because
	// a Kizu build has no linker flag to stamp. Write it before anything
	// builds `compiler/`.
	if err := selfhost.WriteVersionSource(repoRoot); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "kizu")
	build := exec.Command("go", "build", "-o", binary, "./cmd/kizu")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build kizu: %v\n%s", err, out)
	}
	// The binary sits in a temp directory, so point it at the repository's
	// library tree the way a user points at an installed one.
	env := append(os.Environ(), stdlib.LibDirEnv+"="+stdlibtest.RepoLibDir())
	for _, verb := range []string{"check", "test"} {
		command := exec.Command(binary, verb, "compiler")
		command.Dir = repoRoot
		command.Env = env
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("kizu %s compiler: %v\n%s", verb, err, out)
		}
	}
}
