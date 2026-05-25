package lsp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/token"
)

type completionContextKind int

const (
	completionContextGeneral completionContextKind = iota
	completionContextNamespace
	completionContextMember
	completionContextImport
)

type completionContext struct {
	kind         completionContextKind
	receiver     string
	replaceStart Position
	replaceEnd   Position
}

type completionIndex struct {
	functions map[string]functionCompletion
	types     map[string]int
	structs   map[string][]fieldCompletion
	enums     map[string][]string
	unions    map[string][]unionVariantCompletion
	methods   map[string][]functionCompletion
	modules   map[string]bool
	imports   map[string]bool
}

type functionCompletion struct {
	name   string
	params []string
}

type fieldCompletion struct {
	name string
	typ  string
}

type unionVariantCompletion struct {
	name    string
	payload bool
}

type localBinding struct {
	name string
	typ  string
}

// Complete returns Kizu completion items for one in-memory source document.
func Complete(source string, position Position) []completionItem {
	return completionItems(source, position, []string{source}, nil)
}

// completions returns completion items for a tracked LSP document.
func (s *Server) completions(uri string, position Position) []completionItem {
	source, ok := s.documents[uri]
	if !ok {
		return staticCompletionItems()
	}
	sources, modules := s.completionSources(uri, source)
	return completionItems(source, position, sources, modules)
}

// completionSources returns source texts that should inform completions.
func (s *Server) completionSources(uri string, source string) ([]string, []string) {
	path, ok := filePathFromURI(uri)
	if !ok {
		return []string{source}, nil
	}
	root, found, err := findPackageRoot(path)
	if err != nil || !found {
		return []string{source}, nil
	}
	graph, err := loadPackageGraph(root)
	if err != nil || !graphContainsFile(graph, path) {
		return []string{source}, nil
	}
	return s.completionSourcesFromGraph(graph, source), graphModulePaths(graph)
}

// completionSourcesFromGraph reads package module sources with open-buffer overrides.
func (s *Server) completionSourcesFromGraph(graph project.Graph, fallback string) []string {
	overrides := s.packageSourceOverrides(graph)
	sources := []string{}
	for _, module := range graph.Modules {
		cleanFile := filepath.Clean(module.File)
		if source, ok := overrides[cleanFile]; ok {
			sources = append(sources, source)
			continue
		}
		data, err := os.ReadFile(module.File)
		if err == nil {
			sources = append(sources, string(data))
		}
	}
	if len(sources) == 0 {
		return []string{fallback}
	}
	return sources
}

// completionItems builds context-sensitive items from current and package sources.
func completionItems(
	currentSource string,
	position Position,
	sources []string,
	modules []string,
) []completionItem {
	index := newCompletionIndex()
	for _, source := range sources {
		index.scan(source)
	}
	for _, module := range modules {
		index.modules[module] = true
	}

	context := completionContextAt(currentSource, position)
	builder := newCompletionBuilder()
	switch context.kind {
	case completionContextNamespace:
		index.addNamespaceItems(builder, context)
	case completionContextMember:
		index.addMemberItems(builder, context, inferLocalBindings(currentSource, position))
	case completionContextImport:
		index.addImportItems(builder, context)
	default:
		addStaticItems(builder)
		index.addGeneralItems(builder)
		addLocalItems(builder, inferLocalBindings(currentSource, position))
	}
	return builder.sortedItems()
}

// staticCompletionItems returns context-free Kizu keywords, types, and snippets.
func staticCompletionItems() []completionItem {
	builder := newCompletionBuilder()
	addStaticItems(builder)
	return builder.sortedItems()
}

// addStaticItems adds context-free Kizu keywords, types, and snippets.
func addStaticItems(builder *completionBuilder) {
	for _, item := range snippetCompletionItems {
		builder.add(item)
	}
	for _, item := range keywordCompletionItems {
		builder.add(item)
	}
	for _, item := range primitiveTypeCompletionItems {
		builder.add(item)
	}
}

// addLocalItems adds locals visible before the completion position.
func addLocalItems(builder *completionBuilder, bindings []localBinding) {
	for _, binding := range bindings {
		builder.add(completionItem{
			Label:  binding.name,
			Kind:   completionItemKindVariable,
			Detail: binding.typ,
		})
	}
}

