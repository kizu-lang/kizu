package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	kizuast "github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/parser"
)

const (
	parserParityExamplesRoot = "../../examples"
	parserParityStdRoot      = "../../std"
	parserParityCaseStart    = "@@KIZU_PARSER_PARITY_CASE@@"
	parserParityCaseEnd      = "@@KIZU_PARSER_PARITY_END@@"
)

var parserParityFrontendStdPaths = []string{
	"src/mem.kizu",
	"src/array.kizu",
	"src/string.kizu",
	"src/map.kizu",
	"src/fmt.kizu",
	"src/testing.kizu",
	"src/fs.kizu",
	"src/path.kizu",
	"src/io.kizu",
	"src/process.kizu",
	"src/kizu/ast.kizu",
	"src/kizu/lexer.kizu",
	"src/kizu/parser.kizu",
	"src/kizu/diagnostic.kizu",
}

const stdKizuParserParityHarness = `
fn run_case(allocator: Allocator, io: Io, name: []u8, path: []u8) -> !void {
    print("@@KIZU_PARSER_PARITY_CASE@@");
    print(name);
    let text = try std::fs::read_file(io, path);
    let source = std::kizu::ast::source_file(name, text);
    let result = try std::kizu::parser::parse_program(allocator, source);
    try dump_node(text, result.ast, result.root);
    print("@@KIZU_PARSER_PARITY_END@@");
    return;
}

fn dump_node(
    source: []u8,
    ast: std::kizu::ast::Ast,
    id: std::kizu::ast::NodeId
) -> !void {
    let node = ast.get(id);
    match node.data {
        Program(program) => try dump_program(source, ast, program);,
        ImportDecl(import_decl) => try dump_import_decl(source, ast, import_decl);,
        FnDecl(fn_decl) => try dump_fn_decl(source, ast, fn_decl);,
        Var(var_node) => try dump_leaf("Var", source, &node.span);,
        Int(int_node) => try dump_leaf("Int", source, &node.span);,
        String(string_node) => try dump_string(source, &node.span);,
        TypeName(type_name) => try dump_leaf("TypeName", source, &node.span);,
        Bool(bool_node) => dump_bool(bool_node);,
        Prefix(prefix) => try dump_prefix(source, ast, prefix);,
        Binary(binary) => try dump_binary(source, ast, binary);,
        FieldExpr(field_expr) => try dump_field_expr(source, ast, field_expr);,
        DerefExpr(deref_expr) => try dump_deref_expr(source, ast, deref_expr);,
        Call(call) => try dump_call(source, ast, call);,
        TypeApplyExpr(type_apply) => try dump_type_apply_expr(source, ast, type_apply);,
        CastExpr(cast_expr) => try dump_cast_expr(source, ast, cast_expr);,
        IndexExpr(index_expr) => try dump_index_expr(source, ast, index_expr);,
        StructLiteralExpr(struct_literal) => try dump_struct_literal_expr(
            source,
            ast,
            struct_literal
        );,
        StructFieldInit(struct_field_init) => try dump_struct_field_init(
            source,
            ast,
            struct_field_init
        );,
        ArenaNewExpr(arena_new) => try dump_arena_new_expr(source, ast, arena_new);,
        TryExpr(try_expr) => try dump_try_expr(source, ast, try_expr);,
        ComptimeExpr(comptime_expr) => try dump_comptime_expr(source, ast, comptime_expr);,
        Block(block) => try dump_block(source, ast, block);,
        Return(return_node) => try dump_return(source, ast, return_node);,
        Defer(defer_node) => try dump_defer(source, ast, defer_node);,
        ErrDefer(err_defer_node) => try dump_err_defer(source, ast, err_defer_node);,
        ExprStmt(expr_stmt) => try dump_expr_stmt(source, ast, expr_stmt);,
        If(if_node) => try dump_if(source, ast, if_node);,
        Let(let_node) => try dump_let(source, ast, let_node);,
        Assign(assign_node) => try dump_assign(source, ast, assign_node);,
        While(while_node) => try dump_while(source, ast, while_node);,
        For(for_node) => try dump_for(source, ast, for_node);,
        Break(break_node) => try dump_break(source, ast, break_node);,
        Continue(continue_node) => try dump_continue(source, ast, continue_node);,
        Param(param_node) => try dump_param(source, ast, param_node);,
        Field(field_node) => try dump_field(source, ast, field_node);,
        StructDecl(struct_decl) => try dump_struct_decl(source, ast, struct_decl);,
        EnumDecl(enum_decl) => try dump_enum_decl(source, ast, enum_decl);,
        UnionDecl(union_decl) => try dump_union_decl(source, ast, union_decl);,
        ImplDecl(impl_decl) => try dump_impl_decl(source, ast, impl_decl);,
        UnionVariant(union_variant) => try dump_union_variant(source, ast, union_variant);,
        Match(match_node) => try dump_match(source, ast, match_node);,
        MatchArm(match_arm) => try dump_match_arm(source, ast, match_arm);,
        Unsafe(unsafe_node) => try dump_unsafe(source, ast, unsafe_node);,
        ComptimeIf(comptime_if) => try dump_comptime_if(source, ast, comptime_if);,
        ContractDecl(contract_decl) => try dump_leaf("ContractDecl", source, &node.span);,
        Empty => print("Empty");,
    }
    return;
}

fn dump_program(
    source: []u8,
    ast: std::kizu::ast::Ast,
    program: &std::kizu::ast::ProgramNode
) -> !void {
    print("Program");
    try dump_range(source, ast, program.declarations);
    return;
}

fn dump_fn_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    fn_decl: &std::kizu::ast::FnDeclNode
) -> !void {
    print("FnDecl");
    dump_visibility(fn_decl.public);
    dump_safety(fn_decl.requires_unsafe);
    try dump_node(source, ast, fn_decl.extern_abi);
    try dump_node(source, ast, fn_decl.name);
    try dump_range(source, ast, fn_decl.type_params);
    try dump_range(source, ast, fn_decl.params);
    try dump_node(source, ast, fn_decl.return_type);
    try dump_node(source, ast, fn_decl.return_borrow);
    try dump_node(source, ast, fn_decl.body);
    return;
}

fn dump_param(
    source: []u8,
    ast: std::kizu::ast::Ast,
    param: &std::kizu::ast::ParamNode
) -> !void {
    print("Param");
    if param.comptime_param {
        print("Comptime");
    } else {
        print("Runtime");
    }
    try dump_node(source, ast, param.name);
    try dump_node(source, ast, param.type_node);
    return;
}

fn dump_import_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    import_decl: &std::kizu::ast::ImportDeclNode
) -> !void {
    print("ImportDecl");
    try dump_range(source, ast, import_decl.path);
    return;
}

fn dump_field(
    source: []u8,
    ast: std::kizu::ast::Ast,
    field: &std::kizu::ast::FieldNode
) -> !void {
    print("Field");
    dump_visibility(field.public);
    try dump_node(source, ast, field.name);
    try dump_node(source, ast, field.type_node);
    return;
}

fn dump_struct_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    struct_decl: &std::kizu::ast::StructDeclNode
) -> !void {
    print("StructDecl");
    dump_visibility(struct_decl.public);
    try dump_node(source, ast, struct_decl.name);
    try dump_range(source, ast, struct_decl.type_params);
    try dump_range(source, ast, struct_decl.fields);
    return;
}

fn dump_enum_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    enum_decl: &std::kizu::ast::EnumDeclNode
) -> !void {
    print("EnumDecl");
    dump_visibility(enum_decl.public);
    try dump_node(source, ast, enum_decl.name);
    try dump_range(source, ast, enum_decl.tags);
    return;
}

fn dump_union_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    union_decl: &std::kizu::ast::UnionDeclNode
) -> !void {
    print("UnionDecl");
    dump_visibility(union_decl.public);
    try dump_node(source, ast, union_decl.name);
    try dump_range(source, ast, union_decl.type_params);
    try dump_range(source, ast, union_decl.variants);
    return;
}

fn dump_impl_decl(
    source: []u8,
    ast: std::kizu::ast::Ast,
    impl_decl: &std::kizu::ast::ImplDeclNode
) -> !void {
    print("ImplDecl");
    try dump_node(source, ast, impl_decl.type_name);
    try dump_range(source, ast, impl_decl.methods);
    return;
}

fn dump_union_variant(
    source: []u8,
    ast: std::kizu::ast::Ast,
    union_variant: &std::kizu::ast::UnionVariantNode
) -> !void {
    print("UnionVariant");
    try dump_node(source, ast, union_variant.name);
    try dump_node(source, ast, union_variant.payload);
    return;
}

fn dump_visibility(public: bool) -> void {
    if public {
        print("Public");
    } else {
        print("Private");
    }
    return;
}

fn dump_safety(requires_unsafe: bool) -> void {
    if requires_unsafe {
        print("RequiresUnsafe");
    } else {
        print("Safe");
    }
    return;
}

fn dump_block(
    source: []u8,
    ast: std::kizu::ast::Ast,
    block: &std::kizu::ast::BlockNode
) -> !void {
    print("Block");
    try dump_range(source, ast, block.statements);
    return;
}

fn dump_bool(bool_node: &std::kizu::ast::BoolNode) -> void {
    print("Bool");
    if bool_node.value {
        print("true");
    } else {
        print("false");
    }
    return;
}

fn dump_return(
    source: []u8,
    ast: std::kizu::ast::Ast,
    return_node: &std::kizu::ast::ReturnNode
) -> !void {
    print("Return");
    try dump_node(source, ast, return_node.value);
    return;
}

fn dump_defer(
    source: []u8,
    ast: std::kizu::ast::Ast,
    defer_node: &std::kizu::ast::DeferNode
) -> !void {
    print("Defer");
    try dump_node(source, ast, defer_node.expr);
    return;
}

fn dump_err_defer(
    source: []u8,
    ast: std::kizu::ast::Ast,
    err_defer_node: &std::kizu::ast::ErrDeferNode
) -> !void {
    print("ErrDefer");
    try dump_node(source, ast, err_defer_node.expr);
    return;
}

fn dump_if(
    source: []u8,
    ast: std::kizu::ast::Ast,
    if_node: &std::kizu::ast::IfNode
) -> !void {
    print("If");
    try dump_node(source, ast, if_node.condition);
    try dump_node(source, ast, if_node.then_block);
    try dump_node(source, ast, if_node.else_block);
    return;
}

fn dump_let(
    source: []u8,
    ast: std::kizu::ast::Ast,
    let_node: &std::kizu::ast::LetNode
) -> !void {
    print("Let");
    if let_node.mutable {
        print("Mutable");
    } else {
        print("Immutable");
    }
    try dump_node(source, ast, let_node.name);
    try dump_node(source, ast, let_node.value);
    return;
}

fn dump_assign(
    source: []u8,
    ast: std::kizu::ast::Ast,
    assign_node: &std::kizu::ast::AssignNode
) -> !void {
    print("Assign");
    try dump_node(source, ast, assign_node.target);
    try dump_node(source, ast, assign_node.value);
    return;
}

fn dump_while(
    source: []u8,
    ast: std::kizu::ast::Ast,
    while_node: &std::kizu::ast::WhileNode
) -> !void {
    print("While");
    try dump_node(source, ast, while_node.label);
    try dump_node(source, ast, while_node.condition);
    try dump_node(source, ast, while_node.body);
    return;
}

fn dump_for(
    source: []u8,
    ast: std::kizu::ast::Ast,
    for_node: &std::kizu::ast::ForNode
) -> !void {
    print("For");
    try dump_node(source, ast, for_node.label);
    try dump_node(source, ast, for_node.name);
    try dump_node(source, ast, for_node.start);
    try dump_node(source, ast, for_node.end);
    try dump_node(source, ast, for_node.body);
    return;
}

fn dump_break(
    source: []u8,
    ast: std::kizu::ast::Ast,
    break_node: &std::kizu::ast::BreakNode
) -> !void {
    print("Break");
    try dump_node(source, ast, break_node.label);
    return;
}

fn dump_continue(
    source: []u8,
    ast: std::kizu::ast::Ast,
    continue_node: &std::kizu::ast::ContinueNode
) -> !void {
    print("Continue");
    try dump_node(source, ast, continue_node.label);
    return;
}

fn dump_binary(
    source: []u8,
    ast: std::kizu::ast::Ast,
    binary: &std::kizu::ast::BinaryNode
) -> !void {
    print("Binary");
    match binary.op {
        Add => print("Add");,
        Sub => print("Sub");,
        Mul => print("Mul");,
        Div => print("Div");,
        Mod => print("Mod");,
        Eq => print("Eq");,
        NotEq => print("NotEq");,
        LT => print("LT");,
        LTE => print("LTE");,
        GT => print("GT");,
        GTE => print("GTE");,
        And => print("And");,
        Or => print("Or");,
    }
    try dump_node(source, ast, binary.left);
    try dump_node(source, ast, binary.right);
    return;
}

fn dump_prefix(
    source: []u8,
    ast: std::kizu::ast::Ast,
    prefix: &std::kizu::ast::PrefixNode
) -> !void {
    print("Prefix");
    match prefix.op {
        Not => print("Not");,
        Neg => print("Neg");,
        Borrow => print("Borrow");,
        MutBorrow => print("MutBorrow");,
    }
    try dump_node(source, ast, prefix.right);
    return;
}

fn dump_field_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    field_expr: &std::kizu::ast::FieldExprNode
) -> !void {
    print("FieldExpr");
    if field_expr.namespace {
        print("Namespace");
    } else {
        print("Field");
    }
    try dump_node(source, ast, field_expr.receiver);
    try dump_node(source, ast, field_expr.name);
    return;
}

fn dump_deref_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    deref_expr: &std::kizu::ast::DerefExprNode
) -> !void {
    print("DerefExpr");
    try dump_node(source, ast, deref_expr.receiver);
    return;
}

fn dump_call(
    source: []u8,
    ast: std::kizu::ast::Ast,
    call: &std::kizu::ast::CallNode
) -> !void {
    print("Call");
    try dump_node(source, ast, call.callee);
    try dump_range(source, ast, call.args);
    return;
}

fn dump_type_apply_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    type_apply: &std::kizu::ast::TypeApplyExprNode
) -> !void {
    print("TypeApplyExpr");
    try dump_node(source, ast, type_apply.callee);
    try dump_range(source, ast, type_apply.type_args);
    return;
}

fn dump_cast_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    cast_expr: &std::kizu::ast::CastExprNode
) -> !void {
    print("CastExpr");
    try dump_node(source, ast, cast_expr.type_node);
    try dump_node(source, ast, cast_expr.value);
    return;
}

fn dump_index_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    index_expr: &std::kizu::ast::IndexExprNode
) -> !void {
    print("IndexExpr");
    if index_expr.slice {
        print("Slice");
    } else {
        print("Index");
    }
    try dump_node(source, ast, index_expr.target);
    try dump_node(source, ast, index_expr.start);
    try dump_node(source, ast, index_expr.end);
    return;
}

fn dump_struct_literal_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    struct_literal: &std::kizu::ast::StructLiteralExprNode
) -> !void {
    print("StructLiteralExpr");
    try dump_node(source, ast, struct_literal.type_name);
    try dump_range(source, ast, struct_literal.fields);
    return;
}

fn dump_struct_field_init(
    source: []u8,
    ast: std::kizu::ast::Ast,
    struct_field_init: &std::kizu::ast::StructFieldInitNode
) -> !void {
    print("StructFieldInit");
    try dump_node(source, ast, struct_field_init.name);
    try dump_node(source, ast, struct_field_init.value);
    return;
}

fn dump_arena_new_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    arena_new: &std::kizu::ast::ArenaNewExprNode
) -> !void {
    print("ArenaNewExpr");
    try dump_node(source, ast, arena_new.type_node);
    try dump_node(source, ast, arena_new.allocator);
    return;
}

fn dump_try_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    try_expr: &std::kizu::ast::TryExprNode
) -> !void {
    print("TryExpr");
    try dump_node(source, ast, try_expr.value);
    return;
}

fn dump_comptime_expr(
    source: []u8,
    ast: std::kizu::ast::Ast,
    comptime_expr: &std::kizu::ast::ComptimeExprNode
) -> !void {
    print("ComptimeExpr");
    try dump_node(source, ast, comptime_expr.value);
    return;
}

fn dump_match(
    source: []u8,
    ast: std::kizu::ast::Ast,
    match_node: &std::kizu::ast::MatchNode
) -> !void {
    print("Match");
    try dump_node(source, ast, match_node.value);
    try dump_range(source, ast, match_node.arms);
    return;
}

fn dump_match_arm(
    source: []u8,
    ast: std::kizu::ast::Ast,
    match_arm: &std::kizu::ast::MatchArmNode
) -> !void {
    print("MatchArm");
    try dump_node(source, ast, match_arm.pattern);
    try dump_node(source, ast, match_arm.binding);
    try dump_node(source, ast, match_arm.body);
    return;
}

fn dump_unsafe(
    source: []u8,
    ast: std::kizu::ast::Ast,
    unsafe_node: &std::kizu::ast::UnsafeNode
) -> !void {
    print("Unsafe");
    try dump_range(source, ast, unsafe_node.capabilities);
    try dump_node(source, ast, unsafe_node.body);
    return;
}

fn dump_comptime_if(
    source: []u8,
    ast: std::kizu::ast::Ast,
    comptime_if: &std::kizu::ast::ComptimeIfNode
) -> !void {
    print("ComptimeIf");
    try dump_node(source, ast, comptime_if.condition);
    try dump_node(source, ast, comptime_if.then_block);
    try dump_node(source, ast, comptime_if.else_block);
    return;
}

fn dump_range(
    source: []u8,
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
    source: []u8,
    ast: std::kizu::ast::Ast,
    expr_stmt: &std::kizu::ast::ExprStmtNode
) -> !void {
    print("ExprStmt");
    try dump_node(source, ast, expr_stmt.expr);
    return;
}

fn dump_leaf(
    kind: []u8,
    source: []u8,
    span: &std::kizu::ast::Span
) -> !void {
    print(kind);
    let text = try std::mem::slice(source, span.start, span.end);
    print(text);
    return;
}

fn dump_string(
    source: []u8,
    span: &std::kizu::ast::Span
) -> !void {
    print("String");
    if source[span.start] == cast<u8>(34) {
        let text = try std::mem::slice(source, span.start + 1, span.end - 1);
        print(text);
        return;
    }
    let allocator = std::mem::page_allocator();
    var value = std::string::String(allocator);
    var index = span.start;
    var first = true;
    while index < span.end {
        index = index + 2;
        let segment_start = index;
        while index < span.end and source[index] != cast<u8>(10) {
            index = index + 1;
        }
        if first {
            first = false;
        } else {
            try value.append_byte(cast<u8>(10));
        }
        let segment = try std::mem::slice(source, segment_start, index);
        try value.append_bytes(segment);
        if index < span.end {
            index = index + 1;
            while index < span.end and (source[index] == cast<u8>(32) or
                source[index] == cast<u8>(9) or source[index] == cast<u8>(13)) {
                index = index + 1;
            }
        }
    }
    let value_bytes = value.as_bytes();
    print(value_bytes);
    value.deinit();
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

// TestStdKizuParserParsesFrontendStdSources gates std sources parsed by the Kizu-written parser.
func TestStdKizuParserParsesFrontendStdSources(t *testing.T) {
	cases := collectParserFrontendStdSources(t)
	runStdKizuParserParityHarness(t, cases)
	t.Logf("frontend std sources parsed=%d", len(cases))
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

// collectParserFrontendStdSources returns std files the std::kizu frontend can demand-load.
func collectParserFrontendStdSources(t *testing.T) []parserParityCase {
	t.Helper()
	cases := make([]parserParityCase, 0, len(parserParityFrontendStdPaths))
	for _, rel := range parserParityFrontendStdPaths {
		path := filepath.Join(parserParityStdRoot, rel)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, parserParityCase{
			name:   parserParityCaseName(parserParityStdRoot, "std", path),
			source: string(source),
		})
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	return cases
}

// parserParityExampleCase summarizes one example when both parser subsets can handle it.
func parserParityExampleCase(path string) (parserParityCase, bool, string, []string) {
	return parserParityFileCase(path, parserParityExamplesRoot, "examples")
}

// parserParityFileCase summarizes one source file when both parser subsets can handle it.
func parserParityFileCase(
	path string,
	root string,
	prefix string,
) (parserParityCase, bool, string, []string) {
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		return parserParityCase{}, false, err.Error(), nil
	}
	source := string(sourceBytes)
	want, reason, parseErrs := summarizeGoParserSubset(source)
	if len(parseErrs) > 0 || reason != "" {
		return parserParityCase{name: parserParityCaseName(root, prefix, path)}, false, reason, parseErrs
	}
	return parserParityCase{
		name:   parserParityCaseName(root, prefix, path),
		source: source,
		want:   want,
	}, true, "", nil
}

// parserParityExampleName returns a stable corpus name for an example file.
func parserParityExampleName(path string) string {
	return parserParityCaseName(parserParityExamplesRoot, "examples", path)
}

// parserParityCaseName returns a stable corpus name for a file under root.
func parserParityCaseName(root string, prefix string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return prefix + "/" + filepath.ToSlash(rel)
}

// parserParitySeedCases provides stable positive coverage for the current subset.
func parserParitySeedCases(t *testing.T) []parserParityCase {
	t.Helper()
	seeds := parserParityFunctionSeedCases()
	seeds = append(seeds, parserParityExpressionSeedCases()...)
	seeds = append(seeds, parserParityDeclarationSeedCases()...)
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

// parserParityFunctionSeedCases covers function and statement parser parity.
func parserParityFunctionSeedCases() []parserParityCase {
	seeds := parserParityFunctionSignatureSeedCases()
	return append(seeds, parserParityFunctionBodySeedCases()...)
}

// parserParityFunctionSignatureSeedCases covers function signature parser parity.
func parserParityFunctionSignatureSeedCases() []parserParityCase {
	return []parserParityCase{
		{name: "seed/fn_empty", source: "fn main() {}"},
		{name: "seed/two_fns", source: "fn one() {} fn two() {}"},
		{name: "seed/fn_params_return", source: "fn add(a: i64, b: i64) -> i64 { return a + b; }"},
		{name: "seed/fn_error_union_return", source: "fn main() -> !void {}"},
		{name: "seed/fn_typed_error_union_return", source: "fn main() -> ConfigError!void {}"},
		{name: "seed/fn_slice_param", source: "fn write(bytes: []u8) {}"},
		{name: "seed/fn_borrow_param", source: "fn read(value: &i64) {}"},
		{name: "seed/fn_mut_borrow_param", source: "fn fill(out: &var i64) {}"},
		{name: "seed/fn_comptime_param", source: "fn scoped(comptime worker: Function) {}"},
		{name: "seed/fn_type_params", source: "pub fn identity<T>(value: T) -> T { return value; }"},
		{
			name:   "seed/fn_slice_return",
			source: "fn first(bytes: []u8) -> []u8 { return bytes; }",
		},
		{
			name:   "seed/fn_borrow_return_provenance",
			source: "fn first(bytes: []u8) -> []u8 borrows bytes { return bytes; }",
		},
		{
			name:   "seed/fn_slice_borrow_param",
			source: "fn show(bytes: &[]u8) {}",
		},
		{name: "seed/fn_namespace_type", source: "fn use(allocator: std::mem::Allocator) {}"},
		{
			name:   "seed/fn_generic_type",
			source: "fn collect(items: std::array::Array<[]u8>) {}",
		},
	}
}

// parserParityFunctionBodySeedCases covers function body parser parity.
func parserParityFunctionBodySeedCases() []parserParityCase {
	return []parserParityCase{
		{name: "seed/fn_return_int", source: "fn main() { return 1; }"},
		{name: "seed/fn_defer_cleanup", source: "fn main() { defer values.deinit(); return; }"},
		{
			name:   "seed/fn_errdefer_cleanup",
			source: "fn main() { errdefer values.deinit(); return; }",
		},
		{
			name:   "seed/fn_defer_and_errdefer",
			source: "fn main() { defer commit(); errdefer rollback(); return; }",
		},
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
		{
			name:   "seed/fn_return_void_try",
			source: "fn step() -> !void { return; } fn main() -> !void { try step(); return; }",
		},
		{
			name: "seed/fn_let_assignment_bool_qualified_call",
			source: "fn main() -> !void { let ok = true; var age = 30; " +
				"age = age + 1; std::testing::expect(ok); return; }",
		},
		{
			name: "seed/fn_if_else_logical",
			source: "fn main() { let age = 20; if age >= 20 and true { " +
				"print(\"adult\"); } else { print(\"minor\"); } }",
		},
		{
			name: "seed/fn_while_break_continue_label",
			source: "fn main() { var i = 0; outer: while i < 3 { i = i + 1; " +
				"if i == 2 { continue; } break :outer; } }",
		},
		{name: "seed/fn_for_range", source: "fn main() { for 0..5 |i| { print(i); } }"},
		{
			name: "seed/fn_match_statement",
			source: "fn main() { let color = Color::Blue; match color { " +
				"Red => print(\"red\");, Blue(value) => print(value);, } }",
		},
		{
			name: "seed/fn_unsafe_comptime_if",
			source: "fn main() { @unsafe(ptr_read) { print(1); } comptime if 1 + 1 == 2 { " +
				"print(comptime 8); } else { print(0); } }",
		},
	}
}

// parserParityExpressionSeedCases covers expression forms needed by std::kizu source.
func parserParityExpressionSeedCases() []parserParityCase {
	return []parserParityCase{
		{
			name:   "seed/fn_type_apply_call",
			source: "fn main() { let xs = std::array::Array<i64>(allocator); }",
		},
		{name: "seed/fn_cast_expr", source: "fn main() { let byte = cast<u8>(48); }"},
		{name: "seed/fn_deref_field", source: "fn main(value: &User) { print(value.*.name); }"},
		{
			name:   "seed/fn_index_and_slice",
			source: "fn main() { let item = bytes[0]; let part = bytes[1..3]; }",
		},
		{
			name:   "seed/fn_struct_literal",
			source: `fn main() { let user = User { name: "a", age: 1 }; }`,
		},
		{
			name:   "seed/fn_arena_type_apply_call",
			source: "fn main() { let nodes = std::arena::Arena<Node>(allocator); }",
		},
		{
			name: "seed/fn_match_expression",
			source: "fn main() { let color = Color::Green; " +
				`let name = match color { Red => "red", Green => "green", }; }`,
		},
		{
			name:   "seed/fn_multiline_string",
			source: "fn banner() -> []u8 {\n    return\n\\\\hello world\n;\n}",
		},
		{
			name:   "seed/fn_multiline_string_join",
			source: "fn banner() -> []u8 {\n    return\n\\\\foo\n\\\\bar\n;\n}",
		},
		{
			name:   "seed/fn_multiline_string_indent",
			source: "fn banner() -> []u8 {\n    return\n\\\\foo\n    \\\\bar\n;\n}",
		},
	}
}

// parserParityDeclarationSeedCases covers top-level declaration parser parity.
func parserParityDeclarationSeedCases() []parserParityCase {
	seeds := []parserParityCase{
		{name: "seed/import_decl", source: "import app::lexer;"},
		{
			name:   "seed/test_block",
			source: `test "basic" { std::testing::expect(true); }`,
		},
		{
			name:   "seed/pub_struct_decl",
			source: "pub struct User { pub name: []u8, age: i64, }",
		},
		{
			name:   "seed/generic_struct_decl",
			source: "struct Row<T> { data: []T, }",
		},
		{name: "seed/enum_decl", source: "enum Color { Red, Blue, }"},
		{name: "seed/union_decl", source: "union Shape { Point, Circle(i64), }"},
		{name: "seed/extern_fn", source: `extern "c" fn puts(s: ptr<const u8>) -> i32`},
		{name: "seed/requires_unsafe_fn", source: "@requires_unsafe() fn poke() {}"},
		{
			name:   "seed/inherent_impl_decl",
			source: "impl User { fn deinit(self: User) -> void { return; } }",
		},
		{
			name:   "seed/requires_unsafe_impl_method",
			source: "impl Register { @requires_unsafe() fn write(self: Register) -> void { return; } }",
		},
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
		// Comment bytes are trivia to std::kizu::lexer, which drops them in
		// skip_line_comment, so scanning them here rejects sources the lexer
		// handles fine. Prose is not the subset: a `#` in an issue reference is
		// the byte that surfaced this, and an apostrophe or a lone quote in a
		// comment would have desynchronized the string tracking above.
		if r == '/' && index+1 < len(source) && source[index+1] == '/' {
			index = stdParserLineCommentEnd(source, index) - 1
			continue
		}
		if r == '\\' && index+1 < len(source) && source[index+1] == '\\' {
			end := lexerParityStringTokenEnd(source, index)
			if end <= index {
				return "source contains tokens outside std parser subset"
			}
			index = end - 1
			continue
		}
		if isStdParserScannableRune(r) {
			continue
		}
		return "source contains tokens outside std parser subset"
	}
	if inString {
		return "unterminated string literal"
	}
	return ""
}

