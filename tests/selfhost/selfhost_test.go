// Package selfhost_test validates the Kizu self-host compiler skeleton.
package selfhost_test

import (
	"bytes"
	"encoding/json"
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
const moduleConformanceManifest = "tests/conformance/modules/v0_3.json"

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

type tokenSnapshot struct {
	Kind    string
	Literal string
	Start   int
	End     int
	Line    int
	Column  int
}

type astSnapshot struct {
	Functions int
	Imports   int
	Structs   int
	Enums     int
	Unions    int
	Returns   int
}

type moduleConformanceManifestData struct {
	Version string                  `json:"version"`
	Cases   []moduleConformanceCase `json:"cases"`
}

type moduleConformanceCase struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	Path           string   `json:"path"`
	RootSource     string   `json:"root_source"`
	StderrContains string   `json:"stderr_contains"`
	Features       []string `json:"features"`
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
	snapshots := formatTokenSnapshots(goSelfHostTokenSnapshots(t, fixture))
	astSnapshot := formatAstSnapshot(goAstSnapshot(t, fixture))
	want := "source:simple.kizu\n" +
		filepath.ToSlash(filepath.Dir(fixture)) + "\n" +
		"compiler stages\n8\n" +
		"parsed functions\n2\n" +
		"tokens\n19\n" +
		"bootstrap ready\ntrue\n" +
		"token stream\n" +
		tokenStream + "\n" +
		"token stream end\n" +
		"token snapshots\n" +
		snapshots +
		"token snapshots end\n" +
		"ast snapshot\n" +
		astSnapshot +
		"ast snapshot end\n" +
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
	assertTokenSnapshots(t, got, fixture)
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
	assertTokenSnapshots(t, got, fixture)
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
	assertTokenSnapshots(t, got, fixture)
	assertAstSnapshot(t, got, fixture)
}

// TestSelfHostExampleLexerSnapshotsComparedWithGoLexer checks an examples subset.
func TestSelfHostExampleLexerSnapshotsComparedWithGoLexer(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "examples", "hello.kizu")
	got := runSelfHostFrontend(t, fixture)
	assertTokenSnapshots(t, got, fixture)
	assertAstSnapshot(t, got, fixture)
}

// TestSelfHostReadsModuleConformanceManifest uses the shared module fixtures.
func TestSelfHostReadsModuleConformanceManifest(t *testing.T) {
	manifest := loadModuleConformanceManifest(t)
	for _, tt := range manifest.Cases {
		if tt.Mode != "check" {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(tt.RootSource))
			got := runSelfHostFrontend(t, fixture)
			assertTokenSnapshots(t, got, fixture)
			assertAstSnapshot(t, got, fixture)
		})
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

// loadModuleConformanceManifest reads the shared module fixture manifest.
func loadModuleConformanceManifest(t *testing.T) moduleConformanceManifestData {
	t.Helper()
	path := filepath.Join(repoRoot(t), filepath.FromSlash(moduleConformanceManifest))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest moduleConformanceManifestData
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v0.3-modules" {
		t.Fatalf("unexpected module conformance version %q", manifest.Version)
	}
	return manifest
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

// goSelfHostTokenSnapshots returns Go lexer snapshots in self-host spelling.
func goSelfHostTokenSnapshots(t *testing.T, path string) []tokenSnapshot {
	t.Helper()
	l := lexer.New(readSource(t, path))
	snapshots := []tokenSnapshot{}
	for {
		tok := l.NextToken()
		kind, ok := selfHostKindByGoToken[tok.Type]
		if !ok {
			t.Fatalf("missing self-host token mapping for %s", tok.Type)
		}
		snapshots = append(snapshots, tokenSnapshot{
			Kind:    kind,
			Literal: tok.Literal,
			Start:   tok.Start,
			End:     tok.End,
			Line:    tok.Line,
			Column:  tok.Column,
		})
		if tok.Type == token.EOF {
			return snapshots
		}
	}
}

// formatTokenSnapshots formats snapshots exactly as frontend.kizu prints them.
func formatTokenSnapshots(snapshots []tokenSnapshot) string {
	var out strings.Builder
	for _, snapshot := range snapshots {
		out.WriteString(snapshot.Kind + "\n")
		out.WriteString(snapshot.Literal + "\n")
		out.WriteString(strconv.Itoa(snapshot.Start) + "\n")
		out.WriteString(strconv.Itoa(snapshot.End) + "\n")
		out.WriteString(strconv.Itoa(snapshot.Line) + "\n")
		out.WriteString(strconv.Itoa(snapshot.Column) + "\n")
	}
	return out.String()
}

// goAstSnapshot returns the normalized Go parser snapshot.
func goAstSnapshot(t *testing.T, path string) astSnapshot {
	t.Helper()
	program := parseSelfHostSource(t, path)
	snapshot := astSnapshot{}
	for _, decl := range program.Decls {
		countDeclSnapshot(decl, &snapshot)
	}
	return snapshot
}

// countDeclSnapshot adds one top-level declaration to the snapshot.
func countDeclSnapshot(decl ast.Decl, snapshot *astSnapshot) {
	switch d := decl.(type) {
	case *ast.ImportDecl:
		snapshot.Imports++
	case *ast.FunctionDecl:
		snapshot.Functions++
		countBlockSnapshot(d.Body, snapshot)
	case *ast.StructDecl:
		snapshot.Structs++
	case *ast.EnumDecl:
		snapshot.Enums++
	case *ast.UnionDecl:
		snapshot.Unions++
	case *ast.ImplDecl:
		for _, method := range d.Methods {
			snapshot.Functions++
			countBlockSnapshot(method.Body, snapshot)
		}
	}
}