// addGeneralItems adds visible declarations and modules.
func (idx completionIndex) addGeneralItems(builder *completionBuilder) {
	for name, fn := range idx.functions {
		builder.add(functionItem(name, fn, completionItemKindFunction, "function"))
	}
	for name, kind := range idx.types {
		builder.add(completionItem{Label: name, Kind: kind, Detail: "type"})
	}
	for module := range idx.modules {
		builder.add(completionItem{Label: module, Kind: completionItemKindModule, Detail: "module"})
	}
	for alias := range idx.imports {
		builder.add(completionItem{
			Label:  alias,
			Kind:   completionItemKindModule,
			Detail: "imported module",
		})
	}
}

// addNamespaceItems adds items after namespace access such as Color::.
func (idx completionIndex) addNamespaceItems(
	builder *completionBuilder,
	context completionContext,
) {
	if tags := idx.enums[context.receiver]; len(tags) > 0 {
		for _, tag := range tags {
			builder.add(contextualItem(context, completionItem{
				Label:  tag,
				Kind:   completionItemKindEnumMember,
				Detail: context.receiver,
			}))
		}
	}
	if variants := idx.unions[context.receiver]; len(variants) > 0 {
		for _, variant := range variants {
			item := completionItem{
				Label:  variant.name,
				Kind:   completionItemKindEnumMember,
				Detail: context.receiver,
			}
			if variant.payload {
				item.InsertText = variant.name + "($1)"
				item.InsertTextFormat = insertTextFormatSnippet
			}
			builder.add(contextualItem(context, item))
		}
	}
	for module := range idx.modules {
		if strings.HasPrefix(module, context.receiver+"::") {
			builder.add(contextualItem(context, completionItem{
				Label:      module,
				Kind:       completionItemKindModule,
				Detail:     "module",
				InsertText: strings.TrimPrefix(module, context.receiver+"::"),
			}))
		}
	}
}

// addMemberItems adds fields and methods after receiver access.
func (idx completionIndex) addMemberItems(
	builder *completionBuilder,
	context completionContext,
	bindings []localBinding,
) {
	typ := localBindingType(bindings, context.receiver)
	if typ == "" {
		return
	}
	typ = normalizeCompletionType(typ)
	for _, field := range idx.structs[typ] {
		builder.add(contextualItem(context, completionItem{
			Label:  field.name,
			Kind:   completionItemKindField,
			Detail: field.typ,
		}))
	}
	for _, method := range idx.methods[typ] {
		item := functionItem(method.name, method, completionItemKindMethod, typ)
		builder.add(contextualItem(context, item))
	}
}

// addImportItems adds package module paths inside import declarations.
func (idx completionIndex) addImportItems(builder *completionBuilder, context completionContext) {
	for module := range idx.modules {
		builder.add(contextualItem(context, completionItem{
			Label:  module,
			Kind:   completionItemKindModule,
			Detail: "module",
		}))
	}
}

// contextualItem attaches a text edit that replaces the currently typed selector suffix.
func contextualItem(context completionContext, item completionItem) completionItem {
	newText := item.InsertText
	if newText == "" {
		newText = item.Label
	}
	item.TextEdit = &completionTextEdit{
		Range: Range{
			Start: context.replaceStart,
			End:   context.replaceEnd,
		},
		NewText: newText,
	}
	return item
}

type completionBuilder struct {
	seen  map[string]bool
	items []completionItem
}

// newCompletionBuilder creates a duplicate-filtering item builder.
func newCompletionBuilder() *completionBuilder {
	return &completionBuilder{seen: map[string]bool{}}
}

// add appends one item unless an equivalent label/detail pair is already present.
func (b *completionBuilder) add(item completionItem) {
	key := item.Label + "\x00" + item.Detail
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.items = append(b.items, item)
}

// sortedItems returns completion items in stable label order.
func (b *completionBuilder) sortedItems() []completionItem {
	sort.SliceStable(b.items, func(i, j int) bool {
		if b.items[i].Label == b.items[j].Label {
			return b.items[i].Detail < b.items[j].Detail
		}
		return b.items[i].Label < b.items[j].Label
	})
	return b.items
}

