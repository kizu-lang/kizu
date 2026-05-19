package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/token"
)

const (
	lexerParityCaseStart = "@@KIZU_LEXER_PARITY_CASE@@"
	lexerParityCaseEnd   = "@@KIZU_LEXER_PARITY_END@@"
)

const stdKizuLexerParityHarness = `
fn run_lexer_case(name: []const u8, text: []const u8) -> !void {
    print("@@KIZU_LEXER_PARITY_CASE@@");
    print(name);
    var token = std::kizu::lexer::first_token(text);
    while true {
        try dump_token(text, token);
        if is_eof_token(token) {
            print("@@KIZU_LEXER_PARITY_END@@");
            return;
        }
        token = std::kizu::lexer::next_token(text, token);
    }
}

fn run_tokenize_case(name: []const u8, text: []const u8) -> !void {
    print("@@KIZU_LEXER_PARITY_CASE@@");
    print(name);
    let allocator = std::mem::page_allocator();
    var tokens = try std::kizu::lexer::tokenize(allocator, text);
    var index = 0;
    while index < tokens.len() {
        let token = try tokens.get(index);
        try dump_token(text, token);
        index = index + 1;
    }
    tokens.deinit();
    print("@@KIZU_LEXER_PARITY_END@@");
    return;
}

fn is_eof_token(token: std::kizu::lexer::Token) -> bool {
    match token.kind {
        Eof => return true;
        Fn => return false;
        Import => return false;
        Pub => return false;
        Struct => return false;
        Enum => return false;
        Union => return false;
        Extern => return false;
        Let => return false;
        Var => return false;
        Return => return false;
        If => return false;
        Else => return false;
        While => return false;
        For => return false;
        Break => return false;
        Continue => return false;
        Match => return false;
        Unsafe => return false;
        Comptime => return false;
        Try => return false;
        True => return false;
        False => return false;
        And => return false;
        Or => return false;
        Ident => return false;
        Lifetime => return false;
        Number => return false;
        String => return false;
        LBrace => return false;
        RBrace => return false;
        LParen => return false;
        RParen => return false;
        Semicolon => return false;
        Comma => return false;
        Colon => return false;
        DoubleColon => return false;
        Assign => return false;
        Arrow => return false;
        FatArrow => return false;
        Bang => return false;
        Eq => return false;
        NotEq => return false;
        Amp => return false;
        LBracket => return false;
        RBracket => return false;
        LT => return false;
        LTE => return false;
        GT => return false;
        GTE => return false;
        Question => return false;
        Plus => return false;
        Minus => return false;
        Asterisk => return false;
        Slash => return false;
        Percent => return false;
        Dot => return false;
        Range => return false;
        Pipe => return false;
        Mut => return false;
    }
}

fn dump_token(source: []const u8, token: std::kizu::lexer::Token) -> !void {
    match token.kind {
        Fn => print("Fn");
        Import => print("Import");
        Pub => print("Pub");
        Struct => print("Struct");
        Enum => print("Enum");
        Union => print("Union");
        Extern => print("Extern");
        Let => print("Let");
        Var => print("Var");
        Return => print("Return");
        If => print("If");
        Else => print("Else");
        While => print("While");
        For => print("For");
        Break => print("Break");
        Continue => print("Continue");
        Match => print("Match");
        Unsafe => print("Unsafe");
        Comptime => print("Comptime");
        Try => print("Try");
        True => print("True");
        False => print("False");
        And => print("And");
        Or => print("Or");
        Ident => print("Ident");
        Lifetime => print("Lifetime");
        Number => print("Number");
        String => print("String");
        LBrace => print("LBrace");
        RBrace => print("RBrace");
        LParen => print("LParen");
        RParen => print("RParen");
        Semicolon => print("Semicolon");
        Comma => print("Comma");
        Colon => print("Colon");
        DoubleColon => print("DoubleColon");
        Assign => print("Assign");
        Arrow => print("Arrow");
        FatArrow => print("FatArrow");
        Bang => print("Bang");
        Eq => print("Eq");
        NotEq => print("NotEq");
        Amp => print("Amp");
        LBracket => print("LBracket");
        RBracket => print("RBracket");
        LT => print("LT");
        LTE => print("LTE");
        GT => print("GT");
        GTE => print("GTE");
        Question => print("Question");
        Plus => print("Plus");
        Minus => print("Minus");
        Asterisk => print("Asterisk");
        Slash => print("Slash");
        Percent => print("Percent");
        Dot => print("Dot");
        Range => print("Range");
        Pipe => print("Pipe");
        Mut => print("Mut");
        Eof => print("Eof");
    }
    let text = try std::mem::slice(source, token.start, token.end);
    print(text);
    return;
}
`

