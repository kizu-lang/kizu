package lsp

import (
	"sort"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/project"
)

const (
	completionItemKindFunction = 3
	completionItemKindModule   = 9
	completionItemKindKeyword  = 14
	completionItemKindEnum     = 13
	completionItemKindStruct   = 22
)

var keywordCompletionLabels = []string{
	"fn",
	"import",
	"pub",
	"let",
	"var",
	"return",
	"defer",
	"if",
	"else",
	"while",
	"break",
	"continue",
	"match",
	"struct",
	"enum",
	"union",
	"contract",
	"impl",
	"unsafe",
	"extern",
	"comptime",
	"try",
}

var primitiveCompletionLabels = []string{
	"i64",
	"u8",
	"u64",
	"usize",
	"f64",
	"bool",
	"void",
	"[]u8",
}

// CompletionItems returns basic completion candidates for one Kizu document.
func CompletionItems(source string, graph project.Graph, hasGraph bool) []completionItem {
	builder := newCompletionBuilder()
	for _, label := range keywordCompletionLabels {
		builder.add(label, completionItemKindKeyword, "keyword")
	}
	for _, label := range primitiveCompletionLabels {
		builder.add(label, completionItemKindKeyword, "primitive type")
	}
	for _, item := range declarationCompletionItems(source) {
		builder.add(item.Label, item.Kind, item.Detail)
	}
	if hasGraph {
		for _, module := range graph.Modules {
			builder.add(module.Path, completionItemKindModule, "module")
		}
	}
	return builder.items()
}

type completionBuilder struct {
	seen map[string]bool
	out  []completionItem
}

// newCompletionBuilder creates a deterministic de-duplicating item collector.
func newCompletionBuilder() *completionBuilder {
	return &completionBuilder{seen: map[string]bool{}}
}

// add records one completion item unless a label was already emitted.
func (b *completionBuilder) add(label string, kind int, detail string) {
	if label == "" || b.seen[label] {
		return
	}
	b.seen[label] = true
	b.out = append(b.out, completionItem{
		Label:  label,
		Kind:   kind,
		Detail: detail,
	})
}

// items returns completion items in insertion order.
func (b *completionBuilder) items() []completionItem {
	return b.out
}

// declarationCompletionItems extracts top-level declaration completions.
func declarationCompletionItems(source string) []completionItem {
	program, parseErrors := parseSource(source)
	if len(parseErrors) > 0 {
		return nil
	}
	items := make([]completionItem, 0, len(program.Decls))
	for _, decl := range program.Decls {
		item, ok := declarationCompletionItem(decl)
		if ok {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].Label < items[right].Label
	})
	return items
}

// declarationCompletionItem converts one AST declaration into a completion item.
func declarationCompletionItem(decl ast.Decl) (completionItem, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return completionItem{
			Label:  d.Name,
			Kind:   completionItemKindFunction,
			Detail: "function",
		}, true
	case *ast.StructDecl:
		return completionItem{
			Label:  d.Name,
			Kind:   completionItemKindStruct,
			Detail: "struct",
		}, true
	case *ast.EnumDecl:
		return completionItem{
			Label:  d.Name,
			Kind:   completionItemKindEnum,
			Detail: "enum",
		}, true
	case *ast.UnionDecl:
		return completionItem{
			Label:  d.Name,
			Kind:   completionItemKindStruct,
			Detail: "union",
		}, true
	case *ast.ContractDecl:
		return completionItem{
			Label:  d.Name,
			Kind:   completionItemKindStruct,
			Detail: "contract",
		}, true
	default:
		return completionItem{}, false
	}
}
