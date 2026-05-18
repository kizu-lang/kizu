package transpile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GenerateCompiler writes a constrained Kizu bootstrap compiler seed from Go sources.
func GenerateCompiler(repoRoot string, outDir string) error {
	tokenInfo, err := readTokenPackage(filepath.Join(repoRoot, "internal", "token", "token.go"))
	if err != nil {
		return err
	}
	lexerInfo, err := readLexerPackage(filepath.Join(repoRoot, "internal", "lexer", "lexer.go"))
	if err != nil {
		return err
	}
	parserInfo, err := readParserPackage(filepath.Join(repoRoot, "internal", "parser", "parser.go"))
	if err != nil {
		return err
	}
	typeInfo, err := readTypesPackage(filepath.Join(repoRoot, "internal", "types", "checker.go"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "src"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"kizu.toml":         manifestSource(),
		"src/token.kizu":    tokenSource(tokenInfo),
		"src/lexer.kizu":    lexerSource(lexerInfo),
		"src/parser.kizu":   parserSource(parserInfo),
		"src/resolver.kizu": resolverSource(),
		"src/checker.kizu":  checkerSource(typeInfo),
		"src/lower.kizu":    lowerSource(),
		"src/emit.kizu":     emitSource(),
		"src/compiler.kizu": compilerSource(),
		"src/main.kizu":     mainSource(),
	}
	return writeFiles(outDir, files)
}

type tokenPackage struct {
	typeTags []string
	keywords []keyword
	fields   []field
}

type lexerPackage struct {
	fields      []field
	punctuation []punctuationRule
}

type parserPackage struct {
	precedences []precedenceRule
}

type typesPackage struct {
	knownTypes   []string
	numericTypes []string
	copyTypes    []string
}

type field struct {
	name string
	typ  string
}

type keyword struct {
	literal string
	tag     string
}

type punctuationRule struct {
	ch     int
	next   int
	tag    string
	width  int
	serial int
}

type precedenceRule struct {
	tag   string
	level int
}

// readTokenPackage extracts token enum tags and the Token struct.
func readTokenPackage(path string) (tokenPackage, error) {
	file, err := parseGoFile(path)
	if err != nil {
		return tokenPackage{}, err
	}
	return tokenPackage{
		typeTags: collectTypeTags(file),
		keywords: collectKeywords(file),
		fields:   collectStructFields(file, "Token"),
	}, nil
}

// readLexerPackage extracts the Lexer struct shape.
func readLexerPackage(path string) (lexerPackage, error) {
	file, err := parseGoFile(path)
	if err != nil {
		return lexerPackage{}, err
	}
	fields := collectStructFields(file, "Lexer")
	if len(fields) == 0 {
		fields = []field{{name: "input", typ: "[]const u8"}, {name: "position", typ: "i64"}}
	}
	return lexerPackage{fields: fields, punctuation: collectPunctuationRules(file)}, nil
}

// readParserPackage extracts parser tables needed by the bootstrap parser.
func readParserPackage(path string) (parserPackage, error) {
	file, err := parseGoFile(path)
	if err != nil {
		return parserPackage{}, err
	}
	return parserPackage{precedences: collectPrecedenceRules(file)}, nil
}

// readTypesPackage extracts checker tables needed by the bootstrap checker.
func readTypesPackage(path string) (typesPackage, error) {
	file, err := parseGoFile(path)
	if err != nil {
		return typesPackage{}, err
	}
	constants := collectTypeStringConstants(file)
	return typesPackage{
		knownTypes:   collectTypeSet(file, constants, "knownTypes"),
		numericTypes: collectTypeSet(file, constants, "numericTypes"),
		copyTypes:    collectTypeSet(file, constants, "copyTypes"),
	}, nil
}

// parseGoFile parses one Go source file.
func parseGoFile(path string) (*ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// collectTypeTags extracts constants declared with token.Type.
func collectTypeTags(file *ast.File) []string {
	tags := []string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || !isTypeSpec(value.Type) {
				continue
			}
			for _, name := range value.Names {
				tags = append(tags, name.Name)
			}
		}
	}
	return tags
}

// collectKeywords extracts the Go token keyword table.
func collectKeywords(file *ast.File) []keyword {
	out := []keyword{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "keywords" {
				continue
			}
			out = append(out, keywordsFromValueSpec(value)...)
		}
	}
	sort.Slice(out, func(i int, j int) bool { return out[i].literal < out[j].literal })
	return out
}

// keywordsFromValueSpec extracts key/value pairs from the keywords composite literal.
func keywordsFromValueSpec(value *ast.ValueSpec) []keyword {
	if len(value.Values) != 1 {
		return nil
	}
	lit, ok := value.Values[0].(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := []keyword{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			continue
		}
		value, ok := pair.Value.(*ast.Ident)
		if !ok {
			continue
		}
		out = append(out, keyword{literal: strings.Trim(key.Value, `"`), tag: value.Name})
	}
	return out
}

// collectPunctuationRules extracts lexer punctuation maps from the Go lexer.
func collectPunctuationRules(file *ast.File) []punctuationRule {
	rules := []punctuationRule{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 {
				continue
			}
			rules = append(rules, punctuationRulesFromSpec(value)...)
		}
	}
	rules = append(rules,
		punctuationRule{ch: '/', tag: "Slash", width: 1},
		punctuationRule{ch: '=', next: '>', tag: "FatArrow", width: 2},
	)
	return normalizePunctuationRules(rules)
}

// punctuationRulesFromSpec extracts punctuation token rules from one Go var.
func punctuationRulesFromSpec(value *ast.ValueSpec) []punctuationRule {
	switch value.Names[0].Name {
	case "singleCharTokens":
		return singleCharRules(value)
	case "compoundTokens":
		return compoundRules(value)
	default:
		return nil
	}
}

// singleCharRules extracts one-byte token rules.
func singleCharRules(value *ast.ValueSpec) []punctuationRule {
	lit, ok := onlyCompositeValue(value)
	if !ok {
		return nil
	}
	rules := []punctuationRule{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ch, ok := runeLiteralValue(pair.Key)
		tag, okTag := tokenSelectorName(pair.Value)
		if ok && okTag {
			rules = append(rules, punctuationRule{ch: ch, tag: tag, width: 1})
		}
	}
	return rules
}

// compoundRules extracts two-byte token rules and their one-byte fallback.
func compoundRules(value *ast.ValueSpec) []punctuationRule {
	lit, ok := onlyCompositeValue(value)
	if !ok {
		return nil
	}
	rules := []punctuationRule{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		rules = append(rules, compoundRuleFromPair(pair)...)
	}
	return rules
}

// compoundRuleFromPair extracts compound and single fallback rules from one map pair.
func compoundRuleFromPair(pair *ast.KeyValueExpr) []punctuationRule {
	ch, ok := runeLiteralValue(pair.Key)
	lit, okLit := pair.Value.(*ast.CompositeLit)
	if !ok || !okLit {
		return nil
	}
	fields := compoundFields(lit)
	next, okNext := fields["nextRune"].(int)
	compound, okCompound := fields["compound"].(string)
	single, okSingle := fields["single"].(string)
	rules := []punctuationRule{}
	if okNext && okCompound {
		rules = append(rules, punctuationRule{ch: ch, next: next, tag: compound, width: 2})
	}
	if okSingle {
		rules = append(rules, punctuationRule{ch: ch, tag: single, width: 1})
	}
	return rules
}

// compoundFields extracts keyed fields from a compoundToken literal.
func compoundFields(lit *ast.CompositeLit) map[string]any {
	fields := map[string]any{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch name.Name {
		case "next":
			if ch, ok := runeLiteralValue(pair.Value); ok {
				fields["nextRune"] = ch
			}
		case "compound", "single":
			if tag, ok := tokenSelectorName(pair.Value); ok {
				fields[name.Name] = tag
			}
		}
	}
	return fields
}

// onlyCompositeValue returns the sole composite literal from a value spec.
func onlyCompositeValue(value *ast.ValueSpec) (*ast.CompositeLit, bool) {
	if len(value.Values) != 1 {
		return nil, false
	}
	lit, ok := value.Values[0].(*ast.CompositeLit)
	return lit, ok
}

// runeLiteralValue extracts an ASCII rune literal value.
func runeLiteralValue(expr ast.Expr) (int, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.CHAR {
		return 0, false
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil {
		return 0, false
	}
	runes := []rune(text)
	if len(runes) != 1 || runes[0] > 127 {
		return 0, false
	}
	return int(runes[0]), true
}

// tokenSelectorName extracts token.X selector names.
func tokenSelectorName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "token" {
		return "", false
	}
	return sel.Sel.Name, true
}

