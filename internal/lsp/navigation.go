package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kizu-lang/kizu/internal/token"
)

type navigationSource struct {
	uri    string
	path   string
	module string
	source string
}

type navigationDeclaration struct {
	name   string
	detail string
	uri    string
	rng    Range
	kind   int
}

type navigationIndex struct {
	functions     map[string]navigationDeclaration
	types         map[string]navigationDeclaration
	modules       map[string]navigationDeclaration
	enumVariants  map[string]map[string]navigationDeclaration
	unionVariants map[string]map[string]navigationDeclaration
	fields        map[string]map[string]navigationDeclaration
	methods       map[string]map[string]navigationDeclaration
}

// DocumentSymbols returns top-level outline symbols for one source file.
func DocumentSymbols(source string) []documentSymbol {
	return documentSymbolsFromTokens(lexCompletionTokens(source))
}

// definition returns the best known definition location for a document position.
func (s *Server) definition(uri string, position Position) []location {
	source, ok := s.documents[uri]
	if !ok {
		return []location{}
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	if decl, ok := definitionAt(tokens, position, uri, source, index, sources); ok {
		return []location{{URI: decl.uri, Range: decl.rng}}
	}
	return []location{}
}

// hover returns concise hover text for a document position.
func (s *Server) hover(uri string, position Position) *hover {
	source, ok := s.documents[uri]
	if !ok {
		return nil
	}
	tokens := lexCompletionTokens(source)
	index, sources := s.navigationIndex(uri, source)
	decl, ok := hoverAt(tokens, position, uri, source, index, sources)
	if !ok || decl.detail == "" {
		return nil
	}
	return &hover{
		Contents: markupContent{Kind: "markdown", Value: kizuHoverMarkup(decl.detail)},
	}
}

// navigationIndex builds package-aware declarations for navigation requests.
func (s *Server) navigationIndex(uri string, source string) (navigationIndex, []navigationSource) {
	sources := s.navigationSources(uri, source)
	index := newNavigationIndex()
	for _, src := range sources {
		index.addModule(src)
		index.scan(src)
	}
	return index, sources
}

// newNavigationIndex creates empty navigation lookup tables.
func newNavigationIndex() navigationIndex {
	return navigationIndex{
		functions:     map[string]navigationDeclaration{},
		types:         map[string]navigationDeclaration{},
		modules:       map[string]navigationDeclaration{},
		enumVariants:  map[string]map[string]navigationDeclaration{},
		unionVariants: map[string]map[string]navigationDeclaration{},
		fields:        map[string]map[string]navigationDeclaration{},
		methods:       map[string]map[string]navigationDeclaration{},
	}
}

// navigationSources returns current package files with open-buffer overrides.
func (s *Server) navigationSources(uri string, source string) []navigationSource {
	path, ok := filePathFromURI(uri)
	if !ok {
		return []navigationSource{{uri: uri, source: source}}
	}
	root, found, err := findPackageRoot(path)
	if err != nil || !found {
		return []navigationSource{{uri: uri, path: path, source: source}}
	}
	graph, err := loadPackageGraph(root)
	if err != nil || !graphContainsFile(graph, path) {
		return []navigationSource{{uri: uri, path: path, source: source}}
	}
	overrides := s.packageSourceOverrides(graph)
	sources := make([]navigationSource, 0, len(graph.Modules))
	for _, module := range graph.Modules {
		cleanPath := filepath.Clean(module.File)
		text, ok := overrides[cleanPath]
		if !ok {
			data, err := os.ReadFile(module.File)
			if err != nil {
				continue
			}
			text = string(data)
		}
		sources = append(sources, navigationSource{
			uri:    fileURIFromPath(cleanPath),
			path:   cleanPath,
			module: module.Path,
			source: text,
		})
	}
	if len(sources) == 0 {
		return []navigationSource{{uri: uri, path: path, source: source}}
	}
	return sources
}

// addModule registers a package module as a navigable target.
func (idx navigationIndex) addModule(src navigationSource) {
	if src.module == "" {
		return
	}
	idx.modules[src.module] = navigationDeclaration{
		name:   src.module,
		detail: "module " + src.module,
		uri:    src.uri,
		rng:    firstTokenRange(src.source),
		kind:   symbolKindModule,
	}
}

// scan collects navigable declarations from one source.
func (idx navigationIndex) scan(src navigationSource) {
	tokens := lexCompletionTokens(src.source)
	for i := 0; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.Import:
			i = idx.scanImport(tokens, i)
		case token.Function:
			i = idx.scanFunction(src, tokens, i)
		case token.Struct:
			i = idx.scanStruct(src, tokens, i)
		case token.Enum:
			i = idx.scanEnum(src, tokens, i)
		case token.Union:
			i = idx.scanUnion(src, tokens, i)
		case token.Contract:
			i = idx.scanContract(src, tokens, i)
		case token.Impl:
			i = idx.scanImpl(src, tokens, i)
		}
	}
}

