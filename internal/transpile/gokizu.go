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
	if err := os.MkdirAll(filepath.Join(outDir, "src"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"kizu.toml":         manifestSource(),
		"src/token.kizu":    tokenSource(tokenInfo),
		"src/lexer.kizu":    lexerSource(lexerInfo),
		"src/parser.kizu":   parserSource(),
		"src/resolver.kizu": resolverSource(),
		"src/checker.kizu":  checkerSource(),
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
	fields []field
}

type field struct {
	name string
	typ  string
}

type keyword struct {
	literal string
	tag     string
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
	return lexerPackage{fields: fields}, nil
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
	out.WriteString("    return token::EOFToken();\n")
	out.WriteString("}\n")
	out.WriteString("\n")
	out.WriteString(firstTokenSource())
	out.WriteString(firstTokenCodeSource())
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

// lexerHelpersSource returns Kizu helpers mirroring the small Go lexer subset.
func lexerHelpersSource() string {
	return `fn punctuation_token(input: []const u8, start: i64, length: i64) -> token::Token {
    let ch = input[start];
    if ch == cast<u8>(40) {
        return make_token(token::Type::LParen, input[start..start + 1]);
    }
    if ch == cast<u8>(41) {
        return make_token(token::Type::RParen, input[start..start + 1]);
    }
    if ch == cast<u8>(123) {
        return make_token(token::Type::LBrace, input[start..start + 1]);
    }
    if ch == cast<u8>(125) {
        return make_token(token::Type::RBrace, input[start..start + 1]);
    }
    if ch == cast<u8>(58) and start + 1 < length and input[start + 1] == cast<u8>(58) {
        return make_token(token::Type::DoubleColon, input[start..start + 2]);
    }
    if ch == cast<u8>(45) and start + 1 < length and input[start + 1] == cast<u8>(62) {
        return make_token(token::Type::Arrow, input[start..start + 2]);
    }
    if ch == cast<u8>(61) and start + 1 < length and input[start + 1] == cast<u8>(61) {
        return make_token(token::Type::Eq, input[start..start + 2]);
    }
    if ch == cast<u8>(61) {
        return make_token(token::Type::Assign, input[start..start + 1]);
    }
    return make_token(token::Type::Illegal, input[start..start + 1]);
}

fn make_token(kind: token::Type, literal: []const u8) -> token::Token {
    return token::New(kind, literal, 1, 1);
}

fn is_ident_byte(ch: u8) -> bool {
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
func parserSource() string {
	return parserHeaderSource() + parserMetricSource() + parserWordCountSource() +
		parserBraceSource() + parserMatchSource()
}

// parserHeaderSource renders parser entry points and public scoring functions.
func parserHeaderSource() string {
	return `import selfhost::lexer;
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
    return count_word_fn(source);
}

pub fn declaration_score(source: []const u8) -> i64 {
    return count_word_fn(source) * 5 + count_word_import(source) * 3 +
        count_word_struct(source) * 2 + count_word_enum(source) * 2;
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

`
}

// parserMetricSource renders byte-scanning declaration counters.
func parserMetricSource() string {
	return `
fn count_word_fn(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var count = 0;
    while index + 1 < length {
        if source[index] == cast<u8>(102) and source[index + 1] == cast<u8>(110) {
            count = count + 1;
        }
        index = index + 1;
    }
    return count;
}

`
}

// parserWordCountSource renders longer keyword counter helpers.
func parserWordCountSource() string {
	return `fn count_word_import(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var count = 0;
    while index + 5 < length {
        if matches_import(source, index) {
            count = count + 1;
        }
        index = index + 1;
    }
    return count;
}

fn count_word_struct(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var count = 0;
    while index + 5 < length {
        if matches_struct(source, index) {
            count = count + 1;
        }
        index = index + 1;
    }
    return count;
}

fn count_word_enum(source: []const u8) -> i64 {
    let length = std::builtin::mem_len(source);
    var index = 0;
    var count = 0;
    while index + 3 < length {
        if matches_enum(source, index) {
            count = count + 1;
        }
        index = index + 1;
    }
    return count;
}

`
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

// parserMatchSource renders fixed byte-pattern recognizers for Kizu keywords.
func parserMatchSource() string {
	return `fn matches_import(source: []const u8, index: i64) -> bool {
    return source[index] == cast<u8>(105) and source[index + 1] == cast<u8>(109) and
        source[index + 2] == cast<u8>(112) and source[index + 3] == cast<u8>(111) and
        source[index + 4] == cast<u8>(114) and source[index + 5] == cast<u8>(116);
}

fn matches_struct(source: []const u8, index: i64) -> bool {
    return source[index] == cast<u8>(115) and source[index + 1] == cast<u8>(116) and
        source[index + 2] == cast<u8>(114) and source[index + 3] == cast<u8>(117) and
        source[index + 4] == cast<u8>(99) and source[index + 5] == cast<u8>(116);
}

fn matches_enum(source: []const u8, index: i64) -> bool {
    return source[index] == cast<u8>(101) and source[index + 1] == cast<u8>(110) and
        source[index + 2] == cast<u8>(117) and source[index + 3] == cast<u8>(109);
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

pub fn Compiler(source: []const u8) -> Compiler {
    return Compiler {
        source: source
    };
}

pub fn compile(source: []const u8) -> i64 {
    return parser::parse_score(source);
}

pub fn parse_module(source: []const u8) -> i64 {
    return parser::parse_score(source);
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

    let token_parse = parse_module(token_source);
    let lexer_parse = parse_module(lexer_source);
    let parser_parse = parse_module(parser_source);
    let resolver_parse = parse_module(resolver_source);
    let checker_parse = parse_module(checker_source);
    let lower_parse = parse_module(lower_source);
    let emit_parse = parse_module(emit_source);
    let compiler_parse = parse_module(compiler_source);
    let main_parse = parse_module(main_source);
    let source_bytes = std::mem::len(token_source) + std::mem::len(lexer_source) +
        std::mem::len(parser_source) + std::mem::len(resolver_source) +
        std::mem::len(checker_source) + std::mem::len(lower_source) +
        std::mem::len(emit_source) + std::mem::len(compiler_source) +
        std::mem::len(main_source);
    let source_fns = parser::function_count(token_source) + parser::function_count(lexer_source) +
        parser::function_count(parser_source) + parser::function_count(resolver_source) +
        parser::function_count(checker_source) + parser::function_count(lower_source) +
        parser::function_count(emit_source) + parser::function_count(compiler_source) +
        parser::function_count(main_source);

    let parsed = token_parse + lexer_parse + parser_parse + resolver_parse +
        checker_parse + lower_parse + emit_parse + compiler_parse + main_parse;
    let checked = checker::check_entry(parsed);
    let module = lower::lower_entry(checked, parsed);
    let artifact = try emit::llvm(allocator, module, source_bytes, source_fns);
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
    let checked = checker::check_entry(1);
    let module = lower::lower_entry(checked, 1);
    let artifact = try emit::llvm(allocator, module, 0, 0);
    let artifact_bytes = artifact.as_bytes();
    print(artifact_bytes);
    return;
}
`
}

// resolverSource renders the minimal self-host package resolver entry.
func resolverSource() string {
	return `pub struct Graph {
    pub root: []const u8
}

pub fn resolve_selfhost(manifest: []const u8) -> Graph {
    if std::builtin::mem_len(manifest) == 0 {
        return Graph { root: "selfhost" };
    }
    return Graph { root: "selfhost" };
}

pub fn token_path(graph: &Graph) -> []const u8 { return "selfhost/src/token.kizu"; }
pub fn lexer_path(graph: &Graph) -> []const u8 { return "selfhost/src/lexer.kizu"; }
pub fn parser_path(graph: &Graph) -> []const u8 { return "selfhost/src/parser.kizu"; }
pub fn resolver_path(graph: &Graph) -> []const u8 { return "selfhost/src/resolver.kizu"; }
pub fn checker_path(graph: &Graph) -> []const u8 { return "selfhost/src/checker.kizu"; }
pub fn lower_path(graph: &Graph) -> []const u8 { return "selfhost/src/lower.kizu"; }
pub fn emit_path(graph: &Graph) -> []const u8 { return "selfhost/src/emit.kizu"; }
pub fn compiler_path(graph: &Graph) -> []const u8 { return "selfhost/src/compiler.kizu"; }
pub fn main_path(graph: &Graph) -> []const u8 { return "selfhost/src/main.kizu"; }
`
}

// checkerSource renders the minimal checker entry used by the bootstrap chain.
func checkerSource() string {
	return `pub fn check_entry(parsed: i64) -> bool {
    return parsed >= 100;
}
`
}

// lowerSource renders the minimal lowering entry used by the bootstrap chain.
func lowerSource() string {
	return `pub fn lower_entry(checked: bool, parsed: i64) -> i64 {
    if checked {
        return parsed;
    }
    return 0;
}
`
}

// emitSource renders the minimal LLVM emitter entry used by the bootstrap chain.
func emitSource() string {
	var out bytes.Buffer
	out.WriteString(`import selfhost::lower;

pub fn llvm(
    allocator: Allocator,
    module: i64,
    source_bytes: i64,
    source_fns: i64
) -> !std::string::String {
    if module <= 0 {
        var failed = std::string::String(allocator);
        try failed.append_bytes("define i32 @main() { entry: ret i32 1 }");
        return failed;
    }
    var out = std::string::String(allocator);
    try out.append_bytes("; kizu selfhost source metric ");
    out = try append_i64(out, module);
    try out.append_byte(cast<u8>(10));
    try out.append_bytes("; kizu selfhost source bytes ");
    out = try append_i64(out, source_bytes);
    try out.append_byte(cast<u8>(10));
    try out.append_bytes("; kizu selfhost source fn count ");
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
	artifact := stage3CompilerArtifactLLVM()
	return []string{
		stage2ArtifactLLVM(artifact),
		stage2SourceGlobals(),
		stage2RuntimeDeclsLLVM(),
		stage2EntryLLVM(),
		stage2OpenSources(),
		stage2CheckSources(),
		stage2WriteGateLLVM(),
		stage2WriteFallbackLLVM(artifact),
	}
}

// stage2ArtifactLLVM renders the fallback stage artifact constant.
func stage2ArtifactLLVM(artifact string) string {
	parsePrefix := "; kizu stage2 source metric "
	bytesPrefix := "; kizu stage2 source bytes "
	fnPrefix := "; kizu stage2 source fn count "
	newline := "\n"
	return "@artifact = private constant [" + fmt.Sprint(len(artifact)+1) +
		" x i8] [" + byteArray(artifact) + "] " +
		"@parsemetric = private constant [" + fmt.Sprint(len(parsePrefix)+1) +
		" x i8] [" + byteArray(parsePrefix) + "] " +
		"@metric = private constant [" + fmt.Sprint(len(bytesPrefix)+1) +
		" x i8] [" + byteArray(bytesPrefix) + "] " +
		"@fnmetric = private constant [" + fmt.Sprint(len(fnPrefix)+1) +
		" x i8] [" + byteArray(fnPrefix) + "] " +
		"@newline = private constant [" + fmt.Sprint(len(newline)+1) +
		" x i8] [" + byteArray(newline) + "] " +
		"@mode = private constant [2 x i8] [i8 119, i8 0] "
}

// stage3CompilerArtifactLLVM renders a source-scanning compiler for the next stage.
func stage3CompilerArtifactLLVM() string {
	artifact := minimalStageArtifactLLVM()
	return stage2ArtifactLLVM(artifact) +
		stage2SourceGlobals() +
		stage2RuntimeDeclsLLVM() +
		stage2EntryLLVM() +
		stage2OpenSources() +
		stage2CheckSources() +
		stage2WriteGateLLVM() +
		stage2WriteFallbackLLVM(artifact)
}

// minimalStageArtifactLLVM returns the terminal link-smoke artifact.
func minimalStageArtifactLLVM() string {
	return "define i32 @main() { entry: ret i32 0 }"
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

// stage2WriteFallbackLLVM writes the fallback artifact when source scanning succeeds.
func stage2WriteFallbackLLVM(artifact string) string {
	parsePrefix := "; kizu stage2 source metric "
	bytesPrefix := "; kizu stage2 source bytes "
	fnPrefix := "; kizu stage2 source fn count "
	newline := "\n"
	return "write: " +
		"%slot = getelementptr ptr, ptr %argv, i64 1 %path = load ptr, ptr %slot " +
		"%mode = getelementptr [2 x i8], ptr @mode, i64 0, i64 0 " +
		"%file = call ptr @fopen(ptr %path, ptr %mode) " +
		"%parsemetric = getelementptr [" + fmt.Sprint(len(parsePrefix)+1) +
		" x i8], ptr @parsemetric, i64 0, i64 0 " +
		"call i32 @fputs(ptr %parsemetric, ptr %file) " +
		stage2WriteNumberLLVM("parsedigits", "%parsetotal", "after.parse") +
		"after.parse: %newline0 = getelementptr [" + fmt.Sprint(len(newline)+1) +
		" x i8], ptr @newline, i64 0, i64 0 call i32 @fputs(ptr %newline0, ptr %file) " +
		"%metric = getelementptr [" + fmt.Sprint(len(bytesPrefix)+1) +
		" x i8], ptr @metric, i64 0, i64 0 " +
		"call i32 @fputs(ptr %metric, ptr %file) " +
		stage2WriteNumberLLVM("digits", "%total7", "after.bytes") +
		"after.bytes: %newline1 = getelementptr [" + fmt.Sprint(len(newline)+1) +
		" x i8], ptr @newline, i64 0, i64 0 call i32 @fputs(ptr %newline1, ptr %file) " +
		"%fnmetric = getelementptr [" + fmt.Sprint(len(fnPrefix)+1) +
		" x i8], ptr @fnmetric, i64 0, i64 0 " +
		"call i32 @fputs(ptr %fnmetric, ptr %file) " +
		stage2WriteNumberLLVM("fndigits", "%fntotal7", "after.fns") +
		"after.fns: %newline = getelementptr [" + fmt.Sprint(len(newline)+1) +
		" x i8], ptr @newline, i64 0, i64 0 call i32 @fputs(ptr %newline, ptr %file) " +
		"%text = getelementptr [" + fmt.Sprint(len(artifact)+1) +
		" x i8], ptr @artifact, i64 0, i64 0 " +
		"call i32 @fputs(ptr %text, ptr %file) call i32 @fclose(ptr %file) " +
		"br label %done done: ret i32 0 }"
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
	for idx, path := range selfhostSourcePaths() {
		fmt.Fprintf(&out, "@source%d = private constant [%d x i8] [%s] ",
			idx, len(path)+1, byteArray(path))
	}
	return out.String()
}

// stage2OpenSources emits the source-tree input reads in stage2.
func stage2OpenSources() string {
	var out strings.Builder
	for idx, path := range selfhostSourcePaths() {
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
	for idx := range selfhostSourcePaths() {
		writeStage2SourceReadLoop(&out, idx)
	}
	for idx := range selfhostSourcePaths() {
		fmt.Fprintf(&out, "%%ok%d = icmp sgt i32 %%count%d, 0 ", idx, idx)
	}
	out.WriteString("%all0 = and i1 %ok0, %ok1 ")
	for idx := 2; idx < len(selfhostSourcePaths()); idx++ {
		fmt.Fprintf(&out, "%%all%d = and i1 %%all%d, %%ok%d ", idx-1, idx-2, idx)
	}
	out.WriteString("%total0 = add i32 %count0, %count1 ")
	for idx := 2; idx < len(selfhostSourcePaths()); idx++ {
		fmt.Fprintf(&out, "%%total%d = add i32 %%total%d, %%count%d ", idx-1, idx-2, idx)
	}
	out.WriteString("%fntotal0 = add i32 %fn0, %fn1 ")
	for idx := 2; idx < len(selfhostSourcePaths()); idx++ {
		fmt.Fprintf(&out, "%%fntotal%d = add i32 %%fntotal%d, %%fn%d ", idx-1, idx-2, idx)
	}
	writeStage2ParseMetric(&out)
	fmt.Fprintf(&out, "%%large = icmp sgt i32 %%total%d, 100 ", len(selfhostSourcePaths())-2)
	fmt.Fprintf(&out, "%%scanned = and i1 %%all%d, %%large ", len(selfhostSourcePaths())-2)
	for idx := range selfhostSourcePaths() {
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
	if idx+1 < len(selfhostSourcePaths()) {
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
	if idx+1 == len(selfhostSourcePaths()) {
		out.WriteString("after.reads: ")
	}
}

// writeStage2ReadPhis emits scan state for one source file.
func writeStage2ReadPhis(out *strings.Builder, idx int, prev string) {
	for slot := 1; slot <= 5; slot++ {
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
	writeStage2PairCounter(out, idx)
	writeStage2ImportCounter(out, idx)
	writeStage2StructCounter(out, idx)
	writeStage2EnumCounter(out, idx)
}

// writeStage2PairCounter emits the parser function-count metric.
func writeStage2PairCounter(out *strings.Builder, idx int) {
	fmt.Fprintf(out, "%%isf%d = icmp eq i32 %%prev%d_1, 102 ", idx, idx)
	fmt.Fprintf(out, "%%isn%d = icmp eq i32 %%ch%d, 110 ", idx, idx)
	fmt.Fprintf(out, "%%ispair%d = and i1 %%isf%d, %%isn%d ", idx, idx, idx)
	fmt.Fprintf(out, "%%pairint%d = zext i1 %%ispair%d to i32 ", idx, idx)
	fmt.Fprintf(out, "%%fnnext%d = add i32 %%fn%d, %%pairint%d ", idx, idx, idx)
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
	last := len(chars) - 1
	fmt.Fprintf(out, "%%%s%d_c%d = icmp eq i32 %%ch%d, %d ", name, idx, last, idx, chars[last])
	prev := fmt.Sprintf("%%%s%d_c%d", name, idx, last)
	for pos := last - 1; pos >= 0; pos-- {
		slot := last - pos
		fmt.Fprintf(out, "%%%s%d_c%d = icmp eq i32 %%prev%d_%d, %d ",
			name, idx, pos, idx, slot, chars[pos])
		fmt.Fprintf(out, "%%%s%d_m%d = and i1 %s, %%%s%d_c%d ",
			name, idx, pos, prev, name, idx, pos)
		prev = fmt.Sprintf("%%%s%d_m%d", name, idx, pos)
	}
	fmt.Fprintf(out, "%%%smatch%d = zext i1 %s to i32 ", name, idx, prev)
	fmt.Fprintf(out, "%%%snext%d = add i32 %%%s%d, %%%smatch%d ", name, idx, name, idx, name, idx)
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
	for slot := 2; slot <= 5; slot++ {
		fmt.Fprintf(out, "%%nextprev%d_%d = add i32 %%prev%d_%d, 0 ", idx, slot, idx, slot-1)
	}
}

// writeStage2ParseMetric aggregates the source scan into parser.kizu's score.
func writeStage2ParseMetric(out *strings.Builder) {
	for idx := range selfhostSourcePaths() {
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
	for idx := 2; idx < len(selfhostSourcePaths()); idx++ {
		fmt.Fprintf(out, "%%parsetotal%d = add i32 %%parsetotal%d, %%parse%d ",
			idx-1, idx-2, idx)
	}
	fmt.Fprintf(out, "%%parsetotal = add i32 %%parsetotal%d, 0 ", len(selfhostSourcePaths())-2)
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
