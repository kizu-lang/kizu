package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/token"
	"github.com/kizu-lang/kizu/internal/types"
)

const diagnosticSource = "kizu"

var (
	parsePositionPattern = regexp.MustCompile(` at ([0-9]+):([0-9]+)$`)
	quotedNamePattern    = regexp.MustCompile("`([^`]+)`")
	identifierPattern    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// Analyze returns diagnostics for one in-memory Kizu source file.
func Analyze(source string) []Diagnostic {
	program, parseErrors := parseSource(source)
	if len(parseErrors) > 0 {
		return []Diagnostic{diagnosticFromParseError(parseErrors[0])}
	}
	return checkProgramDiagnostics(source, program)
}

// analyzeDocument returns diagnostics using package context when available.
func (s *Server) analyzeDocument(uri string) []Diagnostic {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	graph, hasGraph, err := s.packageGraph(uri)
	if err != nil {
		return []Diagnostic{diagnosticFromMessage(source, err.Error())}
	}
	if !hasGraph {
		return Analyze(source)
	}
	program, err := project.LoadProgramWithSources(graph, s.packageSourceOverrides(graph))
	if err != nil {
		return []Diagnostic{diagnosticFromMessage(source, err.Error())}
	}
	return checkProgramDiagnostics(source, program)
}

// checkProgramDiagnostics runs semantic checks for a parsed program.
func checkProgramDiagnostics(source string, program *ast.Program) []Diagnostic {
	if err := types.New().Check(program); err != nil {
		return []Diagnostic{diagnosticFromMessage(source, err.Error())}
	}
	if err := ownership.New().Check(program); err != nil {
		return []Diagnostic{diagnosticFromMessage(source, err.Error())}
	}
	return []Diagnostic{}
}

// packageGraphForURI returns the resolved graph for completion, ignoring graph errors.
func (s *Server) packageGraphForURI(uri string) (project.Graph, bool) {
	graph, ok, err := s.packageGraph(uri)
	if err != nil {
		return project.Graph{}, false
	}
	return graph, ok
}

// packageGraph resolves the package graph for an opened file-backed document.
func (s *Server) packageGraph(uri string) (project.Graph, bool, error) {
	path, ok := filePathFromURI(uri)
	if !ok {
		return project.Graph{}, false, nil
	}
	root, found, err := findPackageRoot(path)
	if err != nil {
		return project.Graph{}, false, err
	}
	if !found {
		return project.Graph{}, false, nil
	}
	graph, err := loadPackageGraph(root)
	if err != nil {
		return project.Graph{}, false, err
	}
	if !graphContainsFile(graph, path) {
		return project.Graph{}, false, nil
	}
	return graph, true, nil
}

// loadPackageGraph parses the package manifest and resolves its module graph.
func loadPackageGraph(root string) (project.Graph, error) {
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		return project.Graph{}, err
	}
	manifest, err := project.ParseManifest(string(source))
	if err != nil {
		return project.Graph{}, err
	}
	return project.ResolveModules(root, manifest)
}

// packageSourceOverrides returns open buffers that correspond to graph modules.
func (s *Server) packageSourceOverrides(graph project.Graph) map[string]string {
	moduleFiles := map[string]bool{}
	for _, module := range graph.Modules {
		moduleFiles[filepath.Clean(module.File)] = true
	}
	sources := map[string]string{}
	for uri, source := range s.documents {
		path, ok := filePathFromURI(uri)
		if !ok {
			continue
		}
		cleanPath := filepath.Clean(path)
		if moduleFiles[cleanPath] {
			sources[cleanPath] = source
		}
	}
	return sources
}

