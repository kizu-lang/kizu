package lsp

import "github.com/kizu-lang/kizu/internal/unsafecap"

var keywordCompletionItems = []completionItem{
	{Label: "fn", Kind: completionItemKindKeyword},
	{Label: "test", Kind: completionItemKindKeyword},
	{Label: "import", Kind: completionItemKindKeyword},
	{Label: "pub", Kind: completionItemKindKeyword},
	{Label: "let", Kind: completionItemKindKeyword},
	{Label: "var", Kind: completionItemKindKeyword},
	{Label: "return", Kind: completionItemKindKeyword},
	{Label: "defer", Kind: completionItemKindKeyword},
	{Label: "errdefer", Kind: completionItemKindKeyword},
	{Label: "if", Kind: completionItemKindKeyword},
	{Label: "else", Kind: completionItemKindKeyword},
	{Label: "while", Kind: completionItemKindKeyword},
	{Label: "for", Kind: completionItemKindKeyword},
	{Label: "break", Kind: completionItemKindKeyword},
	{Label: "continue", Kind: completionItemKindKeyword},
	{Label: "match", Kind: completionItemKindKeyword},
	{Label: "struct", Kind: completionItemKindKeyword},
	{Label: "enum", Kind: completionItemKindKeyword},
	{Label: "union", Kind: completionItemKindKeyword},
	{Label: "contract", Kind: completionItemKindKeyword},
	{Label: "impl", Kind: completionItemKindKeyword},
	{Label: "unsafe", Kind: completionItemKindKeyword},
	{Label: "extern", Kind: completionItemKindKeyword},
	{Label: "comptime", Kind: completionItemKindKeyword},
	{Label: "try", Kind: completionItemKindKeyword},
	{Label: "dyn", Kind: completionItemKindKeyword},
	{Label: "true", Kind: completionItemKindValue},
	{Label: "false", Kind: completionItemKindValue},
	{Label: "and", Kind: completionItemKindKeyword},
	{Label: "or", Kind: completionItemKindKeyword},
}

var primitiveTypeCompletionItems = []completionItem{
	{Label: "bool", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "void", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "i8", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "i16", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "i32", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "i64", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "u8", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "u16", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "u32", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "u64", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "usize", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "isize", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "f32", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "f64", Kind: completionItemKindStruct, Detail: "primitive type"},
	{Label: "[]u8", Kind: completionItemKindStruct, Detail: "byte string"},
	{Label: "Function", Kind: completionItemKindStruct, Detail: "function type"},
	{Label: "type", Kind: completionItemKindStruct, Detail: "compile-time type value"},
}

var snippetCompletionItems = []completionItem{
	snippet("fn", "function declaration", "fn ${1:name}(${2}) -> ${3:void} {\n    $0\n}"),
	snippet("main", "main function", "fn main() {\n    $0\n}"),
	snippet("test", "test block", "test \"${1:name}\" {\n    $0\n}"),
	snippet("struct", "struct declaration", "struct ${1:Name} {\n    ${2:field}: ${3:i64},\n}"),
	snippet("enum", "enum declaration", "enum ${1:Name} {\n    ${2:Tag},\n}"),
	snippet("union", "union declaration", "union ${1:Name} {\n    ${2:Variant}(${3:i64}),\n}"),
	snippet(
		"method",
		"method declaration",
		"fn (self: ${1:Type}) ${2:name}() -> ${3:void} {\n    $0\n}",
	),
	snippet(
		"contract",
		"contract declaration",
		"contract ${1:Name} {\n    fn ${2:method}(self: &Self) -> ${3:void};\n}",
	),
	snippet("if", "if block", "if ${1:condition} {\n    $0\n}"),
	snippet("if else", "if/else block", "if ${1:condition} {\n    ${2}\n} else {\n    $0\n}"),
	snippet("while", "while loop", "while ${1:condition} {\n    $0\n}"),
	snippet("for", "range loop", "for ${1:start}..${2:end} |${3:i}| {\n    $0\n}"),
	snippet("match", "match expression", "match ${1:value} {\n    ${2:Tag} => ${3},\n}"),
	snippet("let", "let binding", "let ${1:name} = ${2:value};"),
	snippet("var", "mutable binding", "var ${1:name} = ${2:value};"),
	snippet("return", "return statement", "return ${1:value};"),
	snippet("defer", "defer cleanup", "defer ${1:value}.deinit();"),
	snippet("errdefer", "errdefer cleanup", "errdefer ${1:value}.deinit();"),
	snippet("unsafe", "unsafe capability block", "@unsafe(${1:ptr_read}) {\n    $0\n}"),
	snippet(
		"requires unsafe",
		"caller-obligation function declaration",
		"@requires_unsafe() fn ${1:name}(${2}) -> ${3:void} {\n    $0\n}",
	),
	snippet("comptime if", "comptime if block", "comptime if ${1:condition} {\n    $0\n}"),
	snippet("print", "print builtin", "print(${1:value})"),
	snippet("error", "error builtin", "error(${1:message})"),
	snippet("cast", "cast expression", "cast<${1:T}>(${2:value})"),
	snippet("type", "type expression", "type<${1:T}>"),
}

var directiveCompletionItems = []completionItem{
	snippet("unsafe", "unsafe capability block", "unsafe(${1:ptr_read}) {\n    $0\n}"),
	snippet(
		"requires_unsafe",
		"caller-obligation function",
		"requires_unsafe() fn ${1:name}(${2}) -> ${3:void} {\n    $0\n}",
	),
}

var unsafeCapabilityCompletionItems = buildUnsafeCapabilityCompletionItems()

// snippet builds one static snippet completion item.
func snippet(label string, detail string, insertText string) completionItem {
	return completionItem{
		Label:            label,
		Kind:             completionItemKindSnippet,
		Detail:           detail,
		InsertText:       insertText,
		InsertTextFormat: insertTextFormatSnippet,
	}
}

// buildUnsafeCapabilityCompletionItems builds @unsafe capability completion items.
func buildUnsafeCapabilityCompletionItems() []completionItem {
	docs := unsafecap.All()
	items := make([]completionItem, 0, len(docs))
	for _, info := range docs {
		items = append(items, unsafeCapability(info))
	}
	return items
}

// unsafeCapability builds one @unsafe capability completion item.
func unsafeCapability(info unsafecap.Info) completionItem {
	return completionItem{
		Label:         info.Name,
		Kind:          completionItemKindKeyword,
		Detail:        info.Detail,
		Documentation: markdownDocumentation(unsafecap.Markdown(info)),
	}
}
