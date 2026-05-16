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

type runtimeDiagnosticCase struct {
	Path    string
	Literal string
	Message string
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
	Details    []irInstrDetail
	Terminator string
}

type irInstrDetail struct {
	Result    string
	Args      []string
	Immediate string
}

type backendFingerprint struct {
	Target       string
	Status       string
	Message      string
	Functions    int
	Names        []string
	Strings      int
	Instructions int
	Consts       int
	Calls        int
	Entry        string
	Lines        []string
}

type moduleGraphSnapshot struct {
	Status      string
	Message     string
	Root        string
	Modules     int
	ModulePaths []string
	Imports     []string
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
	return selfHostFrontendSmokeHeader(fixture) + selfHostFrontendSmokeSnapshots(t, fixture)
}

// selfHostFrontendSmokeHeader returns source and compiler summary rows.
func selfHostFrontendSmokeHeader(fixture string) string {
	return "source:simple.kizu\n" +
		filepath.ToSlash(filepath.Dir(fixture)) + "\n" +
		"compiler stages\n8\n" +
		"parsed functions\n2\n" +
		"tokens\n19\n" +
		"bootstrap ready\ntrue\n"
}

// selfHostFrontendSmokeSnapshots returns all phase snapshot rows.
func selfHostFrontendSmokeSnapshots(t *testing.T, fixture string) string {
	t.Helper()
	tokenStream := strings.Join(goSelfHostTokenKinds(t, fixture), "\n")
	snapshots := formatTokenSnapshots(goSelfHostTokenSnapshots(t, fixture))
	astSnapshot := formatAstSnapshot(goAstSnapshot(t, fixture))
	astDetailSnapshot := formatAstDetailSnapshot(goAstDetailSnapshot(t, fixture))
	astNodeDumpSnapshot := formatAstNodeDumpSnapshot(goAstNodeDumpSnapshot(t, fixture))
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
	return "token stream\n" +
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
		"ast node dump snapshot\n" +
		astNodeDumpSnapshot +
		"ast node dump snapshot end\n" +
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
	for _, item := range loadModuleConformanceManifest(t).Cases {
		t.Run(item.Name, func(t *testing.T) {
			root := filepath.Join(repoRoot(t), filepath.FromSlash(item.Path))
			source := filepath.Join(repoRoot(t), filepath.FromSlash(item.RootSource))
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
		"examples/if.kizu",
		"examples/while.kizu",
		"examples/for.kizu",
		"examples/match.kizu",
		"examples/variables.kizu",
		"examples/arithmetic.kizu",
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

// TestSelfHostAstNodeDumpOracle compares AST node dumps for every parseable fixture.
func TestSelfHostAstNodeDumpOracle(t *testing.T) {
	cases := selfHostParseableAstNodeDumpSources(t)
	for _, path := range cases {
		t.Run(astNodeDumpCaseName(t, path), func(t *testing.T) {
			fixture := path
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture),
				"ast node dump snapshot", "ast node dump snapshot end",
			)
			want := formatAstNodeDumpSnapshot(goAstNodeDumpSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host AST node dump got %q, want %q", got, want)
			}
		})
	}
}

// selfHostParseableAstNodeDumpSources returns every manifest source the Go parser accepts.
func selfHostParseableAstNodeDumpSources(t *testing.T) []string {
	t.Helper()
	paths := map[string]bool{
		filepath.Join(repoRoot(t), "selfhost", "fixtures", "simple.kizu"): true,
	}
	for _, path := range selfHostConformanceSources(t) {
		if sourceParses(t, path) {
			paths[path] = true
		}
	}
	return sortedMapKeys(paths)
}

// sourceParses reports whether the production Go parser accepts one source file.
func sourceParses(t *testing.T, path string) bool {
	t.Helper()
	l := lexer.New(readSource(t, path))
	p := parser.New(l)
	_ = p.ParseProgram()
	return len(p.Errors()) == 0
}

// astNodeDumpCaseName returns a stable, readable subtest name.
func astNodeDumpCaseName(t *testing.T, path string) string {
	t.Helper()
	name := strings.TrimPrefix(path, repoRoot(t)+string(filepath.Separator))
	return filepath.ToSlash(name)
}

// TestSelfHostDiagnosticObjectOracle compares structured diagnostics.
func TestSelfHostDiagnosticObjectOracle(t *testing.T) {
	for _, path := range selfHostDiagnosticObjectOracleCases() {
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

// TestSelfHostDiagnosticOracleCoversNegativeFixtures keeps failure classes listed.
func TestSelfHostDiagnosticOracleCoversNegativeFixtures(t *testing.T) {
	covered := map[string]bool{}
	for _, path := range selfHostDiagnosticObjectOracleCases() {
		covered[path] = true
	}
	glob := filepath.Join(repoRoot(t), "examples", "negative", "*.kizu")
	paths, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			t.Fatal(err)
		}
		slashPath := filepath.ToSlash(rel)
		if !covered[slashPath] {
			t.Fatalf("negative fixture is missing from diagnostic oracle: %s", slashPath)
		}
	}
}

// selfHostDiagnosticObjectOracleCases returns diagnostic oracle fixtures.
func selfHostDiagnosticObjectOracleCases() []string {
	cases := []string{
		"selfhost/fixtures/illegal_token.kizu",
		"examples/negative/missing_semicolon.kizu",
		"tests/conformance/modules/missing_import/src/main.kizu",
		"tests/conformance/modules/private_module_access/src/main.kizu",
		"tests/conformance/modules/private_type_leak/src/main.kizu",
		"tests/conformance/modules/private_field_construction/src/main.kizu",
	}
	cases = append(cases, selfHostTypeDiagnosticObjectOracleCases()...)
	cases = append(cases, selfHostOwnershipDiagnosticObjectOracleCases()...)
	cases = append(cases, selfHostRuntimeDiagnosticObjectOracleCases()...)
	return cases
}

// selfHostTypeDiagnosticObjectOracleCases returns type diagnostic fixtures.
func selfHostTypeDiagnosticObjectOracleCases() []string {
	cases := []string{
		"examples/negative/std_mem_wrong_type.kizu",
		"selfhost/fixtures/type_arg_count.kizu",
		"selfhost/fixtures/type_arg_type.kizu",
		"examples/negative/invalid_field.kizu",
		"examples/negative/if_expression_type_mismatch.kizu",
		"examples/negative/if_expression_missing_else.kizu",
		"examples/negative/empty_return_value.kizu",
		"examples/negative/missing_return.kizu",
		"examples/negative/invalid_cast.kizu",
		"examples/negative/break_outside_loop.kizu",
		"examples/negative/continue_outside_loop.kizu",
		"examples/negative/unknown_loop_label.kizu",
		"examples/negative/label_on_non_loop.kizu",
		"examples/negative/enum_dot_variant.kizu",
		"examples/negative/union_dot_variant.kizu",
		"examples/negative/match_non_exhaustive.kizu",
		"examples/negative/match_duplicate_tag.kizu",
		"examples/negative/match_unknown_tag.kizu",
		"examples/negative/typed_error_mismatch.kizu",
		"examples/negative/typed_error_untyped_constructor.kizu",
		"examples/negative/comptime_borrow_escape.kizu",
		"examples/negative/missing_contract_method.kizu",
		"examples/negative/owned_dyn.kizu",
		"examples/negative/unsatisfied_dyn.kizu",
		"examples/contract_writer.kizu",
		"examples/negative/unsafe_call.kizu",
		"examples/negative/unsafe_call_after_block.kizu",
		"examples/negative/ptr_read_without_unsafe.kizu",
		"examples/negative/ptr_read_unrelated_nullable.kizu",
		"examples/negative/ptr_read_unrelated_nullable_source.kizu",
		"examples/negative/nullable_ptr_read.kizu",
		"examples/negative/handle_as_pointer.kizu",
		"examples/negative/io_builtin_constructor.kizu",
		"examples/negative/io_evented_unimplemented.kizu",
		"examples/negative/fs_read_without_io.kizu",
		"examples/negative/fs_write_wrong_bytes.kizu",
		"examples/negative/std_fs_exists_without_io.kizu",
		"examples/negative/std_path_wrong_type.kizu",
		"examples/fs_read.kizu",
		"examples/std_fs_path.kizu",
		"examples/std_path.kizu",
		"examples/negative/std_map_wrong_key_type.kizu",
		"examples/negative/std_map_wrong_insert_type.kizu",
		"examples/negative/std_string_wrong_append_type.kizu",
		"examples/negative/std_testing_wrong_type.kizu",
		"examples/negative/std_string_no_allocator.kizu",
		"examples/negative/std_map_no_allocator.kizu",
		"examples/negative/std_string_append_through_shared_borrow.kizu",
		"examples/negative/std_string_deinit_through_shared_borrow.kizu",
		"examples/negative/std_string_deinit_through_mut_borrow.kizu",
		"examples/negative/std_string_as_bytes_direct_use.kizu",
		"examples/negative/std_string_as_bytes_return_escape.kizu",
		"examples/negative/std_string_append_byte_wrong_type.kizu",
		"examples/negative/std_map_insert_through_shared_borrow.kizu",
		"examples/negative/std_map_deinit_through_shared_borrow.kizu",
		"examples/negative/std_map_deinit_through_mut_borrow.kizu",
		"examples/negative/std_map_non_copy_value.kizu",
		"examples/unsafe_nested_block.kizu",
		"examples/unsafe_ptr_read_with_unrelated_nullable_source.kizu",
	}
	cases = append(cases, selfHostArrayTypeDiagnosticCases()...)
	cases = append(cases, selfHostConcurrencyTypeDiagnosticCases()...)
	return cases
}

// selfHostArrayTypeDiagnosticCases returns Array type diagnostic fixtures.
func selfHostArrayTypeDiagnosticCases() []string {
	return []string{
		"examples/negative/std_array_wrong_type.kizu",
		"examples/negative/std_array_at_mut_immutable.kizu",
		"examples/negative/std_array_at_mut_unrelated_var.kizu",
		"examples/negative/std_array_at_pass_to_owned_param.kizu",
		"examples/negative/std_array_at_return_escape.kizu",
		"examples/negative/std_array_atomic_element.kizu",
		"examples/negative/std_array_channel_send.kizu",
		"examples/negative/std_array_get_non_copy.kizu",
		"examples/negative/std_array_handle_element.kizu",
		"examples/negative/std_array_map_element.kizu",
		"examples/negative/std_array_no_allocator.kizu",
		"examples/negative/std_array_raw_pointer_element.kizu",
		"examples/negative/std_array_struct_channel_element.kizu",
		"examples/negative/std_array_struct_nested_array_element.kizu",
		"examples/negative/std_array_struct_raw_pointer_element.kizu",
		"examples/negative/std_array_task_spawn.kizu",
		"examples/negative/std_array_union_handle_element.kizu",
		"examples/std_array_borrow_len.kizu",
		"examples/std_array_holder_with_unrelated_values.kizu",
		"examples/std_array_with_unrelated_pointer.kizu",
	}
}

// selfHostConcurrencyTypeDiagnosticCases returns concurrency type fixtures.
func selfHostConcurrencyTypeDiagnosticCases() []string {
	return []string{
		"examples/negative/atomic_unsupported_type.kizu",
		"examples/negative/atomic_untyped_constructor.kizu",
		"examples/negative/atomic_old_name.kizu",
		"examples/negative/atomic_store_wrong_type.kizu",
		"examples/negative/channel_untyped_constructor.kizu",
		"examples/negative/channel_send_borrow.kizu",
		"examples/negative/channel_send_wrong_type.kizu",
		"examples/negative/channel_send_pointer.kizu",
		"examples/negative/mutex_untyped_constructor.kizu",
		"examples/negative/mutex_wrong_type.kizu",
		"examples/negative/mutex_non_copy.kizu",
		"examples/negative/mutex_pointer.kizu",
		"examples/negative/std_map_channel_send.kizu",
		"examples/negative/std_map_task_spawn.kizu",
		"examples/negative/task_group_without_io.kizu",
		"examples/negative/task_borrow_capture.kizu",
		"examples/negative/task_spawn_borrowed_io.kizu",
		"examples/negative/task_spawn_mut_borrowed_io.kizu",
		"examples/negative/task_spawn_old_io_arg.kizu",
		"examples/negative/task_spawn_pointer.kizu",
		"examples/negative/task_spawn_struct_pointer.kizu",
		"examples/negative/task_spawn_arena.kizu",
		"examples/negative/task_spawn_handle.kizu",
		"examples/negative/task_spawn_mutex.kizu",
		"examples/negative/queue_borrow_capture.kizu",
		"examples/negative/queue_enqueue_pointer.kizu",
		"examples/negative/thread_borrow_capture.kizu",
		"examples/negative/thread_scoped_pointer.kizu",
		"examples/negative/thread_scoped_mutex.kizu",
		"examples/negative/parallel_shared_mutable.kizu",
		"examples/negative/parallel_map_wrong_worker.kizu",
		"examples/negative/partition_mut_non_i64.kizu",
	}
}

// selfHostOwnershipDiagnosticObjectOracleCases returns ownership fixtures.
func selfHostOwnershipDiagnosticObjectOracleCases() []string {
	return []string{
		"examples/move_error.kizu",
		"examples/negative/moved_value.kizu",
		"examples/negative/double_move.kizu",
		"examples/negative/assignment_move.kizu",
		"examples/negative/channel_send_move.kizu",
		"examples/negative/if_branch_move.kizu",
		"examples/negative/if_branch_partial_move.kizu",
		"examples/negative/if_expression_branch_move.kizu",
		"examples/negative/while_body_move.kizu",
		"examples/negative/move_while_borrowed.kizu",
		"examples/negative/unsafe_moved_value.kizu",
		"examples/negative/borrow_before_last_use_move.kizu",
		"examples/negative/borrow_loop_last_use.kizu",
		"examples/negative/field_borrow_owner_move.kizu",
		"examples/negative/field_borrow_same_field_assignment.kizu",
		"examples/negative/borrow_escape.kizu",
		"examples/negative/borrow_field.kizu",
		"examples/negative/borrow_local_alias.kizu",
		"examples/negative/borrow_to_owner.kizu",
		"examples/negative/borrow_deref_move.kizu",
		"examples/negative/mut_borrow_conflict.kizu",
		"examples/negative/mut_borrow_deref_move.kizu",
		"examples/negative/std_array_use_after_deinit.kizu",
		"examples/negative/std_array_append_moves.kizu",
		"examples/negative/std_array_append_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_append_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_deinit_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_set_while_borrowed.kizu",
		"examples/negative/std_array_deinit_while_borrowed.kizu",
		"examples/negative/std_array_read_while_mut_borrowed.kizu",
		"examples/negative/std_array_set_while_borrowed.kizu",
		"examples/negative/std_string_use_after_deinit.kizu",
		"examples/negative/std_map_use_after_deinit.kizu",
		"examples/negative/task_move.kizu",
		"examples/negative/task_await_after_cancel.kizu",
		"examples/negative/task_cancel_after_await.kizu",
		"examples/negative/unawaited_task.kizu",
		"examples/negative/std_string_append_while_viewed.kizu",
		"examples/negative/std_string_clear_while_viewed.kizu",
		"examples/negative/std_string_deinit_while_viewed.kizu",
		"examples/negative/arena_wrong_handle.kizu",
		"examples/negative/arena_inline_wrong_handle.kizu",
		"examples/negative/arena_unknown_handle.kizu",
		"examples/negative/arena_handle_outlive.kizu",
		"examples/negative/arena_add_move.kizu",
		"examples/negative/arena_get_move.kizu",
		"examples/negative/mut_borrow_immutable.kizu",
		"examples/negative/nested_field_borrow.kizu",
		"examples/negative/shared_borrow_assignment.kizu",
		"examples/negative/immutable_assignment.kizu",
		"examples/negative/immutable_field_assignment.kizu",
		"examples/negative/invalid_try.kizu",
		"examples/negative/unsafe_borrow_escape.kizu",
	}
}

// selfHostRuntimeDiagnosticObjectOracleCases returns runtime failure fixtures.
func selfHostRuntimeDiagnosticObjectOracleCases() []string {
	cases := make([]string, 0, len(runtimeDiagnosticCases()))
	for _, item := range runtimeDiagnosticCases() {
		cases = append(cases, item.Path)
	}
	return cases
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
	cases := []string{
		"selfhost/fixtures/type_env.kizu",
		"selfhost/fixtures/type_env_stdlib.kizu",
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture := filepath.Join(repoRoot(t), filepath.FromSlash(path))
			got := extractMarkedSnapshot(
				t, runSelfHostFrontend(t, fixture), "type env snapshot", "type env snapshot end",
			)
			want := formatTypeEnvSnapshot(goLocalTypeEnvSnapshot(t, fixture))
			if got != want {
				t.Fatalf("self-host type env snapshot got %q, want %q", got, want)
			}
		})
	}
}

