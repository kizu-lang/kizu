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
	kir "github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/llvm"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/token"
	"github.com/kizu-lang/kizu/internal/types"
	"github.com/kizu-lang/kizu/internal/wasm"
)

const maxLineWidth = 100
const maxFunctionLines = 70
const maxFunctionStatements = 45
const conformanceManifestGlob = "tests/conformance/v0_*.json"
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

type irDumpSnapshot struct {
	Status    string
	Message   string
	Functions []irDumpFunction
}

type irDumpFunction struct {
	Name       string
	ReturnType string
	Params     []string
	Block      string
	Opcodes    []string
	Terminator string
}

type backendFingerprint struct {
	Target    string
	Status    string
	Message   string
	Functions int
	Names     []string
	Strings   int
	Entry     string
	Lines     []string
}

type moduleGraphSnapshot struct {
	Status  string
	Message string
	Root    string
	Modules int
	Imports []string
}

type moduleConformanceManifestData struct {
	Version string                  `json:"version"`
	Cases   []moduleConformanceCase `json:"cases"`
}

type conformanceManifestData struct {
	Version string            `json:"version"`
	Cases   []conformanceCase `json:"cases"`
}

type conformanceCase struct {
	Name string `json:"name"`
	Path string `json:"path"`
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
	want := selfHostFrontendSmokeOutput(t, fixture)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// selfHostFrontendSmokeOutput returns the expected full skeleton output.
func selfHostFrontendSmokeOutput(t *testing.T, fixture string) string {
	t.Helper()
	tokenStream := strings.Join(goSelfHostTokenKinds(t, fixture), "\n")
	snapshots := formatTokenSnapshots(goSelfHostTokenSnapshots(t, fixture))
	astSnapshot := formatAstSnapshot(goAstSnapshot(t, fixture))
	astDetailSnapshot := formatAstDetailSnapshot(goAstDetailSnapshot(t, fixture))
	declSnapshot := formatDeclSnapshot(goDeclSnapshot(t, fixture))
	moduleSnapshot := formatModuleGraphSnapshot(singleFileModuleGraphSnapshot())
	semanticSnapshot := formatSemanticSnapshot(goSemanticSnapshot(t, fixture))
	typeSnapshot := formatTypeSnapshot(goFunctionReturnTypes(t, fixture))
	typeEnvSnapshot := formatTypeEnvSnapshot(goLocalTypeEnvSnapshot(t, fixture))
	typeCheckSnapshot := formatTypeCheckSnapshot(goTypeCheckSnapshot(t, fixture))
	ownershipSnapshot := formatOwnershipSnapshot(goOwnershipSnapshot(t, fixture))
	irSnapshot := formatIrSnapshot(goIrSnapshot(t, fixture))
	irDumpSnapshot := formatIrDumpSnapshot(goIrDumpSnapshot(t, fixture))
	backendSnapshot := formatBackendFingerprints(goBackendFingerprints(t, fixture))
	cacheSnapshot := formatCacheContractSnapshot()
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
		"ast detail snapshot\n" +
		astDetailSnapshot +
		"ast detail snapshot end\n" +
		"decl snapshot\n" +
		declSnapshot +
		"decl snapshot end\n" +
		"module graph snapshot\n" +
		moduleSnapshot +
		"module graph snapshot end\n" +
		"semantic snapshot\n" +
		semanticSnapshot +
		"semantic snapshot end\n" +
		"type snapshot\n" +
		typeSnapshot +
		"type snapshot end\n" +
		"type env snapshot\n" +
		typeEnvSnapshot +
		"type env snapshot end\n" +
		"type check snapshot\n" +
		typeCheckSnapshot +
		"type check snapshot end\n" +
		"ownership snapshot\n" +
		ownershipSnapshot +
		"ownership snapshot end\n" +
		"ir snapshot\n" +
		irSnapshot +
		"ir snapshot end\n" +
		"ir dump snapshot\n" +
		irDumpSnapshot +
		"ir dump snapshot end\n" +
		"backend fingerprint snapshot\n" +
		backendSnapshot +
		"backend fingerprint snapshot end\n" +
		"cache contract snapshot\n" +
		cacheSnapshot +
		"cache contract snapshot end\n" +
		"TokenKind::Fn\n"
	return want
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

// TestSelfHostConformanceLexerSnapshotsComparedWithGoLexer checks all manifests.
func TestSelfHostConformanceLexerSnapshotsComparedWithGoLexer(t *testing.T) {
	for _, fixture := range selfHostConformanceSources(t) {
		t.Run(filepath.ToSlash(fixture), func(t *testing.T) {
			got := runSelfHostFrontend(t, fixture)
			assertTokenSnapshots(t, got, fixture)
		})
	}
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

// TestSelfHostModuleGraphOracle compares package root graph facts and a failure.
func TestSelfHostModuleGraphOracle(t *testing.T) {
	cases := []struct {
		name   string
		root   string
		source string
	}{
		{
			name:   "basic",
			root:   "tests/conformance/modules/basic",
			source: "tests/conformance/modules/basic/src/main.kizu",
		},
		{
			name:   "missing_import",
			root:   "tests/conformance/modules/missing_import",
			source: "tests/conformance/modules/missing_import/src/main.kizu",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(repoRoot(t), filepath.FromSlash(tt.root))
			source := filepath.Join(repoRoot(t), filepath.FromSlash(tt.source))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, source),
				"module graph snapshot", "module graph snapshot end",
			)
			want := formatModuleGraphSnapshot(goModuleGraphSnapshot(t, root))
			if got != want {
				t.Fatalf("self-host module graph got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostAstDetailOracle compares function parser details and a parse failure.
func TestSelfHostAstDetailOracle(t *testing.T) {
	cases := []string{
		"examples/functions.kizu",
		"examples/struct.kizu",
		"examples/enum.kizu",
		"examples/union.kizu",
		"examples/negative/missing_semicolon.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture), "ast detail snapshot", "ast detail snapshot end",
			)
			want := formatAstDetailSnapshot(goAstDetailSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host AST detail snapshot got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostDiagnosticObjectOracle compares structured diagnostics.
func TestSelfHostDiagnosticObjectOracle(t *testing.T) {
	cases := []string{
		"selfhost/fixtures/illegal_token.kizu",
		"examples/negative/missing_semicolon.kizu",
		"tests/conformance/modules/missing_import/src/main.kizu",
		"examples/negative/std_mem_wrong_type.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractDiagnosticSnapshots(t, runSelfHostDiagnostics(t, fixture))
			want := goDiagnosticSnapshots(t, fixture)
			if !sameDiagnosticSnapshots(got, want) {
				t.Fatalf("self-host diagnostics got %#v, want %#v", got, want)
			}
		})
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

// TestSelfHostTypeEnvironmentOracle compares selected local binding types.
func TestSelfHostTypeEnvironmentOracle(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "selfhost", "fixtures", "type_env.kizu")
	got := extractMarkedSnapshot(
		t, runSelfHostFrontend(t, fixture), "type env snapshot", "type env snapshot end",
	)
	want := formatTypeEnvSnapshot(goLocalTypeEnvSnapshot(t, fixture))
	if got != want {
		t.Fatalf("self-host type env snapshot got %q, want %q", got, want)
	}
}

// TestSelfHostTypeCheckOracle compares a selected type pass/fail subset.
func TestSelfHostTypeCheckOracle(t *testing.T) {
	cases := []string{
		"examples/functions.kizu",
		"examples/negative/std_mem_wrong_type.kizu",
		"selfhost/fixtures/type_arg_count.kizu",
		"selfhost/fixtures/type_arg_type.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture),
				"type check snapshot", "type check snapshot end",
			)
			want := formatTypeCheckSnapshot(goTypeCheckSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host type check snapshot got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostOwnershipMemoryOracle compares minimal move/borrow safety facts.
func TestSelfHostOwnershipMemoryOracle(t *testing.T) {
	cases := []string{
		"examples/borrow.kizu",
		"examples/negative/moved_value.kizu",
		"examples/negative/double_move.kizu",
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

// TestSelfHostIrDumpOracle compares minimal normalized IR dump facts.
func TestSelfHostIrDumpOracle(t *testing.T) {
	cases := []string{
		"examples/functions.kizu",
		"examples/arithmetic.kizu",
		"examples/negative/missing_return.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture), "ir dump snapshot", "ir dump snapshot end",
			)
			want := formatIrDumpSnapshot(goIrDumpSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host IR dump snapshot got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostBackendFingerprintOracle compares backend smoke fingerprints.
func TestSelfHostBackendFingerprintOracle(t *testing.T) {
	cases := []string{
		"examples/hello.kizu",
		"examples/negative/missing_return.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture),
				"backend fingerprint snapshot", "backend fingerprint snapshot end",
			)
			want := formatBackendFingerprints(goBackendFingerprints(t, fixture))
			if got != want {
				t.Fatalf("self-host backend fingerprint got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostCacheContractOracle compares the Go-owned cache switch decision.
func TestSelfHostCacheContractOracle(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "examples", "hello.kizu")
	got := extractMarkedSnapshot(
		t, runSelfHostFrontend(t, fixture),
		"cache contract snapshot", "cache contract snapshot end",
	)
	want := formatCacheContractSnapshot()
	if got != want {
		t.Fatalf("self-host cache contract got %q, want %q", got, want)
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

// selfHostConformanceSources returns every source path from reusable manifests.
func selfHostConformanceSources(t *testing.T) []string {
	t.Helper()
	paths := map[string]bool{}
	for _, manifest := range loadConformanceManifests(t) {
		for _, item := range manifest.Cases {
			paths[filepath.Join(repoRoot(t), filepath.FromSlash(item.Path))] = true
		}
	}
	for _, item := range loadModuleConformanceManifest(t).Cases {
		paths[filepath.Join(repoRoot(t), filepath.FromSlash(item.RootSource))] = true
	}
	return sortedMapKeys(paths)
}

// loadConformanceManifests reads reusable non-module conformance manifests.
func loadConformanceManifests(t *testing.T) []conformanceManifestData {
	t.Helper()
	glob := filepath.Join(repoRoot(t), filepath.FromSlash(conformanceManifestGlob))
	paths, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no conformance manifests found")
	}
	manifests := make([]conformanceManifestData, 0, len(paths))
	for _, path := range paths {
		manifests = append(manifests, loadConformanceManifest(t, path))
	}
	return manifests
}

// loadConformanceManifest reads one reusable non-module manifest.
func loadConformanceManifest(t *testing.T, path string) conformanceManifestData {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest conformanceManifestData
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// sortedMapKeys returns map keys in stable order.
func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

// goDiagnosticSnapshots returns structured diagnostics from Go compiler facts.
func goDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	snapshots := goLexerDiagnosticSnapshots(t, path)
	snapshots = append(snapshots, goParserDiagnosticSnapshots(t, path)...)
	snapshots = append(snapshots, goModuleDiagnosticSnapshots(t, path)...)
	snapshots = append(snapshots, goTypeDiagnosticSnapshots(t, path)...)
	return snapshots
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

// goParserDiagnosticSnapshots returns parser diagnostics in the self-host subset.
func goParserDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	if !strings.Contains(filepath.ToSlash(path), "missing_semicolon") {
		return nil
	}
	l := lexer.New(readSource(t, path))
	p := parser.New(l)
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		return nil
	}
	if normalizeParseError(p.Errors()) != "parser error: missing semicolon" {
		return nil
	}
	token := missingSemicolonToken(t, path)
	return []diagnosticSnapshot{{
		Message:      "parser error: missing semicolon",
		PrimaryStart: token.Start, PrimaryEnd: token.End,
		PrimaryLine: token.Line, PrimaryColumn: token.Column,
		RelatedStart: token.Start, RelatedEnd: token.End,
	}}
}

// goModuleDiagnosticSnapshots returns resolver diagnostics in the self-host subset.
func goModuleDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	if !strings.Contains(filepath.ToSlash(path), "missing_import") {
		return nil
	}
	token := tokenWithLiteral(t, path, "missing")
	return []diagnosticSnapshot{{
		Message:      "module error: missing module",
		PrimaryStart: token.Start, PrimaryEnd: token.End,
		PrimaryLine: token.Line, PrimaryColumn: token.Column,
		RelatedStart: token.Start, RelatedEnd: token.End,
	}}
}

// goTypeDiagnosticSnapshots returns type checker diagnostics in the self-host subset.
func goTypeDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	if !strings.Contains(filepath.ToSlash(path), "std_mem_wrong_type") {
		return nil
	}
	program := parseSelfHostSource(t, path)
	err := types.New().Check(program)
	if err == nil {
		t.Fatalf("expected type diagnostic for %s", path)
	}
	message := normalizeTypeError(err.Error())
	if message != "type error: `std::mem::equal_bytes` arg 2 expects []const u8" {
		t.Fatalf("type diagnostic is outside oracle subset: %q", message)
	}
	tok := equalBytesWrongArgumentToken(t, path)
	return []diagnosticSnapshot{diagnosticFromToken(message, tok, tok)}
}

// diagnosticFromToken constructs the shared diagnostic snapshot row.
func diagnosticFromToken(
	message string,
	primary token.Token,
	related token.Token,
) diagnosticSnapshot {
	return diagnosticSnapshot{
		Message:       message,
		PrimaryStart:  primary.Start,
		PrimaryEnd:    primary.End,
		PrimaryLine:   primary.Line,
		PrimaryColumn: primary.Column,
		RelatedStart:  related.Start,
		RelatedEnd:    related.End,
	}
}

// missingSemicolonToken returns the token used for parser diagnostics.
func missingSemicolonToken(t *testing.T, path string) token.Token {
	t.Helper()
	tokens := goTokens(t, path)
	for index := 1; index < len(tokens); index++ {
		if tokens[index].Type == token.RBrace && tokens[index-1].Type == token.RParen {
			return tokens[index]
		}
	}
	t.Fatalf("missing semicolon token was not found in %s", path)
	return token.Token{}
}

// equalBytesWrongArgumentToken returns the selected std::mem type diagnostic token.
func equalBytesWrongArgumentToken(t *testing.T, path string) token.Token {
	t.Helper()
	tokens := goTokens(t, path)
	for index, tok := range tokens {
		if tok.Literal != "equal_bytes" {
			continue
		}
		return secondArgumentToken(t, path, tokens, index)
	}
	t.Fatalf("equal_bytes call was not found in %s", path)
	return token.Token{}
}

// secondArgumentToken returns the token after the first comma in one call.
func secondArgumentToken(
	t *testing.T,
	path string,
	tokens []token.Token,
	start int,
) token.Token {
	t.Helper()
	for index := start; index < len(tokens); index++ {
		if tokens[index].Type == token.Comma {
			return tokens[index+1]
		}
		if tokens[index].Type == token.RParen {
			break
		}
	}
	t.Fatalf("second argument token was not found in %s", path)
	return token.Token{}
}

// tokenWithLiteral returns the first token with the requested literal.
func tokenWithLiteral(t *testing.T, path string, literal string) token.Token {
	t.Helper()
	for _, tok := range goTokens(t, path) {
		if tok.Literal == literal {
			return tok
		}
	}
	t.Fatalf("literal %q was not found in %s", literal, path)
	return token.Token{}
}

// goTokens returns the Go lexer token stream.
func goTokens(t *testing.T, path string) []token.Token {
	t.Helper()
	l := lexer.New(readSource(t, path))
	tokens := []token.Token{}
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			return tokens
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

// goAstDetailSnapshot returns a normalized parser detail snapshot.
func goAstDetailSnapshot(t *testing.T, path string) []string {
	t.Helper()
	l := lexer.New(readSource(t, path))
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return []string{"status", "fail", "message", normalizeParseError(p.Errors())}
	}
	lines := []string{"status", "pass"}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			lines = append(lines, astStructDetail(d)...)
		case *ast.EnumDecl:
			lines = append(lines, astEnumDetail(d)...)
		case *ast.UnionDecl:
			lines = append(lines, astUnionDetail(d)...)
		case *ast.FunctionDecl:
			lines = append(lines, astFunctionDetail(d)...)
		}
	}
	return lines
}

// astStructDetail returns parser facts for one struct declaration.
func astStructDetail(decl *ast.StructDecl) []string {
	lines := []string{"struct", decl.Name, "fields", strconv.Itoa(len(decl.Fields))}
	for _, field := range decl.Fields {
		lines = append(lines, "field", field.Name, field.TypeName)
	}
	return lines
}

// astEnumDetail returns parser facts for one enum declaration.
func astEnumDetail(decl *ast.EnumDecl) []string {
	lines := []string{"enum", decl.Name, "tags", strconv.Itoa(len(decl.Tags))}
	for _, tag := range decl.Tags {
		lines = append(lines, "tag", tag)
	}
	return lines
}

// astUnionDetail returns parser facts for one union declaration.
func astUnionDetail(decl *ast.UnionDecl) []string {
	lines := []string{"union", decl.Name, "variants", strconv.Itoa(len(decl.Variants))}
	for _, variant := range decl.Variants {
		payload := variant.Payload
		if payload == "" {
			payload = "<none>"
		}
		lines = append(lines, "variant", variant.Name, payload)
	}
	return lines
}

// astFunctionDetail returns parser facts for one function declaration.
func astFunctionDetail(fn *ast.FunctionDecl) []string {
	lines := []string{
		"fn", fn.Name,
		"params", strconv.Itoa(len(fn.Params)),
	}
	for _, param := range fn.Params {
		lines = append(lines, "param", param.TypeName)
	}
	lines = append(lines,
		"return", normalizeReturnType(fn.ReturnType),
		"returns", strconv.Itoa(countReturnsInBlock(fn.Body)),
	)
	return lines
}

// normalizeParseError maps parser diagnostics into the self-host subset.
func normalizeParseError(errors []string) string {
	for _, item := range errors {
		if strings.Contains(item, "expected `;`") {
			return "parser error: missing semicolon"
		}
	}
	return "parser error"
}

// countReturnsInBlock counts return statements in one parser block.
func countReturnsInBlock(block *ast.BlockStmt) int {
	if block == nil {
		return 0
	}
	count := 0
	for _, stmt := range block.Statements {
		if _, ok := stmt.(*ast.ReturnStmt); ok {
			count++
		}
	}
	return count
}

// formatAstDetailSnapshot formats parser detail rows.
func formatAstDetailSnapshot(lines []string) string {
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

// singleFileModuleGraphSnapshot returns the non-package graph shape.
func singleFileModuleGraphSnapshot() moduleGraphSnapshot {
	return moduleGraphSnapshot{Status: "pass", Root: "<single>", Modules: 1}
}

// goModuleGraphSnapshot returns package graph facts from the Go resolver.
func goModuleGraphSnapshot(t *testing.T, root string) moduleGraphSnapshot {
	t.Helper()
	pkg, err := project.LoadPackage(root)
	if err != nil {
		return moduleGraphSnapshot{Status: "fail", Message: normalizeModuleError(err.Error())}
	}
	snapshot := moduleGraphSnapshot{
		Status: "pass", Root: pkg.Graph.Root, Modules: len(pkg.Graph.Modules),
	}
	for _, module := range pkg.Modules {
		if module.Module.Path != pkg.Graph.Root {
			continue
		}
		for _, imported := range module.Imports {
			snapshot.Imports = append(snapshot.Imports, imported.Path)
		}
	}
	return snapshot
}

// normalizeModuleError maps resolver diagnostics into the self-host subset.
func normalizeModuleError(message string) string {
	if strings.Contains(message, "missing module") {
		return "module error: missing module"
	}
	return "module error"
}

// formatModuleGraphSnapshot formats module graph rows.
func formatModuleGraphSnapshot(snapshot moduleGraphSnapshot) string {
	lines := []string{"status", snapshot.Status}
	if snapshot.Status == "fail" {
		lines = append(lines, "message", snapshot.Message)
		return strings.Join(lines, "\n") + "\n"
	}
	lines = append(lines, "root", snapshot.Root, "modules", strconv.Itoa(snapshot.Modules))
	for _, imported := range snapshot.Imports {
		lines = append(lines, "import")
		lines = append(lines, strings.Split(imported, "::")...)
		lines = append(lines, "import end")
	}
	return strings.Join(lines, "\n") + "\n"
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

// goLocalTypeEnvSnapshot returns local binding type facts in source order.
func goLocalTypeEnvSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		lines = append(lines, goBlockLocalTypeEnv(t, fn.Body)...)
	}
	return lines
}

// goBlockLocalTypeEnv returns local binding type facts for one block.
func goBlockLocalTypeEnv(t *testing.T, block *ast.BlockStmt) []string {
	t.Helper()
	lines := []string{}
	for _, stmt := range block.Statements {
		local, ok := stmt.(*ast.LetStmt)
		if !ok {
			continue
		}
		mutability := "let"
		if local.Mutable {
			mutability = "var"
		}
		lines = append(lines, mutability, local.Name, goLocalInitializerType(t, local.Value))
	}
	return lines
}

// goLocalInitializerType infers the supported local initializer type subset.
func goLocalInitializerType(t *testing.T, expr ast.Expression) string {
	t.Helper()
	switch value := expr.(type) {
	case *ast.StringExpr:
		return "[]const u8"
	case *ast.IntExpr:
		return "i64"
	case *ast.BoolExpr:
		return "bool"
	case *ast.StructLiteralExpr:
		return value.TypeName
	default:
		return "unknown"
	}
}

// formatTypeEnvSnapshot formats local binding type facts.
func formatTypeEnvSnapshot(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// goTypeCheckSnapshot returns normalized Go type checker facts.
func goTypeCheckSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	err := types.New().Check(program)
	if err == nil {
		return []string{"status", "pass"}
	}
	return []string{"status", "fail", "message", normalizeTypeError(err.Error())}
}

// normalizeTypeError maps type checker diagnostics into the self-host subset.
func normalizeTypeError(message string) string {
	if strings.Contains(message, "equal_bytes") {
		return "type error: `std::mem::equal_bytes` arg 2 expects []const u8"
	}
	if strings.Contains(message, "expects 2 args") {
		return "type error: call arg count"
	}
	if strings.Contains(message, "arg 1 of `take` expects i64") {
		return "type error: call arg type"
	}
	return "type error"
}

// formatTypeCheckSnapshot formats type checker pass/fail rows.
func formatTypeCheckSnapshot(lines []string) string {
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

// goIrDumpSnapshot returns the normalized Go IR lowering dump subset.
func goIrDumpSnapshot(t *testing.T, path string) irDumpSnapshot {
	t.Helper()
	program := parseSelfHostSource(t, path)
	if err := types.New().Check(program); err != nil {
		return irDumpSnapshot{Status: "fail", Message: "ir error: source is not lowerable"}
	}
	if err := ownership.New().Check(program); err != nil {
		return irDumpSnapshot{Status: "fail", Message: "ir error: source is not lowerable"}
	}
	module, err := kir.Lower(program)
	if err != nil {
		return irDumpSnapshot{Status: "fail", Message: "ir error: source is not lowerable"}
	}
	return goIrDumpFromModule(module)
}

// goIrDumpFromModule converts Go IR into the self-host normalized dump subset.
func goIrDumpFromModule(module *kir.Module) irDumpSnapshot {
	snapshot := irDumpSnapshot{Status: "pass"}
	for _, fn := range module.Functions {
		dump := irDumpFunction{Name: fn.Name, ReturnType: fn.Return}
		for _, param := range fn.Params {
			dump.Params = append(dump.Params, param.Type)
		}
		if len(fn.Blocks) > 0 {
			dump.Block = fn.Blocks[0].Name
			dump.Opcodes = irOpcodes(fn.Blocks[0])
			dump.Terminator = normalizeIrTerminator(fn.Blocks[0].Terminator)
		}
		snapshot.Functions = append(snapshot.Functions, dump)
	}
	return snapshot
}

// irOpcodes returns the instruction opcodes in one block.
func irOpcodes(block *kir.Block) []string {
	opcodes := make([]string, 0, len(block.Instrs))
	for _, instr := range block.Instrs {
		opcodes = append(opcodes, instr.Op)
	}
	return opcodes
}

// normalizeIrTerminator returns the schema shared with frontend.kizu.
func normalizeIrTerminator(term kir.Terminator) string {
	if term.Op == "return" && term.Value.Name == "void" {
		return "return void"
	}
	return term.Op
}

// formatIrDumpSnapshot formats normalized IR dump rows.
func formatIrDumpSnapshot(snapshot irDumpSnapshot) string {
	lines := []string{"status", snapshot.Status}
	if snapshot.Status == "fail" {
		lines = append(lines, "message", snapshot.Message)
		return strings.Join(lines, "\n") + "\n"
	}
	for _, fn := range snapshot.Functions {
		lines = append(lines,
			"fn", fn.Name,
			"return", fn.ReturnType,
			"params", strconv.Itoa(len(fn.Params)),
		)
		for _, param := range fn.Params {
			lines = append(lines, "param", param)
		}
		lines = append(lines, "block", fn.Block, "ops", strconv.Itoa(len(fn.Opcodes)))
		for _, opcode := range fn.Opcodes {
			lines = append(lines, "op", opcode)
		}
		lines = append(lines, "terminator", fn.Terminator)
	}
	return strings.Join(lines, "\n") + "\n"
}

// goBackendFingerprints returns backend smoke facts derived from Go emitters.
func goBackendFingerprints(t *testing.T, path string) []backendFingerprint {
	t.Helper()
	module, ok := goLowerableModule(t, path)
	if !ok {
		return []backendFingerprint{
			failingBackendFingerprint("llvm"),
			failingBackendFingerprint("wasm32-wasi"),
		}
	}
	return []backendFingerprint{
		goLLVMFingerprint(t, module),
		goWASMFingerprint(t, module),
	}
}

// goLowerableModule returns the checked Go IR module when the input is lowerable.
func goLowerableModule(t *testing.T, path string) (*kir.Module, bool) {
	t.Helper()
	program := parseSelfHostSource(t, path)
	if err := types.New().Check(program); err != nil {
		return nil, false
	}
	if err := ownership.New().Check(program); err != nil {
		return nil, false
	}
	module, err := kir.Lower(program)
	if err != nil {
		return nil, false
	}
	return module, true
}

// failingBackendFingerprint returns the normalized backend failure shape.
func failingBackendFingerprint(target string) backendFingerprint {
	return backendFingerprint{
		Target: target, Status: "fail", Message: "backend error: source is not lowerable",
	}
}

// goLLVMFingerprint emits LLVM and extracts the shared smoke schema.
func goLLVMFingerprint(t *testing.T, module *kir.Module) backendFingerprint {
	t.Helper()
	output, err := llvm.Emit(module)
	if err != nil {
		t.Fatalf("LLVM emit failed: %v", err)
	}
	if !strings.Contains(output, "define void @main()") {
		t.Fatalf("LLVM output has no main entry:\n%s", output)
	}
	return backendFingerprint{
		Target: "llvm", Status: "pass", Functions: len(module.Functions),
		Names: backendFunctionNames(module), Strings: countIrStringConstants(module), Entry: "main",
		Lines: []string{"; Kizu LLVM IR", "define void @main()"},
	}
}

// goWASMFingerprint emits WAT and extracts the shared smoke schema.
func goWASMFingerprint(t *testing.T, module *kir.Module) backendFingerprint {
	t.Helper()
	output, err := wasm.Emit(module)
	if err != nil {
		t.Fatalf("WASM emit failed: %v", err)
	}
	if !strings.Contains(output, `(func $_start (export "_start")`) {
		t.Fatalf("WASM output has no _start entry:\n%s", output)
	}
	return backendFingerprint{
		Target: "wasm32-wasi", Status: "pass", Functions: len(module.Functions),
		Names: backendFunctionNames(module), Strings: countIrStringConstants(module), Entry: "_start",
		Lines: []string{
			"import wasi_snapshot_preview1 fd_write",
			"func _start export _start",
		},
	}
}

// backendFunctionNames returns emitted function names in module order.
func backendFunctionNames(module *kir.Module) []string {
	names := make([]string, 0, len(module.Functions))
	for _, fn := range module.Functions {
		names = append(names, fn.Name)
	}
	return names
}

// countIrStringConstants counts source string constants that reach backend input.
func countIrStringConstants(module *kir.Module) int {
	count := 0
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "const" && instr.Result.Type == "[]const u8" {
					count++
				}
			}
		}
	}
	return count
}

// formatBackendFingerprints formats backend smoke rows as frontend.kizu prints them.
func formatBackendFingerprints(fingerprints []backendFingerprint) string {
	lines := []string{}
	for _, fingerprint := range fingerprints {
		lines = append(lines, "target", fingerprint.Target, "status", fingerprint.Status)
		if fingerprint.Status == "fail" {
			lines = append(lines, "message", fingerprint.Message)
			continue
		}
		lines = append(lines,
			"functions", strconv.Itoa(fingerprint.Functions),
		)
		for _, name := range fingerprint.Names {
			lines = append(lines, "function", name)
		}
		lines = append(lines,
			"strings", strconv.Itoa(fingerprint.Strings),
			"entry", fingerprint.Entry,
			"lines", strconv.Itoa(len(fingerprint.Lines)),
		)
		for _, line := range fingerprint.Lines {
			lines = append(lines, "line", line)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// formatCacheContractSnapshot formats the current Go-owned cache contract.
func formatCacheContractSnapshot() string {
	lines := []string{
		"owner", "go",
		"switch", "blocked",
		"required inputs",
		"compiler version",
		"target",
		"input kind",
		"source hash",
		"stdlib hash",
		"positive", "cache hit",
		"negative", "source changed",
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