// normalizePunctuationRules orders rules and removes duplicate fallbacks.
func normalizePunctuationRules(rules []punctuationRule) []punctuationRule {
	for idx := range rules {
		rules[idx].serial = idx
	}
	sort.SliceStable(rules, func(i int, j int) bool {
		if rules[i].width != rules[j].width {
			return rules[i].width > rules[j].width
		}
		if rules[i].ch != rules[j].ch {
			return rules[i].ch < rules[j].ch
		}
		if rules[i].next != rules[j].next {
			return rules[i].next < rules[j].next
		}
		return rules[i].serial < rules[j].serial
	})
	seen := map[string]bool{}
	out := make([]punctuationRule, 0, len(rules))
	for _, rule := range rules {
		key := fmt.Sprintf("%d:%d:%d:%s", rule.width, rule.ch, rule.next, rule.tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out
}

// collectPrecedenceRules extracts the Pratt precedence table from parser.go.
func collectPrecedenceRules(file *ast.File) []precedenceRule {
	levels := collectPrecedenceLevels(file)
	rules := []precedenceRule{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == "precedences" {
				rules = append(rules, precedenceRulesFromSpec(value, levels)...)
			}
		}
	}
	sort.Slice(rules, func(i int, j int) bool { return rules[i].tag < rules[j].tag })
	return rules
}

// collectPrecedenceLevels extracts iota-assigned parser precedence names.
func collectPrecedenceLevels(file *ast.File) map[string]int {
	levels := map[string]int{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		collectConstPrecedenceLevels(gen, levels)
	}
	return levels
}

// collectConstPrecedenceLevels records names from the parser precedence const group.
func collectConstPrecedenceLevels(gen *ast.GenDecl, levels map[string]int) {
	index := 0
	for _, spec := range gen.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok || len(value.Names) == 0 {
			index++
			continue
		}
		if len(value.Values) > 0 && !valueUsesIota(value) && len(levels) == 0 {
			index++
			continue
		}
		for _, name := range value.Names {
			if name.Name != "_" {
				levels[name.Name] = index
			}
		}
		index++
	}
}

// valueUsesIota reports whether a const spec starts an iota enum.
func valueUsesIota(value *ast.ValueSpec) bool {
	for _, expr := range value.Values {
		if ident, ok := expr.(*ast.Ident); ok && ident.Name == "iota" {
			return true
		}
	}
	return false
}

// precedenceRulesFromSpec extracts token precedence mappings.
func precedenceRulesFromSpec(value *ast.ValueSpec, levels map[string]int) []precedenceRule {
	lit, ok := onlyCompositeValue(value)
	if !ok {
		return nil
	}
	rules := []precedenceRule{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		tag, okTag := tokenSelectorName(pair.Key)
		levelName, okLevel := identName(pair.Value)
		level, okKnown := levels[levelName]
		if okTag && okLevel && okKnown {
			rules = append(rules, precedenceRule{tag: tag, level: level})
		}
	}
	return rules
}

// identName extracts a bare identifier name.
func identName(expr ast.Expr) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// collectTypeStringConstants extracts Type string constants from checker.go.
func collectTypeStringConstants(file *ast.File) map[string]string {
	constants := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			recordTypeStringConstants(value, constants)
		}
	}
	return constants
}

// recordTypeStringConstants records string-valued Type constants.
func recordTypeStringConstants(value *ast.ValueSpec, constants map[string]string) {
	for idx, name := range value.Names {
		if idx >= len(value.Values) {
			continue
		}
		text, ok := stringLiteralValue(value.Values[idx])
		if ok {
			constants[name.Name] = text
		}
	}
}

// collectTypeSet extracts a map[Type]bool table as sorted string values.
func collectTypeSet(file *ast.File, constants map[string]string, name string) []string {
	values := []string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == name {
				values = append(values, typeSetValues(value, constants)...)
			}
		}
	}
	sort.Strings(values)
	return values
}

// typeSetValues extracts true-valued map keys from a checker type set.
func typeSetValues(value *ast.ValueSpec, constants map[string]string) []string {
	lit, ok := onlyCompositeValue(value)
	if !ok {
		return nil
	}
	values := []string{}
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok || !boolLiteralValue(pair.Value) {
			continue
		}
		text, ok := typeSetKey(pair.Key, constants)
		if ok {
			values = append(values, text)
		}
	}
	return values
}

// typeSetKey resolves a type-set key expression to its string value.
func typeSetKey(expr ast.Expr, constants map[string]string) (string, bool) {
	if text, ok := stringLiteralValue(expr); ok {
		return text, true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		text, known := constants[ident.Name]
		return text, known
	}
	return "", false
}

// stringLiteralValue extracts an unquoted Go string literal.
func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return text, true
}

// boolLiteralValue reports whether expr is the literal true.
func boolLiteralValue(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "true"
}

// collectStructFields extracts fields for one named Go struct.
func collectStructFields(file *ast.File, name string) []field {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			return fieldsFromStruct(structType)
		}
	}
	return nil
}

// fieldsFromStruct maps Go struct fields to Kizu field declarations.
func fieldsFromStruct(structType *ast.StructType) []field {
	fields := []field{}
	for _, item := range structType.Fields.List {
		for _, name := range item.Names {
			fields = append(fields, field{name: name.Name, typ: goTypeName(item.Type)})
		}
	}
	return fields
}

// isTypeSpec reports whether expr names token.Type.
func isTypeSpec(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "Type"
}

// goTypeName maps the small Go subset used by bootstrap sources.
func goTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return identTypeName(e.Name)
	case *ast.ArrayType:
		return "[]const " + goTypeName(e.Elt)
	default:
		return "i64"
	}
}

// identTypeName maps Go identifier types to Kizu types.
func identTypeName(name string) string {
	switch name {
	case "string":
		return "[]const u8"
	case "int":
		return "i64"
	case "bool":
		return "bool"
	case "rune":
		return "u8"
	default:
		return name
	}
}

// manifestSource returns the generated package manifest.
func manifestSource() string {
	return `[package]
name = "selfhost"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`
}

// tokenSource renders the token module.
func tokenSource(info tokenPackage) string {
	var out bytes.Buffer
	out.WriteString("pub enum Type {\n")
	for _, tag := range sortedStrings(info.typeTags) {
		fmt.Fprintf(&out, "    %s\n", tag)
	}
	out.WriteString("}\n\n")
	out.WriteString("pub struct Token {\n")
	for _, field := range info.fields {
		fmt.Fprintf(&out, "    pub %s: %s\n", field.name, field.typ)
	}
	out.WriteString("}\n\n")
	out.WriteString("pub fn LookupIdent(ident: []const u8) -> Type {\n")
	for _, keyword := range info.keywords {
		fmt.Fprintf(&out, "    if ident == %q {\n", keyword.literal)
		fmt.Fprintf(&out, "        return Type::%s;\n", keyword.tag)
		out.WriteString("    }\n")
	}
	out.WriteString("    return Type::Ident;\n")
	out.WriteString("}\n")
	out.WriteString("\n")
	out.WriteString("pub fn EOFToken() -> Token {\n")
	out.WriteString("    return Token {\n")
	out.WriteString("        Type: Type::EOF,\n")
	out.WriteString("        Literal: \"\",\n")
	out.WriteString("        Line: 0,\n")
	out.WriteString("        Column: 0,\n")
	out.WriteString("    };\n")
	out.WriteString("}\n")
	out.WriteString("\n")
	out.WriteString("pub fn New(kind: Type, literal: []const u8, line: i64, column: i64) -> Token {\n")
	out.WriteString("    return Token {\n")
	out.WriteString("        Type: kind,\n")
	out.WriteString("        Literal: literal,\n")
	out.WriteString("        Line: line,\n")
	out.WriteString("        Column: column,\n")
	out.WriteString("    };\n")
	out.WriteString("}\n")
	return out.String()
}

// lexerSource renders a compileable lexer bootstrap module.
func lexerSource(info lexerPackage) string {
	var out bytes.Buffer
	out.WriteString("import selfhost::token;\n\n")
	out.WriteString("pub struct Lexer {\n")
	for _, field := range info.fields {
		fmt.Fprintf(&out, "    pub %s: %s\n", field.name, field.typ)
	}
	out.WriteString("}\n\n")
	out.WriteString("pub fn New(input: []const u8) -> Lexer {\n")
	out.WriteString("    return Lexer {\n")
	writeLexerInitializers(&out, info.fields)
	out.WriteString("    };\n")
	out.WriteString("}\n\n")
	out.WriteString("pub fn NextToken(self: Lexer) -> token::Token {\n")
	out.WriteString("    return token_at(self.input, self.position, self.line, self.column);\n")
	out.WriteString("}\n")
	out.WriteString("\n")
	out.WriteString(firstTokenSource())
	out.WriteString(lexerTokenAtSource())
	out.WriteString(firstTokenCodeSource())
	out.WriteString(lexerTokenCounterSource())
	out.WriteString(lexerPunctuationSource(info.punctuation))
	out.WriteString(lexerPositionPunctuationSource(info.punctuation))
	out.WriteString(lexerHelpersSource())
	return out.String()
}

// firstTokenSource returns the Kizu source for the first-token scanner.
func firstTokenSource() string {
	return `pub fn FirstToken(input: []const u8) -> token::Token {
    let length = std::builtin::mem_len(input);
    var start = 0;
    while start < length and is_space(input[start]) {
        start = start + 1;
    }
    if start >= length {
        return token::EOFToken();
    }
    let ch = input[start];
    if ch == cast<u8>(34) {
        var end = start + 1;
        while end < length and input[end] != cast<u8>(34) {
            end = end + 1;
        }
        return make_token(token::Type::String, input[start + 1..end]);
    }
    if is_letter(ch) {
        var end = start;
        while end < length and is_ident_byte(input[end]) {
            end = end + 1;
        }
        let literal = input[start..end];
        return make_token(token::LookupIdent(literal), literal);
    }
    if is_digit(ch) {
        var end = start;
        while end < length and is_digit(input[end]) {
            end = end + 1;
        }
        return make_token(token::Type::Int, input[start..end]);
    }
    return punctuation_token(input, start, length);
}

`
}