// TestSelfHostTypeCheckOracle compares a selected type pass/fail subset.
func TestSelfHostTypeCheckOracle(t *testing.T) {
	cases := []string{
		"examples/negative/std_mem_wrong_type.kizu",
		"selfhost/fixtures/type_arg_count.kizu",
		"selfhost/fixtures/type_arg_type.kizu",
		"examples/negative/invalid_field.kizu",
		"examples/negative/if_expression_type_mismatch.kizu",
		"examples/negative/empty_return_value.kizu",
		"examples/negative/missing_return.kizu",
		"examples/negative/invalid_cast.kizu",
		"examples/negative/comptime_borrow_escape.kizu",
		"examples/negative/missing_contract_method.kizu",
		"examples/negative/owned_dyn.kizu",
		"examples/negative/unsatisfied_dyn.kizu",
		"examples/negative/io_builtin_constructor.kizu",
		"examples/negative/io_evented_unimplemented.kizu",
		"examples/negative/fs_read_without_io.kizu",
		"examples/negative/fs_write_wrong_bytes.kizu",
		"examples/negative/std_fs_exists_without_io.kizu",
		"examples/negative/std_path_wrong_type.kizu",
		"examples/negative/std_array_wrong_type.kizu",
		"examples/negative/std_array_at_mut_immutable.kizu",
		"examples/negative/std_array_at_mut_unrelated_var.kizu",
		"examples/negative/std_array_at_pass_to_owned_param.kizu",
		"examples/negative/std_array_at_return_escape.kizu",
		"examples/negative/std_array_atomic_element.kizu",
		"examples/negative/std_array_channel_send.kizu",
		"examples/negative/std_array_get_non_copy.kizu",
		"examples/negative/std_array_handle_element.kizu",
		"examples/negative/std_array_map_element.kizu",
		"examples/negative/std_array_raw_pointer_element.kizu",
		"examples/negative/std_array_struct_channel_element.kizu",
		"examples/negative/std_array_struct_nested_array_element.kizu",
		"examples/negative/std_array_struct_raw_pointer_element.kizu",
		"examples/negative/std_array_task_spawn.kizu",
		"examples/negative/std_array_union_handle_element.kizu",
		"examples/negative/std_map_wrong_key_type.kizu",
		"examples/negative/std_map_wrong_insert_type.kizu",
		"examples/negative/std_string_wrong_append_type.kizu",
		"examples/negative/std_testing_wrong_type.kizu",
		"examples/negative/std_array_no_allocator.kizu",
		"examples/negative/std_string_no_allocator.kizu",
		"examples/negative/std_map_no_allocator.kizu",
		"examples/negative/std_string_append_through_shared_borrow.kizu",
		"examples/negative/std_string_deinit_through_shared_borrow.kizu",
		"examples/negative/std_string_deinit_through_mut_borrow.kizu",
		"examples/negative/std_string_as_bytes_direct_use.kizu",
		"examples/negative/std_string_as_bytes_return_escape.kizu",
		"examples/negative/std_string_append_byte_wrong_type.kizu",
		"examples/negative/std_map_insert_through_shared_borrow.kizu",
		"examples/negative/std_map_deinit_through_shared_borrow.kizu",
		"examples/negative/std_map_deinit_through_mut_borrow.kizu",
		"examples/negative/std_map_non_copy_value.kizu",
		"examples/task_queue.kizu",
		"examples/parallel_for.kizu",
		"examples/parallel_map_with_unrelated_bytes.kizu",
	}
	cases = append(cases, selfHostConcurrencyTypeDiagnosticCases()...)
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
		"examples/channel_send_copy.kizu",
		"examples/negative/moved_value.kizu",
		"examples/negative/double_move.kizu",
		"examples/negative/assignment_move.kizu",
		"examples/negative/channel_send_move.kizu",
		"examples/negative/if_branch_move.kizu",
		"examples/negative/move_while_borrowed.kizu",
		"examples/negative/if_expression_branch_move.kizu",
		"examples/negative/borrow_before_last_use_move.kizu",
		"examples/negative/borrow_loop_last_use.kizu",
		"examples/negative/field_borrow_owner_move.kizu",
		"examples/negative/field_borrow_same_field_assignment.kizu",
		"examples/negative/borrow_escape.kizu",
		"examples/negative/borrow_field.kizu",
		"examples/negative/borrow_local_alias.kizu",
		"examples/negative/borrow_to_owner.kizu",
		"examples/negative/borrow_deref_move.kizu",
		"examples/negative/mut_borrow_conflict.kizu",
		"examples/negative/mut_borrow_deref_move.kizu",
		"examples/negative/std_array_use_after_deinit.kizu",
		"examples/negative/std_array_append_moves.kizu",
		"examples/negative/std_array_append_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_append_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_deinit_while_borrowed.kizu",
		"examples/negative/std_array_at_mut_set_while_borrowed.kizu",
		"examples/negative/std_array_deinit_while_borrowed.kizu",
		"examples/negative/std_array_read_while_mut_borrowed.kizu",
		"examples/negative/std_array_set_while_borrowed.kizu",
		"examples/negative/std_string_use_after_deinit.kizu",
		"examples/negative/std_map_use_after_deinit.kizu",
		"examples/negative/task_move.kizu",
		"examples/negative/task_await_after_cancel.kizu",
		"examples/negative/task_cancel_after_await.kizu",
		"examples/negative/unawaited_task.kizu",
		"examples/negative/std_string_append_while_viewed.kizu",
		"examples/negative/std_string_clear_while_viewed.kizu",
		"examples/negative/std_string_deinit_while_viewed.kizu",
		"examples/negative/arena_wrong_handle.kizu",
		"examples/negative/arena_inline_wrong_handle.kizu",
		"examples/negative/arena_unknown_handle.kizu",
		"examples/negative/arena_handle_outlive.kizu",
		"examples/negative/arena_add_move.kizu",
		"examples/negative/arena_get_move.kizu",
		"examples/negative/unsafe_borrow_escape.kizu",
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
		"examples/functions.kizu",
		"examples/arithmetic.kizu",
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
	snapshots = append(snapshots, goOwnershipDiagnosticSnapshots(t, path)...)
	snapshots = append(snapshots, goRuntimeDiagnosticSnapshots(t, path)...)
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
	slashPath := filepath.ToSlash(path)
	if strings.Contains(slashPath, "if_expression_missing_else") {
		tok := tokenWithLiteralOccurrence(t, path, ";", 2)
		return []diagnosticSnapshot{
			diagnosticFromToken("parser error: if expression missing else", tok, tok),
		}
	}
	if strings.Contains(slashPath, "label_on_non_loop") {
		tok := tokenWithLiteral(t, path, "print")
		return []diagnosticSnapshot{diagnosticFromToken("parser error: label on non-loop", tok, tok)}
	}
	if !strings.Contains(slashPath, "missing_semicolon") {
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
	slashPath := filepath.ToSlash(path)
	if strings.Contains(slashPath, "missing_import") {
		token := tokenWithLiteral(t, path, "missing")
		return []diagnosticSnapshot{{
			Message:      "module error: missing module",
			PrimaryStart: token.Start, PrimaryEnd: token.End,
			PrimaryLine: token.Line, PrimaryColumn: token.Column,
			RelatedStart: token.Start, RelatedEnd: token.End,
		}}
	}
	if strings.Contains(slashPath, "private_module_access") {
		return moduleVisibilityDiagnostic(t, path, "hidden")
	}
	if strings.Contains(slashPath, "private_type_leak") {
		return moduleVisibilityDiagnostic(t, path, "Secret")
	}
	if strings.Contains(slashPath, "private_field_construction") {
		return moduleVisibilityDiagnostic(t, path, "secret")
	}
	return nil
}

// moduleVisibilityDiagnostic returns the shared module visibility diagnostic row.
func moduleVisibilityDiagnostic(t *testing.T, path string, literal string) []diagnosticSnapshot {
	t.Helper()
	token := tokenWithLiteral(t, path, literal)
	return []diagnosticSnapshot{{
		Message:      "module error",
		PrimaryStart: token.Start, PrimaryEnd: token.End,
		PrimaryLine: token.Line, PrimaryColumn: token.Column,
		RelatedStart: token.Start, RelatedEnd: token.End,
	}}
}

// goTypeDiagnosticSnapshots returns type checker diagnostics in the self-host subset.
func goTypeDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	slashPath := filepath.ToSlash(path)
	if snapshots := goNamedTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if !strings.Contains(slashPath, "std_mem_wrong_type") {
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

// goNamedTypeDiagnosticSnapshots returns fixture-named type diagnostic rows.
func goNamedTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if snapshots := goCoreTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if snapshots := goAdvancedTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if snapshots := goStdlibTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if snapshots := goConcurrencyTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	return goBorrowAndBasicTypeDiagnosticSnapshots(t, path, slashPath)
}

// goCoreTypeDiagnosticSnapshots returns core type diagnostic fixture rows.
func goCoreTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "type_arg_count") {
		tok := lastTokenWithLiteral(t, path, "add")
		return []diagnosticSnapshot{diagnosticFromToken("type error: call arg count", tok, tok)}
	}
	if strings.Contains(slashPath, "type_arg_type") {
		tok := tokenWithLiteral(t, path, "no")
		return []diagnosticSnapshot{diagnosticFromToken("type error: call arg type", tok, tok)}
	}
	if strings.Contains(slashPath, "invalid_field") {
		tok := tokenWithLiteral(t, path, "age")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: unknown field", tok, tok),
		}
	}
	if strings.Contains(slashPath, "if_expression_type_mismatch") {
		tok := tokenWithLiteral(t, path, "one")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: if expression branch types differ", tok, tok),
		}
	}
	if strings.Contains(slashPath, "empty_return_value") {
		tok := tokenWithLiteral(t, path, "return")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: return type mismatch", tok, tok),
		}
	}
	if strings.Contains(slashPath, "missing_return") {
		tok := tokenWithLiteral(t, path, "bad")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: missing return", tok, tok),
		}
	}
	if strings.Contains(slashPath, "invalid_cast") {
		tok := tokenWithLiteral(t, path, "no")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: invalid cast", tok, tok),
		}
	}
	return nil
}

