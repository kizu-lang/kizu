package lsp

import (
	"regexp"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/project"
)

const (
	completionItemKindFunction = 3
	completionItemKindMethod   = 2
	completionItemKindModule   = 9
	completionItemKindKeyword  = 14
	completionItemKindEnum     = 13
	completionItemKindMember   = 20
	completionItemKindStruct   = 22
)

var (
	namespaceCompletionPattern = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)::$`)
	receiverCompletionPattern  = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.$`)
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
func CompletionItems(
	source string,
	position Position,
	graph project.Graph,
	hasGraph bool,
) []completionItem {
	program := completionProgram(source)
	context := completionContextAt(source, position)
	if context.namespace != "" {
		return scopedCompletionItems(program, context.namespace)
	}
	if context.receiver != "" {
		return receiverCompletionItems(program, context.receiver)
	}

	builder := newCompletionBuilder()
	for _, label := range keywordCompletionLabels {
		builder.add(label, completionItemKindKeyword, "keyword")
	}
	for _, label := range primitiveCompletionLabels {
		builder.add(label, completionItemKindKeyword, "primitive type")
	}
	for _, item := range declarationCompletionItems(program) {
		builder.add(item.Label, item.Kind, item.Detail)
	}
	if hasGraph {
		for _, module := range graph.Modules {
			builder.add(module.Path, completionItemKindModule, "module")
		}
	}
	return builder.items()
}

type completionContext struct {
	namespace string
	receiver  string
}

// completionContextAt returns the namespace or receiver expression before the cursor.
func completionContextAt(source string, position Position) completionContext {
	before := sourceBeforePosition(source, position)
	if match := namespaceCompletionPattern.FindStringSubmatch(before); len(match) == 2 {
		return completionContext{namespace: match[1]}
	}
	if match := receiverCompletionPattern.FindStringSubmatch(before); len(match) == 2 {
		return completionContext{receiver: match[1]}
	}
	return completionContext{}
}

// sourceBeforePosition returns source text before a zero-based LSP position.
func sourceBeforePosition(source string, position Position) string {
	line := 0
	character := 0
	for idx, r := range source {
		if line == position.Line && character >= position.Character {
			return source[:idx]
		}
		if r == '\n' {
			if line == position.Line {
				return source[:idx]
			}
			line++
			character = 0
			continue
		}
		if r >= 0x10000 {
			character += 2
			continue
		}
		character++
	}
	return source
}

// scopedCompletionItems returns enum tags or union variants after `Type::`.
func scopedCompletionItems(program *ast.Program, typeName string) []completionItem {
	builder := newCompletionBuilder()
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.EnumDecl:
			if d.Name != typeName {
				continue
			}
			for _, tag := range d.Tags {
				builder.add(tag, completionItemKindMember, d.Name+" enum tag")
			}
		case *ast.UnionDecl:
			if d.Name != typeName {
				continue
			}
			for _, variant := range d.Variants {
				builder.add(variant.Name, completionItemKindMember, d.Name+" union variant")
			}
		}
	}
	return builder.items()
}

// receiverCompletionItems returns impl methods for the receiver binding type.
func receiverCompletionItems(program *ast.Program, receiver string) []completionItem {
	typeName := receiverBindingType(program, receiver)
	if typeName == "" {
		return nil
	}
	typeName = completionTypeBase(typeName)
	builder := newCompletionBuilder()
	for _, decl := range program.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok || completionTypeBase(impl.TypeName) != typeName {
			continue
		}
		for _, method := range impl.Methods {
			builder.add(method.Name, completionItemKindMethod, impl.TypeName+" method")
		}
	}
	return builder.items()
}

// receiverBindingType finds the best-effort type for a receiver name.
func receiverBindingType(program *ast.Program, receiver string) string {
	var found string
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || fn.Body == nil {
			continue
		}
		for _, param := range fn.Params {
			if param.Name == receiver {
				found = param.TypeName
			}
		}
		if typ := receiverBindingTypeInBlock(fn.Body, receiver); typ != "" {
			found = typ
		}
	}
	return found
}

// receiverBindingTypeInBlock infers simple local binding types from a block.
func receiverBindingTypeInBlock(block *ast.BlockStmt, receiver string) string {
	for _, stmt := range block.Statements {
		if typ := receiverBindingTypeInStatement(stmt, receiver); typ != "" {
			return typ
		}
	}
	return ""
}

// receiverBindingTypeInStatement infers simple local binding types from one statement.
func receiverBindingTypeInStatement(stmt ast.Statement, receiver string) string {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		if s.Name == receiver {
			return expressionTypeName(s.Value)
		}
	case *ast.BlockStmt:
		return receiverBindingTypeInBlock(s, receiver)
	case *ast.IfStmt:
		if typ := receiverBindingTypeInBlock(s.Consequence, receiver); typ != "" {
			return typ
		}
		if s.Alternative != nil {
			return receiverBindingTypeInBlock(s.Alternative, receiver)
		}
	case *ast.WhileStmt:
		return receiverBindingTypeInBlock(s.Body, receiver)
	case *ast.ForStmt:
		return receiverBindingTypeInBlock(s.Body, receiver)
	}
	return ""
}

// expressionTypeName infers the obvious constructed type for local completion.
func expressionTypeName(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.StructLiteralExpr:
		return e.TypeName
	case *ast.CallExpr:
		return expressionConstructorName(e.Callee)
	case *ast.TypeApplyExpr:
		return expressionConstructorName(e.Callee)
	default:
		return ""
	}
}

// expressionConstructorName returns a constructor-like callee name.
func expressionConstructorName(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if startsUpper(e.Name) {
			return e.Name
		}
	case *ast.FieldExpr:
		if e.Namespace && startsUpper(e.Name) {
			parts := expressionNamespaceParts(e)
			if len(parts) > 0 {
				return strings.Join(parts, "::")
			}
		}
	}
	return ""
}

// expressionNamespaceParts extracts qualified namespace expression names.
func expressionNamespaceParts(expr ast.Expression) []string {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return []string{e.Name}
	case *ast.FieldExpr:
		if !e.Namespace {
			return nil
		}
		parts := expressionNamespaceParts(e.Receiver)
		if len(parts) == 0 {
			return nil
		}
		return append(parts, e.Name)
	default:
		return nil
	}
}

// completionTypeBase strips namespace and static arguments from a type name.
func completionTypeBase(typeName string) string {
	if idx := strings.Index(typeName, "<"); idx >= 0 {
		typeName = typeName[:idx]
	}
	if idx := strings.LastIndex(typeName, "::"); idx >= 0 {
		typeName = typeName[idx+2:]
	}
	return typeName
}

// startsUpper reports whether name looks like a type or constructor.
func startsUpper(name string) bool {
	if name == "" {
		return false
	}
	first := name[0]
	return first >= 'A' && first <= 'Z'
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
func declarationCompletionItems(program *ast.Program) []completionItem {
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

// completionProgram parses a best-effort AST for completion candidates.
func completionProgram(source string) *ast.Program {
	program, _ := parseSource(source)
	return program
}
