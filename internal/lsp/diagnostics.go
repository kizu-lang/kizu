package lsp

import (
	"errors"
	"github.com/kizu-lang/kizu/internal/manifest"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/kizu-lang/kizu/internal/ast"
	diag "github.com/kizu-lang/kizu/internal/diagnostic"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/types"
)

const diagnosticSource = "kizu"

// Analyze returns diagnostics for one in-memory Kizu source file.
func Analyze(source string) []Diagnostic {
	program, parseErrors := parseSource(source)
	if len(parseErrors) > 0 {
		return parseErrorDiagnostics(parseErrors)
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

// checkProgramDiagnostics runs semantic checks for a parsed program, reporting
// every independent type error at once. Ownership checks only run once the
// program type-checks cleanly, since they assume a well-typed program.
func checkProgramDiagnostics(program *ast.Program) []Diagnostic {
	if typeErrors := types.New().CheckAll(program); len(typeErrors) > 0 {
		return diagnosticsFromErrors(typeErrors)
	}
	if moveErrors := ownership.New().CheckAll(program); len(moveErrors) > 0 {
		return diagnosticsFromErrors(moveErrors)
	}
	return []Diagnostic{}
}

// diagnosticsFromErrors converts checker errors into ordered LSP diagnostics.
func diagnosticsFromErrors(errs []error) []Diagnostic {
	diagnostics := make([]Diagnostic, 0, len(errs))
	for _, err := range errs {
		diagnostics = append(diagnostics, diagnosticFromError(err))
	}
	return sortedDiagnostics(diagnostics)
}

// parseErrorDiagnostics converts every parse error into an LSP diagnostic.
func parseErrorDiagnostics(parseErrors []parser.Diagnostic) []Diagnostic {
	diagnostics := make([]Diagnostic, 0, len(parseErrors))
	for _, parseError := range parseErrors {
		diagnostics = append(diagnostics, diagnosticFromParseError(parseError))
	}
	return sortedDiagnostics(diagnostics)
}

// sortedDiagnostics orders diagnostics by start position for stable display.
func sortedDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i].Range.Start, diagnostics[j].Range.Start
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Character < right.Character
	})
	return diagnostics
}

// loadPackageGraph parses the package manifest and resolves its module graph.
func loadPackageGraph(root string) (project.Graph, error) {
	source, err := os.ReadFile(filepath.Join(root, "kizu.toml"))
	if err != nil {
		return project.Graph{}, err
	}
	manifest, err := manifest.ParseManifest(string(source))
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
func parseSource(source string) (*ast.Program, []parser.Diagnostic) {
	p := parser.New(lexer.New(source))
	return p.ParseProgram(), p.Diagnostics()
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
func diagnosticFromParseError(diag parser.Diagnostic) Diagnostic {
	span := diag.SourceSpan()
	if !span.IsZero() {
		return diagnosticAtSpan(diag.Error(), span, lspSeverity(diag.SeverityLevel()))
	}
	return Diagnostic{
		Range:    oneCharacterRange(0, 0),
		Severity: lspSeverity(diag.SeverityLevel()),
		Source:   diagnosticSource,
		Message:  diag.Error(),
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
	var structured *diag.Diagnostic
	if errors.As(err, &structured) {
		span := structured.SourceSpan()
		if !span.IsZero() {
			result := diagnosticAtSpan(structured.Error(), span, lspSeverity(structured.SeverityLevel()))
			result.Code = structured.Code
			return result
		}
		return Diagnostic{
			Range:    oneCharacterRange(0, 0),
			Severity: lspSeverity(structured.SeverityLevel()),
			Code:     structured.Code,
			Source:   diagnosticSource,
			Message:  structured.Error(),
		}
	}
	var withSpan sourceSpanError
	if errors.As(err, &withSpan) {
		span := withSpan.SourceSpan()
		if !span.IsZero() {
			return diagnosticAtSpan(err.Error(), span, diagnosticSeverityError)
		}
	}
	return diagnosticAtStart(err.Error())
}

// diagnosticAtSpan reports a checker diagnostic at a source span.
func diagnosticAtSpan(message string, span ast.Span, severity int) Diagnostic {
	return Diagnostic{
		Range:    rangeFromSpan(span),
		Severity: severity,
		Source:   diagnosticSource,
		Message:  message,
	}
}

// lspSeverity maps compiler diagnostic severities onto LSP severity codes.
func lspSeverity(severity diag.Severity) int {
	if severity == diag.SeverityWarning {
		return diagnosticSeverityWarning
	}
	return diagnosticSeverityError
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