// countBlockSnapshot adds return statements contained by a block.
func countBlockSnapshot(block *ast.BlockStmt, snapshot *astSnapshot) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		countStatementSnapshot(stmt, snapshot)
	}
}

// countStatementSnapshot adds nested return statements to the snapshot.
func countStatementSnapshot(stmt ast.Statement, snapshot *astSnapshot) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		snapshot.Returns++
	case *ast.IfStmt:
		countBlockSnapshot(s.Consequence, snapshot)
		countBlockSnapshot(s.Alternative, snapshot)
	case *ast.WhileStmt:
		countBlockSnapshot(s.Body, snapshot)
	case *ast.ForStmt:
		countBlockSnapshot(s.Body, snapshot)
	case *ast.UnsafeStmt:
		countBlockSnapshot(s.Body, snapshot)
	case *ast.ComptimeIfStmt:
		countBlockSnapshot(s.Consequence, snapshot)
		countBlockSnapshot(s.Alternative, snapshot)
	}
}

// formatAstSnapshot formats snapshots exactly as frontend.kizu prints them.
func formatAstSnapshot(snapshot astSnapshot) string {
	lines := []string{
		"functions", strconv.Itoa(snapshot.Functions),
		"imports", strconv.Itoa(snapshot.Imports),
		"structs", strconv.Itoa(snapshot.Structs),
		"enums", strconv.Itoa(snapshot.Enums),
		"unions", strconv.Itoa(snapshot.Unions),
		"returns", strconv.Itoa(snapshot.Returns),
	}
	return strings.Join(lines, "\n") + "\n"
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

// assertTokenSnapshots compares all self-host token snapshots against Go lexer output.
func assertTokenSnapshots(t *testing.T, output string, fixture string) {
	t.Helper()
	gotSnapshots := extractSelfHostTokenSnapshots(t, output)
	wantSnapshots := goSelfHostTokenSnapshots(t, fixture)
	if !sameSnapshots(gotSnapshots, wantSnapshots) {
		t.Fatalf("self-host token snapshots got %#v, want %#v", gotSnapshots, wantSnapshots)
	}
}

// assertAstSnapshot compares the self-host parser snapshot against Go parser output.
func assertAstSnapshot(t *testing.T, output string, fixture string) {
	t.Helper()
	got := extractSelfHostAstSnapshot(t, output)
	want := formatAstSnapshot(goAstSnapshot(t, fixture))
	if got != want {
		t.Fatalf("self-host AST snapshot got %q, want %q", got, want)
	}
}

// extractSelfHostAstSnapshot returns the AST snapshot printed by frontend.kizu.
func extractSelfHostAstSnapshot(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for idx, line := range lines {
		if line == "ast snapshot" {
			return parseSelfHostAstSnapshot(t, lines[idx+1:])
		}
	}
	t.Fatal("self-host AST snapshot markers were not found")
	return ""
}

// parseSelfHostAstSnapshot parses lines until the AST snapshot end marker.
func parseSelfHostAstSnapshot(t *testing.T, lines []string) string {
	t.Helper()
	out := []string{}
	for _, line := range lines {
		if line == "ast snapshot end" {
			return strings.Join(out, "\n") + "\n"
		}
		out = append(out, line)
	}
	t.Fatal("self-host AST snapshot end marker was not found")
	return ""
}

// extractSelfHostTokenSnapshots returns token snapshots printed by frontend.kizu.
func extractSelfHostTokenSnapshots(t *testing.T, output string) []tokenSnapshot {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for idx, line := range lines {
		if line == "token snapshots" {
			return parseSelfHostTokenSnapshots(t, lines[idx+1:])
		}
	}
	t.Fatal("self-host token snapshot markers were not found")
	return nil
}

// parseSelfHostTokenSnapshots parses six-line token snapshot records.
func parseSelfHostTokenSnapshots(t *testing.T, lines []string) []tokenSnapshot {
	t.Helper()
	snapshots := []tokenSnapshot{}
	for idx := 0; idx < len(lines); {
		if lines[idx] == "token snapshots end" {
			return snapshots
		}
		if idx+5 >= len(lines) {
			t.Fatalf("truncated token snapshot near %q", lines[idx:])
		}
		snapshots = append(snapshots, parseSelfHostTokenSnapshot(t, lines[idx:idx+6]))
		idx += 6
	}
	t.Fatal("self-host token snapshot end marker was not found")
	return nil
}

// parseSelfHostTokenSnapshot parses one six-line token snapshot record.
func parseSelfHostTokenSnapshot(t *testing.T, lines []string) tokenSnapshot {
	t.Helper()
	start, end := atoi(t, lines[2]), atoi(t, lines[3])
	line, column := atoi(t, lines[4]), atoi(t, lines[5])
	return tokenSnapshot{
		Kind:    lines[0],
		Literal: lines[1],
		Start:   start,
		End:     end,
		Line:    line,
		Column:  column,
	}
}

// atoi parses a test integer or fails with context.
func atoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("invalid integer %q: %v", value, err)
	}
	return parsed
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

// sameSnapshots reports whether two token snapshot slices are identical.
func sameSnapshots(left []tokenSnapshot, right []tokenSnapshot) bool {
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