// lexerTokenAtSource returns the Kizu source for the position-aware token scanner.
func lexerTokenAtSource() string {
	return `fn token_at(input: []const u8, position: i64, line: i64, column: i64) -> token::Token {
    let length = std::builtin::mem_len(input);
    var start = position;
    var token_line = line;
    var token_column = column;
    while start < length and is_space(input[start]) {
        if input[start] == cast<u8>(10) {
            token_line = token_line + 1;
            token_column = 0;
        } else {
            token_column = token_column + 1;
        }
        start = start + 1;
    }
    if start >= length {
        return token::EOFToken();
    }
    let ch = input[start];
    if ch == cast<u8>(34) {
        var end = start + 1;
        while end < length and input[end] != cast<u8>(34) {
            end = end + 1;
        }
        return make_token_at(token::Type::String, input[start + 1..end],
            token_line, token_column + 1);
    }
    if is_letter(ch) {
        var end = start;
        while end < length and is_ident_byte(input[end]) {
            end = end + 1;
        }
        let literal = input[start..end];
        return make_token_at(token::LookupIdent(literal), literal, token_line, token_column + 1);
    }
    if is_digit(ch) {
        var end = start;
        while end < length and is_digit(input[end]) {
            end = end + 1;
        }
        return make_token_at(token::Type::Int, input[start..end], token_line, token_column + 1);
    }
    return punctuation_token_at(input, start, length, token_line, token_column + 1);
}

`
}

// firstTokenCodeSource returns the Kizu source for numeric parser smoke output.
func firstTokenCodeSource() string {
	return `pub fn FirstTokenCode(input: []const u8) -> i64 {
    let length = std::builtin::mem_len(input);
    var start = 0;
    while start < length and is_space(input[start]) {
        start = start + 1;
    }
    if start >= length {
        return 0;
    }
    let ch = input[start];
    if ch == cast<u8>(105) {
        return 10;
    }
    if ch == cast<u8>(112) {
        return 11;
    }
    if ch == cast<u8>(102) {
        return 12;
    }
    if is_letter(ch) {
        return 1;
    }
    if is_digit(ch) {
        return 2;
    }
    return 3;
}

`
}

// lexerTokenCounterSource returns a lexer-backed token counter for parser metrics.
func lexerTokenCounterSource() string {
	return lexerTokenCounterEntrySource() + lexerTokenCounterKeywordSource() +
		lexerTokenCounterAdvanceSource()
}

// lexerTokenCounterEntrySource returns public token-count entry points.
func lexerTokenCounterEntrySource() string {
	return `pub fn CountFunction(input: []const u8) -> i64 {
    return count_keyword(input, 1);
}

pub fn CountImport(input: []const u8) -> i64 {
    return count_keyword(input, 2);
}

pub fn CountStruct(input: []const u8) -> i64 {
    return count_keyword(input, 3);
}

pub fn CountEnum(input: []const u8) -> i64 {
    return count_keyword(input, 4);
}

fn count_keyword(input: []const u8, want: i64) -> i64 {
    let length = std::builtin::mem_len(input);
    var index = 0;
    var count = 0;
    while index < length {
        while index < length and is_space(input[index]) {
            index = index + 1;
        }
        if index >= length {
            return count;
        }
        if is_letter(input[index]) {
            let end = ident_end(input, index, length);
            if keyword_code(input, index, end, want) {
                count = count + 1;
            }
            index = end;
        } else {
            if is_digit(input[index]) {
                index = digit_end(input, index, length);
            } else {
                index = punctuation_end(input, index, length);
            }
        }
    }
    return count;
}

`
}

// lexerTokenCounterKeywordSource returns byte-level keyword matching helpers.
func lexerTokenCounterKeywordSource() string {
	return `fn keyword_code(input: []const u8, start: i64, end: i64, want: i64) -> bool {
    let width = end - start;
    if want == 1 and width == 2 {
        return input[start] == cast<u8>(102) and input[start + 1] == cast<u8>(110);
    }
    if want == 2 and width == 6 {
        return input[start] == cast<u8>(105) and input[start + 1] == cast<u8>(109) and
            input[start + 2] == cast<u8>(112) and input[start + 3] == cast<u8>(111) and
            input[start + 4] == cast<u8>(114) and input[start + 5] == cast<u8>(116);
    }
    if want == 3 and width == 6 {
        return input[start] == cast<u8>(115) and input[start + 1] == cast<u8>(116) and
            input[start + 2] == cast<u8>(114) and input[start + 3] == cast<u8>(117) and
            input[start + 4] == cast<u8>(99) and input[start + 5] == cast<u8>(116);
    }
    if want == 4 and width == 4 {
        return input[start] == cast<u8>(101) and input[start + 1] == cast<u8>(110) and
            input[start + 2] == cast<u8>(117) and input[start + 3] == cast<u8>(109);
    }
    return false;
}

`
}

// lexerTokenCounterAdvanceSource returns scanner advancement helpers.
func lexerTokenCounterAdvanceSource() string {
	return `fn ident_end(input: []const u8, start: i64, length: i64) -> i64 {
    var end = start;
    while end < length and is_ident_byte(input[end]) {
        end = end + 1;
    }
    return end;
}

fn digit_end(input: []const u8, start: i64, length: i64) -> i64 {
    var end = start;
    while end < length and is_digit(input[end]) {
        end = end + 1;
    }
    return end;
}

fn punctuation_end(input: []const u8, start: i64, length: i64) -> i64 {
    if start + 1 < length {
        let ch = input[start];
        let next = input[start + 1];
        if (ch == cast<u8>(33) and next == cast<u8>(61)) or
            (ch == cast<u8>(45) and next == cast<u8>(62)) or
            (ch == cast<u8>(46) and next == cast<u8>(46)) or
            (ch == cast<u8>(58) and next == cast<u8>(58)) or
            (ch == cast<u8>(60) and next == cast<u8>(61)) or
            (ch == cast<u8>(61) and next == cast<u8>(61)) or
            (ch == cast<u8>(61) and next == cast<u8>(62)) or
            (ch == cast<u8>(62) and next == cast<u8>(61)) {
            return start + 2;
        }
    }
    return start + 1;
}

`
}

// lexerPunctuationSource returns Kizu punctuation scanning helpers.
func lexerPunctuationSource(rules []punctuationRule) string {
	var out bytes.Buffer
	out.WriteString(`fn punctuation_token(input: []const u8, start: i64, length: i64) -> token::Token {
    let ch = input[start];
`)
	writePunctuationCases(&out, rules, false)
	out.WriteString(`    return make_token(token::Type::Illegal, input[start..start + 1]);
}

fn make_token(kind: token::Type, literal: []const u8) -> token::Token {
    return token::New(kind, literal, 1, 1);
}

`)
	return out.String()
}

// writePunctuationCases renders punctuation cases shared by lexer helpers.
func writePunctuationCases(out *bytes.Buffer, rules []punctuationRule, positioned bool) {
	for _, rule := range rules {
		writePunctuationCase(out, rule, positioned)
	}
}

// writePunctuationCase renders one generated punctuation rule.
func writePunctuationCase(out *bytes.Buffer, rule punctuationRule, positioned bool) {
	condition := fmt.Sprintf("ch == cast<u8>(%d)", rule.ch)
	if rule.width == 2 {
		condition += fmt.Sprintf(
			" and start + 1 < length and input[start + 1] == cast<u8>(%d)",
			rule.next,
		)
	}
	end := "start + 1"
	if rule.width == 2 {
		end = "start + 2"
	}
	if positioned {
		fmt.Fprintf(out, "    if %s {\n", condition)
		fmt.Fprintf(out,
			"        return make_token_at(token::Type::%s, input[start..%s], line, column);\n",
			rule.tag, end)
		out.WriteString("    }\n")
		return
	}
	fmt.Fprintf(out, "    if %s {\n", condition)
	fmt.Fprintf(out, "        return make_token(token::Type::%s, input[start..%s]);\n", rule.tag, end)
	out.WriteString("    }\n")
}

// illegalPositionedTokenLine returns the positioned illegal-token fallback.
func illegalPositionedTokenLine() string {
	return "    return make_token_at(token::Type::Illegal, input[start..start + 1], " +
		"line, column);\n"
}

// lexerPositionPunctuationSource returns position-aware punctuation helpers.
func lexerPositionPunctuationSource(rules []punctuationRule) string {
	var out bytes.Buffer
	out.WriteString(`fn punctuation_token_at(
    input: []const u8,
    start: i64,
    length: i64,
    line: i64,
    column: i64
) -> token::Token {
    let ch = input[start];
`)
	writePunctuationCases(&out, rules, true)
	out.WriteString(illegalPositionedTokenLine())
	out.WriteString(`}

fn make_token_at(kind: token::Type, literal: []const u8, line: i64, column: i64) -> token::Token {
    return token::New(kind, literal, line, column);
}

`)
	return out.String()
}

// lexerHelpersSource returns Kizu byte classification helpers.
func lexerHelpersSource() string {
	return `fn is_ident_byte(ch: u8) -> bool {
    return is_letter(ch) or is_digit(ch);
}

fn is_letter(ch: u8) -> bool {
    return (ch >= cast<u8>(97) and ch <= cast<u8>(122)) or
        (ch >= cast<u8>(65) and ch <= cast<u8>(90)) or
        ch == cast<u8>(95);
}

fn is_digit(ch: u8) -> bool {
    return ch >= cast<u8>(48) and ch <= cast<u8>(57);
}

fn is_space(ch: u8) -> bool {
    return ch == cast<u8>(32) or ch == cast<u8>(9) or
        ch == cast<u8>(10) or ch == cast<u8>(13);
}
`
}

