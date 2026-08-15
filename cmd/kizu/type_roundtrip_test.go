package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kizuast "github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/typ"
)

// TestTypeSpellingRoundTrip keeps the two readers of Kizu type syntax in
// agreement. internal/parser reads what a source file writes; typ.Parse reads
// the spelling typ.String prints. A type that survives one and not the other is
// a type the compiler would understand differently depending on which side of
// the AST it was asked from -- which is how `std::array::Array<!i64>` once
// became `std::array::Array<`.
func TestTypeSpellingRoundTrip(t *testing.T) {
	roots := []string{"../../examples", "../../lib/kizu/std"}
	seen := map[string]bool{}
	checked := 0
	for _, root := range roots {
		for _, path := range kizuSourcePaths(t, root) {
			for _, declared := range declaredTypes(t, path) {
				text := declared.String()
				if seen[text] {
					continue
				}
				seen[text] = true
				checked++
				reparsed, err := typ.Parse(text)
				if err != nil {
					t.Errorf("%s: typ.Parse(%q): %v", path, text, err)
					continue
				}
				if !typ.Equal(declared, reparsed) {
					t.Errorf("%s: %q reparsed as %q", path, text, reparsed.String())
				}
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d distinct type spellings checked, expected the corpus", checked)
	}
}

// kizuSourcePaths lists every Kizu source file under root.
func kizuSourcePaths(t *testing.T, root string) []string {
	t.Helper()
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".kizu") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return paths
}

// declaredTypes returns every type a file's declarations write.
func declaredTypes(t *testing.T, path string) []typ.Type {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p := parser.New(lexer.NewFile(path, string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		return nil
	}
	types := []typ.Type{}
	add := func(node typ.Type) {
		if node != nil {
			types = append(types, node)
		}
	}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *kizuast.FunctionDecl:
			types = append(types, functionTypes(d)...)
		case *kizuast.StructDecl:
			for _, field := range d.Fields {
				add(field.TypeName)
			}
		case *kizuast.UnionDecl:
			for _, variant := range d.Variants {
				add(variant.Payload)
			}
		case *kizuast.ContractDecl:
			for _, method := range d.Methods {
				types = append(types, functionTypes(method)...)
			}
		}
	}
	return types
}

// functionTypes returns the types one function signature writes.
func functionTypes(fn *kizuast.FunctionDecl) []typ.Type {
	types := []typ.Type{}
	for _, param := range fn.Params {
		if param.TypeName != nil {
			types = append(types, param.TypeName)
		}
	}
	for _, param := range fn.StaticParams {
		if param.Type != nil {
			types = append(types, param.Type)
		}
	}
	if fn.ReturnType != nil {
		types = append(types, fn.ReturnType)
	}
	return types
}