// newCompletionIndex creates an empty declaration index.
func newCompletionIndex() completionIndex {
	return completionIndex{
		functions: map[string]functionCompletion{},
		types:     map[string]int{},
		structs:   map[string][]fieldCompletion{},
		enums:     map[string][]string{},
		unions:    map[string][]unionVariantCompletion{},
		methods:   map[string][]functionCompletion{},
		modules:   map[string]bool{},
		imports:   map[string]bool{},
	}
}

// scan collects declarations from one source token stream.
func (idx completionIndex) scan(source string) {
	tokens := lexCompletionTokens(source)
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Import:
			path, next := readImportPath(tokens, i+1)
			if len(path) > 0 {
				idx.modules[strings.Join(path, "::")] = true
				idx.imports[path[len(path)-1]] = true
			}
			i = next
		case token.Function:
			fn, next, ok := readFunction(tokens, i)
			if ok {
				idx.functions[fn.name] = fn
			}
			i = skipDeclarationBody(tokens, next)
		case token.Struct:
			next := idx.scanStruct(tokens, i)
			i = next
		case token.Enum:
			next := idx.scanEnum(tokens, i)
			i = next
		case token.Union:
			next := idx.scanUnion(tokens, i)
			i = next
		case token.Contract:
			if name, ok := nextIdentifier(tokens, i+1); ok {
				idx.types[name] = completionItemKindStruct
			}
			i = skipDeclarationBody(tokens, i+1)
		case token.Impl:
			next := idx.scanImpl(tokens, i)
			i = next
		}
	}
}

// scanStruct collects a struct type and its field names.
func (idx completionIndex) scanStruct(tokens []token.Token, start int) int {
	name, ok := nextIdentifier(tokens, start+1)
	if !ok {
		return start
	}
	idx.types[name] = completionItemKindStruct
	brace := findNextToken(tokens, start+1, token.LBrace)
	if brace < 0 {
		return start
	}
	fields := []fieldCompletion{}
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			idx.structs[name] = fields
			return i
		}
		fieldIndex := i
		if tokens[i].Type == token.Public {
			fieldIndex = i + 1
		}
		if fieldIndex+1 >= len(tokens) ||
			tokens[fieldIndex].Type != token.Ident ||
			tokens[fieldIndex+1].Type != token.Colon {
			continue
		}
		fieldName := tokens[fieldIndex].Literal
		typ, next := readTypeUntil(tokens, fieldIndex+2, token.Comma, token.RBrace)
		fields = append(fields, fieldCompletion{name: fieldName, typ: typ})
		i = next
		if i < len(tokens) && tokens[i].Type == token.RBrace {
			idx.structs[name] = fields
			return i
		}
	}
	idx.structs[name] = fields
	return brace
}

// scanEnum collects an enum type and its tag names.
func (idx completionIndex) scanEnum(tokens []token.Token, start int) int {
	name, ok := nextIdentifier(tokens, start+1)
	if !ok {
		return start
	}
	idx.types[name] = completionItemKindEnum
	brace := findNextToken(tokens, start+1, token.LBrace)
	if brace < 0 {
		return start
	}
	tags := []string{}
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			idx.enums[name] = tags
			return i
		}
		if tokens[i].Type == token.Ident {
			tags = append(tags, tokens[i].Literal)
		}
	}
	idx.enums[name] = tags
	return brace
}

// scanUnion collects a union type and its variant names.
func (idx completionIndex) scanUnion(tokens []token.Token, start int) int {
	name, ok := nextIdentifier(tokens, start+1)
	if !ok {
		return start
	}
	idx.types[name] = completionItemKindEnum
	brace := findNextToken(tokens, start+1, token.LBrace)
	if brace < 0 {
		return start
	}
	variants := []unionVariantCompletion{}
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			idx.unions[name] = variants
			return i
		}
		if tokens[i].Type != token.Ident {
			continue
		}
		variant := unionVariantCompletion{name: tokens[i].Literal}
		if i+1 < len(tokens) && tokens[i+1].Type == token.LParen {
			variant.payload = true
			i = skipBalanced(tokens, i+1, token.LParen, token.RParen)
		}
		variants = append(variants, variant)
	}
	idx.unions[name] = variants
	return brace
}