type lexerParityCase struct {
	name   string
	source string
	want   string
}

type lexerParityStats struct {
	scanned            int
	unsupported        int
	unsupportedReasons map[string]int
	unsupportedSamples map[string]string
}

var lexerParityTokenKinds = map[token.Type]string{
	token.Function:    "Fn",
	token.Import:      "Import",
	token.Public:      "Pub",
	token.Struct:      "Struct",
	token.Enum:        "Enum",
	token.Union:       "Union",
	token.Extern:      "Extern",
	token.Let:         "Let",
	token.Var:         "Var",
	token.Return:      "Return",
	token.If:          "If",
	token.Else:        "Else",
	token.While:       "While",
	token.For:         "For",
	token.Break:       "Break",
	token.Continue:    "Continue",
	token.Match:       "Match",
	token.Unsafe:      "Unsafe",
	token.Comptime:    "Comptime",
	token.Try:         "Try",
	token.True:        "True",
	token.False:       "False",
	token.And:         "And",
	token.Or:          "Or",
	token.Ident:       "Ident",
	token.Lifetime:    "Lifetime",
	token.Int:         "Number",
	token.String:      "String",
	token.LBrace:      "LBrace",
	token.RBrace:      "RBrace",
	token.LParen:      "LParen",
	token.RParen:      "RParen",
	token.Semicolon:   "Semicolon",
	token.Comma:       "Comma",
	token.Colon:       "Colon",
	token.DoubleColon: "DoubleColon",
	token.Assign:      "Assign",
	token.Arrow:       "Arrow",
	token.FatArrow:    "FatArrow",
	token.Bang:        "Bang",
	token.Eq:          "Eq",
	token.NotEq:       "NotEq",
	token.Amp:         "Amp",
	token.LBracket:    "LBracket",
	token.RBracket:    "RBracket",
	token.LT:          "LT",
	token.LTE:         "LTE",
	token.GT:          "GT",
	token.GTE:         "GTE",
	token.Question:    "Question",
	token.Plus:        "Plus",
	token.Minus:       "Minus",
	token.Asterisk:    "Asterisk",
	token.Slash:       "Slash",
	token.Percent:     "Percent",
	token.Dot:         "Dot",
	token.Range:       "Range",
	token.Pipe:        "Pipe",
	token.Mut:         "Mut",
	token.EOF:         "Eof",
}

// TestStdKizuLexerParityExamples checks examples against the std Kizu lexer subset.
func TestStdKizuLexerParityExamples(t *testing.T) {
	examples, stats := collectLexerParityExamples(t)
	seeds := lexerParitySeedCases(t)
	cases := append(seeds, examples...)
	got := runStdKizuLexerParityHarness(t, cases)

	assertLexerParityCases(t, cases, got)
	t.Logf(
		"examples scanned=%d compared=%d unsupported=%d seeds=%d",
		stats.scanned,
		len(examples),
		stats.unsupported,
		len(seeds),
	)
	logUnsupportedLexerParityReasons(t, stats.unsupportedReasons, stats.unsupportedSamples)
}

// TestStdKizuLexerTokenizeParitySeeds checks the Array-backed token path.
func TestStdKizuLexerTokenizeParitySeeds(t *testing.T) {
	cases := lexerParitySeedCases(t)
	got := runStdKizuLexerTokenizeParityHarness(t, cases)

	assertLexerParityCases(t, cases, got)
}

// collectLexerParityExamples finds examples supported by the current std lexer subset.
func collectLexerParityExamples(t *testing.T) ([]lexerParityCase, lexerParityStats) {
	t.Helper()
	stats := lexerParityStats{
		unsupportedReasons: map[string]int{},
		unsupportedSamples: map[string]string{},
	}
	cases := []lexerParityCase{}
	err := filepath.WalkDir(parserParityExamplesRoot, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return err
		}
		stats.scanned++
		next, ok, reason := lexerParityExampleCase(path)
		if !ok {
			stats.unsupported++
			stats.unsupportedReasons[reason]++
			if stats.unsupportedSamples[reason] == "" {
				stats.unsupportedSamples[reason] = parserParityExampleName(path)
			}
			return nil
		}
		cases = append(cases, next)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	return cases, stats
}