// scanImport records imported module aliases.
func (idx navigationIndex) scanImport(tokens []token.Token, start int) int {
	path, next := readImportPath(tokens, start+1)
	if len(path) == 0 {
		return next
	}
	module := strings.Join(path, "::")
	if decl, ok := idx.modules[module]; ok {
		alias := path[len(path)-1]
		idx.modules[alias] = decl
	}
	return next
}

// scanFunction records a top-level function declaration.
func (idx navigationIndex) scanFunction(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	if start+1 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return start
	}
	name := tokens[start+1].Literal
	headerEnd := declarationHeaderEnd(tokens, start)
	idx.functions[name] = navigationDeclaration{
		name:   name,
		detail: tokenText(tokens[start:headerEnd]),
		uri:    src.uri,
		rng:    tokenRange(tokens[start+1]),
		kind:   symbolKindFunction,
	}
	return skipDeclarationBody(tokens, headerEnd)
}

// scanStruct records a struct declaration and its fields.
func (idx navigationIndex) scanStruct(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return start
	}
	name := tokens[nameIndex].Literal
	idx.types[name] = navigationDeclaration{
		name:   name,
		detail: "struct " + name,
		uri:    src.uri,
		rng:    tokenRange(tokens[nameIndex]),
		kind:   symbolKindStruct,
	}
	brace := findNextToken(tokens, nameIndex+1, token.LBrace)
	if brace < 0 {
		return start
	}
	fields, end := scanFieldDeclarations(src, tokens, brace, name)
	idx.fields[name] = fields
	return end
}

// scanEnum records an enum declaration and its variants.
func (idx navigationIndex) scanEnum(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return start
	}
	name := tokens[nameIndex].Literal
	idx.types[name] = navigationDeclaration{
		name:   name,
		detail: "enum " + name,
		uri:    src.uri,
		rng:    tokenRange(tokens[nameIndex]),
		kind:   symbolKindEnum,
	}
	variants, end := scanVariantDeclarations(src, tokens, nameIndex+1, name, "enum")
	idx.enumVariants[name] = variants
	return end
}

// scanUnion records a union declaration and its variants.
func (idx navigationIndex) scanUnion(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return start
	}
	name := tokens[nameIndex].Literal
	idx.types[name] = navigationDeclaration{
		name:   name,
		detail: "union " + name,
		uri:    src.uri,
		rng:    tokenRange(tokens[nameIndex]),
		kind:   symbolKindEnum,
	}
	variants, end := scanVariantDeclarations(src, tokens, nameIndex+1, name, "union")
	idx.unionVariants[name] = variants
	return end
}

// scanContract records a contract declaration.
func (idx navigationIndex) scanContract(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	nameIndex := nextIdentifierIndex(tokens, start+1)
	if nameIndex < 0 {
		return start
	}
	name := tokens[nameIndex].Literal
	idx.types[name] = navigationDeclaration{
		name:   name,
		detail: "contract " + name,
		uri:    src.uri,
		rng:    tokenRange(tokens[nameIndex]),
		kind:   symbolKindInterface,
	}
	return skipDeclarationBody(tokens, start+1)
}

// scanImpl records methods inside an impl block.
func (idx navigationIndex) scanImpl(
	src navigationSource,
	tokens []token.Token,
	start int,
) int {
	typeName, brace := implTarget(tokens, start)
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
		method, next, ok := readMethodDeclaration(src, tokens, i)
		if ok {
			addNestedDeclaration(idx.methods, typeName, method)
		}
		i = skipDeclarationBody(tokens, next)
	}
	return brace
}