// isStdParserScannableRune reports a byte the std lexer can scan outside strings
// and comments: trivia, punctuation, or an identifier/number byte.
func isStdParserScannableRune(r rune) bool {
	return isStdParserSpace(r) || isStdParserPunctuation(r) || isStdParserWordRune(r)
}

// stdParserLineCommentEnd returns the index just past a `//` comment, which the
// std lexer ends at the newline or at end of source. Kizu has line comments only,
// so there is no block form to close.
func stdParserLineCommentEnd(source string, start int) int {
	if next := strings.IndexByte(source[start:], '\n'); next >= 0 {
		return start + next
	}
	return len(source)
}

// summarizeProgramSubset summarizes a complete Go AST program when it is in subset.
func summarizeProgramSubset(program *kizuast.Program) ([]string, string) {
	if len(program.Decls) == 0 {
		return nil, "program is empty"
	}
	lines := []string{"Program", "Range", strconv.Itoa(len(program.Decls))}
	for _, decl := range program.Decls {
		next, reason := summarizeDeclSubset(decl)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeDeclSubset summarizes one top-level declaration in the shared subset.
func summarizeDeclSubset(decl kizuast.Decl) ([]string, string) {
	switch node := decl.(type) {
	case *kizuast.ImportDecl:
		return summarizeImportDeclSubset(node)
	case *kizuast.FunctionDecl:
		return summarizeFunctionSubset(node)
	case *kizuast.TestDecl:
		return summarizeTestDeclSubset(node)
	case *kizuast.StructDecl:
		return summarizeStructDeclSubset(node)
	case *kizuast.EnumDecl:
		return summarizeEnumDeclSubset(node)
	case *kizuast.UnionDecl:
		return summarizeUnionDeclSubset(node)
	case *kizuast.ImplDecl:
		return summarizeImplDeclSubset(node)
	case *kizuast.ContractDecl:
		return nil, "contract declaration outside std parser subset"
	default:
		return nil, "top-level declaration outside std parser subset"
	}
}

// summarizeTestDeclSubset matches the std parser's synthetic FnDecl for test blocks.
func summarizeTestDeclSubset(decl *kizuast.TestDecl) ([]string, string) {
	body, reason := summarizeBlockSubset(decl.Body)
	if reason != "" {
		return nil, reason
	}
	lines := []string{
		"FnDecl",
		"Private",
		"Safe",
		"Empty",
		"String",
		decl.Name,
		"Range",
		"0",
		"Range",
		"0",
		"Empty",
		"Empty",
	}
	return append(lines, body...), ""
}

// summarizeImplDeclSubset summarizes inherent impl methods in the shared subset.
func summarizeImplDeclSubset(decl *kizuast.ImplDecl) ([]string, string) {
	if decl.ContractName != "" {
		return nil, "contract declaration outside std parser subset"
	}
	if !isStdParserIdent(decl.TypeName) {
		return nil, "identifier outside std parser subset"
	}
	lines := []string{"ImplDecl", "Var", decl.TypeName, "Range", strconv.Itoa(len(decl.Methods))}
	for _, method := range decl.Methods {
		next, reason := summarizeFunctionSubset(method)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeImportDeclSubset summarizes one explicit import declaration.
func summarizeImportDeclSubset(decl *kizuast.ImportDecl) ([]string, string) {
	lines := []string{"ImportDecl", "Range", strconv.Itoa(len(decl.Path))}
	for _, segment := range decl.Path {
		if !isStdParserIdent(segment) {
			return nil, "identifier outside std parser subset"
		}
		lines = append(lines, "Var", segment)
	}
	return lines, ""
}

// summarizeFunctionSubset summarizes a function declaration in the shared subset.
func summarizeFunctionSubset(fn *kizuast.FunctionDecl) ([]string, string) {
	typeParams, reason := summarizeGenericParamsSubset(fn.TypeParamNames())
	if reason != "" {
		return nil, reason
	}
	params, reason := summarizeParamsSubset(fn.Params)
	if reason != "" {
		return nil, reason
	}
	returnType, reason := summarizeReturnTypeSubset(fn.ReturnType)
	if reason != "" {
		return nil, reason
	}
	returnBorrow, reason := summarizeReturnBorrowSubset(fn.ReturnBorrow)
	if reason != "" {
		return nil, reason
	}
	body, reason := summarizeFunctionBodySubset(fn)
	if reason != "" {
		return nil, reason
	}
	lines := []string{
		"FnDecl",
		parserParityVisibility(fn.Public),
		parserParitySafety(fn.RequiresUnsafe),
	}
	lines = append(lines, summarizeExternABISubset(fn.ExternABI)...)
	lines = append(lines, "Var", fn.Name)
	lines = append(lines, typeParams...)
	lines = append(lines, params...)
	lines = append(lines, returnType...)
	lines = append(lines, returnBorrow...)
	return append(lines, body...), ""
}

// summarizeReturnBorrowSubset summarizes an optional borrowed-return source.
func summarizeReturnBorrowSubset(source string) ([]string, string) {
	if source == "" {
		return []string{"Empty"}, ""
	}
	if !isStdParserIdent(source) {
		return nil, "identifier outside std parser subset"
	}
	return []string{"Var", source}, ""
}

// summarizeFunctionBodySubset summarizes a required body or extern empty body.
func summarizeFunctionBodySubset(fn *kizuast.FunctionDecl) ([]string, string) {
	if fn.Body == nil && fn.ExternABI == "" {
		return nil, "function has no body"
	}
	if fn.Body == nil {
		return []string{"Empty"}, ""
	}
	return summarizeBlockSubset(fn.Body)
}

// summarizeGenericParamsSubset summarizes type parameter names.
func summarizeGenericParamsSubset(params []string) ([]string, string) {
	lines := []string{"Range", strconv.Itoa(len(params))}
	for _, name := range params {
		if !isStdParserIdent(name) {
			return nil, "identifier outside std parser subset"
		}
		lines = append(lines, "Var", name)
	}
	return lines, ""
}

// summarizeExternABISubset summarizes an optional extern ABI string.
func summarizeExternABISubset(abi string) []string {
	if abi == "" {
		return []string{"Empty"}
	}
	return []string{"String", abi}
}

// parserParityVisibility maps declaration visibility to summary labels.
func parserParityVisibility(public bool) string {
	if public {
		return "Public"
	}
	return "Private"
}

// parserParitySafety maps caller-obligation declaration metadata to summary labels.
func parserParitySafety(unsafe bool) string {
	if unsafe {
		return "RequiresUnsafe"
	}
	return "Safe"
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
	if !isStdParserIdent(param.Name) {
		return nil, "identifier outside std parser subset"
	}
	typeName, reason := summarizeTypeNameSubset(parserParityParamTypeName(param))
	if reason != "" {
		return nil, reason
	}
	mode := "Runtime"
	if param.Comptime {
		mode = "Comptime"
	}
	lines := []string{"Param", mode, "Var", param.Name}
	return append(lines, typeName...), ""
}

// summarizeStructDeclSubset summarizes a top-level struct declaration.
func summarizeStructDeclSubset(decl *kizuast.StructDecl) ([]string, string) {
	if !isStdParserIdent(decl.Name) {
		return nil, "identifier outside std parser subset"
	}
	typeParams, reason := summarizeGenericParamsSubset(decl.TypeParams)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"StructDecl", parserParityVisibility(decl.Public), "Var", decl.Name}
	lines = append(lines, typeParams...)
	lines = append(lines, "Range", strconv.Itoa(len(decl.Fields)))
	for _, field := range decl.Fields {
		next, reason := summarizeFieldSubset(field)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeFieldSubset summarizes one named struct field.
func summarizeFieldSubset(field kizuast.Field) ([]string, string) {
	if !isStdParserIdent(field.Name) {
		return nil, "identifier outside std parser subset"
	}
	typeName, reason := summarizeTypeNameSubset(parserParityFieldTypeName(field))
	if reason != "" {
		return nil, reason
	}
	lines := []string{"Field", parserParityVisibility(field.Public), "Var", field.Name}
	return append(lines, typeName...), ""
}

// parserParityFieldTypeName returns the Go parser spelling used for field types.
func parserParityFieldTypeName(field kizuast.Field) string {
	switch {
	case field.MutBorrow:
		return "&var " + field.TypeName
	case field.Borrow:
		return "&" + field.TypeName
	default:
		return field.TypeName
	}
}

// summarizeEnumDeclSubset summarizes a top-level tag enum declaration.
func summarizeEnumDeclSubset(decl *kizuast.EnumDecl) ([]string, string) {
	if !isStdParserIdent(decl.Name) {
		return nil, "identifier outside std parser subset"
	}
	lines := []string{"EnumDecl", parserParityVisibility(decl.Public), "Var", decl.Name}
	lines = append(lines, "Range", strconv.Itoa(len(decl.Tags)))
	for _, tag := range decl.Tags {
		if !isStdParserIdent(tag) {
			return nil, "identifier outside std parser subset"
		}
		lines = append(lines, "Var", tag)
	}
	return lines, ""
}

// summarizeUnionDeclSubset summarizes a top-level tagged union declaration.
func summarizeUnionDeclSubset(decl *kizuast.UnionDecl) ([]string, string) {
	if !isStdParserIdent(decl.Name) {
		return nil, "identifier outside std parser subset"
	}
	typeParams, reason := summarizeGenericParamsSubset(decl.TypeParams)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"UnionDecl", parserParityVisibility(decl.Public), "Var", decl.Name}
	lines = append(lines, typeParams...)
	lines = append(lines, "Range", strconv.Itoa(len(decl.Variants)))
	for _, variant := range decl.Variants {
		next, reason := summarizeUnionVariantSubset(variant)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeUnionVariantSubset summarizes one tagged union variant.
func summarizeUnionVariantSubset(variant kizuast.UnionVariant) ([]string, string) {
	if !isStdParserIdent(variant.Name) {
		return nil, "identifier outside std parser subset"
	}
	lines := []string{"UnionVariant", "Var", variant.Name}
	if variant.Payload == "" {
		return append(lines, "Empty"), ""
	}
	payload, reason := summarizeTypeNameSubset(variant.Payload)
	if reason != "" {
		return nil, reason
	}
	return append(lines, payload...), ""
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
		return "&var " + param.TypeName
	case param.Borrow:
		return "&" + param.TypeName
	default:
		return param.TypeName
	}
}

// summarizeBlockSubset summarizes one statement block.
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
	case *kizuast.LetStmt:
		return summarizeLetSubset(node)
	case *kizuast.AssignStmt:
		return summarizeAssignSubset(node)
	case *kizuast.ReturnStmt:
		return summarizeReturnSubset(node)
	case *kizuast.DeferStmt:
		return summarizeDeferSubset(node)
	case *kizuast.ErrDeferStmt:
		return summarizeErrDeferSubset(node)
	case *kizuast.ExprStmt:
		return summarizeExprStmtSubset(node)
	case *kizuast.IfStmt:
		return summarizeIfSubset(node)
	case *kizuast.WhileStmt:
		return summarizeWhileSubset(node)
	case *kizuast.ForStmt:
		return summarizeForSubset(node)
	default:
		return summarizeStatementSubsetExtra(stmt)
	}
}

// summarizeStatementSubsetExtra summarizes less common statement forms.
func summarizeStatementSubsetExtra(stmt kizuast.Statement) ([]string, string) {
	switch node := stmt.(type) {
	case *kizuast.BreakStmt:
		return append([]string{"Break"}, summarizeOptionalName(node.Label)...), ""
	case *kizuast.ContinueStmt:
		return append([]string{"Continue"}, summarizeOptionalName(node.Label)...), ""
	case *kizuast.MatchStmt:
		return summarizeMatchSubset(node)
	case *kizuast.UnsafeStmt:
		return summarizeUnsafeSubset(node)
	case *kizuast.ComptimeIfStmt:
		return summarizeComptimeIfSubset(node)
	default:
		return nil, "statement outside std parser subset"
	}
}

// summarizeUnsafeSubset summarizes unsafe capability blocks.
func summarizeUnsafeSubset(node *kizuast.UnsafeStmt) ([]string, string) {
	body, reason := summarizeBlockSubset(node.Body)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"Unsafe", "Range", strconv.Itoa(len(node.Capabilities))}
	for _, capability := range node.Capabilities {
		lines = append(lines, "Var", capability)
	}
	return append(lines, body...), ""
}

// summarizeLetSubset summarizes let and var declarations.
func summarizeLetSubset(node *kizuast.LetStmt) ([]string, string) {
	value, reason := summarizeExprSubset(node.Value)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"Let", parserParityMutability(node.Mutable), "Var", node.Name}
	return append(lines, value...), ""
}

// summarizeAssignSubset summarizes assignment statements.
func summarizeAssignSubset(node *kizuast.AssignStmt) ([]string, string) {
	target, reason := summarizeExprSubset(node.Target)
	if reason != "" {
		return nil, reason
	}
	value, reason := summarizeExprSubset(node.Value)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"Assign"}, target...)
	return append(lines, value...), ""
}

