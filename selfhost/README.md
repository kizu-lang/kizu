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
- `selfhost::source`
- `selfhost::source_oracle`
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
`std::kizu::ast::ParseResult` exposes helper methods for root/node/child access
so later compiler phases can traverse parsed ASTs without moving the owned
arena out of the parse result.

The source manager boundary uses explicit `std::fs`, `std::path`, and `std::io`
capabilities to load `kizu.toml`, `selfhost/src`, and the std sources required by
the selfhost frontend. The source table preserves source ids, source kind, module
name, file path, and loaded text. Source diagnostics use stable source ids,
paths, byte spans, line/column data, and related spans.

The resolver boundary consumes the source table, registers selfhost/std modules,
and scans top-level declarations into qualified symbol and visibility maps using
`std::map::Map<[]u8, V>`. Resolver diagnostics use
`std::kizu::diagnostic` and the oracle covers missing symbols, duplicate
symbols, private access, and import cycles.

The type checker boundary consumes the resolver source table, registers
selfhost/std declared types into explicit type-kind, arity, and copyability
maps, and validates signature, field, variant, cast, and generic-constructor
type references. The oracle covers the production source-table pass plus
primitive, function, struct, union, enum, error-union, optional, and
std-container seed shapes with stable diagnostic spans.

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