// goAdvancedTypeDiagnosticSnapshots returns match, typed-error, and unsafe diagnostics.
func goAdvancedTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if snapshots := goControlVariantTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if strings.Contains(slashPath, "match_non_exhaustive") {
		tok := tokenWithLiteral(t, path, "match")
		return []diagnosticSnapshot{diagnosticFromToken("type error: match not exhaustive", tok, tok)}
	}
	if strings.Contains(slashPath, "match_duplicate_tag") {
		tok := tokenWithLiteralOccurrence(t, path, "Red", 4)
		return []diagnosticSnapshot{diagnosticFromToken("type error: duplicate match tag", tok, tok)}
	}
	if strings.Contains(slashPath, "match_unknown_tag") {
		tok := tokenWithLiteral(t, path, "Blue")
		return []diagnosticSnapshot{diagnosticFromToken("type error: unknown match tag", tok, tok)}
	}
	return goErrorUnsafeTypeDiagnosticSnapshots(t, path, slashPath)
}

// goControlVariantTypeDiagnosticSnapshots returns control and namespace diagnostics.
func goControlVariantTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "break_outside_loop") {
		tok := tokenWithLiteral(t, path, "break")
		return []diagnosticSnapshot{diagnosticFromToken("type error: break outside loop", tok, tok)}
	}
	if strings.Contains(slashPath, "continue_outside_loop") {
		tok := tokenWithLiteral(t, path, "continue")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: continue outside loop", tok, tok),
		}
	}
	if strings.Contains(slashPath, "unknown_loop_label") {
		tok := tokenWithLiteral(t, path, "missing")
		return []diagnosticSnapshot{diagnosticFromToken("type error: unknown loop label", tok, tok)}
	}
	if strings.Contains(slashPath, "enum_dot_variant") {
		tok := tokenWithLiteralOccurrence(t, path, "Color", 2)
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: enum variant must use namespace", tok, tok),
		}
	}
	if strings.Contains(slashPath, "union_dot_variant") {
		tok := tokenWithLiteralOccurrence(t, path, "Shape", 2)
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: union variant must use namespace", tok, tok),
		}
	}
	return nil
}

// goErrorUnsafeTypeDiagnosticSnapshots returns typed-error and unsafe diagnostics.
func goErrorUnsafeTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "typed_error_mismatch") {
		tok := tokenWithLiteral(t, path, "try")
		return []diagnosticSnapshot{diagnosticFromToken("type error: typed error mismatch", tok, tok)}
	}
	if strings.Contains(slashPath, "typed_error_untyped_constructor") {
		tok := tokenWithLiteral(t, path, "error")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: typed error cannot use untyped constructor", tok, tok),
		}
	}
	if strings.Contains(slashPath, "comptime_borrow_escape") {
		return goLastLiteralDiagnostic(t, path, "comptime", "comptime error: runtime value")
	}
	if strings.Contains(slashPath, "missing_contract_method") {
		return goLastLiteralDiagnostic(t, path, "File", "type error: contract missing method")
	}
	if strings.Contains(slashPath, "owned_dyn") {
		return goLastLiteralDiagnostic(t, path, "writer", "type error: Dyn parameter borrowed")
	}
	if strings.Contains(slashPath, "unsatisfied_dyn") {
		return goLastLiteralDiagnostic(t, path, "file", "type error: Dyn not satisfied")
	}
	return goUnsafeTypeDiagnosticSnapshots(t, path, slashPath)
}

// goUnsafeTypeDiagnosticSnapshots returns unsafe and pointer diagnostics.
func goUnsafeTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "unsafe_call") {
		tok := lastTokenWithLiteral(t, path, "source")
		return []diagnosticSnapshot{diagnosticFromToken("unsafe error: call requires unsafe", tok, tok)}
	}
	if strings.Contains(slashPath, "ptr_read_without_unsafe") ||
		strings.Contains(slashPath, "ptr_read_unrelated_nullable") {
		tok := tokenWithLiteral(t, path, "ptr_read")
		return []diagnosticSnapshot{
			diagnosticFromToken("unsafe error: ptr_read requires unsafe", tok, tok),
		}
	}
	if strings.Contains(slashPath, "nullable_ptr_read") {
		tok := tokenWithLiteral(t, path, "ptr_read")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: ptr_read nullable pointer", tok, tok),
		}
	}
	if strings.Contains(slashPath, "handle_as_pointer") {
		tok := tokenWithLiteral(t, path, "cast")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: handle cannot cast to pointer", tok, tok),
		}
	}
	return nil
}

// goStdlibTypeDiagnosticSnapshots returns selected stdlib type diagnostics.
func goStdlibTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if snapshots := goIoFsTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if snapshots := goStdArrayTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if strings.Contains(slashPath, "std_array_wrong_type") {
		tok := tokenWithLiteral(t, path, "no")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Array.append type", tok, tok)}
	}
	if strings.Contains(slashPath, "std_map_wrong_key_type") {
		tok := tokenWithLiteral(t, path, "Map")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Map key type", tok, tok)}
	}
	if strings.Contains(slashPath, "std_map_wrong_insert_type") {
		tok := tokenWithLiteral(t, path, "function")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Map.insert type", tok, tok)}
	}
	if strings.Contains(slashPath, "std_string_wrong_append_type") {
		tok := tokenWithLiteral(t, path, "1")
		return []diagnosticSnapshot{diagnosticFromToken("type error: String.append_bytes type", tok, tok)}
	}
	if strings.Contains(slashPath, "std_testing_wrong_type") {
		tok := tokenWithLiteral(t, path, "four")
		return []diagnosticSnapshot{diagnosticFromToken("type error: testing arg type", tok, tok)}
	}
	if strings.Contains(slashPath, "std_array_no_allocator") {
		tok := tokenWithLiteral(t, path, "Array")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Array allocator", tok, tok)}
	}
	if strings.Contains(slashPath, "std_string_no_allocator") {
		tok := tokenWithLiteral(t, path, "String")
		return []diagnosticSnapshot{diagnosticFromToken("type error: String allocator", tok, tok)}
	}
	if strings.Contains(slashPath, "std_map_no_allocator") {
		tok := tokenWithLiteral(t, path, "Map")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Map allocator", tok, tok)}
	}
	if snapshots := goStdResourceTypeDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	return nil
}

