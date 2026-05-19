# Kizu Selfhost Compiler Package

This package is the source-owned home for Kizu compiler components that will
eventually replace Go-owned frontend phases.

The package currently defines these source-owned modules:

- `selfhost::token`
- `selfhost::lexer`
- `selfhost::ast`
- `selfhost::parser`
- `selfhost::diagnostic`
- `selfhost::resolver`
- `selfhost::resolver_oracle`
- `selfhost::types`
- `selfhost::types_oracle`
- `selfhost::ownership`
- `selfhost::ir`
- `selfhost::backend`

Generated `target/selfhost*` artifacts are not source of truth. New selfhost
work should land in this package or in `std::kizu` when it is reusable stdlib
compiler infrastructure.

The token and lexer boundary is currently `std::kizu::lexer`. Selfhost compiler
modules should reuse that implementation instead of duplicating token shapes in
`selfhost::lexer`; the oracle suite compares the direct token stream and the
Array-backed `tokenize` path against the Go lexer.

The parser and AST boundary is currently `std::kizu::{ast, parser}`. Parser
success gates compare the Arena + NodeId AST summary for every `selfhost/src`
source file, and parser error gates keep recoverable `!T` failures readable.

The resolver boundary uses `std::map::Map<[]const u8, V>` for symbol and
visibility tables. Resolver diagnostics use `std::kizu::diagnostic` and the
oracle covers missing symbols, duplicate symbols, private access, and import
cycles.

The type checker boundary uses explicit type-kind, arity, and copyability maps.
The oracle covers primitive, function, struct, union, enum, error-union,
optional, and std-container seed shapes plus stable diagnostic spans.

Check the package with:

```sh
kizu check selfhost
```

Run the current Go/Kizu component oracle suite with:

```sh
just selfhost-oracle
```