// readMethodDeclaration reads one method header inside an impl block.
func readMethodDeclaration(
	src navigationSource,
	tokens []token.Token,
	start int,
) (navigationDeclaration, int, bool) {
	if start+1 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return navigationDeclaration{}, start, false
	}
	headerEnd := declarationHeaderEnd(tokens, start)
	name := tokens[start+1].Literal
	return navigationDeclaration{
		name:   name,
		detail: tokenText(tokens[start:headerEnd]),
		uri:    src.uri,
		rng:    tokenRange(tokens[start+1]),
		kind:   symbolKindMethod,
	}, headerEnd, true
}

// scanFieldDeclarations records fields inside a struct body.
func scanFieldDeclarations(
	src navigationSource,
	tokens []token.Token,
	brace int,
	typeName string,
) (map[string]navigationDeclaration, int) {
	fields := map[string]navigationDeclaration{}
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			return fields, i
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
		typ, next := readTypeUntil(tokens, fieldIndex+2, token.Comma, token.RBrace)
		name := tokens[fieldIndex].Literal
		fields[name] = navigationDeclaration{
			name:   name,
			detail: typeName + "." + name + ": " + typ,
			uri:    src.uri,
			rng:    tokenRange(tokens[fieldIndex]),
			kind:   symbolKindField,
		}
		i = next
	}
	return fields, brace
}

// scanVariantDeclarations records enum or union variants inside a body.
func scanVariantDeclarations(
	src navigationSource,
	tokens []token.Token,
	start int,
	typeName string,
	prefix string,
) (map[string]navigationDeclaration, int) {
	brace := findNextToken(tokens, start, token.LBrace)
	if brace < 0 {
		return map[string]navigationDeclaration{}, start
	}
	variants := map[string]navigationDeclaration{}
	for i := brace + 1; i < len(tokens) && tokens[i].Type != token.EOF; i++ {
		if tokens[i].Type == token.RBrace {
			return variants, i
		}
		if tokens[i].Type != token.Ident {
			continue
		}
		name := tokens[i].Literal
		variants[name] = navigationDeclaration{
			name:   name,
			detail: prefix + " " + typeName + "::" + name,
			uri:    src.uri,
			rng:    tokenRange(tokens[i]),
			kind:   symbolKindEnumMember,
		}
		if i+1 < len(tokens) && tokens[i+1].Type == token.LParen {
			i = skipBalanced(tokens, i+1, token.LParen, token.RParen)
		}
	}
	return variants, brace
}

// definitionAt resolves the identifier under a position to a declaration.
func definitionAt(
	tokens []token.Token,
	position Position,
	uri string,
	source string,
	index navigationIndex,
	sources []navigationSource,
) (navigationDeclaration, bool) {
	tokenIndex := tokenIndexAtPosition(tokens, position)
	if tokenIndex < 0 || tokens[tokenIndex].Type != token.Ident {
		return navigationDeclaration{}, false
	}
	if decl, ok := moduleDefinitionAt(tokens, tokenIndex, index); ok {
		return decl, true
	}
	if decl, ok := memberDefinitionAt(tokens, tokenIndex, source, position, index); ok {
		return decl, true
	}
	if decl, ok := namespaceDefinitionAt(tokens, tokenIndex, index); ok {
		return decl, true
	}
	if decl, ok := localDefinitionAt(source, uri, position, tokens[tokenIndex].Literal); ok {
		return decl, true
	}
	return globalDefinition(tokens[tokenIndex].Literal, index, sources)
}

// hoverAt resolves hover text for the identifier under a position.
func hoverAt(
	tokens []token.Token,
	position Position,
	uri string,
	source string,
	index navigationIndex,
	sources []navigationSource,
) (navigationDeclaration, bool) {
	tokenIndex := tokenIndexAtPosition(tokens, position)
	if tokenIndex < 0 || tokens[tokenIndex].Type != token.Ident {
		return navigationDeclaration{}, false
	}
	if decl, ok := moduleDefinitionAt(tokens, tokenIndex, index); ok {
		return decl, true
	}
	if decl, ok := memberDefinitionAt(tokens, tokenIndex, source, position, index); ok {
		return decl, true
	}
	if decl, ok := namespaceDefinitionAt(tokens, tokenIndex, index); ok {
		return decl, true
	}
	if decl, ok := localDefinitionAt(source, uri, position, tokens[tokenIndex].Literal); ok {
		return decl, true
	}
	return globalDefinition(tokens[tokenIndex].Literal, index, sources)
}

