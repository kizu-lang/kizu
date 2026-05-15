// Package selfhost_test validates the Kizu self-host compiler skeleton.
package selfhost_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/token"
	"github.com/kizu-lang/kizu/internal/types"
)

const maxLineWidth = 100
const maxFunctionLines = 70
const maxFunctionStatements = 45

// selfHostKindByGoToken maps production Go lexer tokens to self-host enum output.
var selfHostKindByGoToken = map[token.Type]string{
	token.Illegal:     "TokenKind::Illegal",
	token.EOF:         "TokenKind::Eof",
	token.Ident:       "TokenKind::Ident",
	token.Int:         "TokenKind::Number",
	token.String:      "TokenKind::String",
	token.Assign:      "TokenKind::Assign",
	token.Plus:        "TokenKind::Plus",
	token.Minus:       "TokenKind::Minus",
	token.Bang:        "TokenKind::Bang",
	token.Question:    "TokenKind::Question",
	token.Amp:         "TokenKind::Amp",
	token.Asterisk:    "TokenKind::Asterisk",
	token.Slash:       "TokenKind::Slash",
	token.Percent:     "TokenKind::Percent",
	token.Eq:          "TokenKind::Eq",
	token.FatArrow:    "TokenKind::FatArrow",
	token.NotEq:       "TokenKind::NotEq",
	token.LT:          "TokenKind::LT",
	token.LTE:         "TokenKind::LTE",
	token.GT:          "TokenKind::GT",
	token.GTE:         "TokenKind::GTE",
	token.Arrow:       "TokenKind::Arrow",
	token.Dot:         "TokenKind::Dot",
	token.Range:       "TokenKind::Range",
	token.DoubleColon: "TokenKind::DoubleColon",
	token.Comma:       "TokenKind::Comma",
	token.Colon:       "TokenKind::Colon",
	token.Semicolon:   "TokenKind::Semicolon",
	token.Pipe:        "TokenKind::Pipe",
	token.LParen:      "TokenKind::LParen",
	token.RParen:      "TokenKind::RParen",
	token.LBrace:      "TokenKind::LBrace",
	token.RBrace:      "TokenKind::RBrace",
	token.LBracket:    "TokenKind::LBracket",
	token.RBracket:    "TokenKind::RBracket",
	token.Import:      "TokenKind::Import",
	token.Public:      "TokenKind::Public",
	token.Function:    "TokenKind::Fn",
	token.Let:         "TokenKind::Let",
	token.Var:         "TokenKind::Var",
	token.Return:      "TokenKind::Return",
	token.If:          "TokenKind::If",
	token.Else:        "TokenKind::Else",
	token.While:       "TokenKind::While",
	token.Break:       "TokenKind::Break",
	token.Continue:    "TokenKind::Continue",
	token.Match:       "TokenKind::Match",
	token.Struct:      "TokenKind::Struct",
	token.Enum:        "TokenKind::Enum",
	token.Union:       "TokenKind::Union",
	token.Contract:    "TokenKind::Contract",
	token.Satisfy:     "TokenKind::Satisfy",
	token.For:         "TokenKind::For",
	token.Impl:        "TokenKind::Impl",
	token.True:        "TokenKind::True",
	token.False:       "TokenKind::False",
	token.Mut:         "TokenKind::Mut",
	token.Unsafe:      "TokenKind::Unsafe",
	token.Extern:      "TokenKind::Extern",
	token.Comptime:    "TokenKind::Comptime",
	token.Try:         "TokenKind::Try",
}

// TestSelfHostSourcesCheck verifies every self-host Kizu file passes static checks.
func TestSelfHostSourcesCheck(t *testing.T) {
	for _, path := range selfHostSources(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			program := parseSelfHostSource(t, path)
			if err := types.New().Check(program); err != nil {
				t.Fatalf("type check failed: %v", err)
			}
			if err := ownership.New().Check(program); err != nil {
				t.Fatalf("ownership check failed: %v", err)
			}
		})
	}
}