// summarizeReturnSubset summarizes explicit return statements.
func summarizeReturnSubset(node *kizuast.ReturnStmt) ([]string, string) {
	if node.Value == nil {
		return []string{"Return", "Empty"}, ""
	}
	value, reason := summarizeExprSubset(node.Value)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"Return"}, value...), ""
}

// summarizeDeferSubset summarizes deferred cleanup statements.
func summarizeDeferSubset(node *kizuast.DeferStmt) ([]string, string) {
	value, reason := summarizeExprSubset(node.Expr)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"Defer"}, value...), ""
}

// summarizeErrDeferSubset summarizes error-path cleanup statements.
func summarizeErrDeferSubset(node *kizuast.ErrDeferStmt) ([]string, string) {
	value, reason := summarizeExprSubset(node.Expr)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"ErrDefer"}, value...), ""
}

// summarizeExprStmtSubset summarizes expression statements.
func summarizeExprStmtSubset(node *kizuast.ExprStmt) ([]string, string) {
	value, reason := summarizeExprSubset(node.Expr)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"ExprStmt"}, value...), ""
}

// parserParityMutability maps let/var mutability to summary labels.
func parserParityMutability(mutable bool) string {
	if mutable {
		return "Mutable"
	}
	return "Immutable"
}

// summarizeOptionalName summarizes optional labels and bindings.
func summarizeOptionalName(name string) []string {
	if name == "" {
		return []string{"Empty"}
	}
	return []string{"Var", name}
}