// lexerParityExampleCase summarizes one example when the std lexer can scan it.
func lexerParityExampleCase(path string) (lexerParityCase, bool, string) {
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		return lexerParityCase{}, false, err.Error()
	}
	source := string(sourceBytes)
	if reason := unsupportedStdParserSource(source); reason != "" {
		return lexerParityCase{}, false, reason
	}
	want, reason := summarizeGoLexerSubset(source)
	if reason != "" {
		return lexerParityCase{}, false, reason
	}
	return lexerParityCase{
		name:   parserParityExampleName(path),
		source: source,
		want:   want,
	}, true, ""
}

// lexerParitySeedCases provides stable lexer coverage for the current subset.
func lexerParitySeedCases(t *testing.T) []lexerParityCase {
	t.Helper()
	seeds := []lexerParityCase{
		{name: "seed/empty", source: ""},
		{name: "seed/fn_empty", source: "fn main() {}"},
		{name: "seed/fn_return_int", source: "fn main() { return 1; }"},
		{name: "seed/fn_signature", source: "fn add(a: i64, b: i64) -> i64 { return a + b; }"},
		{
			name:   "seed/type_tokens",
			source: "fn f<'a>(a: &'a mut []'a const std::array::Array<i64>) -> !void {}",
		},
		{name: "seed/string_call", source: `fn main() { print("hello"); }`},
		{name: "seed/binary_precedence", source: "fn main() { print(1 + 2 * 3); }"},
		{
			name: "seed/statement_tokens",
			source: "fn main() { let x = true; var y = false; " +
				"if x and !y { try step(); } else { y = y or false; } " +
				"while y != true { break; continue; } for 0..3 |i| { print(i); } " +
				"match x { Yes => print(\"yes\"); } unsafe { return; } " +
				"comptime if 1 <= 2 { return; } }",
		},
		{name: "seed/operator_tokens", source: "a = b - c / d % e != f <= g > h >= i.x .. j | k => l"},
		{
			name: "seed/declaration_tokens",
			source: "import app::lexer; pub struct User { pub name: []const u8; } " +
				"enum Color { Red; Blue; } union Shape { Point; Circle(i64); } " +
				"extern \"c\" fn puts(s: ptr<const u8>) -> i32",
		},
	}
	for index := range seeds {
		want, reason := summarizeGoLexerSubset(seeds[index].source)
		if reason != "" {
			t.Fatalf("%s is unsupported: %s", seeds[index].name, reason)
		}
		seeds[index].want = want
	}
	return seeds
}

// summarizeGoLexerSubset returns a canonical summary for supported Go tokens.
func summarizeGoLexerSubset(source string) (string, string) {
	l := lexer.New(source)
	lines := []string{}
	for {
		tok := l.NextToken()
		kind, ok := lexerParityTokenKind(tok.Type)
		if !ok {
			return "", "token outside std lexer subset: " + string(tok.Type)
		}
		lines = append(lines, kind, lexerParityTokenLiteral(tok))
		if tok.Type == token.EOF {
			break
		}
	}
	return strings.Join(lines, "\n"), ""
}

// lexerParityTokenKind maps Go token kinds to std Kizu lexer summary labels.
func lexerParityTokenKind(tok token.Type) (string, bool) {
	kind, ok := lexerParityTokenKinds[tok]
	return kind, ok
}

// lexerParityTokenLiteral returns the literal spelling used by the std lexer dump.
func lexerParityTokenLiteral(tok token.Token) string {
	if tok.Type == token.String {
		return `"` + tok.Literal + `"`
	}
	return tok.Literal
}

// runStdKizuLexerParityHarness runs the Kizu std lexer once for all cases.
func runStdKizuLexerParityHarness(
	t *testing.T,
	cases []lexerParityCase,
) map[string]string {
	t.Helper()
	return runStdKizuLexerHarness(t, cases, "run_lexer_case")
}

// runStdKizuLexerTokenizeParityHarness runs the Array-backed std lexer path.
func runStdKizuLexerTokenizeParityHarness(
	t *testing.T,
	cases []lexerParityCase,
) map[string]string {
	t.Helper()
	return runStdKizuLexerHarness(t, cases, "run_tokenize_case")
}

