// Package selfhost_test validates the Kizu self-host compiler skeleton.
package selfhost_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
)

const maxLineWidth = 100
const maxFunctionLines = 70
const maxFunctionStatements = 45

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
	program := parseSelfHostSource(t, filepath.Join(repoRoot(t), "selfhost", "frontend.kizu"))
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	var out bytes.Buffer
	if err := interp.New(&out).Run(program); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got, want := out.String(), "parsed functions\n1\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
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