// summarizeIfSubset summarizes a statement-position if branch.
func summarizeIfSubset(node *kizuast.IfStmt) ([]string, string) {
	condition, reason := summarizeExprSubset(node.Condition)
	if reason != "" {
		return nil, reason
	}
	consequence, reason := summarizeBlockSubset(node.Consequence)
	if reason != "" {
		return nil, reason
	}
	alternative, reason := summarizeOptionalBlockSubset(node.Alternative)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"If"}, condition...)
	lines = append(lines, consequence...)
	return append(lines, alternative...), ""
}

// summarizeComptimeIfSubset summarizes a comptime-selected statement branch.
func summarizeComptimeIfSubset(node *kizuast.ComptimeIfStmt) ([]string, string) {
	condition, reason := summarizeExprSubset(node.Condition)
	if reason != "" {
		return nil, reason
	}
	consequence, reason := summarizeBlockSubset(node.Consequence)
	if reason != "" {
		return nil, reason
	}
	alternative, reason := summarizeOptionalBlockSubset(node.Alternative)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"ComptimeIf"}, condition...)
	lines = append(lines, consequence...)
	return append(lines, alternative...), ""
}

// summarizeOptionalBlockSubset summarizes a block or the AST Empty sentinel.
func summarizeOptionalBlockSubset(block *kizuast.BlockStmt) ([]string, string) {
	if block == nil {
		return []string{"Empty"}, ""
	}
	return summarizeBlockSubset(block)
}