// TestSelfHostFrontendSmoke runs the current frontend skeleton entry point.
func TestSelfHostFrontendSmoke(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "selfhost", "fixtures", "simple.kizu")
	got := runSelfHostFrontend(t, fixture)
	tokenStream := strings.Join(goSelfHostTokenKinds(t, fixture), "\n")
	want := "source:simple.kizu\n" +
		filepath.ToSlash(filepath.Dir(fixture)) + "\n" +
		"compiler stages\n8\n" +
		"parsed functions\n2\n" +
		"tokens\n19\n" +
		"bootstrap ready\ntrue\n" +
		"token stream\n" +
		tokenStream + "\n" +
		"token stream end\n" +
		"TokenKind::Fn\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSelfHostFixtureComparedWithGoLexer checks the first Go/self-host bridge.
func TestSelfHostFixtureComparedWithGoLexer(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "selfhost", "fixtures", "simple.kizu")
	got := runSelfHostFrontend(t, fixture)
	want := "parsed functions\n" + strconv.Itoa(countGoFunctionTokens(t, fixture)) + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain %q", got, want)
	}
	want = "tokens\n" + strconv.Itoa(countGoTokens(t, fixture)) + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain %q", got, want)
	}
	gotStream := extractSelfHostTokenKinds(t, got)
	wantStream := goSelfHostTokenKinds(t, fixture)
	if !same(gotStream, wantStream) {
		t.Fatalf("self-host token stream got %v, want %v", gotStream, wantStream)
	}
}

// TestSelfHostRichLexerStreamComparedWithGoLexer checks a wider token corpus.
func TestSelfHostRichLexerStreamComparedWithGoLexer(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "selfhost", "fixtures", "simple_tokens.kizu")
	got := runSelfHostFrontend(t, fixture)
	want := "tokens\n" + strconv.Itoa(countGoTokens(t, fixture)) + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain %q", got, want)
	}
	gotStream := extractSelfHostTokenKinds(t, got)
	wantStream := goSelfHostTokenKinds(t, fixture)
	if !same(gotStream, wantStream) {
		t.Fatalf("self-host token stream got %v, want %v", gotStream, wantStream)
	}
}

// TestSelfHostReadsModuleFixture checks self-host can read the module fixture.
func TestSelfHostReadsModuleFixture(t *testing.T) {
	fixture := filepath.Join(
		repoRoot(t), "tests", "conformance", "modules", "basic", "src", "main.kizu",
	)
	got := runSelfHostFrontend(t, fixture)
	if !strings.Contains(got, "source:main.kizu\n") {
		t.Fatalf("got %q", got)
	}
	want := "tokens\n" + strconv.Itoa(countGoTokens(t, fixture)) + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain %q", got, want)
	}
}

// TestSelfHostSourcePolicy enforces lightweight compiler-code style rules.
func TestSelfHostSourcePolicy(t *testing.T) {
	for _, path := range selfHostSources(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			source := readSource(t, path)
			checkModuleComment(t, source)
			checkLineWidths(t, source)
			checkFunctionComments(t, source)
			checkFunctionSize(t, source)
		})
	}
}

// runSelfHostFrontend executes the current Kizu frontend against one fixture.
func runSelfHostFrontend(t *testing.T, fixture string) string {
	t.Helper()
	program := parseSelfHostSource(t, filepath.Join(repoRoot(t), "selfhost", "frontend.kizu"))
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	var out bytes.Buffer
	if err := interp.NewWithProcessArgs(&out, []string{fixture}).Run(program); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return out.String()
}

// countGoFunctionTokens counts function tokens with the production Go lexer.
func countGoFunctionTokens(t *testing.T, path string) int {
	t.Helper()
	l := lexer.New(readSource(t, path))
	count := 0
	for {
		tok := l.NextToken()
		if tok.Type == token.Function {
			count++
		}
		if tok.Type == token.EOF {
			return count
		}
	}
}

// countGoTokens counts every token with the production Go lexer, including EOF.
func countGoTokens(t *testing.T, path string) int {
	t.Helper()
	l := lexer.New(readSource(t, path))
	count := 0
	for {
		tok := l.NextToken()
		count++
		if tok.Type == token.EOF {
			return count
		}
	}
}