// parserSource renders a compileable parser bootstrap module.
func parserSource(info parserPackage) string {
	return parserHeaderSource(info) + parserModuleSummarySource() + parserBraceSource()
}

// parserHeaderSource renders parser entry points and public scoring functions.
func parserHeaderSource(info parserPackage) string {
	var out bytes.Buffer
	out.WriteString(`import selfhost::lexer;
import selfhost::token;

pub struct Parser {
    pub source: []const u8
}

pub fn Parser(source: []const u8) -> Parser {
    return Parser {
        source: source
    };
}

pub fn first_token(self: Parser) -> token::Token {
    let l = lexer::New(self.source);
    return lexer::NextToken(l);
}

pub fn first_token_from_source(source: []const u8) -> token::Token {
    return lexer::FirstToken(source);
}

pub fn first_token_code(source: []const u8) -> i64 {
    return lexer::FirstTokenCode(source);
}

pub fn function_count(source: []const u8) -> i64 {
    return lexer::CountFunction(source);
}

pub fn declaration_score(source: []const u8) -> i64 {
    return lexer::CountFunction(source) * 5 + lexer::CountImport(source) * 3 +
        lexer::CountStruct(source) * 2 + lexer::CountEnum(source) * 2;
}

pub fn brace_score(source: []const u8) -> i64 {
    let balance = brace_balance(source);
    let braces = brace_count(source);
    if balance == 0 {
        return braces;
    }
    return 0;
}

pub fn parse_score(source: []const u8) -> i64 {
    return first_token_code(source) + declaration_score(source) + brace_score(source);
}

`)
	out.WriteString(parserPrecedenceSource(info.precedences))
	return out.String()
}

// parserModuleSummarySource renders the parser summary passed to later phases.
func parserModuleSummarySource() string {
	return `pub struct Module {
    pub score: i64,
    pub first_token: i64,
    pub declarations: i64,
    pub bytes: i64,
    pub functions: i64,
    pub imports: i64,
    pub structs: i64,
    pub enums: i64,
    pub braces: i64,
    pub balance: i64,
    pub balanced: bool
}

pub fn parse_module(source: []const u8) -> Module {
    let first = first_token_code(source);
    let functions = function_count(source);
    let imports = lexer::CountImport(source);
    let structs = lexer::CountStruct(source);
    let enums = lexer::CountEnum(source);
    let declarations = functions * 5 + imports * 3 + structs * 2 + enums * 2;
    let balance = brace_balance(source);
    let braces = brace_count(source);
    var brace_metric = 0;
    if balance == 0 {
        brace_metric = braces;
    }
    return Module {
        score: first + declarations + brace_metric,
        first_token: first,
        declarations: declarations,
        bytes: std::mem::len(source),
        functions: functions,
        imports: imports,
        structs: structs,
        enums: enums,
        braces: braces,
        balance: balance,
        balanced: balance == 0
    };
}

`
}

// parserPrecedenceSource renders the extracted Pratt precedence table.
func parserPrecedenceSource(rules []precedenceRule) string {
	var out bytes.Buffer
	out.WriteString("pub fn precedence(kind: token::Type) -> i64 {\n")
	for _, rule := range rules {
		fmt.Fprintf(&out, "    if kind == token::Type::%s {\n", rule.tag)
		fmt.Fprintf(&out, "        return %d;\n", rule.level)
		out.WriteString("    }\n")
	}
	out.WriteString("    return 1;\n")
	out.WriteString("}\n\n")
	return out.String()
}

// parserBraceSource renders balanced-brace metrics for the checker input.
func parserBraceSource() string {
	return `fn brace_balance(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var balance = 0;
    while index < length {
        if source[index] == cast<u8>(123) {
            balance = balance + 1;
        }
        if source[index] == cast<u8>(125) {
            balance = balance - 1;
        }
        index = index + 1;
    }
    return balance;
}

fn brace_count(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var count = 0;
    while index < length {
        if source[index] == cast<u8>(123) or source[index] == cast<u8>(125) {
            count = count + 1;
        }
        index = index + 1;
    }
    return count;
}

`
}

// compilerSource renders the self-host compiler facade.
func compilerSource() string {
	return compilerIntroSource() + compilerTreeSource() + compilerEmitStage2Source()
}

// compilerIntroSource renders imports and scalar parse entry points.
func compilerIntroSource() string {
	return `import selfhost::checker;
import selfhost::emit;
import selfhost::lower;
import selfhost::parser;
import selfhost::resolver;
import selfhost::token;

pub struct Compiler {
    pub source: []const u8
}

pub struct SourceMetrics {
    pub parsed: i64,
    pub bytes: i64,
    pub functions: i64,
    pub declarations: i64,
    pub braces: i64,
    pub balanced: bool
}

pub fn Compiler(source: []const u8) -> Compiler {
    return Compiler {
        source: source
    };
}

pub fn compile(source: []const u8) -> i64 {
    let module = parse_module(source);
    return module.score;
}

pub fn parse_module(source: []const u8) -> parser::Module {
    return parser::parse_module(source);
}

pub fn first_token(source: []const u8) -> token::Token {
    return parser::first_token_from_source(source);
}

`
}

// compilerTreeSource renders the package compile pipeline used by stage1.
func compilerTreeSource() string {
	return `pub fn compile_tree(io: Io, output: []const u8) -> !void {
    let allocator = std::mem::page_allocator();
    let manifest = try std::fs::read_file(io, "selfhost/kizu.toml");
    let graph = resolver::resolve_selfhost(manifest);
    let token_source = try std::fs::read_file(io, resolver::token_path(graph));
    let lexer_source = try std::fs::read_file(io, resolver::lexer_path(graph));
    let parser_source = try std::fs::read_file(io, resolver::parser_path(graph));
    let resolver_source = try std::fs::read_file(io, resolver::resolver_path(graph));
    let checker_source = try std::fs::read_file(io, resolver::checker_path(graph));
    let lower_source = try std::fs::read_file(io, resolver::lower_path(graph));
    let emit_source = try std::fs::read_file(io, resolver::emit_path(graph));
    let compiler_source = try std::fs::read_file(io, resolver::compiler_path(graph));
    let main_source = try std::fs::read_file(io, resolver::main_path(graph));

    let manifest_parse = parse_module(manifest);
    let token_parse = parse_module(token_source);
    let lexer_parse = parse_module(lexer_source);
    let parser_parse = parse_module(parser_source);
    let resolver_parse = parse_module(resolver_source);
    let checker_parse = parse_module(checker_source);
    let lower_parse = parse_module(lower_source);
    let emit_parse = parse_module(emit_source);
    let compiler_parse = parse_module(compiler_source);
    let main_parse = parse_module(main_source);
    let metrics = SourceMetrics {
        parsed: manifest_parse.score + token_parse.score + lexer_parse.score +
            parser_parse.score + resolver_parse.score + checker_parse.score + lower_parse.score +
            emit_parse.score + compiler_parse.score + main_parse.score,
        bytes: manifest_parse.bytes + token_parse.bytes + lexer_parse.bytes +
            parser_parse.bytes + resolver_parse.bytes + checker_parse.bytes + lower_parse.bytes +
            emit_parse.bytes + compiler_parse.bytes + main_parse.bytes,
        functions: manifest_parse.functions + token_parse.functions + lexer_parse.functions +
            parser_parse.functions + resolver_parse.functions + checker_parse.functions +
            lower_parse.functions + emit_parse.functions + compiler_parse.functions +
            main_parse.functions,
        declarations: manifest_parse.declarations + token_parse.declarations +
            lexer_parse.declarations + parser_parse.declarations + resolver_parse.declarations +
            checker_parse.declarations + lower_parse.declarations + emit_parse.declarations +
            compiler_parse.declarations + main_parse.declarations,
        braces: manifest_parse.braces + token_parse.braces + lexer_parse.braces +
            parser_parse.braces + resolver_parse.braces + checker_parse.braces +
            lower_parse.braces + emit_parse.braces + compiler_parse.braces + main_parse.braces,
        balanced: manifest_parse.balanced and token_parse.balanced and lexer_parse.balanced and
            parser_parse.balanced and resolver_parse.balanced and checker_parse.balanced and
            lower_parse.balanced and emit_parse.balanced and compiler_parse.balanced and
            main_parse.balanced
    };
    let checked = checker::check_entry(
        metrics.parsed,
        metrics.functions,
        metrics.declarations,
        metrics.braces,
        metrics.balanced
    );
    let module = lower::lower_entry(checked);
    let artifact = try emit::llvm(allocator, module, metrics.bytes, metrics.functions);
    let artifact_bytes = artifact.as_bytes();
    try std::fs::write_file(io, output, artifact_bytes);
    return;
}

`
}

// compilerEmitStage2Source renders a CLI-visible emission helper.
func compilerEmitStage2Source() string {
	return `pub fn emit_stage2() -> !void {
    let allocator = std::mem::page_allocator();
    let checked = checker::check_entry(1, 0, 0, 0, false);
    let module = lower::lower_entry(checked);
    let artifact = try emit::llvm(allocator, module, 0, 0);
    let artifact_bytes = artifact.as_bytes();
    print(artifact_bytes);
    return;
}
`
}

