package transpile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateCompilerDoesNotEmitGoDriver checks the bootstrap source is not a
// shell/native driver for the existing Go compiler.
func TestGenerateCompilerDoesNotEmitGoDriver(t *testing.T) {
	outDir := t.TempDir()
	repoRoot := filepath.Clean(filepath.Join("..", ".."))

	if err := GenerateCompiler(repoRoot, outDir); err != nil {
		t.Fatal(err)
	}
	assertNoGoDriverFragments(t, generatedSourcePaths(t, outDir))
	assertGeneratedSourceContains(t, outDir, "src/token.kizu", `if ident == "fn"`)
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "pub fn FirstTokenCode")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "while start < length")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "return token_at")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "fn token_at")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "return punctuation_token")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "fn make_token_at")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "token::Type::FatArrow")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "token::Type::Slash")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "token::Type::NotEq")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "token::Type::LBracket")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "token::Type::String")
	assertGeneratedSourceContains(t, outDir, "src/lexer.kizu", "input[start + 1..end]")
	assertGeneratedSourceContains(t, outDir, "src/parser.kizu", "pub fn first_token_code")
	assertGeneratedSourceContains(t, outDir, "src/parser.kizu", "pub fn function_count")
	assertGeneratedSourceContains(t, outDir, "src/parser.kizu", "pub fn declaration_score")
	assertGeneratedSourceContains(t, outDir, "src/parser.kizu", "pub fn brace_score")
	assertGeneratedSourceContains(t, outDir, "src/parser.kizu", "pub fn parse_score")
	assertGeneratedSourceContains(t, outDir, "src/checker.kizu", "return parsed >= 100")
	assertGeneratedSourceContains(t, outDir, "src/lower.kizu", "return parsed")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "if module <= 0")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "std::string::String")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "append_i64")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "kizu stage source bytes")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "try out.append_bytes")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "declare ptr @fopen")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "fopen(ptr %source8, ptr %readmode)")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "fgetc(ptr %srcfile8)")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "@parsemetric")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "%parsetotal")
	assertGeneratedSourceContains(t, outDir, "src/emit.kizu", "br i1 %scanned, label %write")
}

// TestCheckedInSelfhostDoesNotEmitGoDriver checks the committed bootstrap seed
// has the same no-driver invariant as freshly generated output.
func TestCheckedInSelfhostDoesNotEmitGoDriver(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	assertNoGoDriverFragments(t, generatedSourcePaths(t, filepath.Join(repoRoot, "selfhost")))
}

// assertNoGoDriverFragments rejects forbidden bootstrap shortcuts.
func assertNoGoDriverFragments(t *testing.T, paths []string) {
	t.Helper()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{"go run", "cmd/kizu", "@system", "system("} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s contains forbidden bootstrap driver fragment %q", path, forbidden)
			}
		}
	}
}

// assertGeneratedSourceContains checks that generator output includes real logic.
func assertGeneratedSourceContains(t *testing.T, dir string, name string, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q", name, want)
	}
}

// generatedSourcePaths returns every generated file under dir.
func generatedSourcePaths(t *testing.T, dir string) []string {
	t.Helper()
	paths := []string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
