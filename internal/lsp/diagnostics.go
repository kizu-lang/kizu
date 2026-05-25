package lsp

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdlib"
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
	if diagnostics := appendStdDecls(program, []string{source}); len(diagnostics) > 0 {
		return diagnostics
	}
	return checkProgramDiagnostics(program)
}

// analyzeDocument returns diagnostics using package context when available.
func (s *Server) analyzeDocument(uri string) []Diagnostic {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	path, ok := filePathFromURI(uri)
	if !ok {
		return Analyze(source)
	}
	root, found, err := findPackageRoot(path)
	if err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	if !found {
		return Analyze(source)
	}
	graph, err := loadPackageGraph(root)
	if err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	if !graphContainsFile(graph, path) {
		return Analyze(source)
	}
	program, err := project.LoadProgramWithSources(graph, s.packageSourceOverrides(graph))
	if err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	sources, err := s.packageSources(graph)
	if err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	if diagnostics := appendStdDecls(program, sources); len(diagnostics) > 0 {
		return diagnostics
	}
	return checkProgramDiagnostics(program)
}

// checkProgramDiagnostics runs semantic checks for a parsed program.
func checkProgramDiagnostics(program *ast.Program) []Diagnostic {
	if err := types.New().Check(program); err != nil {
		return []Diagnostic{diagnosticFromError(err)}
	}
	if err := ownership.New().Check(program); err != nil {
		return []Diagnostic{diagnosticFromError(err)}
	}
	return []Diagnostic{}
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

// packageSources returns graph source text, preferring open buffers over disk.
func (s *Server) packageSources(graph project.Graph) ([]string, error) {
	overrides := s.packageSourceOverrides(graph)
	sources := make([]string, 0, len(graph.Modules))
	for _, module := range graph.Modules {
		cleanPath := filepath.Clean(module.File)
		if source, ok := overrides[cleanPath]; ok {
			sources = append(sources, source)
			continue
		}
		data, err := os.ReadFile(module.File)
		if err != nil {
			return nil, err
		}
		sources = append(sources, string(data))
	}
	return sources, nil
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

// appendStdDecls prepends referenced std wrappers to match CLI check/run/test.
func appendStdDecls(program *ast.Program, sources []string) []Diagnostic {
	stdDecls, stdErrs, err := stdlib.DeclsForSources(sources)
	if err != nil {
		return []Diagnostic{diagnosticAtStart(err.Error())}
	}
	if len(stdErrs) > 0 {
		return []Diagnostic{diagnosticFromParseError(stdErrs[0])}
	}
	program.Decls = append(stdDecls, program.Decls...)
	return nil
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

type sourceSpanError interface {
	SourceSpan() ast.Span
}

// diagnosticFromError converts semantic errors into LSP diagnostics.
func diagnosticFromError(err error) Diagnostic {
	var withSpan sourceSpanError
	if errors.As(err, &withSpan) {
		span := withSpan.SourceSpan()
		if !span.IsZero() {
			return diagnosticAtSpan(err.Error(), span)
		}
	}
	return diagnosticAtStart(err.Error())
}

// diagnosticAtSpan reports a checker diagnostic at a source span.
func diagnosticAtSpan(message string, span ast.Span) Diagnostic {
	return Diagnostic{
		Range:    rangeFromSpan(span),
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

// rangeFromSpan converts a one-based AST span into a zero-based LSP range.
func rangeFromSpan(span ast.Span) Range {
	startLine, startChar := oneBasedPosition(span.Start)
	endLine, endChar := oneBasedPosition(span.End)
	if endLine < startLine || (endLine == startLine && endChar <= startChar) {
		endLine = startLine
		endChar = startChar + 1
	}
	return Range{
		Start: Position{Line: startLine, Character: startChar},
		End:   Position{Line: endLine, Character: endChar},
	}
}

// oneBasedPosition converts a one-based AST position to a zero-based LSP position.
func oneBasedPosition(pos ast.Position) (int, int) {
	line := pos.Line
	character := pos.Column
	if line <= 0 {
		line = 1
	}
	if character <= 0 {
		character = 1
	}
	return line - 1, character - 1
}

// oneCharacterRange creates a stable one-character diagnostic range.
func oneCharacterRange(line int, character int) Range {
	return Range{
		Start: Position{Line: line, Character: character},
		End:   Position{Line: line, Character: character + 1},
	}
}