// summarizeWhileSubset summarizes a while loop.
func summarizeWhileSubset(node *kizuast.WhileStmt) ([]string, string) {
	condition, reason := summarizeExprSubset(node.Condition)
	if reason != "" {
		return nil, reason
	}
	body, reason := summarizeBlockSubset(node.Body)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"While"}, summarizeOptionalName(node.Label)...)
	lines = append(lines, condition...)
	return append(lines, body...), ""
}

// summarizeForSubset summarizes a bounded range loop.
func summarizeForSubset(node *kizuast.ForStmt) ([]string, string) {
	start, reason := summarizeExprSubset(node.Start)
	if reason != "" {
		return nil, reason
	}
	end, reason := summarizeExprSubset(node.End)
	if reason != "" {
		return nil, reason
	}
	body, reason := summarizeBlockSubset(node.Body)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"For"}, summarizeOptionalName(node.Label)...)
	lines = append(lines, "Var", node.Name)
	lines = append(lines, start...)
	lines = append(lines, end...)
	return append(lines, body...), ""
}

// summarizeMatchSubset summarizes a simple match statement.
func summarizeMatchSubset(node *kizuast.MatchStmt) ([]string, string) {
	value, reason := summarizeExprSubset(node.Value)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"Match"}, value...)
	lines = append(lines, "Range", strconv.Itoa(len(node.Arms)))
	for _, arm := range node.Arms {
		next, reason := summarizeMatchArmSubset(arm)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeMatchArmSubset summarizes one tag arm.
func summarizeMatchArmSubset(arm kizuast.MatchArm) ([]string, string) {
	body, reason := summarizeStatementSubset(arm.Body)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"MatchArm", "Var", arm.Tag}
	lines = append(lines, summarizeOptionalName(arm.Binding)...)
	return append(lines, body...), ""
}

