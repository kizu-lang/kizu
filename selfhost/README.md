# Kizu Self-Host Frontend Skeleton

This directory contains Kizu source for the future self-host compiler frontend.

The current goal is not to replace the Go implementation. The skeleton is a
small compatibility probe that verifies whether the v0.2 standard library is
enough to write compiler-shaped Kizu code.

## Current Entry

```sh
kizu run selfhost/frontend.kizu
kizu run selfhost/frontend.kizu -- selfhost/fixtures/simple.kizu
```

The skeleton currently includes:

- token representation
- Go-shaped token kinds and lexer-shaped source scanning with `std::mem`
- token storage with `std::array::Array<T>`
- diagnostic text construction with `std::string::String`
- parser summary shape
- compiler1-8 stage report covering lexer, parser, diagnostics, type, ownership,
  IR, backend artifact, and bootstrap readiness
- source loading through `std::fs`
- path decomposition through `std::path`
- prototype CLI argument handling through `std::process`
- explicit output through `std::io`
- Kizu-native component assertions through `std::testing`

The default smoke input is `selfhost/fixtures/simple.kizu`. Passing a path after
`--` exercises the same runner path with explicit process arguments.
`selfhost/fixtures/simple_tokens.kizu` is a wider lexer corpus used by Go tests
to compare the full self-host token-kind stream against the production lexer.

## v0.3 Handoff

The v0.2 standard-library bridge is complete when `tests/selfhost` can parse,
check, and run the skeleton through the same APIs a future compiler frontend
will need. #31 should build from this by replacing the normalized parser
snapshot with full AST, diagnostics, and conformance comparison components.

## Conformance Reuse

The future self-host compiler must reuse `tests/conformance/v0_*.json` and
produce the same pass/fail behavior as the Go implementation before it can
replace any production path.

For now, Go tests parse, check, and run the skeleton source. They compare the
self-host token snapshots with the production Go lexer and compare normalized
AST snapshots with the Go parser for representative parseable sources. Missing
APIs found while growing this directory must be reflected back into the relevant
stdlib issue instead of becoming isolated TODOs here.

## Compiler Stage Harness

`frontend.kizu` intentionally runs eight stages before it is a full compiler:

1. lexer
2. parser snapshot
3. diagnostics
4. type-check preconditions
5. ownership-check preconditions
6. IR item lowering summary
7. backend artifact summary
8. bootstrap readiness check

Stages 4-8 are still skeletons. They are kept executable so v0.2 stdlib and
language-core gaps are found before the self-host implementation replaces Go
components.

## Lexer And Parser Oracle

The self-host lexer oracle compares token kind, literal spelling, byte start,
byte end, line, and column against the Go lexer. Byte spans are zero-based and
end-exclusive. Line and column are one-based. String token literals exclude the
surrounding quotes so they match the Go `token.Token.Literal` field.

The parser oracle currently compares a normalized AST snapshot containing
function, import, struct, enum, union, and return counts. This is intentionally
smaller than a complete AST dump, but it is generated from the Go parser and the
self-host parser stage for the same source and is the v0.3 bridge toward full
AST oracle coverage.

Current stdlib feedback is tracked in [`STDLIB.md`](STDLIB.md).

## Source Policy

Kizu compiler source in this directory follows the stricter compiler-code style:

- module comment at the top of each file
- comment immediately before each function
- line width at most 100 columns
- function body at most 70 lines
- function body at most 45 semicolon-terminated statements