// scanImpl collects methods from an inherent or contract impl block.
func (idx completionIndex) scanImpl(tokens []token.Token, start int) int {
	typeName := ""
	brace := -1
	for i := start + 1; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.For:
			typeName, brace = readTypeBeforeBrace(tokens, i+1)
			i = brace
		case token.LBrace:
			typeName = tokenText(tokens[start+1 : i])
			brace = i
		}
		if brace >= 0 {
			break
		}
	}
	if brace < 0 {
		return start
	}
	typeName = normalizeCompletionType(typeName)
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			return i
		}
		if tokens[i].Type != token.Function {
			continue
		}
		fn, next, ok := readFunction(tokens, i)
		if ok {
			idx.methods[typeName] = append(idx.methods[typeName], fn)
		}
		i = skipDeclarationBody(tokens, next)
	}
	return brace
}

// inferLocalBindings collects simple locals and params visible before position.
func inferLocalBindings(source string, position Position) []localBinding {
	prefix := sourcePrefixAtPosition(source, position)
	tokens := lexCompletionTokens(prefix)
	bindings := []localBinding{}
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Function:
			params, next := readFunctionParams(tokens, i)
			bindings = append(bindings, params...)
			i = next
		case token.Let, token.Var:
			binding, next, ok := readLetBinding(tokens, i)
			if ok {
				bindings = append(bindings, binding)
			}
			i = next
		case token.For:
			if i+5 < len(tokens) && tokens[i+3].Type == token.Pipe && tokens[i+4].Type == token.Ident {
				bindings = append(bindings, localBinding{name: tokens[i+4].Literal, typ: "i64"})
			}
		}
	}
	return bindings
}

// readLetBinding reads one let or var binding and a simple inferred type.
func readLetBinding(tokens []token.Token, start int) (localBinding, int, bool) {
	if start+2 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return localBinding{}, start, false
	}
	name := tokens[start+1].Literal
	assign := findNextToken(tokens, start+2, token.Assign)
	if assign < 0 {
		return localBinding{name: name}, start + 1, true
	}
	typ, next := inferExprType(tokens, assign+1)
	return localBinding{name: name, typ: typ}, next, true
}

// inferExprType infers a small set of initializer types for completion.
func inferExprType(tokens []token.Token, start int) (string, int) {
	if start >= len(tokens) {
		return "", start
	}
	switch tokens[start].Type {
	case token.Int:
		return "i64", start
	case token.String:
		return "[]u8", start
	case token.True, token.False:
		return "bool", start
	case token.Ident:
		parts := []string{tokens[start].Literal}
		i := start
		for i+2 < len(tokens) &&
			tokens[i+1].Type == token.DoubleColon &&
			tokens[i+2].Type == token.Ident {
			parts = append(parts, tokens[i+2].Literal)
			i += 2
		}
		if i+1 < len(tokens) && tokens[i+1].Type == token.LBrace {
			return strings.Join(parts, "::"), i + 1
		}
		if len(parts) >= 2 {
			return strings.Join(parts[:len(parts)-1], "::"), i
		}
	}
	return "", start
}

// readFunction reads a function name and user-facing call parameters.
func readFunction(tokens []token.Token, start int) (functionCompletion, int, bool) {
	if start+1 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return functionCompletion{}, start, false
	}
	name := tokens[start+1].Literal
	params, next := readFunctionParams(tokens, start)
	paramNames := make([]string, 0, len(params))
	for _, param := range params {
		if param.name == "self" {
			continue
		}
		paramNames = append(paramNames, param.name)
	}
	return functionCompletion{name: name, params: paramNames}, next, true
}

// readFunctionParams reads a function parameter list.
func readFunctionParams(tokens []token.Token, start int) ([]localBinding, int) {
	open := findNextToken(tokens, start, token.LParen)
	if open < 0 {
		return nil, start
	}
	close := skipBalanced(tokens, open, token.LParen, token.RParen)
	if close <= open {
		return nil, open
	}
	params := []localBinding{}
	for i := open + 1; i < close; i++ {
		if tokens[i].Type == token.Comptime {
			i++
		}
		if i+1 >= close || tokens[i].Type != token.Ident || tokens[i+1].Type != token.Colon {
			continue
		}
		typ, next := readTypeUntil(tokens, i+2, token.Comma, token.RParen)
		params = append(params, localBinding{name: tokens[i].Literal, typ: typ})
		i = next
	}
	return params, close
}

