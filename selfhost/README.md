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
- source loading through `std::fs`
- path decomposition through `std::path`
- prototype CLI argument handling through `std::process`
- explicit output through `std::io`
- Kizu-native component assertions through `std::testing`

The default smoke input is `selfhost/fixtures/simple.kizu`. Passing a path after
`--` exercises the same runner path with explicit process arguments.

## v0.3 Handoff

The v0.2 standard-library bridge is complete when `tests/selfhost` can parse,
check, and run the skeleton through the same APIs a future compiler frontend
will need. #31 should build from this by replacing the parser summary with real
lexer, parser, AST, diagnostics, and conformance comparison components.

## Conformance Reuse

The future self-host compiler must reuse `tests/conformance/v0_*.json` and
produce the same pass/fail behavior as the Go implementation before it can
replace any production path.

For now, Go tests parse, check, and run the skeleton source. They also compare
the self-host parser summary for `selfhost/fixtures/simple.kizu` with the
production Go lexer function-token count. Missing APIs found while growing this
directory must be reflected back into the relevant v0.2 stdlib issue instead of
becoming isolated TODOs here.

Current stdlib feedback is tracked in [`STDLIB.md`](STDLIB.md).

## Source Policy

Kizu compiler source in this directory follows the stricter compiler-code style:

- module comment at the top of each file
- comment immediately before each function
- line width at most 100 columns
- function body at most 70 lines
- function body at most 45 semicolon-terminated statements
