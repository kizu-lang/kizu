package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	kizuast "github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/interp"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

const (
	parserParityExamplesRoot = "../../examples"
	parserParityCaseStart    = "@@KIZU_PARSER_PARITY_CASE@@"
	parserParityCaseEnd      = "@@KIZU_PARSER_PARITY_END@@"
)

const stdKizuParserParityHarness = `
fn run_case(allocator: Allocator, name: []const u8, text: []const u8) -> !void {
    print("@@KIZU_PARSER_PARITY_CASE@@");
    print(name);
    let source = std::kizu::ast::source_file(name, text);
    let result = try std::kizu::parser::parse_program(allocator, source);
    try dump_node(text, result.ast, result.root);
    print("@@KIZU_PARSER_PARITY_END@@");
    return;
}

fn dump_node(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    id: std::kizu::ast::NodeId
) -> !void {
    let node = ast.get(id);
    match node.data {
        Program(program) => try dump_program(source, ast, program);
        FnDecl(fn_decl) => try dump_fn_decl(source, ast, fn_decl);
        Var(var_node) => try dump_leaf("Var", source, &node.span);
        Int(int_node) => try dump_leaf("Int", source, &node.span);
        String(string_node) => try dump_string(source, &node.span);
        TypeName(type_name) => try dump_leaf("TypeName", source, &node.span);
        Binary(binary) => try dump_binary(source, ast, binary);
        Call(call) => try dump_call(source, ast, call);
        Block(block) => try dump_block(source, ast, block);
        Return(return_node) => try dump_return(source, ast, return_node);
        ExprStmt(expr_stmt) => try dump_expr_stmt(source, ast, expr_stmt);
        If(if_node) => print("If");
        Let(let_node) => print("Let");
        Param(param_node) => try dump_param(source, ast, param_node);
        Field(field_node) => print("Field");
        StructDecl(struct_decl) => print("StructDecl");
        Match(match_node) => print("Match");
        MatchArm(match_arm) => print("MatchArm");
        Empty => print("Empty");
    }
    return;
}

fn dump_program(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    program: std::kizu::ast::ProgramNode
) -> !void {
    print("Program");
    try dump_range(source, ast, program.declarations);
    return;
}

fn dump_fn_decl(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    fn_decl: std::kizu::ast::FnDeclNode
) -> !void {
    print("FnDecl");
    try dump_node(source, ast, fn_decl.name);
    try dump_range(source, ast, fn_decl.params);
    try dump_node(source, ast, fn_decl.return_type);
    try dump_node(source, ast, fn_decl.body);
    return;
}

fn dump_param(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    param: std::kizu::ast::ParamNode
) -> !void {
    print("Param");
    try dump_node(source, ast, param.name);
    try dump_node(source, ast, param.type_node);
    return;
}

fn dump_block(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    block: std::kizu::ast::BlockNode
) -> !void {
    print("Block");
    try dump_range(source, ast, block.statements);
    return;
}

fn dump_return(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    return_node: std::kizu::ast::ReturnNode
) -> !void {
    print("Return");
    try dump_node(source, ast, return_node.value);
    return;
}

fn dump_binary(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    binary: std::kizu::ast::BinaryNode
) -> !void {
    print("Binary");
    match binary.op {
        Add => print("Add");
        Mul => print("Mul");
    }
    try dump_node(source, ast, binary.left);
    try dump_node(source, ast, binary.right);
    return;
}

fn dump_call(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    call: std::kizu::ast::CallNode
) -> !void {
    print("Call");
    try dump_node(source, ast, call.callee);
    try dump_range(source, ast, call.args);
    return;
}

fn dump_range(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    children: std::kizu::ast::ChildRange
) -> !void {
    print("Range");
    print(children.len);
    var index = 0;
    while index < children.len {
        let child = try ast.child_at(children, index);
        try dump_node(source, ast, child);
        index = index + 1;
    }
    return;
}

fn dump_expr_stmt(
    source: []const u8,
    ast: std::kizu::ast::Ast,
    expr_stmt: std::kizu::ast::ExprStmtNode
) -> !void {
    print("ExprStmt");
    try dump_node(source, ast, expr_stmt.expr);
    return;
}

fn dump_leaf(
    kind: []const u8,
    source: []const u8,
    span: &std::kizu::ast::Span
) -> !void {
    print(kind);
    let text = try std::mem::slice(source, span.start, span.end);
    print(text);
    return;
}

fn dump_string(
    source: []const u8,
    span: &std::kizu::ast::Span
) -> !void {
    print("String");
    let text = try std::mem::slice(source, span.start + 1, span.end - 1);
    print(text);
    return;
}
`

