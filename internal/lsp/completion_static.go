package lsp

import "strings"

var keywordCompletionItems = []completionItem{
	{Label: "fn", Kind: completionItemKindKeyword},
	{Label: "test", Kind: completionItemKindKeyword},
	{Label: "import", Kind: completionItemKindKeyword},
	{Label: "pub", Kind: completionItemKindKeyword},
	{Label: "let", Kind: completionItemKindKeyword},
	{Label: "var", Kind: completionItemKindKeyword},
	{Label: "return", Kind: completionItemKindKeyword},
	{Label: "defer", Kind: completionItemKindKeyword},
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
		"impl",
		"impl block",
		"impl ${1:Type} {\n    fn ${2:method}(self: ${1:Type}) -> ${3:void} {\n        $0\n    }\n}",
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

type unsafeCapabilityDoc struct {
	label   string
	detail  string
	permits []string
}

var unsafeCapabilityDocs = []unsafeCapabilityDoc{
	{
		label:  "extern_call",
		detail: "extern function call",
		permits: []string{
			"Calling `extern \"c\" fn` declarations.",
		},
	},
	{
		label:  "ptr_cast",
		detail: "raw pointer cast",
		permits: []string{
			"Raw pointer to raw pointer casts with `cast<ptr<...>>(value)`.",
		},
	},
	{
		label:  "ptr_deref",
		detail: "raw pointer dereference",
		permits: []string{
			"Reading through `p.*`.",
			"Writing through `p.* = value` when the pointer is mutable.",
			"Reading or assigning struct fields through `p.*.field`.",
		},
	},
	{
		label:  "ptr_int_cast",
		detail: "integer and pointer conversion",
		permits: []string{
			"Creating raw pointers with `ptr_from_int<ptr<...>>(value)`.",
			"Converting raw pointers to integers with `int_from_ptr<usize>(value)`.",
		},
	},
	{
		label:  "ptr_read",
		detail: "raw pointer read",
		permits: []string{
			"Reading a raw pointer with `ptr_read(p)`.",
		},
	},
	{
		label:  "ptr_write",
		detail: "raw pointer write",
		permits: []string{
			"Writing a mutable raw pointer with `ptr_write(p, value)`.",
		},
	},
	{
		label:  "unsafe_call",
		detail: "caller-obligation function call",
		permits: []string{
			"Calling functions or methods declared with `@requires_unsafe()`.",
		},
	},
	{
		label:  "volatile",
		detail: "volatile read or write",
		permits: []string{
			"Volatile raw pointer reads with `volatile_read(p)`.",
			"Volatile raw pointer writes with `volatile_write(p, value)`.",
		},
	},
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
	items := make([]completionItem, 0, len(unsafeCapabilityDocs))
	for _, doc := range unsafeCapabilityDocs {
		items = append(items, unsafeCapability(doc))
	}
	return items
}

// unsafeCapability builds one @unsafe capability completion item.
func unsafeCapability(doc unsafeCapabilityDoc) completionItem {
	return completionItem{
		Label:         doc.label,
		Kind:          completionItemKindKeyword,
		Detail:        doc.detail,
		Documentation: markdownDocumentation(unsafeCapabilityMarkdown(doc)),
	}
}

// unsafeCapabilityDocByName returns documentation for one reserved capability.
func unsafeCapabilityDocByName(name string) (unsafeCapabilityDoc, bool) {
	for _, doc := range unsafeCapabilityDocs {
		if doc.label == name {
			return doc, true
		}
	}
	return unsafeCapabilityDoc{}, false
}

// unsafeCapabilityMarkdown renders user-facing capability documentation.
func unsafeCapabilityMarkdown(doc unsafeCapabilityDoc) string {
	var builder strings.Builder
	builder.WriteString(doc.detail)
	builder.WriteString("\n\nPermits:\n")
	for _, permit := range doc.permits {
		builder.WriteString("- ")
		builder.WriteString(permit)
		builder.WriteByte('\n')
	}
	builder.WriteString("\n`@unsafe(")
	builder.WriteString(doc.label)
	builder.WriteString(")` does not disable type, move, or borrow checks.")
	return strings.TrimSpace(builder.String())
}