// summarizeExprSubset summarizes expressions supported by std::kizu::parser.
func summarizeExprSubset(expr kizuast.Expression) ([]string, string) {
	switch node := expr.(type) {
	case *kizuast.BinaryExpr:
		return summarizeBinarySubset(node)
	case *kizuast.PrefixExpr:
		return summarizePrefixSubset(node)
	case *kizuast.TryExpr:
		value, reason := summarizeExprSubset(node.Value)
		if reason != "" {
			return nil, reason
		}
		return append([]string{"TryExpr"}, value...), ""
	case *kizuast.ComptimeExpr:
		value, reason := summarizeExprSubset(node.Expr)
		if reason != "" {
			return nil, reason
		}
		return append([]string{"ComptimeExpr"}, value...), ""
	case *kizuast.DerefExpr:
		return summarizeDerefExprSubset(node)
	case *kizuast.IfStmt:
		return summarizeIfSubset(node)
	case *kizuast.MatchStmt:
		return summarizeMatchSubset(node)
	default:
		return summarizePrimarySubset(expr)
	}
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
	case "-":
		return "Sub", true
	case "*":
		return "Mul", true
	case "/":
		return "Div", true
	case "%":
		return "Mod", true
	case "==":
		return "Eq", true
	case "!=":
		return "NotEq", true
	case "<":
		return "LT", true
	case "<=":
		return "LTE", true
	case ">":
		return "GT", true
	case ">=":
		return "GTE", true
	case "and":
		return "And", true
	case "or":
		return "Or", true
	default:
		return "", false
	}
}