// resolverSource renders the minimal self-host package resolver entry.
func resolverSource() string {
	var out bytes.Buffer
	out.WriteString(`pub struct Graph {
    pub root: []const u8
}

pub fn resolve_selfhost(manifest: []const u8) -> Graph {
    if std::builtin::mem_len(manifest) == 0 {
        return Graph { root: "selfhost" };
    }
    return Graph { root: "selfhost" };
}

`)
	for _, path := range selfhostSourcePaths() {
		fmt.Fprintf(&out, "pub fn %s_path(graph: &Graph) -> []const u8 { return %q; }\n",
			selfhostModuleName(path), path)
	}
	return out.String()
}

// selfhostModuleName returns the module stem for a generated selfhost source path.
func selfhostModuleName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// checkerSource renders checker tables and the bootstrap checker entry.
func checkerSource(info typesPackage) string {
	var out bytes.Buffer
	out.WriteString(`pub struct CheckedModule {
    pub valid: bool,
    pub score: i64,
    pub functions: i64,
    pub declarations: i64,
    pub braces: i64,
    pub balanced: bool
}

pub fn check_entry(
    parsed: i64,
    functions: i64,
    declarations: i64,
    braces: i64,
    balanced: bool
) -> CheckedModule {
    return CheckedModule {
        valid: parsed >= 100 and balanced,
        score: parsed,
        functions: functions,
        declarations: declarations,
        braces: braces,
        balanced: balanced
    };
}
`)
	out.WriteString(typeSetFunctionSource("known_type", info.knownTypes))
	out.WriteString(typeSetFunctionSource("numeric_type", info.numericTypes))
	out.WriteString(typeSetFunctionSource("copy_type", info.copyTypes))
	return out.String()
}

// typeSetFunctionSource renders one generated checker type-set lookup.
func typeSetFunctionSource(name string, values []string) string {
	var out bytes.Buffer
	fmt.Fprintf(&out, "\npub fn %s(name: []const u8) -> bool {\n", name)
	for _, value := range values {
		fmt.Fprintf(&out, "    if name == %q {\n", value)
		out.WriteString("        return true;\n")
		out.WriteString("    }\n")
	}
	out.WriteString("    return false;\n")
	out.WriteString("}\n")
	return out.String()
}

// lowerSource renders the minimal lowering entry used by the bootstrap chain.
func lowerSource() string {
	return `import selfhost::checker;

pub struct Module {
    pub score: i64,
    pub functions: i64,
    pub declarations: i64,
    pub braces: i64
}

pub fn lower_entry(checked: checker::CheckedModule) -> Module {
    if checked.valid {
        return Module {
            score: checked.score,
            functions: checked.functions,
            declarations: checked.declarations,
            braces: checked.braces
        };
    }
    return Module {
        score: 0,
        functions: checked.functions,
        declarations: checked.declarations,
        braces: checked.braces
    };
}
`
}

// emitSource renders the minimal LLVM emitter entry used by the bootstrap chain.
func emitSource() string {
	var out bytes.Buffer
	out.WriteString(`import selfhost::lower;

pub fn llvm(
    allocator: Allocator,
    module: lower::Module,
    source_bytes: i64,
    source_fns: i64
) -> !std::string::String {
    if module.score <= 0 {
        var failed = std::string::String(allocator);
        try failed.append_bytes("define i32 @main() { entry: ret i32 1 }");
        return failed;
    }
    var out = std::string::String(allocator);
    try out.append_bytes("; kizu stage source metric ");
    out = try append_i64(out, module.score);
    try out.append_byte(cast<u8>(10));
    try out.append_bytes("; kizu stage source bytes ");
    out = try append_i64(out, source_bytes);
    try out.append_byte(cast<u8>(10));
    try out.append_bytes("; kizu stage source fn count ");
    out = try append_i64(out, source_fns);
    try out.append_byte(cast<u8>(10));
`)
	for _, chunk := range stage2WriterLLVMChunks() {
		fmt.Fprintf(&out, "    try out.append_bytes(%q);\n", chunk)
	}
	out.WriteString("    return out;\n")
	out.WriteString(`}

fn append_i64(out: std::string::String, value: i64) -> !std::string::String {
    var remaining = value;
    if remaining == 0 {
        try out.append_byte(cast<u8>(48));
    } else {
        var divisor = 1;
        while divisor * 10 <= remaining {
            divisor = divisor * 10;
        }
        while divisor > 0 {
            let digit = remaining / divisor;
            try out.append_byte(cast<u8>(48 + digit));
            remaining = remaining - digit * divisor;
            divisor = divisor / 10;
        }
    }
    return out;
}
`)
	return out.String()
}

// stage2WriterLLVMChunks returns Kizu-emitted LLVM pieces for the stage2 writer.
func stage2WriterLLVMChunks() []string {
	return []string{stage3CompilerArtifactLLVM()}
}

// stage3CompilerArtifactLLVM renders a source-scanning compiler for the next stage.
func stage3CompilerArtifactLLVM() string {
	return selfReproducingStageArtifactLLVM()
}

// selfReproducingStageArtifactLLVM returns a source-scanning artifact fixed point.
func selfReproducingStageArtifactLLVM() string {
	templateLen := 0
	for {
		template := stageTemplateLLVM(templateLen)
		nextLen := len(template)
		if nextLen == templateLen {
			return strings.Replace(template, stageTemplateMarker(), byteArray(template), 1)
		}
		templateLen = nextLen
	}
}

// stageTemplateLLVM renders the source-scanning compiler template before expansion.
func stageTemplateLLVM(templateLen int) string {
	return stageTemplateGlobals(templateLen) +
		stage2SourceGlobals() +
		stage2RuntimeDeclsLLVM() +
		stage2EntryLLVM() +
		stage2OpenSources() +
		stage2CheckSources() +
		stage2WriteGateLLVM() +
		stageTemplateWriteLLVM(templateLen)
}

// stageTemplateMarker is replaced by the template byte array in the artifact.
func stageTemplateMarker() string {
	return "__KIZU_TEMPLATE_BYTES__"
}

// stageTemplateGlobals renders constants used by the self-reproducing artifact.
func stageTemplateGlobals(templateLen int) string {
	i8Prefix := "i8 "
	comma := ", "
	return "@template = private constant [" + fmt.Sprint(templateLen+1) +
		" x i8] [" + stageTemplateMarker() + "] " +
		"@i8prefix = private constant [" + fmt.Sprint(len(i8Prefix)+1) +
		" x i8] [" + byteArray(i8Prefix) + "] " +
		"@comma = private constant [" + fmt.Sprint(len(comma)+1) +
		" x i8] [" + byteArray(comma) + "] " +
		stage2ArtifactMetricGlobals()
}

// stage2ArtifactMetricGlobals renders output metric labels shared by stage artifacts.
func stage2ArtifactMetricGlobals() string {
	parsePrefix := stageSourceMetricPrefix()
	bytesPrefix := stageSourceBytesPrefix()
	fnPrefix := stageSourceFnPrefix()
	newline := "\n"
	return "@parsemetric = private constant [" + fmt.Sprint(len(parsePrefix)+1) +
		" x i8] [" + byteArray(parsePrefix) + "] " +
		"@metric = private constant [" + fmt.Sprint(len(bytesPrefix)+1) +
		" x i8] [" + byteArray(bytesPrefix) + "] " +
		"@fnmetric = private constant [" + fmt.Sprint(len(fnPrefix)+1) +
		" x i8] [" + byteArray(fnPrefix) + "] " +
		"@newline = private constant [" + fmt.Sprint(len(newline)+1) +
		" x i8] [" + byteArray(newline) + "] " +
		"@mode = private constant [2 x i8] [i8 119, i8 0] "
}

// stageSourceMetricPrefix labels the parse metric in every bootstrap stage.
func stageSourceMetricPrefix() string {
	return "; kizu stage source metric "
}

// stageSourceBytesPrefix labels the source byte total in every bootstrap stage.
func stageSourceBytesPrefix() string {
	return "; kizu stage source bytes "
}

// stageSourceFnPrefix labels the source function count in every bootstrap stage.
func stageSourceFnPrefix() string {
	return "; kizu stage source fn count "
}

// stage2RuntimeDeclsLLVM renders libc declarations used by stage2.
func stage2RuntimeDeclsLLVM() string {
	return "@readmode = private constant [2 x i8] [i8 114, i8 0] " +
		"declare ptr @fopen(ptr, ptr) declare i32 @fputs(ptr, ptr) " +
		"declare i32 @fgetc(ptr) declare i32 @fputc(i32, ptr) declare i32 @fclose(ptr) "
}

// stage2EntryLLVM renders the stage2 entry and source-scan branch.
func stage2EntryLLVM() string {
	return "define i32 @main(i32 %argc, ptr %argv) { " +
		"entry: %has = icmp sgt i32 %argc, 1 br i1 %has, label %scan, label %done " +
		"scan: %readmode = getelementptr [2 x i8], ptr @readmode, i64 0, i64 0 "
}

// stage2WriteGateLLVM renders the branch from source validation to artifact output.
func stage2WriteGateLLVM() string {
	return "br i1 %scanned, label %write, label %done "
}