// goIoFsTypeDiagnosticSnapshots returns selected Io, fs, and path diagnostics.
func goIoFsTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "io_builtin_constructor") {
		return goLastLiteralDiagnostic(t, path, "Io", "type error: Io constructor")
	}
	if strings.Contains(slashPath, "io_evented_unimplemented") {
		return goLastLiteralDiagnostic(t, path, "evented", "type error: Io evented")
	}
	if strings.Contains(slashPath, "fs_read_without_io") {
		return goLastLiteralDiagnostic(t, path, "read_file", "type error: fs.read_file Io")
	}
	if strings.Contains(slashPath, "fs_write_wrong_bytes") {
		return goLastLiteralDiagnostic(t, path, "write_file", "type error: fs.write_file bytes")
	}
	if strings.Contains(slashPath, "std_fs_exists_without_io") {
		tok := tokenWithLiteral(t, path, "exists")
		return []diagnosticSnapshot{diagnosticFromToken("type error: fs.exists Io", tok, tok)}
	}
	if strings.Contains(slashPath, "std_path_wrong_type") {
		return goLastLiteralDiagnostic(t, path, "basename", "type error: path.basename arg")
	}
	return nil
}

// goStdArrayTypeDiagnosticSnapshots returns selected Array resource diagnostics.
func goStdArrayTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "std_array_at_mut_immutable") ||
		strings.Contains(slashPath, "std_array_at_mut_unrelated_var") {
		return goLastLiteralDiagnostic(t, path, "at_mut", "type error: Array.at_mut mutability")
	}
	if strings.Contains(slashPath, "std_array_at_pass_to_owned_param") ||
		strings.Contains(slashPath, "std_array_at_return_escape") {
		return goLastLiteralDiagnostic(t, path, "at", "type error: Array.at bind required")
	}
	if strings.Contains(slashPath, "std_array_get_non_copy") {
		return goLastLiteralDiagnostic(t, path, "get", "type error: Array.get copy element")
	}
	if strings.Contains(slashPath, "std_array_channel_send") ||
		strings.Contains(slashPath, "std_array_task_spawn") {
		return goLastLiteralDiagnostic(t, path, "values", "type error: Array concurrency boundary")
	}
	return goStdArrayElementTypeDiagnosticSnapshots(t, path, slashPath)
}

// goStdArrayElementTypeDiagnosticSnapshots returns Array element safety rows.
func goStdArrayElementTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "std_array_atomic_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element Atomic")
	}
	if strings.Contains(slashPath, "std_array_handle_element") ||
		strings.Contains(slashPath, "std_array_union_handle_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element handle")
	}
	if strings.Contains(slashPath, "std_array_map_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element Map")
	}
	if strings.Contains(slashPath, "std_array_raw_pointer_element") ||
		strings.Contains(slashPath, "std_array_struct_raw_pointer_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element raw pointer")
	}
	if strings.Contains(slashPath, "std_array_struct_channel_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element Channel")
	}
	if strings.Contains(slashPath, "std_array_struct_nested_array_element") {
		return goLastLiteralDiagnostic(t, path, "Array", "type error: Array element nested array")
	}
	return nil
}

// goStdResourceTypeDiagnosticSnapshots returns String/Map resource diagnostics.
func goStdResourceTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "std_string_append_through_shared_borrow") {
		return goLastLiteralDiagnostic(t, path, "append_bytes", "type error: String mutable receiver")
	}
	if strings.Contains(slashPath, "std_string_deinit_through_shared_borrow") ||
		strings.Contains(slashPath, "std_string_deinit_through_mut_borrow") {
		return goLastLiteralDiagnostic(t, path, "deinit", "type error: String owned receiver")
	}
	if strings.Contains(slashPath, "std_string_as_bytes_direct_use") ||
		strings.Contains(slashPath, "std_string_as_bytes_return_escape") {
		return goLastLiteralDiagnostic(t, path, "as_bytes", "type error: String.as_bytes bind required")
	}
	return goStdMapResourceTypeDiagnosticSnapshots(t, path, slashPath)
}

// goStdMapResourceTypeDiagnosticSnapshots returns Map resource diagnostics.
func goStdMapResourceTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "std_string_append_byte_wrong_type") {
		return goLastLiteralDiagnostic(t, path, "x", "type error: String.append_byte type")
	}
	if strings.Contains(slashPath, "std_map_insert_through_shared_borrow") {
		return goLastLiteralDiagnostic(t, path, "insert", "type error: Map mutable receiver")
	}
	if strings.Contains(slashPath, "std_map_deinit_through_shared_borrow") ||
		strings.Contains(slashPath, "std_map_deinit_through_mut_borrow") {
		return goLastLiteralDiagnostic(t, path, "deinit", "type error: Map owned receiver")
	}
	if strings.Contains(slashPath, "std_map_non_copy_value") {
		return goLastLiteralDiagnostic(t, path, "Map", "type error: Map value copy")
	}
	return nil
}

// goConcurrencyTypeDiagnosticSnapshots returns selected concurrency diagnostics.
func goConcurrencyTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if snapshots := goConcurrencyConstructorDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	if snapshots := goConcurrencyMethodDiagnosticSnapshots(t, path, slashPath); snapshots != nil {
		return snapshots
	}
	return goConcurrencyBoundaryDiagnosticSnapshots(t, path, slashPath)
}

// goConcurrencyConstructorDiagnosticSnapshots returns constructor diagnostics.
func goConcurrencyConstructorDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "atomic_unsupported_type") {
		tok := tokenWithLiteral(t, path, "Atomic")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Atomic unsupported type", tok, tok)}
	}
	if strings.Contains(slashPath, "atomic_old_name") {
		tok := tokenWithLiteral(t, path, "AtomicI64")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Atomic old name", tok, tok)}
	}
	if strings.Contains(slashPath, "atomic_untyped_constructor") {
		tok := tokenWithLiteral(t, path, "Atomic")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: Atomic type argument required", tok, tok),
		}
	}
	if strings.Contains(slashPath, "channel_untyped_constructor") {
		tok := tokenWithLiteral(t, path, "Channel")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: Channel type argument required", tok, tok),
		}
	}
	return goMutexAndTaskConstructorDiagnosticSnapshots(t, path, slashPath)
}

// goMutexAndTaskConstructorDiagnosticSnapshots returns mutex/task constructor rows.
func goMutexAndTaskConstructorDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "mutex_untyped_constructor") {
		tok := tokenWithLiteral(t, path, "Mutex")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: Mutex type argument required", tok, tok),
		}
	}
	if strings.Contains(slashPath, "mutex_wrong_type") {
		tok := tokenWithLiteral(t, path, "bad")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Mutex constructor type", tok, tok)}
	}
	if strings.Contains(slashPath, "mutex_non_copy") {
		tok := tokenWithLiteral(t, path, "Mutex")
		return []diagnosticSnapshot{diagnosticFromToken("type error: Mutex requires copy", tok, tok)}
	}
	if strings.Contains(slashPath, "task_group_without_io") {
		tok := tokenWithLiteral(t, path, "Group")
		return []diagnosticSnapshot{diagnosticFromToken("type error: task group expects io", tok, tok)}
	}
	return nil
}

// goConcurrencyMethodDiagnosticSnapshots returns method type diagnostics.
func goConcurrencyMethodDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "atomic_store_wrong_type") {
		tok := tokenWithLiteral(t, path, "bad")
		return []diagnosticSnapshot{diagnosticFromToken("type error: atomic.store type", tok, tok)}
	}
	if strings.Contains(slashPath, "channel_send_wrong_type") {
		tok := tokenWithLiteral(t, path, "bad")
		return []diagnosticSnapshot{diagnosticFromToken("type error: channel.send type", tok, tok)}
	}
	return nil
}

// goConcurrencyBoundaryDiagnosticSnapshots returns boundary diagnostics.
func goConcurrencyBoundaryDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "channel_send_borrow") {
		return goLastLiteralDiagnostic(t, path, "name", "type error: borrow concurrency boundary")
	}
	if strings.Contains(slashPath, "channel_send_pointer") {
		return goLastLiteralDiagnostic(t, path, "p", "type error: raw pointer concurrency boundary")
	}
	if strings.Contains(slashPath, "mutex_pointer") ||
		strings.Contains(slashPath, "queue_enqueue_pointer") {
		return goLastLiteralDiagnostic(t, path, "p", "type error: raw pointer concurrency boundary")
	}
	if strings.Contains(slashPath, "task_spawn_struct_pointer") {
		return goLastLiteralDiagnostic(t, path, "cell", "type error: raw pointer concurrency boundary")
	}
	if strings.Contains(slashPath, "task_spawn_pointer") {
		return goLastLiteralDiagnostic(t, path, "p", "type error: raw pointer concurrency boundary")
	}
	if strings.Contains(slashPath, "thread_scoped_pointer") {
		return goLastLiteralDiagnostic(t, path, "p", "type error: raw pointer concurrency boundary")
	}
	return goTaskThreadBoundaryDiagnosticSnapshots(t, path, slashPath)
}

// goTaskThreadBoundaryDiagnosticSnapshots returns task/thread boundary rows.
func goTaskThreadBoundaryDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "task_spawn_arena") {
		return goLastLiteralDiagnostic(t, path, "users", "type error: arena concurrency boundary")
	}
	if strings.Contains(slashPath, "std_map_channel_send") ||
		strings.Contains(slashPath, "std_map_task_spawn") {
		return goLastLiteralDiagnostic(t, path, "symbols", "type error: Map concurrency boundary")
	}
	if strings.Contains(slashPath, "task_spawn_handle") {
		return goLastLiteralDiagnostic(t, path, "alice", "type error: handle concurrency boundary")
	}
	if strings.Contains(slashPath, "task_spawn_mutex") ||
		strings.Contains(slashPath, "thread_scoped_mutex") {
		return goLastLiteralDiagnostic(t, path, "locked", "type error: Mutex concurrency boundary")
	}
	if strings.Contains(slashPath, "thread_borrow_capture") {
		return goLastLiteralDiagnostic(t, path, "worker", "type error: thread cannot capture borrow")
	}
	if strings.Contains(slashPath, "task_borrow_capture") {
		return goLastLiteralDiagnostic(t, path, "load", "type error: task cannot capture borrow")
	}
	if strings.Contains(slashPath, "queue_borrow_capture") {
		return goLastLiteralDiagnostic(t, path, "job", "type error: queue cannot capture borrow")
	}
	return goTaskWorkerDiagnosticSnapshots(t, path, slashPath)
}