// summarizePrefixSubset summarizes prefix operations.
func summarizePrefixSubset(expr *kizuast.PrefixExpr) ([]string, string) {
	op, ok := parserParityPrefixOp(expr.Operator)
	if !ok {
		return nil, "prefix operator outside std parser subset"
	}
	right, reason := summarizeExprSubset(expr.Right)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"Prefix", op}, right...), ""
}

// parserParityPrefixOp maps shared prefix operators to summary labels.
func parserParityPrefixOp(op string) (string, bool) {
	switch op {
	case "!":
		return "Not", true
	case "-":
		return "Neg", true
	case "&":
		return "Borrow", true
	case "&var":
		return "MutBorrow", true
	default:
		return "", false
	}
}

// summarizePrimarySubset summarizes primary expressions and calls.
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
	case *kizuast.BoolExpr:
		if node.Value {
			return []string{"Bool", "true"}, ""
		}
		return []string{"Bool", "false"}, ""
	case *kizuast.FieldExpr:
		return summarizeFieldExprSubset(node)
	case *kizuast.CallExpr:
		return summarizeCallSubset(node)
	case *kizuast.TypeApplyExpr:
		return summarizeTypeApplyExprSubset(node)
	case *kizuast.CastExpr:
		return summarizeCastExprSubset(node)
	case *kizuast.IndexExpr:
		return summarizeIndexExprSubset(node)
	case *kizuast.StructLiteralExpr:
		return summarizeStructLiteralExprSubset(node)
	case *kizuast.ArenaNewExpr:
		return summarizeArenaNewExprSubset(node)
	default:
		return nil, "expression outside std parser subset"
	}
}

// summarizeFieldExprSubset summarizes namespace and field selection.
func summarizeFieldExprSubset(expr *kizuast.FieldExpr) ([]string, string) {
	receiver, reason := summarizeExprSubset(expr.Receiver)
	if reason != "" {
		return nil, reason
	}
	mode := "Field"
	if expr.Namespace {
		mode = "Namespace"
	}
	lines := []string{"FieldExpr", mode}
	lines = append(lines, receiver...)
	return append(lines, "Var", expr.Name), ""
}

// summarizeDerefExprSubset summarizes explicit postfix dereference.
func summarizeDerefExprSubset(expr *kizuast.DerefExpr) ([]string, string) {
	receiver, reason := summarizeExprSubset(expr.Receiver)
	if reason != "" {
		return nil, reason
	}
	return append([]string{"DerefExpr"}, receiver...), ""
}