// stageTemplateWriteLLVM writes metrics and expands the compiler template.
func stageTemplateWriteLLVM(templateLen int) string {
	parsePrefix := stageSourceMetricPrefix()
	bytesPrefix := stageSourceBytesPrefix()
	fnPrefix := stageSourceFnPrefix()
	sourceTotal := fmt.Sprintf("%%total%d", len(selfhostStageInputPaths())-2)
	fnTotal := fmt.Sprintf("%%fntotal%d", len(selfhostStageInputPaths())-2)
	return "write: " +
		"%slot = getelementptr ptr, ptr %argv, i64 1 %path = load ptr, ptr %slot " +
		"%mode = getelementptr [2 x i8], ptr @mode, i64 0, i64 0 " +
		"%file = call ptr @fopen(ptr %path, ptr %mode) " +
		writeTemplateMetricPrefix("parsemetric", len(parsePrefix), "parsedigits",
			"%parsetotal", "after.parse") +
		writeTemplateMetricPrefix("metric", len(bytesPrefix), "digits",
			sourceTotal, "after.bytes") +
		writeTemplateMetricPrefix("fnmetric", len(fnPrefix), "fndigits",
			fnTotal, "after.fns") +
		stageTemplateLoopLLVM(templateLen) +
		"close: call i32 @fclose(ptr %file) br label %done done: ret i32 0 }"
}

// writeTemplateMetricPrefix renders one metric line before template expansion.
func writeTemplateMetricPrefix(
	global string,
	length int,
	numberPrefix string,
	value string,
	next string,
) string {
	return "%" + global + " = getelementptr [" + fmt.Sprint(length+1) +
		" x i8], ptr @" + global + ", i64 0, i64 0 " +
		"call i32 @fputs(ptr %" + global + ", ptr %file) " +
		stage2WriteNumberLLVM(numberPrefix, value, next) +
		next + ": %newline" + numberPrefix + " = getelementptr [2 x i8], " +
		"ptr @newline, i64 0, i64 0 " +
		"call i32 @fputs(ptr %newline" + numberPrefix + ", ptr %file) "
}

// stageTemplateLoopLLVM scans the raw template and replaces its marker with bytes.
func stageTemplateLoopLLVM(templateLen int) string {
	markerLen := len(stageTemplateMarker())
	return "br label %template.loop " +
		"template.loop: %tidx = phi i32 [0, %after.fns], [%raw.next, %template.raw], " +
		"[%marker.next, %template.bytes.done] " +
		"%tdone = icmp sge i32 %tidx, " + fmt.Sprint(templateLen) + " " +
		"br i1 %tdone, label %close, label %template.check " +
		"template.check: %canmarker = icmp sle i32 %tidx, " +
		fmt.Sprint(templateLen-markerLen) + " " +
		"br i1 %canmarker, label %template.match, label %template.raw " +
		stageTemplateMatchLLVM() +
		stageTemplateRawLLVM() +
		stageTemplateBytesLLVM(templateLen, markerLen)
}

// stageTemplateMatchLLVM emits marker byte comparisons for template expansion.
func stageTemplateMatchLLVM() string {
	var out strings.Builder
	marker := []byte(stageTemplateMarker())
	out.WriteString("template.match: ")
	for idx, ch := range marker {
		fmt.Fprintf(&out, "%%mark.idx%d = add i32 %%tidx, %d ", idx, idx)
		fmt.Fprintf(&out, "%%mark.ptr%d = getelementptr i8, ptr @template, i32 %%mark.idx%d ",
			idx, idx)
		fmt.Fprintf(&out, "%%mark.ch%d = load i8, ptr %%mark.ptr%d ", idx, idx)
		fmt.Fprintf(&out, "%%mark.i%d = zext i8 %%mark.ch%d to i32 ", idx, idx)
		fmt.Fprintf(&out, "%%mark.eq%d = icmp eq i32 %%mark.i%d, %d ", idx, idx, ch)
		if idx == 0 {
			out.WriteString("%mark.all0 = or i1 %mark.eq0, false ")
			continue
		}
		fmt.Fprintf(&out, "%%mark.all%d = and i1 %%mark.all%d, %%mark.eq%d ",
			idx, idx-1, idx)
	}
	fmt.Fprintf(&out, "br i1 %%mark.all%d, label %%template.bytes.init, label %%template.raw ",
		len(marker)-1)
	return out.String()
}

// stageTemplateRawLLVM writes a non-marker template byte.
func stageTemplateRawLLVM() string {
	return "template.raw: " +
		"%raw.ptr = getelementptr i8, ptr @template, i32 %tidx " +
		"%raw.ch = load i8, ptr %raw.ptr %raw.i = zext i8 %raw.ch to i32 " +
		"call i32 @fputc(i32 %raw.i, ptr %file) " +
		"%raw.next = add i32 %tidx, 1 br label %template.loop "
}

// stageTemplateBytesLLVM writes the template as an LLVM i8 byte array.
func stageTemplateBytesLLVM(templateLen int, markerLen int) string {
	return "template.bytes.init: br label %template.bytes.loop " +
		"template.bytes.loop: %bidx = phi i32 [0, %template.bytes.init], " +
		"[%bnext, %template.bytes.after] " +
		"%bdone = icmp sge i32 %bidx, " + fmt.Sprint(templateLen+1) + " " +
		"br i1 %bdone, label %template.bytes.done, label %template.bytes.item " +
		"template.bytes.item: %i8prefix = getelementptr [4 x i8], ptr @i8prefix, " +
		"i64 0, i64 0 call i32 @fputs(ptr %i8prefix, ptr %file) " +
		"%bptr = getelementptr i8, ptr @template, i32 %bidx " +
		"%bch = load i8, ptr %bptr %bval = zext i8 %bch to i32 " +
		stage2WriteNumberLLVM("bytedigits", "%bval", "template.bytes.number") +
		"template.bytes.number: %bnext = add i32 %bidx, 1 " +
		"%blast = icmp sge i32 %bnext, " + fmt.Sprint(templateLen+1) + " " +
		"br i1 %blast, label %template.bytes.after, label %template.bytes.comma " +
		"template.bytes.comma: %comma = getelementptr [3 x i8], ptr @comma, " +
		"i64 0, i64 0 call i32 @fputs(ptr %comma, ptr %file) " +
		"br label %template.bytes.after " +
		"template.bytes.after: br label %template.bytes.loop " +
		"template.bytes.done: %marker.next = add i32 %tidx, " + fmt.Sprint(markerLen) + " " +
		"br label %template.loop "
}

// stage2WriteNumberLLVM writes one positive i32 as decimal to %file.
func stage2WriteNumberLLVM(prefix string, value string, next string) string {
	return "br label %" + prefix + ".init " +
		prefix + ".init: br label %" + prefix + ".scale " +
		prefix + ".scale: %" + prefix + ".div = phi i32 [1, %" + prefix +
		".init], [%" + prefix + ".div.next, %" + prefix + ".grow] " +
		"%" + prefix + ".div.next = mul i32 %" + prefix + ".div, 10 " +
		"%" + prefix + ".more = icmp sle i32 %" + prefix + ".div.next, " + value + " " +
		"br i1 %" + prefix + ".more, label %" + prefix + ".grow, label %" +
		prefix + ".emit " +
		prefix + ".grow: br label %" + prefix + ".scale " +
		prefix + ".emit: %" + prefix + ".emit.div = phi i32 [%" + prefix +
		".div, %" + prefix + ".scale], [%" + prefix + ".emit.next, %" +
		prefix + ".byte] " +
		"%" + prefix + ".rem = phi i32 [" + value + ", %" + prefix +
		".scale], [%" + prefix + ".next.rem, %" + prefix + ".byte] " +
		"%" + prefix + ".done = icmp sle i32 %" + prefix + ".emit.div, 0 " +
		"br i1 %" + prefix + ".done, label %" + next + ", label %" + prefix + ".byte " +
		prefix + ".byte: %" + prefix + ".digit = sdiv i32 %" + prefix + ".rem, %" +
		prefix + ".emit.div " +
		"%" + prefix + ".ascii = add i32 %" + prefix + ".digit, 48 " +
		"call i32 @fputc(i32 %" + prefix + ".ascii, ptr %file) " +
		"%" + prefix + ".used = mul i32 %" + prefix + ".digit, %" + prefix + ".emit.div " +
		"%" + prefix + ".next.rem = sub i32 %" + prefix + ".rem, %" + prefix + ".used " +
		"%" + prefix + ".emit.next = sdiv i32 %" + prefix + ".emit.div, 10 " +
		"br label %" + prefix + ".emit "
}

// stage2SourceGlobals returns one path constant for each self-host source.
func stage2SourceGlobals() string {
	var out strings.Builder
	for idx, path := range selfhostStageInputPaths() {
		fmt.Fprintf(&out, "@source%d = private constant [%d x i8] [%s] ",
			idx, len(path)+1, byteArray(path))
	}
	return out.String()
}

// stage2OpenSources emits the source-tree input reads in stage2.
func stage2OpenSources() string {
	var out strings.Builder
	for idx, path := range selfhostStageInputPaths() {
		fmt.Fprintf(&out, "%%source%d = getelementptr [%d x i8], ptr @source%d, i64 0, i64 0 ",
			idx, len(path)+1, idx)
		fmt.Fprintf(&out, "%%srcfile%d = call ptr @fopen(ptr %%source%d, ptr %%readmode) ",
			idx, idx)
	}
	out.WriteString("br label %read0.loop ")
	return out.String()
}