type parserParityCase struct {
	name   string
	source string
	want   string
}

type parserParityStats struct {
	scanned            int
	goParseErrors      int
	goParseErrorSample string
	unsupported        int
	unsupportedReasons map[string]int
	unsupportedSamples map[string]string
}

// TestStdKizuParserParityExamples checks examples against the std Kizu parser subset.
func TestStdKizuParserParityExamples(t *testing.T) {
	examples, stats := collectParserParityExamples(t)
	seeds := parserParitySeedCases(t)
	cases := append(seeds, examples...)
	got := runStdKizuParserParityHarness(t, cases)

	assertParserParityCases(t, cases, got)
	t.Logf(
		"examples scanned=%d compared=%d go_parse_errors=%d unsupported=%d seeds=%d",
		stats.scanned,
		len(examples),
		stats.goParseErrors,
		stats.unsupported,
		len(seeds),
	)
	if stats.goParseErrorSample != "" {
		t.Logf("go_parse_error_sample=%s", stats.goParseErrorSample)
	}
	logUnsupportedParserParityReasons(t, stats.unsupportedReasons, stats.unsupportedSamples)
}

// collectParserParityExamples finds examples supported by the current std parser subset.
func collectParserParityExamples(t *testing.T) ([]parserParityCase, parserParityStats) {
	t.Helper()
	stats := parserParityStats{
		unsupportedReasons: map[string]int{},
		unsupportedSamples: map[string]string{},
	}
	cases := []parserParityCase{}
	err := filepath.WalkDir(parserParityExamplesRoot, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return err
		}
		stats.scanned++
		next, ok, reason, parseErrs := parserParityExampleCase(path)
		switch {
		case len(parseErrs) > 0:
			stats.goParseErrors++
			if stats.goParseErrorSample == "" {
				stats.goParseErrorSample = parserParityExampleName(path) + ": " + parseErrs[0]
			}
		case !ok:
			stats.unsupported++
			stats.unsupportedReasons[reason]++
			if stats.unsupportedSamples[reason] == "" {
				stats.unsupportedSamples[reason] = parserParityExampleName(path)
			}
		default:
			cases = append(cases, next)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	return cases, stats
}

// parserParityExampleCase summarizes one example when both parser subsets can handle it.
func parserParityExampleCase(path string) (parserParityCase, bool, string, []string) {
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		return parserParityCase{}, false, err.Error(), nil
	}
	source := string(sourceBytes)
	want, reason, parseErrs := summarizeGoParserSubset(source)
	if len(parseErrs) > 0 || reason != "" {
		return parserParityCase{}, false, reason, parseErrs
	}
	return parserParityCase{
		name:   parserParityExampleName(path),
		source: source,
		want:   want,
	}, true, "", nil
}

