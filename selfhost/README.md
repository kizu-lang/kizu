# Kizu Self-Host Compiler Package

This directory contains Kizu source for the module-first self-host compiler
package.

The Go implementation remains the production compiler while Kizu modules prove
each compiler component against the oracle tests. `frontend.kizu` is now a
legacy oracle harness; new compiler code belongs under `selfhost/src`.

Self-host migration is now module-first. The Go compiler keeps owning
multi-file package loading, module graph resolution, visibility diagnostics,
and package-level check/build behavior while Go compiler packages are ported to
Kizu modules one component at a time.

The module-first source layout is a checkable and executable package:

```text
selfhost/
  kizu.toml
  src/
    main.kizu
    token.kizu
    lexer.kizu
    ast.kizu
    parser.kizu
    diagnostics.kizu
    resolver.kizu
    types.kizu
    ownership.kizu
    ir.kizu
    backend.kizu
    cache_contract.kizu
```

The migration reset is tracked by #190. The component ports now cover token,
lexer, AST, parser, diagnostics, resolver, type summaries, ownership summaries,
IR summaries, backend summaries, and cache contract summaries. Compiler logic
should keep moving into these files through GitHub Issues instead of expanding
`frontend.kizu`.

## Current Entry

```sh
kizu run selfhost/frontend.kizu
kizu run selfhost/frontend.kizu -- selfhost/fixtures/simple.kizu
```

The package currently includes:

- token representation
- Go-shaped token kinds and lexer-shaped source scanning with `std::mem`
- token storage with `std::array::Array<T>`
- diagnostic text construction with `std::string::String`
- parser summary shape
- executable package component tests for lexer, parser, diagnostics/resolver,
  type summaries, ownership summaries, IR summaries, backend summaries, and
  cache contract summaries
- compiler1-8 legacy stage report covering lexer, parser, diagnostics, type,
  ownership, IR, backend artifact, and bootstrap readiness
- source loading through `std::fs`
- path decomposition through `std::path`
- prototype CLI argument handling through `std::process`
- explicit output through `std::io`
- Kizu-native component assertions through `std::testing`

The default smoke input is `selfhost/fixtures/simple.kizu`. Passing a path after
`--` exercises the same runner path with explicit process arguments.
`selfhost/fixtures/simple_tokens.kizu` is a wider lexer corpus used by Go tests
to compare the full self-host token-kind stream against the production lexer.

## v0.3 Target

v0.3 is the Kizu-only standalone self-host compiler milestone. This directory is
not v0.3 complete until the compiler package can be built into a standalone
artifact that can check/build Kizu programs and rebuild `selfhost` without using
the Go CLI/interpreter as the compiler execution path.

The current standard-library bridge and component tests are bootstrap inputs for
v0.3. New compiler implementation work should build from the module-first
package tree, not by adding more production logic to `frontend.kizu`.

`frontend.kizu` can be deleted after the module tree owns the same oracle
surface with package-level tests and the standalone compiler path is explicit.

The v0.3 umbrella is #256.

## Conformance Reuse

The self-host compiler must reuse `tests/conformance/v0_*.json` and produce the
same pass/fail behavior as the Go implementation before it can replace any
production path.

Go tests parse, check, and run the self-host package source. They compare
self-host token snapshots with the production Go lexer, normalized AST snapshots
with the Go parser, semantic snapshots with the Go checker oracle, and
IR/backend/cache summary facts with the bootstrap audit. Missing APIs found
while growing this directory must be reflected back into the relevant stdlib or
compiler Issue instead of becoming isolated TODOs here.

## Compiler Stage Harness

`frontend.kizu` still runs the legacy eight-stage smoke path:

1. lexer
2. parser snapshot
3. diagnostics
4. type-check preconditions
5. ownership-check preconditions
6. IR item lowering summary
7. backend artifact summary
8. bootstrap readiness check

The module-first package now has executable component tests for every stage
boundary. The legacy smoke path remains useful as broad stdlib coverage until
the module tree fully replaces it.

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

The semantic oracle currently compares a normalized symbol/diagnostic snapshot
for selected positive sources and keeps selected memory-safety negative fixtures
paired with the Go checker diagnostics. This is the v0.3 bridge for stages 3-5:
diagnostics, type preconditions, and ownership preconditions.

The IR/backend oracle currently compares normalized function, block, and backend
artifact counts. The phase-by-phase 1:1 contract is documented in
[`docs/bootstrap.md`](../docs/bootstrap.md).

Current stdlib feedback is tracked in [`STDLIB.md`](STDLIB.md).

## Source Policy

Kizu compiler source in this directory follows the stricter compiler-code style:

- module comment at the top of each file
- comment immediately before each function
- line width at most 100 columns
- function body at most 70 lines
- function body at most 45 semicolon-terminated statements