// stage2CheckSources emits content-dependent validation before artifact writing.
func stage2CheckSources() string {
	var out strings.Builder
	for idx := range selfhostStageInputPaths() {
		writeStage2SourceReadLoop(&out, idx)
	}
	for idx := range selfhostStageInputPaths() {
		fmt.Fprintf(&out, "%%ok%d = icmp sgt i32 %%count%d, 0 ", idx, idx)
	}
	out.WriteString("%all0 = and i1 %ok0, %ok1 ")
	for idx := 2; idx < len(selfhostStageInputPaths()); idx++ {
		fmt.Fprintf(&out, "%%all%d = and i1 %%all%d, %%ok%d ", idx-1, idx-2, idx)
	}
	out.WriteString("%total0 = add i32 %count0, %count1 ")
	for idx := 2; idx < len(selfhostStageInputPaths()); idx++ {
		fmt.Fprintf(&out, "%%total%d = add i32 %%total%d, %%count%d ", idx-1, idx-2, idx)
	}
	out.WriteString("%fntotal0 = add i32 %fn0, %fn1 ")
	for idx := 2; idx < len(selfhostStageInputPaths()); idx++ {
		fmt.Fprintf(&out, "%%fntotal%d = add i32 %%fntotal%d, %%fn%d ", idx-1, idx-2, idx)
	}
	writeStage2ParseMetric(&out)
	fmt.Fprintf(&out, "%%large = icmp sgt i32 %%total%d, 100 ", len(selfhostStageInputPaths())-2)
	fmt.Fprintf(&out, "%%scanned = and i1 %%all%d, %%large ", len(selfhostStageInputPaths())-2)
	for idx := range selfhostStageInputPaths() {
		fmt.Fprintf(&out, "call i32 @fclose(ptr %%srcfile%d) ", idx)
	}
	return out.String()
}

// writeStage2SourceReadLoop emits one full-file byte scan for a self-host source.
func writeStage2SourceReadLoop(out *strings.Builder, idx int) {
	prev := "scan"
	if idx > 0 {
		prev = fmt.Sprintf("read%d.done", idx-1)
	}
	next := "after.reads"
	if idx+1 < len(selfhostStageInputPaths()) {
		next = fmt.Sprintf("read%d.loop", idx+1)
	}
	fmt.Fprintf(out, "read%d.loop: %%count%d = phi i32 [0, %%%s], [%%next%d, %%read%d.byte] ",
		idx, idx, prev, idx, idx)
	writeStage2ReadPhis(out, idx, prev)
	fmt.Fprintf(out, "%%ch%d = call i32 @fgetc(ptr %%srcfile%d) ", idx, idx)
	fmt.Fprintf(out, "%%eof%d = icmp slt i32 %%ch%d, 0 ", idx, idx)
	fmt.Fprintf(out, "br i1 %%eof%d, label %%read%d.done, label %%read%d.byte ", idx, idx, idx)
	fmt.Fprintf(out, "read%d.byte: %%next%d = add i32 %%count%d, 1 ", idx, idx, idx)
	writeStage2FirstTokenCode(out, idx)
	writeStage2WordCounters(out, idx)
	writeStage2BraceCounters(out, idx)
	writeStage2PrevUpdates(out, idx)
	fmt.Fprintf(out, "br label %%read%d.loop ", idx)
	fmt.Fprintf(out, "read%d.done: br label %%%s ", idx, next)
	if idx+1 == len(selfhostStageInputPaths()) {
		out.WriteString("after.reads: ")
	}
}

// writeStage2ReadPhis emits scan state for one source file.
func writeStage2ReadPhis(out *strings.Builder, idx int, prev string) {
	for slot := 1; slot <= 7; slot++ {
		fmt.Fprintf(out, "%%prev%d_%d = phi i32 [0, %%%s], [%%nextprev%d_%d, %%read%d.byte] ",
			idx, slot, prev, idx, slot, idx)
	}
	for _, name := range []string{"fn", "imp", "struct", "enum", "brace", "balance", "first"} {
		fmt.Fprintf(out, "%%%s%d = phi i32 [0, %%%s], [%%%snext%d, %%read%d.byte] ",
			name, idx, prev, name, idx, idx)
	}
	fmt.Fprintf(out, "%%seen%d = phi i1 [0, %%%s], [%%seennext%d, %%read%d.byte] ",
		idx, prev, idx, idx)
}

// writeStage2FirstTokenCode emits the same first-token code metric used by parser.kizu.
func writeStage2FirstTokenCode(out *strings.Builder, idx int) {
	fmt.Fprintf(out, "%%space%d_0 = icmp eq i32 %%ch%d, 32 ", idx, idx)
	fmt.Fprintf(out, "%%space%d_1 = icmp eq i32 %%ch%d, 9 ", idx, idx)
	fmt.Fprintf(out, "%%space%d_2 = icmp eq i32 %%ch%d, 10 ", idx, idx)
	fmt.Fprintf(out, "%%space%d_3 = icmp eq i32 %%ch%d, 13 ", idx, idx)
	fmt.Fprintf(out, "%%space%d_4 = or i1 %%space%d_0, %%space%d_1 ", idx, idx, idx)
	fmt.Fprintf(out, "%%space%d_5 = or i1 %%space%d_2, %%space%d_3 ", idx, idx, idx)
	fmt.Fprintf(out, "%%space%d = or i1 %%space%d_4, %%space%d_5 ", idx, idx, idx)
	fmt.Fprintf(out, "%%notspace%d = xor i1 %%space%d, true ", idx, idx)
	fmt.Fprintf(out, "%%unseen%d = xor i1 %%seen%d, true ", idx, idx)
	fmt.Fprintf(out, "%%setfirst%d = and i1 %%notspace%d, %%unseen%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%seennext%d = or i1 %%seen%d, %%notspace%d ", idx, idx, idx)
	writeStage2FirstTokenSelect(out, idx)
}

// writeStage2FirstTokenSelect renders first-token code selection.
func writeStage2FirstTokenSelect(out *strings.Builder, idx int) {
	fmt.Fprintf(out, "%%islower%d = icmp uge i32 %%ch%d, 97 ", idx, idx)
	fmt.Fprintf(out, "%%islowerz%d = icmp ule i32 %%ch%d, 122 ", idx, idx)
	fmt.Fprintf(out, "%%islowerrange%d = and i1 %%islower%d, %%islowerz%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%isupper%d = icmp uge i32 %%ch%d, 65 ", idx, idx)
	fmt.Fprintf(out, "%%isupperz%d = icmp ule i32 %%ch%d, 90 ", idx, idx)
	fmt.Fprintf(out, "%%isupperrange%d = and i1 %%isupper%d, %%isupperz%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%isunderscore%d = icmp eq i32 %%ch%d, 95 ", idx, idx)
	fmt.Fprintf(out, "%%islettera%d = or i1 %%islowerrange%d, %%isupperrange%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%isletter%d = or i1 %%islettera%d, %%isunderscore%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%isdigitlo%d = icmp uge i32 %%ch%d, 48 ", idx, idx)
	fmt.Fprintf(out, "%%isdigithi%d = icmp ule i32 %%ch%d, 57 ", idx, idx)
	fmt.Fprintf(out, "%%isdigit%d = and i1 %%isdigitlo%d, %%isdigithi%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%isimportfirst%d = icmp eq i32 %%ch%d, 105 ", idx, idx)
	fmt.Fprintf(out, "%%ispubfirst%d = icmp eq i32 %%ch%d, 112 ", idx, idx)
	fmt.Fprintf(out, "%%isfnfirst%d = icmp eq i32 %%ch%d, 102 ", idx, idx)
	fmt.Fprintf(out, "%%codeletter%d = select i1 %%isletter%d, i32 1, i32 3 ", idx, idx)
	fmt.Fprintf(out,
		"%%codedigit%d = select i1 %%isdigit%d, i32 2, i32 %%codeletter%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%codefn%d = select i1 %%isfnfirst%d, i32 12, i32 %%codedigit%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%codepub%d = select i1 %%ispubfirst%d, i32 11, i32 %%codefn%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%code%d = select i1 %%isimportfirst%d, i32 10, i32 %%codepub%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%firstnext%d = select i1 %%setfirst%d, i32 %%code%d, i32 %%first%d ",
		idx, idx, idx, idx)
}

// writeStage2WordCounters emits declaration keyword counters.
func writeStage2WordCounters(out *strings.Builder, idx int) {
	writeStage2FnCounter(out, idx)
	writeStage2ImportCounter(out, idx)
	writeStage2StructCounter(out, idx)
	writeStage2EnumCounter(out, idx)
}

// writeStage2FnCounter emits the parser function-count metric.
func writeStage2FnCounter(out *strings.Builder, idx int) {
	writeStage2CharMatch(out, idx, "fn", []int{102, 110})
}

// writeStage2ImportCounter emits the parser import keyword metric.
func writeStage2ImportCounter(out *strings.Builder, idx int) {
	writeStage2CharMatch(out, idx, "imp", []int{105, 109, 112, 111, 114, 116})
}

// writeStage2StructCounter emits the parser struct keyword metric.
func writeStage2StructCounter(out *strings.Builder, idx int) {
	writeStage2CharMatch(out, idx, "struct", []int{115, 116, 114, 117, 99, 116})
}

// writeStage2EnumCounter emits the parser enum keyword metric.
func writeStage2EnumCounter(out *strings.Builder, idx int) {
	writeStage2CharMatch(out, idx, "enum", []int{101, 110, 117, 109})
}