// parserParityExampleName returns a stable corpus name for an example file.
func parserParityExampleName(path string) string {
	rel, err := filepath.Rel(parserParityExamplesRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return "examples/" + filepath.ToSlash(rel)
}

// parserParitySeedCases provides stable positive coverage for the current subset.
func parserParitySeedCases(t *testing.T) []parserParityCase {
	t.Helper()
	seeds := []parserParityCase{
		{name: "seed/fn_empty", source: "fn main() {}"},
		{name: "seed/two_fns", source: "fn one() {} fn two() {}"},
		{name: "seed/fn_params_return", source: "fn add(a: i64, b: i64) -> i64 { return a + b; }"},
		{name: "seed/fn_error_union_return", source: "fn main() -> !void {}"},
		{name: "seed/fn_slice_param", source: "fn write(bytes: []const u8) {}"},
		{name: "seed/fn_borrow_param", source: "fn read(value: &i64) {}"},
		{name: "seed/fn_mut_borrow_param", source: "fn fill(out: &mut i64) {}"},
		{name: "seed/fn_namespace_type", source: "fn use(allocator: std::mem::Allocator) {}"},
		{
			name:   "seed/fn_generic_type",
			source: "fn collect(items: std::array::Array<[]const u8>) {}",
		},
		{name: "seed/fn_return_int", source: "fn main() { return 1; }"},
		{name: "seed/fn_expr_stmt_string", source: `fn main() { print("hello"); }`},
		{name: "seed/fn_expr_stmt_precedence", source: "fn main() { print(1 + 2 * 3); }"},
		{name: "seed/fn_expr_stmt_mul_then_add", source: "fn main() { print(1 * 2 + 3); }"},
		{name: "seed/fn_expr_stmt_left_assoc_add", source: "fn main() { print(1 + 2 + 3); }"},
		{
			name:   "seed/fn_return_call_binary",
			source: "fn main() { return add(1 + x); return y; }",
		},
		{
			name:   "seed/fn_return_binary_call",
			source: "fn main() { return add(1, x) + y; }",
		},
	}
	for index := range seeds {
		want, reason, parseErrs := summarizeGoParserSubset(seeds[index].source)
		if len(parseErrs) > 0 {
			t.Fatalf("%s Go parse errors: %v", seeds[index].name, parseErrs)
		}
		if reason != "" {
			t.Fatalf("%s is unsupported: %s", seeds[index].name, reason)
		}
		seeds[index].want = want
	}
	return seeds
}

// summarizeGoParserSubset returns a canonical summary for the shared parser subset.
func summarizeGoParserSubset(source string) (string, string, []string) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", "", p.Errors()
	}
	lines, reason := summarizeProgramSubset(program)
	if reason != "" {
		return "", reason, nil
	}
	if reason := unsupportedStdParserSource(source); reason != "" {
		return "", reason, nil
	}
	return strings.Join(lines, "\n"), "", nil
}

// unsupportedStdParserSource rejects bytes the Kizu std lexer cannot scan yet.
func unsupportedStdParserSource(source string) string {
	inString := false
	for index := 0; index < len(source); index++ {
		r := rune(source[index])
		if inString {
			if r == '"' {
				inString = false
			}
			continue
		}
		if r == '"' {
			inString = true
			continue
		}
		if isStdParserSpace(r) || isStdParserPunctuation(r) {
			continue
		}
		if isStdParserWordRune(r) {
			continue
		}
		return "source contains tokens outside std parser subset"
	}
	if inString {
		return "unterminated string literal"
	}
	return ""
}

