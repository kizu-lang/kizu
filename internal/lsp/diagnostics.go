package lsp

import (
	"regexp"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
)

const diagnosticSource = "kizu"

var parsePositionPattern = regexp.MustCompile(` at ([0-9]+):([0-9]+)$`)

// Analyze returns diagnostics for one in-memory Kizu source file.
func Analyze(source string) []Diagnostic {
	program, parseErrors := parseSource(source)
	if len(parseErrors) > 0 {
		return []Diagnostic{diagnosticFromParseError(parseErrors[0])}
	}
	if err := types.New().Check(program); err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	if err := ownership.New().Check(program); err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	return []Diagnostic{}
}

// parseSource parses one in-memory Kizu document.
func parseSource(source string) (*ast.Program, []string) {
	p := parser.New(lexer.New(source))
	return p.ParseProgram(), p.Errors()
}

// diagnosticFromParseError converts a parser message into an LSP diagnostic.
func diagnosticFromParseError(message string) Diagnostic {
	line, character := parsePosition(message)
	return Diagnostic{
		Range:    oneCharacterRange(line, character),
		Severity: diagnosticSeverityError,
		Source:   diagnosticSource,
		Message:  message,
	}
}

// diagnosticAtStart reports a whole-file checker error at the first byte.
func diagnosticAtStart(message string) Diagnostic {
	return Diagnostic{
		Range:    oneCharacterRange(0, 0),
		Severity: diagnosticSeverityError,
		Source:   diagnosticSource,
		Message:  message,
	}
}

// parsePosition extracts a one-based parser position as a zero-based LSP position.
func parsePosition(message string) (int, int) {
	match := parsePositionPattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return 0, 0
	}
	line, lineErr := strconv.Atoi(match[1])
	column, columnErr := strconv.Atoi(match[2])
	if lineErr != nil || columnErr != nil {
		return 0, 0
	}
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	return line - 1, column - 1
}

// oneCharacterRange creates a stable one-character diagnostic range.
func oneCharacterRange(line int, character int) Range {
	return Range{
		Start: Position{Line: line, Character: character},
		End:   Position{Line: line, Character: character + 1},
	}
}
