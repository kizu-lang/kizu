# Kizu Selfhost Compiler Package

This package is the source-owned home for Kizu compiler components that will
eventually replace Go-owned frontend phases.

The package currently defines these source-owned modules:

- `selfhost::token`
- `selfhost::lexer`
- `selfhost::lexer_oracle`
- `selfhost::ast`
- `selfhost::parser`
- `selfhost::parser_oracle`
- `selfhost::diagnostic`
- `selfhost::resolver`
- `selfhost::resolver_oracle`
- `selfhost::types`
- `selfhost::types_oracle`
- `selfhost::ownership`
- `selfhost::ownership_oracle`
- `selfhost::ir`
- `selfhost::backend`

Generated `target/selfhost*` artifacts are not source of truth. New selfhost
work should land in this package or in `std::kizu` when it is reusable stdlib
compiler infrastructure.

The token and lexer boundary is currently `std::kizu::lexer`. Selfhost compiler
modules reuse that implementation through `selfhost::lexer` instead of
duplicating token shapes. The oracle suite compares the direct token stream, the
Array-backed `tokenize` path, and the selfhost lexer component gate against the
Go lexer.

The parser and AST boundary is currently `std::kizu::{ast, parser}` through
`selfhost::{ast, parser}`. The selfhost parser consumes Kizu lexer token arrays,
returns structured Arena + NodeId `ParseResult` values, and exposes typed
diagnostic summaries for parser-owned errors. Lower-level untyped lexer, parser,
and container failures are explicitly adapted through typed error casts. Parser
success gates compare the Arena + NodeId AST summary for every `selfhost/src`
source file, and parser error gates keep recoverable `!T` failures readable.

The resolver boundary uses `std::map::Map<[]const u8, V>` for symbol and
visibility tables. Resolver diagnostics use `std::kizu::diagnostic` and the
oracle covers missing symbols, duplicate symbols, private access, and import
cycles.

The type checker boundary uses explicit type-kind, arity, and copyability maps.
The oracle covers primitive, function, struct, union, enum, error-union,
optional, and std-container seed shapes plus stable diagnostic spans.

The ownership boundary uses explicit resource-kind and ownership-state maps. The
oracle covers value, array, map, string, arena, handle, and borrowed-view seed
shapes plus stable memory-safety diagnostic spans.

Check the package with:

```sh
kizu check selfhost
```

Run the current Go/Kizu component oracle suite with:

```sh
just selfhost-oracle
```