// completionContextAt identifies whether completion is general, import, member, or namespace.
func completionContextAt(source string, position Position) completionContext {
	offset := offsetFromPosition(source, position)
	before := source[:offset]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	linePrefix := before[lineStart:]
	if start, ok := importPathStart(linePrefix); ok {
		replaceStart := positionFromOffset(source, lineStart+start)
		return completionContext{
			kind:         completionContextImport,
			replaceStart: replaceStart,
			replaceEnd:   position,
		}
	}
	if receiver, start := selectorContext(before, "::"); receiver != "" {
		return completionContext{
			kind:         completionContextNamespace,
			receiver:     receiver,
			replaceStart: positionFromOffset(source, start),
			replaceEnd:   position,
		}
	}
	if receiver, start := selectorContext(before, "."); receiver != "" {
		return completionContext{
			kind:         completionContextMember,
			receiver:     receiver,
			replaceStart: positionFromOffset(source, start),
			replaceEnd:   position,
		}
	}
	return completionContext{kind: completionContextGeneral}
}

// importPathStart reports where an import path starts on the current line.
func importPathStart(linePrefix string) (int, bool) {
	trimmed := strings.TrimLeft(linePrefix, " \t")
	padding := len(linePrefix) - len(trimmed)
	if !strings.HasPrefix(trimmed, "import ") {
		return 0, false
	}
	if strings.Contains(trimmed, ";") {
		return 0, false
	}
	return padding + len("import "), true
}

// selectorContext returns the receiver and replacement start for a selector.
func selectorContext(before string, separator string) (string, int) {
	i := len(before)
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(before[:i])
		if !isIdentRune(r) {
			break
		}
		i -= size
	}
	if i < len(separator) || before[i-len(separator):i] != separator {
		return "", 0
	}
	receiverEnd := i - len(separator)
	receiverStart := receiverEnd
	for receiverStart > 0 {
		r, size := utf8.DecodeLastRuneInString(before[:receiverStart])
		if !isSelectorReceiverRune(r, separator) {
			break
		}
		receiverStart -= size
	}
	receiver := before[receiverStart:receiverEnd]
	if receiver == "" || receiver == ":" || strings.HasSuffix(receiver, "::") {
		return "", 0
	}
	return receiver, i
}

// isSelectorReceiverRune reports whether a rune may appear in a selector receiver.
func isSelectorReceiverRune(r rune, separator string) bool {
	if isIdentRune(r) {
		return true
	}
	return separator == "::" && r == ':'
}

// isIdentRune reports whether a rune is part of a Kizu identifier.
func isIdentRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// localBindingType returns the most recent binding type for a name.
func localBindingType(bindings []localBinding, name string) string {
	for i := len(bindings) - 1; i >= 0; i-- {
		if bindings[i].name == name {
			return bindings[i].typ
		}
	}
	return ""
}

// functionItem builds a call completion item for a function or method.
func functionItem(name string, fn functionCompletion, kind int, detail string) completionItem {
	item := completionItem{Label: name, Kind: kind, Detail: detail}
	item.InsertText = callSnippet(name, fn.params)
	item.InsertTextFormat = insertTextFormatSnippet
	return item
}

// callSnippet builds a function call snippet with named placeholders.
func callSnippet(name string, params []string) string {
	if len(params) == 0 {
		return name + "()"
	}
	parts := make([]string, 0, len(params))
	for idx, param := range params {
		parts = append(parts, "${"+itoa(idx+1)+":"+param+"}")
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

// lexCompletionTokens lexes source into tokens for tolerant completion indexing.
func lexCompletionTokens(source string) []token.Token {
	l := lexer.New(source)
	tokens := []token.Token{}
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			return tokens
		}
	}
}

// nextIdentifier returns the next identifier before a declaration boundary.
func nextIdentifier(tokens []token.Token, start int) (string, bool) {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Type == token.Ident {
			return tokens[i].Literal, true
		}
		if tokens[i].Type == token.LBrace ||
			tokens[i].Type == token.Semicolon ||
			tokens[i].Type == token.EOF {
			return "", false
		}
	}
	return "", false
}

