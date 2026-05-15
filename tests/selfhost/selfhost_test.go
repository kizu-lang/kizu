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
	"github.com/kizu-lang/kizu/internal/project"
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

type semanticSnapshot struct {
	FunctionSymbols int
	TypeSymbols     int
	ValueSymbols    int
	Diagnostics     int
}

type irSnapshot struct {
	Functions        int
	Blocks           int
	BackendArtifacts int
}

type diagnosticSnapshot struct {
	Message       string
	PrimaryStart  int
	PrimaryEnd    int
	PrimaryLine   int
	PrimaryColumn int
	RelatedStart  int
	RelatedEnd    int
}

type ownershipSnapshot struct {
	Status       string
	Message      string
	Value        string
	PrimaryStart int
	RelatedStart int
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
	declSnapshot := formatDeclSnapshot(goDeclSnapshot(t, fixture))
	semanticSnapshot := formatSemanticSnapshot(goSemanticSnapshot(t, fixture))
	typeSnapshot := formatTypeSnapshot(goFunctionReturnTypes(t, fixture))
	ownershipSnapshot := formatOwnershipSnapshot(goOwnershipSnapshot(t, fixture))
	irSnapshot := formatIrSnapshot(goIrSnapshot(t, fixture))
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
		"decl snapshot\n" +
		declSnapshot +
		"decl snapshot end\n" +
		"semantic snapshot\n" +
		semanticSnapshot +
		"semantic snapshot end\n" +
		"type snapshot\n" +
		typeSnapshot +
		"type snapshot end\n" +
		"ownership snapshot\n" +
		ownershipSnapshot +
		"ownership snapshot end\n" +
		"ir snapshot\n" +
		irSnapshot +
		"ir snapshot end\n" +
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
	assertAstSnapshot(t, got, fixture)
	assertDeclSnapshot(t, got, fixture)
	assertSemanticSnapshot(t, got, fixture)
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
	assertDeclSnapshot(t, got, fixture)
	assertSemanticSnapshot(t, got, fixture)
	assertIrSnapshot(t, got, fixture)
}

// TestSelfHostExampleLexerSnapshotsComparedWithGoLexer checks an examples subset.
func TestSelfHostExampleLexerSnapshotsComparedWithGoLexer(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "examples", "hello.kizu")
	got := runSelfHostFrontend(t, fixture)
	assertTokenSnapshots(t, got, fixture)
	assertAstSnapshot(t, got, fixture)
	assertDeclSnapshot(t, got, fixture)
	assertIrSnapshot(t, got, fixture)
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
			assertDeclSnapshot(t, got, fixture)
			assertSemanticSnapshot(t, got, fixture)
			assertIrSnapshot(t, got, fixture)
		})
	}
}

// TestSelfHostModuleImportOracle compares root imports with the Go resolver.
func TestSelfHostModuleImportOracle(t *testing.T) {
	root := filepath.Join(repoRoot(t), "tests", "conformance", "modules", "basic")
	source := filepath.Join(root, "src", "main.kizu")
	got := extractImportPaths(t, extractMarkedSnapshot(
		t, runSelfHostFrontend(t, source), "decl snapshot", "decl snapshot end",
	))
	want := goRootImportPaths(t, root)
	if !same(got, want) {
		t.Fatalf("self-host module imports got %v, want %v", got, want)
	}
}

// TestSelfHostDiagnosticObjectOracle compares structured lexer diagnostics.
func TestSelfHostDiagnosticObjectOracle(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "selfhost", "fixtures", "illegal_token.kizu")
	got := extractDiagnosticSnapshots(t, runSelfHostDiagnostics(t, fixture))
	want := goLexerDiagnosticSnapshots(t, fixture)
	if !sameDiagnosticSnapshots(got, want) {
		t.Fatalf("self-host diagnostics got %#v, want %#v", got, want)
	}
}

// TestSelfHostTypeSubsetOracle compares function return types for a small subset.
func TestSelfHostTypeSubsetOracle(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "examples", "functions.kizu")
	got := extractMarkedSnapshot(
		t, runSelfHostFrontend(t, fixture), "type snapshot", "type snapshot end",
	)
	want := formatTypeSnapshot(goFunctionReturnTypes(t, fixture))
	if got != want {
		t.Fatalf("self-host type snapshot got %q, want %q", got, want)
	}
}