// goSelfHostTokenKinds returns the Go lexer stream using self-host enum names.
func goSelfHostTokenKinds(t *testing.T, path string) []string {
	t.Helper()
	l := lexer.New(readSource(t, path))
	kinds := []string{}
	for {
		tok := l.NextToken()
		kind, ok := selfHostKindByGoToken[tok.Type]
		if !ok {
			t.Fatalf("missing self-host token mapping for %s", tok.Type)
		}
		kinds = append(kinds, kind)
		if tok.Type == token.EOF {
			return kinds
		}
	}
}

// extractSelfHostTokenKinds returns the token stream printed by frontend.kizu.
func extractSelfHostTokenKinds(t *testing.T, output string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	kinds := []string{}
	inStream := false
	for _, line := range lines {
		switch {
		case line == "token stream":
			inStream = true
		case line == "token stream end":
			return kinds
		case inStream:
			kinds = append(kinds, line)
		}
	}
	t.Fatal("self-host token stream markers were not found")
	return nil
}

// same reports whether two token-kind slices are identical.
func same(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

// parseSelfHostSource parses one Kizu source file and fails on parser errors.
func parseSelfHostSource(t *testing.T, path string) *ast.Program {
	t.Helper()
	l := lexer.New(readSource(t, path))
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %s", strings.Join(p.Errors(), "\n"))
	}
	return program
}

// selfHostSources returns all tracked Kizu skeleton sources.
func selfHostSources(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(repoRoot(t), "selfhost", "*.kizu"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no selfhost Kizu sources found")
	}
	sort.Strings(matches)
	return matches
}

// readSource reads a UTF-8 Kizu source file.
func readSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// checkModuleComment requires a module comment before code.
func checkModuleComment(t *testing.T, source string) {
	t.Helper()
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "// Module:") {
			t.Fatalf("first non-empty line must be a module comment")
		}
		return
	}
	t.Fatal("source is empty")
}

// checkLineWidths enforces the self-host source width rule.
func checkLineWidths(t *testing.T, source string) {
	t.Helper()
	for idx, line := range strings.Split(source, "\n") {
		if len(line) > maxLineWidth {
			t.Fatalf("line %d exceeds %d columns", idx+1, maxLineWidth)
		}
	}
}

// checkFunctionComments requires a comment immediately before each function.
func checkFunctionComments(t *testing.T, source string) {
	t.Helper()
	lines := strings.Split(source, "\n")
	for idx, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "fn ") {
			continue
		}
		comment := previousNonEmptyLine(lines, idx)
		if !strings.HasPrefix(strings.TrimSpace(comment), "// ") {
			t.Fatalf("function at line %d needs a preceding comment", idx+1)
		}
	}
}

// checkFunctionSize enforces simple line and statement limits per function.
func checkFunctionSize(t *testing.T, source string) {
	t.Helper()
	lines := strings.Split(source, "\n")
	for idx := 0; idx < len(lines); idx++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[idx]), "fn ") {
			continue
		}
		end, statements := functionExtent(lines, idx)
		if end-idx+1 > maxFunctionLines {
			t.Fatalf("function at line %d exceeds %d lines", idx+1, maxFunctionLines)
		}
		if statements > maxFunctionStatements {
			t.Fatalf("function at line %d exceeds %d statements", idx+1, maxFunctionStatements)
		}
		idx = end
	}
}

// functionExtent returns the inclusive end line and semicolon statement count.
func functionExtent(lines []string, start int) (int, int) {
	depth := 0
	statements := 0
	opened := false
	for idx := start; idx < len(lines); idx++ {
		line := lines[idx]
		statements += strings.Count(line, ";")
		depth += strings.Count(line, "{")
		if depth > 0 {
			opened = true
		}
		depth -= strings.Count(line, "}")
		if opened && depth == 0 {
			return idx, statements
		}
	}
	return len(lines) - 1, statements
}

// previousNonEmptyLine returns the nearest non-empty line before idx.
func previousNonEmptyLine(lines []string, idx int) string {
	for cursor := idx - 1; cursor >= 0; cursor-- {
		if strings.TrimSpace(lines[cursor]) != "" {
			return lines[cursor]
		}
	}
	return ""
}

// repoRoot returns the repository root from this test package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