// moduleDefinitionAt resolves imports and namespace aliases to modules.
func moduleDefinitionAt(
	tokens []token.Token,
	tokenIndex int,
	index navigationIndex,
) (navigationDeclaration, bool) {
	if module, ok := importPathAt(tokens, tokenIndex); ok {
		decl, found := index.modules[module]
		return decl, found
	}
	name := tokens[tokenIndex].Literal
	if tokenIndex+1 < len(tokens) && tokens[tokenIndex+1].Type == token.DoubleColon {
		decl, found := index.modules[name]
		return decl, found
	}
	return navigationDeclaration{}, false
}

// memberDefinitionAt resolves field and method accesses.
func memberDefinitionAt(
	tokens []token.Token,
	tokenIndex int,
	source string,
	position Position,
	index navigationIndex,
) (navigationDeclaration, bool) {
	if tokenIndex < 2 || tokens[tokenIndex-1].Type != token.Dot {
		return navigationDeclaration{}, false
	}
	receiver := tokens[tokenIndex-2]
	if receiver.Type != token.Ident {
		return navigationDeclaration{}, false
	}
	typ := localTypeAt(source, position, receiver.Literal)
	if typ == "" {
		return navigationDeclaration{}, false
	}
	typ = normalizeCompletionType(typ)
	name := tokens[tokenIndex].Literal
	if decl, ok := index.fields[typ][name]; ok {
		return decl, true
	}
	if decl, ok := index.methods[typ][name]; ok {
		return decl, true
	}
	return navigationDeclaration{}, false
}

// namespaceDefinitionAt resolves enum and union namespace accesses.
func namespaceDefinitionAt(
	tokens []token.Token,
	tokenIndex int,
	index navigationIndex,
) (navigationDeclaration, bool) {
	if tokenIndex >= 2 && tokens[tokenIndex-1].Type == token.DoubleColon {
		receiver := tokens[tokenIndex-2].Literal
		name := tokens[tokenIndex].Literal
		if decl, ok := index.enumVariants[receiver][name]; ok {
			return decl, true
		}
		if decl, ok := index.unionVariants[receiver][name]; ok {
			return decl, true
		}
	}
	name := tokens[tokenIndex].Literal
	if tokenIndex+1 < len(tokens) && tokens[tokenIndex+1].Type == token.DoubleColon {
		if decl, ok := index.types[name]; ok {
			return decl, true
		}
	}
	return navigationDeclaration{}, false
}

// globalDefinition resolves top-level functions, types, and modules.
func globalDefinition(
	name string,
	index navigationIndex,
	sources []navigationSource,
) (navigationDeclaration, bool) {
	if decl, ok := index.functions[name]; ok {
		return decl, true
	}
	if decl, ok := index.types[name]; ok {
		return decl, true
	}
	for _, src := range sources {
		if src.module == name {
			if decl, ok := index.modules[src.module]; ok {
				return decl, true
			}
		}
	}
	return navigationDeclaration{}, false
}

// localDefinitionAt resolves visible parameters and local bindings.
func localDefinitionAt(
	source string,
	uri string,
	position Position,
	name string,
) (navigationDeclaration, bool) {
	decls := visibleLocalDeclarations(source, uri, position)
	for i := len(decls) - 1; i >= 0; i-- {
		if decls[i].name == name {
			return decls[i], true
		}
	}
	return navigationDeclaration{}, false
}

// localTypeAt returns the inferred type for a visible local binding.
func localTypeAt(source string, position Position, name string) string {
	if decl, ok := localDefinitionAt(source, "", position, name); ok {
		return strings.TrimPrefix(decl.detail, name+": ")
	}
	return ""
}

