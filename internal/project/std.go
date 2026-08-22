package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/manifest"
	"github.com/kizu-lang/kizu/internal/source"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/token"
)

// StdGraph resolves the standard library as a package, the same way a program's
// own package is resolved. std has a manifest and a source tree and nothing
// else: there is one loader, and std goes through it.
func StdGraph() (Graph, error) {
	stdGraph.Do(func() { stdGraph.graph, stdGraph.err = readStdGraph() })
	return stdGraph.graph, stdGraph.err
}

var stdGraph struct {
	sync.Once
	graph Graph
	err   error
}

// readStdGraph reads the std manifest and source tree from the library tree.
func readStdGraph() (Graph, error) {
	dir, err := stdlib.FindLibFile(stdlib.Root)
	if err != nil {
		return Graph{}, err
	}
	source, err := os.ReadFile(filepath.Join(dir, "kizu.toml"))
	if err != nil {
		return Graph{}, err
	}
	parsed, err := manifest.ParseStdManifest(string(source))
	if err != nil {
		return Graph{}, err
	}
	return ResolveModules(dir, parsed)
}

// stdModule reports whether path names a module of the standard library.
func stdModule(path string) (bool, error) {
	graph, err := StdGraph()
	if err != nil {
		return false, err
	}
	for _, module := range graph.Modules {
		if module.Path == path && len(module.Files) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// stdModulesFor returns the std modules a set of import paths reaches, each one
// after the modules it is built on. A program pays for the part of std it
// imports rather than for all of it: loading every module costs a third of a
// check and twenty kilobytes of every binary.
func stdModulesFor(graph Graph, imports []string) ([]Module, map[string]string, error) {
	loader := &stdLoader{
		modules: map[string]Module{},
		visited: map[string]bool{},
		sources: map[string]string{},
	}
	for _, module := range graph.Modules {
		if len(module.Files) == 0 {
			continue
		}
		loader.modules[module.Path] = module
		loader.paths = append(loader.paths, module.Path)
	}
	sort.Strings(loader.paths)
	for _, path := range imports {
		for _, wanted := range loader.named(path) {
			if err := loader.visit(wanted); err != nil {
				return nil, nil, err
			}
		}
	}
	return loader.out, loader.sources, nil
}

// stdLoader walks std modules, pulling in what each one names.
type stdLoader struct {
	modules map[string]Module
	paths   []string
	visited map[string]bool
	out     []Module
	// sources keeps what visit read, so the parse that follows does not open the
	// same file a second time.
	sources map[string]string
}

// named returns the std modules one import path names. Importing the package
// root names every module a program outside std may reach, because that is what
// a path through the root can spell.
func (l *stdLoader) named(path string) []string {
	if path == stdlib.Root {
		wanted := []string{}
		for _, module := range l.paths {
			if _, _, internal := internalModule(module); !internal {
				wanted = append(wanted, module)
			}
		}
		return wanted
	}
	if _, ok := l.modules[path]; ok {
		return []string{path}
	}
	return nil
}

// visit adds one std module after adding the modules its source names.
func (l *stdLoader) visit(path string) error {
	// Marked on the way in: a module already being visited is one this walk
	// reached through itself, and adding it again would not end.
	if l.visited[path] {
		return nil
	}
	l.visited[path] = true
	module := l.modules[path]
	for _, file := range module.Files {
		source, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		l.sources[file] = string(source)
		for _, dependency := range l.references(string(source)) {
			if err := l.visit(dependency); err != nil {
				return err
			}
		}
	}
	l.out = append(l.out, module)
	return nil
}

// references returns the std modules a source names. std modules reach their
// siblings through the package root namespace rather than through imports, so
// what one is built on is read off the names it writes.
func (l *stdLoader) references(source string) []string {
	seen := scanStdPaths(source)
	modules := []string{}
	for _, path := range l.paths {
		if seen[path] {
			modules = append(modules, path)
		}
	}
	return modules
}

// scanStdPaths records the std module paths a source names, skipping the ones
// inside string literals and comments by reading tokens rather than text.
func scanStdPaths(source string) map[string]bool {
	seen := map[string]bool{}
	lex := lexer.New(source)
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			return seen
		}
		if tok.Type != token.Ident || tok.Literal != stdlib.Root {
			continue
		}
		parts := append([]string{stdlib.Root}, readNamespaceParts(lex)...)
		// The last segment names an item, so every shorter prefix is a module
		// this source could be built on.
		for size := 2; size < len(parts); size++ {
			seen[strings.Join(parts[:size], "::")] = true
		}
	}
}

// readNamespaceParts reads a namespace chain after the initial root name.
func readNamespaceParts(lex *lexer.Lexer) []string {
	parts := []string{}
	for {
		sep := lex.NextToken()
		if sep.Type != token.DoubleColon {
			return parts
		}
		part := lex.NextToken()
		if part.Type != token.Ident {
			return parts
		}
		parts = append(parts, part.Literal)
	}
}

// StdErrorSets returns every error set std declares, with the number each member
// lowers to. The runtime refers to them all whatever a program uses, so they are
// read from the whole of std rather than from the modules one program imported.
//
// Numbering is global: an error value is one integer, and that integer means the
// same member in every error union it travels through, so no union-to-union
// conversion exists. Code 0 is reserved for "no error".
func StdErrorSets() (map[string]map[string]int, error) {
	stdErrors.Do(func() { stdErrors.sets, stdErrors.base, stdErrors.err = readStdErrorSets() })
	return stdErrors.sets, stdErrors.err
}

// StdErrorCodeBase returns the first code available to error sets a program
// declares itself, one past the last code std claims. It is the counter
// readStdErrorSets stopped at, so the two cannot drift.
func StdErrorCodeBase() (int, error) {
	if _, err := StdErrorSets(); err != nil {
		return 0, err
	}
	return stdErrors.base, nil
}

var stdErrors struct {
	sync.Once
	sets map[string]map[string]int
	base int
	err  error
}

// readStdErrorSets parses std once for the numbers its error set members lower
// to. Modules are read in module-path order and files in path order, so the
// numbers a build assigns depend on what std declares and on nothing else.
func readStdErrorSets() (map[string]map[string]int, int, error) {
	graph, err := StdGraph()
	if err != nil {
		return nil, 0, err
	}
	parser := &graphChecker{sources: source.NewMap()}
	sets := map[string]map[string]int{}
	code := 1
	for _, module := range graph.Modules {
		for _, file := range module.Files {
			program, err := parser.parseModuleFile(file)
			if err != nil {
				return nil, 0, err
			}
			for _, decl := range program.Decls {
				set, ok := decl.(*ast.ErrorSetDecl)
				if !ok {
					continue
				}
				members := map[string]int{}
				for _, member := range set.Members {
					members[member] = code
					code++
				}
				sets[module.Path+"::"+set.Name] = members
			}
		}
	}
	return sets, code, nil
}