// summarizeProgramSubset summarizes a complete Go AST program when it is in subset.
func summarizeProgramSubset(program *kizuast.Program) ([]string, string) {
	if len(program.Decls) == 0 {
		return nil, "program is empty"
	}
	lines := []string{"Program", "Range", strconv.Itoa(len(program.Decls))}
	for _, decl := range program.Decls {
		fn, ok := decl.(*kizuast.FunctionDecl)
		if !ok {
			return nil, "top-level declaration is not function"
		}
		next, reason := summarizeFunctionSubset(fn)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeFunctionSubset summarizes a function declaration in the shared subset.
func summarizeFunctionSubset(fn *kizuast.FunctionDecl) ([]string, string) {
	if fn.Public || fn.Unsafe || fn.ExternABI != "" {
		return nil, "function has unsupported modifiers"
	}
	if len(fn.TypeParams) > 0 {
		return nil, "function has unsupported type parameters"
	}
	params, reason := summarizeParamsSubset(fn.Params)
	if reason != "" {
		return nil, reason
	}
	returnType, reason := summarizeReturnTypeSubset(fn.ReturnType)
	if reason != "" {
		return nil, reason
	}
	if fn.Body == nil {
		return nil, "function has no body"
	}
	body, reason := summarizeBlockSubset(fn.Body)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"FnDecl", "Var", fn.Name}
	lines = append(lines, params...)
	lines = append(lines, returnType...)
	return append(lines, body...), ""
}

// summarizeParamsSubset summarizes simple named function parameters.
func summarizeParamsSubset(params []kizuast.Param) ([]string, string) {
	lines := []string{"Range", strconv.Itoa(len(params))}
	for _, param := range params {
		next, reason := summarizeParamSubset(param)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeParamSubset summarizes one simple `name: Type` parameter.
func summarizeParamSubset(param kizuast.Param) ([]string, string) {
	if param.Comptime {
		return nil, "function has unsupported parameters"
	}
	if !isStdParserIdent(param.Name) {
		return nil, "identifier outside std parser subset"
	}
	typeName, reason := summarizeTypeNameSubset(parserParityParamTypeName(param))
	if reason != "" {
		return nil, reason
	}
	lines := []string{"Param", "Var", param.Name}
	return append(lines, typeName...), ""
}

// summarizeReturnTypeSubset summarizes an optional simple function return type.
func summarizeReturnTypeSubset(typeName string) ([]string, string) {
	if typeName == "" {
		return []string{"Empty"}, ""
	}
	return summarizeTypeNameSubset(typeName)
}

// summarizeTypeNameSubset summarizes a plain identifier type name.
func summarizeTypeNameSubset(typeName string) ([]string, string) {
	if typeName == "" || strings.Contains(typeName, "\n") {
		return nil, "type outside std parser subset"
	}
	return []string{"TypeName", typeName}, ""
}

// parserParityParamTypeName returns the Go parser spelling used for param types.
func parserParityParamTypeName(param kizuast.Param) string {
	switch {
	case param.MutBorrow:
		return "&mut " + param.TypeName
	case param.Borrow:
		return "&" + param.TypeName
	default:
		return param.TypeName
	}
}

// summarizeBlockSubset summarizes a block containing only return statements.
func summarizeBlockSubset(block *kizuast.BlockStmt) ([]string, string) {
	lines := []string{"Block", "Range", strconv.Itoa(len(block.Statements))}
	for _, stmt := range block.Statements {
		next, reason := summarizeStatementSubset(stmt)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeStatementSubset summarizes statements supported by std::kizu::parser.
func summarizeStatementSubset(stmt kizuast.Statement) ([]string, string) {
	switch node := stmt.(type) {
	case *kizuast.ReturnStmt:
		if node.Value == nil {
			return nil, "return without value"
		}
		value, reason := summarizeExprSubset(node.Value)
		if reason != "" {
			return nil, reason
		}
		return append([]string{"Return"}, value...), ""
	case *kizuast.ExprStmt:
		value, reason := summarizeExprSubset(node.Expr)
		if reason != "" {
			return nil, reason
		}
		return append([]string{"ExprStmt"}, value...), ""
	default:
		return nil, "statement outside std parser subset"
	}
}

// summarizeExprSubset summarizes expressions supported by std::kizu::parser.
func summarizeExprSubset(expr kizuast.Expression) ([]string, string) {
	if binary, ok := expr.(*kizuast.BinaryExpr); ok {
		return summarizeBinarySubset(binary)
	}
	return summarizePrimarySubset(expr)
}

// summarizeBinarySubset summarizes supported binary expressions.
func summarizeBinarySubset(expr *kizuast.BinaryExpr) ([]string, string) {
	op, ok := parserParityBinaryOp(expr.Operator)
	if !ok {
		return nil, "binary operator outside std parser subset"
	}
	left, reason := summarizeExprSubset(expr.Left)
	if reason != "" {
		return nil, reason
	}
	right, reason := summarizeExprSubset(expr.Right)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"Binary", op}
	lines = append(lines, left...)
	return append(lines, right...), ""
}

// parserParityBinaryOp maps shared binary operators to summary labels.
func parserParityBinaryOp(op string) (string, bool) {
	switch op {
	case "+":
		return "Add", true
	case "*":
		return "Mul", true
	default:
		return "", false
	}
}

// summarizePrimarySubset summarizes identifiers, integers, and calls.
func summarizePrimarySubset(expr kizuast.Expression) ([]string, string) {
	switch node := expr.(type) {
	case *kizuast.IdentExpr:
		if !isStdParserIdent(node.Name) {
			return nil, "identifier outside std parser subset"
		}
		return []string{"Var", node.Name}, ""
	case *kizuast.IntExpr:
		if !isStdParserInt(node.Value) {
			return nil, "integer outside std parser subset"
		}
		return []string{"Int", node.Value}, ""
	case *kizuast.StringExpr:
		return []string{"String", node.Value}, ""
	case *kizuast.CallExpr:
		return summarizeCallSubset(node)
	default:
		return nil, "expression outside std parser subset"
	}
}

// summarizeCallSubset summarizes calls whose callee is a plain identifier.
func summarizeCallSubset(expr *kizuast.CallExpr) ([]string, string) {
	callee, ok := expr.Callee.(*kizuast.IdentExpr)
	if !ok || !isStdParserIdent(callee.Name) {
		return nil, "call callee outside std parser subset"
	}
	lines := []string{"Call", "Var", callee.Name, "Range", strconv.Itoa(len(expr.Args))}
	for _, arg := range expr.Args {
		next, reason := summarizeExprSubset(arg)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// isStdParserSpace reports whitespace understood by std::kizu::lexer.
func isStdParserSpace(r rune) bool {
	return r == ' ' || r == '\n' || r == '\t' || r == '\r'
}

// isStdParserPunctuation reports punctuation understood by std::kizu::lexer.
func isStdParserPunctuation(r rune) bool {
	return strings.ContainsRune("{}();,:!&[]<>?+*-", r)
}

// isStdParserWordRune reports identifier and number bytes understood by the std lexer.
func isStdParserWordRune(r rune) bool {
	return r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') ||
		('0' <= r && r <= '9')
}

// isStdParserIdent reports whether name is a plain std lexer identifier.
func isStdParserIdent(name string) bool {
	if name == "" || !isStdParserIdentStart(rune(name[0])) {
		return false
	}
	for _, r := range name[1:] {
		if !isStdParserWordRune(r) {
			return false
		}
	}
	return name != "fn" && name != "return"
}

// isStdParserIdentStart reports whether r can start a std lexer identifier.
func isStdParserIdentStart(r rune) bool {
	return r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

// isStdParserInt reports whether value is a decimal integer literal.
func isStdParserInt(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// runStdKizuParserParityHarness runs the Kizu std parser once for all cases.
func runStdKizuParserParityHarness(
	t *testing.T,
	cases []parserParityCase,
) map[string]string {
	t.Helper()
	source, err := buildStdKizuParserParityHarness(cases)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "std_kizu_parser_parity.kizu")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	program, errs, err := parsePathWithStd(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) > 0 {
		t.Fatalf("harness parse errors: %v", errs)
	}
	var out bytes.Buffer
	if err := interp.New(&out).Run(program); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out.String())
	}
	got, err := parseStdKizuParserParityOutput(out.String())
	if err != nil {
		t.Fatalf("invalid harness output: %v\n%s", err, out.String())
	}
	return got
}

// buildStdKizuParserParityHarness creates a Kizu program that parses all cases.
func buildStdKizuParserParityHarness(cases []parserParityCase) (string, error) {
	var out strings.Builder
	out.WriteString(stdKizuParserParityHarness)
	out.WriteString("\nfn main() -> !void {\n")
	out.WriteString("    let allocator = std::mem::page_allocator();\n")
	for index, testCase := range cases {
		name, err := kizuRawStringLiteral(testCase.name)
		if err != nil {
			return "", fmt.Errorf("%s name: %w", testCase.name, err)
		}
		source, cleanup, err := writeKizuSourceLiteral(&out, index, testCase.source)
		if err != nil {
			return "", fmt.Errorf("%s source: %w", testCase.name, err)
		}
		fmt.Fprintf(&out, "    try run_case(allocator, %s, %s);\n", name, source)
		if cleanup != "" {
			out.WriteString(cleanup)
		}
	}
	out.WriteString("    return;\n}\n")
	return out.String(), nil
}

// writeKizuSourceLiteral writes source construction and returns its expression.
func writeKizuSourceLiteral(out *strings.Builder, index int, value string) (string, string, error) {
	if !strings.Contains(value, "\"") {
		lit, err := kizuRawStringLiteral(value)
		return lit, "", err
	}
	name := fmt.Sprintf("source_%d", index)
	bytesName := fmt.Sprintf("%s_bytes", name)
	fmt.Fprintf(out, "    var %s = std::string::String(allocator);\n", name)
	if err := writeKizuStringChunks(out, name, value); err != nil {
		return "", "", err
	}
	fmt.Fprintf(out, "    let %s = %s.as_bytes();\n", bytesName, name)
	return bytesName, fmt.Sprintf("    %s.deinit();\n", name), nil
}

// writeKizuStringChunks appends raw chunks and quote bytes to a String value.
func writeKizuStringChunks(out *strings.Builder, name string, value string) error {
	parts := strings.Split(value, "\"")
	for index, part := range parts {
		if part != "" {
			lit, err := kizuRawStringLiteral(part)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "    try %s.append_bytes(%s);\n", name, lit)
		}
		if index < len(parts)-1 {
			fmt.Fprintf(out, "    try %s.append_byte(cast<u8>(34));\n", name)
		}
	}
	return nil
}

// kizuRawStringLiteral returns a literal for Kizu's current no-escape strings.
func kizuRawStringLiteral(value string) (string, error) {
	if strings.Contains(value, "\"") {
		return "", fmt.Errorf("contains double quote")
	}
	return `"` + value + `"`, nil
}

// parseStdKizuParserParityOutput extracts summaries printed by the harness.
func parseStdKizuParserParityOutput(out string) (map[string]string, error) {
	result := map[string]string{}
	trimmed := strings.TrimSuffix(out, "\n")
	if trimmed == "" {
		return result, nil
	}
	lines := strings.Split(trimmed, "\n")
	for index := 0; index < len(lines); {
		if lines[index] != parserParityCaseStart {
			return nil, fmt.Errorf("expected case start at line %d", index+1)
		}
		name, next, err := parseStdKizuParserParityCase(lines, index+1)
		if err != nil {
			return nil, err
		}
		result[name] = strings.Join(lines[index+2:next], "\n")
		index = next + 1
	}
	return result, nil
}

// parseStdKizuParserParityCase finds one case name and end delimiter.
func parseStdKizuParserParityCase(lines []string, start int) (string, int, error) {
	if start >= len(lines) {
		return "", 0, fmt.Errorf("missing case name")
	}
	name := lines[start]
	index := start + 1
	for index < len(lines) && lines[index] != parserParityCaseEnd {
		index++
	}
	if index >= len(lines) {
		return "", 0, fmt.Errorf("missing case end for %s", name)
	}
	return name, index, nil
}

// assertParserParityCases compares all expected and actual summaries.
func assertParserParityCases(
	t *testing.T,
	cases []parserParityCase,
	got map[string]string,
) {
	t.Helper()
	wantNames := map[string]bool{}
	for _, testCase := range cases {
		wantNames[testCase.name] = true
		actual, ok := got[testCase.name]
		if !ok {
			t.Errorf("%s missing from harness output", testCase.name)
			continue
		}
		if actual != testCase.want {
			t.Errorf("%s summary mismatch\nwant:\n%s\ngot:\n%s", testCase.name, testCase.want, actual)
		}
	}
	for name := range got {
		if !wantNames[name] {
			t.Errorf("unexpected harness output for %s", name)
		}
	}
}

// logUnsupportedParserParityReasons reports the most common unsupported reasons.
func logUnsupportedParserParityReasons(
	t *testing.T,
	reasons map[string]int,
	samples map[string]string,
) {
	t.Helper()
	keys := make([]string, 0, len(reasons))
	for reason := range reasons {
		keys = append(keys, reason)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] == reasons[keys[j]] {
			return keys[i] < keys[j]
		}
		return reasons[keys[i]] > reasons[keys[j]]
	})
	for index, reason := range keys {
		if index == 5 {
			break
		}
		t.Logf(
			"unsupported[%d]=%s: %d sample=%s",
			index+1,
			reason,
			reasons[reason],
			samples[reason],
		)
	}
}