// goTaskWorkerDiagnosticSnapshots returns worker and partition diagnostics.
func goTaskWorkerDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "task_spawn_borrowed_io") {
		return goLastLiteralDiagnostic(
			t, path, "load", "type error: spawned function must accept owned Io",
		)
	}
	if strings.Contains(slashPath, "task_spawn_mut_borrowed_io") {
		return goLastLiteralDiagnostic(
			t, path, "load", "type error: spawned function must accept owned Io",
		)
	}
	if strings.Contains(slashPath, "task_spawn_old_io_arg") {
		return goLastLiteralDiagnostic(
			t, path, "spawn", "type error: TaskGroup.spawn function name",
		)
	}
	if strings.Contains(slashPath, "parallel_map_wrong_worker") {
		return goLastLiteralDiagnostic(t, path, "worker", "type error: parallel map worker return")
	}
	if strings.Contains(slashPath, "parallel_shared_mutable") {
		return goLastLiteralDiagnostic(t, path, "worker", "type error: parallel worker param")
	}
	if strings.Contains(slashPath, "partition_mut_non_i64") {
		return goLastLiteralDiagnostic(t, path, "job", "type error: partition init type")
	}
	return nil
}

// goLastLiteralDiagnostic builds one diagnostic at the last matching literal.
func goLastLiteralDiagnostic(
	t *testing.T,
	path string,
	literal string,
	message string,
) []diagnosticSnapshot {
	t.Helper()
	tok := lastTokenWithLiteral(t, path, literal)
	return []diagnosticSnapshot{diagnosticFromToken(message, tok, tok)}
}

// goBorrowAndBasicTypeDiagnosticSnapshots returns borrow/basic type rows.
func goBorrowAndBasicTypeDiagnosticSnapshots(
	t *testing.T,
	path string,
	slashPath string,
) []diagnosticSnapshot {
	t.Helper()
	if strings.Contains(slashPath, "nested_field_borrow") {
		tok := tokenWithLiteralOccurrence(t, path, "profile", 3)
		return []diagnosticSnapshot{diagnosticFromToken("type error: nested field borrow", tok, tok)}
	}
	if strings.Contains(slashPath, "mut_borrow_immutable") {
		tok := lastTokenWithLiteral(t, path, "user")
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: &mut argument must be mutable", tok, tok),
		}
	}
	if strings.Contains(slashPath, "shared_borrow_assignment") {
		tok := tokenWithLiteralOccurrence(t, path, "user", 2)
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: shared borrow is not mutable", tok, tok),
		}
	}
	if strings.Contains(slashPath, "immutable_assignment") {
		tok := tokenWithLiteralOccurrence(t, path, "x", 2)
		return []diagnosticSnapshot{diagnosticFromToken("type error: immutable assignment", tok, tok)}
	}
	if strings.Contains(slashPath, "immutable_field_assignment") {
		tok := tokenWithLiteralOccurrence(t, path, "user", 2)
		return []diagnosticSnapshot{
			diagnosticFromToken("type error: immutable field assignment", tok, tok),
		}
	}
	if strings.Contains(slashPath, "invalid_try") {
		tok := tokenWithLiteral(t, path, "try")
		return []diagnosticSnapshot{diagnosticFromToken("type error: invalid try", tok, tok)}
	}
	return nil
}

// goOwnershipDiagnosticSnapshots returns ownership diagnostics in the self-host subset.
func goOwnershipDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	if !isOwnershipDiagnosticFixture(path) {
		return nil
	}
	snapshot := goOwnershipSnapshot(t, path)
	if snapshot.Status != "fail" {
		t.Fatalf("expected ownership diagnostic for %s", path)
	}
	primary := tokenWithStart(t, path, snapshot.PrimaryStart)
	related := tokenWithStart(t, path, snapshot.RelatedStart)
	return []diagnosticSnapshot{diagnosticFromToken(snapshot.Message, primary, related)}
}

// goRuntimeDiagnosticSnapshots returns selected runtime diagnostics.
func goRuntimeDiagnosticSnapshots(t *testing.T, path string) []diagnosticSnapshot {
	t.Helper()
	item, ok := runtimeDiagnosticCaseForPath(path)
	if !ok {
		return nil
	}
	message := normalizeRuntimeErrorForCase(runGoRuntimeError(t, path), item)
	if message != item.Message {
		t.Fatalf("runtime diagnostic got %q, want %q", message, item.Message)
	}
	tok := tokenWithLiteral(t, path, item.Literal)
	return []diagnosticSnapshot{diagnosticFromToken(message, tok, tok)}
}

// runtimeDiagnosticCaseForPath returns metadata for runtime-failure cases.
func runtimeDiagnosticCaseForPath(path string) (runtimeDiagnosticCase, bool) {
	slashPath := filepath.ToSlash(path)
	for _, item := range runtimeDiagnosticCases() {
		if strings.Contains(slashPath, item.Path) {
			return item, true
		}
	}
	return runtimeDiagnosticCase{}, false
}

// runtimeDiagnosticCases returns runtime failures not caught by static checkers.
func runtimeDiagnosticCases() []runtimeDiagnosticCase {
	return []runtimeDiagnosticCase{
		{"examples/negative/channel_empty_recv.kizu", "recv", "runtime error: channel empty"},
		{"examples/negative/fs_failing_io.kizu", "failing", "runtime error: io failing"},
		{"examples/negative/fs_read_missing.kizu", "read_file", "runtime error: fs missing"},
		{"examples/negative/local_buffer_out_of_bounds.kizu", "get", "runtime error: LocalBuffer bounds"},
		{"examples/negative/parallel_for_error.kizu", "parallel_for", "runtime error: parallel failed"},
		{
			"examples/negative/parallel_map_out_of_bounds.kizu",
			"parallel_map",
			"runtime error: parallel_map bounds",
		},
		{"examples/negative/partition_index_out_of_bounds.kizu", "at", "runtime error: partition bounds"},
		{"examples/negative/std_array_at_out_of_bounds.kizu", "at", "runtime error: Array.at bounds"},
		{"examples/negative/std_array_bounds.kizu", "get", "runtime error: Array.get bounds"},
		{"examples/negative/std_io_failing_write.kizu", "failing", "runtime error: io failing"},
		{"examples/negative/std_map_get_missing.kizu", "get", "runtime error: Map.get missing"},
		{
			"examples/negative/std_mem_byte_at_out_of_bounds.kizu",
			"byte_at",
			"runtime error: byte_at bounds",
		},
		{"examples/negative/std_mem_slice_out_of_bounds.kizu", "slice", "runtime error: slice bounds"},
		{"examples/negative/std_process_arg_bounds.kizu", "arg", "runtime error: process arg bounds"},
		{
			"examples/negative/std_testing_failure.kizu",
			"expect_equal_i64",
			"runtime error: testing failure",
		},
		{"examples/negative/task_await_error.kizu", "recv", "runtime error: channel empty"},
	}
}

// isOwnershipDiagnosticFixture reports whether a path is in the diagnostic subset.
func isOwnershipDiagnosticFixture(path string) bool {
	slashPath := filepath.ToSlash(path)
	name := strings.TrimSuffix(filepath.Base(slashPath), filepath.Ext(slashPath))
	for _, fixture := range ownershipDiagnosticFixtures() {
		if name == fixture {
			return true
		}
	}
	return false
}