// visibleLocalDeclarations returns local declarations visible at a position.
func visibleLocalDeclarations(
	source string,
	uri string,
	position Position,
) []navigationDeclaration {
	tokens := lexCompletionTokens(source)
	decls := []navigationDeclaration{}
	bindings := map[string]string{}
	for i := 0; i < len(tokens); i++ {
		if tokenStartsAfter(tokens[i], position) {
			break
		}
		switch tokens[i].Type {
		case token.Function:
			params, next := readParamDeclarations(tokens, i, uri)
			decls = append(decls[:0], params...)
			bindings = declarationsToBindings(params)
			i = next
		case token.Ident:
			if isTestDeclStart(tokens, i) {
				decls = decls[:0]
				bindings = map[string]string{}
			}
		case token.Let, token.Var:
			if decl, binding, ok := readLocalDeclaration(tokens, i, uri, bindings); ok {
				decls = append(decls, decl)
				if binding.typ != "" {
					bindings[binding.name] = binding.typ
				}
			}
		case token.For:
			if decl, binding, ok := readForPipeDeclaration(tokens, i, uri); ok {
				decls = append(decls, decl)
				bindings[binding.name] = binding.typ
			}
		}
	}
	return decls
}

// readParamDeclarations reads function parameters as local declarations.
func readParamDeclarations(
	tokens []token.Token,
	start int,
	uri string,
) ([]navigationDeclaration, int) {
	open := findNextToken(tokens, start, token.LParen)
	if open < 0 {
		return nil, start
	}
	close := skipBalanced(tokens, open, token.LParen, token.RParen)
	if close <= open {
		return nil, open
	}
	decls := []navigationDeclaration{}
	for i := open + 1; i < close; i++ {
		if tokens[i].Type == token.Comptime {
			i++
		}
		if i+1 >= close || tokens[i].Type != token.Ident || tokens[i+1].Type != token.Colon {
			continue
		}
		typ, next := readTypeUntil(tokens, i+2, token.Comma, token.RParen)
		name := tokens[i].Literal
		decls = append(decls, navigationDeclaration{
			name:   name,
			detail: name + ": " + typ,
			uri:    uri,
			rng:    tokenRange(tokens[i]),
			kind:   symbolKindField,
		})
		i = next
	}
	return decls, close
}

// declarationsToBindings converts declaration details into type bindings.
func declarationsToBindings(decls []navigationDeclaration) map[string]string {
	bindings := map[string]string{}
	for _, decl := range decls {
		if name, typ, ok := splitLocalDetail(decl.detail); ok {
			bindings[name] = typ
		}
	}
	return bindings
}

// readLocalDeclaration reads a let or var binding as a local declaration.
func readLocalDeclaration(
	tokens []token.Token,
	start int,
	uri string,
	bindings map[string]string,
) (navigationDeclaration, localBinding, bool) {
	if start+1 >= len(tokens) || tokens[start+1].Type != token.Ident {
		return navigationDeclaration{}, localBinding{}, false
	}
	name := tokens[start+1].Literal
	typ := ""
	assign := findBindingAssign(tokens, start+2)
	if assign >= 0 {
		typ, _ = inferInlayExprType(tokens, assign+1, bindings)
	}
	return navigationDeclaration{
		name:   name,
		detail: name + localDetailSuffix(typ),
		uri:    uri,
		rng:    tokenRange(tokens[start+1]),
		kind:   symbolKindField,
	}, localBinding{name: name, typ: typ}, true
}

// readForPipeDeclaration reads the loop pipe binding in a for expression.
func readForPipeDeclaration(
	tokens []token.Token,
	start int,
	uri string,
) (navigationDeclaration, localBinding, bool) {
	if start+5 >= len(tokens) || tokens[start+3].Type != token.Pipe ||
		tokens[start+4].Type != token.Ident {
		return navigationDeclaration{}, localBinding{}, false
	}
	name := tokens[start+4].Literal
	return navigationDeclaration{
		name:   name,
		detail: name + ": i64",
		uri:    uri,
		rng:    tokenRange(tokens[start+4]),
		kind:   symbolKindField,
	}, localBinding{name: name, typ: "i64"}, true
}

// splitLocalDetail splits a local hover detail into name and type.
func splitLocalDetail(detail string) (string, string, bool) {
	name, typ, ok := strings.Cut(detail, ": ")
	return name, typ, ok
}

// localDetailSuffix formats an optional local type suffix.
func localDetailSuffix(typ string) string {
	if typ == "" {
		return ""
	}
	return ": " + typ
}