// summarizeCallSubset summarizes calls with a supported callee expression.
func summarizeCallSubset(expr *kizuast.CallExpr) ([]string, string) {
	callee, reason := summarizeExprSubset(expr.Callee)
	if reason != "" {
		return nil, "call callee outside std parser subset"
	}
	lines := append([]string{"Call"}, callee...)
	lines = append(lines, "Range", strconv.Itoa(len(expr.Args)))
	for _, arg := range expr.Args {
		next, reason := summarizeExprSubset(arg)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeTypeApplyExprSubset summarizes namespace generic constructor selection.
func summarizeTypeApplyExprSubset(expr *kizuast.TypeApplyExpr) ([]string, string) {
	callee, reason := summarizeExprSubset(expr.Callee)
	if reason != "" {
		return nil, reason
	}
	args, reason := summarizeTypeArgListSubset(expr.TypeArg)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"TypeApplyExpr"}, callee...)
	return append(lines, args...), ""
}

// summarizeTypeArgListSubset summarizes comma-separated generic arguments.
func summarizeTypeArgListSubset(typeArgs string) ([]string, string) {
	args := splitTopLevelTypeArgs(typeArgs)
	if len(args) == 0 {
		return nil, "type outside std parser subset"
	}
	lines := []string{"Range", strconv.Itoa(len(args))}
	for _, arg := range args {
		next, reason := summarizeTypeNameSubset(arg)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// splitTopLevelTypeArgs splits a parser type argument list at outer commas.
func splitTopLevelTypeArgs(typeArgs string) []string {
	args := []string{}
	depth := 0
	start := 0
	for index, r := range typeArgs {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(typeArgs[start:index]))
				start = index + 1
			}
		}
	}
	last := strings.TrimSpace(typeArgs[start:])
	if last != "" {
		args = append(args, last)
	}
	return args
}

// summarizeCastExprSubset summarizes cast<T>(value).
func summarizeCastExprSubset(expr *kizuast.CastExpr) ([]string, string) {
	target, reason := summarizeTypeNameSubset(expr.TargetType)
	if reason != "" {
		return nil, reason
	}
	value, reason := summarizeExprSubset(expr.Value)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"CastExpr"}, target...)
	return append(lines, value...), ""
}

// summarizeIndexExprSubset summarizes index and one-dimensional slice expressions.
func summarizeIndexExprSubset(expr *kizuast.IndexExpr) ([]string, string) {
	target, reason := summarizeExprSubset(expr.Target)
	if reason != "" {
		return nil, reason
	}
	mode := "Index"
	startExpr := expr.Index
	var endExpr kizuast.Expression
	if expr.Slice {
		mode = "Slice"
		startExpr = expr.Start
		endExpr = expr.End
	}
	start, reason := summarizeOptionalExprSubset(startExpr)
	if reason != "" {
		return nil, reason
	}
	end, reason := summarizeOptionalExprSubset(endExpr)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"IndexExpr", mode}
	lines = append(lines, target...)
	lines = append(lines, start...)
	return append(lines, end...), ""
}

// summarizeOptionalExprSubset summarizes an expression or the AST Empty sentinel.
func summarizeOptionalExprSubset(expr kizuast.Expression) ([]string, string) {
	if expr == nil {
		return []string{"Empty"}, ""
	}
	return summarizeExprSubset(expr)
}

// summarizeStructLiteralExprSubset summarizes Type { field: value }.
func summarizeStructLiteralExprSubset(expr *kizuast.StructLiteralExpr) ([]string, string) {
	typeName, reason := summarizeTypeExprNameSubset(expr.TypeName)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"StructLiteralExpr"}, typeName...)
	lines = append(lines, "Range", strconv.Itoa(len(expr.Fields)))
	for _, field := range expr.Fields {
		next, reason := summarizeStructFieldInitSubset(field)
		if reason != "" {
			return nil, reason
		}
		lines = append(lines, next...)
	}
	return lines, ""
}

// summarizeTypeExprNameSubset renders a namespace type name as expression nodes.
func summarizeTypeExprNameSubset(typeName string) ([]string, string) {
	parts := strings.Split(typeName, "::")
	if len(parts) == 0 || !isStdParserIdent(parts[0]) {
		return nil, "identifier outside std parser subset"
	}
	lines := []string{"Var", parts[0]}
	for _, part := range parts[1:] {
		if !isStdParserIdent(part) {
			return nil, "identifier outside std parser subset"
		}
		next := []string{"FieldExpr", "Namespace"}
		next = append(next, lines...)
		next = append(next, "Var", part)
		lines = next
	}
	return lines, ""
}

// summarizeStructFieldInitSubset summarizes one struct field initializer.
func summarizeStructFieldInitSubset(field kizuast.FieldValue) ([]string, string) {
	if !isStdParserIdent(field.Name) {
		return nil, "identifier outside std parser subset"
	}
	value, reason := summarizeExprSubset(field.Value)
	if reason != "" {
		return nil, reason
	}
	lines := []string{"StructFieldInit", "Var", field.Name}
	return append(lines, value...), ""
}

// summarizeArenaNewExprSubset summarizes the legacy arena constructor AST node.
func summarizeArenaNewExprSubset(expr *kizuast.ArenaNewExpr) ([]string, string) {
	typeName, reason := summarizeTypeNameSubset(expr.TypeName)
	if reason != "" {
		return nil, reason
	}
	allocator, reason := summarizeExprSubset(expr.Allocator)
	if reason != "" {
		return nil, reason
	}
	lines := append([]string{"ArenaNewExpr"}, typeName...)
	return append(lines, allocator...), ""
}

// isStdParserSpace reports whitespace understood by std::kizu::lexer.
func isStdParserSpace(r rune) bool {
	return r == ' ' || r == '\n' || r == '\t' || r == '\r'
}

// isStdParserPunctuation reports punctuation understood by std::kizu::lexer.
func isStdParserPunctuation(r rune) bool {
	return strings.ContainsRune("{}();,:!&[]<>?+*-=/>%.|'@", r)
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
	dir := t.TempDir()
	source, err := buildStdKizuParserParityHarness(t, dir, cases)
	if err != nil {
		t.Fatal(err)
	}
	binary := buildNativeParityHarness(t, dir, "std_kizu_parser_parity", source)
	out := runNativeParityHarness(t, binary)
	got, err := parseStdKizuParserParityOutput(out)
	if err != nil {
		t.Fatalf("invalid harness output: %v\n%s", err, tailForLog(out))
	}
	return got
}

// buildStdKizuParserParityHarness creates a Kizu program that parses all cases.
func buildStdKizuParserParityHarness(
	t *testing.T,
	dir string,
	cases []parserParityCase,
) (string, error) {
	t.Helper()
	var out strings.Builder
	out.WriteString(stdKizuParserParityHarness)
	out.WriteString("\nfn main() -> !void {\n")
	out.WriteString("    let allocator = std::mem::page_allocator();\n")
	out.WriteString("    let io = std::io::blocking();\n")
	for index, testCase := range cases {
		name, err := kizuRawStringLiteral(testCase.name)
		if err != nil {
			return "", fmt.Errorf("%s name: %w", testCase.name, err)
		}
		path, err := kizuRawStringLiteral(writeParityCaseFile(t, dir, index, testCase.source))
		if err != nil {
			return "", fmt.Errorf("%s path: %w", testCase.name, err)
		}
		fmt.Fprintf(&out, "    try run_case(allocator, io, %s, %s);\n", name, path)
	}
	out.WriteString("    return;\n}\n")
	return out.String(), nil
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