// ownershipDiagnosticFixtures returns the ownership fixtures in the diagnostic oracle.
func ownershipDiagnosticFixtures() []string {
	return []string{
		"double_move",
		"assignment_move",
		"channel_send_move",
		"moved_value",
		"if_branch_move",
		"move_error",
		"if_branch_partial_move",
		"if_expression_branch_move",
		"while_body_move",
		"unsafe_moved_value",
		"borrow_before_last_use_move",
		"borrow_loop_last_use",
		"field_borrow_owner_move",
		"field_borrow_same_field_assignment",
		"borrow_field",
		"borrow_escape",
		"borrow_local_alias",
		"borrow_to_owner",
		"borrow_deref_move",
		"mut_borrow_conflict",
		"mut_borrow_deref_move",
		"std_array_use_after_deinit",
		"std_array_append_moves",
		"std_array_append_while_borrowed",
		"std_array_at_mut_append_while_borrowed",
		"std_array_at_mut_deinit_while_borrowed",
		"std_array_at_mut_set_while_borrowed",
		"std_array_deinit_while_borrowed",
		"std_array_read_while_mut_borrowed",
		"std_array_set_while_borrowed",
		"std_string_use_after_deinit",
		"std_map_use_after_deinit",
		"std_string_append_while_viewed",
		"std_string_clear_while_viewed",
		"std_string_deinit_while_viewed",
		"task_move",
		"task_await_after_cancel",
		"task_cancel_after_await",
		"unawaited_task",
		"arena_wrong_handle",
		"arena_inline_wrong_handle",
		"arena_unknown_handle",
		"arena_handle_outlive",
		"arena_add_move",
		"arena_get_move",
		"unsafe_borrow_escape",
		"move_while_borrowed",
	}
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

// tokenWithStart returns the token at one byte offset.
func tokenWithStart(t *testing.T, path string, start int) token.Token {
	t.Helper()
	for _, tok := range goTokens(t, path) {
		if tok.Start == start {
			return tok
		}
	}
	t.Fatalf("no token starts at %d in %s", start, path)
	return token.Token{}
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

// tokenWithLiteralOccurrence returns a one-based token occurrence for a literal.
func tokenWithLiteralOccurrence(
	t *testing.T,
	path string,
	literal string,
	occurrence int,
) token.Token {
	t.Helper()
	found := 0
	for _, tok := range goTokens(t, path) {
		if tok.Literal == literal {
			found++
			if found == occurrence {
				return tok
			}
		}
	}
	t.Fatalf("literal %q occurrence %d was not found in %s", literal, occurrence, path)
	return token.Token{}
}

// lastTokenWithLiteral returns the last token with the requested literal.
func lastTokenWithLiteral(t *testing.T, path string, literal string) token.Token {
	t.Helper()
	var found token.Token
	ok := false
	for _, tok := range goTokens(t, path) {
		if tok.Literal == literal {
			found = tok
			ok = true
		}
	}
	if ok {
		return found
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
	lines = append(lines, astFunctionControlDetail(fn.Body)...)
	lines = append(lines, astFunctionExpressionDetail(fn.Body)...)
	return lines
}

// astFunctionControlDetail returns selected statement node counts.
func astFunctionControlDetail(block *ast.BlockStmt) []string {
	counts := countControlsInBlock(block)
	return []string{
		"controls",
		"ifs", strconv.Itoa(counts.Ifs),
		"whiles", strconv.Itoa(counts.Whiles),
		"fors", strconv.Itoa(counts.Fors),
		"matches", strconv.Itoa(counts.Matches),
		"breaks", strconv.Itoa(counts.Breaks),
		"continues", strconv.Itoa(counts.Continues),
		"match arms", strconv.Itoa(counts.MatchArms),
	}
}

// controlCounts stores selected AST statement counts.
type controlCounts struct {
	Ifs       int
	Whiles    int
	Fors      int
	Matches   int
	Breaks    int
	Continues int
	MatchArms int
}

// countControlsInBlock counts selected control statements recursively.
func countControlsInBlock(block *ast.BlockStmt) controlCounts {
	counts := controlCounts{}
	if block == nil {
		return counts
	}
	for _, stmt := range block.Statements {
		counts.Add(countControlsInStatement(stmt))
	}
	return counts
}

// Add accumulates statement counts.
func (c *controlCounts) Add(other controlCounts) {
	c.Ifs += other.Ifs
	c.Whiles += other.Whiles
	c.Fors += other.Fors
	c.Matches += other.Matches
	c.Breaks += other.Breaks
	c.Continues += other.Continues
	c.MatchArms += other.MatchArms
}

// countControlsInStatement counts one statement and its children.
func countControlsInStatement(stmt ast.Statement) controlCounts {
	counts := controlCounts{}
	switch s := stmt.(type) {
	case *ast.IfStmt:
		counts.Ifs++
		counts.Add(countControlsInBlock(s.Consequence))
		counts.Add(countControlsInBlock(s.Alternative))
	case *ast.WhileStmt:
		counts.Whiles++
		counts.Add(countControlsInBlock(s.Body))
	case *ast.ForStmt:
		counts.Fors++
		counts.Add(countControlsInBlock(s.Body))
	case *ast.MatchStmt:
		counts.Matches++
		counts.MatchArms += len(s.Arms)
	case *ast.BreakStmt:
		counts.Breaks++
	case *ast.ContinueStmt:
		counts.Continues++
	}
	return counts
}

// astFunctionExpressionDetail returns selected expression and statement counts.
func astFunctionExpressionDetail(block *ast.BlockStmt) []string {
	counts := countExpressionsInBlock(block)
	return []string{
		"expressions",
		"locals", strconv.Itoa(counts.Locals),
		"assignments", strconv.Itoa(counts.Assignments),
		"calls", strconv.Itoa(counts.Calls),
		"field accesses", strconv.Itoa(counts.FieldAccesses),
		"struct literals", strconv.Itoa(counts.StructLiterals),
		"binary expressions", strconv.Itoa(counts.BinaryExpressions),
	}
}

// expressionCounts stores selected expression and statement counts.
type expressionCounts struct {
	Locals            int
	Assignments       int
	Calls             int
	FieldAccesses     int
	StructLiterals    int
	BinaryExpressions int
}

// countExpressionsInBlock counts selected expressions recursively.
func countExpressionsInBlock(block *ast.BlockStmt) expressionCounts {
	counts := expressionCounts{}
	if block == nil {
		return counts
	}
	for _, stmt := range block.Statements {
		counts.Add(countExpressionsInStatement(stmt))
	}
	return counts
}

// Add accumulates expression counts.
func (c *expressionCounts) Add(other expressionCounts) {
	c.Locals += other.Locals
	c.Assignments += other.Assignments
	c.Calls += other.Calls
	c.FieldAccesses += other.FieldAccesses
	c.StructLiterals += other.StructLiterals
	c.BinaryExpressions += other.BinaryExpressions
}

// countExpressionsInStatement counts selected expressions in one statement.
func countExpressionsInStatement(stmt ast.Statement) expressionCounts {
	counts := expressionCounts{}
	switch s := stmt.(type) {
	case *ast.LetStmt:
		counts.Locals++
		counts.Add(countExpressionsInExpr(s.Value))
	case *ast.AssignStmt:
		counts.Assignments++
		counts.Add(countExpressionsInExpr(s.Target))
		counts.Add(countExpressionsInExpr(s.Value))
	case *ast.ExprStmt:
		counts.Add(countExpressionsInExpr(s.Expr))
	case *ast.ReturnStmt:
		counts.Add(countExpressionsInExpr(s.Value))
	case *ast.IfStmt:
		counts.Add(countExpressionsInExpr(s.Condition))
		counts.Add(countExpressionsInBlock(s.Consequence))
		counts.Add(countExpressionsInBlock(s.Alternative))
	case *ast.WhileStmt:
		counts.Add(countExpressionsInExpr(s.Condition))
		counts.Add(countExpressionsInBlock(s.Body))
	case *ast.ForStmt:
		counts.Add(countExpressionsInExpr(s.Start))
		counts.Add(countExpressionsInExpr(s.End))
		counts.Add(countExpressionsInBlock(s.Body))
	case *ast.MatchStmt:
		counts.Add(countExpressionsInExpr(s.Value))
		for _, arm := range s.Arms {
			counts.Add(countExpressionsInStatement(arm.Body))
		}
	}
	return counts
}

// countExpressionsInExpr counts selected expression nodes recursively.
func countExpressionsInExpr(expr ast.Expression) expressionCounts {
	counts := expressionCounts{}
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		counts.BinaryExpressions++
		counts.Add(countExpressionsInExpr(e.Left))
		counts.Add(countExpressionsInExpr(e.Right))
	case *ast.CallExpr:
		counts.Calls++
		counts.Add(countExpressionsInExpr(e.Callee))
		for _, arg := range e.Args {
			counts.Add(countExpressionsInExpr(arg))
		}
	case *ast.FieldExpr:
		if !e.Namespace {
			counts.FieldAccesses++
		}
		counts.Add(countExpressionsInExpr(e.Receiver))
	case *ast.StructLiteralExpr:
		counts.StructLiterals++
		for _, field := range e.Fields {
			counts.Add(countExpressionsInExpr(field.Value))
		}
	}
	return counts
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

// goAstNodeDumpSnapshot returns a selected pre-order AST node dump.
func goAstNodeDumpSnapshot(t *testing.T, path string) []string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	lines := []string{}
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		lines = append(lines, astFunctionNodeDump(fn)...)
	}
	return lines
}

// astFunctionNodeDump returns selected function node rows.
func astFunctionNodeDump(fn *ast.FunctionDecl) []string {
	lines := []string{
		"FunctionDecl", fn.Name,
		"params", strconv.Itoa(len(fn.Params)),
		"return", normalizeReturnType(fn.ReturnType),
		"BlockStmt",
	}
	return append(lines, astBlockNodeDump(fn.Body)...)
}

// astBlockNodeDump returns selected statement and expression node rows.
func astBlockNodeDump(block *ast.BlockStmt) []string {
	if block == nil {
		return nil
	}
	lines := []string{}
	for _, stmt := range block.Statements {
		lines = append(lines, astStatementNodeDump(stmt)...)
	}
	return lines
}

// astStatementNodeDump returns selected statement node rows.
func astStatementNodeDump(stmt ast.Statement) []string {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return append([]string{"ReturnStmt"}, astExpressionNodeDump(s.Value)...)
	case *ast.LetStmt:
		kind := "LetStmt"
		if s.Mutable {
			kind = "VarStmt"
		}
		return append([]string{kind, s.Name}, astExpressionNodeDump(s.Value)...)
	case *ast.AssignStmt:
		return append([]string{"AssignStmt"}, astExpressionNodeDump(s.Value)...)
	case *ast.IfStmt:
		lines := append([]string{"IfStmt"}, astExpressionNodeDump(s.Condition)...)
		lines = append(lines, astBlockNodeDump(s.Consequence)...)
		return append(lines, astBlockNodeDump(s.Alternative)...)
	case *ast.WhileStmt:
		lines := append([]string{"WhileStmt"}, astExpressionNodeDump(s.Condition)...)
		return append(lines, astBlockNodeDump(s.Body)...)
	case *ast.ForStmt:
		lines := append([]string{"ForStmt"}, astExpressionNodeDump(s.Start)...)
		lines = append(lines, astExpressionNodeDump(s.End)...)
		return append(lines, astBlockNodeDump(s.Body)...)
	case *ast.MatchStmt:
		lines := append([]string{"MatchStmt"}, astExpressionNodeDump(s.Value)...)
		for _, arm := range s.Arms {
			lines = append(lines, astStatementNodeDump(arm.Body)...)
		}
		return lines
	case *ast.BreakStmt:
		return []string{"BreakStmt"}
	case *ast.ContinueStmt:
		return []string{"ContinueStmt"}
	case *ast.ExprStmt:
		return astExpressionNodeDump(s.Expr)
	}
	return nil
}

// astExpressionNodeDump returns selected expression node rows.
func astExpressionNodeDump(expr ast.Expression) []string {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		lines := []string{"BinaryExpr", tokenKindForOperator(e.Operator)}
		lines = append(lines, astExpressionNodeDump(e.Left)...)
		return append(lines, astExpressionNodeDump(e.Right)...)
	case *ast.IfExpr:
		lines := append([]string{"IfExpr"}, astExpressionNodeDump(e.Condition)...)
		lines = append(lines, astBlockNodeDump(e.Consequence)...)
		return append(lines, astBlockNodeDump(e.Alternative)...)
	case *ast.CallExpr:
		lines := []string{"CallExpr", e.Callee.String(), strconv.Itoa(len(e.Args))}
		for _, arg := range e.Args {
			lines = append(lines, astExpressionNodeDump(arg)...)
		}
		return lines
	case *ast.FieldExpr:
		return astExpressionNodeDump(e.Receiver)
	}
	return nil
}

// tokenKindForOperator returns the self-host token kind spelling for an operator.
func tokenKindForOperator(operator string) string {
	switch operator {
	case "+":
		return "TokenKind::Plus"
	case "-":
		return "TokenKind::Minus"
	case "*":
		return "TokenKind::Asterisk"
	case "/":
		return "TokenKind::Slash"
	case "%":
		return "TokenKind::Percent"
	case "==":
		return "TokenKind::Eq"
	case "!=":
		return "TokenKind::NotEq"
	case "<":
		return "TokenKind::LT"
	case "<=":
		return "TokenKind::LTE"
	case ">":
		return "TokenKind::GT"
	case ">=":
		return "TokenKind::GTE"
	default:
		return "TokenKind::Illegal"
	}
}

// formatAstNodeDumpSnapshot formats AST node dump rows.
func formatAstNodeDumpSnapshot(lines []string) string {
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
	return moduleGraphSnapshot{
		Status: "pass", Root: "<single>", Modules: 1, ModulePaths: []string{"<single>"},
	}
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
	for _, module := range pkg.Graph.Modules {
		snapshot.ModulePaths = append(snapshot.ModulePaths, module.Path)
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
	for _, module := range snapshot.ModulePaths {
		lines = append(lines, "module")
		lines = append(lines, strings.Split(module, "::")...)
		lines = append(lines, "module end")
	}
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
	case *ast.CallExpr:
		return goCallInitializerType(value)
	default:
		return "unknown"
	}
}