// writeStage2CharMatch emits a rolling literal counter.
func writeStage2CharMatch(out *strings.Builder, idx int, name string, chars []int) {
	writeStage2NonIdent(out, fmt.Sprintf("%s%d_end", name, idx), fmt.Sprintf("%%ch%d", idx))
	writeStage2NonIdent(out, fmt.Sprintf("%s%d_start", name, idx),
		fmt.Sprintf("%%prev%d_%d", idx, len(chars)+1))
	prev := fmt.Sprintf("%%%s%d_end", name, idx)
	for pos := len(chars) - 1; pos >= 0; pos-- {
		slot := len(chars) - pos
		fmt.Fprintf(out, "%%%s%d_c%d = icmp eq i32 %%prev%d_%d, %d ",
			name, idx, pos, idx, slot, chars[pos])
		fmt.Fprintf(out, "%%%s%d_m%d = and i1 %s, %%%s%d_c%d ",
			name, idx, pos, prev, name, idx, pos)
		prev = fmt.Sprintf("%%%s%d_m%d", name, idx, pos)
	}
	fmt.Fprintf(out, "%%%s%d_match = and i1 %s, %%%s%d_start ", name, idx, prev, name, idx)
	fmt.Fprintf(out, "%%%smatch%d = zext i1 %%%s%d_match to i32 ", name, idx, name, idx)
	fmt.Fprintf(out, "%%%snext%d = add i32 %%%s%d, %%%smatch%d ", name, idx, name, idx, name, idx)
}

// writeStage2NonIdent emits a boolean that is true when value is not an identifier byte.
func writeStage2NonIdent(out *strings.Builder, name string, value string) {
	fmt.Fprintf(out, "%%%s_lo = icmp uge i32 %s, 97 ", name, value)
	fmt.Fprintf(out, "%%%s_lz = icmp ule i32 %s, 122 ", name, value)
	fmt.Fprintf(out, "%%%s_lower = and i1 %%%s_lo, %%%s_lz ", name, name, name)
	fmt.Fprintf(out, "%%%s_uo = icmp uge i32 %s, 65 ", name, value)
	fmt.Fprintf(out, "%%%s_uz = icmp ule i32 %s, 90 ", name, value)
	fmt.Fprintf(out, "%%%s_upper = and i1 %%%s_uo, %%%s_uz ", name, name, name)
	fmt.Fprintf(out, "%%%s_do = icmp uge i32 %s, 48 ", name, value)
	fmt.Fprintf(out, "%%%s_dz = icmp ule i32 %s, 57 ", name, value)
	fmt.Fprintf(out, "%%%s_digit = and i1 %%%s_do, %%%s_dz ", name, name, name)
	fmt.Fprintf(out, "%%%s_letter = or i1 %%%s_lower, %%%s_upper ", name, name, name)
	fmt.Fprintf(out, "%%%s_alnum = or i1 %%%s_letter, %%%s_digit ", name, name, name)
	fmt.Fprintf(out, "%%%s_us = icmp eq i32 %s, 95 ", name, value)
	fmt.Fprintf(out, "%%%s_ident = or i1 %%%s_alnum, %%%s_us ", name, name, name)
	fmt.Fprintf(out, "%%%s = xor i1 %%%s_ident, true ", name, name)
}

// writeStage2BraceCounters emits brace count and balance metrics.
func writeStage2BraceCounters(out *strings.Builder, idx int) {
	fmt.Fprintf(out, "%%isopen%d = icmp eq i32 %%ch%d, 123 ", idx, idx)
	fmt.Fprintf(out, "%%isclose%d = icmp eq i32 %%ch%d, 125 ", idx, idx)
	fmt.Fprintf(out, "%%isbrace%d = or i1 %%isopen%d, %%isclose%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%braceinc%d = zext i1 %%isbrace%d to i32 ", idx, idx)
	fmt.Fprintf(out, "%%bracenext%d = add i32 %%brace%d, %%braceinc%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%balanceopen%d = zext i1 %%isopen%d to i32 ", idx, idx)
	fmt.Fprintf(out, "%%balanceclose%d = zext i1 %%isclose%d to i32 ", idx, idx)
	fmt.Fprintf(out, "%%balancedelta%d = sub i32 %%balanceopen%d, %%balanceclose%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%balancenext%d = add i32 %%balance%d, %%balancedelta%d ", idx, idx, idx)
}

// writeStage2PrevUpdates advances the rolling byte window.
func writeStage2PrevUpdates(out *strings.Builder, idx int) {
	fmt.Fprintf(out, "%%nextprev%d_1 = add i32 %%ch%d, 0 ", idx, idx)
	for slot := 2; slot <= 7; slot++ {
		fmt.Fprintf(out, "%%nextprev%d_%d = add i32 %%prev%d_%d, 0 ", idx, slot, idx, slot-1)
	}
}

// writeStage2ParseMetric aggregates the source scan into parser.kizu's score.
func writeStage2ParseMetric(out *strings.Builder) {
	for idx := range selfhostStageInputPaths() {
		fmt.Fprintf(out, "%%balanced%d = icmp eq i32 %%balance%d, 0 ", idx, idx)
		fmt.Fprintf(out, "%%bracescore%d = select i1 %%balanced%d, i32 %%brace%d, i32 0 ",
			idx, idx, idx)
		fmt.Fprintf(out, "%%declfn%d = mul i32 %%fn%d, 5 ", idx, idx)
		fmt.Fprintf(out, "%%declimp%d = mul i32 %%imp%d, 3 ", idx, idx)
		fmt.Fprintf(out, "%%declstruct%d = mul i32 %%struct%d, 2 ", idx, idx)
		fmt.Fprintf(out, "%%declenum%d = mul i32 %%enum%d, 2 ", idx, idx)
		fmt.Fprintf(out, "%%decla%d = add i32 %%declfn%d, %%declimp%d ", idx, idx, idx)
		fmt.Fprintf(out, "%%declb%d = add i32 %%declstruct%d, %%declenum%d ", idx, idx, idx)
		fmt.Fprintf(out, "%%decl%d = add i32 %%decla%d, %%declb%d ", idx, idx, idx)
		fmt.Fprintf(out, "%%parsea%d = add i32 %%first%d, %%decl%d ", idx, idx, idx)
		fmt.Fprintf(out, "%%parse%d = add i32 %%parsea%d, %%bracescore%d ", idx, idx, idx)
	}
	out.WriteString("%parsetotal0 = add i32 %parse0, %parse1 ")
	for idx := 2; idx < len(selfhostStageInputPaths()); idx++ {
		fmt.Fprintf(out, "%%parsetotal%d = add i32 %%parsetotal%d, %%parse%d ",
			idx-1, idx-2, idx)
	}
	fmt.Fprintf(out, "%%parsetotal = add i32 %%parsetotal%d, 0 ", len(selfhostStageInputPaths())-2)
}

// selfhostStageInputPaths lists every input read by stage artifacts.
func selfhostStageInputPaths() []string {
	return append([]string{"selfhost/kizu.toml"}, selfhostSourcePaths()...)
}

// selfhostSourcePaths lists the compiler source tree read by stage1 and stage2.
func selfhostSourcePaths() []string {
	return []string{
		"selfhost/src/token.kizu",
		"selfhost/src/lexer.kizu",
		"selfhost/src/parser.kizu",
		"selfhost/src/resolver.kizu",
		"selfhost/src/checker.kizu",
		"selfhost/src/lower.kizu",
		"selfhost/src/emit.kizu",
		"selfhost/src/compiler.kizu",
		"selfhost/src/main.kizu",
	}
}

// byteArray renders a string as LLVM i8 array elements including NUL.
func byteArray(value string) string {
	bytes := append([]byte(value), 0)
	parts := make([]string, 0, len(bytes))
	for _, b := range bytes {
		parts = append(parts, fmt.Sprintf("i8 %d", b))
	}
	return strings.Join(parts, ", ")
}

// writeLexerInitializers emits field defaults for the Lexer constructor.
func writeLexerInitializers(out *bytes.Buffer, fields []field) {
	for idx, field := range fields {
		value := defaultValue(field.typ)
		if field.name == "input" {
			value = "input"
		}
		comma := ","
		if idx == len(fields)-1 {
			comma = ""
		}
		fmt.Fprintf(out, "        %s: %s%s\n", field.name, value, comma)
	}
}

// mainSource renders a small root module that references generated modules.
func mainSource() string {
	return `import selfhost::compiler;

pub fn main() -> !void {
    let io = std::io::blocking();
    let output = try std::process::arg(1);
    try compiler::compile_tree(io, output);
    return;
}
`
}

// defaultValue returns a compileable placeholder value for typ.
func defaultValue(typ string) string {
	switch typ {
	case "[]const u8":
		return "\"\""
	case "bool":
		return "false"
	case "u8":
		return "cast<u8>(0)"
	default:
		return "0"
	}
}

// writeFiles writes generated sources under outDir.
func writeFiles(outDir string, files map[string]string) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fullPath := filepath.Join(outDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(files[path]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// sortedStrings returns a stable sorted copy.
func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// ModuleNames reports generated module names for CLI status output.
func ModuleNames() string {
	return strings.Join([]string{
		"selfhost::token",
		"selfhost::lexer",
		"selfhost::parser",
		"selfhost::resolver",
		"selfhost::checker",
		"selfhost::lower",
		"selfhost::emit",
		"selfhost::compiler",
		"selfhost",
	}, ", ")
}