// TestSelfHostOwnershipMemoryOracle compares minimal move/borrow safety facts.
func TestSelfHostOwnershipMemoryOracle(t *testing.T) {
	cases := []string{
		"examples/borrow.kizu",
		"examples/negative/moved_value.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture), "ownership snapshot", "ownership snapshot end",
			)
			want := formatOwnershipSnapshot(goOwnershipSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host ownership snapshot got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostSemanticOracleCorpus checks selected semantic pass/fail cases.
func TestSelfHostSemanticOracleCorpus(t *testing.T) {
	positives := []string{
		"examples/hello.kizu",
		"examples/borrow.kizu",
		"examples/std_mem.kizu",
	}
	for _, path := range positives {
		t.Run("positive/"+filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			checkGoSourcePasses(t, fixture)
			got := runSelfHostFrontend(t, fixture)
			assertSemanticSnapshot(t, got, fixture)
			assertIrSnapshot(t, got, fixture)
		})
	}
	negatives := map[string]string{
		"examples/negative/moved_value.kizu":          "moved value `name` was used",
		"examples/negative/move_while_borrowed.kizu":  "cannot be moved while borrowed",
		"examples/negative/immutable_assignment.kizu": "immutable binding `x`",
	}
	for path, want := range negatives {
		t.Run("negative/"+filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			checkGoSourceFails(t, fixture, want)
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
	return runSelfHost(t, []string{fixture})
}

// runSelfHostDiagnostics executes the diagnostic-only frontend path.
func runSelfHostDiagnostics(t *testing.T, fixture string) string {
	t.Helper()
	return runSelfHost(t, []string{fixture, "--diagnostics"})
}

// runSelfHost executes the current Kizu frontend with process arguments.
func runSelfHost(t *testing.T, args []string) string {
	t.Helper()
	program := parseSelfHostSource(t, filepath.Join(repoRoot(t), "selfhost", "frontend.kizu"))
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	var out bytes.Buffer
	if err := interp.NewWithProcessArgs(&out, args).Run(program); err != nil {
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

// goLexerDiagnosticSnapshots returns structured diagnostics from Go lexer facts.
func goLexerDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	l := lexer.New(readSource(t, path))
	snapshots := []diagnosticSnapshot{}
	for {
		tok := l.NextToken()
		if tok.Type == token.Illegal {
			snapshots = append(snapshots, diagnosticSnapshot{
				Message:       "illegal token",
				PrimaryStart:  tok.Start,
				PrimaryEnd:    tok.End,
				PrimaryLine:   tok.Line,
				PrimaryColumn: tok.Column,
				RelatedStart:  tok.Start,
				RelatedEnd:    tok.End,
			})
		}
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

// goDeclSnapshot returns the top-level declaration sequence from the Go AST.
func goDeclSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		lines = appendDeclSnapshot(lines, decl)
	}
	return lines
}

// appendDeclSnapshot appends one top-level declaration to a snapshot.
func appendDeclSnapshot(lines []string, decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.ImportDecl:
		lines = append(lines, "import")
		lines = append(lines, d.Path...)
		return append(lines, "import end")
	case *ast.FunctionDecl:
		return append(lines, "TokenKind::Fn", d.Name)
	case *ast.StructDecl:
		return append(lines, "TokenKind::Struct", d.Name)
	case *ast.EnumDecl:
		return append(lines, "TokenKind::Enum", d.Name)
	case *ast.UnionDecl:
		return append(lines, "TokenKind::Union", d.Name)
	case *ast.ContractDecl:
		return append(lines, "TokenKind::Contract", d.Name)
	default:
		return lines
	}
}

// formatDeclSnapshot formats declaration snapshots as frontend.kizu prints them.
func formatDeclSnapshot(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

// extractImportPaths converts declaration snapshot lines to module import paths.
func extractImportPaths(t *testing.T, snapshot string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(snapshot, "\n"), "\n")
	imports := []string{}
	for idx := 0; idx < len(lines); idx++ {
		if lines[idx] != "import" {
			continue
		}
		parts := []string{}
		for idx++; idx < len(lines) && lines[idx] != "import end"; idx++ {
			parts = append(parts, lines[idx])
		}
		imports = append(imports, strings.Join(parts, "::"))
	}
	return imports
}

// goRootImportPaths returns resolved import paths for a package root module.
func goRootImportPaths(t *testing.T, root string) []string {
	t.Helper()
	pkg, err := project.LoadPackage(root)
	if err != nil {
		t.Fatalf("load package failed: %v", err)
	}
	for _, module := range pkg.Modules {
		if module.Module.Path != pkg.Graph.Root {
			continue
		}
		imports := make([]string, 0, len(module.Imports))
		for _, imported := range module.Imports {
			imports = append(imports, imported.Path)
		}
		return imports
	}
	t.Fatalf("root module %q was not found", pkg.Graph.Root)
	return nil
}

// goSemanticSnapshot returns the normalized Go semantic snapshot.
func goSemanticSnapshot(t *testing.T, path string) semanticSnapshot {
	t.Helper()
	ast := goAstSnapshot(t, path)
	return semanticSnapshot{
		FunctionSymbols: ast.Functions,
		TypeSymbols:     ast.Structs + ast.Enums + ast.Unions,
		ValueSymbols:    0,
		Diagnostics:     0,
	}
}

// goFunctionReturnTypes returns function names and normalized return types.
func goFunctionReturnTypes(t *testing.T, path string) []string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		lines = append(lines, fn.Name, normalizeReturnType(fn.ReturnType))
	}
	return lines
}

// normalizeReturnType maps omitted returns to Kizu void.
func normalizeReturnType(returnType string) string {
	if returnType == "" {
		return "void"
	}
	return returnType
}

// formatTypeSnapshot formats function type snapshots as frontend.kizu prints them.
func formatTypeSnapshot(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

// goOwnershipSnapshot returns normalized Go ownership-checker facts.
func goOwnershipSnapshot(t *testing.T, path string) ownershipSnapshot {
	t.Helper()
	program := parseSelfHostSource(t, path)
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed before ownership oracle: %v", err)
	}
	err := ownership.New().Check(program)
	if err == nil {
		return ownershipSnapshot{Status: "pass", PrimaryStart: -1, RelatedStart: -1}
	}
	value := movedValueFromDiagnostic(t, err.Error())
	primary, related := movedValueSpans(t, path, value)
	return ownershipSnapshot{
		Status: "fail", Message: "move error: moved value was used",
		Value: value, PrimaryStart: primary, RelatedStart: related,
	}
}

// movedValueFromDiagnostic extracts the identifier from a moved-value diagnostic.
func movedValueFromDiagnostic(t *testing.T, message string) string {
	t.Helper()
	prefix := "moved value `"
	start := strings.Index(message, prefix)
	if start < 0 {
		t.Fatalf("ownership diagnostic is outside oracle subset: %q", message)
	}
	rest := message[start+len(prefix):]
	end := strings.Index(rest, "`")
	if end < 0 {
		t.Fatalf("ownership diagnostic has no moved value terminator: %q", message)
	}
	return rest[:end]
}

// movedValueSpans returns the move site and first later use for one identifier.
func movedValueSpans(t *testing.T, path string, value string) (int, int) {
	t.Helper()
	tokens := goSelfHostTokenSnapshots(t, path)
	positions := []int{}
	for _, tok := range tokens {
		if tok.Kind != "TokenKind::Ident" || tok.Literal != value {
			continue
		}
		positions = append(positions, tok.Start)
	}
	if len(positions) < 2 {
		t.Fatalf("could not find moved value spans for %q in %s", value, path)
	}
	return positions[len(positions)-1], positions[len(positions)-2]
}

// formatOwnershipSnapshot formats ownership facts as frontend.kizu prints them.
func formatOwnershipSnapshot(snapshot ownershipSnapshot) string {
	lines := []string{"status", snapshot.Status}
	if snapshot.Status == "fail" {
		lines = append(lines,
			snapshot.Message,
			snapshot.Value,
			strconv.Itoa(snapshot.PrimaryStart),
			strconv.Itoa(snapshot.RelatedStart),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

// formatSemanticSnapshot formats semantic snapshots as frontend.kizu prints them.
func formatSemanticSnapshot(snapshot semanticSnapshot) string {
	lines := []string{
		"function symbols", strconv.Itoa(snapshot.FunctionSymbols),
		"type symbols", strconv.Itoa(snapshot.TypeSymbols),
		"value symbols", strconv.Itoa(snapshot.ValueSymbols),
		"diagnostics", strconv.Itoa(snapshot.Diagnostics),
	}
	return strings.Join(lines, "\n") + "\n"
}

// goIrSnapshot returns the normalized Go IR/backend snapshot.
func goIrSnapshot(t *testing.T, path string) irSnapshot {
	t.Helper()
	ast := goAstSnapshot(t, path)
	return irSnapshot{
		Functions:        ast.Functions,
		Blocks:           ast.Functions,
		BackendArtifacts: 1,
	}
}

// formatIrSnapshot formats IR snapshots as frontend.kizu prints them.
func formatIrSnapshot(snapshot irSnapshot) string {
	lines := []string{
		"functions", strconv.Itoa(snapshot.Functions),
		"blocks", strconv.Itoa(snapshot.Blocks),
		"backend artifacts", strconv.Itoa(snapshot.BackendArtifacts),
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

// assertDeclSnapshot compares top-level declaration order with the Go parser.
func assertDeclSnapshot(t *testing.T, output string, fixture string) {
	t.Helper()
	got := extractMarkedSnapshot(t, output, "decl snapshot", "decl snapshot end")
	want := formatDeclSnapshot(goDeclSnapshot(t, fixture))
	if got != want {
		t.Fatalf("self-host declaration snapshot got %q, want %q", got, want)
	}
}

// assertSemanticSnapshot compares self-host semantic output with Go parser facts.
func assertSemanticSnapshot(t *testing.T, output string, fixture string) {
	t.Helper()
	got := extractMarkedSnapshot(t, output, "semantic snapshot", "semantic snapshot end")
	want := formatSemanticSnapshot(goSemanticSnapshot(t, fixture))
	if got != want {
		t.Fatalf("self-host semantic snapshot got %q, want %q", got, want)
	}
}

// assertIrSnapshot compares self-host IR/backend output with Go-derived facts.
func assertIrSnapshot(t *testing.T, output string, fixture string) {
	t.Helper()
	got := extractMarkedSnapshot(t, output, "ir snapshot", "ir snapshot end")
	want := formatIrSnapshot(goIrSnapshot(t, fixture))
	if got != want {
		t.Fatalf("self-host IR snapshot got %q, want %q", got, want)
	}
}

// extractDiagnosticSnapshots returns diagnostic records printed by frontend.kizu.
func extractDiagnosticSnapshots(t *testing.T, output string) []diagnosticSnapshot {
	t.Helper()
	snapshot := extractMarkedSnapshot(t, output, "diagnostic snapshot", "diagnostic snapshot end")
	lines := strings.Split(strings.TrimSuffix(snapshot, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines)%7 != 0 {
		t.Fatalf("diagnostic snapshot has incomplete records: %q", snapshot)
	}
	diagnostics := []diagnosticSnapshot{}
	for idx := 0; idx < len(lines); idx += 7 {
		diagnostics = append(diagnostics, diagnosticSnapshot{
			Message:       lines[idx],
			PrimaryStart:  atoi(t, lines[idx+1]),
			PrimaryEnd:    atoi(t, lines[idx+2]),
			PrimaryLine:   atoi(t, lines[idx+3]),
			PrimaryColumn: atoi(t, lines[idx+4]),
			RelatedStart:  atoi(t, lines[idx+5]),
			RelatedEnd:    atoi(t, lines[idx+6]),
		})
	}
	return diagnostics
}

// extractSelfHostAstSnapshot returns the AST snapshot printed by frontend.kizu.
func extractSelfHostAstSnapshot(t *testing.T, output string) string {
	t.Helper()
	return extractMarkedSnapshot(t, output, "ast snapshot", "ast snapshot end")
}

// extractMarkedSnapshot returns lines between a pair of snapshot markers.
func extractMarkedSnapshot(t *testing.T, output string, start string, end string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for idx, line := range lines {
		if line == start {
			return parseMarkedSnapshot(t, lines[idx+1:], end)
		}
	}
	t.Fatalf("self-host snapshot marker %q was not found", start)
	return ""
}

// parseMarkedSnapshot parses lines until the end marker.
func parseMarkedSnapshot(t *testing.T, lines []string, end string) string {
	t.Helper()
	out := []string{}
	for _, line := range lines {
		if line == end {
			return strings.Join(out, "\n") + "\n"
		}
		out = append(out, line)
	}
	t.Fatalf("self-host snapshot end marker %q was not found", end)
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

// sameDiagnosticSnapshots reports whether two diagnostic snapshots are identical.
func sameDiagnosticSnapshots(left []diagnosticSnapshot, right []diagnosticSnapshot) bool {
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

// checkGoSourcePasses runs the Go semantic checkers for a positive fixture.
func checkGoSourcePasses(t *testing.T, path string) {
	t.Helper()
	program := parseSelfHostSource(t, path)
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
}

// checkGoSourceFails runs the Go semantic checkers for a negative fixture.
func checkGoSourceFails(t *testing.T, path string, want string) {
	t.Helper()
	program := parseSelfHostSource(t, path)
	err := types.New().Check(program)
	if err == nil {
		err = ownership.New().Check(program)
	}
	if err == nil {
		t.Fatalf("expected semantic error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got error %q, want substring %q", err, want)
	}
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