// goCallInitializerType infers selected stdlib call initializer types.
func goCallInitializerType(call *ast.CallExpr) string {
	name := call.Callee.String()
	switch name {
	case "std::mem::page_allocator":
		return "std::mem::Allocator"
	case "std::string::String":
		return "std::string::String"
	case "std::array::Array<i64>":
		return "std::array::Array<i64>"
	case "std::map::Map<[]const u8, i64>":
		return "std::map::Map<[]const u8, i64>"
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
	if strings.Contains(message, "array::Array") && strings.Contains(message, "expects allocator") {
		return "type error: Array allocator"
	}
	if strings.Contains(message, "string::String") && strings.Contains(message, "expects allocator") {
		return "type error: String allocator"
	}
	if strings.Contains(message, "map::Map") && strings.Contains(message, "expects allocator") {
		return "type error: Map allocator"
	}
	if strings.Contains(message, "requires mutable String receiver") {
		return "type error: String mutable receiver"
	}
	if strings.Contains(message, "requires owned String receiver") {
		return "type error: String owned receiver"
	}
	if strings.Contains(message, "requires mutable Map receiver") {
		return "type error: Map mutable receiver"
	}
	if strings.Contains(message, "requires owned Map receiver") {
		return "type error: Map owned receiver"
	}
	for _, item := range typeErrorNormalizers() {
		if strings.Contains(message, item.match) {
			return item.normalized
		}
	}
	return "type error"
}

// runGoRuntimeError executes a runtime-failure fixture with the Go implementation.
func runGoRuntimeError(t *testing.T, path string) string {
	t.Helper()
	program := parseSelfHostSource(t, path)
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed before runtime oracle: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed before runtime oracle: %v", err)
	}
	var out bytes.Buffer
	err := interp.NewWithProcessArgs(&out, nil).Run(program)
	if err == nil {
		t.Fatalf("expected runtime error for %s", path)
	}
	return err.Error()
}

// normalizeRuntimeError maps runtime messages into the self-host subset.
func normalizeRuntimeError(message string) string {
	for _, item := range runtimeErrorNormalizers() {
		if strings.Contains(message, item.match) {
			return item.normalized
		}
	}
	return "runtime error"
}

// normalizeRuntimeErrorForCase maps context-sensitive runtime failures.
func normalizeRuntimeErrorForCase(message string, item runtimeDiagnosticCase) string {
	if strings.Contains(item.Path, "task_await_error") {
		if strings.Contains(message, "channel is empty") {
			return item.Message
		}
	}
	return normalizeRuntimeError(message)
}

// runtimeErrorNormalizers returns runtime diagnostic substring mappings.
func runtimeErrorNormalizers() []typeErrorNormalizer {
	return []typeErrorNormalizer{
		{"task failed: channel is empty", "runtime error: task await error"},
		{"channel is empty", "runtime error: channel empty"},
		{"io runtime is failing", "runtime error: io failing"},
		{"no such file", "runtime error: fs missing"},
		{"LocalBuffer index out of bounds", "runtime error: LocalBuffer bounds"},
		{"parallel failed", "runtime error: parallel failed"},
		{"parallel_map range out of bounds", "runtime error: parallel_map bounds"},
		{"partition index out of bounds", "runtime error: partition bounds"},
		{"Array.at index out of bounds", "runtime error: Array.at bounds"},
		{"Array.get index out of bounds", "runtime error: Array.get bounds"},
		{"Map.get key not found", "runtime error: Map.get missing"},
		{"byte_at index out of bounds", "runtime error: byte_at bounds"},
		{"slice range out of bounds", "runtime error: slice bounds"},
		{"process arg index out of bounds", "runtime error: process arg bounds"},
		{"expected 4, got 3", "runtime error: testing failure"},
	}
}

// typeErrorNormalizer maps a source diagnostic substring to a normalized message.
type typeErrorNormalizer struct {
	match      string
	normalized string
}

// typeErrorNormalizers returns simple substring-based type diagnostic mappings.
func typeErrorNormalizers() []typeErrorNormalizer {
	return []typeErrorNormalizer{
		{"equal_bytes", "type error: `std::mem::equal_bytes` arg 2 expects []const u8"},
		{"expects 2 args", "type error: call arg count"},
		{"arg 1 of `take` expects i64", "type error: call arg type"},
		{"unknown field", "type error: unknown field"},
		{"if expression branch types differ", "type error: if expression branch types differ"},
		{"return expects", "type error: return type mismatch"},
		{"runtime value cannot be used", "comptime error: runtime value"},
		{"missing method", "type error: contract missing method"},
		{"Dyn parameter", "type error: Dyn parameter borrowed"},
		{"does not satisfy `Writer`", "type error: Dyn not satisfied"},
		{"parallel map worker", "type error: parallel map worker return"},
		{"must return", "type error: missing return"},
		{"cannot cast", "type error: invalid cast"},
		{"use `std::io::blocking()`", "type error: Io constructor"},
		{"`std::io::evented` is not implemented", "type error: Io evented"},
		{"`std::fs::read_file` expects Io", "type error: fs.read_file Io"},
		{"`std::fs::write_file` expects []const u8 bytes", "type error: fs.write_file bytes"},
		{"`std::fs::exists` expects Io", "type error: fs.exists Io"},
		{"std.path.basename", "type error: path.basename arg"},
		{"`Array.at_mut` requires mutable array binding", "type error: Array.at_mut mutability"},
		{"`Array.at` must be bound", "type error: Array.at bind required"},
		{"`Array.get` requires copy element", "type error: Array.get copy element"},
		{"Array element cannot be Atomic", "type error: Array element Atomic"},
		{"Array element cannot be handle", "type error: Array element handle"},
		{"Array element cannot be std::map::Map", "type error: Array element Map"},
		{"Array element cannot be raw pointer", "type error: Array element raw pointer"},
		{"Array element cannot be Channel", "type error: Array element Channel"},
		{"Array element cannot be nested array", "type error: Array element nested array"},
		{"Array cannot cross concurrency boundary", "type error: Array concurrency boundary"},
		{"Array.append", "type error: Array.append type"},
		{"Map key type", "type error: Map key type"},
		{"String.append_byte`", "type error: String.append_byte type"},
		{"String.as_bytes", "type error: String.as_bytes bind required"},
		{"Map value type must be copy", "type error: Map value copy"},
		{"Map.insert", "type error: Map.insert type"},
		{"String.append_bytes", "type error: String.append_bytes type"},
		{"expect_equal_i64", "type error: testing arg type"},
		{"Atomic<i64>", "type error: Atomic old name"},
		{"AtomicI64", "type error: Atomic old name"},
		{"use `std::atomic::Atomic<T>(value)`", "type error: Atomic type argument required"},
		{"unsupported atomic type", "type error: Atomic unsupported type"},
		{"atomic.store", "type error: atomic.store type"},
		{"use `std::channel::Channel<T>()`", "type error: Channel type argument required"},
		{"channel.send", "type error: channel.send type"},
		{"borrow cannot cross", "type error: borrow concurrency boundary"},
		{"use `std::sync::Mutex<T>(value)`", "type error: Mutex type argument required"},
		{"Mutex<i64>", "type error: Mutex constructor type"},
		{"requires copy value", "type error: Mutex requires copy"},
		{"std::task::Group", "type error: task group expects io"},
		{"TaskGroup.spawn` expects function name", "type error: TaskGroup.spawn function name"},
		{"must accept owned Io", "type error: spawned function must accept owned Io"},
		{"raw pointer cannot cross", "type error: raw pointer concurrency boundary"},
		{"arena cannot cross", "type error: arena concurrency boundary"},
		{"Map cannot cross", "type error: Map concurrency boundary"},
		{"handle cannot cross", "type error: handle concurrency boundary"},
		{"Mutex cannot cross", "type error: Mutex concurrency boundary"},
		{"task cannot capture borrow", "type error: task cannot capture borrow"},
		{"queue cannot capture borrow", "type error: queue cannot capture borrow"},
		{"thread cannot capture borrow", "type error: thread cannot capture borrow"},
		{"must accept i64", "type error: parallel worker param"},
		{"partition init", "type error: partition init type"},
	}
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
	if strings.Contains(err.Error(), "cannot store borrow") {
		return structBorrowFieldOwnershipSnapshot(t, path)
	}
	if strings.Contains(err.Error(), "cannot be moved out of borrow") {
		return borrowedDerefMoveOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "cannot be mutably borrowed while borrowed") {
		return mutableBorrowConflictOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "cannot be assigned while borrowed") {
		return fieldBorrowAssignmentOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "cannot be moved while borrowed") {
		return borrowedMoveOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "arena error:") {
		return arenaOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "array error:") {
		return arrayOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "task error:") {
		return taskOwnershipSnapshot(t, path, err.Error())
	}
	if strings.Contains(err.Error(), "string error:") {
		return stringViewOwnershipSnapshot(t, path, err.Error())
	}
	value := movedValueFromDiagnostic(t, err.Error())
	primary, related := movedValueSpans(t, path, value)
	return ownershipSnapshot{
		Status: "fail", Message: "move error: moved value was used",
		Value: value, PrimaryStart: primary, RelatedStart: related,
	}
}

// taskOwnershipSnapshot normalizes selected task ownership diagnostics.
func taskOwnershipSnapshot(t *testing.T, path string, message string) ownershipSnapshot {
	t.Helper()
	if strings.Contains(message, "must be awaited or canceled") {
		task := lastTokenWithLiteral(t, path, "task")
		spawn := lastTokenWithLiteral(t, path, "spawn")
		return ownershipSnapshot{
			Status: "fail", Message: "task error: task must be awaited or canceled",
			Value: "task", PrimaryStart: task.Start, RelatedStart: spawn.Start,
		}
	}
	if strings.Contains(message, "already completed") {
		task := lastTokenWithLiteral(t, path, "task")
		spawn := lastTokenWithLiteral(t, path, "spawn")
		return ownershipSnapshot{
			Status: "fail", Message: "task error: task already completed",
			Value: "task", PrimaryStart: task.Start, RelatedStart: spawn.Start,
		}
	}
	t.Fatalf("task diagnostic is outside oracle subset: %q", message)
	return ownershipSnapshot{}
}

// arrayOwnershipSnapshot normalizes Array borrow/resource diagnostics.
func arrayOwnershipSnapshot(t *testing.T, path string, message string) ownershipSnapshot {
	t.Helper()
	if strings.Contains(message, "append") {
		return arrayCustomSnapshot(t, path, "append", "tokens",
			"array error: Array.append while borrowed")
	}
	if strings.Contains(message, "deinit") {
		return arrayCustomSnapshot(t, path, "deinit", "tokens",
			"array error: Array.deinit while borrowed")
	}
	if strings.Contains(message, "len") {
		return arrayCustomSnapshot(t, path, "len", "tokens",
			"array error: Array.len while mutably borrowed")
	}
	if strings.Contains(message, "set") {
		return arrayCustomSnapshot(t, path, "set", "tokens",
			"array error: Array.set while borrowed")
	}
	t.Fatalf("array diagnostic is outside oracle subset: %q", message)
	return ownershipSnapshot{}
}