// runStdKizuLexerHarness runs the Kizu std lexer once for all cases.
func runStdKizuLexerHarness(
	t *testing.T,
	cases []lexerParityCase,
	runner string,
) map[string]string {
	t.Helper()
	source, err := buildStdKizuLexerParityHarness(cases, runner)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "std_kizu_lexer_parity.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) > 0 {
		t.Fatalf("harness parse errors: %v", errs)
	}
	var out bytes.Buffer
	if err := interp.New(&out).Run(program); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out.String())
	}
	got, err := parseStdKizuLexerParityOutput(out.String())
	if err != nil {
		t.Fatalf("invalid harness output: %v\n%s", err, out.String())
	}
	return got
}

// buildStdKizuLexerParityHarness creates a Kizu program that lexes all cases.
func buildStdKizuLexerParityHarness(cases []lexerParityCase, runner string) (string, error) {
	var out strings.Builder
	out.WriteString(stdKizuLexerParityHarness)
	out.WriteString("\nfn main() -> !void {\n")
	out.WriteString("    let allocator = std::mem::page_allocator();\n")
	for index, testCase := range cases {
		name, err := kizuRawStringLiteral(testCase.name)
		if err != nil {
			return "", fmt.Errorf("%s name: %w", testCase.name, err)
		}
		source, cleanup, err := writeKizuSourceLiteral(&out, index, testCase.source)
		if err != nil {
			return "", fmt.Errorf("%s source: %w", testCase.name, err)
		}
		fmt.Fprintf(&out, "    try %s(%s, %s);\n", runner, name, source)
		if cleanup != "" {
			out.WriteString(cleanup)
		}
	}
	out.WriteString("    return;\n}\n")
	return out.String(), nil
}

// parseStdKizuLexerParityOutput extracts summaries printed by the harness.
func parseStdKizuLexerParityOutput(out string) (map[string]string, error) {
	result := map[string]string{}
	trimmed := strings.TrimSuffix(out, "\n")
	if trimmed == "" {
		return result, nil
	}
	lines := strings.Split(trimmed, "\n")
	for index := 0; index < len(lines); {
		if lines[index] != lexerParityCaseStart {
			return nil, fmt.Errorf("expected case start at line %d", index+1)
		}
		name, next, err := parseStdKizuLexerParityCase(lines, index+1)
		if err != nil {
			return nil, err
		}
		result[name] = strings.Join(lines[index+2:next], "\n")
		index = next + 1
	}
	return result, nil
}

// parseStdKizuLexerParityCase finds one case name and end delimiter.
func parseStdKizuLexerParityCase(lines []string, start int) (string, int, error) {
	if start >= len(lines) {
		return "", 0, fmt.Errorf("missing case name")
	}
	name := lines[start]
	index := start + 1
	for index < len(lines) && lines[index] != lexerParityCaseEnd {
		index++
	}
	if index >= len(lines) {
		return "", 0, fmt.Errorf("missing case end for %s", name)
	}
	return name, index, nil
}

// assertLexerParityCases compares all expected and actual summaries.
func assertLexerParityCases(
	t *testing.T,
	cases []lexerParityCase,
	got map[string]string,
) {
	t.Helper()
	wantNames := map[string]bool{}
	for _, testCase := range cases {
		wantNames[testCase.name] = true
		actual, ok := got[testCase.name]
		if !ok {
			t.Errorf("%s missing from harness output", testCase.name)
			continue
		}
		if actual != testCase.want {
			t.Errorf("%s summary mismatch\nwant:\n%s\ngot:\n%s", testCase.name, testCase.want, actual)
		}
	}
	for name := range got {
		if !wantNames[name] {
			t.Errorf("unexpected harness output for %s", name)
		}
	}
}

// logUnsupportedLexerParityReasons reports the most common unsupported reasons.
func logUnsupportedLexerParityReasons(
	t *testing.T,
	reasons map[string]int,
	samples map[string]string,
) {
	t.Helper()
	keys := make([]string, 0, len(reasons))
	for reason := range reasons {
		keys = append(keys, reason)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] == reasons[keys[j]] {
			return keys[i] < keys[j]
		}
		return reasons[keys[i]] > reasons[keys[j]]
	})
	for index, reason := range keys {
		if index == 5 {
			break
		}
		t.Logf(
			"unsupported[%d]=%s: %d sample=%s",
			index+1,
			reason,
			reasons[reason],
			samples[reason],
		)
	}
}