// implTarget reads the target type and body brace of an impl block.
func implTarget(tokens []token.Token, start int) (string, int) {
	for i := start + 1; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.For:
			return readTypeBeforeBrace(tokens, i+1)
		case token.LBrace:
			return tokenText(tokens[start+1 : i]), i
		case token.EOF:
			return "", -1
		}
	}
	return "", -1
}

// declarationHeaderEnd returns where a declaration header stops.
func declarationHeaderEnd(tokens []token.Token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].Type {
		case token.LParen, token.LBracket, token.LT:
			depth++
		case token.RParen, token.RBracket, token.GT:
			if depth > 0 {
				depth--
			}
		case token.LBrace, token.Semicolon, token.EOF:
			if depth == 0 {
				return i
			}
		}
	}
	return start
}

// importPathAt returns the full import path containing a token.
func importPathAt(tokens []token.Token, tokenIndex int) (string, bool) {
	for i := tokenIndex; i >= 0; i-- {
		switch tokens[i].Type {
		case token.Import:
			path, next := readImportPath(tokens, i+1)
			if tokenIndex <= next && len(path) > 0 {
				return strings.Join(path, "::"), true
			}
			return "", false
		case token.Semicolon, token.LBrace, token.RBrace:
			return "", false
		}
	}
	return "", false
}

// nextIdentifierIndex returns the next identifier before a declaration boundary.
func nextIdentifierIndex(tokens []token.Token, start int) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Type == token.Ident {
			return i
		}
		if tokens[i].Type == token.LBrace ||
			tokens[i].Type == token.Semicolon ||
			tokens[i].Type == token.EOF {
			return -1
		}
	}
	return -1
}

// addNestedDeclaration stores a declaration under an owner type.
func addNestedDeclaration(
	table map[string]map[string]navigationDeclaration,
	owner string,
	decl navigationDeclaration,
) {
	if table[owner] == nil {
		table[owner] = map[string]navigationDeclaration{}
	}
	table[owner][decl.name] = decl
}

// tokenIndexAtPosition returns the token under an LSP position.
func tokenIndexAtPosition(tokens []token.Token, position Position) int {
	for i, tok := range tokens {
		if tok.Type == token.EOF {
			return -1
		}
		if tokenContainsPosition(tok, position) {
			return i
		}
	}
	return -1
}

// tokenContainsPosition checks whether an LSP position is inside a token.
func tokenContainsPosition(tok token.Token, position Position) bool {
	if tok.Line <= 0 || tok.Column <= 0 {
		return false
	}
	line := tok.Line - 1
	start := tok.Column - 1
	end := start + utf16Len(tok.Literal)
	return position.Line == line &&
		position.Character >= start &&
		position.Character < end
}

// tokenStartsAfter reports whether a token starts after a position.
func tokenStartsAfter(tok token.Token, position Position) bool {
	if tok.Line <= 0 || tok.Column <= 0 {
		return false
	}
	line := tok.Line - 1
	character := tok.Column - 1
	return line > position.Line ||
		line == position.Line && character > position.Character
}

// tokenRange converts one token into an LSP range.
func tokenRange(tok token.Token) Range {
	start := Position{Line: tok.Line - 1, Character: tok.Column - 1}
	return Range{
		Start: start,
		End: Position{
			Line:      start.Line,
			Character: start.Character + utf16Len(tok.Literal),
		},
	}
}

// rangeFromTokenSpan converts a token span into an LSP range.
func rangeFromTokenSpan(tokens []token.Token, start int, end int) Range {
	if start < 0 || start >= len(tokens) {
		return Range{}
	}
	if end < start || end >= len(tokens) || tokens[end].Type == token.EOF {
		end = start
	}
	rng := tokenRange(tokens[start])
	rng.End = tokenRange(tokens[end]).End
	return rng
}

// firstTokenRange returns the range of the first token in source.
func firstTokenRange(source string) Range {
	tokens := lexCompletionTokens(source)
	for _, tok := range tokens {
		if tok.Type != token.EOF {
			return tokenRange(tok)
		}
	}
	return oneCharacterRange(0, 0)
}

// fileURIFromPath converts a local path into a file URI.
func fileURIFromPath(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// kizuHoverMarkup renders hover content as a Kizu markdown code block.
func kizuHoverMarkup(detail string) string {
	return "```kizu\n" + detail + "\n```"
}