// arrayCustomSnapshot builds one normalized Array borrow diagnostic.
func arrayCustomSnapshot(
	t *testing.T,
	path string,
	method string,
	related string,
	message string,
) ownershipSnapshot {
	t.Helper()
	primary := lastTokenWithLiteral(t, path, method)
	relatedToken := tokenWithLiteralOccurrence(t, path, related, 3)
	return ownershipSnapshot{
		Status: "fail", Message: message, Value: method,
		PrimaryStart: primary.Start, RelatedStart: relatedToken.Start,
	}
}

// stringViewOwnershipSnapshot normalizes String view lifetime diagnostics.
func stringViewOwnershipSnapshot(t *testing.T, path string, message string) ownershipSnapshot {
	t.Helper()
	if strings.Contains(message, "append_bytes") {
		return stringViewCustomSnapshot(t, path, "append_bytes",
			"string error: String.append_bytes while borrowed")
	}
	if strings.Contains(message, "clear") {
		return stringViewCustomSnapshot(t, path, "clear",
			"string error: String.clear while borrowed")
	}
	if strings.Contains(message, "deinit") {
		return stringViewCustomSnapshot(t, path, "deinit",
			"string error: String.deinit while borrowed")
	}
	t.Fatalf("string diagnostic is outside oracle subset: %q", message)
	return ownershipSnapshot{}
}

// stringViewCustomSnapshot builds one normalized String view diagnostic.
func stringViewCustomSnapshot(
	t *testing.T,
	path string,
	method string,
	message string,
) ownershipSnapshot {
	t.Helper()
	primary := lastTokenWithLiteral(t, path, method)
	related := tokenWithLiteralOccurrence(t, path, "text", 3)
	return ownershipSnapshot{
		Status: "fail", Message: message, Value: method,
		PrimaryStart: primary.Start, RelatedStart: related.Start,
	}
}

// arenaOwnershipSnapshot normalizes Arena/Handle provenance diagnostics.
func arenaOwnershipSnapshot(t *testing.T, path string, message string) ownershipSnapshot {
	t.Helper()
	slashPath := filepath.ToSlash(path)
	switch {
	case strings.Contains(slashPath, "arena_wrong_handle"):
		return arenaCustomSnapshot(t, path, "alice", "right",
			"arena error: handle does not belong to arena")
	case strings.Contains(slashPath, "arena_inline_wrong_handle"):
		return arenaCustomSnapshot(t, path, "left", "right",
			"arena error: handle from wrong arena")
	case strings.Contains(slashPath, "arena_unknown_handle"):
		return arenaCustomSnapshot(t, path, "users", "user",
			"arena error: arena has unknown provenance")
	case strings.Contains(slashPath, "arena_handle_outlive"):
		return arenaCustomSnapshot(t, path, "alice", "users",
			"arena error: handle cannot outlive arena")
	case strings.Contains(slashPath, "arena_get_move"):
		return arenaCustomSnapshot(t, path, "get", "take",
			"arena error: arena.get local borrow cannot be moved")
	default:
		t.Fatalf("arena diagnostic is outside oracle subset: %q", message)
		return ownershipSnapshot{}
	}
}

// arenaCustomSnapshot builds one normalized Arena diagnostic snapshot.
func arenaCustomSnapshot(
	t *testing.T,
	path string,
	primary string,
	related string,
	message string,
) ownershipSnapshot {
	t.Helper()
	primaryToken := lastTokenWithLiteral(t, path, primary)
	relatedToken := lastTokenWithLiteral(t, path, related)
	return ownershipSnapshot{
		Status: "fail", Message: message, Value: primary,
		PrimaryStart: primaryToken.Start, RelatedStart: relatedToken.Start,
	}
}

// structBorrowFieldOwnershipSnapshot normalizes struct borrow-storage diagnostics.
func structBorrowFieldOwnershipSnapshot(t *testing.T, path string) ownershipSnapshot {
	t.Helper()
	field := tokenWithLiteral(t, path, "value")
	typeName := tokenWithLiteral(t, path, "Bad")
	return ownershipSnapshot{
		Status: "fail", Message: "borrow error: struct field cannot store borrow",
		Value: "value", PrimaryStart: field.Start, RelatedStart: typeName.Start,
	}
}

// borrowedDerefMoveOwnershipSnapshot normalizes moving a value through a borrow.
func borrowedDerefMoveOwnershipSnapshot(
	t *testing.T,
	path string,
	message string,
) ownershipSnapshot {
	t.Helper()
	value := movedValueFromDiagnostic(t, message)
	related := tokenWithLiteralOccurrence(t, path, value, 1)
	primary := tokenWithLiteralOccurrence(t, path, value, 2)
	return ownershipSnapshot{
		Status: "fail", Message: "borrow error: value cannot be moved out of borrow",
		Value: value, PrimaryStart: primary.Start, RelatedStart: related.Start,
	}
}

// mutableBorrowConflictOwnershipSnapshot normalizes overlapping & and &mut args.
func mutableBorrowConflictOwnershipSnapshot(
	t *testing.T,
	path string,
	message string,
) ownershipSnapshot {
	t.Helper()
	value := movedValueFromDiagnostic(t, message)
	primary, related := lastTwoValueSpans(t, path, value)
	return ownershipSnapshot{
		Status: "fail", Message: "borrow error: value cannot be mutably borrowed while borrowed",
		Value: value, PrimaryStart: primary, RelatedStart: related,
	}
}

// fieldBorrowAssignmentOwnershipSnapshot normalizes a field-borrow assignment error.
func fieldBorrowAssignmentOwnershipSnapshot(
	t *testing.T,
	path string,
	message string,
) ownershipSnapshot {
	t.Helper()
	value := fieldBorrowOwnerFromDiagnostic(t, message)
	primary, related := movedValueSpans(t, path, value)
	return ownershipSnapshot{
		Status: "fail", Message: "borrow error: value cannot be moved while borrowed",
		Value: value, PrimaryStart: primary, RelatedStart: related,
	}
}

// borrowedMoveOwnershipSnapshot normalizes a borrow-while-move diagnostic.
func borrowedMoveOwnershipSnapshot(t *testing.T, path string, message string) ownershipSnapshot {
	t.Helper()
	value := movedValueFromDiagnostic(t, message)
	primary, related := movedValueSpans(t, path, value)
	return ownershipSnapshot{
		Status: "fail", Message: "borrow error: value cannot be moved while borrowed",
		Value: value, PrimaryStart: primary, RelatedStart: related,
	}
}

// fieldBorrowOwnerFromDiagnostic extracts the owner from a field-borrow diagnostic.
func fieldBorrowOwnerFromDiagnostic(t *testing.T, message string) string {
	t.Helper()
	prefix := "field `"
	start := strings.Index(message, prefix)
	if start < 0 {
		t.Fatalf("field borrow diagnostic is outside oracle subset: %q", message)
	}
	rest := message[start+len(prefix):]
	end := strings.Index(rest, ".")
	if end < 0 {
		t.Fatalf("field borrow diagnostic has no owner terminator: %q", message)
	}
	return rest[:end]
}

// lastTwoValueSpans returns the final two occurrences of one identifier.
func lastTwoValueSpans(t *testing.T, path string, value string) (int, int) {
	t.Helper()
	tokens := goSelfHostTokenSnapshots(t, path)
	positions := []int{}
	for _, tok := range tokens {
		if tok.Kind == "TokenKind::Ident" && tok.Literal == value {
			positions = append(positions, tok.Start)
		}
	}
	if len(positions) < 2 {
		t.Fatalf("could not find final value spans for %q in %s", value, path)
	}
	return positions[len(positions)-1], positions[len(positions)-2]
}

// movedValueFromDiagnostic extracts the identifier from a moved-value diagnostic.
func movedValueFromDiagnostic(t *testing.T, message string) string {
	t.Helper()
	prefix := "moved value `"
	start := strings.Index(message, prefix)
	if start < 0 {
		prefix = "value `"
		start = strings.Index(message, prefix)
	}
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
			dump.Details = irDetails(fn.Blocks[0])
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

// irDetails returns selected result and operand facts for one block.
func irDetails(block *kir.Block) []irInstrDetail {
	details := make([]irInstrDetail, 0, len(block.Instrs))
	for _, instr := range block.Instrs {
		detail := irInstrDetail{
			Result:    normalizeIrResultType(instr.Result),
			Immediate: normalizeImmediate(instr.Immediate),
		}
		for _, arg := range instr.Args {
			detail.Args = append(detail.Args, arg.Type)
		}
		details = append(details, detail)
	}
	return details
}

// normalizeIrResultType maps void result values to the shared dump schema.
func normalizeIrResultType(value kir.Value) string {
	if value.Type == "" {
		return "<none>"
	}
	return value.Type
}

// normalizeImmediate maps empty immediates to the shared dump schema.
func normalizeImmediate(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
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
		for index, opcode := range fn.Opcodes {
			lines = append(lines, "op", opcode)
			if index < len(fn.Details) {
				lines = append(lines, formatIrInstrDetail(fn.Details[index])...)
			}
		}
		lines = append(lines, "terminator", fn.Terminator)
	}
	return strings.Join(lines, "\n") + "\n"
}

// formatIrInstrDetail formats selected result and operand rows.
func formatIrInstrDetail(detail irInstrDetail) []string {
	lines := []string{"result", detail.Result, "args", strconv.Itoa(len(detail.Args))}
	for _, arg := range detail.Args {
		lines = append(lines, "arg", arg)
	}
	lines = append(lines, "immediate", detail.Immediate)
	return lines
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
		Names: backendFunctionNames(module), Strings: countIrStringConstants(module),
		Instructions: countBackendInstructions(module),
		Consts:       countBackendOpcodes(module, "const"),
		Calls:        countBackendCalls(module),
		Entry:        "main",
		Lines:        []string{"; Kizu LLVM IR", "define void @main()"},
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
		Names: backendFunctionNames(module), Strings: countIrStringConstants(module),
		Instructions: countBackendInstructions(module),
		Consts:       countBackendOpcodes(module, "const"),
		Calls:        countBackendCalls(module),
		Entry:        "_start",
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

// countBackendInstructions counts all lowered instructions reaching emitters.
func countBackendInstructions(module *kir.Module) int {
	count := 0
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			count += len(block.Instrs)
		}
	}
	return count
}

// countBackendOpcodes counts one opcode across backend input blocks.
func countBackendOpcodes(module *kir.Module, opcode string) int {
	count := 0
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == opcode {
					count++
				}
			}
		}
	}
	return count
}

// countBackendCalls counts call-family instructions reaching emitters.
func countBackendCalls(module *kir.Module) int {
	count := 0
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "call.") {
					count++
				}
			}
		}
	}
	return count
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
			"instructions", strconv.Itoa(fingerprint.Instructions),
			"consts", strconv.Itoa(fingerprint.Consts),
			"calls", strconv.Itoa(fingerprint.Calls),
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