// findPackageRoot finds the nearest parent directory containing kizu.toml.
func findPackageRoot(path string) (string, bool, error) {
	dir := filepath.Dir(filepath.Clean(path))
	for {
		manifest := filepath.Join(dir, "kizu.toml")
		info, err := os.Stat(manifest)
		if err == nil {
			if info.IsDir() {
				return "", false, errPackageManifestIsDir(manifest)
			}
			return dir, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// errPackageManifestIsDir reports an invalid manifest path shape.
func errPackageManifestIsDir(path string) error {
	return &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
}

// graphContainsFile reports whether path is one of the resolved graph modules.
func graphContainsFile(graph project.Graph, path string) bool {
	cleanPath := filepath.Clean(path)
	for _, module := range graph.Modules {
		if filepath.Clean(module.File) == cleanPath {
			return true
		}
	}
	return false
}

// filePathFromURI converts local file URIs into file system paths.
func filePathFromURI(rawURI string) (string, bool) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme != "file" || parsed.Path == "" {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", false
	}
	return filepath.Clean(filepath.FromSlash(parsed.Path)), true
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

// diagnosticFromMessage anchors checker messages to a referenced source token.
func diagnosticFromMessage(source string, message string) Diagnostic {
	if line, character, ok := parsePositionOK(message); ok {
		return Diagnostic{
			Range:    oneCharacterRange(line, character),
			Severity: diagnosticSeverityError,
			Source:   diagnosticSource,
			Message:  message,
		}
	}
	if tokenRange, ok := diagnosticTokenRange(source, message); ok {
		return Diagnostic{
			Range:    tokenRange,
			Severity: diagnosticSeverityError,
			Source:   diagnosticSource,
			Message:  message,
		}
	}
	return diagnosticAtStart(message)
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
	line, character, _ := parsePositionOK(message)
	return line, character
}

// parsePositionOK extracts a one-based parser position as a zero-based LSP position.
func parsePositionOK(message string) (int, int, bool) {
	match := parsePositionPattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return 0, 0, false
	}
	line, lineErr := strconv.Atoi(match[1])
	column, columnErr := strconv.Atoi(match[2])
	if lineErr != nil || columnErr != nil {
		return 0, 0, false
	}
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	return line - 1, column - 1, true
}

// diagnosticTokenRange finds the most specific quoted identifier in source.
func diagnosticTokenRange(source string, message string) (Range, bool) {
	for _, candidate := range diagnosticNameCandidates(message) {
		if tokenRange, ok := lastIdentifierRange(source, candidate); ok {
			return tokenRange, true
		}
	}
	return Range{}, false
}

// diagnosticNameCandidates returns likely source identifiers from quoted names.
func diagnosticNameCandidates(message string) []string {
	var candidates []string
	for _, match := range quotedNamePattern.FindAllStringSubmatch(message, -1) {
		if len(match) != 2 {
			continue
		}
		parts := identifierPattern.FindAllString(match[1], -1)
		for idx := len(parts) - 1; idx >= 0; idx-- {
			candidates = append(candidates, parts[idx])
		}
		if strings.IndexFunc(match[1], func(r rune) bool {
			return !(r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
		}) < 0 {
			candidates = append(candidates, match[1])
		}
	}
	return candidates
}

// lastIdentifierRange returns the final matching identifier token range in source.
func lastIdentifierRange(source string, name string) (Range, bool) {
	l := lexer.New(source)
	var out Range
	found := false
	for {
		tok := l.NextToken()
		if tok.Type == token.EOF {
			break
		}
		if tok.Type != token.Ident && tok.Literal != name {
			continue
		}
		if tok.Literal == name {
			out = tokenRange(tok)
			found = true
		}
	}
	return out, found
}

// tokenRange converts a lexer token into a zero-based LSP range.
func tokenRange(tok token.Token) Range {
	line := tok.Line - 1
	character := tok.Column - 1
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	return Range{
		Start: Position{Line: line, Character: character},
		End:   Position{Line: line, Character: character + utf16Len(tok.Literal)},
	}
}

// utf16Len returns the number of UTF-16 code units in text.
func utf16Len(text string) int {
	units := 0
	for _, r := range text {
		if r >= 0x10000 {
			units += 2
			continue
		}
		units++
	}
	return units
}

// oneCharacterRange creates a stable one-character diagnostic range.
func oneCharacterRange(line int, character int) Range {
	return Range{
		Start: Position{Line: line, Character: character},
		End:   Position{Line: line, Character: character + 1},
	}
}