// readImportPath reads an import path as module segments.
func readImportPath(tokens []token.Token, start int) ([]string, int) {
	parts := []string{}
	for i := start; i < len(tokens); i++ {
		if tokens[i].Type != token.Ident {
			return parts, i
		}
		parts = append(parts, tokens[i].Literal)
		if i+1 >= len(tokens) || tokens[i+1].Type != token.DoubleColon {
			return parts, i
		}
		i++
	}
	return parts, start
}

// readTypeBeforeBrace reads a type spelling before the next block brace.
func readTypeBeforeBrace(tokens []token.Token, start int) (string, int) {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Type == token.LBrace {
			return tokenText(tokens[start:i]), i
		}
	}
	return "", -1
}

// readTypeUntil reads a type spelling until one of the stop tokens.
func readTypeUntil(tokens []token.Token, start int, stops ...token.Type) (string, int) {
	stopSet := map[token.Type]bool{}
	for _, stop := range stops {
		stopSet[stop] = true
	}
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.LT, token.LParen, token.LBracket:
			depth++
		case token.GT, token.RParen, token.RBracket:
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && stopSet[tokens[i].Type] {
			return tokenText(tokens[start:i]), i
		}
	}
	return tokenText(tokens[start:]), len(tokens) - 1
}

// tokenText reconstructs compact source text for a token slice.
func tokenText(tokens []token.Token) string {
	var out strings.Builder
	previousWord := false
	for _, tok := range tokens {
		if tok.Type == token.EOF {
			break
		}
		currentWord := tok.Type == token.Ident || tok.Type == token.Var ||
			tok.Type == token.Dyn || tok.Type == token.Comptime
		if previousWord && currentWord {
			out.WriteByte(' ')
		}
		out.WriteString(tok.Literal)
		previousWord = currentWord
	}
	return out.String()
}

// findNextToken returns the next token index with the requested type.
func findNextToken(tokens []token.Token, start int, typ token.Type) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Type == typ {
			return i
		}
		if tokens[i].Type == token.EOF {
			return -1
		}
	}
	return -1
}

// skipDeclarationBody skips a declaration body or signature terminator.
func skipDeclarationBody(tokens []token.Token, start int) int {
	for i := start; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Semicolon:
			return i
		case token.LBrace:
			return skipBalanced(tokens, i, token.LBrace, token.RBrace)
		case token.EOF:
			return i
		}
	}
	return start
}

// skipBalanced skips a balanced token pair starting at open.
func skipBalanced(tokens []token.Token, start int, open token.Type, close token.Type) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].Type {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		case token.EOF:
			return i
		}
	}
	return start
}

// normalizeCompletionType strips simple wrappers from a type spelling.
func normalizeCompletionType(typ string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "&var ")
	typ = strings.TrimPrefix(typ, "&")
	typ = strings.TrimPrefix(typ, "?")
	typ = strings.TrimPrefix(typ, "!")
	return typ
}

// graphModulePaths returns module path strings from a graph.
func graphModulePaths(graph project.Graph) []string {
	paths := make([]string, 0, len(graph.Modules))
	for _, module := range graph.Modules {
		paths = append(paths, module.Path)
	}
	return paths
}

// sourcePrefixAtPosition returns the source prefix before an LSP position.
func sourcePrefixAtPosition(source string, position Position) string {
	return source[:offsetFromPosition(source, position)]
}

// offsetFromPosition converts an LSP position to a byte offset.
func offsetFromPosition(source string, position Position) int {
	line := 0
	character := 0
	for offset, r := range source {
		if line == position.Line && character >= position.Character {
			return offset
		}
		if r == '\n' {
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
	return len(source)
}

// positionFromOffset converts a byte offset to an LSP position.
func positionFromOffset(source string, target int) Position {
	line := 0
	character := 0
	for offset, r := range source {
		if offset >= target {
			return Position{Line: line, Character: character}
		}
		if r == '\n' {
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
	return Position{Line: line, Character: character}
}

// itoa formats a positive integer without pulling in strconv for one small loop.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
