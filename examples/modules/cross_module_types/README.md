# Cross-Module Type References

This package-shaped example mirrors the conformance fixture used for imported
type references and exercises the Kizu-written `std::kizu` compiler components.

Run it with:

```sh
kizu check examples/modules/cross_module_types
kizu test examples/modules/cross_module_types
```

`kizu.toml`:

```toml
[package]
name = "app"
version = "0.2.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
```

The parser implementation lives in stdlib source:

```text
std/src/kizu/ast.kizu
std/src/kizu/lexer.kizu
std/src/kizu/parser.kizu
```

`src/checks.kizu` keeps this package example multi-module while using
`std::kizu::*` types:

```kizu
pub fn accept_token(token: &std::kizu::lexer::Token) -> void {
    return;
}

pub fn expect_fn_decl(
    source: []const u8,
    node: &std::kizu::ast::Node,
    name: []const u8,
    start: i64,
    end: i64,
) -> !void {
    match node {
        FnDecl(span) => try expect_span(source, span, name, start, end);
        Ident(span) => return std::testing::fail("expected fn decl");
        Empty => return std::testing::fail("expected fn decl");
    }
    return;
}
```

`src/main.kizu` parses Kizu-like source text through stdlib:

```kizu
import app::checks;

pub fn main() -> !void {
    let source = "fn main";
    let token = std::kizu::lexer::first_token(source);
    checks::accept_token(&token);
    try std::testing::expect_equal_i64(0, token.start);
    try std::testing::expect_equal_i64(2, token.end);

    let node = try std::kizu::parser::parse_first_node(source);
    try checks::expect_fn_decl(source, &node, "main", 3, 7);

    let ident_source = "token";
    let ident = try std::kizu::parser::parse_first_node(ident_source);
    try checks::expect_ident(ident_source, &ident, "token", 0, 5);

    let indented = std::kizu::lexer::first_token("   lexer");
    try std::testing::expect_equal_i64(3, indented.start);
    try std::testing::expect_equal_i64(8, indented.end);

    let eof = std::kizu::lexer::first_token("   ");
    try std::testing::expect_equal_i64(3, eof.start);
    try std::testing::expect_equal_i64(3, eof.end);

    print(token.start);
    print(token.end);
    print("fn");
    print("main");
    print("token");
    print(indented.start);
    print(indented.end);
    print(eof.start);
    print(eof.end);
    return;
}
```
